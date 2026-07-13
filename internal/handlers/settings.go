package handlers

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/thinkscotty/binbash/internal/auth"
)

// snapshotKeep is how many pre-update database snapshots survive pruning.
const snapshotKeep = 3

func (h *Handlers) SettingsPage(w http.ResponseWriter, r *http.Request) {
	h.renderSettings(w, nil)
}

// renderSettings renders settings.html with the running version always
// available to the template, whatever else the handler passes.
func (h *Handlers) renderSettings(w http.ResponseWriter, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["Version"] = h.Updater.Version()
	h.render(w, "settings.html", data)
}

func (h *Handlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	// Shares the login endpoint's throttle state (keyed by IP): otherwise
	// someone locked out of /login for guessing the password could just
	// switch to this endpoint and keep guessing unthrottled.
	ip := h.Auth.ClientIP(r)
	if wait, blocked := h.Auth.Throttled(ip); blocked {
		logAuthFailure(ip, "attempt while locked out")
		h.renderSettings(w, map[string]any{"Error": lockoutMessage(wait)})
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
		logAuthFailure(ip, "incorrect current password at /settings/password")
		if lockedOut := h.Auth.RecordFailure(ip); lockedOut {
			log.Printf("binbash: locked out %s for 15 minutes after %d failed attempts", ip, auth.MaxAttempts)
		}
	}

	if errMsg := validatePasswordChange(currentOK, newPassword, confirm); errMsg != "" {
		h.renderSettings(w, map[string]any{"Error": errMsg})
		return
	}

	if err := h.Auth.Rotate(newPassword); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Rotate also rotated the session-signing key, so every session -- this
	// one included -- is now invalid. Re-issuing the cookie here is what keeps
	// the person who just changed the password signed in while signing out
	// everyone else, which is the entire point: if a session cookie leaks,
	// changing the password is the remedy a user reaches for, and it has to
	// actually work. Must happen before renderSettings writes the body.
	h.Auth.Login(w, r)

	h.Auth.RecordSuccess(ip)
	log.Printf("binbash: password changed from %s; all other sessions signed out", ip)
	h.renderSettings(w, map[string]any{
		"Success": "Password updated. Any other devices signed in to binbash will need to sign in again.",
	})
}

// updateInfo is what the settings template needs to offer an update.
type updateInfo struct {
	Tag string
	URL string
}

func (h *Handlers) CheckUpdate(w http.ResponseWriter, r *http.Request) {
	rel, err := h.Updater.Check(r.Context())
	if err != nil {
		h.renderSettings(w, map[string]any{"UpdateError": "Update check failed: " + err.Error() + "."})
		return
	}

	if h.Updater.IsNewer(rel.Tag) {
		h.renderSettings(w, map[string]any{"UpdateAvailable": updateInfo{Tag: rel.Tag, URL: rel.URL}})
		return
	}
	if h.Updater.DevBuild() {
		h.renderSettings(w, map[string]any{
			"UpdateStatus": fmt.Sprintf("This is a development build; the latest release is %s. Development builds don't self-update.", rel.Tag),
		})
		return
	}
	h.renderSettings(w, map[string]any{
		"UpdateStatus": fmt.Sprintf("You're up to date — %s is the latest release.", rel.Tag),
	})
}

func (h *Handlers) ApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	tag := r.FormValue("tag")

	// Re-check rather than trusting the posted tag: it closes the gap
	// between the user's check and their click, and means the download URLs
	// always come from GitHub, never from the form.
	rel, err := h.Updater.Check(r.Context())
	if err != nil {
		h.renderSettings(w, map[string]any{"UpdateError": "Update check failed: " + err.Error() + "."})
		return
	}
	if rel.Tag != tag {
		h.renderSettings(w, map[string]any{
			"UpdateError": fmt.Sprintf("The latest release is now %s, not the %s you checked for — please check again.", rel.Tag, tag),
		})
		return
	}
	if !h.Updater.IsNewer(rel.Tag) {
		msg := fmt.Sprintf("You're up to date — %s is the latest release.", rel.Tag)
		if h.Updater.DevBuild() {
			msg = "This is a development build; it doesn't self-update."
		}
		h.renderSettings(w, map[string]any{"UpdateStatus": msg})
		return
	}

	// The snapshot is the exact rollback if the new version misbehaves —
	// migrations only move forward, so without it there's no way back to
	// this schema. No snapshot, no update.
	snapshot, err := h.snapshotDB()
	if err != nil {
		h.renderSettings(w, map[string]any{
			"UpdateError": "Could not snapshot the database, so the update was not started: " + err.Error() + ".",
		})
		return
	}

	if err := h.Updater.Apply(r.Context(), rel); err != nil {
		h.renderSettings(w, map[string]any{"UpdateError": "Update failed: " + err.Error() + "."})
		return
	}

	// The new binary is on disk but this process still runs the old code.
	// Render the goodbye page first — Shutdown waits for this response to
	// finish — then ask main to restart. The meta-refresh reloads the page
	// once the new version is listening.
	h.renderSettings(w, map[string]any{
		"Updated":      rel.Tag,
		"SnapshotPath": snapshot,
		"MetaRefresh":  "8;url=/settings",
	})
	h.RequestRestart()
}

// snapshotDB writes a point-in-time copy of the live database (VACUUM INTO
// is WAL-safe and produces a compact single file) and returns its path.
// Snapshots go to the auto-backup dir when one is configured, else next to
// the database file itself.
func (h *Handlers) snapshotDB() (string, error) {
	dir := h.AutoBackupDir
	if dir == "" {
		var dbPath string
		if err := h.DB.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&dbPath); err != nil {
			return "", fmt.Errorf("locate database file: %w", err)
		}
		dir = filepath.Dir(dbPath)
	}

	name := fmt.Sprintf("binbash-pre-update-%s-%s.db", h.Updater.Version(), time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, name)
	// VACUUM INTO doesn't take bound parameters, so the path is embedded as
	// a SQL string literal with single quotes doubled. The path is
	// server-derived (config + generated name), not user input.
	if _, err := h.DB.Exec("VACUUM INTO '" + strings.ReplaceAll(path, "'", "''") + "'"); err != nil {
		return "", err
	}

	pruneSnapshots(dir)
	return path, nil
}

// pruneSnapshots deletes all but the newest snapshotKeep pre-update
// snapshots in dir, by modification time — names embed versions, which
// don't sort lexicographically (v0.1.10 < v0.1.9). Best-effort: pruning
// failures never block an update.
func pruneSnapshots(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, "binbash-pre-update-*.db"))
	if err != nil || len(matches) <= snapshotKeep {
		return
	}
	sort.Slice(matches, func(i, j int) bool {
		ii, iErr := os.Stat(matches[i])
		jj, jErr := os.Stat(matches[j])
		if iErr != nil || jErr != nil {
			return iErr == nil
		}
		return ii.ModTime().After(jj.ModTime())
	})
	for _, old := range matches[snapshotKeep:] {
		os.Remove(old)
	}
}
