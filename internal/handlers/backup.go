package handlers

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	autoBackupItemThreshold = 50
	autoBackupKeepFiles     = 5
	maxImportFileSize       = 10 << 20 // 10MB
)

// backupFilenamePattern matches exactly the "binbash-YYYYMMDD-HHMMSS.csv"
// format auto-backups are written with. pruneOldBackups only ever deletes
// files matching this precisely, so if AutoBackupDir is ever pointed at a
// directory shared with something else, an unrelated file that merely starts
// with "binbash-" and ends in ".csv" won't get swept up in retention.
var backupFilenamePattern = regexp.MustCompile(`^binbash-\d{8}-\d{6}\.csv$`)

// csvFormulaPrefixes are leading characters that spreadsheet programs (Excel,
// Sheets, LibreOffice) may interpret as the start of a formula when a CSV is
// opened in them ("CSV/formula injection"). Since the whole point of the
// export is to be opened in a spreadsheet, fields starting with one of these
// are defused with a leading single quote, which spreadsheet programs treat
// as "force this cell to plain text".
const csvFormulaPrefixes = "=+-@"

// sanitizeCSVField defuses a field for safe opening in a spreadsheet program.
func sanitizeCSVField(s string) string {
	if s != "" && strings.ContainsRune(csvFormulaPrefixes, rune(s[0])) {
		return "'" + s
	}
	return s
}

// desanitizeCSVField reverses sanitizeCSVField, so re-importing binbash's own
// export round-trips exactly instead of accumulating a leading quote.
func desanitizeCSVField(s string) string {
	if len(s) >= 2 && s[0] == '\'' && strings.ContainsRune(csvFormulaPrefixes, rune(s[1])) {
		return s[1:]
	}
	return s
}

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
	h.backupMu.Lock()
	err := h.markBackupDone()
	h.backupMu.Unlock()
	if err != nil {
		log.Printf("export backup: mark done: %v", err)
	}
}

