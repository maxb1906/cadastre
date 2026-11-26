// Package analysis turns a scan result into everything the interfaces show:
// ghost findings (rules) and the reconciliation of the used space.
package analysis

import (
	"sync"
	"time"

	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/reclaim"
	"github.com/maxb1906/cadastre/internal/rules"
	"github.com/maxb1906/cadastre/internal/sysinfo"
	"github.com/maxb1906/cadastre/internal/volume"
)

// Analysis is a scan with its findings and space reconciliation.
type Analysis struct {
	Result   *engine.Result
	Catalog  *rules.Catalog
	Findings []*rules.Finding
	Recon    *reclaim.Reconciliation
	At       time.Time
}

// Options tunes the analysis.
type Options struct {
	OlderThan map[string]time.Duration
	Only      []string
	// NoSystem skips system queries (WMI, registry): for tests and images.
	NoSystem bool
}

// Run evaluates the rules and reconciles the space, in parallel.
func Run(res *engine.Result, cat *rules.Catalog, opt Options) *Analysis {
	a := &Analysis{Result: res, Catalog: cat, At: time.Now()}
	system := !opt.NoSystem && res.Volume.Kind != "image" && !res.FromSnapshot
	var vss reclaim.VSSInfo
	var wg sync.WaitGroup
	if system {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if st, err := sysinfo.ShadowStorageOn(volume.GUIDPath(res.Volume.Root)); err == nil {
				vss = reclaim.VSSInfo{Known: true, Used: st.Used, Allocated: st.Allocated, Max: st.Max, Copies: st.Copies}
			}
		}()
	}
	ro := rules.Options{Now: a.At, Prefix: rules.PrefixOf(res.Index.RootPath), OlderThan: opt.OlderThan, Only: opt.Only}
	if system {
		ro.Env = sysinfo.Env{}
	}
	a.Findings = rules.Evaluate(res.Index, cat, ro)
	wg.Wait()
	if res.WholeVolume {
		a.Recon = reclaim.Compute(res, vss)
	}
	return a
}

// Finding returns the finding of a rule, if any.
func (a *Analysis) Finding(id string) *rules.Finding {
	for _, f := range a.Findings {
		if f.Rule.ID == id {
			return f
		}
	}
	return nil
}
