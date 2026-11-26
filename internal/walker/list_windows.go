//go:build windows

package walker

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/ntfs"
	"github.com/maxb1906/cadastre/internal/wtf8"
)

const pathSep = '\\'

const (
	findExInfoBasic       = 1
	findExSearchNameMatch = 0
	findFirstExLargeFetch = 2

	fileAttrRecallOnOpen       = 0x00040000
	fileAttrRecallOnDataAccess = 0x00400000
)

var (
	modkernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procFindFirstFileExW       = modkernel32.NewProc("FindFirstFileExW")
	procGetCompressedFileSizeW = modkernel32.NewProc("GetCompressedFileSizeW")
)

func joinPath(dir, name string) string {
	if strings.HasSuffix(dir, `\`) {
		return dir + name
	}
	return dir + `\` + name
}

func isDenied(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD)
}

func deviceOf(string) uint64 { return 0 }

// LongPath converts a path to its \\?\ form so that paths longer than
// MAX_PATH work.
func LongPath(p string) string {
	switch {
	case strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`):
		return p
	case strings.HasPrefix(p, `\\`):
		return `\\?\UNC\` + p[2:]
	default:
		return `\\?\` + p
	}
}

func utf16Ptr(s string) *uint16 {
	u := wtf8.ToUTF16(s)
	u = append(u, 0)
	return &u[0]
}

func listDir(path string, out []dirEntry, cluster uint32, _ uint64) ([]dirEntry, error) {
	pattern := utf16Ptr(LongPath(joinPath(path, "*")))
	var data windows.Win32finddata
	h, _, errno := procFindFirstFileExW.Call(uintptr(unsafe.Pointer(pattern)), findExInfoBasic,
		uintptr(unsafe.Pointer(&data)), findExSearchNameMatch, 0, findFirstExLargeFetch)
	if windows.Handle(h) == windows.InvalidHandle {
		if errno == windows.ERROR_FILE_NOT_FOUND {
			return out, nil
		}
		return out, errno
	}
	defer windows.FindClose(windows.Handle(h))
	for {
		n := 0
		for n < len(data.FileName) && data.FileName[n] != 0 {
			n++
		}
		name := wtf8.FromUTF16(data.FileName[:n])
		if name != "." && name != ".." {
			out = append(out, entryFrom(path, name, &data, cluster))
		}
		if err := windows.FindNextFile(windows.Handle(h), &data); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				return out, nil
			}
			return out, err
		}
	}
}

func entryFrom(dir, name string, d *windows.Win32finddata, cluster uint32) dirEntry {
	a := d.FileAttributes
	e := dirEntry{name: name, attr: a & 0xFFFF}
	ft := int64(d.LastWriteTime.HighDateTime)<<32 | int64(d.LastWriteTime.LowDateTime)
	e.mtime = index.FileTimeToUnix(ft)
	reparse := a&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	if reparse && ntfs.IsCloudReparseTag(d.Reserved0) || a&(fileAttrRecallOnOpen|fileAttrRecallOnDataAccess) != 0 {
		e.attr |= index.FlagCloud
	}
	if a&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		e.attr |= index.FlagDir
		// Never follow junctions, symlinks or mount points.
		e.descend = !reparse
		return e
	}
	e.size = uint64(d.FileSizeHigh)<<32 | uint64(d.FileSizeLow)
	switch {
	case e.attr&index.FlagCloud != 0 && a&fileAttrRecallOnDataAccess != 0:
		e.alloc = 0 // dehydrated placeholder: data lives in the cloud
	case a&(windows.FILE_ATTRIBUTE_COMPRESSED|windows.FILE_ATTRIBUTE_SPARSE_FILE) != 0 || reparse && d.Reserved0 == ntfs.ReparseTagWOF:
		e.alloc = compressedSize(joinPath(dir, name), e.size, cluster)
		if reparse && d.Reserved0 == ntfs.ReparseTagWOF {
			e.attr |= index.FlagWOF
		}
	default:
		e.alloc = roundUp(e.size, cluster)
	}
	return e
}

func compressedSize(path string, size uint64, cluster uint32) uint64 {
	var high uint32
	low, _, err := procGetCompressedFileSizeW.Call(uintptr(unsafe.Pointer(utf16Ptr(LongPath(path)))), uintptr(unsafe.Pointer(&high)))
	if uint32(low) == 0xFFFFFFFF && err != windows.ERROR_SUCCESS {
		return roundUp(size, cluster)
	}
	return roundUp(uint64(high)<<32|uint64(uint32(low)), cluster)
}
