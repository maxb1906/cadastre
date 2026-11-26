package rules

import (
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maxb1906/cadastre/internal/index"
)

// Profile is an entry of the Windows ProfileList.
type Profile struct {
	SID  string
	Path string // e.g. C:\Users\bob
}

// Env gives the rules access to system information (Windows only; other
// platforms return errors and the corresponding builtins report nothing).
type Env interface {
	// LookupSID resolves a SID to "DOMAIN\user"; ok is false when the SID
	// does not map to an existing account.
	LookupSID(sid string) (account string, ok bool, err error)
	// Profiles lists the registered user profiles.
	Profiles() ([]Profile, error)
	// InstallerReferences returns the lowercase file names of the .msi/.msp
	// packages referenced by Windows Installer.
	InstallerReferences() (map[string]bool, error)
}

// Group is a labelled part of a finding (a user's recycle bin, a profile).
type Group struct {
	Dir   uint32 `json:"dir"`
	Label string `json:"label"`
	Files uint64 `json:"files"`
	Alloc uint64 `json:"alloc"`
	Ghost bool   `json:"ghost,omitempty"`
}

// Finding is what a rule found in an index.
type Finding struct {
	Rule   *Rule
	Items  []uint32 // files or directories, never nested
	Files  uint64   // number of files covered
	Size   uint64
	Alloc  uint64
	Groups []Group
	// ADSOnly: sizes count only the alternate streams of the items.
	ADSOnly bool
	// Excluded from Items but present: cloud placeholders and folded files.
	CloudSkipped   uint64
	FoldedFiles    uint64
	FoldedAlloc    uint64
	EnvUnavailable bool
}

// Options tunes an evaluation.
type Options struct {
	Now time.Time
	// Prefix is the position of the scanned root in its volume
	// (["Users","bob"] for a scan of C:\Users\bob), nil for a volume root.
	Prefix []string
	// OlderThan overrides the age filter of some rules (rule id → age).
	OlderThan map[string]time.Duration
	Env       Env
	// Only restricts the evaluation to these rule ids (all if empty).
	Only []string
}

// PrefixOf returns the volume-relative segments of a Windows root path:
// `C:\Users\bob` → [Users bob]. Volume roots, shares and non-Windows paths
// give nil.
func PrefixOf(root string) []string {
	if len(root) < 3 || root[1] != ':' || (root[2] != '\\' && root[2] != '/') {
		return nil
	}
	return strings.FieldsFunc(root[3:], func(r rune) bool { return r == '\\' || r == '/' })
}

// Evaluate runs the rules of the catalogue in parallel against a finalized
// index and returns the non-empty findings, largest first.
func Evaluate(ix *index.Index, cat *Catalog, opt Options) []*Finding {
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	var only map[string]bool
	if len(opt.Only) > 0 {
		only = map[string]bool{}
		for _, id := range opt.Only {
			only[id] = true
		}
	}
	var mu sync.Mutex
	var out []*Finding
	var wg sync.WaitGroup
	for _, r := range cat.Rules {
		if only != nil && !only[r.ID] {
			continue
		}
		wg.Add(1)
		go func(r *Rule) {
			defer wg.Done()
			f := evaluate(ix, r, opt)
			if f != nil && (len(f.Items) > 0 || f.Alloc > 0) {
				mu.Lock()
				out = append(out, f)
				mu.Unlock()
			}
		}(r)
	}
	wg.Wait()
	sort.Slice(out, func(a, b int) bool {
		if out[a].Alloc != out[b].Alloc {
			return out[a].Alloc > out[b].Alloc
		}
		return out[a].Rule.ID < out[b].Rule.ID
	})
	return out
}

type collector struct {
	ix     *index.Index
	r      *Rule
	f      *Finding
	cutoff uint32 // files modified at or after this Unix time are kept (0 = no filter)
	names  []string
	seen   map[uint32]bool
}

