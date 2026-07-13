package db

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRestrictPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are meaningless on windows")
	}

	tests := []struct {
		name  string
		start os.FileMode
		want  os.FileMode
	}{
		{"world-readable is tightened", 0o644, 0o600},
		{"group-readable is tightened", 0o640, 0o600},
		{"world-writable is tightened", 0o666, 0o600},
		{"already private is left alone", 0o600, 0o600},
		// Owner bits are preserved rather than forced to 0600, so this can only
		// ever tighten a file. That is what makes it safe to run on every
		// startup against files the operator may have deliberately set.
		{"read-only for the owner is not loosened", 0o444, 0o400},
		{"executable owner bit survives", 0o755, 0o700},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "binbash.db")
			if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := os.Chmod(path, tt.start); err != nil {
				t.Fatalf("chmod: %v", err)
			}

			if err := RestrictPermissions(path); err != nil {
				t.Fatalf("RestrictPermissions: %v", err)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if got := info.Mode().Perm(); got != tt.want {
				t.Errorf("mode = %#o, want %#o", got, tt.want)
			}
		})
	}
}

// TestRestrictPermissionsCoversSidecars pins that the WAL and shared-memory
// files are tightened too. They hold recently written database pages, so a
// world-readable -wal leaks exactly the same secrets the database does --
// including the session-signing key, which is a skeleton key for the whole app.
func TestRestrictPermissionsCoversSidecars(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are meaningless on windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "binbash.db")
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	if err := RestrictPermissions(path); err != nil {
		t.Fatalf("RestrictPermissions: %v", err)
	}

	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode = %#o, want 0600", filepath.Base(p), got)
		}
	}
}

func TestRestrictPermissionsMissingFileIsNotAnError(t *testing.T) {
	if err := RestrictPermissions(filepath.Join(t.TempDir(), "nope.db")); err != nil {
		t.Errorf("RestrictPermissions on a missing path: %v", err)
	}
}

// TestOpenSecuresExistingInstall is the upgrade path: a database and directory
// created by an older binbash are left world-readable at 0644/0755, and simply
// starting the new version has to fix them. If this regresses, every existing
// install silently keeps leaking its session key and nobody finds out.
func TestOpenSecuresExistingInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are meaningless on windows")
	}

	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "binbash.db")

	// Create the database the way an older binbash would have, then loosen it
	// to the permissions SQLite's defaults used to leave behind.
	database, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	database.Close()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod db: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}

	// Now start "the new version" against it.
	database, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer database.Close()

	dbInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	if got := dbInfo.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("database still readable by others: mode %#o", got)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("data directory still accessible to others: mode %#o", got)
	}
}

// TestOpenCreatesPrivateDatabase covers the fresh-install path: SQLite's own
// default would leave the new file at 0644.
func TestOpenCreatesPrivateDatabase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are meaningless on windows")
	}

	path := filepath.Join(t.TempDir(), "data", "binbash.db")
	database, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	for _, p := range []string{path, filepath.Dir(path)} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if got := info.Mode().Perm(); got&0o077 != 0 {
			t.Errorf("%s is accessible to other users: mode %#o", filepath.Base(p), got)
		}
	}
}
