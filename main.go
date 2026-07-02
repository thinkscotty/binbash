package main

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/thinkscotty/binbash/internal/ai"
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
var pages = []string{"login.html", "search.html", "bins.html", "bin_edit.html", "items.html", "item_edit.html", "account.html", "backup.html"}

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

	if cfg.AutoBackupDir != "" {
		if err := os.MkdirAll(cfg.AutoBackupDir, 0o755); err != nil {
			log.Fatalf("auto-backup directory: %v", err)
		}
	}

	templates, err := loadTemplates()
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

	a, err := auth.New(database, cfg.Password)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}

	var aiClient *ai.Client
	if cfg.AIEnabled() {
		aiClient = ai.New(cfg.AIBaseURL, cfg.AIAPIKey, cfg.AIModel)
	}
	h := handlers.New(database, a, templates, cfg.AutoBackupDir, aiClient, cfg.AITagCount, cfg.AITagBreadth)

	static, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		log.Fatalf("static assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.LoginSubmit)
	mux.HandleFunc("POST /logout", h.LogoutSubmit)
	mux.HandleFunc("GET /account", h.AccountPage)
	mux.HandleFunc("POST /account/password", h.ChangePassword)
	mux.HandleFunc("GET /backup", h.BackupPage)
	mux.HandleFunc("POST /backup/export", h.ExportBackup)
	mux.HandleFunc("POST /backup/import", h.ImportBackup)
	mux.HandleFunc("GET /{$}", h.Search)
	mux.HandleFunc("GET /bins", h.ListBins)
	mux.HandleFunc("POST /bins", h.CreateBin)
	mux.HandleFunc("GET /bins/{id}/edit", h.EditBinForm)
	mux.HandleFunc("POST /bins/{id}", h.UpdateBin)
	mux.HandleFunc("GET /items", h.ListItems)
	mux.HandleFunc("POST /items", h.CreateItem)
	mux.HandleFunc("GET /items/{id}/edit", h.EditItemForm)
	mux.HandleFunc("POST /items/{id}", h.UpdateItem)
	mux.HandleFunc("POST /items/tag", h.TagItems)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	addr := ":" + cfg.Port
	log.Printf("binbash listening on %s", addr)
	if err := http.ListenAndServe(addr, a.Middleware(mux)); err != nil {
		log.Fatal(err)
	}
}
