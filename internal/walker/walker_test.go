package walker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxb1906/cadastre/internal/index"
)

func TestWalk(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, size int) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("a/b/c.txt", 5000)
	mk("a/d.log", 100)
	mk("e.bin", 70000)
	for i := 0; i < 500; i++ {
		mk(filepath.Join("many", "f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".dat"), i)
	}
	os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "link-to-a"))
	os.Link(filepath.Join(root, "e.bin"), filepath.Join(root, "a", "e-hard.bin"))
	os.MkdirAll(filepath.Join(root, "locked", "inner"), 0o755)
	mk("locked/inner/secret", 10)
	os.Chmod(filepath.Join(root, "locked"), 0)
	defer os.Chmod(filepath.Join(root, "locked"), 0o755)

	ix, st, err := Walk(context.Background(), root, Options{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if i, ok := ix.Find("a/b/c.txt"); !ok || ix.Size[i] != 5000 || ix.Path(i) != filepath.Join(root, "a/b/c.txt") {
		t.Fatalf("c.txt: %v", ok)
	}
	link, ok := ix.Find("link-to-a")
	if !ok || !ix.Has(link, index.AttrReparsePoint) || len(ix.Kids(link)) != 0 {
		t.Fatal("symlink must be recorded, not followed")
	}
	var want uint64 = 5000 + 100 + 70000
	for i := 0; i < 500; i++ {
		want += uint64(i)
	}
	if os.Geteuid() != 0 {
		locked, _ := ix.Find("locked")
		if !ix.Has(locked, index.FlagDenied) || st.Denied != 1 {
			t.Errorf("locked dir not flagged denied (denied=%d)", st.Denied)
		}
	} else {
		want += 10
	}
	// Hard link counted once; the symlink has its own (tiny) size.
	got := ix.Size[ix.Root] - ix.Size[link]
	if got != want {
		t.Errorf("total %d want %d", got, want)
	}
	if f, _ := ix.SubtreeCounts(ix.Root); f < 503 {
		t.Errorf("files %d", f)
	}
}

func TestWalkCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Walk(ctx, t.TempDir(), Options{}); err == nil {
		t.Fatal("expected error")
	}
}
