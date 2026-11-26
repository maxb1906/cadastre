//go:build !windows

package engine

import "syscall"

func deviceID(p string) uint64 {
	var st syscall.Stat_t
	if syscall.Stat(p, &st) != nil {
		return 0
	}
	return uint64(st.Dev)
}
