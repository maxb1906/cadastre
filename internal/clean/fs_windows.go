//go:build windows

package clean

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/wtf8"
)

// OSFS implements FS with the Windows API.
type OSFS struct{}

const (
	fileAttrRecallOnOpen       = 0x00040000
	fileAttrRecallOnDataAccess = 0x00400000

	fileDispositionInfo   = 4
	fileDispositionInfoEx = 21

	fileDispositionFlagDelete                  = 0x1
	fileDispositionFlagPosixSemantics          = 0x2
	fileDispositionFlagIgnoreReadonlyAttribute = 0x10
)

var procSetFileInformationByHandle = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetFileInformationByHandle")

// longPath converts to the \\?\ form so that paths beyond MAX_PATH work.
func longPath(p string) string {
	switch {
	case strings.HasPrefix(p, `\\?\`):
		return p
	case strings.HasPrefix(p, `\\`):
		return `\\?\UNC\` + p[2:]
	default:
		return `\\?\` + p
	}
}

func utf16(p string) *uint16 {
	u := append(wtf8.ToUTF16(p), 0)
	return &u[0]
}

func open(path string, access uint32) (windows.Handle, error) {
	h, err := windows.CreateFile(utf16(longPath(path)), access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	return h, mapErr(err)
}

func infoOf(h windows.Handle) (Info, error) {
	var d windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &d); err != nil {
		return Info{}, mapErr(err)
	}
	ft := int64(d.LastWriteTime.HighDateTime)<<32 | int64(d.LastWriteTime.LowDateTime)
	return Info{
		Size:    uint64(d.FileSizeHigh)<<32 | uint64(d.FileSizeLow),
		MTime:   index.FileTimeToUnix(ft),
		FileID:  uint64(d.FileIndexHigh)<<32 | uint64(d.FileIndexLow),
		Dir:     d.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0,
		Reparse: d.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0,
		Cloud:   d.FileAttributes&(fileAttrRecallOnOpen|fileAttrRecallOnDataAccess) != 0,
	}, nil
}

func (OSFS) Stat(path string) (Info, error) {
	h, err := open(path, windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return Info{}, err
	}
	defer windows.CloseHandle(h)
	return infoOf(h)
}

// DeleteVerified opens the entry once (without following reparse points),
// checks it on that very handle, then marks that handle for deletion: the
// file that is checked is the file that is deleted.
func (OSFS) DeleteVerified(path string, exp Expectation) error {
	h, err := open(path, windows.DELETE|windows.FILE_READ_ATTRIBUTES|windows.FILE_WRITE_ATTRIBUTES)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	info, err := infoOf(h)
	if err != nil {
		return err
	}
	if err := Check(info, exp); err != nil {
		return err
	}
	// Windows 10 1809+: POSIX semantics, read-only attribute ignored.
	ex := uint32(fileDispositionFlagDelete | fileDispositionFlagPosixSemantics | fileDispositionFlagIgnoreReadonlyAttribute)
	r, _, e := procSetFileInformationByHandle.Call(uintptr(h), fileDispositionInfoEx, uintptr(unsafe.Pointer(&ex)), 4)
	if r != 0 {
		return nil
	}
	if errors.Is(e, windows.ERROR_DIR_NOT_EMPTY) {
		return ErrNotEmpty
	}
	// Older systems: clear read-only, then classic disposition.
	var d windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &d) == nil && d.FileAttributes&windows.FILE_ATTRIBUTE_READONLY != 0 {
		basic := struct {
			Creation, Access, Write, Change int64
			Attributes                      uint32
			_                               uint32
		}{Attributes: d.FileAttributes &^ windows.FILE_ATTRIBUTE_READONLY}
		procSetFileInformationByHandle.Call(uintptr(h), 0 /* FileBasicInfo */, uintptr(unsafe.Pointer(&basic)), unsafe.Sizeof(basic))
	}
	del := byte(1)
	r, _, e = procSetFileInformationByHandle.Call(uintptr(h), fileDispositionInfo, uintptr(unsafe.Pointer(&del)), 1)
	if r == 0 {
		return mapErr(e)
	}
	return nil
}

func (OSFS) Recycle(paths []string) map[string]error {
	return recycle(paths)
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, windows.ERROR_FILE_NOT_FOUND), errors.Is(err, windows.ERROR_PATH_NOT_FOUND):
		return ErrGone
	case errors.Is(err, windows.ERROR_ACCESS_DENIED), errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD):
		return ErrDenied
	case errors.Is(err, windows.ERROR_SHARING_VIOLATION), errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return ErrLocked
	case errors.Is(err, windows.ERROR_DIR_NOT_EMPTY):
		return ErrNotEmpty
	}
	return err
}
