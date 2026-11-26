// Package rules holds the catalogue of "ghost" files (dumps, caches, logs,
// leftovers...), described in embedded YAML and extensible with custom YAML
// files, and evaluates it against a scan index.
package rules

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/maxb1906/cadastre/internal/units"
)

//go:embed catalog.yaml
var builtinCatalog []byte

// Risk levels.
const (
	RiskGreen  = "green"  // safe: selectable directly
	RiskOrange = "orange" // with care: explicit confirmation
	RiskRed    = "red"    // never deleted by Cadastre
)

// Clean modes.
const (
	CleanDelete   = "delete"
	CleanOfficial = "official"
	CleanNone     = "none"
)

// Targets.
const (
	TargetItem     = "item"
	TargetContents = "contents"
	TargetFiles    = "files"
)

// Text is a message in French and English.
type Text struct {
	FR string `yaml:"fr" json:"fr"`
	EN string `yaml:"en" json:"en"`
}

// In returns the text in lang.
func (t Text) In(lang string) string {
	if lang == "fr" && t.FR != "" {
		return t.FR
	}
	if t.EN != "" {
		return t.EN
	}
	return t.FR
}

// Step is one step of an official action: run a command, or let Cadastre
// delete the rule's items (with every safeguard) at that point.
type Step struct {
	Run   []string `yaml:"run,omitempty" json:"run,omitempty"`
	Clean string   `yaml:"clean,omitempty" json:"clean,omitempty"` // "items"
}

// Official is the official way to reclaim the space of a rule.
type Official struct {
	Title        Text   `yaml:"title" json:"title"`
	Steps        []Step `yaml:"steps" json:"steps"`
	PerItem      bool   `yaml:"per_item" json:"perItem"`
	RestorePoint bool   `yaml:"restore_point" json:"restorePoint"`
	// Reclaims tells that the action deletes the targets, so their space
	// counts as reclaimable. Off for actions that change the configuration
	// (powercfg /h off) or whose gain is unknown (Optimize-VHD, wevtutil cl).
	// An action with a "clean: items" step always reclaims its items.
	Reclaims bool `yaml:"reclaims" json:"reclaims"`
}

// Rule is one entry of the catalogue.
type Rule struct {
	ID         string    `yaml:"id" json:"id"`
	Category   string    `yaml:"category" json:"category"`
	Risk       string    `yaml:"risk" json:"risk"`
	Title      Text      `yaml:"title" json:"title"`
	Advice     Text      `yaml:"advice" json:"advice"`
	Paths      []string  `yaml:"paths" json:"paths"`
	Names      []string  `yaml:"names" json:"names,omitempty"`
	Target     string    `yaml:"target" json:"target"`
	OlderThan  string    `yaml:"older_than" json:"olderThan,omitempty"`
	MinSize    string    `yaml:"min_size" json:"minSize,omitempty"`
	System     bool      `yaml:"system" json:"system"`
	Permanent  bool      `yaml:"permanent" json:"permanent"`
	Clean      string    `yaml:"clean" json:"clean"`
	Official   *Official `yaml:"official" json:"official,omitempty"`
	Builtin    string    `yaml:"builtin" json:"builtin,omitempty"`
	CloseFirst []string  `yaml:"close_first" json:"closeFirst,omitempty"`
	Source     string    `yaml:"-" json:"source"` // "builtin" or the custom file

	patterns  []pattern
	olderThan time.Duration
	minSize   uint64
}

// Deletable reports whether Cadastre may delete the rule's items itself.
func (r *Rule) Deletable() bool { return r.Clean == CleanDelete && r.Risk != RiskRed }

// Reclaimable reports whether the rule's space counts as reclaimable: Cadastre
// may delete its items itself, or run an official action that deletes them
// (never for red rules, whose official action is only offered). Official
// actions that change the configuration or compact a file do not count.
func (r *Rule) Reclaimable() bool {
	if r.Deletable() {
		return true
	}
	return r.Clean == CleanOfficial && r.Risk != RiskRed && r.HasOfficial() && (r.Official.Reclaims || r.Official.cleansItems())
}

func (o *Official) cleansItems() bool {
	for _, s := range o.Steps {
		if s.Clean == "items" {
			return true
		}
	}
	return false
}

// HasOfficial reports whether the rule has an official action.
func (r *Rule) HasOfficial() bool { return r.Official != nil && len(r.Official.Steps) > 0 }

