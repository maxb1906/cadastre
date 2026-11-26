package clean

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/rules"
)

// Modes of deletion.
const (
	ModeRecycle   = "recycle"
	ModePermanent = "permanent"
)

// Skip reasons besides the blacklist (i18n keys "clean.reason.*").
const (
	ReasonRed           = "red"            // red rule: never deleted
	ReasonReportOnly    = "report_only"    // rule without cleanup
	ReasonOfficialOnly  = "official_only"  // only the official action may reclaim it
	ReasonCloud         = "cloud"          // Cloud Files placeholder
	ReasonFolded        = "folded"         // indexed without name (--min-size)
	ReasonAdmin         = "admin"          // system zone without administrator rights
	ReasonNeedPermanent = "need_permanent" // recycle bin makes no sense there
	ReasonProcess       = "process"        // an application must be closed first
	ReasonChanged       = "changed"        // modified since the scan
	ReasonGone          = "gone"           // no longer exists
	ReasonTypeChanged   = "type_changed"   // file became folder or link, or the reverse
	ReasonIDChanged     = "id_changed"     // replaced by another file (file number)
	ReasonUnreadable    = "unreadable"     // could not be checked
	ReasonLocked        = "locked"         // in use
	ReasonDenied        = "denied"         // access denied
	ReasonFailed        = "failed"         // other error
	ReasonNotEmpty      = "not_empty"      // folder still holds files
	ReasonCanceled      = "canceled"
	ReasonUnknownRule   = "unknown_rule"
	ReasonNotPrepared   = "not_prepared"
)

// Item is one file, link or folder to delete.
type Item struct {
	Entry      uint32   `json:"entry"`
	Path       string   `json:"path"`
	Rule       string   `json:"rule,omitempty"` // "" for a manual selection
	Risk       string   `json:"risk"`
	Dir        bool     `json:"dir,omitempty"` // empty folder to remove after its files
	Reparse    bool     `json:"reparse,omitempty"`
	Size       uint64   `json:"size"`
	Alloc      uint64   `json:"alloc"` // space freed (0 for an extra hard link)
	MTime      uint32   `json:"mtime"`
	FileID     uint64   `json:"fileId,omitempty"` // expected file number, 0 if unknown
	Hardlink   bool     `json:"hardlink,omitempty"`
	Permanent  bool     `json:"permanentOnly,omitempty"`
	System     bool     `json:"system,omitempty"`
	CloseFirst []string `json:"closeFirst,omitempty"`
}

