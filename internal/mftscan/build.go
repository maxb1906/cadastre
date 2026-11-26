// Package mftscan builds an index.Index from the raw MFT of an NTFS volume
// (engine A). It only needs an io.ReaderAt over the volume, so it runs on
// disk images on any platform as well as on \\.\C: on Windows.
package mftscan

import (
	"errors"

	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/ntfs"
)

// Windows attributes not kept in the low 16 bits of index.Attr.
const (
	fileAttrRecallOnOpen       = 0x00040000
	fileAttrRecallOnDataAccess = 0x00400000
)

// extPart is what an extension record contributes to its base record.
type extPart struct {
	base       uint32
	baseSeq    uint16
	names      []nameRef
	main       *ntfs.Stream
	ads        []index.Stream
	wofAlloc   uint64
	hasWof     bool
	extraAlloc uint64
	reparseTag uint32
	hasReparse bool
}

type nameRef struct {
	parent    uint32
	parentSeq uint16
	ns        uint8
	name      string
}

// chunkResult gathers what one chunk of records produced besides the dense
// arrays, to be merged by a single goroutine.
type chunkResult struct {
	first, count uint64
	names        []byte // local arena; NameOff of the chunk's entries are relative to it
	ads          map[uint32][]index.Stream
	links        map[uint32][]index.Link
	dirAlloc     map[uint32]uint64
	dosOnly      []uint32 // entries named after their DOS 8.3 name only
	exts         []extPart
	bad          uint64
	inUse        uint64
}

func newChunkResult() *chunkResult {
	return &chunkResult{
		ads:      map[uint32][]index.Stream{},
		links:    map[uint32][]index.Link{},
		dirAlloc: map[uint32]uint64{},
	}
}

func (c *chunkResult) reset(first, count uint64) {
	c.first, c.count = first, count
	c.names = c.names[:0]
	clear(c.ads)
	clear(c.links)
	clear(c.dirAlloc)
	c.dosOnly = c.dosOnly[:0]
	c.exts = c.exts[:0]
	c.bad, c.inUse = 0, 0
}

// builder holds the state shared by the workers. Workers write the dense
// arrays of the index at their record numbers only (disjoint ranges).
type builder struct {
	ix          *index.Index
	parentSeq   []uint16 // temporary: sequence number expected for the parent
	minSize     uint64
	recSize     int
	clusterSize uint32
}

// processChunk parses count records stored back to back in buf, the first
// one being record number first.
func (b *builder) processChunk(buf []byte, first uint64, count int, rec *ntfs.Record, out *chunkResult) {
	out.reset(first, uint64(count))
	for k := 0; k < count; k++ {
		recNo := first + uint64(k)
		raw := buf[k*b.recSize : (k+1)*b.recSize]
		err := ntfs.Parse(raw, recNo, rec, ntfs.ParseOptions{ClusterSize: b.clusterSize})
		if err != nil {
			if !errors.Is(err, ntfs.ErrNotInUse) {
				out.bad++
			}
			continue
		}
		out.inUse++
		if !rec.IsBase() {
			b.collectExtension(rec, out)
			continue
		}
		b.applyBase(rec, out)
	}
}

