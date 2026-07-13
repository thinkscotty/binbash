// Package handlers implements binbash's HTTP handlers.
package handlers

import (
	"bytes"
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/thinkscotty/binbash/internal/ai"
	"github.com/thinkscotty/binbash/internal/auth"
	"github.com/thinkscotty/binbash/internal/update"
)

// Templates maps a page name (e.g. "login.html") to a template set combining
// that page's content with the shared layout. Each entry is self-contained
// so pages can safely reuse the same block names (e.g. "content").
type Templates map[string]*template.Template

type Handlers struct {
	DB            *sql.DB
	Auth          *auth.Auth
	Templates     Templates
	AutoBackupDir string
	AI            *ai.Client // nil when AI tagging is disabled
	AITagCount    int
	AITagBreadth  string

	// Updater and RequestRestart back the settings page's self-update flow;
	// main assigns them after New. RequestRestart asks main to gracefully
	// shut down and re-exec once the update response has gone out.
	Updater        *update.Updater
	RequestRestart func()

	// backupMu serializes checkAndRunAutoBackup's read-decide-write sequence
	// against itself and against ExportBackup's markBackupDone call. Those are
	// each several separate DB round trips rather than a single transaction,
	// so without this lock, concurrent requests (e.g. two browser tabs loading
	// the search page at once) can all read the same stale backup watermark
	// and race to write the same timestamped auto-backup file.
	backupMu sync.Mutex

	// aiTagMu serializes TagItems: it selects a batch of untagged items and
	// then commits them one at a time over the course of the request, so
	// without this lock, two concurrent invocations (a double-click, two open
	// tabs) could both select the same "untagged" items before either commits
	// and pay for duplicate AI calls on the same items.
	aiTagMu sync.Mutex
}

func New(db *sql.DB, a *auth.Auth, templates Templates, autoBackupDir string, aiClient *ai.Client, aiTagCount int, aiTagBreadth string) *Handlers {
	return &Handlers{
		DB: db, Auth: a, Templates: templates, AutoBackupDir: autoBackupDir,
		AI: aiClient, AITagCount: aiTagCount, AITagBreadth: aiTagBreadth,
	}
}

func (h *Handlers) render(w http.ResponseWriter, page string, data any) {
	tmpl, ok := h.Templates[page]
	if !ok {
		log.Printf("render: unknown page %s", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Every call site passes either nil or a map[string]any, so this always
	// succeeds in practice; it lets layout.html know which nav link is active
	// without every handler having to set it individually.
	m, ok := data.(map[string]any)
	if !ok {
		m = map[string]any{}
	}
	m["CurrentPage"] = strings.TrimSuffix(page, ".html")
	data = m

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