// Skip is an item left out, and why.
type Skip struct {
	Path   string `json:"path"`
	Rule   string `json:"rule,omitempty"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
	Alloc  uint64 `json:"alloc"`
}

// Official is an official action of the plan.
type Official struct {
	Rule         string     `json:"rule"`
	Title        rules.Text `json:"title"`
	Risk         string     `json:"risk"`
	Commands     [][]string `json:"commands"` // expanded, in order; nil entry = "delete the items here"
	RestorePoint bool       `json:"restorePoint"`
	Items        []Item     `json:"items,omitempty"` // deleted at the "clean: items" step
	Targets      []string   `json:"targets"`         // paths concerned (for the journal and the rescan)
	Alloc        uint64     `json:"alloc"`
	System       bool       `json:"system"`
}

// Selection is what the user asked to clean.
type Selection struct {
	Rules     []string                 `json:"rules"`
	Entries   []uint32                 `json:"entries"`
	OlderThan map[string]time.Duration `json:"-"`
	Mode      string                   `json:"mode"`
	// Official includes the official actions of the selected rules.
	Official bool `json:"official"`
}

// Plan is the exact list of what a cleanup would do. It must be prepared
// (live checks) before it can be executed.
type Plan struct {
	ID           string     `json:"id"`
	Created      time.Time  `json:"created"`
	Root         string     `json:"root"`
	Engine       string     `json:"engine"`
	Mode         string     `json:"mode"`
	FromSnapshot bool       `json:"fromSnapshot"`
	Items        []Item     `json:"items"` // files and links first, then folders deepest first
	Skipped      []Skip     `json:"skipped"`
	Official     []Official `json:"official"`
	Bytes        uint64     `json:"bytes"`
	Files        int        `json:"files"`
	Orange       bool       `json:"orange"` // needs the explicit confirmation
	Prepared     bool       `json:"prepared"`
	Admin        bool       `json:"admin"`

	prefix []string
}

// Env tells the planner about the session.
type Env struct {
	Admin   bool
	Running map[string]bool // lowercase process names (nil: unknown)
	Now     time.Time
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// relSegments returns the volume-relative segments of entry i.
func relSegments(ix *index.Index, i uint32, prefix []string) []string {
	var chain []string
	for j := i; j != ix.Root && j != index.NoParent; j = ix.Parent[j] {
		chain = append(chain, ix.Name(j))
		if len(chain) > 1<<12 {
			break
		}
	}
	out := append([]string(nil), prefix...)
	for k := len(chain) - 1; k >= 0; k-- {
		out = append(out, chain[k])
	}
	return out
}

// fileID returns the expected file number of an entry (MFT engine only).
func fileID(res *engine.Result, i uint32) uint64 {
	if res.Engine != engine.EngineMFT || res.FromSnapshot && res.Index.Seq[i] == 0 {
		return 0
	}
	return uint64(res.Index.Seq[i])<<48 | uint64(i)
}

type builder struct {
	a      *analysis.Analysis
	ix     *index.Index
	p      *Plan
	env    Env
	seen   map[uint32]bool
	dirs   map[uint32]bool
	byRule map[string]*rules.Rule
}

// Build turns a selection into a plan. It only reads the index: nothing is
// touched on disk. Call Prepare before Execute.
func Build(a *analysis.Analysis, sel Selection, env Env) *Plan {
	res := a.Result
	if sel.Mode != ModePermanent {
		sel.Mode = ModeRecycle
	}
	if env.Now.IsZero() {
		env.Now = time.Now()
	}
	p := &Plan{ID: newID(), Created: env.Now, Root: res.Index.RootPath, Engine: res.Engine, Mode: sel.Mode,
		FromSnapshot: res.FromSnapshot, Admin: env.Admin, prefix: rules.PrefixOf(res.Index.RootPath)}
	b := &builder{a: a, ix: res.Index, p: p, env: env, seen: map[uint32]bool{}, dirs: map[uint32]bool{}, byRule: map[string]*rules.Rule{}}

	for _, id := range sel.Rules {
		r, ok := a.Catalog.Get(id)
		if !ok {
			p.Skipped = append(p.Skipped, Skip{Rule: id, Reason: ReasonUnknownRule})
			continue
		}
		b.byRule[id] = r
		f := a.Finding(id)
		if f == nil {
			continue // nothing found for this rule
		}
		// Re-evaluate with a custom age when asked (CLI --older-than).
		if age, ok := sel.OlderThan[id]; ok {
			fs := rules.Evaluate(b.ix, a.Catalog, rules.Options{Now: env.Now, Prefix: p.prefix, OlderThan: map[string]time.Duration{id: age}, Only: []string{id}})
			f = nil
			if len(fs) > 0 {
				f = fs[0]
			}
			if f == nil {
				continue
			}
		}
		b.addRule(r, f, sel)
	}
	for _, e := range sel.Entries {
		b.addManual(e, sel)
	}
	b.finish()
	return p
}

func (b *builder) skip(i uint32, rule, reason, detail string) {
	b.p.Skipped = append(b.p.Skipped, Skip{Path: b.ix.Path(i), Rule: rule, Reason: reason, Detail: detail, Alloc: b.ix.TotalAlloc(i)})
}

func (b *builder) addRule(r *rules.Rule, f *rules.Finding, sel Selection) {
	switch {
	case r.Risk == rules.RiskRed && !r.HasOfficial():
		for _, i := range f.Items {
			b.skip(i, r.ID, ReasonRed, "")
		}
		return
	case r.Clean == rules.CleanNone:
		for _, i := range f.Items {
			b.skip(i, r.ID, ReasonReportOnly, "")
		}
		return
	}
	if r.System && !b.env.Admin {
		for _, i := range f.Items {
			b.skip(i, r.ID, ReasonAdmin, "")
		}
		return
	}
	if running := b.running(r.CloseFirst); running != "" {
		for _, i := range f.Items {
			b.skip(i, r.ID, ReasonProcess, running)
		}
		return
	}
	official := sel.Official && r.HasOfficial()
	if r.Clean == rules.CleanOfficial || r.Risk == rules.RiskRed {
		if !official {
			for _, i := range f.Items {
				b.skip(i, r.ID, ReasonOfficialOnly, r.Official.Title.EN)
			}
			return
		}
		b.addOfficial(r, f, sel)
		return
	}
	if official {
		b.addOfficial(r, f, sel)
		return
	}
	for _, i := range f.Items {
		b.addEntry(i, r.ID, r.Risk, true, r.Permanent, r.System, r.CloseFirst, sel.Mode, &b.p.Items)
	}
}

func (b *builder) running(names []string) string {
	if b.env.Running == nil {
		return ""
	}
	for _, n := range names {
		if b.env.Running[strings.ToLower(n)] {
			return n
		}
	}
	return ""
}

func (b *builder) addManual(i uint32, sel Selection) {
	ix := b.ix
	if !ix.Valid(i) {
		return
	}
	b.addEntry(i, "", rules.RiskOrange, false, false, false, nil, sel.Mode, &b.p.Items)
}

// addEntry adds entry i (file, link or whole folder) after the guardrails,
// expanding folders into their files plus the folders to remove.
func (b *builder) addEntry(i uint32, rule, risk string, viaRule, permanentOnly, system bool, closeFirst []string, mode string, out *[]Item) {
	ix := b.ix
	if b.seen[i] {
		return
	}
	for p := ix.Parent[i]; p != index.NoParent && p != ix.Root; p = ix.Parent[p] {
		if b.seen[p] {
			return // already covered by a selected ancestor
		}
	}
	if blocked, reason := Blocked(relSegments(ix, i, b.p.prefix), viaRule); blocked {
		b.skip(i, rule, reason, "")
		return
	}
	if ix.Has(i, index.FlagMeta) {
		b.skip(i, rule, ReasonMetafile, "")
		return
	}
	if permanentOnly && mode != ModePermanent {
		b.skip(i, rule, ReasonNeedPermanent, "")
		return
	}
	if !viaRule && !b.env.Admin && systemArea(relSegments(ix, i, b.p.prefix)) {
		b.skip(i, rule, ReasonAdmin, "")
		return
	}
	b.seen[i] = true
	if risk == rules.RiskOrange {
		b.p.Orange = true
	}
	mk := func(j uint32, dir bool) Item {
		it := Item{Entry: j, Path: ix.Path(j), Rule: rule, Risk: risk, Dir: dir, Reparse: ix.Has(j, index.AttrReparsePoint),
			MTime: ix.MTime[j], FileID: fileID(b.a.Result, j), Hardlink: ix.Has(j, index.FlagHardlink),
			Permanent: permanentOnly, System: system, CloseFirst: closeFirst}
		if !dir {
			it.Size = ix.Size[j]
			if !it.Hardlink {
				it.Alloc = ix.TotalAlloc(j)
			}
		}
		return it
	}
	// A file, or a link/junction (deleted itself, never followed).
	if !ix.IsDir(i) || ix.Has(i, index.AttrReparsePoint) {
		if ix.Has(i, index.FlagCloud) {
			b.skip(i, rule, ReasonCloud, "")
			return
		}
		if ix.Has(i, index.FlagFolded) {
			b.skip(i, rule, ReasonFolded, "")
			return
		}
		*out = append(*out, mk(i, ix.IsDir(i)))
		return
	}
	// A folder: its files, then the folder itself and its subfolders,
	// removed only if they end up empty.
	ix.Walk(i, func(j uint32) bool {
		if !ix.Valid(j) {
			return false
		}
		if ix.IsDir(j) && !ix.Has(j, index.AttrReparsePoint) {
			if ix.Has(j, index.FlagDenied) {
				b.skip(j, rule, ReasonDenied, "")
			}
			b.dirs[j] = true
			*out = append(*out, mk(j, true))
			return true
		}
		switch {
		case ix.Has(j, index.FlagCloud):
			b.skip(j, rule, ReasonCloud, "")
		case ix.Has(j, index.FlagFolded):
			b.skip(j, rule, ReasonFolded, "")
		case ix.Has(j, index.FlagMeta):
			b.skip(j, rule, ReasonMetafile, "")
		default:
			*out = append(*out, mk(j, ix.IsDir(j)))
		}
		return false
	})
}

// systemArea: locations needing administrator rights.
func systemArea(segs []string) bool {
	if len(segs) == 0 {
		return true
	}
	switch strings.ToLower(segs[0]) {
	case "windows", "program files", "program files (x86)", "programdata", "$recycle.bin", "inetpub":
		return true
	}
	return false
}

func (b *builder) addOfficial(r *rules.Rule, f *rules.Finding, sel Selection) {
	ix := b.ix
	o := Official{Rule: r.ID, Title: r.Official.Title, Risk: r.Risk, RestorePoint: r.Official.RestorePoint, System: r.System}
	for _, i := range f.Items {
		o.Targets = append(o.Targets, ix.Path(i))
		o.Alloc += ix.TotalAlloc(i)
	}
	expand := func(step rules.Step, item string) []string {
		out := make([]string, len(step.Run))
		for k, a := range step.Run {
			out[k] = expandPlaceholders(a, item, b.p.Root)
		}
		return out
	}
	if r.Official.PerItem {
		for _, t := range o.Targets {
			for _, s := range r.Official.Steps {
				if len(s.Run) > 0 {
					o.Commands = append(o.Commands, expand(s, t))
				}
			}
		}
	} else {
		for _, s := range r.Official.Steps {
			if s.Clean == "items" {
				o.Commands = append(o.Commands, nil)
				for _, i := range f.Items {
					b.addEntry(i, r.ID, r.Risk, true, false, r.System, r.CloseFirst, ModePermanent, &o.Items)
				}
				continue
			}
			o.Commands = append(o.Commands, expand(s, ""))
		}
	}
	if r.Risk == rules.RiskOrange {
		b.p.Orange = true
	}
	b.p.Official = append(b.p.Official, o)
}

// finish orders the items (files before folders, deepest folders first) and
// computes the totals.
func (b *builder) finish() {
	order := func(items []Item) {
		sort.SliceStable(items, func(x, y int) bool {
			if items[x].Dir != items[y].Dir {
				return !items[x].Dir
			}
			if items[x].Dir {
				return strings.Count(items[x].Path, string(b.ix.Sep)) > strings.Count(items[y].Path, string(b.ix.Sep))
			}
			return false
		})
	}
	order(b.p.Items)
	for k := range b.p.Official {
		order(b.p.Official[k].Items)
	}
	for _, it := range b.p.Items {
		if !it.Dir {
			b.p.Files++
			b.p.Bytes += it.Alloc
		}
	}
	for _, o := range b.p.Official {
		b.p.Bytes += o.Alloc
	}
}
