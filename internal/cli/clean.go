package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/clean"
	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/report"
	"github.com/maxb1906/cadastre/internal/rules"
	"github.com/maxb1906/cadastre/internal/sysinfo"
	"github.com/maxb1906/cadastre/internal/units"
	"github.com/maxb1906/cadastre/internal/volume"
)

// runClean: simulation by default; --apply is required to act.
func runClean(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "clean", "cli.clean.usage")
	var ruleIDs, paths, ruleFiles multiFlag
	fs.Var(&ruleIDs, "rule", i18n.T(env.Lang, "cli.flag.rule"))
	fs.Var(&paths, "path", i18n.T(env.Lang, "cli.flag.path"))
	fs.Var(&ruleFiles, "rules", i18n.T(env.Lang, "cli.flag.rules"))
	olderThan := fs.String("older-than", "", i18n.T(env.Lang, "cli.flag.olderthan"))
	apply := fs.Bool("apply", false, i18n.T(env.Lang, "cli.flag.apply"))
	permanent := fs.Bool("permanent", false, i18n.T(env.Lang, "cli.flag.permanent"))
	yesOrange := fs.Bool("yes-orange", false, i18n.T(env.Lang, "cli.flag.yesorange"))
	official := fs.Bool("official", false, i18n.T(env.Lang, "cli.flag.official"))
	restore := fs.Bool("restore-point", false, i18n.T(env.Lang, "cli.flag.restore"))
	script := fs.String("script", "", i18n.T(env.Lang, "cli.flag.script"))
	yes := fs.Bool("yes", false, i18n.T(env.Lang, "cli.flag.yes"))
	journalDir := fs.String("journal", "", i18n.T(env.Lang, "cli.flag.journal"))
	eng := fs.String("engine", "auto", i18n.T(env.Lang, "cli.flag.engine"))
	listRules := fs.Bool("list-rules", false, i18n.T(env.Lang, "cli.flag.listrules"))
	reportPath := fs.String("report", "", i18n.T(env.Lang, "cli.flag.report"))
	pos, err := parseFlags(fs, args)
	if err != nil {
		return errUsage
	}
	cat, err := rules.Load(ruleFiles...)
	if err != nil {
		return err
	}
	if *listRules {
		return printRules(env, cat)
	}
	if len(pos) != 1 || len(ruleIDs) == 0 && len(paths) == 0 {
		fs.Usage()
		return errUsage
	}
	sel := clean.Selection{Rules: ruleIDs, Official: *official, Mode: clean.ModeRecycle}
	if *permanent {
		sel.Mode = clean.ModePermanent
	}
	for _, id := range ruleIDs {
		if _, ok := cat.Get(id); !ok {
			return fmt.Errorf("%s: %s", i18n.T(env.Lang, "clean.reason.unknown_rule"), id)
		}
	}
	if *olderThan != "" {
		age, err := units.ParseAge(*olderThan)
		if err != nil {
			return err
		}
		sel.OlderThan = map[string]time.Duration{}
		for _, id := range ruleIDs {
			sel.OlderThan[id] = age
		}
	}

	// A fresh scan: a cleanup never relies on an old view of the disk.
	res, err := engine.Scan(ctx, pos[0], engine.Options{Engine: *eng})
	if err != nil {
		return err
	}
	a := analysis.Run(res, cat, analysis.Options{OlderThan: sel.OlderThan})
	for _, p := range paths {
		i, ok := res.Index.Find(p)
		if !ok {
			return fmt.Errorf("%s: %s", i18n.T(env.Lang, "clean.reason.gone"), p)
		}
		sel.Entries = append(sel.Entries, i)
	}
	running, _ := sysinfo.RunningProcesses()
	plan := clean.Build(a, sel, clean.Env{Admin: volume.IsAdmin(), Running: running})
	clean.Prepare(plan, clean.OSFS{})
	printPlan(env, plan)

	if *script != "" {
		f, err := os.Create(*script)
		if err != nil {
			return err
		}
		if err := clean.WriteScript(f, plan, env.Lang); err != nil {
			f.Close()
			return err
		}
		f.Close()
		fmt.Fprintln(env.Stderr, i18n.T(env.Lang, "cli.written", *script))
	}
	if len(plan.Items) == 0 && len(plan.Official) == 0 {
		fmt.Fprintln(env.Stdout, i18n.T(env.Lang, "clean.no_items"))
		return nil
	}
	if !*apply {
		fmt.Fprintln(env.Stdout, "\n"+i18n.T(env.Lang, "clean.need_apply"))
		if plan.Orange && !*yesOrange {
			fmt.Fprintln(env.Stdout, i18n.T(env.Lang, "clean.need_orange"))
		}
		return nil
	}
	if plan.Orange && !*yesOrange {
		fmt.Fprintln(env.Stdout, i18n.T(env.Lang, "clean.need_orange"))
	}
	if !*yes {
		fmt.Fprint(env.Stdout, i18n.T(env.Lang, "clean.confirm", units.Count(uint64(len(plan.Items)), env.Lang),
			units.Bytes(plan.Bytes, env.Lang), i18n.T(env.Lang, "clean.mode."+plan.Mode)))
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "oui" && ans != "yes" && ans != "o" && ans != "y" {
			fmt.Fprintln(env.Stdout, i18n.T(env.Lang, "clean.aborted"))
			return nil
		}
	}
	j, err := clean.OpenJournal(*journalDir)
	if err != nil {
		return err
	}
	defer j.Close()
	freeBefore := res.Volume.Free
	last := -1
	r, err := clean.Execute(ctx, plan, clean.OSFS{}, j, clean.Options{
		ConfirmOrange: *yesOrange,
		RestorePoint:  *restore,
		Running: func() map[string]bool {
			m, _ := sysinfo.RunningProcesses()
			return m
		},
		Progress: func(p clean.Progress) {
			if pct := 100 * p.Done / max(p.Total, 1); pct != last {
				last = pct
				fmt.Fprintf(env.Stderr, "\r%3d %% %s", pct, units.Bytes(p.Freed, env.Lang))
			}
		},
	})
	fmt.Fprint(env.Stderr, "\r\033[K")
	if err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, i18n.T(env.Lang, "clean.result", r.Done, r.Failed, r.Skipped, units.Bytes(r.Freed, env.Lang)))
	if v, err := volume.Stat(res.Volume.Root); err == nil && freeBefore > 0 {
		fmt.Fprintln(env.Stdout, i18n.T(env.Lang, "clean.freed_real", units.Bytes(freeBefore, env.Lang), units.Bytes(v.Free, env.Lang)))
	}
	fmt.Fprintln(env.Stdout, i18n.T(env.Lang, "clean.journal", r.Journal))
	if *reportPath != "" {
		a = clean.Refresh(ctx, a, r, analysis.Options{})
		f, err := os.Create(*reportPath)
		if err != nil {
			return err
		}
		defer f.Close()
		return report.WriteHTML(f, report.Build(a, report.Options{Lang: env.Lang, Cleanup: Summary(r)}))
	}
	return nil
}

