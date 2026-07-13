package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Size caps are pure sanity limits — a real archive is ~10 MB — so a
// corrupted or malicious response can't fill the disk before the checksum
// check gets a chance to reject it.
const (
	maxArchiveBytes = 150 << 20
	maxBinaryBytes  = 150 << 20
)

const checksumsName = "SHA256SUMS.txt"

// Apply downloads rel, verifies it, and atomically swaps the new binary over
// the running executable, keeping the previous one alongside as
// "<binary>.old" for manual rollback. On success the process is still
// running the old code — the caller is responsible for restarting.
// Errors are phrased for direct display on the settings page.
func (u *Updater) Apply(ctx context.Context, rel Release) error {
	if !u.busy.TryLock() {
		return fmt.Errorf("an update is already in progress")
	}
	defer u.busy.Unlock()

	exePath, asset, err := u.preflight(rel)
	if err != nil {
		return err
	}

	want, err := u.fetchChecksum(ctx, rel, asset.Name)
	if err != nil {
		return err
	}

	archive, got, err := u.downloadArchive(ctx, asset)
	if err != nil {
		return err
	}
	defer os.Remove(archive)

	if got != want {
		return fmt.Errorf("the downloaded archive failed checksum verification — not installing it")
	}

	// The new binary is written next to the current one so the final rename
	// stays on one filesystem, which is what makes it atomic.
	newPath := exePath + ".new"
	member := strings.TrimSuffix(asset.Name, ".tar.gz") + "/binbash"
	if err := extractBinary(archive, member, newPath); err != nil {
		return err
	}

	if err := sanityCheck(ctx, newPath, rel.Tag); err != nil {
		os.Remove(newPath)
		return err
	}

	if err := swapBinary(exePath, newPath, exePath+".old"); err != nil {
		os.Remove(newPath)
		return err
	}
	return nil
}

// preflight rejects every situation where an update can't safely proceed,
// and resolves the executable path and matching release asset when it can.
func (u *Updater) preflight(rel Release) (string, Asset, error) {
	if runtime.GOOS == "windows" {
		return "", Asset{}, fmt.Errorf("self-update isn't available on Windows — download the new release from GitHub and replace binbash.exe by hand")
	}
	if inContainer() {
		return "", Asset{}, fmt.Errorf("binbash appears to be running in a container — update by deploying the new image instead")
	}
	if u.DevBuild() {
		return "", Asset{}, fmt.Errorf("this is a development build — update from source instead")
	}
	if !u.IsNewer(rel.Tag) {
		return "", Asset{}, fmt.Errorf("no update to apply: the latest release is %s and this is %s", rel.Tag, u.version)
	}

	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return "", Asset{}, fmt.Errorf("could not locate the running binary: %w", err)
	}
	if err := writableDir(filepath.Dir(exe)); err != nil {
		return "", Asset{}, fmt.Errorf(
			"binbash can't write to its install directory (%s) — update manually, or give the service user ownership of that directory",
			filepath.Dir(exe))
	}

	name := assetName(rel.Tag, runtime.GOOS, runtime.GOARCH)
	asset, ok := rel.FindAsset(name)
	if !ok {
		return "", Asset{}, fmt.Errorf("release %s has no build for this platform (%s/%s)", rel.Tag, runtime.GOOS, runtime.GOARCH)
	}
	return exe, asset, nil
}

// assetName mirrors the naming in .github/workflows/release.yml. Windows
// uses .zip, but never reaches here — preflight refuses it first.
func assetName(tag, goos, goarch string) string {
	return fmt.Sprintf("binbash-%s-%s-%s.tar.gz", tag, goos, goarch)
}

func inContainer() bool {
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	return false
}

