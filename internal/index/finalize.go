package index

// OrphansName is the name of the synthetic directory gathering entries whose
// parent is missing or corrupt. Interfaces translate it.
const OrphansName = "<orphans>"

// Finalize computes extensions, repairs the tree (missing parents, cycles),
// builds the children lists and aggregates directory sizes bottom-up in a
// single reverse pass, without recursion. It can be called again after
// MarkRemoved or Append to refresh everything.
func (ix *Index) Finalize() {
	ix.extCache = nil
	ix.repairParents()
	order := ix.buildTree()
	if ix.reattachUnreached(order) {
		order = ix.buildTree()
	}
	ix.aggregate(order)
	if c := cap(ix.Names); c > len(ix.Names)+len(ix.Names)/16+4096 {
		// Release the slack left by append in the name arena.
		ix.Names = append(make([]byte, 0, len(ix.Names)), ix.Names...)
	}
	ix.finalized = true
}

func (ix *Index) orphans() uint32 {
	if ix.Orphans == NoParent {
		ix.Orphans = ix.Append(Entry{Parent: ix.Root, Name: OrphansName, Attr: FlagDir | AttrDirectory | FlagVirtual})
	}
	return ix.Orphans
}

// repairParents attaches to the orphans node every entry whose parent is not
// a live directory, and propagates removals to children.
func (ix *Index) repairParents() {
	n := uint32(len(ix.Parent))
	for i := uint32(0); i < n; i++ {
		a := ix.Attr[i]
		if a&FlagValid == 0 || a&FlagRemoved != 0 || i == ix.Root {
			continue
		}
		p := ix.Parent[i]
		if p < n && ix.Attr[p]&FlagRemoved != 0 {
			ix.Attr[i] |= FlagRemoved
			continue
		}
		if p >= n || p == i || ix.Attr[p]&(FlagValid|FlagDir) != FlagValid|FlagDir {
			ix.Parent[i] = ix.orphans()
			ix.Attr[i] |= FlagOrphan
			n = uint32(len(ix.Parent))
		}
	}
	ix.Parent[ix.Root] = NoParent
}

// buildTree fills ChildStart/Children and returns the entries in BFS order
// from the root (parents before children).
func (ix *Index) buildTree() []uint32 {
	n := len(ix.Parent)
	start := resize(ix.ChildStart[:0], n+1)
	clear(start)
	for i := 0; i < n; i++ {
		if ix.Valid(uint32(i)) && uint32(i) != ix.Root {
			start[ix.Parent[i]+1]++
		}
	}
	for i := 1; i <= n; i++ {
		start[i] += start[i-1]
	}
	kids := resize(ix.Children[:0], int(start[n]))
	fill := make([]uint32, n)
	copy(fill, start[:n])
	for i := 0; i < n; i++ {
		if ix.Valid(uint32(i)) && uint32(i) != ix.Root {
			p := ix.Parent[i]
			kids[fill[p]] = uint32(i)
			fill[p]++
		}
	}
	ix.ChildStart, ix.Children = start, kids
	order := fill[:0] // reuse the buffer: same capacity n
	order = append(order, ix.Root)
	for head := 0; head < len(order); head++ {
		order = append(order, ix.Kids(order[head])...)
	}
	return order
}

// reattachUnreached moves entries that are not reachable from the root (parent
// cycles in a corrupt MFT) under the orphans node. It reports whether the
// tree must be rebuilt.
func (ix *Index) reattachUnreached(order []uint32) bool {
	valid := 0
	for i := range ix.Parent {
		if ix.Valid(uint32(i)) {
			valid++
		}
	}
	if valid == len(order) {
		return false
	}
	reached := make([]bool, len(ix.Parent))
	for _, i := range order {
		reached[i] = true
	}
	orph := ix.orphans()
	reached = append(reached, true)
	for i := range reached {
		if !reached[i] && ix.Valid(uint32(i)) && reached[ix.Parent[i]] == false {
			ix.Parent[i] = orph
			ix.Attr[i] |= FlagOrphan
		}
	}
	return true
}

func (ix *Index) aggregate(order []uint32) {
	for _, i := range order {
		if ix.Attr[i]&FlagDir != 0 {
			ix.Size[i] = 0
			ix.Alloc[i] = ix.DirOwnAlloc[i]
		}
	}
	for k := len(order) - 1; k > 0; k-- {
		i := order[k]
		p := ix.Parent[i]
		ix.Size[p] += ix.TotalSize(i)
		ix.Alloc[p] += ix.TotalAlloc(i)
	}
}
