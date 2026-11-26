package report

import (
	"sort"
	"time"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/clean"
	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/reclaim"
	"github.com/maxb1906/cadastre/internal/rules"
	"github.com/maxb1906/cadastre/internal/view"
	"github.com/maxb1906/cadastre/internal/volume"
)

// Version of Cadastre, set by the CLI.
var Version = "dev"

// TreemapJS is the shared treemap component (frontend/src/shared/treemap.js),
// injected by the main package which embeds it.
var TreemapJS string

// Item is a file or folder in a report.
type Item struct {
	Path  string   `json:"path"`
	Dir   bool     `json:"dir,omitempty"`
	Size  uint64   `json:"size"`
	Alloc uint64   `json:"alloc"`
	Files uint64   `json:"files,omitempty"`
	MTime string   `json:"modified,omitempty"`
	Flags []string `json:"flags,omitempty"`
}

// FindingData is a ghost finding in a report.
type FindingData struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Category    string        `json:"category"`
	Risk        string        `json:"risk"`
	Advice      string        `json:"advice"`
	Clean       string        `json:"clean"`
	Official    string        `json:"official,omitempty"`
	Deletable   bool          `json:"deletable"`
	Reclaimable bool          `json:"reclaimable"`
	Reclaim     uint64        `json:"reclaim"` // space really freed by its cleanup
	Files       uint64        `json:"files"`
	Size        uint64        `json:"size"`
	Alloc       uint64        `json:"alloc"`
	ADSOnly     bool          `json:"adsOnly,omitempty"`
	Items       []Item        `json:"items"`
	MoreItems   int           `json:"moreItems,omitempty"`
	Groups      []rules.Group `json:"groups,omitempty"`
	OlderThan   string        `json:"olderThan,omitempty"`
	MinSize     string        `json:"minSize,omitempty"`
	CloudSkip   uint64        `json:"cloudSkipped,omitempty"`
	Unavailable bool          `json:"envUnavailable,omitempty"`
}

// CleanupEntry is a line of the cleanup journal summarized in a report.
type CleanupEntry struct {
	Time   string `json:"time"`
	Action string `json:"action"`
	Path   string `json:"path"`
	Size   uint64 `json:"size"`
	Mode   string `json:"mode"`
	Result string `json:"result"`
	Detail string `json:"detail,omitempty"`
}

// CleanupSummary summarizes a cleanup session.
type CleanupSummary struct {
	Journal string         `json:"journal"`
	Done    int            `json:"done"`
	Failed  int            `json:"failed"`
	Skipped int            `json:"skipped"`
	Freed   uint64         `json:"freed"`
	Entries []CleanupEntry `json:"entries"`
}

// Data is the content of a report, shared by HTML, Markdown and JSON.
type Data struct {
	Tool         string                  `json:"tool"`
	Version      string                  `json:"version"`
	Generated    time.Time               `json:"generated"`
	Lang         string                  `json:"lang"`
	Volume       volume.Info             `json:"volume"`
	Engine       string                  `json:"engine"`
	Started      time.Time               `json:"started"`
	DurationSec  float64                 `json:"durationSeconds"`
	Admin        bool                    `json:"admin"`
	FromSnapshot bool                    `json:"fromSnapshot"`
	Totals       index.Totals            `json:"totals"`
	Warnings     []string                `json:"warnings"`
	Recon        *reclaim.Reconciliation `json:"reconciliation"`
	Findings     []FindingData           `json:"findings"`
	// Reclaimable is the space really freed by cleaning every reclaimable
	// rule (see clean.EstimateReclaim).
	Reclaimable          uint64          `json:"reclaimable"`
	ReclaimPermanentOnly uint64          `json:"reclaimPermanentOnly"`
	TopFiles             []Item          `json:"topFiles"`
	TopDirs              []Item          `json:"topFolders"`
	Extensions           []index.ExtStat `json:"extensions"`
	Cleanup              *CleanupSummary `json:"cleanup,omitempty"`
	Treemap              *view.Node      `json:"-"`
}

// Options tunes the content of a report.
type Options struct {
	Lang         string
	Top          int // rows of the top tables (default 50)
	FindingItems int // items listed per finding (default 50)
	TreemapNodes int // default 5000
	Cleanup      *CleanupSummary
	// Reclaim is the precomputed estimate (computed when nil).
	Reclaim *clean.Reclaim
}

