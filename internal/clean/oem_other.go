//go:build !windows

package clean

import "os"

func decodeOEM(b []byte) string { return string(b) }

func osMkdirAll(d string) error { return os.MkdirAll(d, 0o755) }
