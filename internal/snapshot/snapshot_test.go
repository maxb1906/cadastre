package snapshot

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/ntfs/ntfstest"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "v.img")
	os.WriteFile(img, ntfstest.Fixture(t), 0o644)
	res, err := engine.ScanImage(context.Background(), img, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "scan.cad")
	if err := Save(p, res); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	t.Logf("snapshot: %d bytes for %d entries", st.Size(), res.Index.Len())
	back, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	a, b := res.Index, back.Index
	if !back.FromSnapshot || back.Engine != res.Engine || b.RootPath != a.RootPath || b.Len() != a.Len() {
		t.Fatalf("header mismatch")
	}
	if a.Size[a.Root] != b.Size[b.Root] || a.Alloc[a.Root] != b.Alloc[b.Root] {
		t.Fatalf("totals %d/%d vs %d/%d", a.Size[a.Root], a.Alloc[a.Root], b.Size[b.Root], b.Alloc[b.Root])
	}
	i, ok := b.Find(`Users\alice\notes.txt`)
	if !ok || len(b.ADS[i]) != 2 || b.Path(i) != a.Path(i) {
		t.Fatal("notes.txt lost its streams")
	}
	if a.Totals() != b.Totals() {
		t.Fatalf("totals differ:\n%+v\n%+v", a.Totals(), b.Totals())
	}
}

func TestRejectsGarbage(t *testing.T) {
	if _, err := Read(bytes.NewReader([]byte("not a snapshot at all"))); err == nil {
		t.Fatal("garbage accepted")
	}
	var buf bytes.Buffer
	buf.WriteString(magic)
	buf.WriteString("broken zstd")
	if _, err := Read(&buf); err == nil {
		t.Fatal("broken stream accepted")
	}
}

func FuzzRead(f *testing.F) {
	f.Add([]byte(magic))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = Read(bytes.NewReader(append([]byte(magic), b...)))
	})
}
