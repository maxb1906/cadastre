package main

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/clean"
	"github.com/maxb1906/cadastre/internal/cli"
	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/report"
	"github.com/maxb1906/cadastre/internal/sysinfo"
	"github.com/maxb1906/cadastre/internal/volume"
)

// cleanState holds the cleanup plans of the session.
type cleanState struct {
	mu      sync.Mutex
	plans   map[string]*clean.Plan
	last    *clean.Result
	cancel  context.CancelFunc
	running bool
}

func (a *App) cleanupSummary() *report.CleanupSummary {
	a.clean.mu.Lock()
	defer a.clean.mu.Unlock()
	return cli.Summary(a.clean.last)
}

// CleanRequest is the selection sent by the interface.
type CleanRequest struct {
	Rules    []string `json:"rules"`
	Entries  []uint32 `json:"entries"`
	Mode     string   `json:"mode"`
	Official bool     `json:"official"`
}

// SkipGroup gathers left-out items by reason.
type SkipGroup struct {
	Reason   string   `json:"reason"`
	Label    string   `json:"label"`
	Count    int      `json:"count"`
	Alloc    uint64   `json:"alloc"`
	Examples []string `json:"examples"`
}

// OfficialView is an official action of a plan.
type OfficialView struct {
	Rule         string   `json:"rule"`
	Title        string   `json:"title"`
	Risk         string   `json:"risk"`
	Commands     []string `json:"commands"`
	Items        int      `json:"items"`
	Alloc        uint64   `json:"alloc"`
	RestorePoint bool     `json:"restorePoint"`
	Targets      []string `json:"targets"`
}

// PlanView is the dry-run screen.
type PlanView struct {
	ID           string         `json:"id"`
	Mode         string         `json:"mode"`
	Files        int            `json:"files"`
	Bytes        uint64         `json:"bytes"`
	Orange       bool           `json:"orange"`
	FromSnapshot bool           `json:"fromSnapshot"`
	Items        []clean.Item   `json:"items"` // first files, for display
	MoreItems    int            `json:"moreItems"`
	Skipped      []SkipGroup    `json:"skipped"`
	Official     []OfficialView `json:"official"`
	RestorePoint bool           `json:"restorePoint"`
}

// PlanClean builds the plan of a selection and runs the mandatory dry-run
// (every item is checked on disk).
func (a *App) PlanClean(req CleanRequest) (*PlanView, error) {
	cur, err := a.current()
	if err != nil {
		return nil, err
	}
	running, _ := sysinfo.RunningProcesses()
	p := clean.Build(cur, clean.Selection{Rules: req.Rules, Entries: req.Entries, Mode: req.Mode, Official: req.Official},
		clean.Env{Admin: volume.IsAdmin(), Running: running})
	clean.Prepare(p, clean.OSFS{})
	a.clean.mu.Lock()
	if a.clean.plans == nil {
		a.clean.plans = map[string]*clean.Plan{}
	}
	a.clean.plans[p.ID] = p
	a.clean.mu.Unlock()

	v := &PlanView{ID: p.ID, Mode: p.Mode, Files: p.Files, Bytes: p.Bytes, Orange: p.Orange, FromSnapshot: p.FromSnapshot}
	for _, it := range p.Items {
		if it.Dir && !it.Reparse {
			continue
		}
		if len(v.Items) >= 2000 {
			v.MoreItems++
			continue
		}
		v.Items = append(v.Items, it)
	}
	groups := map[string]*SkipGroup{}
	for _, s := range p.Skipped {
		g := groups[s.Reason]
		if g == nil {
			g = &SkipGroup{Reason: s.Reason, Label: i18n.T(a.lang, "clean.reason."+s.Reason)}
			groups[s.Reason] = g
		}
		g.Count++
		g.Alloc += s.Alloc
		if len(g.Examples) < 15 && s.Path != "" {
			g.Examples = append(g.Examples, s.Path)
		}
	}
	for _, g := range groups {
		v.Skipped = append(v.Skipped, *g)
	}
	sort.Slice(v.Skipped, func(i, j int) bool { return v.Skipped[i].Count > v.Skipped[j].Count })
	for _, o := range p.Official {
		ov := OfficialView{Rule: o.Rule, Title: o.Title.In(a.lang), Risk: o.Risk, Items: len(o.Items), Alloc: o.Alloc, RestorePoint: o.RestorePoint, Targets: o.Targets}
		for _, c := range o.Commands {
			if c == nil {
				ov.Commands = append(ov.Commands, "→ "+i18n.T(a.lang, "clean.files_tab")+" ("+itoa(len(o.Items))+")")
				continue
			}
			ov.Commands = append(ov.Commands, joinArgs(c))
		}
		v.RestorePoint = v.RestorePoint || o.RestorePoint
		v.Official = append(v.Official, ov)
	}
	return v, nil
}

