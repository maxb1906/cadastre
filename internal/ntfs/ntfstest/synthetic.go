package ntfstest

import (
	"encoding/binary"
	"io"
	"strconv"

	"github.com/maxb1906/cadastre/internal/ntfs"
)

// Synthetic is a virtual NTFS volume whose MFT records are generated on the
// fly by ReadAt, so that benchmarks can scan millions of records without
// storing them. Records 24+ form a tree: one directory every DirEvery
// records, each file living in one of the directories created before it.
type Synthetic struct {
	Records  uint64
	DirEvery uint64
	Info     ntfs.VolumeInfo

	rec0      []byte
	root      []byte
	fileTmpl  []byte
	dirTmpl   []byte
	nameOff   int // offset of the 10-digit name suffix in templates
	parentOff int // offset of the parent reference in $FILE_NAME
	sizeOff   int // offset of the real size of the non-resident $DATA
	allocOff  int
}

const (
	synthMftLcn = 16
	synthDigits = 10
)

// NewSynthetic returns a generator for n records.
func NewSynthetic(n uint64) *Synthetic {
	s := &Synthetic{Records: n, DirEvery: 20}
	s.Info = ntfs.VolumeInfo{BytesPerSector: 512, BytesPerCluster: 4096, BytesPerRecord: 1024,
		MftStartLcn: synthMftLcn, MftValidDataLength: n * 1024}
	s.Info.TotalClusters = synthMftLcn + n/4 + 1<<20
	clusters := (n*1024 + 4095) / 4096
	s.rec0 = Record{Seq: 1, Flags: ntfs.RecordInUse, Attrs: []Attr{
		StdInfo(1, 1, 6),
		FileName(5, 5, "$MFT", ntfs.NamespaceWin32DOS),
		NonResidentData("", []ntfs.Run{{LCN: synthMftLcn, Length: clusters}}, n*1024, clusters*4096),
	}}.Bytes()
	s.root = Record{Seq: 5, Flags: ntfs.RecordInUse | ntfs.RecordDirectory, Attrs: []Attr{
		StdInfo(1, 1, 0x16), FileName(5, 5, ".", ntfs.NamespaceWin32DOS),
	}}.Bytes()
	name := "file-" + string(make([]byte, synthDigits)) + ".dat"
	s.fileTmpl = Record{Seq: 1, Flags: ntfs.RecordInUse, Attrs: []Attr{
		StdInfo(132000000000000000, 133000000000000000, 0x20),
		FileName(5, 5, name, ntfs.NamespaceWin32),
		NonResidentData("", []ntfs.Run{{LCN: 1 << 20, Length: 1}}, 1, 4096),
	}}.Bytes()
	s.dirTmpl = Record{Seq: 1, Flags: ntfs.RecordInUse | ntfs.RecordDirectory, Attrs: []Attr{
		StdInfo(132000000000000000, 133000000000000000, 0x10),
		FileName(5, 5, "dir--"+string(make([]byte, synthDigits))+".dat", ntfs.NamespaceWin32),
	}}.Bytes()
	// Locate the patchable fields in the (unprotected) layout. The templates
	// are stored with fixups applied: undo them to patch, redo after.
	tmp := append([]byte(nil), s.fileTmpl...)
	if err := ntfs.ApplyFixups(tmp); err != nil {
		panic(err)
	}
	le := binary.LittleEndian
	off := int(le.Uint16(tmp[0x14:]))
	for {
		typ := le.Uint32(tmp[off:])
		alen := int(le.Uint32(tmp[off+4:]))
		switch typ {
		case ntfs.AttrFileName:
			voff := off + int(le.Uint16(tmp[off+0x14:]))
			s.parentOff = voff
			s.nameOff = voff + 0x42 + 2*5
		case ntfs.AttrData:
			s.sizeOff = off + 0x30
			s.allocOff = off + 0x28
		}
		if typ == ntfs.AttrEnd || alen == 0 {
			break
		}
		off += alen
	}
	if err := ntfs.ApplyFixups(s.fileTmpl); err != nil {
		panic(err)
	}
	if err := ntfs.ApplyFixups(s.dirTmpl); err != nil {
		panic(err)
	}
	return s
}

func (s *Synthetic) isDir(r uint64) bool { return r >= 24 && (r-24)%s.DirEvery == 0 }

// parentOf returns the directory holding record r: a pseudo-random earlier
// directory, or the root.
func (s *Synthetic) parentOf(r uint64) uint64 {
	k := (r - 24) / s.DirEvery // directories created before r
	if k == 0 {
		return ntfs.RecRoot
	}
	h := r * 0x9E3779B97F4A7C15
	d := (h >> 20) % k
	if s.isDir(r) && d >= k {
		d = k - 1
	}
	return 24 + d*s.DirEvery
}

// FileSize returns the logical size given to file record r.
func (s *Synthetic) FileSize(r uint64) uint64 { return (r*2654435761)%(1<<20) + 1 }

func (s *Synthetic) record(dst []byte, r uint64) {
	switch {
	case r == 0:
		copy(dst, s.rec0)
		return
	case r == ntfs.RecRoot:
		copy(dst, s.root)
		return
	case r < 24:
		clear(dst)
		return
	}
	le := binary.LittleEndian
	if s.isDir(r) {
		copy(dst, s.dirTmpl)
	} else {
		copy(dst, s.fileTmpl)
		size := s.FileSize(r)
		le.PutUint64(dst[s.sizeOff:], size)
		le.PutUint64(dst[s.allocOff:], (size+4095)&^4095)
	}
	parent, seq := s.parentOf(r), uint64(1)
	if parent == ntfs.RecRoot {
		seq = ntfs.RecRoot
	}
	le.PutUint64(dst[s.parentOff:], parent|seq<<48)
	var digits [synthDigits]byte
	v := strconv.AppendUint(digits[:0], r, 10)
	pad := synthDigits - len(v)
	for k := 0; k < synthDigits; k++ {
		c := byte('0')
		if k >= pad {
			c = v[k-pad]
		}
		dst[s.nameOff+2*k] = c
		dst[s.nameOff+2*k+1] = 0
	}
	Protect(dst, uint16(r)|1)
}

// ReadAt serves the MFT region of the virtual volume; everything else reads
// as zeros.
func (s *Synthetic) ReadAt(p []byte, off int64) (int, error) {
	mftStart := int64(synthMftLcn * 4096)
	end := mftStart + int64(s.Records*1024)
	clear(p)
	for done := 0; done < len(p); {
		pos := off + int64(done)
		if pos < mftStart || pos >= end {
			done++
			continue
		}
		rel := pos - mftStart
		r := uint64(rel / 1024)
		in := int(rel % 1024)
		var tmp [1024]byte
		buf := tmp[:]
		if in == 0 && len(p)-done >= 1024 {
			buf = p[done : done+1024]
		}
		s.record(buf, r)
		n := copy(p[done:], buf[in:])
		done += n
	}
	if off+int64(len(p)) > end+1<<32 {
		return len(p), io.EOF
	}
	return len(p), nil
}