func (o *Options) defaults() {
	if o.Lang == "" {
		o.Lang = "fr"
	}
	if o.Top <= 0 {
		o.Top = 50
	}
	if o.FindingItems <= 0 {
		o.FindingItems = 50
	}
	if o.TreemapNodes <= 0 {
		o.TreemapNodes = 5000
	}
}

func fmtTime(u uint32) string {
	if u == 0 {
		return ""
	}
	return time.Unix(int64(u), 0).UTC().Format(time.RFC3339)
}

func makeItem(ix *index.Index, i uint32) Item {
	it := Item{Path: ix.Path(i), Dir: ix.IsDir(i), Size: ix.TotalSize(i), Alloc: ix.TotalAlloc(i), MTime: fmtTime(ix.MTime[i]), Flags: view.Flags(ix, i)}
	if it.Dir {
		it.Files, _ = ix.SubtreeCounts(i)
	}
	return it
}

// Build assembles the report data of an analysis.
func Build(a *analysis.Analysis, opt Options) *Data {
	opt.defaults()
	r := a.Result
	ix := r.Index
	p := i18n.Printer{Lang: opt.Lang}
	d := &Data{
		Tool: "Cadastre", Version: Version, Generated: a.At, Lang: opt.Lang,
		Volume: r.Volume, Engine: r.Engine, Started: r.Started, DurationSec: r.Duration.Seconds(),
		Admin: r.Admin, FromSnapshot: r.FromSnapshot, Totals: ix.Totals(), Recon: a.Recon, Cleanup: opt.Cleanup,
	}
	for _, w := range r.Warnings {
		d.Warnings = append(d.Warnings, p.T(w.Key, w.Args...))
	}
	est := opt.Reclaim
	if est == nil {
		e := clean.EstimateReclaim(a, clean.Env{Admin: r.Admin})
		est = &e
	}
	d.Reclaimable, d.ReclaimPermanentOnly = est.Total, est.PermanentOnly
	for _, f := range a.Findings {
		fd := FindingData{
			ID: f.Rule.ID, Title: f.Rule.Title.In(opt.Lang), Category: f.Rule.Category, Risk: f.Rule.Risk,
			Advice: f.Rule.Advice.In(opt.Lang), Clean: f.Rule.Clean, Deletable: f.Rule.Deletable(),
			Reclaimable: f.Rule.Reclaimable(), Reclaim: est.ByRule[f.Rule.ID],
			Files: f.Files, Size: f.Size, Alloc: f.Alloc, ADSOnly: f.ADSOnly, Groups: f.Groups,
			OlderThan: f.Rule.OlderThan, MinSize: f.Rule.MinSize, CloudSkip: f.CloudSkipped, Unavailable: f.EnvUnavailable,
		}
		if f.Rule.HasOfficial() {
			fd.Official = f.Rule.Official.Title.In(opt.Lang)
		}
		items := append([]uint32(nil), f.Items...)
		sort.Slice(items, func(x, y int) bool { return ix.TotalAlloc(items[x]) > ix.TotalAlloc(items[y]) })
		for k, i := range items {
			if k >= opt.FindingItems {
				fd.MoreItems = len(items) - k
				break
			}
			it := makeItem(ix, i)
			if f.ADSOnly {
				it.Size, it.Alloc = ix.ADSTotals(i)
			}
			fd.Items = append(fd.Items, it)
		}
		d.Findings = append(d.Findings, fd)
	}
	for _, i := range ix.TopFiles(opt.Top, index.ByAlloc, false) {
		d.TopFiles = append(d.TopFiles, makeItem(ix, i))
	}
	for _, ds := range ix.TopDirs(opt.Top, index.ByAlloc) {
		d.TopDirs = append(d.TopDirs, Item{Path: ix.Path(ds.Dir), Dir: true, Size: ds.Size, Alloc: ds.Alloc, Files: ds.Files, MTime: fmtTime(ix.MTime[ds.Dir])})
	}
	exts := ix.Extensions()
	if len(exts) > opt.Top {
		exts = exts[:opt.Top]
	}
	d.Extensions = exts
	d.Treemap = view.Treemap(ix, ix.Root, opt.TreemapNodes)
	return d
}
