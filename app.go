package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/clean"
	"github.com/maxb1906/cadastre/internal/cli"
	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/reclaim"
	"github.com/maxb1906/cadastre/internal/report"
	"github.com/maxb1906/cadastre/internal/rules"
	"github.com/maxb1906/cadastre/internal/snapshot"
	"github.com/maxb1906/cadastre/internal/units"
	"github.com/maxb1906/cadastre/internal/view"
	"github.com/maxb1906/cadastre/internal/volume"
)

// App is the backend bound to the Wails frontend. The index stays in Go;
// the interface asks for what it displays, level by level.
type App struct {
	ctx  context.Context
	lang string

	mu       sync.RWMutex
	cur      *analysis.Analysis
	catalog  *rules.Catalog
	cancel   context.CancelFunc
	scanning bool

	clean cleanState   // see app_clean.go
	multi []*multiDisk // multi-disk session, see app_multi.go

	estMu  sync.Mutex
	est    *clean.Reclaim
	estFor *analysis.Analysis
}

// reclaim returns the real reclaimable space of an analysis, computed once
// per analysis (a new scan or a cleanup gives a new one).
func (a *App) reclaim(cur *analysis.Analysis) *clean.Reclaim {
	a.estMu.Lock()
	defer a.estMu.Unlock()
	if a.estFor != cur {
		e := clean.EstimateReclaim(cur, clean.Env{Admin: volume.IsAdmin()})
		a.est, a.estFor = &e, cur
	}
	return a.est
}

// NewApp creates the backend.
func NewApp() *App {
	return &App{lang: i18n.Detect()}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	cat, err := rules.Load()
	if err != nil {
		cat = rules.MustBuiltin()
	}
	a.catalog = cat
	wruntime.OnFileDrop(ctx, func(x, y int, paths []string) {
		for _, p := range paths {
			if strings.EqualFold(filepath.Ext(p), ".cad") {
				if err := a.OpenSnapshot(p); err != nil {
					wruntime.EventsEmit(ctx, "scan:error", err.Error())
				}
				return
			}
		}
	})
}

// Info describes the application state at startup.
type Info struct {
	Version string            `json:"version"`
	Lang    string            `json:"lang"`
	Admin   bool              `json:"admin"`
	OS      string            `json:"os"`
	Catalog map[string]string `json:"catalog"`
	Rules   int               `json:"rules"`
}

// GetInfo returns version, language, privileges and the translations.
func (a *App) GetInfo() Info {
	n := 0
	if a.catalog != nil {
		n = len(a.catalog.Rules)
	}
	return Info{Version: cli.Version, Lang: a.lang, Admin: volume.IsAdmin(), OS: runtime.GOOS, Catalog: i18n.Catalog(a.lang), Rules: n}
}

// SetLang switches the language and returns the new catalog.
func (a *App) SetLang(lang string) map[string]string {
	if l := i18n.Normalize(lang); l != "" {
		a.lang = l
	}
	return i18n.Catalog(a.lang)
}

// VolumeView is a volume card of the welcome screen.
type VolumeView struct {
	Root   string `json:"root"`
	Label  string `json:"label"`
	FS     string `json:"fs"`
	Kind   string `json:"kind"`
	Total  uint64 `json:"total"`
	Free   uint64 `json:"free"`
	CanMFT bool   `json:"canMft"`
}

// ListVolumes returns the mounted volumes.
func (a *App) ListVolumes() []VolumeView {
	vols, _ := volume.List()
	out := make([]VolumeView, 0, len(vols))
	admin := volume.IsAdmin()
	for _, v := range vols {
		out = append(out, VolumeView{Root: v.Root, Label: v.Label, FS: v.FileSystem, Kind: v.Kind, Total: v.Total, Free: v.Free,
			CanMFT: admin && runtime.GOOS == "windows" && strings.EqualFold(v.FileSystem, "NTFS")})
	}
	return out
}

// PickFolder opens the native folder picker.
func (a *App) PickFolder() string {
	p, _ := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: i18n.T(a.lang, "gui.pick_folder")})
	return p
}