func evaluate(ix *index.Index, r *Rule, opt Options) *Finding {
	c := &collector{ix: ix, r: r, f: &Finding{Rule: r}, seen: map[uint32]bool{}}
	age := r.olderThan
	if a, ok := opt.OlderThan[r.ID]; ok {
		age = a
	}
	if age > 0 {
		c.cutoff = uint32(max(opt.Now.Add(-age).Unix(), 0))
	}
	for _, n := range r.Names {
		c.names = append(c.names, strings.ToLower(n))
	}
	switch r.Builtin {
	case "large-ads":
		c.largeADS()
		return c.f
	case "ghost-profiles":
		return c.ghostProfiles(opt)
	}
	var matched []uint32
	for k := range r.patterns {
		r.patterns[k].match(ix, opt.Prefix, func(i uint32) { matched = append(matched, i) })
	}
	switch r.Builtin {
	case "installer-orphans":
		c.installerOrphans(matched, opt)
		return c.f
	}
	for _, m := range matched {
		switch {
		case r.Target == TargetItem || !ix.IsDir(m):
			if r.Target == TargetItem || r.Target == TargetContents {
				c.consider(m)
			} else {
				c.considerFile(m)
			}
		case r.Target == TargetContents:
			for _, k := range ix.Kids(m) {
				c.consider(k)
			}
		case r.Target == TargetFiles:
			ix.Walk(m, func(j uint32) bool {
				if ix.IsDir(j) {
					return j == m || ix.Attr[j]&(index.AttrReparsePoint|index.FlagMeta) == 0
				}
				c.considerFile(j)
				return true
			})
		}
	}
	c.dropNested()
	if r.Builtin == "recycle-bins" {
		c.recycleGroups(matched, opt)
	}
	return c.f
}

// consider adds an item (file or whole directory) after filters.
func (c *collector) consider(i uint32) {
	ix := c.ix
	if !ix.Valid(i) || c.seen[i] || ix.Attr[i]&(index.FlagMeta|index.FlagVirtual) != 0 {
		return
	}
	if !ix.IsDir(i) {
		c.considerFile(i)
		return
	}
	if len(c.names) > 0 && !matchesAny(c.names, ix.NameBytes(i)) {
		return
	}
	alloc := ix.TotalAlloc(i)
	if c.r.minSize > 0 && alloc < c.r.minSize {
		return
	}
	c.seen[i] = true
	files, _ := ix.SubtreeCounts(i)
	c.f.Items = append(c.f.Items, i)
	c.f.Files += files
	c.f.Size += ix.TotalSize(i)
	c.f.Alloc += alloc
}

func (c *collector) considerFile(i uint32) {
	ix := c.ix
	if !ix.Valid(i) || c.seen[i] || ix.IsDir(i) || ix.Attr[i]&index.FlagMeta != 0 {
		return
	}
	if ix.Attr[i]&index.FlagFolded != 0 {
		if len(c.names) == 0 && c.r.minSize == 0 {
			c.f.FoldedFiles++
			c.f.FoldedAlloc += ix.TotalAlloc(i)
		}
		return
	}
	if len(c.names) > 0 && !matchesAny(c.names, ix.NameBytes(i)) {
		return
	}
	alloc := ix.TotalAlloc(i)
	if c.r.minSize > 0 && alloc < c.r.minSize {
		return
	}
	if c.cutoff > 0 && ix.MTime[i] >= c.cutoff {
		return
	}
	if ix.Attr[i]&index.FlagCloud != 0 {
		c.f.CloudSkipped++
		return
	}
	c.seen[i] = true
	c.f.Items = append(c.f.Items, i)
	c.f.Files++
	c.f.Size += ix.TotalSize(i)
	c.f.Alloc += alloc
}

// dropNested removes items contained in another item of the same finding.
func (c *collector) dropNested() {
	ix := c.ix
	hasDir := false
	for _, i := range c.f.Items {
		if ix.IsDir(i) {
			hasDir = true
			break
		}
	}
	if !hasDir {
		return
	}
	keep := c.f.Items[:0]
	var files, size, alloc uint64
	for _, i := range c.f.Items {
		nested := false
		for p := ix.Parent[i]; p != index.NoParent && p != ix.Root; p = ix.Parent[p] {
			if c.seen[p] {
				nested = true
				break
			}
		}
		if nested {
			continue
		}
		keep = append(keep, i)
		if ix.IsDir(i) {
			f, _ := ix.SubtreeCounts(i)
			files += f
		} else {
			files++
		}
		size += ix.TotalSize(i)
		alloc += ix.TotalAlloc(i)
	}
	c.f.Items, c.f.Files, c.f.Size, c.f.Alloc = keep, files, size, alloc
}

