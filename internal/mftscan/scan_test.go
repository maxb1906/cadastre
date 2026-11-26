package mftscan

import (
	"bytes"
	"context"
	"testing"

	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/ntfs"
	"github.com/maxb1906/cadastre/internal/ntfs/ntfstest"
)

func scanFixture(t testing.TB, opt Options) (*index.Index, Stats) {
	img := ntfstest.Fixture(t)
	vi, err := ntfs.ParseBootSector(img)
	if err != nil {
		t.Fatal(err)
	}
	ix, st, err := Scan(context.Background(), bytes.NewReader(img), vi, opt)
	if err != nil {
		t.Fatal(err)
	}
	ix.RootPath = `T:\`
	return ix, st
}

func mustFind(t *testing.T, ix *index.Index, p string) uint32 {
	t.Helper()
	i, ok := ix.Find(p)
	if !ok {
		t.Fatalf("%s not found", p)
	}
	return i
}

func TestScanFixture(t *testing.T) {
	for _, chunk := range []int{4096, 64 << 10, 0} {
		ix, st := scanFixture(t, Options{ChunkBytes: chunk})
		if st.Bad != 0 {
			t.Errorf("chunk %d: %d bad records", chunk, st.Bad)
		}
		notes := mustFind(t, ix, `Users\alice\notes.txt`)
		if ix.Size[notes] != 10000 || !ix.Has(notes, index.FlagADS) || len(ix.ADS[notes]) != 2 {
			t.Errorf("notes.txt size=%d ads=%v", ix.Size[notes], ix.ADS[notes])
		}
		if got := ix.TotalSize(notes); got != 10000+30+200000 {
			t.Errorf("notes.txt total %d", got)
		}
		// One of the two names is primary, the other is recorded as a link;
		// the size is counted once.
		big, ok := ix.Find(`Windows\Temp\big.log`)
		other := "hardlink.log"
		if !ok {
			big = mustFind(t, ix, `Users\alice\hardlink.log`)
			other = "big.log"
		}
		if !ix.Has(big, index.FlagHardlink) || len(ix.Links[big]) != 1 || ix.Links[big][0].Name != other {
			t.Errorf("hard link: %+v", ix.Links[big])
		}
		alice := mustFind(t, ix, `Users\alice`)
		temp := mustFind(t, ix, `Windows\Temp`)
		if got := ix.Size[alice] + ix.Size[temp]; got != 10000+30+200000+300+1<<20 {
			t.Errorf("alice+temp size %d", got)
		}
		sp := mustFind(t, ix, `sparse.bin`)
		if ix.Size[sp] != 100<<20 || ix.Alloc[sp] != 4096 || !ix.Has(sp, index.AttrSparse) {
			t.Errorf("sparse.bin %d/%d attr %x", ix.Size[sp], ix.Alloc[sp], ix.Attr[sp])
		}
		z := mustFind(t, ix, `compressed\zeros.bin`)
		if !ix.Has(z, index.AttrCompressed) || ix.Alloc[z] >= ix.Size[z] {
			t.Errorf("zeros.bin %d/%d attr %x", ix.Size[z], ix.Alloc[z], ix.Attr[z])
		}
		j := mustFind(t, ix, `deep\junction`)
		if !ix.Has(j, index.AttrReparsePoint) || !ix.IsDir(j) {
			t.Errorf("junction attr %x", ix.Attr[j])
		}
		mustFind(t, ix, `Données été\日本語 😀.txt`)
		many := mustFind(t, ix, `many`)
		if f, _ := ix.SubtreeCounts(many); f != 6000 {
			t.Errorf("many has %d files", f)
		}
		if len(ix.Path(mustFind(t, ix, `deep`))) == 0 {
			t.Error("empty path")
		}
		mft := mustFind(t, ix, `$MFT`)
		if !ix.Has(mft, index.FlagMeta) || ix.Alloc[mft] < st.MFTBytes {
			t.Errorf("$MFT meta=%v alloc=%d", ix.Has(mft, index.FlagMeta), ix.Alloc[mft])
		}
		if usn, ok := ix.Find(`$Extend\$ObjId`); ok && !ix.Has(usn, index.FlagMeta) {
			t.Error("$Extend children should be metafiles")
		}
		tot := ix.Totals()
		if tot.Orphans != 0 {
			t.Errorf("%d orphans", tot.Orphans)
		}
		var longest string
		ix.Walk(ix.Root, func(i uint32) bool {
			if p := ix.Path(i); len(p) > len(longest) {
				longest = p
			}
			return true
		})
		if len(longest) < 260 {
			t.Errorf("longest path only %d chars", len(longest))
		}
	}
}

func TestScanMinSize(t *testing.T) {
	ix, _ := scanFixture(t, Options{MinSize: 1000})
	many := mustFind(t, ix, `many`)
	if f, _ := ix.SubtreeCounts(many); f != 6000 {
		t.Errorf("folded files must still be counted, got %d", f)
	}
	folded := 0
	for _, k := range ix.Kids(many) {
		if ix.Has(k, index.FlagFolded) && ix.NameLen[k] == 0 {
			folded++
		}
	}
	if folded != 6000 {
		t.Errorf("folded %d", folded)
	}
	if ix.Size[mustFind(t, ix, `Windows`)]+ix.Size[mustFind(t, ix, `Users`)] < 1<<20 {
		t.Error("big.log lost")
	}
}

func TestScanSurvivesCorruption(t *testing.T) {
	img := ntfstest.Fixture(t)
	vi, _ := ntfs.ParseBootSector(img)
	l, err := ntfs.ReadLayout(bytes.NewReader(img), vi)
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt every 7th record (except the first 16) in place.
	off := int(l.Runs[0].LCN) * int(vi.BytesPerCluster)
	for r := 16; r < int(l.RecordCount); r += 7 {
		p := off + r*int(vi.BytesPerRecord)
		for k := 0x30; k < 0x200; k += 13 {
			img[p+k] ^= 0x5A
		}
	}
	ix, st, err := Scan(context.Background(), bytes.NewReader(img), vi, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Bad == 0 {
		t.Error("expected bad records")
	}
	if !ix.Valid(ix.Root) {
		t.Error("root lost")
	}
}