// writableDir verifies dir accepts new files by creating and removing a
// probe, since checking permission bits can't account for ownership, ACLs,
// or a read-only mount.
func writableDir(dir string) error {
	f, err := os.CreateTemp(dir, ".binbash-write-probe-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

// fetchChecksum downloads the release's SHA256SUMS.txt and returns the
// expected digest for assetName.
func (u *Updater) fetchChecksum(ctx context.Context, rel Release, assetName string) (string, error) {
	sums, ok := rel.FindAsset(checksumsName)
	if !ok {
		return "", fmt.Errorf("release %s has no %s to verify the download against", rel.Tag, checksumsName)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sums.URL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "binbash/"+u.version)

	resp, err := u.api.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not download %s: %w", checksumsName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s returned status %d", checksumsName, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", checksumsName, err)
	}
	return checksumFor(string(body), assetName)
}

// checksumFor extracts name's digest from sha256sum-format contents: one
// "<hex>  <filename>" line per file (a leading '*' on the filename marks
// binary mode and is tolerated).
func checksumFor(sums, name string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if len(fields[0]) != sha256.Size*2 {
			return "", fmt.Errorf("%s has a malformed entry for %s", checksumsName, name)
		}
		return strings.ToLower(fields[0]), nil
	}
	return "", fmt.Errorf("%s has no entry for %s", checksumsName, name)
}

// downloadArchive streams the asset to a temp file, hashing as it goes, and
// returns the file path and hex digest. The caller removes the file.
func (u *Updater) downloadArchive(ctx context.Context, asset Asset) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "binbash/"+u.version)

	resp, err := u.dl.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("could not download %s: %w", asset.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("downloading %s returned status %d", asset.Name, resp.StatusCode)
	}

	f, err := os.CreateTemp("", "binbash-update-*.tar.gz")
	if err != nil {
		return "", "", fmt.Errorf("create temp file: %w", err)
	}

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxArchiveBytes+1))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil && n > maxArchiveBytes {
		err = fmt.Errorf("%s is unexpectedly large (over %d MB)", asset.Name, maxArchiveBytes>>20)
	}
	if err != nil {
		os.Remove(f.Name())
		return "", "", fmt.Errorf("download %s: %w", asset.Name, err)
	}
	return f.Name(), hex.EncodeToString(h.Sum(nil)), nil
}

// extractBinary copies the archive member at path member (e.g.
// "binbash-v0.1.4-linux-arm64/binbash") out of the tar.gz at archivePath to
// dest, executable. Any stale dest from an earlier failed attempt is
// replaced.
func extractBinary(archivePath, member, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("the downloaded file is not a valid archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("the archive does not contain %s — its layout may have changed", member)
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || strings.TrimPrefix(hdr.Name, "./") != member {
			continue
		}

		os.Remove(dest)
		out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
		if err != nil {
			return fmt.Errorf("write new binary: %w", err)
		}
		n, err := io.Copy(out, io.LimitReader(tr, maxBinaryBytes+1))
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err == nil && n > maxBinaryBytes {
			err = fmt.Errorf("binary is unexpectedly large (over %d MB)", maxBinaryBytes>>20)
		}
		if err != nil {
			os.Remove(dest)
			return fmt.Errorf("extract new binary: %w", err)
		}
		return nil
	}
}

// sanityCheck runs the downloaded binary's -version flag and requires it to
// report the expected tag, catching a corrupt, truncated, or wrong-platform
// binary before it replaces a working install.
func sanityCheck(ctx context.Context, path, wantTag string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "-version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("the downloaded binary failed to run (%v) — not installing it", err)
	}
	got := strings.TrimSpace(string(out))
	if !strings.Contains(got, wantTag) {
		return fmt.Errorf("the downloaded binary reports %q instead of %s — not installing it", got, wantTag)
	}
	return nil
}

// swapBinary atomically installs newPath over exePath, keeping the previous
// binary at oldPath for manual rollback. The running process keeps executing
// the old code (its inode stays open) until the caller restarts.
func swapBinary(exePath, newPath, oldPath string) error {
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("could not move the current binary aside: %w", err)
	}
	if err := os.Rename(newPath, exePath); err != nil {
		// Put the original back so the install isn't left without a binary.
		if rbErr := os.Rename(oldPath, exePath); rbErr != nil {
			return fmt.Errorf("installing the new binary failed (%v) and restoring the old one also failed (%v) — restore %s manually from %s", err, rbErr, exePath, oldPath)
		}
		return fmt.Errorf("could not install the new binary (the current one was restored): %w", err)
	}
	return nil
}
