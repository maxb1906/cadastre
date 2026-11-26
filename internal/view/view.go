// Package view serves the scanned tree level by level, as the interfaces
// need it: children of a folder, breadcrumbs, and a treemap limited to a
// few thousand rectangles. The full tree never leaves Go.
package view

import (
	"sort"
	"strings"

	"github.com/maxb1906/cadastre/internal/index"
)

// Item is one row of a folder listing.
type Item struct {
	ID      uint32   `json:"id"`
	Name    string   `json:"name"`
	Dir     bool     `json:"dir"`
	Size    uint64   `json:"size"`
	Alloc   uint64   `json:"alloc"`
	MTime   uint32   `json:"mtime"`
	Files   uint64   `json:"files,omitempty"` // directories: files in the subtree
	Ext     string   `json:"ext,omitempty"`
	Flags   []string `json:"flags,omitempty"`
	Grouped uint64   `json:"grouped,omitempty"` // >0: synthetic row of folded files
}

// Flags returns readable markers of an entry.
func Flags(ix *index.Index, i uint32) []string {
	a := ix.Attr[i]
	var f []string
	add := func(cond bool, s string) {
		if cond {
			f = append(f, s)
		}
	}
	add(a&index.AttrReparsePoint != 0, "reparse")
	add(a&index.FlagHardlink != 0, "hardlink")
	add(a&index.FlagADS != 0, "ads")
	add(a&index.FlagMeta != 0, "meta")
	add(a&index.FlagCloud != 0, "cloud")
	add(a&index.AttrCompressed != 0, "compressed")
	add(a&index.AttrSparse != 0, "sparse")
	add(a&index.FlagWOF != 0, "wof")
	add(a&index.FlagDenied != 0, "denied")
	add(a&index.FlagOrphan != 0, "orphan")
	add(a&index.FlagVirtual != 0, "virtual")
	add(a&index.AttrHidden != 0, "hidden")
	add(a&index.AttrSystem != 0, "system")
	return f
}

// MakeItem builds the row of entry i. counts gives subtree file counts of
// directories (nil to skip).
func MakeItem(ix *index.Index, i uint32, counts map[uint32]uint64) Item {
	it := Item{ID: i, Name: ix.Name(i), Dir: ix.IsDir(i), Size: ix.TotalSize(i), Alloc: ix.TotalAlloc(i), MTime: ix.MTime[i], Flags: Flags(ix, i)}
	if i == ix.Root {
		it.Name = ix.RootPath
	}
	if it.Dir {
		it.Files = counts[i]
	} else {
		it.Ext = ix.ExtName(i)
	}
	return it
}

// Listing is a sorted page of the children of a folder.
type Listing struct {
	Dir   Item   `json:"dir"`
	Path  []Item `json:"path"` // breadcrumbs from the root
	Items []Item `json:"items"`
	Total int    `json:"total"`
}

// Children lists the children of dir sorted by key ("alloc", "size",
// "name", "mtime", "files"), descending except for names, and returns
// items [offset, offset+limit). Folded files are grouped into one row.
func Children(ix *index.Index, dir uint32, sortKey string, offset, limit int) Listing {
	kids := ix.Kids(dir)
	counts := subtreeFileCounts(ix, dir)
	items := make([]Item, 0, len(kids))
	var folded Item
	for _, k := range kids {
		if !ix.Valid(k) {
			continue
		}
		if ix.Has(k, index.FlagFolded) {
			folded.Grouped++
			folded.Size += ix.TotalSize(k)
			folded.Alloc += ix.TotalAlloc(k)
			continue
		}
		items = append(items, MakeItem(ix, k, counts))
	}
	if folded.Grouped > 0 {
		folded.ID = dir
		folded.Name = ""
		items = append(items, folded)
	}
	SortItems(items, sortKey)
	l := Listing{Dir: MakeItem(ix, dir, counts), Path: Breadcrumbs(ix, dir), Total: len(items)}
	if offset < 0 {
		offset = 0
	}
	if offset > len(items) {
		offset = len(items)
	}
	end := len(items)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	l.Items = items[offset:end]
	return l
}

