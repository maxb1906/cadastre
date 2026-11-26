package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/maxb1906/cadastre/internal/volume"
	"github.com/maxb1906/cadastre/internal/walker"
)

// Scan surveys target (a volume like "C:", a folder or a network share),
// choosing the engine as follows when opt.Engine is "auto" or empty:
//
//   - volume root, NTFS, administrator, Windows → engine A (raw MFT);
//   - otherwise → engine C (directory walk).
//
// If engine A fails, Scan falls back to engine C and records a warning.
func Scan(ctx context.Context, target string, opt Options) (*Result, error) {
	if opt.Image != "" {
		return ScanImage(ctx, opt.Image, opt)
	}
	target = NormalizeTarget(target)
	if runtime.GOOS != "windows" || !isVolumeRoot(target) {
		abs, err := filepath.Abs(target)
		if err == nil && runtime.GOOS != "windows" {
			target = abs
		}
	}
	if _, err := os.Stat(target); err != nil {
		return nil, err
	}
	res := &Result{Started: time.Now(), Admin: volume.IsAdmin()}
	vi, err := volume.Stat(target)
	if err != nil {
		vi = volume.Info{Root: target}
	}
	res.Volume = vi
	res.Volume.Root = target
	res.WholeVolume = isVolumeRoot(target) || isMountRoot(target)
	res.ClusterSize = volume.ClusterSize(target)

	engine := strings.ToLower(opt.Engine)
	if engine == "" {
		engine = "auto"
	}
	canMFT := runtime.GOOS == "windows" && isVolumeRoot(target) && strings.EqualFold(vi.FileSystem, "NTFS")
	switch engine {
	case "auto":
		if canMFT && res.Admin {
			err := scanVolumeMFT(ctx, target, res, opt)
			if err == nil {
				return res, nil
			}
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			res.Warn("warn.mft_fallback", err.Error())
		} else if canMFT {
			res.Warn("warn.not_admin")
		} else if isVolumeRoot(target) && vi.FileSystem != "" && !strings.EqualFold(vi.FileSystem, "NTFS") {
			res.Warn("warn.not_ntfs", vi.FileSystem)
		}
	case EngineMFT:
		if !canMFT {
			return nil, fmt.Errorf("%w: %s", ErrEngine, "MFT engine needs an NTFS volume root on Windows")
		}
		if err := scanVolumeMFT(ctx, target, res, opt); err != nil {
			return nil, err
		}
		return res, nil
	case EngineWalk:
	default:
		return nil, fmt.Errorf("unknown engine %q", opt.Engine)
	}
	return res, scanWalk(ctx, target, res, opt)
}

func scanVolumeMFT(ctx context.Context, root string, res *Result, opt Options) error {
	raw, err := volume.OpenRaw(root)
	if err != nil {
		return err
	}
	defer raw.Close()
	if err := scanMFT(ctx, raw, res, opt); err != nil {
		return err
	}
	res.Engine = EngineMFT
	res.Index.RootPath = root
	return nil
}

func scanWalk(ctx context.Context, root string, res *Result, opt Options) error {
	wo := walker.Options{Workers: opt.Workers, MinSize: opt.MinSize, ClusterSize: res.ClusterSize}
	if opt.Progress != nil {
		wo.Progress = func(files uint64) { opt.Progress(Progress{Phase: "walk", Done: files}) }
	}
	ix, st, err := walker.Walk(ctx, root, wo)
	if err != nil {
		return err
	}
	res.Engine = EngineWalk
	res.Index = ix
	res.WalkErrors = st.Errors
	res.Duration = time.Since(res.Started)
	if st.Denied > 0 {
		res.Warn("warn.denied", st.Denied)
	}
	return nil
}

// isVolumeRoot reports whether t is "X:\".
func isVolumeRoot(t string) bool {
	return len(t) == 3 && isLetter(t[0]) && t[1] == ':' && t[2] == '\\'
}

// isMountRoot reports whether a non-Windows path is the root of its file
// system (its parent lives on another device, or it is "/").
func isMountRoot(p string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	if p == "/" {
		return true
	}
	return deviceID(p) != 0 && deviceID(p) != deviceID(filepath.Dir(p))
}
