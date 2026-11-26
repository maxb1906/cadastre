// Package index stores a scanned file tree as a Structure of Arrays: one flat
// slice per field, indexed by a dense uint32, with no pointer inside the
// arrays. Millions of entries therefore cost a few allocations and leave the
// Go garbage collector idle.
//
// For the MFT engine the entry index is the MFT record number. Synthetic
// entries (orphans node) are appended after the last record.
package index

import "sync"

// Windows file attributes kept in the low 16 bits of Attr.
const (
	AttrReadOnly     uint32 = 0x0001
	AttrHidden       uint32 = 0x0002
	AttrSystem       uint32 = 0x0004
	AttrDirectory    uint32 = 0x0010
	AttrArchive      uint32 = 0x0020
	AttrTemporary    uint32 = 0x0100
	AttrSparse       uint32 = 0x0200
	AttrReparsePoint uint32 = 0x0400
	AttrCompressed   uint32 = 0x0800
	AttrOffline      uint32 = 0x1000
	AttrEncrypted    uint32 = 0x4000
	winAttrMask      uint32 = 0xFFFF
)

// Cadastre flags kept in the high 16 bits of Attr.
const (
	FlagValid    uint32 = 1 << 16 // slot holds an entry
	FlagDir      uint32 = 1 << 17
	FlagHardlink uint32 = 1 << 18 // file has several names; counted once
	FlagADS      uint32 = 1 << 19 // file has named streams (see Index.ADS)
	FlagMeta     uint32 = 1 << 20 // NTFS metafile ($MFT, $LogFile, $Extend\...)
	FlagCloud    uint32 = 1 << 21 // Cloud Files placeholder (OneDrive...)
	FlagFolded   uint32 = 1 << 22 // small file indexed without its name (--min-size)
	FlagOrphan   uint32 = 1 << 23 // parent was missing; attached to the orphans node
	FlagRemoved  uint32 = 1 << 24 // deleted by Cadastre after the scan
	FlagVirtual  uint32 = 1 << 25 // synthetic node (orphans)
	FlagWOF      uint32 = 1 << 26 // compressed by Windows Overlay Filter
	FlagDenied   uint32 = 1 << 27 // directory could not be listed (access denied)
)

// NoParent marks the parent of the root.
const NoParent = ^uint32(0)

// Stream is a named data stream (ADS).
type Stream struct {
	Name  string
	Size  uint64
	Alloc uint64
}

// Link is an additional name of a hard-linked file.
type Link struct {
	Parent uint32
	Name   string
}

// Index is the scanned tree. Fields are exported so that engines can fill
// disjoint ranges concurrently; use the methods to read.
type Index struct {
	Parent  []uint32
	Size    []uint64 // logical size of the main stream; total for directories after Finalize
	Alloc   []uint64 // allocated size of the main stream; total for directories after Finalize
	MTime   []int64  // last write time, Windows FILETIME (100 ns since 1601)
	Attr    []uint32
	NameOff []uint32
	NameLen []uint16
	Ext     []uint16
	Seq     []uint16 // MFT sequence number (0 when unknown)

	// Children in CSR form, built by Finalize: children of i are
	// Children[ChildStart[i]:ChildStart[i+1]].
	ChildStart []uint32
	Children   []uint32

	Names []byte   // name arena (WTF-8)
	Exts  []string // interned lowercase extensions; Exts[0] = ""

	// Sparse side tables (rare entries).
	ADS         map[uint32][]Stream
	Links       map[uint32][]Link
	DirOwnAlloc map[uint32]uint64 // directory index allocation ($I30)

	Root    uint32
	Orphans uint32 // NoParent until needed
	// RootPath is the path of the root ("C:\", "\\srv\share\", "/home").
	RootPath string
	// Sep is the path separator used to build paths.
	Sep byte

	mu        sync.Mutex
	finalized bool
}

