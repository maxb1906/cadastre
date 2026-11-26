package rules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/walker"
)

type fakeEnv struct {
	accounts map[string]string
	profiles []Profile
	refs     map[string]bool
}

func (e fakeEnv) LookupSID(sid string) (string, bool, error) {
	a, ok := e.accounts[sid]
	return a, ok, nil
}
func (e fakeEnv) Profiles() ([]Profile, error)                  { return e.profiles, nil }
func (e fakeEnv) InstallerReferences() (map[string]bool, error) { return e.refs, nil }

// fakeVolume builds a Windows-like tree in a temp dir and walks it.
func fakeVolume(t *testing.T) (*index.Index, string) {
	root := t.TempDir()
	old := time.Now().Add(-60 * 24 * time.Hour)
	mk := func(rel string, size int, aged bool) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		if aged {
			os.Chtimes(p, old, old)
		}
	}
	mk("Windows/MEMORY.DMP", 50000, false)
	mk("Windows/Minidump/a.dmp", 1000, false)
	mk("Windows/Temp/old.tmp", 2000, true)
	mk("Windows/Temp/new.tmp", 3000, false)
	mk("Windows/Temp/sub/older.log", 4000, true)
	mk("Windows/SoftwareDistribution/Download/abc/pkg.cab", 7000, false)
	mk("Windows/SoftwareDistribution/Download/x.esd", 8000, false)
	mk("Windows/WinSxS/amd64_foo/foo.dll", 9000, false)
	mk("Windows/Installer/1a2b.msi", 10000, false)
	mk("Windows/Installer/kept.msi", 11000, false)
	mk("Windows/Installer/$PatchCache$/x.msp", 12000, false)
	mk("Users/alice/AppData/Local/Temp/t.tmp", 100, true)
	mk("Users/alice/AppData/Local/CrashDumps/app.dmp", 200, false)
	mk("Users/alice/proj/node_modules/lib/index.js", 11<<20, false)
	mk("Users/alice/proj/node_modules/lib/node_modules/dep/x.js", 1000, false)
	mk("Users/alice/AppData/Local/Google/Chrome/User Data/Default/Cache/Cache_Data/f_0001", 500, false)
	mk("Users/alice/AppData/Local/Google/Chrome/User Data/Profile 1/Code Cache/js/x", 600, false)
	mk("Users/ghost/ntuser.dat", 1300, false)
	mk("Users/Public/x.txt", 10, false)
	mk("$Recycle.Bin/S-1-5-21-1/$R123.zip", 5000, false)
	mk("$Recycle.Bin/S-1-5-21-2/$Rdir/a", 6000, false)
	mk("inetpub/logs/LogFiles/W3SVC1/u_ex2001.log", 700, true)
	mk("inetpub/logs/LogFiles/W3SVC1/u_ex2611.log", 800, false)
	mk("tools/Apache24/logs/access.log", 900, true)
	mk("data/disk.vhdx", 10, false)
	mk("hiberfil.sys", 1500, false)
	ix, _, err := walker.Walk(context.Background(), root, walker.Options{Workers: 2, ClusterSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	return ix, root
}

func findings(t *testing.T, ix *index.Index, opt Options) map[string]*Finding {
	t.Helper()
	cat, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]*Finding{}
	for _, f := range Evaluate(ix, cat, opt) {
		m[f.Rule.ID] = f
	}
	return m
}

func names(ix *index.Index, f *Finding) string {
	var out []string
	for _, i := range f.Items {
		out = append(out, ix.Name(i))
	}
	return strings.Join(out, ",")
}

func TestBuiltinCatalogLoads(t *testing.T) {
	cat, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Rules) < 20 {
		t.Fatalf("only %d rules", len(cat.Rules))
	}
	for _, r := range cat.Rules {
		if r.Title.FR == "" || r.Title.EN == "" || r.Advice.FR == "" || r.Advice.EN == "" {
			t.Errorf("%s: missing translation", r.ID)
		}
		if r.Risk == RiskRed && r.Deletable() {
			t.Errorf("%s: red rule must not be deletable", r.ID)
		}
	}
}

