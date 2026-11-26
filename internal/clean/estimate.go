package clean

import (
	"github.com/maxb1906/cadastre/internal/analysis"
)

// Reclaim is the space a cleanup of every reclaimable rule would really free:
// it is computed from the cleanup plan itself, so everything the plan leaves
// out (blacklist, metafiles, Cloud Files placeholders, extra hard links,
// system zones without administrator rights) is not counted.
type Reclaim struct {
	Total uint64 `json:"total"`
	// PermanentOnly is the part only freed in permanent mode (Windows\Temp,
	// recycle bins…): the recycle bin makes no sense there.
	PermanentOnly uint64            `json:"permanentOnly"`
	ByRule        map[string]uint64 `json:"byRule"`
}

// EstimateReclaim plans, without touching the disk, the cleanup of every
// reclaimable rule (permanent mode, official actions included) and sums it.
// Processes to close first are not taken into account: closing an
// application is up to the user, and the cleanup screen tells it.
func EstimateReclaim(a *analysis.Analysis, env Env) Reclaim {
	var ids []string
	for _, f := range a.Findings {
		if f.Rule.Reclaimable() {
			ids = append(ids, f.Rule.ID)
		}
	}
	r := Reclaim{ByRule: map[string]uint64{}}
	if len(ids) == 0 {
		return r
	}
	env.Running = nil
	p := Build(a, Selection{Rules: ids, Mode: ModePermanent, Official: true}, env)
	for _, ac := range p.Actions() {
		rule, ok := a.Catalog.Get(ac.Rule)
		if !ok || !rule.Reclaimable() || ac.Bytes == 0 {
			continue
		}
		r.ByRule[ac.Rule] += ac.Bytes
		r.Total += ac.Bytes
		if ac.Kind == ActionDelete && rule.Permanent {
			r.PermanentOnly += ac.Bytes
		}
	}
	return r
}
