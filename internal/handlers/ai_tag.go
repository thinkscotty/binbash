package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/thinkscotty/binbash/internal/ai"
)

const (
	aiTagBatchSize               = 25
	aiTagConsecutiveFailureLimit = 3

	// aiTagBatchTimeout bounds the whole batch's wall-clock time, comfortably
	// under common reverse-proxy read-timeout defaults (e.g. nginx's 60s), so
	// a slow-but-successful provider can't hold the request open for the
	// worst-case 25 * 30s-per-item duration. Items past the cutoff are simply
	// left untagged for the next click, same as any other stopping point.
	aiTagBatchTimeout = 45 * time.Second
)

type untaggedItem struct {
	id                                                                int64
	name, description, keywords, binName, binCategory, binDescription string
}

// TagItems tags up to aiTagBatchSize untagged items (items.ai_tagged = 0),
// appending AI-generated keywords onto each item's existing keywords field
// and marking it ai_tagged so future runs skip it. Existing keyword text is
// never modified or removed, only appended to. Each item is committed with
// its own UPDATE as soon as it's tagged, so a mid-batch failure still keeps
// everything tagged so far. Stops early after several consecutive API
// failures rather than grinding through the rest of the batch against a
// broken endpoint.
func (h *Handlers) TagItems(w http.ResponseWriter, r *http.Request) {
	if h.AI == nil {
		h.renderItems(w, map[string]any{"Error": "AI tagging isn't configured."})
		return
	}

	if !h.aiTagMu.TryLock() {
		h.renderItems(w, map[string]any{"Error": "Tagging is already running — give it a moment and try again."})
		return
	}
	defer h.aiTagMu.Unlock()

	rows, err := h.DB.Query(`
		SELECT items.id, items.name, COALESCE(items.description, ''), COALESCE(items.keywords, ''),
		       bins.name, COALESCE(bins.category, ''), COALESCE(bins.description, '')
		FROM items JOIN bins ON bins.id = items.bin_id
		WHERE items.ai_tagged = 0
		ORDER BY items.id
		LIMIT ?`, aiTagBatchSize)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var untagged []untaggedItem
	for rows.Next() {
		var it untaggedItem
		if err := rows.Scan(&it.id, &it.name, &it.description, &it.keywords, &it.binName, &it.binCategory, &it.binDescription); err != nil {
			rows.Close()
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		untagged = append(untagged, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), aiTagBatchTimeout)
	defer cancel()

	tagged, failed, emptied, skipped, consecutiveFailures := 0, 0, 0, 0, 0
	timedOut := false
	for _, it := range untagged {
		if ctx.Err() != nil {
			timedOut = true
			break
		}
		tags, err := h.AI.TagItem(ctx, ai.ItemContext{
			Name:             it.name,
			Description:      it.description,
			ExistingKeywords: it.keywords,
			BinName:          it.binName,
			BinCategory:      it.binCategory,
			BinDescription:   it.binDescription,
		}, h.AITagCount, h.AITagBreadth)
		if err != nil {
			log.Printf("ai tag item %d: %v", it.id, err)
			failed++
			consecutiveFailures++
			if consecutiveFailures >= aiTagConsecutiveFailureLimit {
				break
			}
			continue
		}
		if len(tags) == 0 && h.AITagCount > 0 {
			// A successful call that yielded no usable tags -- typically a
			// reasoning model that spent its whole token budget thinking and
			// never emitted the answer (empty content). Leave the item
			// untagged so it's retried next run instead of being silently
			// marked done with nothing. (tag_count == 0 is the deliberate
			// "mark tagged without generating" mode: TagItem returns no tags
			// by design there, so it's excluded from this check and falls
			// through to the UPDATE below.)
			log.Printf("ai tag item %d: response contained no tags", it.id)
			emptied++
			consecutiveFailures++
			if consecutiveFailures >= aiTagConsecutiveFailureLimit {
				break
			}
			continue
		}
		consecutiveFailures = 0

		// Drop tags that only restate words already in the item's name. The name
		// is full-text indexed (see the items_fts virtual table), so such tags
		// widen the item's search surface by nothing -- they're display clutter.
		// This runs after the empty-response check above, so the item genuinely
		// got tags back (or tag_count is 0). If the filter removes everything, we
		// still mark the item done: the model gave a real answer with nothing
		// worth keeping, and retrying would only reproduce the same echoes.
		filtered := dropNameEchoes(tags, it.name)

		merged := mergeKeywords(it.keywords, filtered)
		if _, err := h.DB.Exec(
			`UPDATE items SET keywords = ?, ai_tagged = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			merged, it.id,
		); err != nil {
			log.Printf("ai tag item %d: update: %v", it.id, err)
			failed++
			continue
		}
		if len(filtered) > 0 {
			tagged++
		} else {
			skipped++
		}
	}

	msg := fmt.Sprintf("Tagged %d item(s).", tagged)
	if skipped > 0 {
		msg += fmt.Sprintf(" %d had no new tags to add.", skipped)
	}
	if emptied > 0 {
		msg += fmt.Sprintf(" %d returned no tags and will be retried next run.", emptied)
	}
	if failed > 0 {
		msg += fmt.Sprintf(" %d failed — check the server log and try again.", failed)
	}
	if timedOut {
		msg += " Stopped early to keep this page responsive — click the button again to keep going."
	}
	h.renderItems(w, map[string]any{"Success": msg})
}

// mergeKeywords appends newTags onto existing (a comma-separated keyword
// string), skipping any tag already present in existing, case-insensitively.
// existing is never altered or reordered, only extended.
func mergeKeywords(existing string, newTags []string) string {
	if len(newTags) == 0 {
		return existing
	}

	seen := map[string]bool{}
	for _, k := range strings.Split(existing, ",") {
		if k = strings.TrimSpace(k); k != "" {
			seen[strings.ToLower(k)] = true
		}
	}

	merged := existing
	for _, tag := range newTags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[strings.ToLower(tag)] {
			continue
		}
		seen[strings.ToLower(tag)] = true
		if merged == "" {
			merged = tag
		} else {
			merged += ", " + tag
		}
	}
	return merged
}

// dropNameEchoes removes AI-suggested tags whose every word already appears in
// the item's name. binbash full-text indexes the item name (see the items_fts
// virtual table), so a tag that merely restates name words adds no new way to
// find the item -- it's redundant clutter. A tag with at least one word not in
// the name is kept, since that word is a genuinely new search term. Matching is
// case-insensitive with light plural tolerance ("necklace"/"necklaces"),
// approximating the porter stemmer the search index uses.
func dropNameEchoes(tags []string, name string) []string {
	nameWords := splitWords(name)
	if len(nameWords) == 0 {
		return tags
	}
	kept := make([]string, 0, len(tags))
	for _, tag := range tags {
		if !isNameEcho(tag, nameWords) {
			kept = append(kept, tag)
		}
	}
	return kept
}

// isNameEcho reports whether every word in tag already appears in nameWords, so
// the tag contributes no search term the name doesn't already carry. A tag with
// no word tokens at all (e.g. punctuation only) is not treated as an echo.
func isNameEcho(tag string, nameWords []string) bool {
	tagWords := splitWords(tag)
	if len(tagWords) == 0 {
		return false
	}
	for _, tw := range tagWords {
		if !containsWord(nameWords, tw) {
			return false
		}
	}
	return true
}

// containsWord reports whether w matches any of words, tolerating a single
// trailing "s" difference so a plural tag doesn't slip past a singular name
// word or vice versa. All inputs are already lowercased by splitWords.
func containsWord(words []string, w string) bool {
	for _, x := range words {
		if x == w || x+"s" == w || w+"s" == x {
			return true
		}
	}
	return false
}

// splitWords lowercases s and splits it into alphanumeric word tokens,
// discarding punctuation and whitespace.
func splitWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}
