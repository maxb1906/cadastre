// Package ntfstest builds synthetic NTFS structures (FILE records, boot
// sectors, whole volume images) for tests, fuzzing and benchmarks.
package ntfstest

import (
	"encoding/binary"
	"unicode/utf16"

	"github.com/maxb1906/cadastre/internal/ntfs"
)

// Attr describes one attribute to encode.
type Attr struct {
	Type     uint32
	Name     string
	Flags    uint16
	Resident bool
	Value    []byte // resident value

	// Non-resident fields.
	LowestVCN  uint64
	HighestVCN uint64 // computed from Runs when zero and Runs not empty
	Runs       []ntfs.Run
	Alloc      uint64
	Real       uint64
	Compressed uint64 // written when Flags has compressed/sparse bits
}

// Record describes one FILE record to encode.
type Record struct {
	Size    int // record size in bytes, default 1024
	Seq     uint16
	Flags   uint16 // ntfs.RecordInUse etc.
	BaseRec uint64
	BaseSeq uint16
	Links   uint16
	USN     uint16 // update sequence number, default 1
	Attrs   []Attr
}

// Bytes encodes the record with update sequence protection applied, as it
// would be found on disk.
func (r Record) Bytes() []byte {
	size := r.Size
	if size == 0 {
		size = 1024
	}
	b := make([]byte, size)
	le := binary.LittleEndian
	copy(b, "FILE")
	blocks := size / 512
	usaOff := 0x30
	le.PutUint16(b[4:], uint16(usaOff))
	le.PutUint16(b[6:], uint16(blocks+1))
	le.PutUint16(b[0x10:], r.Seq)
	le.PutUint16(b[0x12:], r.Links)
	first := (usaOff + 2*(blocks+1) + 7) &^ 7
	le.PutUint16(b[0x14:], uint16(first))
	le.PutUint16(b[0x16:], r.Flags)
	le.PutUint32(b[0x1C:], uint32(size))
	le.PutUint64(b[0x20:], r.BaseRec|uint64(r.BaseSeq)<<48)
	off := first
	for i, a := range r.Attrs {
		enc := encodeAttr(a, uint16(i))
		if off+len(enc)+8 > size {
			panic("ntfstest: record overflow")
		}
		copy(b[off:], enc)
		off += len(enc)
	}
	le.PutUint32(b[off:], ntfs.AttrEnd)
	off += 8
	le.PutUint32(b[0x18:], uint32(off))
	le.PutUint16(b[0x28:], uint16(len(r.Attrs)))
	Protect(b, r.USN)
	return b
}

// Protect applies update sequence protection to a record whose header
// already declares its USA offset and count.
func Protect(b []byte, usn uint16) {
	if usn == 0 {
		usn = 1
	}
	le := binary.LittleEndian
	usaOff := int(le.Uint16(b[4:]))
	count := int(le.Uint16(b[6:]))
	le.PutUint16(b[usaOff:], usn)
	for i := 0; i < count-1; i++ {
		end := (i+1)*512 - 2
		copy(b[usaOff+2+2*i:], b[end:end+2])
		le.PutUint16(b[end:], usn)
	}
}

func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(out[2*i:], c)
	}
	return out
}

func align8(n int) int { return (n + 7) &^ 7 }

func encodeAttr(a Attr, id uint16) []byte {
	le := binary.LittleEndian
	name := utf16le(a.Name)
	if a.Resident {
		nameOff := 0x18
		valOff := align8(nameOff + len(name))
		total := align8(valOff + len(a.Value))
		b := make([]byte, total)
		le.PutUint32(b[0:], a.Type)
		le.PutUint32(b[4:], uint32(total))
		b[9] = byte(len(name) / 2)
		le.PutUint16(b[0x0A:], uint16(nameOff))
		le.PutUint16(b[0x0C:], a.Flags)
		le.PutUint16(b[0x0E:], id)
		le.PutUint32(b[0x10:], uint32(len(a.Value)))
		le.PutUint16(b[0x14:], uint16(valOff))
		copy(b[nameOff:], name)
		copy(b[valOff:], a.Value)
		return b
	}
	hdr := 0x40
	if a.Flags&0x80FF != 0 {
		hdr = 0x48
	}
	nameOff := hdr
	runOff := align8(nameOff + len(name))
	runs := ntfs.EncodeRunlist(a.Runs)
	total := align8(runOff + len(runs))
	b := make([]byte, total)
	le.PutUint32(b[0:], a.Type)
	le.PutUint32(b[4:], uint32(total))
	b[8] = 1
	b[9] = byte(len(name) / 2)
	le.PutUint16(b[0x0A:], uint16(nameOff))
	le.PutUint16(b[0x0C:], a.Flags)
	le.PutUint16(b[0x0E:], id)
	hi := a.HighestVCN
	if hi == 0 && len(a.Runs) > 0 {
		var n uint64
		for _, r := range a.Runs {
			n += r.Length
		}
		hi = a.LowestVCN + n - 1
	}
	le.PutUint64(b[0x10:], a.LowestVCN)
	le.PutUint64(b[0x18:], hi)
	le.PutUint16(b[0x20:], uint16(runOff))
	le.PutUint64(b[0x28:], a.Alloc)
	le.PutUint64(b[0x30:], a.Real)
	le.PutUint64(b[0x38:], a.Real)
	if hdr == 0x48 {
		le.PutUint16(b[0x22:], 4)
		le.PutUint64(b[0x40:], a.Compressed)
	}
	copy(b[nameOff:], name)
	copy(b[runOff:], runs)
	return b
}

