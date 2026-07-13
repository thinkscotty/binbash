//go:build windows

package main

import "log"

// reexec is unreachable on Windows — the updater refuses to self-update
// there — but must exist for the windows release target to compile.
func reexec(exePath string) {
	log.Fatalf("restart: in-place restart is not supported on Windows; start %s manually", exePath)
}
