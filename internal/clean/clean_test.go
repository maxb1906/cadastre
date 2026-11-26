package clean

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/rules"
)

type testVol struct {
	root string
	t    *testing.T
}

func (v testVol) mk(rel string, size int, age time.Duration) string {
	p := filepath.Join(v.root, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, bytes.Repeat([]byte{'x'}, size), 0o644); err != nil {
		v.t.Fatal(err)
	}
	if age > 0 {
		old := time.Now().Add(-age)
		os.Chtimes(p, old, old)
	}
	return p
}

func newVolume(t *testing.T) testVol {
	v := testVol{root: t.TempDir(), t: t}
	week := 10 * 24 * time.Hour
	v.mk("Windows/Temp/old1.tmp", 1000, week)
	v.mk("Windows/Temp/sub/old2.tmp", 2000, week)
	v.mk("Windows/Temp/fresh.tmp", 3000, 0)
	v.mk("Windows/WinSxS/amd64_a/a.dll", 4000, 0)
	v.mk("Windows/System32/kernel.dll", 5000, 0)
	v.mk("Windows/MEMORY.DMP", 6000, 0)
	v.mk("Users/bob/Videos/film.mp4", 7000, 0)
	v.mk("Users/bob/proj/node_modules/lib/big.js", 11<<20, 0)
	v.mk("Users/bob/proj/node_modules/lib/small.js", 100, 0)
	v.mk("pagefile.sys", 800, 0)
	v.mk("data/keep.txt", 10, 0)
	os.Symlink(filepath.Join(v.root, "data"), filepath.Join(v.root, "Users/bob/link-to-data"))
	return v
}

func analyze(t *testing.T, root string) *analysis.Analysis {
	res, err := engine.Scan(context.Background(), root, engine.Options{Engine: engine.EngineWalk})
	if err != nil {
		t.Fatal(err)
	}
	return analysis.Run(res, rules.MustBuiltin(), analysis.Options{NoSystem: true})
}

func reasons(p *Plan) map[string]int {
	m := map[string]int{}
	for _, s := range p.Skipped {
		m[s.Reason]++
	}
	return m
}

func paths(items []Item) string {
	var out []string
	for _, it := range items {
		out = append(out, filepath.Base(it.Path))
	}
	return strings.Join(out, ",")
}

func TestPlanGuardrails(t *testing.T) {
	v := newVolume(t)
	a := analyze(t, v.root)
	ix := a.Result.Index
	find := func(rel string) uint32 {
		i, ok := ix.Find(rel)
		if !ok {
			t.Fatalf("%s not indexed", rel)
		}
		return i
	}
	env := Env{Admin: true}

	// Temp only allows permanent deletion.
	p := Build(a, Selection{Rules: []string{"temp"}}, env)
	if len(p.Items) != 0 || reasons(p)[ReasonNeedPermanent] != 2 {
		t.Errorf("recycle mode on temp: items=%s skipped=%v", paths(p.Items), reasons(p))
	}
	p = Build(a, Selection{Rules: []string{"temp"}, Mode: ModePermanent}, env)
	if paths(p.Items) != "old1.tmp,old2.tmp" || p.Files != 2 || p.Orange {
		t.Errorf("temp permanent: %s files=%d", paths(p.Items), p.Files)
	}
	// Without admin, system rules are greyed out.
	if p := Build(a, Selection{Rules: []string{"temp"}, Mode: ModePermanent}, Env{}); len(p.Items) != 0 || reasons(p)[ReasonAdmin] == 0 {
		t.Errorf("temp without admin: %v", reasons(p))
	}
	// Red rule: never deleted; official only when asked.
	p = Build(a, Selection{Rules: []string{"component-store"}}, env)
	if len(p.Items) != 0 || len(p.Official) != 0 || reasons(p)[ReasonOfficialOnly] != 1 {
		t.Errorf("WinSxS: items=%d official=%d %v", len(p.Items), len(p.Official), reasons(p))
	}
	p = Build(a, Selection{Rules: []string{"component-store"}, Official: true}, env)
	if len(p.Items) != 0 || len(p.Official) != 1 || len(p.Official[0].Items) != 0 || p.Official[0].Commands[0][0] != "Dism.exe" {
		t.Errorf("WinSxS official: %+v", p.Official)
	}
	// Report-only rule.
	if p := Build(a, Selection{Rules: []string{"pagefile"}}, env); len(p.Items) != 0 || reasons(p)[ReasonReportOnly] != 1 {
		t.Errorf("pagefile: %v", reasons(p))
	}
	// Whole node_modules: files first, then folders deepest first.
	p = Build(a, Selection{Rules: []string{"node-modules"}}, env)
	if paths(p.Items) != "big.js,small.js,lib,node_modules" {
		t.Errorf("node_modules order: %s", paths(p.Items))
	}
	// Manual selections: blacklist without rule exception.
	p = Build(a, Selection{Entries: []uint32{
		find("Windows/Temp/fresh.tmp"), find("Windows/System32/kernel.dll"), find("pagefile.sys"),
		find("Users/bob"), find("Users/bob/Videos/film.mp4"), find("Users/bob/link-to-data"), ix.Root,
	}}, env)
	r := reasons(p)
	if paths(p.Items) != "film.mp4,link-to-data" || !p.Orange || r[ReasonWindows] != 1 || r[ReasonSystem] != 1 ||
		r[ReasonSystemFile] != 1 || r[ReasonProfile] != 1 || r[ReasonVolumeRoot] != 1 {
		t.Errorf("manual: items=%s reasons=%v", paths(p.Items), r)
	}
	if !p.Items[1].Reparse {
		t.Error("link must be flagged as reparse point")
	}
	// Ancestor already selected: no duplicate.
	p = Build(a, Selection{Entries: []uint32{find("Users/bob/proj"), find("Users/bob/proj/node_modules/lib/big.js")}}, env)
	if strings.Count(paths(p.Items), "big.js") != 1 {
		t.Errorf("duplicate: %s", paths(p.Items))
	}
}