// StdInfo returns a $STANDARD_INFORMATION attribute.
func StdInfo(created, modified int64, fileAttrs uint32) Attr {
	v := make([]byte, 0x48)
	le := binary.LittleEndian
	le.PutUint64(v[0x00:], uint64(created))
	le.PutUint64(v[0x08:], uint64(modified))
	le.PutUint64(v[0x10:], uint64(modified))
	le.PutUint64(v[0x18:], uint64(modified))
	le.PutUint32(v[0x20:], fileAttrs)
	return Attr{Type: ntfs.AttrStandardInformation, Resident: true, Value: v}
}

// FileName returns a $FILE_NAME attribute.
func FileName(parent uint64, parentSeq uint16, name string, ns uint8) Attr {
	n := utf16le(name)
	v := make([]byte, 0x42+len(n))
	le := binary.LittleEndian
	le.PutUint64(v[0:], parent|uint64(parentSeq)<<48)
	v[0x40] = byte(len(n) / 2)
	v[0x41] = ns
	copy(v[0x42:], n)
	return Attr{Type: ntfs.AttrFileName, Resident: true, Value: v}
}

// ResidentData returns a resident $DATA attribute holding data.
func ResidentData(stream string, data []byte) Attr {
	return Attr{Type: ntfs.AttrData, Name: stream, Resident: true, Value: data}
}

// NonResidentData returns a non-resident $DATA attribute.
func NonResidentData(stream string, runs []ntfs.Run, real, alloc uint64) Attr {
	return Attr{Type: ntfs.AttrData, Name: stream, Runs: runs, Real: real, Alloc: alloc}
}

// Reparse returns a resident $REPARSE_POINT attribute with the given tag.
func Reparse(tag uint32) Attr {
	v := make([]byte, 8)
	binary.LittleEndian.PutUint32(v, tag)
	return Attr{Type: ntfs.AttrReparsePoint, Resident: true, Value: v}
}

// AttrList returns a resident $ATTRIBUTE_LIST attribute.
func AttrList(entries []ntfs.AttrListEntry) Attr {
	var v []byte
	le := binary.LittleEndian
	for _, e := range entries {
		ent := make([]byte, 0x20)
		le.PutUint32(ent[0:], e.Type)
		le.PutUint16(ent[4:], 0x20)
		ent[6] = e.NameLen
		ent[7] = 0x1A
		le.PutUint64(ent[8:], e.LowestVCN)
		le.PutUint64(ent[0x10:], e.Rec|uint64(e.Seq)<<48)
		v = append(v, ent...)
	}
	return Attr{Type: ntfs.AttrAttributeList, Resident: true, Value: v}
}

// BootSector returns a minimal NTFS boot sector.
func BootSector(bytesPerSector, sectorsPerCluster int, totalSectors, mftLcn uint64, recordSize int) []byte {
	b := make([]byte, 512)
	le := binary.LittleEndian
	copy(b[3:], "NTFS    ")
	le.PutUint16(b[0x0B:], uint16(bytesPerSector))
	b[0x0D] = byte(sectorsPerCluster)
	le.PutUint64(b[0x28:], totalSectors)
	le.PutUint64(b[0x30:], mftLcn)
	le.PutUint64(b[0x38:], mftLcn+2)
	cs := bytesPerSector * sectorsPerCluster
	if recordSize >= cs {
		b[0x40] = byte(recordSize / cs)
	} else {
		e := 0
		for 1<<e < recordSize {
			e++
		}
		b[0x40] = byte(int8(-e))
	}
	b[0x44] = 1
	b[510], b[511] = 0x55, 0xAA
	return b
}
