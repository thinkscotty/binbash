package handlers

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thinkscotty/binbash/internal/db"
)

// newTestDB opens a fresh, fully-migrated SQLite database in a temp dir.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// TestParseHomeboxCSVSample runs the parser over a fixture of real rows from an
// actual Homebox export, covering the cases that motivated the feature: nested
// locations, a bare location with no bin, an item that merely mentions "Bin" in
// its name while living loose in the garage, tag transformation, and a
// multi-line quoted description.
func TestParseHomeboxCSVSample(t *testing.T) {
	f, err := os.Open("testdata/homebox_sample.csv")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if !isHomeboxHeader(records[0]) {
		t.Fatal("fixture header not detected as Homebox")
	}

	rows, noBin, archived, noName := parseHomeboxCSV(records)

	if len(rows) != 5 {
		t.Errorf("imported rows = %d, want 5", len(rows))
	}
	if noBin != 2 {
		t.Errorf("skippedNoBin = %d, want 2 (both bare-Garage rows)", noBin)
	}
	if archived != 0 || noName != 0 {
		t.Errorf("skippedArchived=%d skippedNoName=%d, want 0/0", archived, noName)
	}

	// Deepest bin wins, and the bare-Garage rows (including the item literally
	// named "Bin 09 - Computer and Devices") never become bins.
	gotBins := map[string]bool{}
	for _, r := range rows {
		gotBins[r.binName] = true
	}
	wantBins := []string{
		"Electronics Bin",
		"Bin 01 - Dogs",
		"Passive Electronics Mini Bin",
		"Bin 03 - Scotty's Hobbies",
		"Small Bin - Scotty's Miscellaneous",
	}
	for _, name := range wantBins {
		if !gotBins[name] {
			t.Errorf("missing expected bin %q", name)
		}
	}
	if len(gotBins) != len(wantBins) {
		t.Errorf("distinct bins = %d, want %d (%v)", len(gotBins), len(wantBins), gotBins)
	}

	// Tags become comma-separated keywords on the row they belong to.
	for _, r := range rows {
		if r.binName == "Electronics Bin" {
			if r.keywords != "DIY Electronics, Battery and Battery Charging" {
				t.Errorf("keywords = %q, want %q", r.keywords, "DIY Electronics, Battery and Battery Charging")
			}
		}
	}
}

// TestCommitImportMergeAndCreate verifies the shared committer both parsers rely
// on: existing bins are matched by name and never duplicated or overwritten,
// while unknown bins are created once and reused within the same import.
func TestCommitImportMergeAndCreate(t *testing.T) {
	database := newTestDB(t)
	if _, err := database.Exec(`INSERT INTO bins (name, category, description) VALUES ('Bin 1', 'Large', 'existing')`); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{DB: database}

	rows := []importRow{
		{itemName: "Item A", binName: "Bin 1"}, // merges into pre-existing bin
		{itemName: "Item B", binName: "Bin 2"}, // creates a new bin
		{itemName: "Item C", binName: "Bin 2"}, // reuses the just-created bin
	}
	items, newBins, err := h.commitImport(rows, false)
	if err != nil {
		t.Fatalf("commitImport: %v", err)
	}
	if items != 3 {
		t.Errorf("items = %d, want 3", items)
	}
	if newBins != 1 {
		t.Errorf("newBins = %d, want 1", newBins)
	}

	var binCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM bins`).Scan(&binCount); err != nil {
		t.Fatal(err)
	}
	if binCount != 2 {
		t.Errorf("total bins = %d, want 2", binCount)
	}

	// A merge must leave the existing bin's own fields untouched.
	var category, description string
	if err := database.QueryRow(`SELECT category, description FROM bins WHERE name = 'Bin 1'`).Scan(&category, &description); err != nil {
		t.Fatal(err)
	}
	if category != "Large" || description != "existing" {
		t.Errorf("existing bin overwritten: category=%q description=%q", category, description)
	}
}

// TestCommitImportReplace verifies the replace path wipes prior data.
func TestCommitImportReplace(t *testing.T) {
	database := newTestDB(t)
	if _, err := database.Exec(`INSERT INTO bins (name) VALUES ('Old Bin')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO items (bin_id, name) VALUES (1, 'Old Item')`); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{DB: database}

	if _, _, err := h.commitImport([]importRow{{itemName: "New Item", binName: "New Bin"}}, true); err != nil {
		t.Fatalf("commitImport replace: %v", err)
	}

	var bins, items int
	if err := database.QueryRow(`SELECT COUNT(*) FROM bins`).Scan(&bins); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if bins != 1 || items != 1 {
		t.Errorf("after replace: bins=%d items=%d, want 1/1", bins, items)
	}
}

// TestImportBackupHomeboxEndToEnd drives the whole HTTP handler: a multipart
// upload of the real-row fixture, through format detection, parsing, the DB
// commit, and the rendered response — the same path a browser upload takes.
func TestImportBackupHomeboxEndToEnd(t *testing.T) {
	database := newTestDB(t)
	// A stand-in layout template so renderBackup can execute without pulling in
	// the real embedded templates; it surfaces just the Success/Error strings.
	tmpl := template.Must(template.New("backup.html").Parse(
		`{{define "layout"}}{{if .Error}}ERROR: {{.Error}}{{end}}{{.Success}}{{end}}`))
	h := &Handlers{DB: database, Templates: Templates{"backup.html": tmpl}}

	fixture, err := os.ReadFile("testdata/homebox_sample.csv")
	if err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "homebox_sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(fixture); err != nil {
		t.Fatal(err)
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/backup/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	h.ImportBackup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()
	if !strings.Contains(got, "Imported 5 item(s) (5 new bin(s)).") {
		t.Errorf("response missing import summary; got: %q", got)
	}
	if !strings.Contains(got, "Skipped 2 with no bin in their location.") {
		t.Errorf("response missing skip note; got: %q", got)
	}

	var items, bins int
	if err := database.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM bins`).Scan(&bins); err != nil {
		t.Fatal(err)
	}
	if items != 5 || bins != 5 {
		t.Errorf("DB after import: items=%d bins=%d, want 5/5", items, bins)
	}
}

