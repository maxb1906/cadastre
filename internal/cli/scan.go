package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/report"
	"github.com/maxb1906/cadastre/internal/units"
)

func runScan(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "scan", "cli.scan.usage")
	top := fs.Int("top", 20, i18n.T(env.Lang, "cli.flag.top"))
	image := fs.String("image", "", i18n.T(env.Lang, "cli.flag.image"))
	minSize := fs.String("min-size", "0", i18n.T(env.Lang, "cli.flag.minsize"))
	quiet := fs.Bool("quiet", false, i18n.T(env.Lang, "cli.flag.quiet"))
	pos, err := parseFlags(fs, args)
	if err != nil {
		return errUsage
	}
	ms, err := units.ParseSize(*minSize)
	if err != nil {
		return err
	}
	opt := engine.Options{MinSize: ms}
	if !*quiet {
		opt.Progress = progressPrinter(env)
	}
	var res *engine.Result
	switch {
	case *image != "":
		res, err = engine.ScanImage(ctx, *image, opt)
	case len(pos) == 1:
		err = fmt.Errorf("%s", i18n.T(env.Lang, "cli.scan.noengine"))
	default:
		fs.Usage()
		return errUsage
	}
	if err != nil {
		return err
	}
	if !*quiet {
		fmt.Fprint(env.Stderr, "\r\033[K")
	}
	return report.WriteText(env.Stdout, res, *top, env.Lang)
}

func progressPrinter(env *Env) func(engine.Progress) {
	last := -1
	return func(p engine.Progress) {
		if p.Total == 0 {
			return
		}
		pct := int(100 * p.Done / p.Total)
		if pct == last {
			return
		}
		last = pct
		fmt.Fprintf(env.Stderr, "\r%s %3d %%", i18n.T(env.Lang, "progress."+p.Phase), pct)
		if f, ok := env.Stderr.(*os.File); ok {
			f.Sync()
		}
	}
}
