package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maxb1906/cadastre/internal/mftscan"
	"github.com/maxb1906/cadastre/internal/ntfs"
	"github.com/maxb1906/cadastre/internal/volume"
)

// Progress reports the advancement of a scan.
type Progress struct {
	Phase string `json:"phase"` // "mft", "walk", "finalize"
	Done  uint64 `json:"done"`
	Total uint64 `json:"total"` // 0 when unknown
}

// Options tunes a scan.
type Options struct {
	Engine   string // "auto" (default), "mft" or "walk"
	Image    string // NTFS image file to scan with engine A instead of a volume
	MinSize  uint64
	Workers  int
	Progress func(Progress)
}

// ErrEngine is returned when the requested engine cannot run.
var ErrEngine = errors.New("engine unavailable")

// ScanImage scans an NTFS volume image with engine A.
func ScanImage(ctx context.Context, path string, opt Options) (*Result, error) {
	raw, err := volume.OpenImage(path)
	if err != nil {
		return nil, err
	}
	defer raw.Close()
	res := &Result{Engine: EngineMFT, Started: time.Now(), Admin: volume.IsAdmin(), WholeVolume: true}
	res.Volume = volume.Info{Root: `\`, Label: path, FileSystem: "NTFS", Kind: "image"}
	if err := scanMFT(ctx, raw, res, opt); err != nil {
		return nil, err
	}
	vi, _ := raw.NTFSInfo()
	cs := uint64(vi.BytesPerCluster)
	res.Volume.Total = vi.TotalClusters * cs
	// No OS to ask for the free space of an image: count the clusters
	// marked in use in $Bitmap, exactly like NTFS does.
	if l, err := ntfs.ReadLayout(raw, vi); err == nil {
		if used, err := ntfs.UsedClusters(raw, l); err == nil && used <= vi.TotalClusters {
			res.Volume.Free = (vi.TotalClusters - used) * cs
		}
	}
	res.Index.RootPath = `\`
	return res, nil
}

func scanMFT(ctx context.Context, raw volume.Raw, res *Result, opt Options) error {
	vi, err := raw.NTFSInfo()
	if err != nil {
		return err
	}
	res.ClusterSize = vi.BytesPerCluster
	mo := mftscan.Options{MinSize: opt.MinSize, Workers: opt.Workers}
	if opt.Progress != nil {
		mo.Progress = func(done, total uint64) { opt.Progress(Progress{Phase: "mft", Done: done, Total: total}) }
	}
	ix, st, err := mftscan.Scan(ctx, raw, vi, mo)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEngine, err)
	}
	res.Index = ix
	res.MFTRecords, res.MFTBad, res.MFTBytes, res.MFTRuns = st.Records, st.Bad, st.MFTBytes, st.MFTRuns
	res.Duration = time.Since(res.Started)
	if st.Bad > 0 {
		res.Warn("warn.mft_bad", st.Bad)
	}
	return nil
}

// NormalizeTarget turns "c", "C:", "c:\" into "C:\"; other paths are kept.
func NormalizeTarget(t string) string {
	s := strings.TrimSpace(t)
	if len(s) == 1 && isLetter(s[0]) {
		return strings.ToUpper(s) + `:\`
	}
	if len(s) >= 2 && isLetter(s[0]) && s[1] == ':' && strings.Trim(s[2:], `\/`) == "" {
		return strings.ToUpper(s[:1]) + `:\`
	}
	return s
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
