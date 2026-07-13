package main

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/thinkscotty/binbash/internal/auth"
	"github.com/thinkscotty/binbash/internal/config"
	"github.com/thinkscotty/binbash/internal/db"
	"golang.org/x/term"
)

// resetPassword implements `binbash -reset-password`: the way back in when the
// password is forgotten. binbash has no usernames and no email address, so
// there is nobody to send a reset link to; the only remaining authority is
// being able to reach the database file on the server, which is exactly what
// this command requires.
//
// It writes to the database directly, with no server running. That is the
// important constraint, and the reason for the ownership check and the restart
// notice below: a running binbash holds the password hash and session key in
// memory and would go on honouring the old password until restarted.
func resetPassword(cfg *config.Config) error {
	if err := checkDBOwnership(cfg.DBPath); err != nil {
		return err
	}

	fmt.Println("Resetting the binbash login password.")
	fmt.Printf("Database: %s\n\n", cfg.DBPath)

	password, err := readNewPassword()
	if err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	if err := auth.ResetPassword(database, password); err != nil {
		return err
	}

	fmt.Println("\nPassword reset. Every signed-in device has been signed out.")
	fmt.Println("\nRestart binbash for this to take effect — until you do, a running")
	fmt.Println("copy still has the old password held in memory:")
	fmt.Println("\n    sudo systemctl restart binbash")
	return nil
}

// checkDBOwnership refuses to touch a database belonging to another user.
//
// The trap this exists for: on a systemd install the service runs as `binbash`
// and owns /opt/binbash/data. Someone locked out will very reasonably reach for
// `sudo ./binbash -reset-password` -- and SQLite would then create root-owned
// -wal and -shm files alongside the database, which the service user cannot
// write. The password reset would appear to work and the app would then fail to
// start, which is a far worse place to be than merely locked out.
func checkDBOwnership(path string) error {
	owner, ok := fileOwner(path)
	if !ok {
		return nil // no database yet, or a platform without uids
	}
	if owner == currentUID() {
		return nil
	}

	name := strconv.Itoa(owner)
	if u, err := user.LookupId(name); err == nil {
		name = u.Username
	}

	exe, err := os.Executable()
	if err != nil {
		exe = "./binbash"
	}

	return fmt.Errorf(
		"the database at %s belongs to the user %q, but this command is running as %q.\n\n"+
			"Resetting the password as the wrong user would leave files that binbash itself\n"+
			"can no longer write, so it has not been touched. Run it as %[2]q instead:\n\n"+
			"    sudo -u %[2]s %s -reset-password",
		path, name, currentUsername(), exe)
}

func currentUsername() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return strconv.Itoa(currentUID())
}

// readNewPassword prompts twice on a terminal (a typo here would lock the user
// straight back out, which is the one outcome this command exists to prevent),
// and falls back to reading a single line when stdin is piped, so the command
// can still be scripted.
func readNewPassword() (string, error) {
	stdin := int(os.Stdin.Fd())
	if !term.IsTerminal(stdin) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		password := strings.TrimRight(line, "\r\n")
		if problem := auth.ValidatePassword(password); problem != "" {
			return "", fmt.Errorf("%s", problem)
		}
		return password, nil
	}

	fmt.Printf("New password (at least %d characters): ", auth.MinPasswordLen)
	first, err := term.ReadPassword(stdin)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}

	if problem := auth.ValidatePassword(string(first)); problem != "" {
		return "", fmt.Errorf("%s", problem)
	}

	fmt.Print("Confirm new password: ")
	second, err := term.ReadPassword(stdin)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read confirmation: %w", err)
	}

	if string(first) != string(second) {
		return "", fmt.Errorf("the two passwords didn't match — nothing was changed")
	}
	return string(first), nil
}
