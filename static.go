package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
)

// Files embedded in the binary carry no modification time, so on their own
// they're served with neither Last-Modified nor ETag. That leaves a browser
// no way to ask "has this changed?", and some (Safari, Firefox) simply keep
// using the copy they cached -- so after an upgrade, new pages could render
// against an old stylesheet. Content hashes close that gap in two ways:
// pages link to each file as /static/<file>?v=<hash> (see assetURL), so a
// release that changes a file also changes its URL; and every response
// carries the hash as an ETag, so anything fetched without the right ?v is
// checked on each use rather than trusted.

// hashAssets returns a short content hash for every file in static, keyed by
// its path within it ("style.css", "fonts/jetbrains-mono-latin.woff2").
func hashAssets(static fs.FS) (map[string]string, error) {
	hashes := make(map[string]string)
	err := fs.WalkDir(static, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(static, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		hashes[path] = hex.EncodeToString(sum[:6])
		return nil
	})
	return hashes, err
}

// assetURL returns the "asset" template function, which turns a static file's
// path into its versioned URL. An unknown name falls back to the plain path,
// so a typo shows up as a missing file in the browser rather than taking down
// every page with a render error.
func assetURL(hashes map[string]string) func(string) string {
	return func(name string) string {
		if hash, ok := hashes[name]; ok {
			return "/static/" + name + "?v=" + hash
		}
		return "/static/" + name
	}
}

// staticHandler serves the embedded static files under /static/.
func staticHandler(static fs.FS, hashes map[string]string) http.Handler {
	files := http.FileServer(http.FS(static))
	cached := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hash, ok := hashes[r.URL.Path]; ok {
			// http.FileServer honours an ETag already set on the response, so
			// a matching If-None-Match gets a body-less 304.
			w.Header().Set("ETag", `"`+hash+`"`)
			if r.URL.Query().Get("v") == hash {
				// This exact URL can never serve different bytes: a changed
				// file gets a new hash, and pages link to the new URL.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				// Fonts (linked from style.css by plain path) or a stale ?v:
				// fine to cache, but confirm it's current before each use.
				w.Header().Set("Cache-Control", "no-cache")
			}
		}
		files.ServeHTTP(w, r)
	})
	return noDirListing(http.StripPrefix("/static/", cached))
}