func TestEvaluate(t *testing.T) {
	ix, _ := fakeVolume(t)
	env := fakeEnv{
		accounts: map[string]string{"S-1-5-21-1": `PC\alice`, "S-1-5-21-1000": `PC\alice`},
		profiles: []Profile{{SID: "S-1-5-21-1000", Path: `C:\Users\alice`}, {SID: "S-1-5-21-9", Path: `C:\Users\ghost`}},
		refs:     map[string]bool{"kept.msi": true},
	}
	m := findings(t, ix, Options{Env: env})

	if f := m["memory-dumps"]; f == nil || f.Files != 3 || f.Size != 50000+1000+200 {
		t.Errorf("memory-dumps: %+v", f)
	}
	if f := m["temp"]; f == nil || names(ix, f) == "" || f.Files != 3 || f.Size != 2000+4000+100 {
		t.Errorf("temp (older than 7d only): %+v %s", f, names(ix, f))
	}
	if f := m["windows-update-cache"]; f == nil || len(f.Items) != 2 || f.Files != 2 {
		t.Errorf("WU cache contents: %+v", f)
	}
	if f := m["component-store"]; f == nil || f.Rule.Deletable() || f.Size != 9000 {
		t.Errorf("WinSxS: %+v", f)
	}
	if f := m["installer-orphans"]; f == nil || names(ix, f) != "1a2b.msi" {
		t.Errorf("installer orphans: %v", f)
	}
	if f := m["node-modules"]; f == nil || len(f.Items) != 1 || f.Files != 2 {
		t.Errorf("node_modules (outermost only): %+v", f)
	}
	if f := m["browser-caches"]; f == nil || f.Files != 2 {
		t.Errorf("browser caches: %+v", f)
	}
	if f := m["recycle-bins"]; f == nil || len(f.Groups) != 2 {
		t.Errorf("recycle bins: %+v", f)
	} else {
		labels := f.Groups[0].Label + "|" + f.Groups[1].Label
		if !strings.Contains(labels, `PC\alice`) || !strings.Contains(labels, "S-1-5-21-2") {
			t.Errorf("recycle labels %s", labels)
		}
	}
	if f := m["ghost-profiles"]; f == nil || names(ix, f) != "ghost" {
		t.Errorf("ghost profiles: %v", f)
	}
	if f := m["iis-logs"]; f == nil || names(ix, f) != "u_ex2001.log" {
		t.Errorf("iis logs older than 30d: %v", f)
	}
	if f := m["web-server-logs"]; f == nil || names(ix, f) != "access.log" {
		t.Errorf("apache logs: %v", f)
	}
	if f := m["hibernation"]; f == nil || f.Rule.Clean != CleanOfficial {
		t.Errorf("hiberfil: %v", f)
	}
	if m["virtual-disks"] != nil {
		t.Error("tiny vhdx below min_size must not be reported")
	}
}

func TestOlderThanOverrideAndPrefix(t *testing.T) {
	ix, _ := fakeVolume(t)
	m := findings(t, ix, Options{OlderThan: map[string]time.Duration{"temp": 0}})
	if f := m["temp"]; f == nil || f.Files != 4 {
		t.Errorf("temp with no age filter: %+v", f)
	}
	// Scanning C:\Users\alice: the Temp rule still applies below it.
	alice, _ := ix.Find("Users/alice")
	sub := index.New(0)
	sub.RootPath = `C:\Users\alice`
	copyTree(ix, alice, sub)
	sub.Finalize()
	m = findings(t, sub, Options{Prefix: PrefixOf(sub.RootPath)})
	if f := m["temp"]; f == nil || f.Files != 1 {
		t.Errorf("prefixed temp: %+v", f)
	}
	if m["memory-dumps"] == nil {
		t.Error("CrashDumps under the prefixed root should match")
	}
}

func copyTree(src *index.Index, from uint32, dst *index.Index) {
	dst.Root = dst.Append(index.Entry{Parent: index.NoParent, Attr: index.FlagDir})
	var rec func(s, d uint32)
	rec = func(s, d uint32) {
		for _, k := range src.Kids(s) {
			n := dst.Append(index.Entry{Parent: d, Name: src.Name(k), Size: src.Size[k], Alloc: src.Alloc[k], MTime: src.MTime[k], Attr: src.Attr[k] &^ index.FlagValid})
			if src.IsDir(k) {
				rec(k, n)
			}
		}
	}
	rec(from, dst.Root)
}

