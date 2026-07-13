// Package update implements binbash's self-update: checking the GitHub
// releases API for a newer version, and downloading, verifying, and
// atomically swapping in the new binary.
//
// The trust chain for an update is: release metadata from the GitHub API
// (over HTTPS) → the downloaded archive's SHA-256 must match the release's
// own SHA256SUMS.txt → the extracted binary must run `-version` and report
// the expected tag → only then is it renamed over the current executable,
// with the previous binary kept alongside as a manual rollback.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

const defaultBaseURL = "https://api.github.com"

// Updater checks a GitHub repository's releases against the running version.
type Updater struct {
	version string
	repo    string // GitHub "owner/name"
	baseURL string

	// api has an overall timeout because release metadata is small; asset
	// downloads instead use dl, bounded only by the request context, since a
	// multi-megabyte archive on a slow link can legitimately take minutes.
	api *http.Client
	dl  *http.Client

	// busy makes Apply single-flight: a second submission while an update is
	// running (double-click, second tab) is refused rather than queued, since
	// the first one ends in a restart anyway.
	busy sync.Mutex
}

// New returns an Updater for the running binary's version ("dev" when built
// untagged). BINBASH_UPDATE_BASE_URL is an undocumented hook that points the
// updater at a mock of the GitHub API so tests can exercise the full update
// path without touching the real one.
func New(version string) *Updater {
	base := defaultBaseURL
	if v := os.Getenv("BINBASH_UPDATE_BASE_URL"); v != "" {
		base = v
	}
	return &Updater{
		version: version,
		repo:    "thinkscotty/binbash",
		baseURL: base,
		api:     &http.Client{Timeout: 30 * time.Second},
		dl:      &http.Client{},
	}
}

// Version returns the running binary's version string.
func (u *Updater) Version() string { return u.version }

// DevBuild reports whether the running binary was built without a release
// tag. Dev builds can check for releases but refuse to self-update: the
// binary on disk is ahead of (or unrelated to) any published version.
func (u *Updater) DevBuild() bool { return !semver.IsValid(u.version) }

// IsNewer reports whether tag is a strictly newer release than the running
// version. Dev builds and malformed tags never compare newer.
func (u *Updater) IsNewer(tag string) bool {
	if u.DevBuild() || !semver.IsValid(tag) {
		return false
	}
	return semver.Compare(tag, u.version) > 0
}

// Release describes a published GitHub release.
type Release struct {
	Tag    string // e.g. "v0.1.4"
	URL    string // human-readable release-notes page
	Assets []Asset
}

// Asset is a single downloadable file attached to a release.
type Asset struct {
	Name string
	URL  string
}

// FindAsset returns the asset with the given name, or false if the release
// doesn't carry one.
func (r Release) FindAsset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// Check fetches the latest published release. Errors are phrased for direct
// display on the settings page.
func (u *Updater) Check(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.baseURL+"/repos/"+u.repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// GitHub's API rejects requests without a User-Agent.
	req.Header.Set("User-Agent", "binbash/"+u.version)

	resp, err := u.api.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("could not reach GitHub — check the server's internet connection (%w)", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Release{}, fmt.Errorf("read response: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Release{}, fmt.Errorf("no published releases found for %s", u.repo)
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		// Unauthenticated GitHub API access is rate-limited per IP.
		return Release{}, fmt.Errorf("GitHub is rate-limiting update checks from this address — try again in an hour")
	case resp.StatusCode != http.StatusOK:
		return Release{}, fmt.Errorf("GitHub returned status %d", resp.StatusCode)
	}

	var parsed struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Release{}, fmt.Errorf("GitHub returned an unparseable response")
	}
	if !semver.IsValid(parsed.TagName) {
		return Release{}, fmt.Errorf("latest release has unexpected tag %q", parsed.TagName)
	}

	rel := Release{Tag: parsed.TagName, URL: parsed.HTMLURL}
	for _, a := range parsed.Assets {
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.BrowserDownloadURL})
	}
	return rel, nil
}