func TestExecuteRevalidatesAndJournals(t *testing.T) {
	v := newVolume(t)
	a := analyze(t, v.root)
	p := Build(a, Selection{Rules: []string{"temp", "node-modules"}, Mode: ModePermanent}, Env{Admin: true})
	if _, err := Execute(context.Background(), p, OSFS{}, nil, Options{}); !errors.Is(err, ErrNotPrepared) {
		t.Fatalf("execution without simulation: %v", err)
	}
	// Changed after the scan: caught by the simulation.
	v.mk("Windows/Temp/old1.tmp", 1500, 10*24*time.Hour)
	Prepare(p, OSFS{})
	if reasons(p)[ReasonChanged] != 1 || strings.Contains(paths(p.Items), "old1.tmp") {
		t.Fatalf("prepare: %v %s", reasons(p), paths(p.Items))
	}
	for _, it := range p.Items {
		if it.FileID == 0 {
			t.Fatalf("file number not captured for %s", it.Path)
		}
	}
	// Replaced between simulation and execution: caught by revalidation.
	small := filepath.Join(v.root, "Users/bob/proj/node_modules/lib/small.js")
	os.Remove(small)
	os.WriteFile(small, []byte("other content!!"), 0o644)

	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Execute(context.Background(), p, OSFS{}, j, Options{})
	j.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(v.root, "Windows/Temp/sub/old2.tmp")); !os.IsNotExist(err) {
		t.Error("old2.tmp not deleted")
	}
	if _, err := os.Stat(filepath.Join(v.root, "Windows/Temp/old1.tmp")); err != nil {
		t.Error("changed file deleted!")
	}
	if _, err := os.Stat(small); err != nil {
		t.Error("replaced file deleted!")
	}
	if _, err := os.Stat(filepath.Join(v.root, "Users/bob/proj/node_modules/lib/big.js")); !os.IsNotExist(err) {
		t.Error("big.js not deleted")
	}
	if _, err := os.Stat(filepath.Join(v.root, "Users/bob/proj/node_modules")); err != nil {
		t.Error("node_modules removed although not empty")
	}
	if _, err := os.Stat(filepath.Join(v.root, "Windows/Temp/sub")); err != nil {
		t.Error("Temp/sub is a planned folder? it should still be there (not part of the temp items)")
	}
	if res.Done != 2 || res.Freed < 11<<20 {
		t.Errorf("result %+v", res)
	}
	// Journal: one JSON object per line, with the end marker.
	f, _ := os.Open(res.Journal)
	defer f.Close()
	sc := bufio.NewScanner(f)
	var actions []string
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("journal line %q: %v", sc.Text(), err)
		}
		if e.Session != p.ID {
			t.Errorf("session %q", e.Session)
		}
		actions = append(actions, e.Action+":"+e.Result)
	}
	j2 := strings.Join(actions, " ")
	if !strings.HasPrefix(j2, "plan:ok") || !strings.HasSuffix(j2, "end:ok") || !strings.Contains(j2, "delete:skipped") || !strings.Contains(j2, "rmdir:skipped") {
		t.Errorf("journal actions: %s", j2)
	}
}

func TestSymlinkIsDeletedNotFollowed(t *testing.T) {
	v := newVolume(t)
	a := analyze(t, v.root)
	link, _ := a.Result.Index.Find("Users/bob/link-to-data")
	p := Build(a, Selection{Entries: []uint32{link}, Mode: ModePermanent}, Env{Admin: true})
	Prepare(p, OSFS{})
	res, err := Execute(context.Background(), p, OSFS{}, nil, Options{ConfirmOrange: true})
	if err != nil || res.Done != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	if _, err := os.Lstat(filepath.Join(v.root, "Users/bob/link-to-data")); !os.IsNotExist(err) {
		t.Error("link still there")
	}
	if _, err := os.Stat(filepath.Join(v.root, "data/keep.txt")); err != nil {
		t.Error("link target was deleted!")
	}
}

