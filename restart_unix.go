//go:build unix

package main

import (
	"log"
	"os"
	"syscall"
)

// reexec replaces the current process with the binary at exePath — after a
// self-update, the freshly installed version. Exec preserves the PID, so a
// supervising systemd unit stays attached, and the working directory and
// environment carry over, so config discovery behaves as if the service had
// just started. Only returns on failure.
func reexec(exePath string) {
	log.Printf("restarting %s", exePath)
	if err := syscall.Exec(exePath, os.Args, os.Environ()); err != nil {
		// Dying here is still recoverable under systemd: Restart=on-failure
		// brings the new binary up. Run by hand, the user restarts manually.
		log.Fatalf("restart: exec: %v", err)
	}
}
