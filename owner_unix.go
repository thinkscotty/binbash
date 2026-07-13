//go:build unix

package main

import (
	"os"
	"syscall"
)

// fileOwner returns the uid owning path. ok is false when the file doesn't
// exist or the platform won't say.
func fileOwner(path string) (uid int, ok bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}

func currentUID() int { return os.Geteuid() }
