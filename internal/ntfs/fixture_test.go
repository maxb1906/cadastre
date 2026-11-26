package ntfs_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/maxb1906/cadastre/internal/ntfs"
	"github.com/maxb1906/cadastre/internal/ntfs/ntfstest"
)

type parsed struct {
	recNo  uint64
	name   string
	parent uint64
	dir    bool
	size   uint64
	alloc  uint64
	ads    map[string]uint64
	tag    uint32
	links  int
}

func parseFixture(t *testing.T) (map[string][]parsed, *ntfs.Layout) {
	img := ntfstest.Fixture(t)
	vi, err := ntfs.ParseBootSector(img)
	if err != nil {
		t.Fatal(err)
	}
	l, err := ntfs.ReadLayout(bytes.NewReader(img), vi)
	if err != nil {
		t.Fatal(err)
	}
	sr := ntfs.NewStreamReader(bytes.NewReader(img), l.Runs, vi.BytesPerCluster, l.MFTSize())
	all := make([]byte, l.MFTSize())
	if _, err := sr.ReadAt(all, 0); err != nil {
		t.Fatal(err)
	}
	byName := map[string][]parsed{}
	var rec ntfs.Record
	rs := uint64(vi.BytesPerRecord)
	for i := uint64(0); i < l.RecordCount; i++ {
		err := ntfs.Parse(all[i*rs:(i+1)*rs], i, &rec, ntfs.ParseOptions{})
		if errors.Is(err, ntfs.ErrNotInUse) {
			continue
		}
		if err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		if !rec.IsBase() {
			continue
		}
		fn, ok := rec.BestName()
		if !ok {
			continue
		}
		p := parsed{recNo: i, name: string(rec.Name(fn)), parent: fn.ParentRec, dir: rec.IsDir(), ads: map[string]uint64{}, tag: rec.ReparseTag}
		for _, n := range rec.Names {
			if n.Namespace != ntfs.NamespaceDOS {
				p.links++
			}
		}
		for _, s := range rec.Streams {
			if name := string(rec.StreamName(s)); name == "" {
				p.size, p.alloc = s.Size, s.Alloc
			} else {
				p.ads[name] = s.Size
			}
		}
		for _, n := range rec.Names {
			if n.Namespace != ntfs.NamespaceDOS || len(rec.Names) == 1 {
				byName[string(rec.Name(n))] = append(byName[string(rec.Name(n))], p)
			}
		}
	}
	return byName, l
}

func one(t *testing.T, m map[string][]parsed, name string) parsed {
	t.Helper()
	if len(m[name]) != 1 {
		t.Fatalf("%q: found %d records", name, len(m[name]))
	}
	return m[name][0]
}

func TestRealNTFSFixture(t *testing.T) {
	m, l := parseFixture(t)
	if l.RecordCount < 3000 {
		t.Fatalf("expected MFT to have grown, got %d records", l.RecordCount)
	}
	t.Logf("MFT: %d records in %d runs", l.RecordCount, len(l.Runs))

	if p := one(t, m, "$MFT"); p.recNo != 0 || p.parent != ntfs.RecRoot {
		t.Errorf("$MFT %+v", p)
	}
	if p := one(t, m, "."); p.recNo != ntfs.RecRoot || !p.dir {
		t.Errorf("root %+v", p)
	}
	notes := one(t, m, "notes.txt")
	if notes.size != 10000 || notes.ads["secret"] != 30 || notes.ads["bigads"] != 200000 {
		t.Errorf("notes.txt %+v", notes)
	}
	if p := one(t, m, "small.tmp"); p.size != 300 || p.alloc != 0 {
		t.Errorf("small.tmp should be resident: %+v", p)
	}
	big := one(t, m, "big.log")
	if big.size != 1<<20 || big.alloc != 1<<20 || big.links != 2 {
		t.Errorf("big.log %+v", big)
	}
	if one(t, m, "hardlink.log").recNo != big.recNo {
		t.Error("hard link should share the record of big.log")
	}
	if p := one(t, m, "sparse.bin"); p.size != 100<<20 || p.alloc != 4096 {
		t.Errorf("sparse.bin %+v", p)
	}
	if p := one(t, m, "zeros.bin"); p.size != 262144 || p.alloc >= 262144 {
		t.Errorf("zeros.bin should be compressed: %+v", p)
	}
	if p := one(t, m, "junction"); p.tag != ntfs.ReparseTagMountPoint || !p.dir {
		t.Errorf("junction %+v", p)
	}
	one(t, m, "日本語 😀.txt")
	one(t, m, "Données été")
	one(t, m, "fond.txt")
	many := one(t, m, "many")
	for _, name := range []string{"f1.txt", "f1500.txt", "f6000.txt"} {
		if p := one(t, m, name); p.parent != many.recNo {
			t.Errorf("%s parent %d want %d", name, p.parent, many.recNo)
		}
	}
}
