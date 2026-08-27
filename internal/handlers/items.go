package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
)

// recentItemsLimit caps the "Recent items" list under the add-item form. That
// list exists to confirm what you just added, not to browse the whole
// inventory -- and CreateItem redirects straight back to this page, so without
// a cap every single add re-renders every item ever entered. Measured at
// household scale that reached well over a megabyte of HTML per add, on the
// one page where entry speed matters most. Browsing lives on the bins page and
// in search, which already caps at the same number.
const recentItemsLimit = 50

type Item struct {
	ID          int64
	BinID       int64
	BinName     string
	Name        string
	Description string
	Keywords    string
	// Color is a slug from colorTags, or "" for no color tag. Always loaded
	// and always stored, whether or not the feature is switched on, so that
	// turning it off hides the colors rather than losing them.
	Color string
}

// ColorName is the human-readable name of this item's color tag ("Light
// blue"), or "" if it has none. A method rather than a template FuncMap so
// the dot's screen-reader label costs nothing at the template-parsing layer.
func (i Item) ColorName() string {
	return ColorLabel(i.Color)
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
	color := h.submittedColor(r)

	if formErr := validateItem(name, description, keywords, binID, binErr); formErr != "" {
		h.renderItems(w, map[string]any{
			"Error":         formErr,
			"Name":          name,
			"Description":   description,
			"Keywords":      keywords,
			"Color":         color,
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
			"Color":         color,
			"SelectedBinID": binID,
		})
		return
	}

	if _, err := h.DB.Exec(
		`INSERT INTO items (bin_id, name, description, keywords, color) VALUES (?, ?, ?, ?, ?)`,
		binID, name, description, keywords, color,
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
	color := h.submittedColor(r)

	if formErr := validateItem(name, description, keywords, binID, binErr); formErr != "" {
		bins, err := h.loadBins()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.render(w, "item_edit.html", map[string]any{
			"Error": formErr,
			"Item":  Item{ID: id, BinID: binID, Name: name, Description: description, Keywords: keywords, Color: color},
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
			"Item":  Item{ID: id, BinID: binID, Name: name, Description: description, Keywords: keywords, Color: color},
			"Bins":  bins,
		})
		return
	}

	// Two statements rather than one, because with color tags switched off the
	// edit form doesn't render the swatches and so the submission carries no
	// color at all. Writing the (empty) submitted value would then quietly
	// erase a color the item was tagged with while the feature was on -- so
	// when it's off, the column is left out of the SET clause entirely.
	var res sql.Result
	if h.ColorTags.Enabled {
		res, err = h.DB.Exec(
			`UPDATE items SET bin_id = ?, name = ?, description = ?, keywords = ?, color = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			binID, name, description, keywords, color, id,
		)
	} else {
		res, err = h.DB.Exec(
			`UPDATE items SET bin_id = ?, name = ?, description = ?, keywords = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			binID, name, description, keywords, id,
		)
	}
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

// submittedColor reads the color the item form posted, normalized to a known
// slug. It reports "" whenever the feature is off, so a stale browser tab left
// open from before it was switched off -- or a hand-crafted POST -- can't
// write a color into an instance that isn't using them.
func (h *Handlers) submittedColor(r *http.Request) string {
	if !h.ColorTags.Enabled {
		return ""
	}
	return normalizeColor(r.FormValue("color"))
}

func (h *Handlers) loadItem(id int64) (Item, error) {
	var it Item
	err := h.DB.QueryRow(
		`SELECT items.id, items.bin_id, bins.name, items.name, COALESCE(items.description, ''), COALESCE(items.keywords, ''), items.color
		 FROM items JOIN bins ON bins.id = items.bin_id WHERE items.id = ?`, id,
	).Scan(&it.ID, &it.BinID, &it.BinName, &it.Name, &it.Description, &it.Keywords, &it.Color)
	return it, err
}

func (h *Handlers) renderItems(w http.ResponseWriter, data map[string]any) {
	bins, err := h.loadBins()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// One row more than we display, so "is there anything older than this?" is
	// answered by the same query instead of a second COUNT.
	rows, err := h.DB.Query(
		`SELECT items.id, items.bin_id, bins.name, items.name, COALESCE(items.description, ''), COALESCE(items.keywords, ''), items.color
		 FROM items JOIN bins ON bins.id = items.bin_id ORDER BY items.id DESC LIMIT ?`,
		recentItemsLimit+1,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.BinID, &it.BinName, &it.Name, &it.Description, &it.Keywords, &it.Color); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	moreItems := len(items) > recentItemsLimit
	if moreItems {
		items = items[:recentItemsLimit]
	}

	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["SelectedBinID"]; !ok {
		var lastBinID int64
		h.DB.QueryRow(`SELECT bin_id FROM items ORDER BY id DESC LIMIT 1`).Scan(&lastBinID)
		data["SelectedBinID"] = lastBinID
	}
	// Always a string, even on the happy path where nothing set it: the
	// swatch template compares it against each slug with eq, and a missing map
	// key would arrive as nil and fail that comparison on type mismatch.
	//
	// Deliberately *not* carried over from the last item the way SelectedBinID
	// is. A bin is required, so defaulting it only saves a mandatory step; a
	// color is optional, and "no dot" positively means "not in a bag" -- so a
	// remembered color would silently claim something false about every item
	// added after you stopped using that bag.
	if _, ok := data["Color"]; !ok {
		data["Color"] = ""
	}
	data["Bins"] = bins
	data["Items"] = items
	data["MoreItems"] = moreItems
	data["AIEnabled"] = h.AI != nil
	if h.AI != nil {
		var untagged int
		h.DB.QueryRow(`SELECT COUNT(*) FROM items WHERE ai_tagged = 0`).Scan(&untagged)
		data["UntaggedCount"] = untagged
	}
	h.render(w, "items.html", data)
}
