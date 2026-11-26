package index

import (
	"bytes"
	"container/heap"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Totals summarizes an index.
type Totals struct {
	Files, Dirs               uint64
	Size, Alloc               uint64 // all live entries incl. ADS and metafiles
	ADSFiles, ADSStreams      uint64
	ADSSize, ADSAlloc         uint64
	MetaFiles                 uint64
	MetaSize, MetaAlloc       uint64
	Hardlinks, Reparse, Cloud uint64
	Orphans, Denied, Folded   uint64
	Compressed, Sparse, WOF   uint64
	CompressedSaved           uint64 // logical - allocated for compressed/sparse/WOF files
}

// Totals computes the summary of live entries.
func (ix *Index) Totals() Totals {
	var t Totals
	for i := range ix.Parent {
		u := uint32(i)
		if !ix.Valid(u) {
			continue
		}
		a := ix.Attr[i]
		if a&FlagOrphan != 0 {
			t.Orphans++
		}
		if a&FlagDenied != 0 {
			t.Denied++
		}
		if a&AttrReparsePoint != 0 {
			t.Reparse++
		}
		if a&FlagDir != 0 {
			if a&FlagVirtual == 0 {
				t.Dirs++
			}
			if a&FlagMeta != 0 {
				t.MetaAlloc += ix.DirOwnAlloc[u]
			}
			continue
		}
		t.Files++
		size, alloc := ix.TotalSize(u), ix.TotalAlloc(u)
		t.Size += size
		t.Alloc += alloc
		if a&FlagMeta != 0 {
			t.MetaFiles++
			t.MetaSize += size
			t.MetaAlloc += alloc
		} else if a&FlagADS != 0 {
			t.ADSFiles++
			t.ADSStreams += uint64(len(ix.ADS[u]))
			s, al := ix.ADSTotals(u)
			t.ADSSize += s
			t.ADSAlloc += al
		}
		if a&FlagHardlink != 0 {
			t.Hardlinks++
		}
		if a&FlagCloud != 0 {
			t.Cloud++
		}
		if a&FlagFolded != 0 {
			t.Folded++
		}
		if a&(AttrCompressed|AttrSparse) != 0 || a&FlagWOF != 0 {
			switch {
			case a&FlagWOF != 0:
				t.WOF++
			case a&AttrCompressed != 0:
				t.Compressed++
			default:
				t.Sparse++
			}
			if ix.Size[i] > ix.Alloc[i] {
				t.CompressedSaved += ix.Size[i] - ix.Alloc[i]
			}
		}
	}
	// Directory index allocations are part of the volume usage too.
	for d, a := range ix.DirOwnAlloc {
		if ix.Valid(d) {
			t.Alloc += a
		}
	}
	return t
}

// SortKey selects what to rank by.
type SortKey int

const (
	ByAlloc SortKey = iota
	BySize
	ByName
	ByMTime
	ByCount
)

func (ix *Index) key(i uint32, by SortKey) uint64 {
	switch by {
	case BySize:
		return ix.TotalSize(i)
	case ByMTime:
		return uint64(ix.MTime[i])
	default:
		return ix.TotalAlloc(i)
	}
}

type minHeap struct {
	idx  []uint32
	keys []uint64
}

func (h *minHeap) Len() int           { return len(h.idx) }
func (h *minHeap) Less(a, b int) bool { return h.keys[a] < h.keys[b] }
func (h *minHeap) Swap(a, b int) {
	h.idx[a], h.idx[b] = h.idx[b], h.idx[a]
	h.keys[a], h.keys[b] = h.keys[b], h.keys[a]
}
func (h *minHeap) Push(x any) { panic("unused") }
func (h *minHeap) Pop() any   { panic("unused") }

// topN keeps the n entries with the largest key among those accepted.
func topN(n int, count int, key func(i uint32) (uint64, bool)) []uint32 {
	h := &minHeap{}
	for i := 0; i < count; i++ {
		k, ok := key(uint32(i))
		if !ok {
			continue
		}
		if len(h.idx) < n {
			h.idx = append(h.idx, uint32(i))
			h.keys = append(h.keys, k)
			if len(h.idx) == n {
				heap.Init(h)
			}
			continue
		}
		if n > 0 && k > h.keys[0] {
			h.idx[0], h.keys[0] = uint32(i), k
			heap.Fix(h, 0)
		}
	}
	out := make([]uint32, len(h.idx))
	ord := make([]int, len(h.idx))
	for i := range ord {
		ord[i] = i
	}
	sort.SliceStable(ord, func(a, b int) bool { return h.keys[ord[a]] > h.keys[ord[b]] })
	for i, o := range ord {
		out[i] = h.idx[o]
	}
	return out
}

// TopFiles returns the n largest files (metafiles excluded unless withMeta).
func (ix *Index) TopFiles(n int, by SortKey, withMeta bool) []uint32 {
	return topN(n, len(ix.Parent), func(i uint32) (uint64, bool) {
		if !ix.Valid(i) || ix.Attr[i]&FlagDir != 0 || (!withMeta && ix.Attr[i]&FlagMeta != 0) {
			return 0, false
		}
		return ix.key(i, by), true
	})
}

// DirStat is the weight of a directory's direct files.
type DirStat struct {
	Dir   uint32
	Files uint64
	Size  uint64
	Alloc uint64
}

// TopDirs returns the n directories whose direct files weigh the most. This
// points at where space really accumulates rather than at the ancestors.
func (ix *Index) TopDirs(n int, by SortKey) []DirStat {
	own := make(map[uint32]*DirStat)
	for i := range ix.Parent {
		u := uint32(i)
		if !ix.Valid(u) || ix.Attr[i]&FlagDir != 0 || ix.Attr[i]&FlagMeta != 0 {
			continue
		}
		p := ix.Parent[i]
		d := own[p]
		if d == nil {
			d = &DirStat{Dir: p}
			own[p] = d
		}
		d.Files++
		d.Size += ix.TotalSize(u)
		d.Alloc += ix.TotalAlloc(u)
	}
	list := make([]DirStat, 0, len(own))
	for _, d := range own {
		list = append(list, *d)
	}
	sort.Slice(list, func(a, b int) bool {
		if by == BySize {
			return list[a].Size > list[b].Size
		}
		return list[a].Alloc > list[b].Alloc
	})
	if len(list) > n {
		list = list[:n]
	}
	return list
}

// ExtStat aggregates files by extension.
type ExtStat struct {
	Ext   string
	Files uint64
	Size  uint64
	Alloc uint64
}

// Extensions returns per-extension totals sorted by allocated size. The
// result is computed in parallel on first use and cached until the next
// Finalize.
func (ix *Index) Extensions() []ExtStat {
	if ix.extCache != nil {
		return ix.extCache
	}
	n := len(ix.Parent)
	workers := runtime.GOMAXPROCS(0)
	if n < 1<<16 {
		workers = 1
	}
	chunk := (n + workers - 1) / workers
	parts := make([]map[string]*ExtStat, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w, lo, hi int) {
			defer wg.Done()
			m := map[string]*ExtStat{}
			var buf [16]byte
			for i := lo; i < hi; i++ {
				u := uint32(i)
				if !ix.Valid(u) || ix.Attr[i]&(FlagDir|FlagMeta) != 0 {
					continue
				}
				e := extBytes(ix.NameBytes(u), &buf)
				s := m[string(e)]
				if s == nil {
					s = &ExtStat{Ext: string(e)}
					m[s.Ext] = s
				}
				s.Files++
				s.Size += ix.TotalSize(u)
				s.Alloc += ix.TotalAlloc(u)
			}
			parts[w] = m
		}(w, w*chunk, min((w+1)*chunk, n))
	}
	wg.Wait()
	all := map[string]*ExtStat{}
	for _, m := range parts {
		for k, v := range m {
			if s := all[k]; s != nil {
				s.Files += v.Files
				s.Size += v.Size
				s.Alloc += v.Alloc
			} else {
				all[k] = v
			}
		}
	}
	out := make([]ExtStat, 0, len(all))
	for _, s := range all {
		out = append(out, *s)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Alloc != out[b].Alloc {
			return out[a].Alloc > out[b].Alloc
		}
		return out[a].Ext < out[b].Ext
	})
	ix.extCache = out
	return out
}

