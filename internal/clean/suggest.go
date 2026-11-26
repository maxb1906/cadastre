package clean

import (
	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/rules"
)

// Suggested returns the rules a suggested cleanup checks by default, in the
// order of the findings: rules whose space is really reclaimable (see
// EstimateReclaim), green only unless orange is true. Red rules and actions
// that change the configuration are never suggested: they have no
// reclaimable space.
func Suggested(a *analysis.Analysis, est Reclaim, orange bool) []string {
	var out []string
	for _, f := range a.Findings {
		r := f.Rule
		if est.ByRule[r.ID] == 0 {
			continue
		}
		if r.Risk == rules.RiskGreen || orange && r.Risk == rules.RiskOrange {
			out = append(out, r.ID)
		}
	}
	return out
}
