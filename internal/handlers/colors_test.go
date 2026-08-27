package handlers

import (
	"bytes"
	"encoding/csv"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestNormalizeColor(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"known slug", "red", "red"},
		{"multi-word slug", "lightblue", "lightblue"},
		{"uppercase is accepted", "GREEN", "green"},
		{"surrounding space is trimmed", "  pink  ", "pink"},
		{"empty means no color", "", ""},
		{"unknown color falls back to none", "chartreuse", ""},
		{"a label is not a slug", "Light blue", ""},
		{"hex codes are not accepted", "#ff0000", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeColor(tc.in); got != tc.want {
				t.Errorf("normalizeColor(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestColorLabel(t *testing.T) {
	if got := ColorLabel("lightblue"); got != "Light blue" {
		t.Errorf("ColorLabel(lightblue) = %q, want %q", got, "Light blue")
	}
	if got := ColorLabel(""); got != "" {
		t.Errorf("ColorLabel(\"\") = %q, want empty", got)
	}
	if got := ColorLabel("chartreuse"); got != "" {
		t.Errorf("ColorLabel(chartreuse) = %q, want empty", got)
	}
	if got := (Item{Color: "darkblue"}).ColorName(); got != "Dark blue" {
		t.Errorf("Item.ColorName() = %q, want %q", got, "Dark blue")
	}
}

// TestSubmittedColorIgnoredWhenDisabled pins the rule that keeps a stale
// browser tab (or a hand-crafted POST) from writing colors into an instance
// that has the feature switched off.
func TestSubmittedColorIgnoredWhenDisabled(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader("color=red"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	off := &Handlers{}
	if got := off.submittedColor(req); got != "" {
		t.Errorf("submittedColor with feature off = %q, want empty", got)
	}

	on := &Handlers{ColorTags: ColorTagConfig{Enabled: true, Label: "Color"}}
	if got := on.submittedColor(req); got != "red" {
		t.Errorf("submittedColor with feature on = %q, want %q", got, "red")
	}
}

// TestUpdateItemPreservesColorWhenDisabled covers the data-loss trap in
// turning the feature off: the edit form stops rendering the swatches, so the
// submission carries no color at all, and a naive UPDATE would blank out a
// color the item was tagged with while the feature was on.
func TestUpdateItemPreservesColorWhenDisabled(t *testing.T) {
	database := newTestDB(t)
	tmpl := template.Must(template.New("item_edit.html").Parse(
		`{{define "layout"}}{{if .Error}}ERROR: {{.Error}}{{end}}{{end}}`))
	h := &Handlers{DB: database, Templates: Templates{"item_edit.html": tmpl}}

	res, err := database.Exec(`INSERT INTO bins (name) VALUES ('Bin 1')`)
	if err != nil {
		t.Fatalf("seed bin: %v", err)
	}
	binID, _ := res.LastInsertId()
	res, err = database.Exec(
		`INSERT INTO items (bin_id, name, color) VALUES (?, 'Hammer', 'red')`, binID)
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	itemID, _ := res.LastInsertId()

	// No color field in the form, exactly as the disabled edit page renders it.
	form := "name=Hammer&bin_id=" + strconv.FormatInt(binID, 10)
	req := httptest.NewRequest(http.MethodPost, "/items/"+strconv.FormatInt(itemID, 10), strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", strconv.FormatInt(itemID, 10))
	rec := httptest.NewRecorder()

	h.UpdateItem(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	var color string
	if err := database.QueryRow(`SELECT color FROM items WHERE id = ?`, itemID).Scan(&color); err != nil {
		t.Fatalf("read back color: %v", err)
	}
	if color != "red" {
		t.Errorf("color after edit with feature off = %q, want it preserved as %q", color, "red")
	}

	// With the feature on, the same empty submission *should* clear it —
	// that's the None swatch doing its job.
	h.ColorTags = ColorTagConfig{Enabled: true, Label: "Color"}
	req = httptest.NewRequest(http.MethodPost, "/items/"+strconv.FormatInt(itemID, 10), strings.NewReader(form+"&color="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", strconv.FormatInt(itemID, 10))
	h.UpdateItem(httptest.NewRecorder(), req)

	if err := database.QueryRow(`SELECT color FROM items WHERE id = ?`, itemID).Scan(&color); err != nil {
		t.Fatalf("read back color: %v", err)
	}
	if color != "" {
		t.Errorf("color after selecting None = %q, want it cleared", color)
	}
}

// TestBackupColorRoundTrip exports an inventory with color tags and imports
// the result back, which is the path a real restore takes.
func TestBackupColorRoundTrip(t *testing.T) {
	database := newTestDB(t)
	h := &Handlers{DB: database}

	if _, err := database.Exec(`INSERT INTO bins (id, name) VALUES (1, 'Bin 1')`); err != nil {
		t.Fatalf("seed bin: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO items (bin_id, name, color) VALUES
			(1, 'Hammer', 'red'),
			(1, 'Fuses', 'lightblue'),
			(1, 'Tape measure', '')`); err != nil {
		t.Fatalf("seed items: %v", err)
	}

	var out bytes.Buffer
	if _, err := h.writeCSVTo(&out); err != nil {
		t.Fatalf("export: %v", err)
	}

	records, err := csv.NewReader(bytes.NewReader(out.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("parse export: %v", err)
	}
	if !equalHeader(records[0], backupCSVHeader) {
		t.Fatalf("export header = %v, want %v", records[0], backupCSVHeader)
	}
	// Rows come out ordered by bin then item name.
	wantColors := map[string]string{"Fuses": "lightblue", "Hammer": "red", "Tape measure": ""}
	for _, rec := range records[1:] {
		if got, want := rec[6], wantColors[rec[0]]; got != want {
			t.Errorf("exported color for %s = %q, want %q", rec[0], got, want)
		}
	}

	// Import it back over a wiped inventory and confirm the colors survived.
	fresh := newTestDB(t)
	tmpl := template.Must(template.New("backup.html").Parse(
		`{{define "layout"}}{{if .Error}}ERROR: {{.Error}}{{end}}{{.Success}}{{end}}`))
	importer := &Handlers{DB: fresh, Templates: Templates{"backup.html": tmpl}}

	rec := postCSV(t, importer, out.String())
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "ERROR") {
		t.Fatalf("import failed: %d %s", rec.Code, rec.Body.String())
	}

	for name, want := range wantColors {
		var got string
		if err := fresh.QueryRow(`SELECT color FROM items WHERE name = ?`, name).Scan(&got); err != nil {
			t.Fatalf("read back %s: %v", name, err)
		}
		if got != want {
			t.Errorf("imported color for %s = %q, want %q", name, got, want)
		}
	}
}

// TestImportUnknownColorDegrades checks that a hand-edited backup naming a
// color binbash doesn't know still restores, with that one field dropped —
// rather than the whole import being refused over something cosmetic.
func TestImportUnknownColorDegrades(t *testing.T) {
	database := newTestDB(t)
	tmpl := template.Must(template.New("backup.html").Parse(
		`{{define "layout"}}{{if .Error}}ERROR: {{.Error}}{{end}}{{.Success}}{{end}}`))
	h := &Handlers{DB: database, Templates: Templates{"backup.html": tmpl}}

	csvText := strings.Join([]string{
		strings.Join(backupCSVHeader, ","),
		"Hammer,Claw hammer,tools,Bin 1,Large,Garage tools,chartreuse",
		"Fuses,,spares,Bin 1,Large,Garage tools,RED",
	}, "\n")

	rec := postCSV(t, h, csvText)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "Imported 2 item(s)") {
		t.Fatalf("unexpected response: %q", got)
	}

	var color string
	database.QueryRow(`SELECT color FROM items WHERE name = 'Hammer'`).Scan(&color)
	if color != "" {
		t.Errorf("unknown color imported as %q, want it dropped", color)
	}
	// Case-insensitive, matching what normalizeColor accepts from the form.
	database.QueryRow(`SELECT color FROM items WHERE name = 'Fuses'`).Scan(&color)
	if color != "red" {
		t.Errorf("uppercase color imported as %q, want %q", color, "red")
	}
}

// postCSV uploads csvText to ImportBackup the way the browser's file input
// does, and returns the recorded response.
func postCSV(t *testing.T, h *Handlers, csvText string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "binbash-backup.csv")
	if err != nil {
		t.Fatalf("build upload: %v", err)
	}
	if _, err := fw.Write([]byte(csvText)); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/backup/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ImportBackup(rec, req)
	return rec
}
