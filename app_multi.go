package main

import (
	"context"
	"errors"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/clean"
	"github.com/maxb1906/cadastre/internal/cli"
	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/volume"
)

// Multi-disk cleanup: the selected volumes are scanned one after the other
// (scanning them in parallel would multiply the memory of the indexes), each
// gets a suggested selection, and the cleanup runs volume by volume through
// the same plan, checks and journal as a single-volume cleanup.

// multiDisk is one volume of the multi-disk session.
type multiDisk struct {
	root string
	an   *analysis.Analysis
	est  *clean.Reclaim
	err  string
}

// multiPlan ties a multi-disk confirmation screen to its per-volume plans.
type multiPlan struct {
	roots []string
	plans []string
}

func (a *App) multiDisk(root string) *multiDisk {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, d := range a.multi {
		if d.root == root {
			return d
		}
	}
	return nil
}

// StartMulti scans the given volumes one after the other. Events:
// "multi:progress" {root, index, count, progress}, then "multi:done" or
// "multi:canceled". A volume that fails is reported in GetMulti and does not
// stop the others.
func (a *App) StartMulti(roots []string, engineName string) error {
	if len(roots) == 0 {
		return errors.New(i18n.T(a.lang, "multi.none"))
	}
	a.mu.Lock()
	if a.scanning {
		a.mu.Unlock()
		return errors.New(i18n.T(a.lang, "gui.busy"))
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	a.scanning = true
	a.multi = nil
	a.mu.Unlock()

	go func() {
		defer cancel()
		var disks []*multiDisk
		admin := volume.IsAdmin()
		for k, root := range roots {
			var last time.Time
			emit := func(p engine.Progress) {
				wruntime.EventsEmit(a.ctx, "multi:progress", map[string]any{"root": root, "index": k, "count": len(roots), "progress": p})
			}
			emit(engine.Progress{})
			res, err := engine.Scan(ctx, root, engine.Options{Engine: engineName, Progress: func(p engine.Progress) {
				if time.Since(last) < 100*time.Millisecond && p.Done != p.Total {
					return
				}
				last = time.Now()
				emit(p)
			}})
			if ctx.Err() != nil {
				break
			}
			d := &multiDisk{root: root}
			if err != nil {
				d.err = err.Error()
			} else {
				emit(engine.Progress{Phase: "analyze"})
				d.an = analysis.Run(res, a.catalog, analysis.Options{})
				e := clean.EstimateReclaim(d.an, clean.Env{Admin: admin})
				d.est = &e
			}
			disks = append(disks, d)
		}
		a.mu.Lock()
		a.scanning = false
		canceled := ctx.Err() != nil
		if !canceled {
			a.multi = disks
		}
		a.mu.Unlock()
		if canceled {
			wruntime.EventsEmit(a.ctx, "multi:canceled")
			return
		}
		wruntime.EventsEmit(a.ctx, "multi:done")
	}()
	return nil
}

// DiskView is one volume of the multi-disk screen.
type DiskView struct {
	Root        string           `json:"root"`
	Label       string           `json:"label"`
	FS          string           `json:"fs"`
	Engine      string           `json:"engine"`
	Total       uint64           `json:"total"`
	Free        uint64           `json:"free"`
	Error       string           `json:"error,omitempty"`
	Reclaimable uint64           `json:"reclaimable"`
	PermOnly    uint64           `json:"reclaimPermanentOnly"`
	AdminOnly   uint64           `json:"reclaimAdminOnly"`
	Findings    []map[string]any `json:"findings"`
	Suggested   []string         `json:"suggested"` // rules checked by default
}

// GetMulti returns the volumes of the multi-disk session with their findings
// and suggested selection (green rules whose space is really reclaimable).
func (a *App) GetMulti() []DiskView {
	a.mu.RLock()
	disks := a.multi
	a.mu.RUnlock()
	out := make([]DiskView, 0, len(disks))
	for _, d := range disks {
		v := DiskView{Root: d.root, Error: d.err}
		if d.an != nil {
			r := d.an.Result
			v.Label, v.FS, v.Engine, v.Total, v.Free = r.Volume.Label, r.Volume.FileSystem, r.Engine, r.Volume.Total, r.Volume.Free
			v.Reclaimable, v.PermOnly, v.AdminOnly = d.est.Total, d.est.PermanentOnly, d.est.AdminOnly
			v.Findings = a.findingsOf(d.an, d.est)
			v.Suggested = clean.Suggested(d.an, *d.est, false)
		}
		if v.Suggested == nil {
			v.Suggested = []string{}
		}
		out = append(out, v)
	}
	return out
}

// DiskSelection is the rules checked on one volume.
type DiskSelection struct {
	Root  string   `json:"root"`
	Rules []string `json:"rules"`
}

// MultiRequest is the selection of the multi-disk screen.
type MultiRequest struct {
	Disks []DiskSelection `json:"disks"`
	Mode  string          `json:"mode"`
}

// DiskPlan is the confirmation screen of one volume.
type DiskPlan struct {
	Root  string    `json:"root"`
	Label string    `json:"label"`
	Plan  *PlanView `json:"plan"`
}

// MultiPlanView is the confirmation screen of a multi-disk cleanup.
type MultiPlanView struct {
	ID    string     `json:"id"`
	Mode  string     `json:"mode"`
	Disks []DiskPlan `json:"disks"`
}

// PlanMulti builds and checks on disk the plan of every volume with a
// selection (the mandatory dry-run, as for a single volume).
func (a *App) PlanMulti(req MultiRequest) (*MultiPlanView, error) {
	v := &MultiPlanView{Mode: req.Mode}
	mp := &multiPlan{}
	for _, sel := range req.Disks {
		if len(sel.Rules) == 0 {
			continue
		}
		d := a.multiDisk(sel.Root)
		if d == nil || d.an == nil {
			return nil, errors.New(i18n.T(a.lang, "multi.unknown", sel.Root))
		}
		// Rules with an official action go through it, as in the panel.
		official := false
		for _, id := range sel.Rules {
			if r, ok := d.an.Catalog.Get(id); ok && r.HasOfficial() {
				official = true
			}
		}
		pv := a.planView(d.an, clean.Selection{Rules: sel.Rules, Mode: req.Mode, Official: official})
		v.Disks = append(v.Disks, DiskPlan{Root: d.root, Label: d.an.Result.Volume.Label, Plan: pv})
		mp.roots = append(mp.roots, d.root)
		mp.plans = append(mp.plans, pv.ID)
	}
	if len(v.Disks) == 0 {
		return nil, errors.New(i18n.T(a.lang, "multi.none"))
	}
	v.ID = mp.plans[0] + "-multi"
	a.clean.mu.Lock()
	if a.clean.multi == nil {
		a.clean.multi = map[string]*multiPlan{}
	}
	a.clean.multi[v.ID] = mp
	a.clean.mu.Unlock()
	return v, nil
}

// ExecuteMulti runs the plans of a multi-disk screen volume by volume, into
// one journal. Events: "clean:progress" (with root), "clean:done" (totals
// plus per-volume results), "clean:error". Canceling stops between two items
// and skips the remaining volumes.
func (a *App) ExecuteMulti(id string, confirmOrange, restorePoint bool) error {
	a.clean.mu.Lock()
	mp := a.clean.multi[id]
	a.clean.mu.Unlock()
	if mp == nil {
		return errors.New(i18n.T(a.lang, "clean.reason.not_prepared"))
	}
	ctx, err := a.beginClean()
	if err != nil {
		return err
	}
	go func() {
		defer a.endClean()
		j, err := clean.OpenJournal("")
		if err != nil {
			wruntime.EventsEmit(a.ctx, "clean:error", err.Error())
			return
		}
		defer j.Close()
		total := map[string]any{"done": 0, "failed": 0, "skipped": 0, "freed": uint64(0), "canceled": false, "journal": j.Path()}
		var disks []map[string]any
		var entries []any
		var freeBefore, freeAfter uint64
		for k, root := range mp.roots {
			if ctx.Err() != nil {
				total["canceled"] = true
				break
			}
			a.clean.mu.Lock()
			p := a.clean.plans[mp.plans[k]]
			a.clean.mu.Unlock()
			d := a.multiDisk(root)
			if p == nil || d == nil || d.an == nil {
				continue
			}
			before := d.an.Result.Volume.Free
			r, fresh, err := a.runPlan(ctx, p, d.an, j, confirmOrange, restorePoint, root)
			if err != nil {
				disks = append(disks, map[string]any{"root": root, "error": err.Error()})
				continue
			}
			e := clean.EstimateReclaim(fresh, clean.Env{Admin: volume.IsAdmin()})
			a.mu.Lock()
			d.an, d.est = fresh, &e
			// The main view of this volume is now out of date: show the
			// cleaned one.
			if a.cur != nil && a.cur.Result.Volume.Root == root {
				a.cur = fresh
			}
			a.mu.Unlock()
			a.clean.mu.Lock()
			a.clean.last = r
			delete(a.clean.plans, mp.plans[k])
			a.clean.mu.Unlock()
			after := fresh.Result.Volume.Free
			freeBefore += before
			freeAfter += after
			total["done"] = total["done"].(int) + r.Done
			total["failed"] = total["failed"].(int) + r.Failed
			total["skipped"] = total["skipped"].(int) + r.Skipped
			total["freed"] = total["freed"].(uint64) + r.Freed
			if r.Canceled {
				total["canceled"] = true
			}
			disks = append(disks, map[string]any{"root": root, "done": r.Done, "failed": r.Failed, "skipped": r.Skipped, "freed": r.Freed,
				"freeBefore": before, "freeAfter": after, "canceled": r.Canceled})
			for _, en := range cli.Summary(r).Entries {
				if len(entries) < 1000 {
					entries = append(entries, en)
				}
			}
		}
		a.clean.mu.Lock()
		delete(a.clean.multi, id)
		a.clean.mu.Unlock()
		total["freeBefore"], total["freeAfter"], total["entries"], total["disks"] = freeBefore, freeAfter, entries, disks
		wruntime.EventsEmit(a.ctx, "clean:done", total)
	}()
	return nil
}
