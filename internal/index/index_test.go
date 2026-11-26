package index

import (
	"testing"
)

// tree builds:
//
//	C:\
//	├── Windows\            (own index alloc 4096)
//	│   ├── notepad.exe      100 / 4096
//	│   └── Temp\
//	│       ├── a.TMP        10  / 0 (resident)
//	│       └── b.log        5000 / 8192 + ADS 100/4096
//	└── Users\
//	    └── big.ISO          1 GiB
func tree() (*Index, map[string]uint32) {
	ix := New(0)
	ix.RootPath = `C:\`
	ids := map[string]uint32{}
	add := func(key string, parent string, e Entry) {
		if parent != "" {
			e.Parent = ids[parent]
		}
		ids[key] = ix.Append(e)
	}
	add("root", "", Entry{Name: ".", Attr: FlagDir | AttrDirectory})
	ix.Root = ids["root"]
	add("win", "root", Entry{Name: "Windows", Attr: FlagDir | AttrDirectory})
	ix.DirOwnAlloc[ids["win"]] = 4096
	add("notepad", "win", Entry{Name: "notepad.exe", Size: 100, Alloc: 4096})
	add("temp", "win", Entry{Name: "Temp", Attr: FlagDir})
	add("a", "temp", Entry{Name: "a.TMP", Size: 10})
	add("b", "temp", Entry{Name: "b.log", Size: 5000, Alloc: 8192, Attr: FlagADS})
	ix.ADS[ids["b"]] = []Stream{{Name: "Zone.Identifier", Size: 100, Alloc: 4096}}
	add("users", "root", Entry{Name: "Users", Attr: FlagDir})
	add("iso", "users", Entry{Name: "big.ISO", Size: 1 << 30, Alloc: 1 << 30})
	return ix, ids
}

func TestFinalizeAggregates(t *testing.T) {
	ix, id := tree()
	ix.Finalize()
	if got := ix.Size[id["temp"]]; got != 10+5000+100 {
		t.Errorf("Temp size %d", got)
	}
	if got := ix.Alloc[id["win"]]; got != 4096+4096+8192+4096 {
		t.Errorf("Windows alloc %d", got)
	}
	if got := ix.Alloc[id["root"]]; got != 4096+4096+8192+4096+1<<30 {
		t.Errorf("root alloc %d", got)
	}
	// Idempotent.
	ix.Finalize()
	if got := ix.Size[id["temp"]]; got != 5110 {
		t.Errorf("second Finalize: Temp size %d", got)
	}
	if ix.ExtName(id["a"]) != "tmp" || ix.ExtName(id["iso"]) != "iso" || ix.ExtName(id["win"]) != "" {
		t.Errorf("exts %q %q", ix.ExtName(id["a"]), ix.ExtName(id["iso"]))
	}
	if p := ix.Path(id["b"]); p != `C:\Windows\Temp\b.log` {
		t.Errorf("path %q", p)
	}
	if p := ix.Path(ix.Root); p != `C:\` {
		t.Errorf("root path %q", p)
	}
	if f, d := ix.SubtreeCounts(ix.Root); f != 4 || d != 3 {
		t.Errorf("counts %d %d", f, d)
	}
}

func TestFind(t *testing.T) {
	ix, id := tree()
	ix.Finalize()
	for path, want := range map[string]uint32{
		`windows\TEMP\B.LOG`: id["b"],
		`C:\Windows\Temp`:    id["temp"],
		`/Users/big.iso`:     id["iso"],
		`C:`:                 ix.Root,
		``:                   ix.Root,
	} {
		if got, ok := ix.Find(path); !ok || got != want {
			t.Errorf("Find(%q) = %d,%v want %d", path, got, ok, want)
		}
	}
	if _, ok := ix.Find(`Windows\nope`); ok {
		t.Error("found missing path")
	}
}

func TestOrphansAndCycles(t *testing.T) {
	ix, id := tree()
	// Entry whose parent slot is invalid.
	lost := ix.Append(Entry{Parent: 999, Name: "lost.txt", Size: 7})
	// Two directories forming a cycle, with a file under them.
	c1 := ix.Append(Entry{Name: "c1", Attr: FlagDir})
	c2 := ix.Append(Entry{Parent: c1, Name: "c2", Attr: FlagDir})
	ix.Parent[c1] = c2
	f := ix.Append(Entry{Parent: c2, Name: "in-cycle.bin", Size: 3})
	ix.Finalize()
	if ix.Orphans == NoParent || ix.Parent[lost] != ix.Orphans || !ix.Has(lost, FlagOrphan) {
		t.Fatalf("lost.txt not attached to orphans")
	}
	if !ix.IsAncestor(ix.Root, f) {
		t.Fatal("file in cycle not reachable from root")
	}
	if got := ix.Size[ix.Root]; got != 10+5000+100+100+1<<30+7+3 {
		t.Errorf("root size %d", got)
	}
	_ = id
}

func TestMarkRemoved(t *testing.T) {
	ix, id := tree()
	ix.Finalize()
	ix.MarkRemoved(id["temp"])
	ix.Finalize()
	if ix.Valid(id["a"]) || ix.Valid(id["temp"]) {
		t.Fatal("removed entries still valid")
	}
	if got := ix.Size[id["win"]]; got != 100 {
		t.Errorf("Windows size after removal %d", got)
	}
}

func TestTopAndExtensions(t *testing.T) {
	ix, id := tree()
	ix.Finalize()
	top := ix.TopFiles(2, ByAlloc, false)
	if len(top) != 2 || top[0] != id["iso"] || top[1] != id["b"] {
		t.Errorf("top files %v", top)
	}
	dirs := ix.TopDirs(10, ByAlloc)
	if dirs[0].Dir != id["users"] || dirs[1].Dir != id["temp"] || dirs[1].Files != 2 {
		t.Errorf("top dirs %+v", dirs)
	}
	exts := ix.Extensions()
	if exts[0].Ext != "iso" || len(exts) != 4 {
		t.Errorf("exts %+v", exts)
	}
	tot := ix.Totals()
	if tot.Files != 4 || tot.Dirs != 4 || tot.ADSFiles != 1 || tot.ADSAlloc != 4096 {
		t.Errorf("totals %+v", tot)
	}
}

func TestExtBytes(t *testing.T) {
	var buf [16]byte
	for name, want := range map[string]string{
		"a.TXT": "txt", "archive.tar.GZ": "gz", ".gitignore": "", "noext": "", "trail.": "",
		"x.averyveryverylongextension": "", "photo.jpég": "", "space. bar": "",
	} {
		if got := string(extBytes([]byte(name), &buf)); got != want {
			t.Errorf("%q: got %q want %q", name, got, want)
		}
	}
}

func TestParallelExtensions(t *testing.T) {
	ix := New(0)
	ix.Append(Entry{Name: ".", Attr: FlagDir})
	exts := []string{"dll", "exe", "txt", "log", "DLL"}
	for i := 0; i < 200000; i++ {
		ix.Append(Entry{Name: "f." + exts[i%len(exts)], Size: 1})
	}
	ix.Finalize()
	st := ix.Extensions()
	if len(st) != 4 {
		t.Fatalf("got %d extensions: %+v", len(st), st)
	}
	for _, s := range st {
		if s.Ext == "dll" && s.Files != 80000 {
			t.Errorf("dll files %d", s.Files)
		}
	}
}
