package ntfs

import (
	"encoding/binary"
	"errors"

	"github.com/maxb1906/cadastre/internal/wtf8"
)

// Attribute type codes.
const (
	AttrStandardInformation uint32 = 0x10
	AttrAttributeList       uint32 = 0x20
	AttrFileName            uint32 = 0x30
	AttrObjectID            uint32 = 0x40
	AttrSecurityDescriptor  uint32 = 0x50
	AttrVolumeName          uint32 = 0x60
	AttrVolumeInformation   uint32 = 0x70
	AttrData                uint32 = 0x80
	AttrIndexRoot           uint32 = 0x90
	AttrIndexAllocation     uint32 = 0xA0
	AttrBitmap              uint32 = 0xB0
	AttrReparsePoint        uint32 = 0xC0
	AttrEnd                 uint32 = 0xFFFFFFFF
)

// FILE record header flags.
const (
	RecordInUse     uint16 = 0x0001
	RecordDirectory uint16 = 0x0002
)

// Attribute header flags.
const (
	attrFlagCompressionMask uint16 = 0x00FF
	attrFlagEncrypted       uint16 = 0x4000
	attrFlagSparse          uint16 = 0x8000
)

// File name namespaces.
const (
	NamespacePOSIX    uint8 = 0
	NamespaceWin32    uint8 = 1
	NamespaceDOS      uint8 = 2
	NamespaceWin32DOS uint8 = 3
)

// Well-known reparse tags.
const (
	ReparseTagMountPoint uint32 = 0xA0000003
	ReparseTagSymlink    uint32 = 0xA000000C
	ReparseTagWOF        uint32 = 0x80000017
	ReparseTagDedup      uint32 = 0x80000013
	ReparseTagAppExecLnk uint32 = 0x8000001B
	ReparseTagLxSymlink  uint32 = 0xA000001D
	// Cloud Files placeholders (OneDrive...) use 0x9000001A and variants
	// 0x9000101A ... 0x9000F01A.
	reparseTagCloudMask uint32 = 0xFFFF0FFF
	reparseTagCloud     uint32 = 0x9000001A
)

// IsCloudReparseTag reports whether tag denotes a Cloud Files placeholder.
func IsCloudReparseTag(tag uint32) bool { return tag&reparseTagCloudMask == reparseTagCloud }

// wofStreamName is the named stream holding the real data of files
// compressed by Windows Overlay Filter (CompactOS, compact /exe).
const wofStreamName = "WofCompressedData"

// MaxRecordNumber is the largest record number Cadastre indexes (dense
// uint32 indices).
const MaxRecordNumber = 1<<32 - 16

// FileName is one $FILE_NAME attribute. Name is stored in Record.NameBuf.
type FileName struct {
	ParentRec uint64
	ParentSeq uint16
	Namespace uint8
	nameOff   uint32
	nameLen   uint32
}

// Stream is one $DATA attribute (unnamed main stream or named ADS), taken from
// its first segment (lowest VCN 0), which is the only one carrying sizes.
type Stream struct {
	Size     uint64 // logical size
	Alloc    uint64 // clusters actually allocated, in bytes
	Resident bool
	Flags    uint16
	nameOff  uint32
	nameLen  uint32
}

// RunSegment is the runlist of one segment of a non-resident attribute.
type RunSegment struct {
	LowestVCN  uint64
	HighestVCN uint64
	Runs       []Run
}

// AttrListEntry is one entry of an $ATTRIBUTE_LIST.
type AttrListEntry struct {
	Type      uint32
	LowestVCN uint64
	Rec       uint64 // record holding the attribute
	Seq       uint16
	NameLen   uint8
}

