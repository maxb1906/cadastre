//go:build !windows

// Package console reattaches a GUI-subsystem executable to the console of
// its parent process (Windows only; no-op elsewhere).
package console

// Attach is a no-op outside Windows.
func Attach() {}

// Detached reports whether the process has no console.
func Detached() bool { return false }
