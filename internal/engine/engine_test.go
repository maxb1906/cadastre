package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxb1906/cadastre/internal/ntfs/ntfstest"
)

func TestNormalizeTarget(t *testing.T) {
	for in, want := range map[string]string{"c": `C:\`, "C:": `C:\`, `d:\`: `D:\`, "c:/": `C:\`, `C:\Users`: `C:\Users`, `\\srv\share`: `\\srv\share`} {
		if got := NormalizeTarget(in); got != want {
			t.Errorf("%q → %q want %q", in, got, want)
		}
	}
}

func TestScanAutoWalksFolders(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x.bin"), make([]byte, 1234), 0o644)
	res, err := Scan(context.Background(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Engine != EngineWalk || res.Index.Size[res.Index.Root] != 1234 {
		t.Fatalf("engine %s size %d", res.Engine, res.Index.Size[res.Index.Root])
	}
	if _, err := Scan(context.Background(), dir, Options{Engine: EngineMFT}); err == nil {
		t.Fatal("MFT engine on a folder must fail")
	}
}

func TestCheckIdentical(t *testing.T) {
	img := filepath.Join(t.TempDir(), "v.img")
	if err := os.WriteFile(img, ntfstest.Fixture(t), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := ScanImage(context.Background(), img, Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := Check(a.Index, a.Index, 5)
	if r.CountOnlyB != 0 || r.CountMismatch != 0 || r.Common == 0 {
		t.Fatalf("%+v", r)
	}
}