// Record is the decoded content of one FILE record segment. A Record is meant
// to be reused across calls to Parse to avoid allocations.
type Record struct {
	RecNo     uint64
	Seq       uint16
	Flags     uint16
	LinkCount uint16
	BaseRec   uint64 // 0 for a base record
	BaseSeq   uint16

	HasStdInfo bool
	Created    int64 // FILETIME
	Modified   int64
	Accessed   int64
	FileAttrs  uint32

	Names   []FileName
	Streams []Stream

	IndexAlloc uint64 // allocated bytes of $INDEX_ALLOCATION ($I30), first segment

	HasReparse bool
	ReparseTag uint32

	// Only filled when ParseOptions.WantAttrList is set.
	AttrList            []AttrListEntry
	AttrListNonResident []Run
	AttrListSize        uint64
	HasAttrList         bool

	// Only filled when ParseOptions.WantDataRuns is set: segments of the
	// unnamed $DATA attribute present in this record.
	DataRuns []RunSegment

	NameBuf []byte
}

// ParseOptions selects optional, more expensive decoding.
type ParseOptions struct {
	WantAttrList bool
	WantDataRuns bool
	// ClusterSize is used to report resident or bogus sizes consistently; if
	// zero, allocated sizes are taken as is.
	ClusterSize uint32
}

// Name returns the name of fn.
func (r *Record) Name(fn FileName) []byte { return r.NameBuf[fn.nameOff : fn.nameOff+fn.nameLen] }

// StreamName returns the name of a stream ("" for the main stream).
func (r *Record) StreamName(s Stream) []byte {
	return r.NameBuf[s.nameOff : s.nameOff+s.nameLen]
}

// InUse reports whether the record is allocated.
func (r *Record) InUse() bool { return r.Flags&RecordInUse != 0 }

// IsDir reports whether the record is a directory.
func (r *Record) IsDir() bool { return r.Flags&RecordDirectory != 0 }

// IsBase reports whether the record is a base record (not an extension).
func (r *Record) IsBase() bool { return r.BaseRec == 0 }

// MainStream returns the unnamed $DATA stream if it is present in this record.
func (r *Record) MainStream() (Stream, bool) {
	for _, s := range r.Streams {
		if s.nameLen == 0 {
			return s, true
		}
	}
	return Stream{}, false
}

// BestName returns the preferred file name for display: Win32 or POSIX names
// win over DOS 8.3 names. ok is false if the record has no name.
func (r *Record) BestName() (fn FileName, ok bool) {
	best := -1
	for i, n := range r.Names {
		if n.Namespace == NamespaceDOS {
			if best < 0 {
				best = i
			}
			continue
		}
		if best < 0 || r.Names[best].Namespace == NamespaceDOS {
			best = i
		}
	}
	if best < 0 {
		return FileName{}, false
	}
	return r.Names[best], true
}

func (r *Record) reset() {
	*r = Record{
		Names:    r.Names[:0],
		Streams:  r.Streams[:0],
		AttrList: r.AttrList[:0],
		AttrListNonResident: r.AttrListNonResident[:0],
		DataRuns: r.DataRuns[:0],
		NameBuf:  r.NameBuf[:0],
	}
}

var (
	ErrNotInUse     = errors.New("ntfs: record not in use")
	ErrBadRecord    = errors.New("ntfs: malformed FILE record")
	ErrBadAttribute = errors.New("ntfs: malformed attribute")
)

// Parse decodes a FILE record. buf must hold exactly one record and is
// modified in place (fixups). Records not in use return ErrNotInUse with only
// the header fields set. Parse never panics on corrupt input.
func Parse(buf []byte, recNo uint64, r *Record, opt ParseOptions) error {
	r.reset()
	r.RecNo = recNo
	if len(buf) < 48 || string(buf[0:4]) != "FILE" {
		if len(buf) >= 4 && (string(buf[0:4]) == "\x00\x00\x00\x00" || string(buf[0:4]) == "BAAD") {
			return ErrNotInUse
		}
		return ErrBadSignature
	}
	if err := ApplyFixups(buf); err != nil {
		return err
	}
	le := binary.LittleEndian
	r.Seq = le.Uint16(buf[0x10:])
	r.LinkCount = le.Uint16(buf[0x12:])
	firstAttr := int(le.Uint16(buf[0x14:]))
	r.Flags = le.Uint16(buf[0x16:])
	used := int(le.Uint32(buf[0x18:]))
	base := le.Uint64(buf[0x20:])
	r.BaseRec = base & 0xFFFFFFFFFFFF
	r.BaseSeq = uint16(base >> 48)
	if !r.InUse() {
		return ErrNotInUse
	}
	if used > len(buf) || used < firstAttr || firstAttr < 0x18 || firstAttr&7 != 0 {
		return ErrBadRecord
	}
	return r.parseAttributes(buf[:used], firstAttr, opt)
}