func (b *builder) applyBase(rec *ntfs.Record, out *chunkResult) {
	ix := b.ix
	i := uint32(rec.RecNo)
	fn, ok := rec.BestName()
	if !ok && rec.RecNo != ntfs.RecRoot && !rec.HasAttrList {
		return // in-use record without name: reserved or being created
	}
	if !ok {
		// Names live in extension records (see applyExtensions); until then
		// the parent is unknown.
		fn.ParentRec = ntfs.MaxRecordNumber
	}
	attr := rec.FileAttrs&0xFFFF | index.FlagValid
	if rec.IsDir() {
		attr |= index.FlagDir | index.AttrDirectory
	}
	if rec.HasReparse {
		attr |= index.AttrReparsePoint
		if ntfs.IsCloudReparseTag(rec.ReparseTag) {
			attr |= index.FlagCloud
		}
	}
	if rec.FileAttrs&(fileAttrRecallOnOpen|fileAttrRecallOnDataAccess) != 0 {
		attr |= index.FlagCloud
	}
	if rec.RecNo < ntfs.FirstUserRecord && rec.RecNo != ntfs.RecRoot {
		attr |= index.FlagMeta
	}

	// Streams.
	var size, alloc uint64
	var ads []index.Stream
	for _, s := range rec.Streams {
		name := rec.StreamName(s)
		switch {
		case len(name) == 0:
			size, alloc = s.Size, s.Alloc
		case rec.IsWofStream(s):
			// Real data of a WOF-compressed file: its allocation is the
			// file's footprint, it is not an alternate stream.
			alloc += s.Alloc
			attr |= index.FlagWOF
		default:
			ads = append(ads, index.Stream{Name: string(name), Size: s.Size, Alloc: s.Alloc})
		}
	}
	if rec.HasReparse && rec.ReparseTag == ntfs.ReparseTagWOF {
		attr |= index.FlagWOF
	}
	if len(ads) > 0 {
		attr |= index.FlagADS
		out.ads[i] = ads
	}
	if rec.ExtraAlloc > 0 {
		if rec.IsDir() {
			out.dirAlloc[i] = rec.ExtraAlloc
		} else {
			// View indexes of metafiles ($Secure:$SII/$SDH, $ObjId:$O...),
			// large security descriptors, attribute lists.
			alloc += rec.ExtraAlloc
		}
	}

	// Names: primary name, then additional hard links.
	nonDOS := 0
	for _, n := range rec.Names {
		if n.Namespace != ntfs.NamespaceDOS {
			nonDOS++
		}
	}
	if nonDOS > 1 {
		attr |= index.FlagHardlink
		for _, n := range rec.Names {
			if n.Namespace == ntfs.NamespaceDOS || n == fn {
				continue
			}
			out.links[i] = append(out.links[i], index.Link{Parent: uint32(n.ParentRec), Name: string(rec.Name(n))})
		}
	}

	ix.Parent[i] = uint32(min(fn.ParentRec, ntfs.MaxRecordNumber))
	b.parentSeq[i] = fn.ParentSeq
	ix.Size[i] = size
	ix.Alloc[i] = alloc
	ix.MTime[i] = index.FileTimeToUnix(rec.Modified)
	ix.Seq[i] = rec.Seq
	if rec.RecNo == ntfs.RecRoot {
		ix.Parent[i] = index.NoParent
		ix.NameOff[i], ix.NameLen[i] = 0, 0
	} else if !rec.IsDir() && b.minSize > 0 && size < b.minSize && len(ads) == 0 && nonDOS <= 1 {
		attr |= index.FlagFolded
		ix.NameOff[i], ix.NameLen[i] = 0, 0
	} else if ok {
		name := rec.Name(fn)
		if len(name) > 0xFFFF {
			name = name[:0xFFFF]
		}
		ix.NameOff[i] = uint32(len(out.names))
		ix.NameLen[i] = uint16(len(name))
		out.names = append(out.names, name...)
		if fn.Namespace == ntfs.NamespaceDOS {
			out.dosOnly = append(out.dosOnly, i)
		}
	}
	if !ok {
		ix.NameOff[i], ix.NameLen[i] = 0, 0
	}
	ix.Attr[i] = attr
}

func (b *builder) collectExtension(rec *ntfs.Record, out *chunkResult) {
	if rec.BaseRec >= ntfs.MaxRecordNumber {
		return
	}
	p := extPart{base: uint32(rec.BaseRec), baseSeq: rec.BaseSeq, extraAlloc: rec.ExtraAlloc,
		hasReparse: rec.HasReparse, reparseTag: rec.ReparseTag}
	for _, n := range rec.Names {
		p.names = append(p.names, nameRef{parent: uint32(min(n.ParentRec, ntfs.MaxRecordNumber)), parentSeq: n.ParentSeq, ns: n.Namespace, name: string(rec.Name(n))})
	}
	for _, s := range rec.Streams {
		name := rec.StreamName(s)
		switch {
		case len(name) == 0:
			s := s
			p.main = &s
		case rec.IsWofStream(s):
			p.wofAlloc += s.Alloc
			p.hasWof = true
		default:
			p.ads = append(p.ads, index.Stream{Name: string(name), Size: s.Size, Alloc: s.Alloc})
		}
	}
	if len(p.names) > 0 || p.main != nil || len(p.ads) > 0 || p.hasWof || p.extraAlloc > 0 || p.hasReparse {
		out.exts = append(out.exts, p)
	}
}

