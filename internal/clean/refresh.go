package clean

import (
	"context"
	"os"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/volume"
	"github.com/maxb1906/cadastre/internal/walker"
)

// Refresh brings the index up to date after a cleanup: deleted entries are
// removed and the zones touched by official actions are rescanned; then the
// analysis is recomputed.
func Refresh(ctx context.Context, a *analysis.Analysis, r *Result, opt analysis.Options) *analysis.Analysis {
	ix := a.Result.Index
	for _, i := range r.Removed {
		if ix.Valid(i) {
			ix.MarkRemoved(i)
		}
	}
	ix.Finalize()
	for _, p := range r.Rescan {
		i, ok := ix.Find(p)
		if !ok {
			continue
		}
		st, err := os.Lstat(p)
		switch {
		case err != nil:
			ix.MarkRemoved(i)
		case st.IsDir() && ix.IsDir(i):
			sub, _, err := walker.Walk(ctx, p, walker.Options{ClusterSize: volume.ClusterSize(p)})
			if err == nil {
				ix.Graft(i, sub)
			}
		case !st.IsDir():
			ix.Size[i] = uint64(st.Size())
			ix.Alloc[i] = (uint64(st.Size()) + 4095) &^ 4095
			ix.MTime[i] = uint32(max(st.ModTime().Unix(), 0))
		}
		ix.Finalize()
	}
	if v, err := volume.Stat(a.Result.Volume.Root); err == nil && a.Result.Volume.Kind != "image" {
		a.Result.Volume.Free, a.Result.Volume.Total = v.Free, v.Total
	}
	_ = index.NoParent
	return analysis.Run(a.Result, a.Catalog, opt)
}
