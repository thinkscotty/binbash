package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// The failure this whole file exists to prevent: on a systemd install the
// service runs as `binbash` and owns /opt/binbash/data, but an operator
// test-running `sudo ./binbash` creates the data directory, the database, and
// its -wal/-shm sidecars as root. Everything looks fine until systemd next
// starts the service, which then dies with SQLite's "unable to open database
// file (14)" -- an error that says nothing about ownership and sends people
// hunting through their unit file instead.
//
// So both entry points that touch the database check first, and say plainly
// which file belongs to whom.

// dataPaths lists the files whose ownership decides whether binbash can use
// its database. The directory comes first and matters most: a data directory
// owned by another user at mode 0700 cannot even be traversed, which makes the
// database inside it unstattable and would leave a file-only check reporting
// that all is well.
//
// The sidecars are listed because SQLite creates them on first write, not at
// open: a root-run binbash against a binbash-owned database still leaves
// root-owned -wal and -shm files behind, and the service breaks on its next
// write rather than at startup.
func dataPaths(dbPath string) []string {
	return []string{
		filepath.Dir(dbPath),
		dbPath,
		dbPath + "-wal",
		dbPath + "-shm",
	}
}

// foreignOwner returns the first of binbash's data paths that belongs to a
// user other than the one running this process, along with that user's name.
//
// A path that doesn't exist yet, or a platform without uids, is not a problem:
// fileOwner reports !ok and it is skipped. First run and Windows therefore pass
// straight through.
func foreignOwner(dbPath string) (path, owner string, found bool) {
	me := currentUID()
	for _, p := range dataPaths(dbPath) {
		uid, ok := fileOwner(p)
		if !ok || uid == me {
			continue
		}
		return p, usernameFor(uid), true
	}
	return "", "", false
}

// checkDataOwnership stops the server before it opens a database it cannot
// actually use, and tells the operator how to hand the files back.
func checkDataOwnership(dbPath string) error {
	path, owner, found := foreignOwner(dbPath)
	if !found {
		return nil
	}

	// Recursive, and on the directory rather than the offending path, because
	// the cause is always the same -- a root-run binbash -- and it never leaves
	// exactly one file behind. Fixing the whole directory in one command also
	// spares the operator a second round of this error for the sidecar it
	// didn't mention.
	dir := filepath.Dir(dbPath)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}

	return fmt.Errorf(
		"%s belongs to the user %q, but binbash is running as %q, so it cannot open its database.\n\n"+
			"This usually means binbash was started once with sudo, which created these files as root.\n"+
			"Hand them back to the user binbash runs as:\n\n"+
			"    sudo chown -R %s %s",
		path, owner, currentUsername(), currentOwnerSpec(), dir)
}

// checkResetOwnership refuses to reset the password against a database
// belonging to another user.
//
// Someone locked out will very reasonably reach for `sudo ./binbash
// -reset-password`. Left alone, that would write root-owned sidecars next to
// the service user's database: the reset would report success and the app would
// then fail to start, which is a considerably worse place to be than merely
// locked out.
func checkResetOwnership(dbPath string) error {
	path, owner, found := foreignOwner(dbPath)
	if !found {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		exe = "./binbash"
	}

	// Every verb is indexed explicitly: Printf numbering continues from the
	// last explicit index rather than resuming where the implicit run left off,
	// so a bare %s after %[2]s would silently reach for the wrong argument.
	return fmt.Errorf(
		"%[1]s belongs to the user %[2]q, but this command is running as %[3]q.\n\n"+
			"Resetting the password as the wrong user would leave files that binbash itself\n"+
			"can no longer write, so nothing has been touched. Run it as %[2]q instead:\n\n"+
			"    sudo -u %[2]s %[4]s -reset-password",
		path, owner, currentUsername(), exe)
}

// usernameFor turns a uid into the name an operator would recognise, falling
// back to the number when the user isn't in the password database.
func usernameFor(uid int) string {
	if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
		return u.Username
	}
	return strconv.Itoa(uid)
}

func currentUsername() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return strconv.Itoa(currentUID())
}

// currentOwnerSpec is this process's identity in chown's user:group form.
// Falling back to the bare username is harmless -- chown then leaves the group
// alone, which is the right outcome anyway.
func currentOwnerSpec() string {
	u, err := user.Current()
	if err != nil {
		return currentUsername()
	}
	if g, err := user.LookupGroupId(u.Gid); err == nil {
		return u.Username + ":" + g.Name
	}
	return u.Username
}