// Summary converts a cleanup result for the reports.
func Summary(r *clean.Result) *report.CleanupSummary {
	if r == nil {
		return nil
	}
	s := &report.CleanupSummary{Journal: r.Journal, Done: r.Done, Failed: r.Failed, Skipped: r.Skipped, Freed: r.Freed}
	for k, e := range r.Entries {
		if k >= 1000 {
			break
		}
		if e.Action == "plan" || e.Action == "end" {
			continue
		}
		res := e.Result
		if e.Reason != "" {
			res += " (" + e.Reason + ")"
		}
		s.Entries = append(s.Entries, report.CleanupEntry{Time: e.Time.Format("15:04:05"), Action: e.Action, Path: e.Path, Size: e.Size, Mode: e.Mode, Result: res, Detail: firstLine(e.Detail)})
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func printPlan(env *Env, p *clean.Plan) {
	l := env.Lang
	tr := i18n.Printer{Lang: l}
	fmt.Fprintln(env.Stdout, tr.T("clean.dryrun", units.Count(uint64(p.Files), l), units.Bytes(p.Bytes, l))+" · "+tr.T("clean.mode."+p.Mode))
	tw := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	shown := 0
	for _, it := range p.Items {
		if it.Dir {
			continue
		}
		if shown++; shown > 40 {
			fmt.Fprintf(tw, "  %s\n", tr.T("clean.more", p.Files-40))
			break
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", report.RiskMark(it.Risk), units.Bytes(it.Alloc, l), it.Path)
	}
	tw.Flush()
	for _, o := range p.Official {
		fmt.Fprintf(env.Stdout, "  ⚙ %s\n", tr.T("clean.official_line", o.Rule, o.Title.In(l)))
		for _, c := range o.Commands {
			if c == nil {
				fmt.Fprintf(env.Stdout, "      → %d %s\n", len(o.Items), strings.ToLower(tr.T("clean.files_tab")))
			} else {
				fmt.Fprintf(env.Stdout, "      > %s\n", strings.Join(c, " "))
			}
		}
	}
	if len(p.Skipped) > 0 {
		fmt.Fprintln(env.Stdout, tr.T("clean.skipped_title", len(p.Skipped)))
		by := map[string]int{}
		for _, s := range p.Skipped {
			by[s.Reason]++
		}
		keys := make([]string, 0, len(by))
		for k := range by {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(a, b int) bool { return by[keys[a]] > by[keys[b]] })
		for _, k := range keys {
			fmt.Fprintf(env.Stdout, "  %5d  %s\n", by[k], tr.T("clean.reason."+k))
		}
	}
}

func printRules(env *Env, cat *rules.Catalog) error {
	tw := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	for _, r := range cat.Rules {
		action := i18n.T(env.Lang, "rep.clean.none")
		switch {
		case r.Deletable():
			action = i18n.T(env.Lang, "rep.clean.delete")
		case r.Clean == rules.CleanOfficial:
			action = i18n.T(env.Lang, "rep.clean.official", r.Official.Title.In(env.Lang))
		}
		fmt.Fprintf(tw, "%s %s\t%s\t%s\n", report.RiskMark(r.Risk), r.ID, r.Title.In(env.Lang), action)
	}
	return tw.Flush()
}
