package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
)

type Item struct {
	ID          int64
	BinID       int64
	BinName     string
	Name        string
	Description string
	Keywords    string
}

func (h *Handlers) ListItems(w http.ResponseWriter, r *http.Request) {
	// ?bin=N preselects that bin in the add-item form, so "Add an item" from a
	// bin on the bins page lands here ready to type. An unknown or malformed id
	// just falls through to renderItems' usual default of the last-used bin.
	var data map[string]any
	if binID, err := strconv.ParseInt(r.URL.Query().Get("bin"), 10, 64); err == nil {
		if exists, err := h.binExists(binID); err == nil && exists {
			data = map[string]any{"SelectedBinID": binID}
		}
	}
	h.renderItems(w, data)
}

func (h *Handlers) CreateItem(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Normalize before validating so the stored name, the length check, and the
	// value echoed back into the form on error are all the same string.
	name := titleCase(strings.TrimSpace(r.FormValue("name")))
	description := strings.TrimSpace(r.FormValue("description"))
	keywords := strings.TrimSpace(r.FormValue("keywords"))
	binID, binErr := strconv.ParseInt(r.FormValue("bin_id"), 10, 64)

	if formErr := validateItem(name, description, keywords, binID, binErr); formErr != "" {
		h.renderItems(w, map[string]any{
			"Error":         formErr,
			"Name":          name,
			"Description":   description,
			"Keywords":      keywords,
			"SelectedBinID": binID,
		})
		return
	}

	exists, err := h.binExists(binID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !exists {
		h.renderItems(w, map[string]any{
			"Error":         "That bin no longer exists. Pick another and try again.",
			"Name":          name,
			"Description":   description,
			"Keywords":      keywords,
			"SelectedBinID": binID,
		})
		return
	}

	if _, err := h.DB.Exec(
		`INSERT INTO items (bin_id, name, description, keywords) VALUES (?, ?, ?, ?)`,
		binID, name, description, keywords,
	); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/items", http.StatusSeeOther)
}

func (h *Handlers) EditItemForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	item, err := h.loadItem(id)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	bins, err := h.loadBins()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.render(w, "item_edit.html", map[string]any{"Item": item, "Bins": bins})
}

func (h *Handlers) UpdateItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Deliberately *not* title-cased. Speed matters when first entering an item;
	// by the time someone opens the edit form they want control, so an edit is
	// the escape hatch for casing titleCase can't express — a brand that is
	// meant to stay all-lowercase, say.
	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))
	keywords := strings.TrimSpace(r.FormValue("keywords"))
	binID, binErr := strconv.ParseInt(r.FormValue("bin_id"), 10, 64)

	if formErr := validateItem(name, description, keywords, binID, binErr); formErr != "" {
		bins, err := h.loadBins()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.render(w, "item_edit.html", map[string]any{
			"Error": formErr,
			"Item":  Item{ID: id, BinID: binID, Name: name, Description: description, Keywords: keywords},
			"Bins":  bins,
		})
		return
	}

	exists, err := h.binExists(binID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !exists {
		bins, err := h.loadBins()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.render(w, "item_edit.html", map[string]any{
			"Error": "That bin no longer exists. Pick another and try again.",
			"Item":  Item{ID: id, BinID: binID, Name: name, Description: description, Keywords: keywords},
			"Bins":  bins,
		})
		return
	}

	res, err := h.DB.Exec(
		`UPDATE items SET bin_id = ?, name = ?, description = ?, keywords = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		binID, name, description, keywords, id,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, "/items", http.StatusSeeOther)
}

func (h *Handlers) DeleteItemConfirm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	item, err := h.loadItem(id)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.render(w, "item_delete.html", map[string]any{"Item": item})
}

func (h *Handlers) DeleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// The items_ad trigger keeps the FTS index in sync on delete, so search
	// won't surface a ghost of the removed item.
	res, err := h.DB.Exec(`DELETE FROM items WHERE id = ?`, id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, "/items", http.StatusSeeOther)
}

func (h *Handlers) loadItem(id int64) (Item, error) {
	var it Item
	err := h.DB.QueryRow(
		`SELECT items.id, items.bin_id, bins.name, items.name, COALESCE(items.description, ''), COALESCE(items.keywords, '')
		 FROM items JOIN bins ON bins.id = items.bin_id WHERE items.id = ?`, id,
	).Scan(&it.ID, &it.BinID, &it.BinName, &it.Name, &it.Description, &it.Keywords)
	return it, err
}

func (h *Handlers) renderItems(w http.ResponseWriter, data map[string]any) {
	bins, err := h.loadBins()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	rows, err := h.DB.Query(
		`SELECT items.id, items.bin_id, bins.name, items.name, COALESCE(items.description, ''), COALESCE(items.keywords, '')
		 FROM items JOIN bins ON bins.id = items.bin_id ORDER BY items.id DESC`,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var items []Item
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

	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["SelectedBinID"]; !ok {
		var lastBinID int64
		h.DB.QueryRow(`SELECT bin_id FROM items ORDER BY id DESC LIMIT 1`).Scan(&lastBinID)
		data["SelectedBinID"] = lastBinID
	}
	data["Bins"] = bins
	data["Items"] = items
	data["AIEnabled"] = h.AI != nil
	if h.AI != nil {
		var untagged int
		h.DB.QueryRow(`SELECT COUNT(*) FROM items WHERE ai_tagged = 0`).Scan(&untagged)
		data["UntaggedCount"] = untagged
	}
	h.render(w, "items.html", data)
}
