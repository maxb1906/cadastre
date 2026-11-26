package clean

import "errors"

// Info is the live state of a file system entry, read right before
// deleting it.
type Info struct {
	Size    uint64
	MTime   uint32 // Unix seconds
	FileID  uint64 // NTFS file reference (sequence<<48 | record), inode elsewhere
	Dir     bool
	Reparse bool
	Cloud   bool
}

// Expectation is what an entry must still be to be deleted.
type Expectation struct {
	Size    uint64
	MTime   uint32
	FileID  uint64 // 0: not checked
	Dir     bool
	Reparse bool
}

// FS is the file system access used by the cleaner (Windows API, or os
// elsewhere; a fake in tests).
type FS interface {
	Stat(path string) (Info, error)
	// DeleteVerified deletes path permanently after checking, on the same
	// open handle when the platform allows it, that it still matches exp.
	// Folders must be empty; reparse points are deleted themselves.
	DeleteVerified(path string, exp Expectation) error
	// Recycle sends paths to the recycle bin and returns the errors of the
	// paths that could not be recycled.
	Recycle(paths []string) map[string]error
}

// Errors returned by FS implementations.
var (
	ErrGone        = errors.New("no longer exists")
	ErrChanged     = errors.New("modified since the scan")
	ErrIDChanged   = errors.New("replaced by another file")
	ErrTypeChanged = errors.New("type changed")
	ErrLocked      = errors.New("in use")
	ErrDenied      = errors.New("access denied")
	ErrNotEmpty    = errors.New("folder not empty")
	ErrNoRecycle   = errors.New("recycle bin unavailable")
)

// Check compares the live state with the expectation.
func Check(info Info, exp Expectation) error {
	if info.Dir != exp.Dir || info.Reparse != exp.Reparse {
		return ErrTypeChanged
	}
	if exp.FileID != 0 && info.FileID != exp.FileID {
		return ErrIDChanged
	}
	if !info.Dir && !info.Reparse && (info.Size != exp.Size || info.MTime != exp.MTime) {
		return ErrChanged
	}
	return nil
}

func reasonOf(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrGone):
		return ReasonGone
	case errors.Is(err, ErrChanged):
		return ReasonChanged
	case errors.Is(err, ErrIDChanged):
		return ReasonIDChanged
	case errors.Is(err, ErrTypeChanged):
		return ReasonTypeChanged
	case errors.Is(err, ErrLocked):
		return ReasonLocked
	case errors.Is(err, ErrDenied):
		return ReasonDenied
	case errors.Is(err, ErrNotEmpty):
		return ReasonNotEmpty
	}
	return ReasonFailed
}
