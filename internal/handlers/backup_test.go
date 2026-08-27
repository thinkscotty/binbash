package handlers

import (
	"encoding/csv"
	"strings"
	"testing"
)

// TestCSVFieldRoundTrip pins that sanitizeCSVField and desanitizeCSVField are
// exact inverses. They defuse spreadsheet formula injection by prefixing a
// single quote, so if they ever disagree about which fields carry one, import
// silently eats or duplicates a character the user typed -- permanent
// corruption that only shows up on the day someone restores a backup.
func TestCSVFieldRoundTrip(t *testing.T) {
	fields := []string{
		"",
		"Hammer",
		"Claw hammer, 16oz",
		"=SUM(A1:A9)", // the formula injection the escaping exists for
		"+1 spare",    //
		"-40C probe",  //
		"@home",       //
		"'-40C probe", // starts with a quote the user typed, then a formula char
		"'=mc2",       //
		"'@home",      //
		"'",           //
		"''",          //
		"'tis the season",
		"O'Brien's tools",
		"naïve café ☕",
	}

	for _, f := range fields {
		t.Run(f, func(t *testing.T) {
			exported := sanitizeCSVField(f)
			if got := desanitizeCSVField(exported); got != f {
				t.Errorf("round trip corrupted the field: %q -> export %q -> import %q", f, exported, got)
			}
		})
	}
}

// TestSanitizeCSVFieldDefusesFormulas guards the security property the
// escaping exists for: no exported field may begin with a character a
// spreadsheet would treat as the start of a formula.
func TestSanitizeCSVFieldDefusesFormulas(t *testing.T) {
	for _, f := range []string{"=SUM(A1)", "+1", "-40C", "@home", "=cmd|'/c calc'!A1"} {
		got := sanitizeCSVField(f)
		if got[0] != '\'' {
			t.Errorf("sanitizeCSVField(%q) = %q, which a spreadsheet would still evaluate", f, got)
		}
	}
}

// TestDesanitizeCSVFieldReadsLegacyExports makes sure tightening the escaping
// didn't break the backups users already have on disk, which were written
// before a leading quote was escaped.
func TestDesanitizeCSVFieldReadsLegacyExports(t *testing.T) {
	legacy := map[string]string{
		"Hammer":       "Hammer",      // never escaped
		"'=SUM(A1:A9)": "=SUM(A1:A9)", // escaped by the old rule, still unwrapped
		"'-40C probe":  "-40C probe",  //
		"'tis":         "'tis",        // old rule left this alone; must stay intact
		"O'Brien":      "O'Brien",     //
	}
	for in, want := range legacy {
		if got := desanitizeCSVField(in); got != want {
			t.Errorf("desanitizeCSVField(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExportedFieldsAreRoundTrippable drives the property through the real
// export path, so a field that needs escaping is exercised end to end.
func TestExportedFieldsAreRoundTrippable(t *testing.T) {
	database := newTestDB(t)
	h := &Handlers{DB: database}

	if _, err := database.Exec(`INSERT INTO bins (id, name) VALUES (1, '-40C Freezer')`); err != nil {
		t.Fatalf("seed bin: %v", err)
	}
	if _, err := database.Exec(
		`INSERT INTO items (bin_id, name, description) VALUES (1, ?, ?)`,
		"'-40C probe", "=SUM(A1:A9)"); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	var out strings.Builder
	if _, err := h.writeCSVTo(&out); err != nil {
		t.Fatalf("export: %v", err)
	}

	fresh := newTestDB(t)
	importer := &Handlers{DB: fresh}
	rows, errMsg := parseBinbashCSV(splitCSV(t, out.String()), true)
	if errMsg != "" {
		t.Fatalf("re-import rejected the export: %s", errMsg)
	}
	if _, _, err := importer.commitImport(rows, false); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var name, desc, binName string
	if err := fresh.QueryRow(
		`SELECT items.name, items.description, bins.name FROM items JOIN bins ON bins.id = items.bin_id`,
	).Scan(&name, &desc, &binName); err != nil {
		t.Fatal(err)
	}
	if name != "'-40C probe" {
		t.Errorf("item name restored as %q, want %q", name, "'-40C probe")
	}
	if desc != "=SUM(A1:A9)" {
		t.Errorf("description restored as %q, want %q", desc, "=SUM(A1:A9)")
	}
	if binName != "-40C Freezer" {
		t.Errorf("bin name restored as %q, want %q", binName, "-40C Freezer")
	}
}

// splitCSV parses exported CSV text back into records.
func splitCSV(t *testing.T, text string) [][]string {
	t.Helper()
	records, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		t.Fatalf("parse export: %v", err)
	}
	return records
}
