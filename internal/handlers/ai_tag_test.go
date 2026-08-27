package handlers

import (
	"html/template"
	"net/http"
	"strings"
	"testing"
)

// TestMergeKeywordsRespectsLimit pins the bound that keeps AI tagging from
// writing a keywords value no other part of binbash will accept. TagItems is
// the only writer that bypasses validateItem, so without this cap it can
// produce an item its own edit form refuses to save -- and, far worse, a CSV
// export that import rejects outright, since parseBinbashCSV is
// all-or-nothing and fails the whole restore on one over-long row.
func TestMergeKeywordsRespectsLimit(t *testing.T) {
	// 494 characters: the most a user could legitimately have entered and
	// still be under the 500 limit.
	nearLimit := strings.TrimSuffix(strings.Repeat("widget, ", 62), ", ")
	if tooLong(nearLimit, maxKeywordsLen) {
		t.Fatalf("fixture is already over the limit at %d runes", len([]rune(nearLimit)))
	}

	tests := []struct {
		name     string
		existing string
		tags     []string
		want     string
	}{
		{"appends when there is room", "tools", []string{"hammer", "spare"}, "tools, hammer, spare"},
		{"skips duplicates case-insensitively", "Tools", []string{"tools", "spare"}, "Tools, spare"},
		{"fills an empty field", "", []string{"hammer"}, "hammer"},
		{"drops blank tags", "tools", []string{"", "  "}, "tools"},
		{"stops before crossing the limit", nearLimit, []string{"storage", "household"}, nearLimit},
		{"no tags leaves it alone", nearLimit, nil, nearLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeKeywords(tt.existing, tt.tags)
			if got != tt.want {
				t.Errorf("mergeKeywords(%d runes, %v) = %q, want %q",
					len([]rune(tt.existing)), tt.tags, got, tt.want)
			}
			if tooLong(got, maxKeywordsLen) {
				t.Errorf("result is %d runes, over the %d limit", len([]rune(got)), maxKeywordsLen)
			}
		})
	}
}

// TestMergeKeywordsCountsRunesNotBytes makes sure the cap agrees with
// validateItem, which measures in runes. Measuring bytes here would reject
// accented or non-Latin tags well before the real limit.
func TestMergeKeywordsCountsRunesNotBytes(t *testing.T) {
	// 400 runes of 3-byte characters: 1200 bytes, comfortably under 500 runes.
	existing := strings.Repeat("箱", 400)
	got := mergeKeywords(existing, []string{"spare"})
	if got != existing+", spare" {
		t.Errorf("a multi-byte field under the rune limit was not extended; got %d runes", len([]rune(got)))
	}
}

// TestAITaggedItemStillRestores is the end-to-end guard on the same bug: an
// item whose keywords were extended by tagging must still survive binbash's
// own export/import round trip.
func TestAITaggedItemStillRestores(t *testing.T) {
	database := newTestDB(t)
	h := &Handlers{DB: database}

	if _, err := database.Exec(`INSERT INTO bins (id, name) VALUES (1, 'Bin 1')`); err != nil {
		t.Fatalf("seed bin: %v", err)
	}
	nearLimit := strings.TrimSuffix(strings.Repeat("widget, ", 62), ", ")
	merged := mergeKeywords(nearLimit, []string{"storage", "household", "hardware"})
	if _, err := database.Exec(
		`INSERT INTO items (bin_id, name, keywords, ai_tagged) VALUES (1, 'Hammer', ?, 1)`, merged); err != nil {
		t.Fatalf("seed item: %v", err)
	}

	var out strings.Builder
	if _, err := h.writeCSVTo(&out); err != nil {
		t.Fatalf("export: %v", err)
	}

	fresh := newTestDB(t)
	tmpl := template.Must(template.New("backup.html").Parse(
		`{{define "layout"}}{{if .Error}}ERROR: {{.Error}}{{end}}{{.Success}}{{end}}`))
	importer := &Handlers{DB: fresh, Templates: Templates{"backup.html": tmpl}}

	rec := postCSV(t, importer, out.String())
	body := strings.TrimSpace(rec.Body.String())
	if rec.Code != http.StatusOK || strings.Contains(body, "ERROR") {
		t.Fatalf("an AI-tagged item made the backup unrestorable: %d %s", rec.Code, body)
	}
}