// OlderThanDuration returns the parsed age filter (0 = none).
func (r *Rule) OlderThanDuration() time.Duration { return r.olderThan }

// MinSizeBytes returns the parsed size filter (0 = none).
func (r *Rule) MinSizeBytes() uint64 { return r.minSize }

// Catalog is an ordered set of rules.
type Catalog struct {
	Rules []*Rule
	byID  map[string]*Rule
}

// Get returns the rule with that id.
func (c *Catalog) Get(id string) (*Rule, bool) {
	r, ok := c.byID[id]
	return r, ok
}

type file struct {
	Rules []*Rule `yaml:"rules"`
}

// Load returns the built-in catalogue extended with custom rule files. A
// custom rule with the id of an existing rule replaces it; others are added.
func Load(custom ...string) (*Catalog, error) {
	c := &Catalog{byID: map[string]*Rule{}}
	if err := c.add(builtinCatalog, "builtin"); err != nil {
		return nil, fmt.Errorf("built-in catalogue: %w", err)
	}
	for _, path := range custom {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := c.add(b, path); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	return c, nil
}

// MustBuiltin returns the built-in catalogue and panics on error (it is
// validated by tests).
func MustBuiltin() *Catalog {
	c, err := Load()
	if err != nil {
		panic(err)
	}
	return c
}

func (c *Catalog) add(b []byte, source string) error {
	var f file
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, r := range f.Rules {
		r.Source = source
		if err := r.compile(); err != nil {
			return fmt.Errorf("rule %q: %w", r.ID, err)
		}
		if seen[r.ID] {
			return fmt.Errorf("duplicate rule id %q", r.ID)
		}
		seen[r.ID] = true
		if old, ok := c.byID[r.ID]; ok {
			for k, x := range c.Rules {
				if x == old {
					c.Rules[k] = r
				}
			}
		} else {
			c.Rules = append(c.Rules, r)
		}
		c.byID[r.ID] = r
	}
	return nil
}

var builtins = map[string]bool{"installer-orphans": true, "recycle-bins": true, "ghost-profiles": true, "large-ads": true}

func (r *Rule) compile() error {
	if r.ID == "" || strings.ContainsAny(r.ID, " \t,") {
		return errors.New("invalid id")
	}
	switch r.Risk {
	case RiskGreen, RiskOrange, RiskRed:
	default:
		return fmt.Errorf("invalid risk %q", r.Risk)
	}
	if r.Target == "" {
		r.Target = TargetItem
	}
	switch r.Target {
	case TargetItem, TargetContents, TargetFiles:
	default:
		return fmt.Errorf("invalid target %q", r.Target)
	}
	if r.Clean == "" {
		r.Clean = CleanNone
	}
	switch r.Clean {
	case CleanDelete, CleanOfficial, CleanNone:
	default:
		return fmt.Errorf("invalid clean mode %q", r.Clean)
	}
	// Policy, not configurable: a red rule is never deleted by Cadastre,
	// whatever a custom file says.
	if r.Risk == RiskRed && r.Clean == CleanDelete {
		r.Clean = CleanNone
	}
	if r.Clean == CleanOfficial && !r.HasOfficial() {
		return errors.New("clean: official without official steps")
	}
	if r.Official != nil {
		for _, s := range r.Official.Steps {
			if len(s.Run) == 0 && s.Clean != "items" {
				return errors.New("official step must have run or clean: items")
			}
			if s.Clean == "items" && r.Risk == RiskRed {
				return errors.New("a red rule cannot delete items, even in an official action")
			}
		}
	}
	if r.Builtin != "" && !builtins[r.Builtin] {
		return fmt.Errorf("unknown builtin %q", r.Builtin)
	}
	if len(r.Paths) == 0 && r.Builtin != "large-ads" {
		return errors.New("no paths")
	}
	for _, p := range r.Paths {
		pt, err := compilePattern(p)
		if err != nil {
			return err
		}
		r.patterns = append(r.patterns, pt)
	}
	for k, n := range r.Names {
		if strings.ContainsAny(n, `\/`) {
			return fmt.Errorf("names must not contain separators: %q", n)
		}
		r.Names[k] = strings.ToLower(n)
	}
	var err error
	if r.olderThan, err = units.ParseAge(r.OlderThan); err != nil {
		return err
	}
	if r.MinSize != "" {
		if r.minSize, err = units.ParseSize(r.MinSize); err != nil {
			return err
		}
	}
	return nil
}