func joinArgs(c []string) string {
	out := ""
	for k, s := range c {
		if k > 0 {
			out += " "
		}
		out += s
	}
	return out
}

func itoa(n int) string {
	return report.Itoa(n)
}

// ExecuteClean runs a simulated plan asynchronously. Events: "clean:progress",
// "clean:done" (result), "clean:error".
func (a *App) ExecuteClean(planID string, confirmOrange, restorePoint bool) error {
	a.clean.mu.Lock()
	p := a.clean.plans[planID]
	if p == nil {
		a.clean.mu.Unlock()
		return errors.New(i18n.T(a.lang, "clean.reason.not_prepared"))
	}
	if a.clean.running {
		a.clean.mu.Unlock()
		return errors.New(i18n.T(a.lang, "gui.busy"))
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.clean.cancel = cancel
	a.clean.running = true
	a.clean.mu.Unlock()

	go func() {
		defer cancel()
		defer func() {
			a.clean.mu.Lock()
			a.clean.running = false
			a.clean.mu.Unlock()
		}()
		j, err := clean.OpenJournal("")
		if err != nil {
			wruntime.EventsEmit(a.ctx, "clean:error", err.Error())
			return
		}
		defer j.Close()
		cur, err := a.current()
		if err != nil {
			wruntime.EventsEmit(a.ctx, "clean:error", err.Error())
			return
		}
		freeBefore := cur.Result.Volume.Free
		var last time.Time
		r, err := clean.Execute(ctx, p, clean.OSFS{}, j, clean.Options{
			ConfirmOrange: confirmOrange,
			RestorePoint:  restorePoint,
			Running: func() map[string]bool {
				m, _ := sysinfo.RunningProcesses()
				return m
			},
			Progress: func(pr clean.Progress) {
				if time.Since(last) > 80*time.Millisecond || pr.Done == pr.Total {
					last = time.Now()
					wruntime.EventsEmit(a.ctx, "clean:progress", pr)
				}
			},
		})
		if err != nil {
			wruntime.EventsEmit(a.ctx, "clean:error", err.Error())
			return
		}
		// Bring the view up to date (removed entries, rescanned zones).
		a.mu.Lock()
		a.cur = clean.Refresh(ctx, cur, r, analysis.Options{})
		freeAfter := a.cur.Result.Volume.Free
		a.mu.Unlock()
		a.clean.mu.Lock()
		a.clean.last = r
		delete(a.clean.plans, planID)
		a.clean.mu.Unlock()
		wruntime.EventsEmit(a.ctx, "clean:done", map[string]any{
			"done": r.Done, "failed": r.Failed, "skipped": r.Skipped, "freed": r.Freed, "canceled": r.Canceled,
			"journal": r.Journal, "freeBefore": freeBefore, "freeAfter": freeAfter, "entries": cli.Summary(r).Entries,
		})
	}()
	return nil
}

// CancelClean stops the cleanup between two items.
func (a *App) CancelClean() {
	a.clean.mu.Lock()
	defer a.clean.mu.Unlock()
	if a.clean.cancel != nil {
		a.clean.cancel()
	}
}

// ExportScript writes the PowerShell script of a plan (dialog).
func (a *App) ExportScript(planID string) (string, error) {
	a.clean.mu.Lock()
	p := a.clean.plans[planID]
	a.clean.mu.Unlock()
	if p == nil {
		return "", errors.New(i18n.T(a.lang, "clean.reason.not_prepared"))
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           i18n.T(a.lang, "clean.script"),
		DefaultFilename: "cadastre-" + time.Now().Format("20060102-1504") + ".ps1",
		Filters:         []wruntime.FileFilter{{DisplayName: "PowerShell (*.ps1)", Pattern: "*.ps1"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := clean.WriteScript(f, p, a.lang); err != nil {
		return "", err
	}
	return path, f.Close()
}

// OpenJournalFolder shows the journal folder.
func (a *App) OpenJournalFolder() {
	openPath(clean.DefaultDir())
}
