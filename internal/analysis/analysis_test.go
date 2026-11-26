package analysis

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/rules"
)

func TestRuleReclaimable(t *testing.T) {
	cat := rules.MustBuiltin()
	want := map[string]bool{
		"memory-dumps":          true,  // green, delete
		"windows-update-cache":  true,  // orange, delete (official too)
		"delivery-optimization": true,  // orange, official only
		"component-store":       false, // red: official action offered, never counted
		"installer-orphans":     false, // red, report only
		"pagefile":              false, // report only
	}
	for id, w := range want {
		r, ok := cat.Get(id)
		if !ok {
			t.Fatalf("rule %s missing", id)
		}
		if r.Reclaimable() != w {
			t.Errorf("%s: Reclaimable() = %v, want %v", id, r.Reclaimable(), w)
		}
	}
}

func TestAnalysisReclaimable(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, size int) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("Windows/MEMORY.DMP", 50000)                      // green
	mk("Windows/WinSxS/amd64_a/a.dll", 9000)             // red, official
	mk("Windows/Installer/orphan.msi", 10000)            // red, none
	mk("Users/bob/proj/node_modules/lib/big.js", 11<<20) // green
	mk("pagefile.sys", 800)                              // report only
	res, err := engine.Scan(context.Background(), root, engine.Options{Engine: engine.EngineWalk})
	if err != nil {
		t.Fatal(err)
	}
	a := Run(res, rules.MustBuiltin(), Options{NoSystem: true})
	var want, other uint64
	seen := map[string]bool{}
	for _, f := range a.Findings {
		seen[f.Rule.ID] = true
		if f.Rule.Reclaimable() {
			want += f.Alloc
		} else {
			other += f.Alloc
		}
	}
	if !seen["memory-dumps"] || !seen["node-modules"] || !seen["component-store"] {
		t.Fatalf("unexpected findings: %v", seen)
	}
	if other == 0 {
		t.Fatal("expected non-reclaimable findings in the fixture")
	}
	if got := a.Reclaimable(); got != want || got == 0 {
		t.Errorf("Reclaimable() = %d, want %d", got, want)
	}
}