// SubtreeCounts returns the number of files and directories under i.
func (ix *Index) SubtreeCounts(i uint32) (files, dirs uint64) {
	stack := []uint32{i}
	for len(stack) > 0 {
		j := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, k := range ix.Kids(j) {
			if ix.Attr[k]&FlagDir != 0 {
				dirs++
				stack = append(stack, k)
			} else {
				files++
			}
		}
	}
	return
}

// Walk calls fn for i and its descendants in depth-first order. If fn returns
// false for a directory, its children are skipped.
func (ix *Index) Walk(i uint32, fn func(j uint32) bool) {
	stack := []uint32{i}
	for len(stack) > 0 {
		j := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !fn(j) {
			continue
		}
		kids := ix.Kids(j)
		for k := len(kids) - 1; k >= 0; k-- {
			stack = append(stack, kids[k])
		}
	}
}

// Child returns the child of dir named name (case-insensitive).
func (ix *Index) Child(dir uint32, name string) (uint32, bool) {
	nb := []byte(name)
	for _, k := range ix.Kids(dir) {
		if equalFold(ix.NameBytes(k), nb) {
			return k, true
		}
	}
	return 0, false
}

// Find resolves a path relative to the root ("Windows\Temp", "/Windows/Temp"
// or a full path starting with RootPath). Comparison is case-insensitive.
func (ix *Index) Find(path string) (uint32, bool) {
	if rp := ix.RootPath; rp != "" && len(path) >= len(rp) && strings.EqualFold(path[:len(rp)], rp) {
		path = path[len(rp):]
	} else if rp != "" && strings.EqualFold(path, strings.TrimRight(rp, `\/`)) {
		path = ""
	}
	cur := ix.Root
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '\\' || r == '/' }) {
		next, ok := ix.Child(cur, part)
		if !ok {
			return 0, false
		}
		cur = next
	}
	return cur, true
}

// equalFold compares names case-insensitively (ASCII fast path, then Unicode
// simple folding, which matches NTFS for practical purposes).
func equalFold(a, b []byte) bool {
	if len(a) == len(b) {
		ascii := true
		for k := range a {
			ca, cb := a[k], b[k]
			if ca >= 0x80 || cb >= 0x80 {
				ascii = false
				break
			}
			if ca != cb && lower(ca) != lower(cb) {
				return false
			}
		}
		if ascii {
			return true
		}
	}
	if !utf8.Valid(a) || !utf8.Valid(b) {
		return bytes.Equal(a, b)
	}
	return bytes.EqualFold(a, b)
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// Memory returns the approximate number of bytes used by the index arrays.
func (ix *Index) Memory() uint64 {
	n := uint64(cap(ix.Parent))
	per := uint64(4 + 8 + 8 + 4 + 4 + 4 + 2 + 2)
	return n*per + uint64(cap(ix.ChildStart)+cap(ix.Children))*4 + uint64(cap(ix.Names))
}
