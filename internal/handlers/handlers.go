// Package handlers implements binbash's HTTP handlers.
package handlers

import (
	"bytes"
	"database/sql"
	"html/template"
	"log"
	"net/http"

	"github.com/thinkscotty/binbash/internal/auth"
)

// Templates maps a page name (e.g. "login.html") to a template set combining
// that page's content with the shared layout. Each entry is self-contained
// so pages can safely reuse the same block names (e.g. "content").
type Templates map[string]*template.Template

type Handlers struct {
	DB        *sql.DB
	Auth      *auth.Auth
	Templates Templates
}

func New(db *sql.DB, a *auth.Auth, templates Templates) *Handlers {
	return &Handlers{DB: db, Auth: a, Templates: templates}
}

func (h *Handlers) render(w http.ResponseWriter, page string, data any) {
	tmpl, ok := h.Templates[page]
	if !ok {
		log.Printf("render: unknown page %s", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Render into a buffer first so a template error becomes a clean 500 rather
	// than a half-written page whose 200 status has already been committed.
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		log.Printf("render %s: %v", page, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := buf.WriteTo(w); err != nil {
		log.Printf("write %s: %v", page, err)
	}
}
