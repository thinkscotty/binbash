package handlers

import (
	"net/http"
	"strings"
)

func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	var binCount, itemCount int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM bins`).Scan(&binCount); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&itemCount); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if tooLong(query, maxSearchLen) {
		query = truncateRunes(query, maxSearchLen)
	}

	var items []Item
	if ftsQuery := buildFTSQuery(query); ftsQuery != "" {
		rows, err := h.DB.Query(
			`SELECT items.id, items.bin_id, bins.name, items.name, COALESCE(items.description, ''), COALESCE(items.keywords, '')
			 FROM items_fts
			 JOIN items ON items.id = items_fts.rowid
			 JOIN bins ON bins.id = items.bin_id
			 WHERE items_fts MATCH ?
			 ORDER BY rank
			 LIMIT 50`, ftsQuery,
		)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var it Item
			if err := rows.Scan(&it.ID, &it.BinID, &it.BinName, &it.Name, &it.Description, &it.Keywords); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	newItems, _ := h.checkAndRunAutoBackup()

	h.render(w, "search.html", map[string]any{
		"BinCount":    binCount,
		"ItemCount":   itemCount,
		"Query":       query,
		"Searched":    query != "",
		"Items":       items,
		"NewItems":    newItems,
		"ReminderDue": newItems >= autoBackupItemThreshold,
	})
}

// buildFTSQuery turns free-text user input into a safe FTS5 MATCH expression.
// Each whitespace-separated token is wrapped in double quotes (doubling any
// embedded double quote per FTS5 escaping rules) and given a trailing '*' for
// prefix matching, so tokens like "-kit" or a stray '"' can't be misread as
// FTS5 query-language operators (leading '-' is NOT, an unescaped '"' breaks
// the string, etc). Tokens are implicitly AND'd. Returns "" when there's
// nothing to search — callers must skip the query entirely then, since
// MATCH '' is a syntax error, not zero results.
func buildFTSQuery(raw string) string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, `"`+strings.ReplaceAll(f, `"`, `""`)+`"*`)
	}
	return strings.Join(parts, " ")
}
