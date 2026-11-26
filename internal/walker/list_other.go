//go:build !windows

package walker

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/maxb1906/cadastre/internal/index"
)

const pathSep = '/'

func joinPath(dir, name string) string { return filepath.Join(dir, name) }

func isDenied(err error) bool { return errors.Is(err, fs.ErrPermission) }

func deviceOf(path string) uint64 {
	var st syscall.Stat_t
	if err := syscall.Lstat(path, &st); err != nil {
		return 0
	}
	return uint64(st.Dev)
}

func listDir(path string, out []dirEntry, cluster uint32, rootDev uint64) ([]dirEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	var firstErr error
	for {
		names, err := f.Readdirnames(1024)
		for _, name := range names {
			var st syscall.Stat_t
			if err := syscall.Lstat(filepath.Join(path, name), &st); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			e := dirEntry{name: name, mtime: uint32(max(st.Mtim.Sec, 0)), inode: st.Ino, nlink: uint32(st.Nlink)}
			switch st.Mode & syscall.S_IFMT {
			case syscall.S_IFDIR:
				e.attr = index.FlagDir | index.AttrDirectory
				e.descend = uint64(st.Dev) == rootDev
			case syscall.S_IFLNK:
				e.attr = index.AttrReparsePoint
				e.size = uint64(st.Size)
			case syscall.S_IFREG:
				e.size = uint64(st.Size)
				e.alloc = uint64(st.Blocks) * 512
				if e.alloc < e.size && e.size-e.alloc >= uint64(cluster) {
					e.attr |= index.AttrSparse
				}
			default:
				e.attr = index.AttrSystem // device, socket, fifo
			}
			if len(name) > 0 && name[0] == '.' {
				e.attr |= index.AttrHidden
			}
			out = append(out, e)
		}
		if err != nil {
			break
		}
	}
	return out, firstErr
}
