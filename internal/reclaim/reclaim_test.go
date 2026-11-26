package reclaim

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/ntfs/ntfstest"
)

func TestReconcileRealImage(t *testing.T) {
	img := filepath.Join(t.TempDir(), "v.img")
	if err := os.WriteFile(img, ntfstest.Fixture(t), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := engine.ScanImage(context.Background(), img, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := Compute(res, VSSInfo{})
	if r.Used == 0 || r.Free == 0 {
		t.Fatalf("no usage: %+v", r)
	}
	meta := r.Get(Meta)
	if meta.Bytes == 0 || len(meta.Details) == 0 || meta.Details[0].Name == "" {
		t.Errorf("meta %+v", meta)
	}
	if r.Get(ADS).Bytes == 0 {
		t.Error("ADS of notes.txt not counted")
	}
	// On an unmodified image the MFT scan must explain the used space to the
	// cluster: every allocated cluster belongs to a file, a stream, a
	// directory index or a metafile ($Boot, $BadClus...).
	if r.Unattributed != 0 {
		for _, c := range r.Components {
			t.Logf("%-12s %10d %v", c.Key, c.Bytes, c.Details)
		}
		t.Errorf("unattributed %d bytes (used %d, seen %d)", r.Unattributed, r.Used, r.Seen)
	}
}

func TestReconcileWalkUsesWMI(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), make([]byte, 10000), 0o644)
	res, err := engine.Scan(context.Background(), dir, engine.Options{Engine: engine.EngineWalk})
	if err != nil {
		t.Fatal(err)
	}
	r := Compute(res, VSSInfo{Known: true, Allocated: 5 << 30, Used: 4 << 30, Copies: 3})
	if v := r.Get(VSS); v.Source != "wmi" || v.Bytes != 5<<30 {
		t.Errorf("vss %+v", v)
	}
	if len(r.Notes) == 0 || r.Notes[0] != "reclaim.note.walk" {
		t.Errorf("notes %v", r.Notes)
	}
}