// SortItems sorts rows in place.
func SortItems(items []Item, key string) {
	less := func(a, b int) bool { return items[a].Alloc > items[b].Alloc }
	switch key {
	case "size":
		less = func(a, b int) bool { return items[a].Size > items[b].Size }
	case "name":
		less = func(a, b int) bool { return strings.ToLower(items[a].Name) < strings.ToLower(items[b].Name) }
	case "mtime":
		less = func(a, b int) bool { return items[a].MTime > items[b].MTime }
	case "files":
		less = func(a, b int) bool { return items[a].Files > items[b].Files }
	}
	sort.SliceStable(items, less)
}

// subtreeFileCounts counts files under each child directory of dir in one
// traversal of the subtree.
func subtreeFileCounts(ix *index.Index, dir uint32) map[uint32]uint64 {
	counts := map[uint32]uint64{}
	var total uint64
	for _, k := range ix.Kids(dir) {
		if !ix.IsDir(k) {
			total++
			continue
		}
		f, _ := ix.SubtreeCounts(k)
		counts[k] = f
		total += f
	}
	counts[dir] = total
	return counts
}

// Breadcrumbs returns the chain root → i.
func Breadcrumbs(ix *index.Index, i uint32) []Item {
	var chain []uint32
	for j := i; j != index.NoParent; j = ix.Parent[j] {
		chain = append(chain, j)
		if j == ix.Root || len(chain) > 1<<12 {
			break
		}
	}
	out := make([]Item, 0, len(chain))
	for k := len(chain) - 1; k >= 0; k-- {
		j := chain[k]
		name := ix.Name(j)
		if j == ix.Root {
			name = ix.RootPath
		}
		out = append(out, Item{ID: j, Name: name, Dir: true, Alloc: ix.TotalAlloc(j)})
	}
	return out
}

// Node is a treemap node. Short JSON names keep embedded reports small.
type Node struct {
	ID       uint32  `json:"i"`
	Name     string  `json:"n"`
	Size     uint64  `json:"s"` // allocated bytes (area)
	Dir      bool    `json:"d,omitempty"`
	Ext      string  `json:"e,omitempty"`
	Others   uint64  `json:"o,omitempty"` // >0: "N others" aggregate
	Children []*Node `json:"c,omitempty"`
}

// Treemap builds the treemap of dir with at most maxNodes nodes. Folders are
// expanded largest first; small children (below 1/2000 of dir or beyond
// the 300 largest of a folder) are grouped into one "others" node.
func Treemap(ix *index.Index, dir uint32, maxNodes int) *Node {
	if maxNodes <= 0 {
		maxNodes = 5000
	}
	root := &Node{ID: dir, Name: ix.Name(dir), Size: ix.TotalAlloc(dir), Dir: true}
	if dir == ix.Root {
		root.Name = ix.RootPath
	}
	minSize := root.Size / 2000
	count := 1
	// Expand folders in decreasing size order (simple priority queue).
	frontier := []*Node{root}
	for len(frontier) > 0 && count < maxNodes {
		best := 0
		for k := range frontier {
			if frontier[k].Size > frontier[best].Size {
				best = k
			}
		}
		n := frontier[best]
		frontier[best] = frontier[len(frontier)-1]
		frontier = frontier[:len(frontier)-1]
		kids := append([]uint32(nil), ix.Kids(n.ID)...)
		sort.Slice(kids, func(a, b int) bool { return ix.TotalAlloc(kids[a]) > ix.TotalAlloc(kids[b]) })
		var others *Node
		for _, k := range kids {
			if !ix.Valid(k) {
				continue
			}
			s := ix.TotalAlloc(k)
			if s == 0 {
				continue
			}
			if s < minSize || len(n.Children) >= 300 || count >= maxNodes || ix.Has(k, index.FlagFolded) {
				if others == nil {
					others = &Node{ID: n.ID}
				}
				others.Others++
				others.Size += s
				continue
			}
			c := &Node{ID: k, Name: ix.Name(k), Size: s, Dir: ix.IsDir(k)}
			if !c.Dir {
				c.Ext = ix.ExtName(k)
			} else if ix.Attr[k]&index.AttrReparsePoint == 0 {
				frontier = append(frontier, c)
			}
			n.Children = append(n.Children, c)
			count++
		}
		if others != nil {
			n.Children = append(n.Children, others)
			count++
		}
	}
	return root
}
