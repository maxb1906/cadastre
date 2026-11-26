package cli

import (
	"context"
	"fmt"

	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/units"
	"github.com/maxb1906/cadastre/internal/walker"
)

// runCheck is the cross-engine oracle: it scans the same volume with engine
// A and engine C and compares the results file by file.
func runCheck(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "check", "cli.check.usage")
	image := fs.String("image", "", i18n.T(env.Lang, "cli.flag.image"))
	against := fs.String("against", "", i18n.T(env.Lang, "cli.flag.against"))
	examples := fs.Int("examples", 15, i18n.T(env.Lang, "cli.flag.examples"))
	pos, err := parseFlags(fs, args)
	if err != nil {
		return errUsage
	}
	p := i18n.Printer{Lang: env.Lang}
	var a, c *engine.Result
	switch {
	case *image != "" && *against != "":
		if a, err = engine.ScanImage(ctx, *image, engine.Options{}); err != nil {
			return err
		}
		ix, _, err := walker.Walk(ctx, *against, walker.Options{})
		if err != nil {
			return err
		}
		c = &engine.Result{Index: ix, Engine: engine.EngineWalk}
	case len(pos) == 1:
		if a, err = engine.Scan(ctx, pos[0], engine.Options{Engine: engine.EngineMFT}); err != nil {
			return err
		}
		if c, err = engine.Scan(ctx, pos[0], engine.Options{Engine: engine.EngineWalk}); err != nil {
			return err
		}
	default:
		fs.Usage()
		return errUsage
	}
	r := engine.Check(a.Index, c.Index, *examples)
	b := func(v uint64) string { return units.Bytes(v, env.Lang) }
	n := func(v uint64) string { return units.Count(v, env.Lang) }
	fmt.Fprintln(env.Stdout, p.T("check.title"))
	fmt.Fprintln(env.Stdout, p.T("check.files", n(r.FilesA), b(r.SizeA), n(r.FilesB), b(r.SizeB)))
	fmt.Fprintln(env.Stdout, p.T("check.skipped", n(r.SkippedMetafiles), n(r.SkippedProtected)))
	fmt.Fprintln(env.Stdout, p.T("check.common", n(r.Common)))
	fmt.Fprintln(env.Stdout, p.T("check.only_a", n(r.CountOnlyA), b(r.SizeOnlyA)))
	for _, d := range r.OnlyA {
		fmt.Fprintf(env.Stdout, "    %10s  %s\n", b(d.SizeA), d.Path)
	}
	fmt.Fprintln(env.Stdout, p.T("check.only_c", n(r.CountOnlyB), b(r.SizeOnlyB)))
	for _, d := range r.OnlyB {
		fmt.Fprintf(env.Stdout, "    %10s  %s\n", b(d.SizeB), d.Path)
	}
	fmt.Fprintln(env.Stdout, p.T("check.mismatch", n(r.CountMismatch)))
	for _, d := range r.Mismatch {
		fmt.Fprintf(env.Stdout, "    A %10s  C %10s  %s\n", b(d.SizeA), b(d.SizeB), d.Path)
	}
	if r.OK() {
		fmt.Fprintln(env.Stdout, p.T("check.ok"))
	} else {
		fmt.Fprintln(env.Stdout, p.T("check.notes"))
	}
	return nil
}
