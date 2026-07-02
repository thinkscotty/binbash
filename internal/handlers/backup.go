package handlers

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	autoBackupItemThreshold = 50
	autoBackupKeepFiles     = 5
	maxImportFileSize       = 10 << 20 // 10MB
)

var backupCSVHeader = []string{"item_name", "item_description", "keywords", "bin_name", "bin_category", "bin_description"}

func (h *Handlers) BackupPage(w http.ResponseWriter, r *http.Request) {
	h.renderBackup(w, nil)
}

func (h *Handlers) ExportBackup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="binbash-backup-%s.csv"`, time.Now().Format("20060102-150405")))
	if _, err := h.writeCSVTo(w); err != nil {
		log.Printf("export backup: %v", err)
		return
	}
	if err := h.markBackupDone(); err != nil {
		log.Printf("export backup: mark done: %v", err)
	}
}

func (h *Handlers) ImportBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportFileSize)
	if err := r.ParseMultipartForm(maxImportFileSize); err != nil {
		h.renderBackup(w, map[string]any{"Error": "That file is too large or couldn't be read."})
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		h.renderBackup(w, map[string]any{"Error": "Choose a CSV file to import."})
		return
	}
	defer file.Close()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		h.renderBackup(w, map[string]any{"Error": "That file couldn't be parsed as CSV."})
		return
	}
	if len(records) == 0 || !equalHeader(records[0], backupCSVHeader) {
		h.renderBackup(w, map[string]any{
			"Error": fmt.Sprintf("That doesn't look like a binbash export. Expected columns: %s", strings.Join(backupCSVHeader, ", ")),
		})
		return
	}

	type importRow struct {
		itemName, itemDescription, keywords, binName, binCategory, binDescription string
	}

	rows := make([]importRow, 0, len(records)-1)
	for i, rec := range records[1:] {
		row := importRow{
			itemName:        strings.TrimSpace(rec[0]),
			itemDescription: strings.TrimSpace(rec[1]),
			keywords:        strings.TrimSpace(rec[2]),
			binName:         strings.TrimSpace(rec[3]),
			binCategory:     strings.TrimSpace(rec[4]),
			binDescription:  strings.TrimSpace(rec[5]),
		}
		if formErr := validateImportRow(row.itemName, row.itemDescription, row.keywords, row.binName, row.binCategory, row.binDescription); formErr != "" {
			h.renderBackup(w, map[string]any{"Error": fmt.Sprintf("Row %d: %s", i+2, formErr)})
			return
		}
		rows = append(rows, row)
	}

	replace := r.FormValue("replace") == "on"

	tx, err := h.DB.Begin()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if replace {
		if _, err := tx.Exec(`DELETE FROM items`); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if _, err := tx.Exec(`DELETE FROM bins`); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// Without AUTOINCREMENT, item ids restart at 1 once the table is empty,
		// so the existing "id > watermark" backup-due check would otherwise stay
		// permanently blind to a full replace until the id counter climbs back
		// past whatever it reached before. Reset it so newly-loaded items count
		// correctly toward the next backup reminder/auto-backup.
		if _, err := tx.Exec(`UPDATE backup_state SET last_backup_max_item_id = 0 WHERE id = 1`); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	binIDs := map[string]int64{}
	binRows, err := tx.Query(`SELECT id, name FROM bins`)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	for binRows.Next() {
		var id int64
		var name string
		if err := binRows.Scan(&id, &name); err != nil {
			binRows.Close()
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		binIDs[name] = id
	}
	binRows.Close()
	if err := binRows.Err(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	newBins := 0
	for _, row := range rows {
		binID, ok := binIDs[row.binName]
		if !ok {
			res, err := tx.Exec(
				`INSERT INTO bins (name, description, category) VALUES (?, ?, ?)`,
				row.binName, row.binDescription, row.binCategory,
			)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			binID, err = res.LastInsertId()
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			binIDs[row.binName] = binID
			newBins++
		}
		if _, err := tx.Exec(
			`INSERT INTO items (bin_id, name, description, keywords) VALUES (?, ?, ?, ?)`,
			binID, row.itemName, row.itemDescription, row.keywords,
		); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Deliberately not calling markBackupDone here: importing is a restore, not
	// a backup, so it shouldn't reset the "when did I last back up" watermark.
	h.renderBackup(w, map[string]any{
		"Success": fmt.Sprintf("Imported %d item(s) (%d new bin(s)).", len(rows), newBins),
	})
}

// renderBackup refreshes the auto-backup/reminder status and renders backup.html.
func (h *Handlers) renderBackup(w http.ResponseWriter, data map[string]any) {
	newItems, lastBackupAt := h.checkAndRunAutoBackup()

	if data == nil {
		data = map[string]any{}
	}
	data["NewItems"] = newItems
	data["ReminderDue"] = newItems >= autoBackupItemThreshold
	if lastBackupAt.Valid {
		// SQLite's CURRENT_TIMESTAMP is UTC; convert to local time so the
		// displayed timestamp matches the wall clock the user actually has.
		data["LastBackupAt"] = lastBackupAt.Time.Local().Format("Jan 2, 2006 3:04 PM")
	}
	h.render(w, "backup.html", data)
}

// writeCSVTo streams the full inventory as a denormalized CSV (one row per
// item, with its bin's fields repeated) to w. It's shared by the manual
// export download and the automatic on-device backup writer.
func (h *Handlers) writeCSVTo(w io.Writer) (int, error) {
	rows, err := h.DB.Query(`
		SELECT items.name, COALESCE(items.description, ''), COALESCE(items.keywords, ''),
		       bins.name, COALESCE(bins.category, ''), COALESCE(bins.description, '')
		FROM items JOIN bins ON bins.id = items.bin_id
		ORDER BY bins.name, items.name`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	cw := csv.NewWriter(w)
	if err := cw.Write(backupCSVHeader); err != nil {
		return 0, err
	}

	var itemName, itemDescription, keywords, binName, binCategory, binDescription string
	count := 0
	for rows.Next() {
		if err := rows.Scan(&itemName, &itemDescription, &keywords, &binName, &binCategory, &binDescription); err != nil {
			return count, err
		}
		if err := cw.Write([]string{itemName, itemDescription, keywords, binName, binCategory, binDescription}); err != nil {
			return count, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, err
	}

	cw.Flush()
	return count, cw.Error()
}

// checkAndRunAutoBackup reports how many items have been added since the last
// backup watermark. If AutoBackupDir is configured and that count has crossed
// autoBackupItemThreshold, it writes a timestamped CSV there, prunes old
// files beyond autoBackupKeepFiles, and advances the watermark - which also
// clears the manual reminder, since an automatic backup is still a backup.
// Failures are logged rather than surfaced: this is a best-effort convenience
// feature that must never break a page load.
func (h *Handlers) checkAndRunAutoBackup() (newItems int, lastBackupAt sql.NullTime) {
	var maxItemID int64
	if err := h.DB.QueryRow(
		`SELECT last_backup_at, last_backup_max_item_id FROM backup_state WHERE id = 1`,
	).Scan(&lastBackupAt, &maxItemID); err != nil {
		log.Printf("backup status: %v", err)
		return 0, lastBackupAt
	}

	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM items WHERE id > ?`, maxItemID).Scan(&newItems); err != nil {
		log.Printf("backup status: %v", err)
		return 0, lastBackupAt
	}

	if h.AutoBackupDir == "" || newItems < autoBackupItemThreshold {
		return newItems, lastBackupAt
	}

	filename := filepath.Join(h.AutoBackupDir, fmt.Sprintf("binbash-%s.csv", time.Now().Format("20060102-150405")))
	f, err := os.Create(filename)
	if err != nil {
		log.Printf("auto-backup: create %s: %v", filename, err)
		return newItems, lastBackupAt
	}
	_, writeErr := h.writeCSVTo(f)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		log.Printf("auto-backup: write %s: write=%v close=%v", filename, writeErr, closeErr)
		os.Remove(filename)
		return newItems, lastBackupAt
	}

	if err := h.markBackupDone(); err != nil {
		log.Printf("auto-backup: mark done: %v", err)
		return newItems, lastBackupAt
	}
	h.pruneOldBackups()
	log.Printf("auto-backup: wrote %s (%d items)", filename, newItems)

	return 0, sql.NullTime{Time: time.Now(), Valid: true}
}

func (h *Handlers) markBackupDone() error {
	_, err := h.DB.Exec(`
		UPDATE backup_state
		SET last_backup_at = CURRENT_TIMESTAMP,
		    last_backup_max_item_id = (SELECT COALESCE(MAX(id), 0) FROM items)
		WHERE id = 1`)
	return err
}

// pruneOldBackups deletes all but the newest autoBackupKeepFiles backups in
// AutoBackupDir. Filenames sort chronologically because they share the
// "binbash-YYYYMMDD-HHMMSS.csv" format.
func (h *Handlers) pruneOldBackups() {
	entries, err := os.ReadDir(h.AutoBackupDir)
	if err != nil {
		log.Printf("auto-backup: prune: %v", err)
		return
	}

	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "binbash-") || !strings.HasSuffix(name, ".csv") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	if len(names) <= autoBackupKeepFiles {
		return
	}
	for _, old := range names[:len(names)-autoBackupKeepFiles] {
		if err := os.Remove(filepath.Join(h.AutoBackupDir, old)); err != nil {
			log.Printf("auto-backup: prune %s: %v", old, err)
		}
	}
}

func equalHeader(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if strings.TrimSpace(got[i]) != want[i] {
			return false
		}
	}
	return true
}
