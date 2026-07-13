package main

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/thinkscotty/binbash/internal/ai"
	"github.com/thinkscotty/binbash/internal/auth"
	"github.com/thinkscotty/binbash/internal/config"
	"github.com/thinkscotty/binbash/internal/db"
	"github.com/thinkscotty/binbash/internal/handlers"
	"github.com/thinkscotty/binbash/internal/update"
)

//go:embed web/templates/*.html
var templatesFS embed.FS

//go:embed web/static
var staticFS embed.FS

// version is the release tag baked in at build time via
// -ldflags "-X main.version=v1.2.3" (see .github/workflows/release.yml).
// Untagged builds run as "dev", which the updater refuses to replace.
var version = "dev"

// pages lists every content template that gets rendered inside the shared layout.
var pages = []string{"login.html", "search.html", "bins.html", "bin_edit.html", "bin_delete.html", "items.html", "item_edit.html", "item_delete.html", "settings.html", "backup.html"}

// noDirListing wraps a handler (typically an http.FileServer) so that
// directory-style requests 404 instead of rendering an auto-generated file
// listing, which http.FileServer has no built-in way to suppress.
func noDirListing(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

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
	// Handled before config.Load so it works anywhere, with no config file.
	// The self-updater also runs this against a freshly downloaded binary to
	// confirm it executes and reports the expected version before swapping.
	if len(os.Args) > 1 && (os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Println("binbash " + version)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Needs the config (for the database path) but must not start a server:
	// it writes to the database directly, and then exits.
	if len(os.Args) > 1 && (os.Args[1] == "-reset-password" || os.Args[1] == "--reset-password") {
		if err := resetPassword(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "\n%v\n", err)
			os.Exit(1)
		}
		return
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}

	if cfg.AutoBackupDir != "" {
		if err := os.MkdirAll(cfg.AutoBackupDir, db.DirMode); err != nil {
			log.Fatalf("auto-backup directory: %v", err)
		}
		// Backups are a full copy of the inventory, and pre-update snapshots
		// kept here are full copies of the database. Same secrets, same rules.
		if err := db.RestrictPermissions(cfg.AutoBackupDir); err != nil {
			log.Fatalf("auto-backup directory: %v", err)
		}
	}

	templates, err := loadTemplates()
	if err != nil {
		log.Fatalf("templates: %v", err)
	}

	trusted, err := auth.ParseTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		log.Fatalf("trusted proxies: %v", err)
	}

	a, err := auth.New(database, cfg.Password, trusted)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}

	var aiClient *ai.Client
	if cfg.AIEnabled() {
		aiClient = ai.New(cfg.AIBaseURL, cfg.AIAPIKey, cfg.AIModel)
	}
	h := handlers.New(database, a, templates, cfg.AutoBackupDir, aiClient, cfg.AITagCount, cfg.AITagBreadth)

	// The executable path is resolved now, at startup, because a self-update
	// renames the running binary's inode aside — after that, /proc/self/exe
	// (and so os.Executable) can report the .old path on Linux.
	exePath, exeErr := os.Executable()
	if exeErr == nil {
		exePath, exeErr = filepath.EvalSymlinks(exePath)
	}
	if exeErr != nil {
		log.Printf("warning: could not resolve the executable path; in-app update restart unavailable: %v", exeErr)
		exePath = ""
	}

	// restart is signaled by the settings handlers after a successful
	// self-update; the buffered non-blocking send means a duplicate signal is
	// dropped rather than deadlocking a handler.
	restart := make(chan struct{}, 1)
	h.Updater = update.New(version)
	h.RequestRestart = func() {
		select {
		case restart <- struct{}{}:
		default:
		}
	}

	static, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		log.Fatalf("static assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.LoginSubmit)
	mux.HandleFunc("POST /logout", h.LogoutSubmit)
	mux.HandleFunc("GET /settings", h.SettingsPage)
	mux.HandleFunc("POST /settings/password", h.ChangePassword)
	mux.HandleFunc("POST /settings/check-update", h.CheckUpdate)
	mux.HandleFunc("POST /settings/update", h.ApplyUpdate)
	// Account became Settings; old bookmarks and any account page still open
	// in a tab from before an upgrade keep working.
	mux.HandleFunc("GET /account", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/settings", http.StatusMovedPermanently)
	})
	mux.HandleFunc("POST /account/password", h.ChangePassword)
	mux.HandleFunc("GET /backup", h.BackupPage)
	mux.HandleFunc("POST /backup/export", h.ExportBackup)
	mux.HandleFunc("POST /backup/import", h.ImportBackup)
	mux.HandleFunc("GET /{$}", h.Search)
	mux.HandleFunc("GET /bins", h.ListBins)
	mux.HandleFunc("POST /bins", h.CreateBin)
	mux.HandleFunc("GET /bins/{id}/edit", h.EditBinForm)
	mux.HandleFunc("GET /bins/{id}/delete", h.DeleteBinConfirm)
	mux.HandleFunc("POST /bins/{id}/delete", h.DeleteBin)
	mux.HandleFunc("POST /bins/{id}", h.UpdateBin)
	mux.HandleFunc("GET /items", h.ListItems)
	mux.HandleFunc("POST /items", h.CreateItem)
	mux.HandleFunc("GET /items/{id}/edit", h.EditItemForm)
	mux.HandleFunc("GET /items/{id}/delete", h.DeleteItemConfirm)
	mux.HandleFunc("POST /items/{id}/delete", h.DeleteItem)
	mux.HandleFunc("POST /items/{id}", h.UpdateItem)
	mux.HandleFunc("POST /items/tag", h.TagItems)
	mux.Handle("GET /static/", noDirListing(http.StripPrefix("/static/", http.FileServer(http.FS(static)))))

	addr := net.JoinHostPort(cfg.BindAddress, cfg.Port)
	srv := &http.Server{
		Addr: addr,
		// Protect layers security headers, CSRF rejection, and the login gate
		// around every route, in that order.
		Handler: a.Protect(mux),
		// ReadHeaderTimeout and IdleTimeout guard against slow-client connection
		// exhaustion (Slowloris-style). WriteTimeout is deliberately left unset:
		// it would also bound total handler runtime, and AI batch-tagging can
		// legitimately take several minutes against a slow provider.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()

	// SIGTERM (systemctl stop) and SIGINT (Ctrl-C) get the same graceful
	// path as a post-update restart: drain in-flight requests, then close
	// the database cleanly so the WAL is checkpointed.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	log.Printf("binbash %s listening on %s", version, addr)

	restarting := false
	select {
	case err := <-serveErr:
		log.Fatal(err)
	case s := <-sig:
		log.Printf("received %v, shutting down", s)
	case <-restart:
		log.Printf("update installed, restarting")
		restarting = true
	}

	// Shutdown waits for active requests — including the update handler's
	// success response — before returning, bounded so a stuck connection
	// can't wedge a stop forever.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	if err := database.Close(); err != nil {
		log.Printf("database close: %v", err)
	}

	if restarting {
		if exePath == "" {
			log.Fatal("restart: executable path unknown — start binbash manually to finish the update")
		}
		reexec(exePath) // only returns on failure, via log.Fatal
	}
}
