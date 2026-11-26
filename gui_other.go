//go:build !windows

package main

import "os"

func preflight() bool { return true }

func fatal(title, msg string) { os.Stderr.WriteString(title + ": " + msg + "\n") }
