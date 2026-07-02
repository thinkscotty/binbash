package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
)

type Bin struct {
	ID          int64
	Name        string
	Description string
	Category    string
}

func (h *Handlers) ListBins(w http.ResponseWriter, r *http.Request) {
	h.renderBins(w, nil)
}

func (h *Handlers) CreateBin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))
	category := strings.TrimSpace(r.FormValue("category"))

	if formErr := validateBin(name, category, description); formErr != "" {
		h.renderBins(w, map[string]any{
			"Error":       formErr,
			"Name":        name,
			"Description": description,
			"Category":    category,
		})
		return
	}

	if _, err := h.DB.Exec(
		`INSERT INTO bins (name, description, category) VALUES (?, ?, ?)`,
		name, description, category,
	); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/bins", http.StatusSeeOther)
}

func (h *Handlers) EditBinForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	bin, err := h.loadBin(id)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.render(w, "bin_edit.html", map[string]any{"Bin": bin})
}

func (h *Handlers) UpdateBin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))
	category := strings.TrimSpace(r.FormValue("category"))

	if formErr := validateBin(name, category, description); formErr != "" {
		h.render(w, "bin_edit.html", map[string]any{
			"Error": formErr,
			"Bin":   Bin{ID: id, Name: name, Description: description, Category: category},
		})
		return
	}

	res, err := h.DB.Exec(
		`UPDATE bins SET name = ?, description = ?, category = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, description, category, id,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, "/bins", http.StatusSeeOther)
}

// binExists reports whether a bin with the given id is present.
func (h *Handlers) binExists(id int64) (bool, error) {
	var n int
	err := h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM bins WHERE id = ?)`, id).Scan(&n)
	return n == 1, err
}

func (h *Handlers) loadBin(id int64) (Bin, error) {
	var b Bin
	err := h.DB.QueryRow(
		`SELECT id, name, COALESCE(description, ''), COALESCE(category, '') FROM bins WHERE id = ?`, id,
	).Scan(&b.ID, &b.Name, &b.Description, &b.Category)
	return b, err
}

func (h *Handlers) renderBins(w http.ResponseWriter, data map[string]any) {
	bins, err := h.loadBins()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if data == nil {
		data = map[string]any{}
	}
	data["Bins"] = bins
	h.render(w, "bins.html", data)
}

func (h *Handlers) loadBins() ([]Bin, error) {
	rows, err := h.DB.Query(`SELECT id, name, COALESCE(description, ''), COALESCE(category, '') FROM bins ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bins []Bin
	for rows.Next() {
		var b Bin
		if err := rows.Scan(&b.ID, &b.Name, &b.Description, &b.Category); err != nil {
			return nil, err
		}
		bins = append(bins, b)
	}
	return bins, rows.Err()
}