// StartScan scans target asynchronously. Progress is emitted as
// "scan:progress" events, the end as "scan:done" or "scan:error".
func (a *App) StartScan(target, engineName, minSize string) error {
	a.mu.Lock()
	if a.scanning {
		a.mu.Unlock()
		return errors.New(i18n.T(a.lang, "gui.busy"))
	}
	ms, err := units.ParseSize(orZero(minSize))
	if err != nil {
		a.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	a.scanning = true
	a.mu.Unlock()

	go func() {
		defer cancel()
		var last time.Time
		opt := engine.Options{Engine: engineName, MinSize: ms, Progress: func(p engine.Progress) {
			if time.Since(last) < 100*time.Millisecond && p.Done != p.Total {
				return
			}
			last = time.Now()
			wruntime.EventsEmit(a.ctx, "scan:progress", p)
		}}
		res, err := engine.Scan(ctx, target, opt)
		if err == nil {
			wruntime.EventsEmit(a.ctx, "scan:progress", engine.Progress{Phase: "analyze"})
			a.setResult(res)
		}
		a.mu.Lock()
		a.scanning = false
		a.mu.Unlock()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				wruntime.EventsEmit(a.ctx, "scan:canceled")
				return
			}
			wruntime.EventsEmit(a.ctx, "scan:error", err.Error())
			return
		}
		wruntime.EventsEmit(a.ctx, "scan:done")
	}()
	return nil
}

func orZero(s string) string {
	if strings.TrimSpace(s) == "" {
		return "0"
	}
	return s
}

func (a *App) setResult(res *engine.Result) {
	an := analysis.Run(res, a.catalog, analysis.Options{})
	a.mu.Lock()
	a.cur = an
	a.mu.Unlock()
}

// CancelScan stops the running scan.
func (a *App) CancelScan() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *App) current() (*analysis.Analysis, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.cur == nil {
		return nil, errors.New(i18n.T(a.lang, "gui.noscan"))
	}
	return a.cur, nil
}

// OpenSnapshot reopens a .cad file (dialog when path is empty).
func (a *App) OpenSnapshot(path string) error {
	if path == "" {
		p, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
			Title:   i18n.T(a.lang, "gui.open_snapshot"),
			Filters: []wruntime.FileFilter{{DisplayName: "Cadastre (*.cad)", Pattern: "*.cad"}},
		})
		if err != nil || p == "" {
			return err
		}
		path = p
	}
	res, err := snapshot.Load(path)
	if err != nil {
		return err
	}
	a.setResult(res)
	wruntime.EventsEmit(a.ctx, "scan:done")
	return nil
}

// SaveSnapshot saves the current scan (dialog) and returns the path.
func (a *App) SaveSnapshot() (string, error) {
	cur, err := a.current()
	if err != nil {
		return "", err
	}
	p, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           i18n.T(a.lang, "gui.save_snapshot"),
		DefaultFilename: defaultName(cur, "cad"),
		Filters:         []wruntime.FileFilter{{DisplayName: "Cadastre (*.cad)", Pattern: "*.cad"}},
	})
	if err != nil || p == "" {
		return "", err
	}
	return p, snapshot.Save(p, cur.Result)
}