// New returns an index with room for n entries, all invalid.
func New(n int) *Index {
	ix := &Index{
		ADS:         map[uint32][]Stream{},
		Links:       map[uint32][]Link{},
		DirOwnAlloc: map[uint32]uint64{},
		Exts:        []string{""},
		Orphans:     NoParent,
		Sep:         '\\',
	}
	ix.Resize(n)
	return ix
}

// Len returns the number of slots.
func (ix *Index) Len() int { return len(ix.Parent) }

// Resize grows (or shrinks) every array to n slots.
func (ix *Index) Resize(n int) {
	ix.Parent = resize(ix.Parent, n)
	ix.Size = resize(ix.Size, n)
	ix.Alloc = resize(ix.Alloc, n)
	ix.MTime = resize(ix.MTime, n)
	ix.Attr = resize(ix.Attr, n)
	ix.NameOff = resize(ix.NameOff, n)
	ix.NameLen = resize(ix.NameLen, n)
	ix.Ext = resize(ix.Ext, n)
	ix.Seq = resize(ix.Seq, n)
	ix.finalized = false
}

func resize[T any](s []T, n int) []T {
	if n <= len(s) {
		return s[:n]
	}
	if n <= cap(s) {
		return s[:n]
	}
	out := make([]T, n)
	copy(out, s)
	return out
}

// AddNames appends a block of names to the arena and returns the offset at
// which it starts. It is safe for concurrent use.
func (ix *Index) AddNames(b []byte) uint32 {
	ix.mu.Lock()
	off := uint32(len(ix.Names))
	ix.Names = append(ix.Names, b...)
	ix.mu.Unlock()
	return off
}

// Entry describes one entry to append.
type Entry struct {
	Parent uint32
	Name   string
	Size   uint64
	Alloc  uint64
	MTime  int64
	Attr   uint32
	Seq    uint16
}

// Append adds an entry at the end and returns its index. Not safe for
// concurrent use; the walker engine serializes its calls.
func (ix *Index) Append(e Entry) uint32 {
	i := uint32(len(ix.Parent))
	ix.Parent = append(ix.Parent, e.Parent)
	ix.Size = append(ix.Size, e.Size)
	ix.Alloc = append(ix.Alloc, e.Alloc)
	ix.MTime = append(ix.MTime, e.MTime)
	ix.Attr = append(ix.Attr, e.Attr|FlagValid)
	ix.NameOff = append(ix.NameOff, uint32(len(ix.Names)))
	ix.NameLen = append(ix.NameLen, uint16(len(e.Name)))
	ix.Ext = append(ix.Ext, 0)
	ix.Seq = append(ix.Seq, e.Seq)
	ix.Names = append(ix.Names, e.Name...)
	ix.finalized = false
	return i
}

// Valid reports whether slot i holds a live entry.
func (ix *Index) Valid(i uint32) bool {
	return int(i) < len(ix.Attr) && ix.Attr[i]&(FlagValid|FlagRemoved) == FlagValid
}

// IsDir reports whether entry i is a directory.
func (ix *Index) IsDir(i uint32) bool { return ix.Attr[i]&FlagDir != 0 }

// Has reports whether entry i has all the given flag bits.
func (ix *Index) Has(i uint32, flags uint32) bool { return ix.Attr[i]&flags == flags }

// NameBytes returns the name of entry i without copying.
func (ix *Index) NameBytes(i uint32) []byte {
	off := ix.NameOff[i]
	return ix.Names[off : off+uint32(ix.NameLen[i])]
}

// Name returns the name of entry i.
func (ix *Index) Name(i uint32) string { return string(ix.NameBytes(i)) }

// ExtName returns the extension of entry i ("" if none).
func (ix *Index) ExtName(i uint32) string { return ix.Exts[ix.Ext[i]] }

// Kids returns the children of i (valid after Finalize).
func (ix *Index) Kids(i uint32) []uint32 {
	if int(i)+1 >= len(ix.ChildStart) {
		return nil
	}
	return ix.Children[ix.ChildStart[i]:ix.ChildStart[i+1]]
}

