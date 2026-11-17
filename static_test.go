package main

import (
	"bytes"
	"html/template"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func embeddedStatic(t *testing.T) (fs.FS, map[string]string) {
	t.Helper()
	static, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		t.Fatal(err)
	}
	hashes, err := hashAssets(static)
	if err != nil {
		t.Fatal(err)
	}
	return static, hashes
}

// The layout must link to the stylesheet and htmx by their versioned URLs;
// otherwise a browser can keep a pre-upgrade copy against post-upgrade pages.
func TestLayoutLinksVersionedAssets(t *testing.T) {
	_, hashes := embeddedStatic(t)
	templates, err := loadTemplates(template.FuncMap{"asset": assetURL(hashes)})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	data := map[string]any{"CurrentPage": "login"}
	if err := templates["login.html"].ExecuteTemplate(&buf, "layout", data); err != nil {
		t.Fatal(err)
	}
	page := buf.String()
	for _, name := range []string{"style.css", "htmx.min.js"} {
		if hashes[name] == "" {
			t.Fatalf("no hash for %s", name)
		}
		want := "/static/" + name + "?v=" + hashes[name]
		if !strings.Contains(page, want) {
			t.Errorf("layout does not link %s", want)
		}
	}
}

func TestAssetURLUnknownFileFallsBack(t *testing.T) {
	if got := assetURL(map[string]string{})("nope.css"); got != "/static/nope.css" {
		t.Errorf("got %q", got)
	}
}

func TestStaticCaching(t *testing.T) {
	static, hashes := embeddedStatic(t)
	handler := staticHandler(static, hashes)
	css := hashes["style.css"]

	get := func(target, ifNoneMatch string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	tests := []struct {
		name, target, ifNoneMatch string
		wantStatus                int
		wantCache                 string
	}{
		{"current version is cached long-term", "/static/style.css?v=" + css, "", http.StatusOK, "public, max-age=31536000, immutable"},
		{"unversioned must revalidate", "/static/style.css", "", http.StatusOK, "no-cache"},
		{"old version must revalidate", "/static/style.css?v=000000000000", "", http.StatusOK, "no-cache"},
		{"font must revalidate", "/static/fonts/jetbrains-mono-latin.woff2", "", http.StatusOK, "no-cache"},
		{"unchanged file revalidates to 304", "/static/style.css", `"` + css + `"`, http.StatusNotModified, "no-cache"},
		{"changed file sends a fresh copy", "/static/style.css", `"000000000000"`, http.StatusOK, "no-cache"},
		{"directory listing stays hidden", "/static/", "", http.StatusNotFound, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(tc.target, tc.ifNoneMatch)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tc.wantCache)
			}
			if tc.wantStatus == http.StatusOK && rec.Header().Get("ETag") == "" {
				t.Error("missing ETag")
			}
		})
	}
}
