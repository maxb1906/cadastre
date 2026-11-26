package engine

import (
	"sort"
	"strings"

	"github.com/maxb1906/cadastre/internal/index"
)

// Diff is one discrepancy between two scans of the same tree.
type Diff struct {
	Path  string `json:"path"`
	SizeA uint64 `json:"sizeA"`
	SizeB uint64 `json:"sizeB"`
}

// CheckReport compares engine A (MFT) and engine C (walk) on the same tree.
type CheckReport struct {
	FilesA, FilesB   uint64
	SizeA, SizeB     uint64 // sum of logical sizes of compared files
	Common           uint64
	OnlyA, OnlyB     []Diff // examples (capped)
	CountOnlyA       uint64
	CountOnlyB       uint64
	SizeOnlyA        uint64
	SizeOnlyB        uint64
	Mismatch         []Diff // examples (capped)
	CountMismatch    uint64
	SkippedMetafiles uint64
	SkippedProtected uint64
}

// OK reports whether both engines agree on every compared file.
func (r *CheckReport) OK() bool {
	return r.CountOnlyA == 0 && r.CountOnlyB == 0 && r.CountMismatch == 0
}

// hardlinkUnknown marks a hard link name whose size was not recorded.
const hardlinkUnknown = ^uint64(0)

// protectedDirs are not readable by a walk even as administrator, or hold
// data the MFT shows but a walk cannot (VSS diff areas).
var protectedDirs = []string{"system volume information", "$recycle.bin", "$extend", "config.msi"}

func fileSizes(ix *index.Index, withLinks bool, skippedMeta, skippedProt *uint64) map[string]uint64 {
	out := make(map[string]uint64, ix.Len()/2)
	var buf []byte
	ix.Walk(ix.Root, func(i uint32) bool {
		if !ix.Valid(i) {
			return false
		}
		if i != ix.Root && ix.Parent[i] == ix.Root {
			name := strings.ToLower(ix.Name(i))
			for _, p := range protectedDirs {
				if name == p {
					*skippedProt++
					return false
				}
			}
		}
		if ix.Has(i, index.FlagMeta) {
			*skippedMeta++
			return false
		}
		if ix.Has(i, index.FlagVirtual) || ix.IsDir(i) {
			return true
		}
		buf = relPath(ix, i, buf[:0])
		if ix.Has(i, index.FlagHardlink) && ix.Size[i] == 0 && !withLinks {
			// Walk engine: secondary name of a hard link, counted once
			// under its first name. Its size is unknown here.
			out[string(buf)] = hardlinkUnknown
			return true
		}
		out[string(buf)] = ix.Size[i]
		if withLinks {
			for _, l := range ix.Links[i] {
				if ix.Valid(l.Parent) {
					p := strings.ToLower(string(relPath(ix, l.Parent, nil))) + `\` + strings.ToLower(l.Name)
					out[strings.TrimPrefix(p, `\`)] = ix.Size[i]
				}
			}
		}
		return true
	})
	return out
}

func relPath(ix *index.Index, i uint32, dst []byte) []byte {
	var parts []uint32
	for j := i; j != ix.Root && j != index.NoParent; j = ix.Parent[j] {
		parts = append(parts, j)
	}
	for k := len(parts) - 1; k >= 0; k-- {
		if len(dst) > 0 {
			dst = append(dst, '\\')
		}
		dst = append(dst, strings.ToLower(ix.Name(parts[k]))...)
	}
	return dst
}

// Check compares an MFT scan (a) with a walk (b) of the same tree. Hard
// links are compared under each of their names, since a walk sees them all.
func Check(a, b *index.Index, maxExamples int) *CheckReport {
	r := &CheckReport{}
	var dummy uint64
	ma := fileSizes(a, true, &r.SkippedMetafiles, &r.SkippedProtected)
	mb := fileSizes(b, false, &dummy, &dummy)
	r.FilesA, r.FilesB = uint64(len(ma)), uint64(len(mb))
	for p, sa := range ma {
		r.SizeA += sa
		sb, ok := mb[p]
		if !ok {
			r.CountOnlyA++
			r.SizeOnlyA += sa
			r.OnlyA = append(r.OnlyA, Diff{Path: p, SizeA: sa})
			continue
		}
		r.Common++
		if sa != sb && sb != hardlinkUnknown {
			r.CountMismatch++
			r.Mismatch = append(r.Mismatch, Diff{Path: p, SizeA: sa, SizeB: sb})
		}
	}
	for p, sb := range mb {
		if sb == hardlinkUnknown {
			sb = 0
		}
		r.SizeB += sb
		if _, ok := ma[p]; !ok {
			r.CountOnlyB++
			r.SizeOnlyB += sb
			r.OnlyB = append(r.OnlyB, Diff{Path: p, SizeB: sb})
		}
	}
	cap := func(d []Diff) []Diff {
		sort.Slice(d, func(i, j int) bool {
			return max(d[i].SizeA, d[i].SizeB) > max(d[j].SizeA, d[j].SizeB)
		})
		if len(d) > maxExamples {
			d = d[:maxExamples]
		}
		return d
	}
	r.OnlyA, r.OnlyB, r.Mismatch = cap(r.OnlyA), cap(r.OnlyB), cap(r.Mismatch)
	return r
}
