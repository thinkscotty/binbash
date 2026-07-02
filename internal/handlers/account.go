package handlers

import "net/http"

func (h *Handlers) AccountPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, "account.html", nil)
}

func (h *Handlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	// Shares the login endpoint's throttle state (keyed by IP): otherwise
	// someone locked out of /login for guessing the password could just
	// switch to this endpoint and keep guessing unthrottled.
	ip := h.Auth.ClientIP(r)
	if wait, blocked := h.Auth.Throttled(ip); blocked {
		h.render(w, "account.html", map[string]any{"Error": lockoutMessage(wait)})
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	current := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	currentOK := h.Auth.CheckPassword(current)
	if !currentOK {
		h.Auth.RecordFailure(ip)
	}

	if errMsg := validatePasswordChange(currentOK, newPassword, confirm); errMsg != "" {
		h.render(w, "account.html", map[string]any{"Error": errMsg})
		return
	}

	if err := h.Auth.Rotate(newPassword); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.Auth.RecordSuccess(ip)
	h.render(w, "account.html", map[string]any{"Success": "Password updated."})
}
