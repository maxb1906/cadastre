package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/report"
	"github.com/maxb1906/cadastre/internal/rules"
	"github.com/maxb1906/cadastre/internal/snapshot"
	"github.com/maxb1906/cadastre/internal/units"
)

// outputs are the export options shared by scan and open.
type outputs struct {
	report, json, csv, md, snapshot string
	csvMin                          string
	top                             int
	open                            bool
}

func (o *outputs) register(fs interface {
	StringVar(*string, string, string, string)
	IntVar(*int, string, int, string)
	BoolVar(*bool, string, bool, string)
}, lang string, withSnapshot bool) {
	fs.StringVar(&o.report, "report", "", i18n.T(lang, "cli.flag.report"))
	fs.StringVar(&o.json, "json", "", i18n.T(lang, "cli.flag.json"))
	fs.StringVar(&o.csv, "csv", "", i18n.T(lang, "cli.flag.csv"))
	fs.StringVar(&o.csvMin, "csv-min-size", "0", i18n.T(lang, "cli.flag.csvmin"))
	fs.StringVar(&o.md, "md", "", i18n.T(lang, "cli.flag.md"))
	fs.IntVar(&o.top, "top", 20, i18n.T(lang, "cli.flag.top"))
	fs.BoolVar(&o.open, "open", false, i18n.T(lang, "cli.flag.open"))
	if withSnapshot {
		fs.StringVar(&o.snapshot, "snapshot", "", i18n.T(lang, "cli.flag.snapshot"))
	}
}

// suffixed adds "-C" before the extension when several volumes are scanned.
func suffixed(path, tag string, many bool) string {
	if path == "" || !many {
		return path
	}
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + "-" + tag + ext
}

func volumeTag(root string) string {
	t := strings.Trim(root, `\/:`)
	if t == "" {
		return "root"
	}
	return strings.NewReplacer(`\`, "_", "/", "_", ":", "").Replace(t)
}

// emit prints the console summary and writes the requested files.
func emit(env *Env, a *analysis.Analysis, o outputs, many bool) error {
	tag := volumeTag(a.Result.Volume.Root)
	if err := report.WriteText(env.Stdout, a, o.top, env.Lang); err != nil {
		return err
	}
	data := report.Build(a, report.Options{Lang: env.Lang})
	write := func(path string, fn func(f *os.File) error) error {
		if path == "" {
			return nil
		}
		path = suffixed(path, tag, many)
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		if err := fn(f); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		fmt.Fprintln(env.Stderr, i18n.T(env.Lang, "cli.written", path))
		return nil
	}
	min, err := units.ParseSize(o.csvMin)
	if err != nil {
		return err
	}
	errs := []error{
		write(o.report, func(f *os.File) error { return report.WriteHTML(f, data) }),
		write(o.json, func(f *os.File) error { return report.WriteJSON(f, data) }),
		write(o.md, func(f *os.File) error { return report.WriteMarkdown(f, data) }),
		write(o.csv, func(f *os.File) error {
			return report.WriteCSV(f, a.Result.Index, report.CSVOptions{Lang: env.Lang, MinSize: min})
		}),
	}
	if o.snapshot != "" {
		p := suffixed(o.snapshot, tag, many)
		if err := snapshot.Save(p, a.Result); err != nil {
			errs = append(errs, err)
		} else {
			fmt.Fprintln(env.Stderr, i18n.T(env.Lang, "cli.written", p))
		}
	}
	if o.open && o.report != "" {
		openInBrowser(suffixed(o.report, tag, many))
	}
	return errors.Join(errs...)
}

func runScan(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "scan", "cli.scan.usage")
	var o outputs
	o.register(fs, env.Lang, true)
	image := fs.String("image", "", i18n.T(env.Lang, "cli.flag.image"))
	minSize := fs.String("min-size", "0", i18n.T(env.Lang, "cli.flag.minsize"))
	quiet := fs.Bool("quiet", false, i18n.T(env.Lang, "cli.flag.quiet"))
	eng := fs.String("engine", "auto", i18n.T(env.Lang, "cli.flag.engine"))
	var ruleFiles multiFlag
	fs.Var(&ruleFiles, "rules", i18n.T(env.Lang, "cli.flag.rules"))
	pos, err := parseFlags(fs, args)
	if err != nil {
		return errUsage
	}
	if len(pos) == 0 && *image == "" {
		fs.Usage()
		return errUsage
	}
	ms, err := units.ParseSize(*minSize)
	if err != nil {
		return err
	}
	cat, err := rules.Load(ruleFiles...)
	if err != nil {
		return err
	}
	targets := pos
	if *image != "" {
		targets = []string{*image}
	}
	many := len(targets) > 1
	// One pipeline per volume, in parallel; outputs are printed in order.
	results := make([]*analysis.Analysis, len(targets))
	errs := make([]error, len(targets))
	var wg sync.WaitGroup
	for k, t := range targets {
		wg.Add(1)
		go func(k int, t string) {
			defer wg.Done()
			opt := engine.Options{MinSize: ms, Engine: *eng}
			if !*quiet && !many {
				opt.Progress = progressPrinter(env)
			}
			var res *engine.Result
			if *image != "" {
				res, errs[k] = engine.ScanImage(ctx, t, opt)
			} else {
				res, errs[k] = engine.Scan(ctx, t, opt)
			}
			if errs[k] == nil {
				results[k] = analysis.Run(res, cat, analysis.Options{})
			}
		}(k, t)
	}
	wg.Wait()
	if !*quiet && !many {
		fmt.Fprint(env.Stderr, "\r\033[K")
	}
	var all []error
	for k := range targets {
		if errs[k] != nil {
			all = append(all, fmt.Errorf("%s: %w", targets[k], errs[k]))
			continue
		}
		if k > 0 {
			fmt.Fprintln(env.Stdout)
		}
		if err := emit(env, results[k], o, many); err != nil {
			all = append(all, err)
		}
	}
	return errors.Join(all...)
}

func runOpen(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "open", "cli.open.usage")
	var o outputs
	o.register(fs, env.Lang, false)
	var ruleFiles multiFlag
	fs.Var(&ruleFiles, "rules", i18n.T(env.Lang, "cli.flag.rules"))
	pos, err := parseFlags(fs, args)
	if err != nil || len(pos) != 1 {
		fs.Usage()
		return errUsage
	}
	res, err := snapshot.Load(pos[0])
	if err != nil {
		return err
	}
	cat, err := rules.Load(ruleFiles...)
	if err != nil {
		return err
	}
	return emit(env, analysis.Run(res, cat, analysis.Options{}), o, false)
}

func progressPrinter(env *Env) func(engine.Progress) {
	last := -1
	var lastFiles uint64
	return func(p engine.Progress) {
		if p.Total == 0 {
			if p.Done-lastFiles < 5000 {
				return
			}
			lastFiles = p.Done
			fmt.Fprintf(env.Stderr, "\r%s %s", i18n.T(env.Lang, "progress."+p.Phase), units.Count(p.Done, env.Lang))
			return
		}
		pct := int(100 * p.Done / p.Total)
		if pct == last {
			return
		}
		last = pct
		fmt.Fprintf(env.Stderr, "\r%s %3d %%", i18n.T(env.Lang, "progress."+p.Phase), pct)
	}
}

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
