package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/thinkscotty/binbash/internal/ai"
)

const (
	aiTagBatchSize               = 25
	aiTagConsecutiveFailureLimit = 3
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

	tagged, failed, consecutiveFailures := 0, 0, 0
	for _, it := range untagged {
		tags, err := h.AI.TagItem(r.Context(), ai.ItemContext{
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
		consecutiveFailures = 0

		merged := mergeKeywords(it.keywords, tags)
		if _, err := h.DB.Exec(
			`UPDATE items SET keywords = ?, ai_tagged = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			merged, it.id,
		); err != nil {
			log.Printf("ai tag item %d: update: %v", it.id, err)
			failed++
			continue
		}
		tagged++
	}

	msg := fmt.Sprintf("Tagged %d item(s).", tagged)
	if failed > 0 {
		msg += fmt.Sprintf(" %d failed — check the server log and try again.", failed)
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