func defaultName(a *analysis.Analysis, ext string) string {
	tag := strings.NewReplacer(`\`, "", "/", "_", ":", "").Replace(a.Result.Volume.Root)
	if len(tag) > 40 {
		tag = tag[len(tag)-40:]
	}
	return fmt.Sprintf("cadastre-%s-%s.%s", strings.Trim(tag, "_"), a.At.Format("20060102-1504"), ext)
}

// Export writes the current scan as "html", "json", "csv" or "md" (dialog)
// and returns the path.
func (a *App) Export(kind string) (string, error) {
	cur, err := a.current()
	if err != nil {
		return "", err
	}
	names := map[string]string{"html": "HTML", "json": "JSON", "csv": "CSV", "md": "Markdown"}
	if names[kind] == "" {
		return "", fmt.Errorf("unknown export %q", kind)
	}
	p, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           i18n.T(a.lang, "gui.export"),
		DefaultFilename: defaultName(cur, kind),
		Filters:         []wruntime.FileFilter{{DisplayName: names[kind], Pattern: "*." + kind}},
	})
	if err != nil || p == "" {
		return "", err
	}
	f, err := os.Create(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opt := report.Options{Lang: a.lang, Cleanup: a.cleanupSummary(), Reclaim: a.reclaim(cur)}
	switch kind {
	case "html":
		err = report.WriteHTML(f, report.Build(cur, opt))
	case "json":
		err = report.WriteJSON(f, report.Build(cur, opt))
	case "md":
		err = report.WriteMarkdown(f, report.Build(cur, opt))
	case "csv":
		err = report.WriteCSV(f, cur.Result.Index, report.CSVOptions{Lang: a.lang})
	}
	if err != nil {
		return "", err
	}
	if kind == "html" {
		openPath(p)
	}
	return p, f.Close()
}

// Summary is the header and the right panels of the main screen.
type Summary struct {
	Root         string                  `json:"root"`
	RootID       uint32                  `json:"rootId"`
	Label        string                  `json:"label"`
	FS           string                  `json:"fs"`
	Engine       string                  `json:"engine"`
	Duration     float64                 `json:"duration"`
	Admin        bool                    `json:"admin"`
	FromSnapshot bool                    `json:"fromSnapshot"`
	ScannedAt    time.Time               `json:"scannedAt"`
	Totals       index.Totals            `json:"totals"`
	Warnings     []string                `json:"warnings"`
	Recon        *reclaim.Reconciliation `json:"recon"`
	Reclaimable  uint64                  `json:"reclaimable"`
	PermOnly     uint64                  `json:"reclaimPermanentOnly"`
	Entries      int                     `json:"entries"`
	MemoryMB     uint64                  `json:"memoryMb"`
}

// GetSummary returns the summary of the current scan.
func (a *App) GetSummary() (*Summary, error) {
	cur, err := a.current()
	if err != nil {
		return nil, err
	}
	r := cur.Result
	ix := r.Index
	s := &Summary{Root: r.Volume.Root, RootID: ix.Root, Label: r.Volume.Label, FS: r.Volume.FileSystem, Engine: r.Engine,
		Duration: r.Duration.Seconds(), Admin: r.Admin, FromSnapshot: r.FromSnapshot, ScannedAt: r.Started,
		Totals: ix.Totals(), Recon: cur.Recon, Entries: ix.Len(), MemoryMB: ix.Memory() >> 20}
	for _, w := range r.Warnings {
		s.Warnings = append(s.Warnings, i18n.T(a.lang, w.Key, w.Args...))
	}
	est := a.reclaim(cur)
	s.Reclaimable, s.PermOnly = est.Total, est.PermanentOnly
	return s, nil
}

// GetChildren lists a folder (paged, sorted).
func (a *App) GetChildren(dir uint32, sortKey string, offset, limit int) (*view.Listing, error) {
	cur, err := a.current()
	if err != nil {
		return nil, err
	}
	ix := cur.Result.Index
	if !ix.Valid(dir) || !ix.IsDir(dir) {
		dir = ix.Root
	}
	l := view.Children(ix, dir, sortKey, offset, limit)
	return &l, nil
}

// GetTreemap returns the treemap of a folder, capped to maxNodes rectangles.
func (a *App) GetTreemap(dir uint32, maxNodes int) (*view.Node, error) {
	cur, err := a.current()
	if err != nil {
		return nil, err
	}
	ix := cur.Result.Index
	if !ix.Valid(dir) || !ix.IsDir(dir) {
		dir = ix.Root
	}
	if maxNodes <= 0 || maxNodes > 20000 {
		maxNodes = 5000
	}
	return view.Treemap(ix, dir, maxNodes), nil
}

// GetFindings returns the ghost findings with their first items, as flat
// JSON objects (report.FindingData plus cleanup hints).
func (a *App) GetFindings() ([]map[string]any, error) {
	cur, err := a.current()
	if err != nil {
		return nil, err
	}
	return a.findingsOf(cur, a.reclaim(cur)), nil
}

// findingsOf returns the findings of an analysis for the cleanup panel.
func (a *App) findingsOf(cur *analysis.Analysis, est *clean.Reclaim) []map[string]any {
	d := report.Build(cur, report.Options{Lang: a.lang, Top: 1, FindingItems: 200, TreemapNodes: 1, Reclaim: est})
	out := make([]map[string]any, 0, len(d.Findings))
	for k, fd := range d.Findings {
		r := cur.Findings[k].Rule
		b, _ := json.Marshal(fd)
		m := map[string]any{}
		json.Unmarshal(b, &m)
		m["system"] = r.System
		m["permanent"] = r.Permanent
		m["closeFirst"] = r.CloseFirst
		m["restorePoint"] = r.Official != nil && r.Official.RestorePoint
		out = append(out, m)
	}
	return out
}

// GetFindingItems lists the items of a finding as rows (paged).
func (a *App) GetFindingItems(ruleID string, offset, limit int) ([]view.Item, error) {
	cur, err := a.current()
	if err != nil {
		return nil, err
	}
	f := cur.Finding(ruleID)
	if f == nil {
		return nil, nil
	}
	ix := cur.Result.Index
	items := make([]view.Item, 0, len(f.Items))
	for _, i := range f.Items {
		it := view.MakeItem(ix, i, nil)
		it.Name = ix.Path(i)
		items = append(items, it)
	}
	view.SortItems(items, "alloc")
	if offset > len(items) {
		offset = len(items)
	}
	end := len(items)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return items[offset:end], nil
}

// GetTopFiles returns the n largest files with their paths.
func (a *App) GetTopFiles(n int) ([]view.Item, error) {
	cur, err := a.current()
	if err != nil {
		return nil, err
	}
	ix := cur.Result.Index
	var out []view.Item
	for _, i := range ix.TopFiles(n, index.ByAlloc, false) {
		it := view.MakeItem(ix, i, nil)
		it.Name = ix.Path(i)
		out = append(out, it)
	}
	return out, nil
}

// GetExtensions returns the n heaviest extensions.
func (a *App) GetExtensions(n int) ([]index.ExtStat, error) {
	cur, err := a.current()
	if err != nil {
		return nil, err
	}
	e := cur.Result.Index.Extensions()
	if len(e) > n {
		e = e[:n]
	}
	return e, nil
}

// Locate returns the parent folder of an entry, to navigate to it.
func (a *App) Locate(id uint32) (uint32, error) {
	cur, err := a.current()
	if err != nil {
		return 0, err
	}
	ix := cur.Result.Index
	if !ix.Valid(id) {
		return ix.Root, nil
	}
	if id == ix.Root {
		return id, nil
	}
	return ix.Parent[id], nil
}

// GetItem describes one entry, with its full path as name (cleanup selection).
func (a *App) GetItem(id uint32) (*view.Item, error) {
	cur, err := a.current()
	if err != nil {
		return nil, err
	}
	ix := cur.Result.Index
	if !ix.Valid(id) {
		return nil, fmt.Errorf("invalid entry %d", id)
	}
	it := view.MakeItem(ix, id, nil)
	it.Name = ix.Path(id)
	return &it, nil
}

// PathOf returns the full path of an entry.
func (a *App) PathOf(id uint32) string {
	cur, err := a.current()
	if err != nil || !cur.Result.Index.Valid(id) {
		return ""
	}
	return cur.Result.Index.Path(id)
}

// Reveal shows an entry in the file manager.
func (a *App) Reveal(id uint32) error {
	p := a.PathOf(id)
	if p == "" {
		return nil
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", "/select,", p).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(p)).Start()
	}
}

// RuleList returns the catalogue (for the rules panel).
func (a *App) RuleList() []map[string]any {
	var out []map[string]any
	for _, r := range a.catalog.Rules {
		out = append(out, map[string]any{"id": r.ID, "title": r.Title.In(a.lang), "risk": r.Risk, "category": r.Category,
			"advice": r.Advice.In(a.lang), "clean": r.Clean, "source": r.Source})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(string) < out[j]["id"].(string) })
	return out
}

func openPath(p string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", p).Start()
	default:
		exec.Command("xdg-open", p).Start()
	}
}
