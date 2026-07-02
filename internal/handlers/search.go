package handlers

import "net/http"

func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	var binCount, itemCount int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM bins`).Scan(&binCount); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&itemCount); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.render(w, "home.html", map[string]any{
		"BinCount":  binCount,
		"ItemCount": itemCount,
	})
}
