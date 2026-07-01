package main

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"

	"github.com/thinkscotty/binbash/internal/auth"
	"github.com/thinkscotty/binbash/internal/config"
	"github.com/thinkscotty/binbash/internal/db"
	"github.com/thinkscotty/binbash/internal/handlers"
)

//go:embed web/templates/*.html
var templatesFS embed.FS

//go:embed web/static
var staticFS embed.FS

// pages lists every content template that gets rendered inside the shared layout.
var pages = []string{"login.html", "home.html"}

func loadTemplates() (handlers.Templates, error) {
	templates := make(handlers.Templates, len(pages))
	for _, page := range pages {
		tmpl, err := template.ParseFS(templatesFS, "web/templates/layout.html", "web/templates/"+page)
		if err != nil {
			return nil, err
		}
		templates[page] = tmpl
	}
	return templates, nil
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer database.Close()

	templates, err := loadTemplates()
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

	a := auth.New(cfg.Password)
	h := handlers.New(database, a, templates)

	static, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		log.Fatalf("static assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.LoginSubmit)
	mux.HandleFunc("POST /logout", h.LogoutSubmit)
	mux.HandleFunc("GET /{$}", h.Home)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	addr := ":" + cfg.Port
	log.Printf("binbash listening on %s", addr)
	if err := http.ListenAndServe(addr, a.Middleware(mux)); err != nil {
		log.Fatal(err)
	}
}
