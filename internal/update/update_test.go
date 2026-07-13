package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testUpdater(version, baseURL string) *Updater {
	return &Updater{
		version: version,
		repo:    "thinkscotty/binbash",
		baseURL: baseURL,
		api:     &http.Client{Timeout: 5 * time.Second},
		dl:      &http.Client{},
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name    string
		running string
		tag     string
		want    bool
	}{
		{"patch newer", "v0.1.3", "v0.1.4", true},
		{"minor newer", "v0.1.3", "v0.2.0", true},
		{"major newer", "v0.1.3", "v1.0.0", true},
		{"same version", "v0.1.3", "v0.1.3", false},
		{"older", "v0.1.3", "v0.1.2", false},
		{"dev build never updates", "dev", "v99.0.0", false},
		{"malformed tag", "v0.1.3", "latest", false},
		{"missing v prefix", "v0.1.3", "0.9.9", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := testUpdater(tt.running, "")
			if got := u.IsNewer(tt.tag); got != tt.want {
				t.Errorf("IsNewer(%q) with running %q = %v, want %v", tt.tag, tt.running, got, tt.want)
			}
		})
	}
}

func TestDevBuild(t *testing.T) {
	if !testUpdater("dev", "").DevBuild() {
		t.Error("DevBuild() = false for version dev")
	}
	if testUpdater("v0.1.3", "").DevBuild() {
		t.Error("DevBuild() = true for version v0.1.3")
	}
}

func TestAssetName(t *testing.T) {
	got := assetName("v0.1.4", "linux", "arm64")
	want := "binbash-v0.1.4-linux-arm64.tar.gz"
	if got != want {
		t.Errorf("assetName = %q, want %q", got, want)
	}
}