func (r *Record) parseAttributes(b []byte, off int, opt ParseOptions) error {
	le := binary.LittleEndian
	for off+8 <= len(b) {
		typ := le.Uint32(b[off:])
		if typ == AttrEnd {
			return nil
		}
		alen := int(le.Uint32(b[off+4:]))
		if alen < 0x18 || alen&7 != 0 || off+alen > len(b) {
			return ErrBadAttribute
		}
		a := b[off : off+alen]
		off += alen
		if err := r.parseAttribute(typ, a, opt); err != nil {
			return err
		}
	}
	// A record without end marker is truncated: keep what was decoded.
	return ErrBadRecord
}

// attrHeader is the decoded common part of an attribute header.
type attrHeader struct {
	nonResident bool
	name        []byte // raw UTF-16LE
	flags       uint16
	// resident
	value []byte
	// non-resident
	lowestVCN, highestVCN uint64
	runs                  []byte
	allocated, real       uint64
	compressed            uint64
	hasCompressed         bool
}

func decodeAttrHeader(a []byte) (h attrHeader, err error) {
	le := binary.LittleEndian
	h.nonResident = a[8] != 0
	nameLen := int(a[9]) * 2
	nameOff := int(le.Uint16(a[0x0A:]))
	h.flags = le.Uint16(a[0x0C:])
	if nameLen > 0 {
		if nameOff+nameLen > len(a) {
			return h, ErrBadAttribute
		}
		h.name = a[nameOff : nameOff+nameLen]
	}
	if !h.nonResident {
		vlen := int(le.Uint32(a[0x10:]))
		voff := int(le.Uint16(a[0x14:]))
		if vlen < 0 || voff+vlen > len(a) || voff > len(a) {
			return h, ErrBadAttribute
		}
		h.value = a[voff : voff+vlen]
		return h, nil
	}
	if len(a) < 0x40 {
		return h, ErrBadAttribute
	}
	h.lowestVCN = le.Uint64(a[0x10:])
	h.highestVCN = le.Uint64(a[0x18:])
	runOff := int(le.Uint16(a[0x20:]))
	h.allocated = le.Uint64(a[0x28:])
	h.real = le.Uint64(a[0x30:])
	if h.flags&(attrFlagCompressionMask|attrFlagSparse) != 0 && runOff >= 0x48 && len(a) >= 0x48 {
		h.compressed = le.Uint64(a[0x40:])
		h.hasCompressed = true
	}
	if runOff > len(a) || runOff < 0x40 {
		return h, ErrBadAttribute
	}
	h.runs = a[runOff:]
	return h, nil
}

// allocatedBytes returns the space really used on disk by a non-resident
// attribute: the "compressed size" field for compressed or sparse
// attributes (which excludes holes), the allocated size otherwise.
func (h *attrHeader) allocatedBytes() uint64 {
	if h.hasCompressed {
		return h.compressed
	}
	return h.allocated
}