// Path returns the full path of entry i.
func (ix *Index) Path(i uint32) string {
	return string(ix.AppendPath(nil, i))
}

// AppendPath appends the full path of entry i to dst.
func (ix *Index) AppendPath(dst []byte, i uint32) []byte {
	var chain [128]uint32
	parts := chain[:0]
	for j, n := i, 0; j != ix.Root && j != NoParent && n < 1<<16; n++ {
		parts = append(parts, j)
		j = ix.Parent[j]
	}
	root := ix.RootPath
	dst = append(dst, root...)
	needSep := len(root) > 0 && root[len(root)-1] != ix.Sep && root[len(root)-1] != '/'
	for k := len(parts) - 1; k >= 0; k-- {
		if needSep {
			dst = append(dst, ix.Sep)
		}
		dst = append(dst, ix.NameBytes(parts[k])...)
		needSep = true
	}
	return dst
}

// Depth returns the number of ancestors of i up to the root.
func (ix *Index) Depth(i uint32) int {
	d := 0
	for j := i; j != ix.Root && j != NoParent && d < 1<<16; d++ {
		j = ix.Parent[j]
	}
	return d
}

// IsAncestor reports whether a is i or one of its ancestors.
func (ix *Index) IsAncestor(a, i uint32) bool {
	for n := 0; n < 1<<16; n++ {
		if i == a {
			return true
		}
		if i == ix.Root || i == NoParent {
			return false
		}
		i = ix.Parent[i]
	}
	return false
}

// ADSTotals returns the summed size and allocation of the named streams of i.
func (ix *Index) ADSTotals(i uint32) (size, alloc uint64) {
	if ix.Attr[i]&FlagADS == 0 {
		return 0, 0
	}
	for _, s := range ix.ADS[i] {
		size += s.Size
		alloc += s.Alloc
	}
	return
}

// TotalSize returns the size of entry i including named streams (files) or
// of the whole subtree (directories, after Finalize).
func (ix *Index) TotalSize(i uint32) uint64 {
	if ix.IsDir(i) {
		return ix.Size[i]
	}
	s, _ := ix.ADSTotals(i)
	return ix.Size[i] + s
}

// TotalAlloc is TotalSize for allocated space.
func (ix *Index) TotalAlloc(i uint32) uint64 {
	if ix.IsDir(i) {
		return ix.Alloc[i]
	}
	_, a := ix.ADSTotals(i)
	return ix.Alloc[i] + a
}

// Finalized reports whether Finalize has run since the last modification.
func (ix *Index) Finalized() bool { return ix.finalized }

// MarkRemoved flags entry i (and, for a directory, its subtree) as deleted.
// Call Finalize afterwards to refresh the aggregates.
func (ix *Index) MarkRemoved(i uint32) {
	stack := []uint32{i}
	for len(stack) > 0 {
		j := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		ix.Attr[j] |= FlagRemoved
		if ix.finalized {
			stack = append(stack, ix.Kids(j)...)
		}
	}
	ix.finalized = false
}

// extBytes writes into buf the lowercase extension of a file name and
// returns it, or nil when the name has none or it is not a plausible
// extension (too long, spaces, non-ASCII).
func extBytes(name []byte, buf *[16]byte) []byte {
	dot := -1
	for k := len(name) - 1; k >= 0 && len(name)-k <= 17; k-- {
		if name[k] == '.' {
			dot = k
			break
		}
	}
	if dot <= 0 || dot == len(name)-1 {
		return nil
	}
	ext := name[dot+1:]
	for k, c := range ext {
		switch {
		case c == ' ' || c >= 0x80:
			return nil
		case c >= 'A' && c <= 'Z':
			c += 'a' - 'A'
		}
		buf[k] = c
	}
	return buf[:len(ext)]
}
