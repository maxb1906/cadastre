// Package volume gives access to volumes: enumeration, usage, privileges and
// raw reading of NTFS volumes (Windows) or NTFS disk images (any platform).
package volume

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/maxb1906/cadastre/internal/ntfs"
)

// Info describes a mounted volume.
type Info struct {
	Root       string // "C:\" on Windows, mount point elsewhere
	Label      string
	FileSystem string // "NTFS", "ReFS", "FAT32"...
	Kind       string // "fixed", "removable", "network", "cdrom", "ram", "unknown"
	Total      uint64
	Free       uint64
}

// Used returns the space in use on the volume.
func (i Info) Used() uint64 {
	if i.Free > i.Total {
		return 0
	}
	return i.Total - i.Free
}

// Raw is a read-only raw view of an NTFS volume.
type Raw interface {
	io.ReaderAt
	io.Closer
	// NTFSInfo returns the geometry needed to read the MFT.
	NTFSInfo() (ntfs.VolumeInfo, error)
}

// ErrNotSupported is returned when an operation is not available on this
// platform or for this kind of volume.
var ErrNotSupported = errors.New("volume: not supported")

// imageRaw reads an NTFS volume stored in a regular file (disk image).
type imageRaw struct{ f *os.File }

// OpenImage opens an NTFS volume image (a file containing the volume starting
// at its boot sector).
func OpenImage(path string) (Raw, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &imageRaw{f: f}, nil
}

func (r *imageRaw) ReadAt(p []byte, off int64) (int, error) { return r.f.ReadAt(p, off) }
func (r *imageRaw) Close() error                             { return r.f.Close() }
func (r *imageRaw) NTFSInfo() (ntfs.VolumeInfo, error) {
	b := make([]byte, 512)
	if _, err := r.f.ReadAt(b, 0); err != nil {
		return ntfs.VolumeInfo{}, fmt.Errorf("volume: reading boot sector: %w", err)
	}
	return ntfs.ParseBootSector(b)
}

// alignedReader turns a device that only accepts sector-aligned reads into a
// general io.ReaderAt by reading through an aligned bounce buffer.
type alignedReader struct {
	sector int64
	read   func(p []byte, off int64) (int, error) // aligned reads only
}

func (a *alignedReader) ReadAt(p []byte, off int64) (int, error) {
	s := a.sector
	if off%s == 0 && int64(len(p))%s == 0 {
		return a.readFull(p, off)
	}
	start := off - off%s
	end := off + int64(len(p))
	if r := end % s; r != 0 {
		end += s - r
	}
	tmp := make([]byte, end-start)
	n, err := a.readFull(tmp, start)
	avail := n - int(off-start)
	if avail < 0 {
		avail = 0
	}
	if avail > len(p) {
		avail = len(p)
	}
	copy(p, tmp[off-start:off-start+int64(avail)])
	if avail == len(p) {
		return avail, nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return avail, err
}

func (a *alignedReader) readFull(p []byte, off int64) (int, error) {
	done := 0
	for done < len(p) {
		n, err := a.read(p[done:], off+int64(done))
		done += n
		if err != nil {
			return done, err
		}
		if n == 0 {
			return done, io.ErrUnexpectedEOF
		}
	}
	return done, nil
}
