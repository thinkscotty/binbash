//go:build windows

package main

// Windows has no uid to compare, and the failure this guards against -- a
// root-run command leaving files the service user can't write -- doesn't arise
// on a single-user desktop install. Reporting "unknown" skips the check.
func fileOwner(path string) (uid int, ok bool) { return 0, false }

func currentUID() int { return -1 }
