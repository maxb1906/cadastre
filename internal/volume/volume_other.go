//go:build !windows

package volume

import (
	"os"
	"path/filepath"
	"syscall"
)

// OpenRaw is only available on Windows; use OpenImage for disk images.
func OpenRaw(root string) (Raw, error) { return nil, ErrNotSupported }

// IsAdmin reports whether the process runs as root.
func IsAdmin() bool { return os.Geteuid() == 0 }

// List returns the root file system only on non-Windows platforms.
func List() ([]Info, error) {
	i, err := Stat("/")
	if err != nil {
		return nil, err
	}
	return []Info{i}, nil
}

// Stat returns usage information about the file system containing root.
func Stat(root string) (Info, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return Info{}, err
	}
	abs, _ := filepath.Abs(root)
	return Info{
		Root:       abs,
		FileSystem: "unix",
		Kind:       "fixed",
		Total:      st.Blocks * uint64(st.Bsize),
		Free:       st.Bavail * uint64(st.Bsize),
	}, nil
}

// ClusterSize returns the block size of the file system containing root.
func ClusterSize(root string) uint32 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil || st.Bsize <= 0 {
		return 4096
	}
	return uint32(st.Bsize)
}

// GUIDPath is Windows-only.
func GUIDPath(root string) string { return "" }