// importRow is one item plus the bin it belongs in, parsed from an uploaded
// CSV (binbash's own export or a foreign format such as Homebox) and ready to
// be loaded by commitImport.
type importRow struct {
	itemName, itemDescription, keywords  string
	binName, binCategory, binDescription string
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

	// Spreadsheet programs (notably Excel on Windows) commonly prepend a UTF-8
	// BOM when saving a CSV. Strip it so a user's own re-saved export still
	// matches the expected header instead of being rejected as unrecognized.
	buffered := bufio.NewReader(file)
	if bom, err := buffered.Peek(3); err == nil && bytes.Equal(bom, []byte{0xEF, 0xBB, 0xBF}) {
		buffered.Discard(3)
	}

	records, err := csv.NewReader(buffered).ReadAll()
	if err != nil {
		h.renderBackup(w, map[string]any{"Error": "That file couldn't be parsed as CSV."})
		return
	}
	if len(records) == 0 {
		h.renderBackup(w, map[string]any{"Error": "That file is empty."})
		return
	}

	// Pick a parser by sniffing the header row. binbash's own export is strict
	// (any bad row aborts); the Homebox path is lenient and reports how many
	// rows it skipped, appended to the success message as skipNote.
	var (
		rows     []importRow
		skipNote string
		errMsg   string
	)
	switch {
	case equalHeader(records[0], backupCSVHeader):
		rows, errMsg = parseBinbashCSV(records)
	case isHomeboxHeader(records[0]):
		var noBin, archived, noName int
		rows, noBin, archived, noName = parseHomeboxCSV(records)
		skipNote = homeboxSkipNote(noBin, archived, noName)
	default:
		errMsg = fmt.Sprintf("That doesn't look like a binbash or Homebox export. A binbash export starts with the columns: %s", strings.Join(backupCSVHeader, ", "))
	}
	if errMsg != "" {
		h.renderBackup(w, map[string]any{"Error": errMsg})
		return
	}

	replace := r.FormValue("replace") == "on"

	// Guard against a destructive no-op: "Replace" wipes the whole inventory
	// before loading the file, but the Homebox path can legitimately reduce a
	// full-looking export to zero importable rows (e.g. no location names a
	// bin). Wiping and then loading nothing would silently destroy the user's
	// data, so refuse rather than replace with an empty set.
	if replace && len(rows) == 0 {
		h.renderBackup(w, map[string]any{"Error": "That file had no importable items, so nothing was replaced — your existing inventory is unchanged."})
		return
	}

	itemCount, newBins, err := h.commitImport(rows, replace)
	if err != nil {
		log.Printf("import: commit: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Deliberately not calling markBackupDone here: importing is a restore, not
	// a backup, so it shouldn't reset the "when did I last back up" watermark.
	summary := fmt.Sprintf("Imported %d item(s) (%d new bin(s)).", itemCount, newBins)
	if skipNote != "" {
		summary += " " + skipNote
	}
	h.renderBackup(w, map[string]any{"Success": summary})
}

// parseBinbashCSV converts the rows of a binbash export (row 0 already
// confirmed to carry backupCSVHeader) into importRows. It returns a
// human-friendly message for the first invalid row, preserving the strict
// all-or-nothing validation the export/import round-trip relies on: binbash's
// own data is expected to already be within limits, so anything out of bounds
// signals a corrupt or hand-edited file worth rejecting outright.
func parseBinbashCSV(records [][]string) (rows []importRow, errMsg string) {
	rows = make([]importRow, 0, len(records)-1)
	for i, rec := range records[1:] {
		row := importRow{
			itemName:        desanitizeCSVField(strings.TrimSpace(rec[0])),
			itemDescription: desanitizeCSVField(strings.TrimSpace(rec[1])),
			keywords:        desanitizeCSVField(strings.TrimSpace(rec[2])),
			binName:         desanitizeCSVField(strings.TrimSpace(rec[3])),
			binCategory:     desanitizeCSVField(strings.TrimSpace(rec[4])),
			binDescription:  desanitizeCSVField(strings.TrimSpace(rec[5])),
		}
		if formErr := validateImportRow(row.itemName, row.itemDescription, row.keywords, row.binName, row.binCategory, row.binDescription); formErr != "" {
			return nil, fmt.Sprintf("Row %d: %s", i+2, formErr)
		}
		rows = append(rows, row)
	}
	return rows, ""
}

// commitImport loads rows into the database in a single all-or-nothing
// transaction. Bins are matched to existing rows by exact name and created on
// demand; an existing bin's category/description is left untouched, so
// importing a name that already exists merges into it rather than duplicating.
// When replace is true, all current bins and items are deleted first and the
// backup watermark reset. It returns the number of items inserted and the
// number of bins newly created.
func (h *Handlers) commitImport(rows []importRow, replace bool) (itemCount, newBins int, err error) {
	tx, err := h.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	if replace {
		if _, err := tx.Exec(`DELETE FROM items`); err != nil {
			return 0, 0, err
		}
		if _, err := tx.Exec(`DELETE FROM bins`); err != nil {
			return 0, 0, err
		}
		// Without AUTOINCREMENT, item ids restart at 1 once the table is empty,
		// so the existing "id > watermark" backup-due check would otherwise stay
		// permanently blind to a full replace until the id counter climbs back
		// past whatever it reached before. Reset it so newly-loaded items count
		// correctly toward the next backup reminder/auto-backup.
		if _, err := tx.Exec(`UPDATE backup_state SET last_backup_max_item_id = 0 WHERE id = 1`); err != nil {
			return 0, 0, err
		}
	}

	binIDs := map[string]int64{}
	binRows, err := tx.Query(`SELECT id, name FROM bins`)
	if err != nil {
		return 0, 0, err
	}
	for binRows.Next() {
		var id int64
		var name string
		if err := binRows.Scan(&id, &name); err != nil {
			binRows.Close()
			return 0, 0, err
		}
		binIDs[name] = id
	}
	binRows.Close()
	if err := binRows.Err(); err != nil {
		return 0, 0, err
	}

	for _, row := range rows {
		binID, ok := binIDs[row.binName]
		if !ok {
			res, err := tx.Exec(
				`INSERT INTO bins (name, description, category) VALUES (?, ?, ?)`,
				row.binName, row.binDescription, row.binCategory,
			)
			if err != nil {
				return 0, 0, err
			}
			binID, err = res.LastInsertId()
			if err != nil {
				return 0, 0, err
			}
			binIDs[row.binName] = binID
			newBins++
		}
		if _, err := tx.Exec(
			`INSERT INTO items (bin_id, name, description, keywords) VALUES (?, ?, ?, ?)`,
			binID, row.itemName, row.itemDescription, row.keywords,
		); err != nil {
			return 0, 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return len(rows), newBins, nil
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
		if err := cw.Write([]string{
			sanitizeCSVField(itemName), sanitizeCSVField(itemDescription), sanitizeCSVField(keywords),
			sanitizeCSVField(binName), sanitizeCSVField(binCategory), sanitizeCSVField(binDescription),
		}); err != nil {
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
	h.backupMu.Lock()
	defer h.backupMu.Unlock()

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

	filename := fmt.Sprintf("binbash-%s.csv", time.Now().Format("20060102-150405"))
	finalPath := filepath.Join(h.AutoBackupDir, filename)
	// Write to a hidden temp file first and rename into place only once the
	// write fully succeeds, so a crash or power loss mid-write (this app's
	// homelab/VPS deployment target makes that a real possibility) can never
	// leave a truncated, corrupt file sitting under a name that looks like a
	// valid backup -- which would otherwise count toward retention and could
	// end up evicting a good backup in its place.
	tmpPath := filepath.Join(h.AutoBackupDir, "."+filename+".tmp")
	f, err := os.Create(tmpPath)
	if err != nil {
		log.Printf("auto-backup: create %s: %v", tmpPath, err)
		return newItems, lastBackupAt
	}
	_, writeErr := h.writeCSVTo(f)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		log.Printf("auto-backup: write %s: write=%v close=%v", tmpPath, writeErr, closeErr)
		os.Remove(tmpPath)
		return newItems, lastBackupAt
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		log.Printf("auto-backup: rename %s -> %s: %v", tmpPath, finalPath, err)
		os.Remove(tmpPath)
		return newItems, lastBackupAt
	}

	if err := h.markBackupDone(); err != nil {
		log.Printf("auto-backup: mark done: %v", err)
		return newItems, lastBackupAt
	}
	h.pruneOldBackups()
	log.Printf("auto-backup: wrote %s (%d items)", finalPath, newItems)

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
		if e.IsDir() || !backupFilenamePattern.MatchString(name) {
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
