//go:build windows

package clean

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/rules"
)

// TestIntegrationClean deletes real files on the test volume created by
// scripts/new-testvhd.ps1, with both engines and both modes. Run as
// administrator: $env:CADASTRE_TEST_VOLUME = 'T:'; go test ./internal/clean -run Integration -v
func TestIntegrationClean(t *testing.T) {
	vol := os.Getenv("CADASTRE_TEST_VOLUME")
	if vol == "" {
		t.Skip("CADASTRE_TEST_VOLUME not set (see scripts/new-testvhd.ps1)")
	}
	root := filepath.Join(vol+`\`, "cadastre-clean-test")
	os.RemoveAll(root)
	defer os.RemoveAll(root)
	mk := func(rel string, n int) string {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, eng := range []string{engine.EngineMFT, engine.EngineWalk} {
		for _, mode := range []string{ModePermanent, ModeRecycle} {
			t.Run(eng+"/"+mode, func(t *testing.T) {
				os.RemoveAll(root)
				del := mk(`a\delete-me.bin`, 70000)
				changed := mk(`a\changed.bin`, 1000)
				ro := mk(`a\readonly.bin`, 500)
				os.Chmod(ro, 0o444)
				target := mk(`target\keep.bin`, 10)
				if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(root, `a\junction`), filepath.Join(root, "target")).CombinedOutput(); err != nil {
					t.Fatalf("mklink: %v %s", err, out)
				}
				res, err := engine.Scan(context.Background(), vol, engine.Options{Engine: eng})
				if err != nil {
					t.Fatal(err)
				}
				a := analysis.Run(res, rules.MustBuiltin(), analysis.Options{NoSystem: true})
				dir, ok := res.Index.Find(`cadastre-clean-test\a`)
				if !ok {
					t.Fatal("test folder not indexed")
				}
				p := Build(a, Selection{Entries: []uint32{dir}, Mode: mode}, Env{Admin: true})
				Prepare(p, OSFS{})
				os.WriteFile(changed, make([]byte, 2000), 0o644) // modified after the simulation
				r, err := Execute(context.Background(), p, OSFS{}, nil, Options{ConfirmOrange: true})
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("%+v", r)
				if _, err := os.Stat(del); !os.IsNotExist(err) {
					t.Error("delete-me.bin still there")
				}
				if _, err := os.Stat(changed); err != nil {
					t.Error("changed.bin was deleted!")
				}
				if _, err := os.Stat(target); err != nil {
					t.Error("junction target deleted!")
				}
				if _, err := os.Lstat(filepath.Join(root, `a\junction`)); !os.IsNotExist(err) {
					t.Error("junction itself still there")
				}
				if mode == ModePermanent {
					if _, err := os.Stat(ro); !os.IsNotExist(err) {
						t.Error("read-only file not deleted")
					}
				}
			})
		}
	}
}
