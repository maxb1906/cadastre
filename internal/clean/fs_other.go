//go:build !windows

package clean

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// OSFS implements FS with the os package (non-Windows: tests, Linux CLI).
type OSFS struct{}

func (OSFS) Stat(path string) (Info, error) {
	var st syscall.Stat_t
	if err := syscall.Lstat(path, &st); err != nil {
		return Info{}, mapErr(err)
	}
	mode := st.Mode & syscall.S_IFMT
	return Info{
		Size:    uint64(st.Size),
		MTime:   uint32(max(st.Mtim.Sec, 0)),
		FileID:  st.Ino,
		Dir:     mode == syscall.S_IFDIR,
		Reparse: mode == syscall.S_IFLNK,
	}, nil
}

func (f OSFS) DeleteVerified(path string, exp Expectation) error {
	info, err := f.Stat(path)
	if err != nil {
		return err
	}
	if err := Check(info, exp); err != nil {
		return err
	}
	if info.Dir {
		return mapErr(syscall.Rmdir(path))
	}
	return mapErr(syscall.Unlink(path)) // removes a symlink itself, not its target
}

func (OSFS) Recycle(paths []string) map[string]error {
	out := map[string]error{}
	for _, p := range paths {
		out[p] = ErrNoRecycle
	}
	return out
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return ErrGone
	case errors.Is(err, fs.ErrPermission):
		return ErrDenied
	case errors.Is(err, syscall.ENOTEMPTY), errors.Is(err, syscall.EEXIST):
		return ErrNotEmpty
	case errors.Is(err, syscall.EBUSY):
		return ErrLocked
	}
	return err
}

var _ = os.Remove