func TestPatterns(t *testing.T) {
	for _, c := range []struct {
		pat, path string
		want      bool
	}{
		{`Windows\Temp`, `windows/TEMP`, true},
		{`Users\*\AppData`, `Users/bob/AppData`, true},
		{`Users\*\AppData`, `Users/AppData`, false},
		{`**\node_modules`, `node_modules`, true},
		{`**\node_modules`, `a/b/c/node_modules`, true},
		{`inetpub\logs\LogFiles\W3SVC*`, `inetpub/logs/logfiles/w3svc12`, true},
		{`**\Apache*\logs`, `x/Apache24/logs`, true},
		{`a\**\b\**\c`, `a/x/b/y/z/c`, true},
		{`a\**\b\**\c`, `a/c`, false},
		{`Données\été`, `DONNÉES/ÉTÉ`, true},
	} {
		pt, err := compilePattern(c.pat)
		if err != nil {
			t.Fatal(err)
		}
		s := pt.start(strings.Split(c.path, "/"))
		if got := s&pt.final() != 0; got != c.want {
			t.Errorf("%s vs %s: %v", c.pat, c.path, got)
		}
	}
	if _, err := compilePattern(`a\..\b`); err == nil {
		t.Error("expected error for ..")
	}
}

func TestCustomRulesCannotDeleteRed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "perso.yaml")
	os.WriteFile(p, []byte(`rules:
  - id: component-store
    category: x
    risk: red
    title: {fr: a, en: a}
    advice: {fr: a, en: a}
    paths: ['Windows\WinSxS']
    clean: delete
  - id: my-logs
    category: logs
    risk: green
    title: {fr: Mes logs, en: My logs}
    advice: {fr: a, en: a}
    paths: ['apps\*\logs']
    target: files
    older_than: 14d
    clean: delete
`), 0o644)
	cat, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := cat.Get("component-store")
	if r.Deletable() || r.Source != p {
		t.Errorf("custom red rule became deletable: %+v", r)
	}
	if r, ok := cat.Get("my-logs"); !ok || r.OlderThanDuration() != 14*24*time.Hour {
		t.Error("custom rule not loaded")
	}
	os.WriteFile(p, []byte("rules:\n  - id: bad\n    risk: purple\n"), 0o644)
	if _, err := Load(p); err == nil {
		t.Error("invalid risk accepted")
	}
}

func TestGlob(t *testing.T) {
	for _, c := range []struct {
		pat, name string
		want      bool
	}{
		{"*.dmp", "MEMORY.DMP", true}, {"*.dmp", "dmp", false}, {"w3svc*", "W3SVC1", true},
		{"a?c", "abc", true}, {"*", "", true}, {"*.ét?", "x.ÉTÉ", true}, {"*a*b*", "xxaYYbzz", true},
	} {
		if got := globMatch(c.pat, []byte(c.name)); got != c.want {
			t.Errorf("%s ~ %s = %v", c.pat, c.name, got)
		}
	}
}

func TestExampleRulesFile(t *testing.T) {
	cat, err := Load("../../rules/exemple-perso.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r, ok := cat.Get("iis-logs")
	if !ok || r.OlderThanDuration() != 90*24*time.Hour {
		t.Error("example should override iis-logs")
	}
	if _, ok := cat.Get("app-logs"); !ok {
		t.Error("app-logs missing")
	}
}

func TestRuleReclaimable(t *testing.T) {
	cat := MustBuiltin()
	want := map[string]bool{
		"memory-dumps":          true,  // green, delete
		"windows-update-cache":  true,  // orange, delete (official too)
		"delivery-optimization": true,  // orange, official only
		"component-store":       false, // red: official action offered, never counted
		"installer-orphans":     false, // red, report only
		"pagefile":              false, // report only
		"upgrade-leftovers":     true,  // Disk Cleanup deletes Windows.old
		"hibernation":           false, // powercfg /h off: a configuration change
		"virtual-disks":         false, // Optimize-VHD compacts, gain unknown
		"event-logs":            false, // wevtutil cl: gain not guaranteed
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
