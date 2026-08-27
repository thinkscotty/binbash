package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRecentItemsAreCapped pins the bound on the add-item page's list.
// CreateItem redirects straight back here, so an uncapped list means every
// single add re-renders the entire inventory -- on the one page where entry
// speed matters most.
func TestRecentItemsAreCapped(t *testing.T) {
	tests := []struct {
		name      string
		seed      int
		wantShown int
		wantMore  bool
	}{
		{"under the cap", 10, 10, false},
		{"exactly at the cap", recentItemsLimit, recentItemsLimit, false},
		{"over the cap", recentItemsLimit + 25, recentItemsLimit, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := newTestDB(t)
			// A stub page that reports just what the assertions need.
			tmpl := template.Must(template.New("items.html").Parse(
				`{{define "layout"}}shown={{len .Items}} more={{.MoreItems}} first={{with index .Items 0}}{{.Name}}{{end}}{{end}}`))
			h := &Handlers{DB: database, Templates: Templates{"items.html": tmpl}}

			if _, err := database.Exec(`INSERT INTO bins (id, name) VALUES (1, 'Bin 1')`); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < tt.seed; i++ {
				if _, err := database.Exec(
					`INSERT INTO items (bin_id, name) VALUES (1, ?)`, fmt.Sprintf("Item %03d", i)); err != nil {
					t.Fatal(err)
				}
			}

			rec := httptest.NewRecorder()
			h.ListItems(rec, httptest.NewRequest(http.MethodGet, "/items", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}

			body := rec.Body.String()
			want := fmt.Sprintf("shown=%d more=%t", tt.wantShown, tt.wantMore)
			if !strings.HasPrefix(body, want) {
				t.Errorf("got %q, want it to start with %q", body, want)
			}
			// Newest first: the cap must trim the oldest, not the newest, or
			// the item you just added wouldn't be on the page you land on.
			newest := fmt.Sprintf("Item %03d", tt.seed-1)
			if !strings.Contains(body, "first="+newest) {
				t.Errorf("got %q, want the most recent item (%s) at the top", body, newest)
			}
		})
	}
}
