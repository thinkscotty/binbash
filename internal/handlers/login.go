package handlers

import "net/http"

func (h *Handlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.Auth.IsAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.render(w, "login.html", nil)
}

func (h *Handlers) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if !h.Auth.CheckPassword(r.FormValue("password")) {
		h.render(w, "login.html", map[string]any{"Error": "Incorrect password"})
		return
	}

	h.Auth.Login(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) LogoutSubmit(w http.ResponseWriter, r *http.Request) {
	h.Auth.Logout(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
