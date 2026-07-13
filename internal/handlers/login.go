package handlers

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/thinkscotty/binbash/internal/auth"
)

// logAuthFailure records a rejected password attempt in a single, stable,
// greppable line. This is the hook fail2ban (see deploy/fail2ban/) matches on
// to ban repeat offenders at the firewall, which keeps a password-guessing
// flood off the app entirely -- and, just as importantly, off bcrypt, which is
// deliberately slow and would otherwise burn real CPU on every guess.
//
// The format is load-bearing: "binbash: auth failure from <ip>" is what the
// shipped filter's failregex expects, so changing it silently breaks anyone's
// existing fail2ban setup. The attempted password is deliberately never logged.
func logAuthFailure(ip, reason string) {
	log.Printf("binbash: auth failure from %s: %s", ip, reason)
}

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
		// Logged as a failure too, so an attacker who keeps hammering a
		// locked-out IP keeps feeding fail2ban the lines it needs to ban them.
		logAuthFailure(ip, "attempt while locked out")
		h.render(w, "login.html", map[string]any{"Error": lockoutMessage(wait)})
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if !h.Auth.CheckPassword(r.FormValue("password")) {
		logAuthFailure(ip, "incorrect password at /login")
		if lockedOut := h.Auth.RecordFailure(ip); lockedOut {
			log.Printf("binbash: locked out %s for 15 minutes after %d failed attempts", ip, auth.MaxAttempts)
		}
		h.render(w, "login.html", map[string]any{"Error": "Incorrect password"})
		return
	}

	// Successes are logged as well: on an internet-facing install this is the
	// line that tells the owner someone else got in.
	log.Printf("binbash: login succeeded from %s", ip)
	h.Auth.RecordSuccess(ip)
	h.Auth.Login(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) LogoutSubmit(w http.ResponseWriter, r *http.Request) {
	h.Auth.Logout(w, r)
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
