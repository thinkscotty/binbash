package handlers

import "net/http"

func (h *Handlers) AccountPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "account.html", nil)
}

func (h *Handlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	current := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	if errMsg := validatePasswordChange(h.Auth.CheckPassword(current), newPassword, confirm); errMsg != "" {
		h.render(w, "account.html", map[string]any{"Error": errMsg})
		return
	}

	if err := h.Auth.Rotate(newPassword); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.render(w, "account.html", map[string]any{"Success": "Password updated."})
}