func (c *collector) largeADS() {
	ix := c.ix
	c.f.ADSOnly = true
	ids := make([]uint32, 0, len(ix.ADS))
	for i := range ix.ADS {
		ids = append(ids, i)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	for _, i := range ids {
		if !ix.Valid(i) || ix.Attr[i]&index.FlagMeta != 0 {
			continue
		}
		size, alloc := ix.ADSTotals(i)
		if alloc < max(c.r.minSize, 1) {
			continue
		}
		c.f.Items = append(c.f.Items, i)
		c.f.Files++
		c.f.Size += size
		c.f.Alloc += alloc
	}
}

func (c *collector) recycleGroups(matched []uint32, opt Options) {
	ix := c.ix
	for _, d := range matched {
		if !ix.IsDir(d) {
			continue
		}
		g := Group{Dir: d, Label: ix.Name(d)}
		for _, it := range c.f.Items {
			if ix.Parent[it] == d {
				g.Alloc += ix.TotalAlloc(it)
				if ix.IsDir(it) {
					f, _ := ix.SubtreeCounts(it)
					g.Files += f
				} else {
					g.Files++
				}
			}
		}
		if g.Alloc == 0 && g.Files == 0 {
			continue
		}
		if opt.Env != nil {
			if acct, ok, err := opt.Env.LookupSID(ix.Name(d)); err == nil {
				if ok {
					g.Label = acct
				} else {
					g.Ghost = true
				}
			}
		}
		c.f.Groups = append(c.f.Groups, g)
	}
	sort.Slice(c.f.Groups, func(a, b int) bool { return c.f.Groups[a].Alloc > c.f.Groups[b].Alloc })
}

// Folders of C:\Users that are not user profiles.
var nonProfiles = map[string]bool{"public": true, "default": true, "default user": true, "all users": true, "defaultapppool": true}

func (c *collector) ghostProfiles(opt Options) *Finding {
	ix := c.ix
	if opt.Env == nil {
		c.f.EnvUnavailable = true
		return c.f
	}
	profiles, err := opt.Env.Profiles()
	if err != nil {
		c.f.EnvUnavailable = true
		return c.f
	}
	bySID := map[string]Profile{}
	byDir := map[string]Profile{}
	for _, p := range profiles {
		bySID[p.SID] = p
		byDir[strings.ToLower(path.Base(strings.ReplaceAll(p.Path, `\`, "/")))] = p
	}
	var matched []uint32
	for k := range c.r.patterns {
		c.r.patterns[k].match(ix, opt.Prefix, func(i uint32) { matched = append(matched, i) })
	}
	for _, d := range matched {
		name := strings.ToLower(ix.Name(d))
		if !ix.IsDir(d) || nonProfiles[name] || ix.Attr[d]&index.AttrReparsePoint != 0 {
			continue
		}
		p, registered := byDir[name]
		label := ""
		ghost := false
		if !registered {
			ghost, label = true, "?"
		} else if acct, ok, err := opt.Env.LookupSID(p.SID); err == nil && !ok {
			ghost, label = true, p.SID
		} else if err == nil {
			label = acct
		}
		if !ghost {
			continue
		}
		files, _ := ix.SubtreeCounts(d)
		c.f.Items = append(c.f.Items, d)
		c.f.Files += files
		c.f.Size += ix.TotalSize(d)
		c.f.Alloc += ix.TotalAlloc(d)
		c.f.Groups = append(c.f.Groups, Group{Dir: d, Label: label, Files: files, Alloc: ix.TotalAlloc(d), Ghost: true})
	}
	return c.f
}

func (c *collector) installerOrphans(matched []uint32, opt Options) {
	ix := c.ix
	if opt.Env == nil {
		c.f.EnvUnavailable = true
		return
	}
	refs, err := opt.Env.InstallerReferences()
	if err != nil || len(refs) == 0 {
		// Without the registry, or with an unreadable one, every package
		// would look orphaned: report nothing rather than false positives.
		c.f.EnvUnavailable = true
		return
	}
	for _, d := range matched {
		for _, k := range ix.Kids(d) {
			if ix.IsDir(k) || !matchesAny(c.names, ix.NameBytes(k)) {
				continue
			}
			if refs[strings.ToLower(ix.Name(k))] {
				continue
			}
			c.considerFile(k)
		}
	}
}
