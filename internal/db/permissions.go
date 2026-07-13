package db

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"runtime"
)

// groupAndWorld are the permission bits that let anyone other than the owner
// near a file.
const groupAndWorld fs.FileMode = 0o077

// RestrictPermissions strips group and world access from path, and -- when
// path is a SQLite database -- from its -wal and -shm sidecars.
//
// This matters more than it looks. The database stores the session-signing key
// in the clear (it has to: it signs every session cookie with it). Anyone who
// can read the file can therefore mint themselves a valid session cookie and
// walk straight in, skipping the password, the login throttle, and fail2ban
// alike. That makes the database file a better skeleton key than the password
// ever was, and it is why the file's permissions -- not the secrecy of the
// password in the config -- are the control that actually protects binbash on
// a machine with more than one user or service on it.
//
// Only the owner's own bits are preserved; the group and world bits are
// cleared. That means this can tighten a file but never loosen one, so it is
// safe to run unconditionally at every startup -- which is deliberate, since
// that is what quietly fixes installs created before binbash did this, where
// SQLite's default left the database world-readable at 0644.
func RestrictPermissions(paths ...string) error {
	// Unix permission bits don't mean anything on Windows: os.Chmod there can
	// only toggle the read-only flag, and Stat always reports 0666 or 0444. We
	// would re-chmod and re-log on every single startup and achieve nothing.
	// Windows installs are single-user desktop ones anyway.
	if runtime.GOOS == "windows" {
		return nil
	}

	for _, path := range paths {
		// SQLite gives a new -wal/-shm the same mode as the main database file,
		// so tightening the database first means fresh sidecars are born
		// private. The sidecars are listed anyway to catch ones left behind by
		// an older, looser install -- and they hold recently written pages, so
		// a readable -wal leaks the same secrets the database does.
		for _, p := range []string{path, path + "-wal", path + "-shm"} {
			if err := restrict(p); err != nil {
				return err
			}
		}
	}
	return nil
}

func restrict(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		// Sidecars only exist while the database is open, and a caller may
		// hand us a path that isn't a database at all. Neither is an error.
		return nil
	}
	if err != nil {
		return err
	}

	perm := info.Mode().Perm()
	if perm&groupAndWorld == 0 {
		return nil // already private to its owner
	}

	if err := os.Chmod(path, perm&^groupAndWorld); err != nil {
		return err
	}
	// Worth saying out loud: on an upgrade this is binbash silently changing
	// permissions on the operator's files, and they should be able to see why.
	log.Printf("secured %s (was mode %#o, readable by other users on this machine)", path, perm)
	return nil
}