// TestImportBackupReplaceWithNoImportableRows verifies the safety guard: a
// Homebox file whose rows are all skipped (none name a bin) must NOT wipe an
// existing inventory when "Replace" is checked — that would be silent data loss.
func TestImportBackupReplaceWithNoImportableRows(t *testing.T) {
	database := newTestDB(t)
	if _, err := database.Exec(`INSERT INTO bins (name) VALUES ('Keeper Bin')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO items (bin_id, name) VALUES (1, 'Keeper Item')`); err != nil {
		t.Fatal(err)
	}
	tmpl := template.Must(template.New("backup.html").Parse(
		`{{define "layout"}}{{if .Error}}ERROR: {{.Error}}{{end}}{{.Success}}{{end}}`))
	h := &Handlers{DB: database, Templates: Templates{"backup.html": tmpl}}

	// A Homebox file with a real item, but a bare location that names no bin —
	// so parseHomeboxCSV yields zero importable rows.
	csvData := strings.Join([]string{
		"HB.name,HB.location,HB.archived",
		"Loose Thing,Garage,false",
	}, "\n")

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("replace", "on")
	fw, _ := mw.CreateFormFile("file", "homebox.csv")
	fw.Write([]byte(csvData))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/backup/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	h.ImportBackup(rec, req)

	if got := rec.Body.String(); !strings.Contains(got, "nothing was replaced") {
		t.Errorf("expected refusal message, got: %q", got)
	}

	// The pre-existing inventory must be completely untouched.
	var bins, items int
	database.QueryRow(`SELECT COUNT(*) FROM bins`).Scan(&bins)
	database.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&items)
	if bins != 1 || items != 1 {
		t.Errorf("inventory changed after refused replace: bins=%d items=%d, want 1/1", bins, items)
	}
}

// TestImportBackupBinbashEndToEnd guards against regressions in binbash's own
// import format after the parser split: a native export must still be detected
// and loaded through the same refactored handler.
func TestImportBackupBinbashEndToEnd(t *testing.T) {
	database := newTestDB(t)
	tmpl := template.Must(template.New("backup.html").Parse(
		`{{define "layout"}}{{if .Error}}ERROR: {{.Error}}{{end}}{{.Success}}{{end}}`))
	h := &Handlers{DB: database, Templates: Templates{"backup.html": tmpl}}

	native := strings.Join([]string{
		"item_name,item_description,keywords,bin_name,bin_category,bin_description",
		"Hammer,Claw hammer,tools,Bin 1,Large,Garage tools",
		"Screwdriver,,tools,Bin 1,Large,Garage tools",
	}, "\n")

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "binbash-backup.csv")
	fw.Write([]byte(native))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/backup/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	h.ImportBackup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "Imported 2 item(s) (1 new bin(s)).") {
		t.Errorf("unexpected response: %q", got)
	}

	var items, bins int
	database.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&items)
	database.QueryRow(`SELECT COUNT(*) FROM bins`).Scan(&bins)
	if items != 2 || bins != 1 {
		t.Errorf("DB after import: items=%d bins=%d, want 2/1", items, bins)
	}
}
