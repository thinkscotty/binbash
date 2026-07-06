package handlers

import (
	"fmt"
	"regexp"
	"strings"
)

// binWordPattern matches "bin" (or the plural "bins") as a standalone,
// case-insensitive word. Plain substring matching would misfire on words that
// merely contain the letters b-i-n such as "cabinet", "cabin", or "binder", so
// we require word boundaries to honour the intent: only locations that
// actually name a bin qualify.
var binWordPattern = regexp.MustCompile(`(?i)\bbins?\b`)

// deriveBinName collapses a Homebox location path into a single flat binbash
// bin name. Homebox locations are "/"-separated paths that can nest several
// levels deep (e.g. "Upstairs / Electronics Bin / Passive Electronics Mini
// Bin"); binbash bins are flat. We walk from the deepest segment outward and
// return the first segment that names a bin — "deepest bin wins", so an item
// sitting in a mini-bin lands in that mini-bin rather than being merged up into
// its parent. A path with no bin segment (e.g. a bare "Garage" holding loose
// items) returns "", signalling the caller to skip the row: the point of the
// import is knowing which labelled bin to open, and such a location names none.
func deriveBinName(location string) string {
	segments := strings.Split(location, "/")
	for i := len(segments) - 1; i >= 0; i-- {
		seg := strings.TrimSpace(segments[i])
		if binWordPattern.MatchString(seg) {
			return seg
		}
	}
	return ""
}

// isHomeboxHeader reports whether a CSV header row looks like a Homebox item
// export. Homebox prefixes every column with "HB."; we key off the two columns
// the import actually needs (HB.name and HB.location) rather than an exact
// full-header match, so a Homebox version that adds or reorders columns still
// imports.
func isHomeboxHeader(header []string) bool {
	idx := headerIndex(header)
	_, hasName := idx["HB.name"]
	_, hasLocation := idx["HB.location"]
	return hasName && hasLocation
}

// headerIndex maps each (trimmed) column name in a CSV header row to its index,
// so parsers can look columns up by name instead of hard-coding positions.
func headerIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, name := range header {
		idx[strings.TrimSpace(name)] = i
	}
	return idx
}

// parseHomeboxCSV converts a Homebox item export (row 0 already confirmed by
// isHomeboxHeader) into importRows. Only four columns map onto binbash: name,
// description, tags->keywords, and location->bin name; every other Homebox
// column (asset id, warranty, purchase, serial, quantity, sold_*, ...) is
// intentionally dropped, since binbash tracks where things are, not stock
// levels or provenance.
//
// Unlike parseBinbashCSV this path is lenient: it is migrating another app's
// data, so rather than aborting the whole import on an imperfect row it skips
// rows that can't be placed and truncates over-long fields to binbash's limits,
// reporting the skipped counts back to the caller.
func parseHomeboxCSV(records [][]string) (rows []importRow, skippedNoBin, skippedArchived, skippedNoName int) {
	idx := headerIndex(records[0])
	col := func(name string) int {
		if i, ok := idx[name]; ok {
			return i
		}
		return -1
	}
	nameCol := col("HB.name")
	locCol := col("HB.location")
	descCol := col("HB.description")
	tagsCol := col("HB.tags")
	archCol := col("HB.archived")

	// get reads a trimmed cell, tolerating absent columns (-1) and short rows
	// so a ragged file can never panic on an out-of-range index.
	get := func(rec []string, i int) string {
		if i < 0 || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	rows = make([]importRow, 0, len(records)-1)
	for _, rec := range records[1:] {
		if strings.EqualFold(get(rec, archCol), "true") {
			skippedArchived++
			continue
		}
		itemName := get(rec, nameCol)
		if itemName == "" {
			skippedNoName++
			continue
		}
		binName := deriveBinName(get(rec, locCol))
		if binName == "" {
			skippedNoBin++
			continue
		}
		rows = append(rows, importRow{
			itemName:        truncateRunes(itemName, maxNameLen),
			itemDescription: truncateRunes(get(rec, descCol), maxDescriptionLen),
			keywords:        truncateRunes(homeboxTags(get(rec, tagsCol)), maxKeywordsLen),
			binName:         truncateRunes(binName, maxNameLen),
			// Homebox locations carry no binbash-equivalent category or
			// description, so new bins are created bare; a merge into an
			// existing bin leaves that bin's own fields untouched.
		})
	}
	return rows, skippedNoBin, skippedArchived, skippedNoName
}

// homeboxTags converts Homebox's ";"-separated tag list into binbash's
// comma-separated keywords, dropping blank entries. e.g. "DIY Electronics;
// Battery and Battery Charging" -> "DIY Electronics, Battery and Battery Charging".
func homeboxTags(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

// homeboxSkipNote renders the "Skipped ..." sentence appended to a Homebox
// import's success message, or "" when nothing was skipped. Each bucket is
// mentioned only when non-zero, so a clean import stays quiet.
func homeboxSkipNote(noBin, archived, noName int) string {
	var parts []string
	if noBin > 0 {
		parts = append(parts, fmt.Sprintf("%d with no bin in their location", noBin))
	}
	if archived > 0 {
		parts = append(parts, fmt.Sprintf("%d archived", archived))
	}
	if noName > 0 {
		parts = append(parts, fmt.Sprintf("%d with no name", noName))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Skipped " + strings.Join(parts, ", ") + "."
}