func (r *Record) parseAttribute(typ uint32, a []byte, opt ParseOptions) error {
	h, err := decodeAttrHeader(a)
	if err != nil {
		return err
	}
	le := binary.LittleEndian
	switch typ {
	case AttrStandardInformation:
		if h.nonResident || len(h.value) < 0x24 {
			return nil // tolerate: keep the record without timestamps
		}
		v := h.value
		r.HasStdInfo = true
		r.Created = int64(le.Uint64(v[0x00:]))
		r.Modified = int64(le.Uint64(v[0x08:]))
		r.Accessed = int64(le.Uint64(v[0x18:]))
		r.FileAttrs = le.Uint32(v[0x20:])

	case AttrFileName:
		if h.nonResident || len(h.value) < 0x42 {
			return nil
		}
		v := h.value
		n := int(v[0x40]) * 2
		if 0x42+n > len(v) {
			return nil
		}
		ref := le.Uint64(v[0:])
		off := len(r.NameBuf)
		r.NameBuf = wtf8.AppendUTF16LE(r.NameBuf, v[0x42:0x42+n])
		r.Names = append(r.Names, FileName{
			ParentRec: ref & 0xFFFFFFFFFFFF,
			ParentSeq: uint16(ref >> 48),
			Namespace: v[0x41],
			nameOff:   uint32(off),
			nameLen:   uint32(len(r.NameBuf) - off),
		})

	case AttrData:
		if h.nonResident {
			if opt.WantDataRuns && len(h.name) == 0 {
				runs, err := DecodeRunlist(h.runs)
				if err != nil {
					return err
				}
				r.DataRuns = append(r.DataRuns, RunSegment{LowestVCN: h.lowestVCN, HighestVCN: h.highestVCN, Runs: runs})
			}
			if h.lowestVCN != 0 {
				return nil // continuation segment: sizes are in the first one
			}
		}
		s := Stream{Flags: h.flags, Resident: !h.nonResident}
		if h.nonResident {
			s.Size = h.real
			s.Alloc = h.allocatedBytes()
		} else {
			s.Size = uint64(len(h.value))
		}
		if len(h.name) > 0 {
			off := len(r.NameBuf)
			r.NameBuf = wtf8.AppendUTF16LE(r.NameBuf, h.name)
			s.nameOff, s.nameLen = uint32(off), uint32(len(r.NameBuf)-off)
		}
		r.Streams = append(r.Streams, s)

	case AttrIndexAllocation:
		if h.nonResident && h.lowestVCN == 0 {
			r.IndexAlloc += h.allocatedBytes()
		}

	case AttrReparsePoint:
		r.HasReparse = true
		if !h.nonResident && len(h.value) >= 4 {
			r.ReparseTag = le.Uint32(h.value)
		}

	case AttrAttributeList:
		r.HasAttrList = true
		if !opt.WantAttrList {
			return nil
		}
		if h.nonResident {
			runs, err := DecodeRunlist(h.runs)
			if err != nil {
				return err
			}
			r.AttrListNonResident = runs
			r.AttrListSize = h.real
			return nil
		}
		r.AttrList = AppendAttrList(r.AttrList, h.value)
	}
	return nil
}

// AppendAttrList decodes the entries of an $ATTRIBUTE_LIST value. Decoding
// stops silently at the first malformed entry.
func AppendAttrList(dst []AttrListEntry, v []byte) []AttrListEntry {
	le := binary.LittleEndian
	for off := 0; off+0x1A <= len(v); {
		elen := int(le.Uint16(v[off+4:]))
		if elen < 0x1A || off+elen > len(v) {
			break
		}
		ref := le.Uint64(v[off+0x10:])
		dst = append(dst, AttrListEntry{
			Type:      le.Uint32(v[off:]),
			NameLen:   v[off+6],
			LowestVCN: le.Uint64(v[off+8:]),
			Rec:       ref & 0xFFFFFFFFFFFF,
			Seq:       uint16(ref >> 48),
		})
		off += elen
		if len(dst) > 1<<16 {
			break
		}
	}
	return dst
}

// IsWofStream reports whether a named stream holds WOF-compressed data.
func (r *Record) IsWofStream(s Stream) bool {
	return string(r.StreamName(s)) == wofStreamName
}