func TestOrangeNeedsConfirmationAndRecycleUnavailable(t *testing.T) {
	v := newVolume(t)
	a := analyze(t, v.root)
	film, _ := a.Result.Index.Find("Users/bob/Videos/film.mp4")
	p := Build(a, Selection{Entries: []uint32{film}}, Env{})
	Prepare(p, OSFS{})
	res, _ := Execute(context.Background(), p, OSFS{}, nil, Options{})
	if res.Skipped != 1 || res.Done != 0 {
		t.Errorf("unconfirmed orange: %+v", res)
	}
	// Recycle bin unavailable here: the file must stay.
	res, _ = Execute(context.Background(), p, OSFS{}, nil, Options{ConfirmOrange: true})
	if res.Failed != 1 {
		t.Errorf("recycle: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(v.root, "Users/bob/Videos/film.mp4")); err != nil {
		t.Error("file deleted although the recycle bin failed")
	}
}

func TestCancel(t *testing.T) {
	v := newVolume(t)
	a := analyze(t, v.root)
	p := Build(a, Selection{Rules: []string{"node-modules"}, Mode: ModePermanent}, Env{Admin: true})
	Prepare(p, OSFS{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Execute(ctx, p, OSFS{}, nil, Options{})
	if err != nil || !res.Canceled || res.Done != 0 {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestOfficialActions(t *testing.T) {
	v := newVolume(t)
	v.mk("Windows/SoftwareDistribution/Download/a/pkg.cab", 5000, 0)
	v.mk("Windows/SoftwareDistribution/Download/b.esd", 6000, 0)
	a := analyze(t, v.root)
	p := Build(a, Selection{Rules: []string{"windows-update-cache", "component-store"}, Official: true, Mode: ModeRecycle}, Env{Admin: true})
	if len(p.Official) != 2 || len(p.Items) != 0 {
		t.Fatalf("official: %+v items=%s", p.Official, paths(p.Items))
	}
	Prepare(p, OSFS{})
	var ran []string
	restore := 0
	res, err := Execute(context.Background(), p, OSFS{}, nil, Options{ConfirmOrange: true, RestorePoint: true,
		Runner: func(_ context.Context, args []string) (string, error) {
			ran = append(ran, strings.Join(args, " "))
			return "ok", nil
		},
		RestorePointer: func(context.Context) (string, error) { restore++; return "", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(ran, " | ")
	if got != "net stop wuauserv | net stop bits | net start bits | net start wuauserv | Dism.exe /Online /Cleanup-Image /StartComponentCleanup" {
		t.Errorf("commands: %s", got)
	}
	if restore != 1 {
		t.Errorf("restore point created %d times", restore)
	}
	if _, err := os.Stat(filepath.Join(v.root, "Windows/SoftwareDistribution/Download/b.esd")); !os.IsNotExist(err) {
		t.Error("WU cache not emptied between the service steps")
	}
	if _, err := os.Stat(filepath.Join(v.root, "Windows/SoftwareDistribution/Download")); err != nil {
		t.Error("Download folder itself must be kept")
	}
	if _, err := os.Stat(filepath.Join(v.root, "Windows/WinSxS/amd64_a/a.dll")); err != nil {
		t.Error("WinSxS touched!")
	}
	if len(res.Rescan) == 0 {
		t.Error("no zone to rescan")
	}
}

func TestPlaceholdersAndScript(t *testing.T) {
	if got := expandPlaceholders("{logname}", `C:\Windows\System32\winevt\Logs\Microsoft-Windows-Foo%4Operational.evtx`, `C:\`); got != "Microsoft-Windows-Foo/Operational" {
		t.Errorf("logname %q", got)
	}
	if got := expandPlaceholders("Optimize-VHD -Path {path_ps}", `D:\VMs\it's.vhdx`, `D:\`); got != `Optimize-VHD -Path 'D:\VMs\it''s.vhdx'` {
		t.Errorf("path_ps %q", got)
	}
	if got := expandPlaceholders("/d {drive}", "", `C:\`); got != "/d C:" {
		t.Errorf("drive %q", got)
	}
	v := newVolume(t)
	a := analyze(t, v.root)
	p := Build(a, Selection{Rules: []string{"temp", "component-store"}, Mode: ModePermanent, Official: true}, Env{Admin: true})
	var buf bytes.Buffer
	if err := WriteScript(&buf, p, "fr"); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{"param(", "Remove-Checked '", "old1.tmp' 1000 ", "if ($Apply -and $Official)", "& 'Dism.exe'", "Checkpoint-Computer", "\r\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	if !strings.HasPrefix(s, "\uFEFF") {
		t.Error("script without BOM")
	}
}
