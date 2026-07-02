package handlers

import (
	"fmt"
	"math"
	"net/http"
	"time"
)

func (h *Handlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.Auth.IsAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.render(w, "login.html", nil)
}

func (h *Handlers) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	ip := h.Auth.ClientIP(r)
	if wait, blocked := h.Auth.Throttled(ip); blocked {
		h.render(w, "login.html", map[string]any{"Error": lockoutMessage(wait)})
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if !h.Auth.CheckPassword(r.FormValue("password")) {
		h.Auth.RecordFailure(ip)
		h.render(w, "login.html", map[string]any{"Error": "Incorrect password"})
		return
	}

	h.Auth.RecordSuccess(ip)
	h.Auth.Login(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) LogoutSubmit(w http.ResponseWriter, r *http.Request) {
	h.Auth.Logout(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// lockoutMessage formats a friendly, minute-granularity wait time for a
// throttled login attempt.
func lockoutMessage(wait time.Duration) string {
	mins := int(math.Ceil(wait.Minutes()))
	if mins < 1 {
		mins = 1
	}
	if mins == 1 {
		return "Too many attempts. Try again in 1 minute."
	}
	return fmt.Sprintf("Too many attempts. Try again in %d minutes.", mins)
}