func TestChecksumFor(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	sums := digest + "  binbash-v0.1.4-linux-amd64.tar.gz\n" +
		strings.Repeat("cd", 32) + "  *binbash-v0.1.4-darwin-arm64.tar.gz\n" +
		"not a checksum line\n"

	t.Run("plain entry", func(t *testing.T) {
		got, err := checksumFor(sums, "binbash-v0.1.4-linux-amd64.tar.gz")
		if err != nil || got != digest {
			t.Errorf("got %q, %v; want %q, nil", got, err, digest)
		}
	})
	t.Run("binary-mode asterisk tolerated", func(t *testing.T) {
		got, err := checksumFor(sums, "binbash-v0.1.4-darwin-arm64.tar.gz")
		if err != nil || got != strings.Repeat("cd", 32) {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("missing entry", func(t *testing.T) {
		if _, err := checksumFor(sums, "binbash-v0.1.4-windows-amd64.zip"); err == nil {
			t.Error("expected an error for a missing entry")
		}
	})
	t.Run("malformed digest", func(t *testing.T) {
		if _, err := checksumFor("abc123  file.tar.gz\n", "file.tar.gz"); err == nil {
			t.Error("expected an error for a short digest")
		}
	})
}

// writeArchive builds a release-shaped tar.gz (files nested under a top-level
// directory) and returns its path.
func writeArchive(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractBinary(t *testing.T) {
	member := "binbash-v0.1.4-linux-amd64/binbash"
	archive := writeArchive(t, map[string]string{
		"binbash-v0.1.4-linux-amd64/README.md": "docs",
		member:                                 "#!/bin/sh\necho fake binary\n",
	})

	dest := filepath.Join(t.TempDir(), "binbash.new")
	if err := extractBinary(archive, member, dest); err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "#!/bin/sh\necho fake binary\n" {
		t.Errorf("extracted content = %q", got)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("extracted mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestExtractBinaryDotSlashPrefix(t *testing.T) {
	member := "binbash-v0.1.4-linux-amd64/binbash"
	archive := writeArchive(t, map[string]string{"./" + member: "bin"})
	dest := filepath.Join(t.TempDir(), "binbash.new")
	if err := extractBinary(archive, member, dest); err != nil {
		t.Fatalf("extractBinary with ./ prefix: %v", err)
	}
}

func TestExtractBinaryMissingMember(t *testing.T) {
	archive := writeArchive(t, map[string]string{"binbash-v0.1.4-linux-amd64/README.md": "docs"})
	dest := filepath.Join(t.TempDir(), "binbash.new")
	err := extractBinary(archive, "binbash-v0.1.4-linux-amd64/binbash", dest)
	if err == nil {
		t.Fatal("expected an error for a missing member")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("dest should not exist after a failed extraction")
	}
}

func TestExtractBinaryNotAnArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bogus.tar.gz")
	if err := os.WriteFile(path, []byte("<html>error page</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractBinary(path, "x/binbash", filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("expected an error for a non-archive file")
	}
}

func TestSwapBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "binbash")
	newBin := exe + ".new"
	oldBin := exe + ".old"
	if err := os.WriteFile(exe, []byte("old code"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newBin, []byte("new code"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := swapBinary(exe, newBin, oldBin); err != nil {
		t.Fatalf("swapBinary: %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new code" {
		t.Errorf("exe content = %q, want new code", got)
	}
	if got, _ := os.ReadFile(oldBin); string(got) != "old code" {
		t.Errorf(".old content = %q, want old code", got)
	}
	if _, err := os.Stat(newBin); !os.IsNotExist(err) {
		t.Error(".new should be gone after a successful swap")
	}
}

func TestSwapBinaryRollsBackWhenInstallFails(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "binbash")
	oldBin := exe + ".old"
	if err := os.WriteFile(exe, []byte("old code"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A nonexistent .new makes the second rename fail after the first
	// succeeded, exercising the rollback path.
	err := swapBinary(exe, filepath.Join(dir, "does-not-exist"), oldBin)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got, readErr := os.ReadFile(exe); readErr != nil || string(got) != "old code" {
		t.Errorf("exe not restored after failed install: %q, %v", got, readErr)
	}
}

func TestCheck(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/thinkscotty/binbash/releases/latest" {
				t.Errorf("unexpected path %s", r.URL.Path)
			}
			if r.Header.Get("User-Agent") == "" {
				t.Error("missing User-Agent header")
			}
			w.Write([]byte(`{
				"tag_name": "v0.1.4",
				"html_url": "https://github.com/thinkscotty/binbash/releases/tag/v0.1.4",
				"assets": [
					{"name": "binbash-v0.1.4-linux-amd64.tar.gz", "browser_download_url": "https://example.com/a"},
					{"name": "SHA256SUMS.txt", "browser_download_url": "https://example.com/s"}
				]
			}`))
		}))
		defer srv.Close()

		rel, err := testUpdater("v0.1.3", srv.URL).Check(context.Background())
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if rel.Tag != "v0.1.4" || len(rel.Assets) != 2 {
			t.Errorf("rel = %+v", rel)
		}
		if _, ok := rel.FindAsset("SHA256SUMS.txt"); !ok {
			t.Error("FindAsset missed SHA256SUMS.txt")
		}
	})

	t.Run("no releases", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message": "Not Found"}`, http.StatusNotFound)
		}))
		defer srv.Close()
		if _, err := testUpdater("v0.1.3", srv.URL).Check(context.Background()); err == nil ||
			!strings.Contains(err.Error(), "no published releases") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message": "API rate limit exceeded"}`, http.StatusForbidden)
		}))
		defer srv.Close()
		if _, err := testUpdater("v0.1.3", srv.URL).Check(context.Background()); err == nil ||
			!strings.Contains(err.Error(), "rate-limiting") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("malformed tag", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"tag_name": "nightly", "html_url": "", "assets": []}`))
		}))
		defer srv.Close()
		if _, err := testUpdater("v0.1.3", srv.URL).Check(context.Background()); err == nil ||
			!strings.Contains(err.Error(), "unexpected tag") {
			t.Errorf("err = %v", err)
		}
	})
}
