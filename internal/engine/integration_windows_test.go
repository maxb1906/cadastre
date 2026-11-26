//go:build windows

package engine

import (
	"context"
	"os"
	"testing"
)

// TestIntegrationOracle compares engine A and engine C on a real NTFS
// volume created by scripts/new-testvhd.ps1. Run as administrator:
//
//	$env:CADASTRE_TEST_VOLUME = 'T:'; go test ./internal/engine -run Integration -v
func TestIntegrationOracle(t *testing.T) {
	vol := os.Getenv("CADASTRE_TEST_VOLUME")
	if vol == "" {
		t.Skip("CADASTRE_TEST_VOLUME not set (see scripts/new-testvhd.ps1)")
	}
	ctx := context.Background()
	a, err := Scan(ctx, vol, Options{Engine: EngineMFT})
	if err != nil {
		t.Fatalf("engine A: %v", err)
	}
	c, err := Scan(ctx, vol, Options{Engine: EngineWalk})
	if err != nil {
		t.Fatalf("engine C: %v", err)
	}
	t.Logf("A: %d entries in %v (%d MFT records, %d runs); C: %d entries in %v",
		a.Index.Len(), a.Duration, a.MFTRecords, a.MFTRuns, c.Index.Len(), c.Duration)
	r := Check(a.Index, c.Index, 20)
	t.Logf("common %d, only A %d, only C %d, mismatches %d", r.Common, r.CountOnlyA, r.CountOnlyB, r.CountMismatch)
	for _, d := range r.OnlyA {
		t.Logf("  only A: %s (%d)", d.Path, d.SizeA)
	}
	for _, d := range r.OnlyB {
		t.Logf("  only C: %s (%d)", d.Path, d.SizeB)
	}
	for _, d := range r.Mismatch {
		t.Logf("  size: %s A=%d C=%d", d.Path, d.SizeA, d.SizeB)
	}
	if !r.OK() {
		t.Error("engines disagree")
	}
	ix := a.Index
	checks := map[string]func(i uint32) bool{
		`data\notes.txt`:  func(i uint32) bool { s, _ := ix.ADSTotals(i); return ix.Size[i] == 10000 && s == 200000 },
		`data\big.bin`:    func(i uint32) bool { return ix.Size[i] == 50<<20 },
		`data\sparse.bin`: func(i uint32) bool { return ix.Size[i] == 1<<30 && ix.Alloc[i] < 1<<20 },
		`data\junction`:   func(i uint32) bool { return len(ix.Kids(i)) == 0 },
	}
	for p, ok := range checks {
		i, found := ix.Find(p)
		if !found || !ok(i) {
			t.Errorf("%s: unexpected (found=%v)", p, found)
		}
	}
}