// merge integrates a chunk result into the index. Called by one goroutine.
func (b *builder) merge(c *chunkResult, pending *[]extPart, dosOnly map[uint32]bool) {
	ix := b.ix
	base := ix.AddNames(c.names)
	if base != 0 {
		for i := c.first; i < c.first+c.count; i++ {
			if ix.NameLen[i] != 0 {
				ix.NameOff[i] += base
			}
		}
	}
	for k, v := range c.ads {
		ix.ADS[k] = v
	}
	for k, v := range c.links {
		ix.Links[k] = v
	}
	for k, v := range c.dirAlloc {
		ix.DirOwnAlloc[k] = v
	}
	for _, i := range c.dosOnly {
		dosOnly[i] = true
	}
	for _, e := range c.exts {
		*pending = append(*pending, e)
	}
}

// applyExtensions merges extension records into their base records once all
// records are parsed.
func (b *builder) applyExtensions(parts []extPart, dosOnly map[uint32]bool) {
	ix := b.ix
	n := uint32(ix.Len())
	for _, p := range parts {
		i := p.base
		if i >= n || ix.Attr[i]&index.FlagValid == 0 || ix.Seq[i] != p.baseSeq {
			continue
		}
		for _, nm := range p.names {
			if nm.ns == ntfs.NamespaceDOS && ix.NameLen[i] != 0 {
				continue
			}
			if ix.NameLen[i] == 0 && ix.Attr[i]&index.FlagFolded == 0 || dosOnly[i] && nm.ns != ntfs.NamespaceDOS {
				ix.NameOff[i] = ix.AddNames([]byte(nm.name))
				ix.NameLen[i] = uint16(len(nm.name))
				ix.Parent[i] = nm.parent
				b.parentSeq[i] = nm.parentSeq
				dosOnly[i] = nm.ns == ntfs.NamespaceDOS
				continue
			}
			ix.Attr[i] |= index.FlagHardlink
			ix.Links[i] = append(ix.Links[i], index.Link{Parent: nm.parent, Name: nm.name})
		}
		if p.main != nil {
			ix.Size[i], ix.Alloc[i] = p.main.Size, ix.Alloc[i]+p.main.Alloc
		}
		if p.hasWof {
			ix.Alloc[i] += p.wofAlloc
			ix.Attr[i] |= index.FlagWOF
		}
		if len(p.ads) > 0 {
			ix.ADS[i] = append(ix.ADS[i], p.ads...)
			ix.Attr[i] |= index.FlagADS
		}
		if p.extraAlloc > 0 {
			if ix.Attr[i]&index.FlagDir != 0 {
				ix.DirOwnAlloc[i] += p.extraAlloc
			} else {
				ix.Alloc[i] += p.extraAlloc
			}
		}
		if p.hasReparse {
			ix.Attr[i] |= index.AttrReparsePoint
			if ntfs.IsCloudReparseTag(p.reparseTag) {
				ix.Attr[i] |= index.FlagCloud
			}
		}
	}
}

// checkParents detaches entries whose parent record was reused (sequence
// mismatch): they become orphans during Finalize.
func (b *builder) checkParents() {
	ix := b.ix
	n := uint32(ix.Len())
	for i := uint32(0); i < n; i++ {
		if ix.Attr[i]&index.FlagValid == 0 || i == ix.Root {
			continue
		}
		p := ix.Parent[i]
		if p < n && ix.Attr[p]&index.FlagValid != 0 && ix.Seq[p] != b.parentSeq[i] && b.parentSeq[i] != 0 {
			ix.Parent[i] = n + 1 // invalid: reattached by Finalize
		}
	}
}

// markMeta flags the $Extend subtree ($UsnJrnl, $Quota, $ObjId, $Reparse,
// $RmMetadata...) as metafiles. Requires a finalized index.
func markMeta(ix *index.Index) {
	if !ix.Valid(ntfs.RecExtend) {
		return
	}
	ix.Walk(ntfs.RecExtend, func(j uint32) bool {
		ix.Attr[j] |= index.FlagMeta
		return true
	})
}
