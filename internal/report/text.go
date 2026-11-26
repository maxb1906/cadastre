// Package report renders scan results: console text, HTML, CSV, JSON and
// Markdown.
package report

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/reclaim"
	"github.com/maxb1906/cadastre/internal/rules"
	"github.com/maxb1906/cadastre/internal/units"
)

// RiskMark returns the symbol of a risk level.
func RiskMark(risk string) string {
	switch risk {
	case rules.RiskGreen:
		return "🟢"
	case rules.RiskOrange:
		return "🟠"
	default:
		return "🔴"
	}
}

// WriteText prints a console summary: ghost findings, space
// reconciliation, then the top n files, folders and extensions.
func WriteText(w io.Writer, a *analysis.Analysis, n int, lang string) error {
	r := a.Result
	ix := r.Index
	p := i18n.Printer{Lang: lang}
	t := ix.Totals()
	b := func(v uint64) string { return units.Bytes(v, lang) }
	fmt.Fprintf(w, "%s\n", p.T("summary.title", r.Volume.Root))
	fmt.Fprintf(w, "  %s\n", p.T("summary.engine_line", p.T("engine."+r.Engine), units.Duration(r.Duration, lang)))
	fmt.Fprintf(w, "  %s\n", p.T("summary.files", units.Count(t.Files, lang), units.Count(t.Dirs, lang)))
	fmt.Fprintf(w, "  %s\n\n", p.T("summary.total_line", b(t.Size), b(t.Alloc)))
	for _, wr := range r.Warnings {
		fmt.Fprintf(w, "  ! %s\n", p.T(wr.Key, wr.Args...))
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintln(w)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	section := func(title string) {
		fmt.Fprintf(w, "%s\n%s\n", title, strings.Repeat("─", len([]rune(title))))
	}

	section(p.T("ghosts.title"))
	if len(a.Findings) == 0 {
		fmt.Fprintln(w, "  "+p.T("ghosts.none"))
	} else {
		fmt.Fprintf(tw, "\t%s\t%s\t %s\n", p.T("col.alloc"), p.T("col.files"), p.T("col.rule"))
	}
	for _, f := range a.Findings {
		fmt.Fprintf(tw, "%s\t%s\t%s\t %s (%s)\n", RiskMark(f.Rule.Risk), b(f.Alloc), units.Count(f.Files, lang), f.Rule.Title.In(lang), f.Rule.ID)
	}
	tw.Flush()
	for _, f := range a.Findings {
		for _, g := range f.Groups {
			fmt.Fprintf(w, "    %s · %s : %s\n", f.Rule.Title.In(lang), g.Label, b(g.Alloc))
		}
	}
	fmt.Fprintln(w)

	if rc := a.Recon; rc != nil && rc.Used > 0 {
		section(p.T("recon.title"))
		fmt.Fprintf(w, "  %s\n", p.T("recon.summary", b(rc.Used), b(rc.Total), b(rc.Get(reclaim.Files).Bytes)))
		if rc.Missing > 0 {
			fmt.Fprintf(w, "  %s\n", p.T("recon.missing", b(uint64(rc.Missing))))
		}
		for _, c := range rc.Components {
			if c.Key == reclaim.Files || c.Bytes == 0 {
				continue
			}
			fmt.Fprintf(tw, "%s\t %s\n", b(c.Bytes), p.T("recon."+c.Key))
		}
		tw.Flush()
		for _, note := range rc.Notes {
			fmt.Fprintf(w, "  ! %s\n", p.T(note))
		}
		fmt.Fprintln(w)
	}
	section(p.T("top.files"))
	fmt.Fprintf(tw, "%s\t%s\t %s\n", p.T("col.alloc"), p.T("col.size"), p.T("col.path"))
	for _, i := range ix.TopFiles(n, index.ByAlloc, false) {
		fmt.Fprintf(tw, "%s\t%s\t %s\n", b(ix.TotalAlloc(i)), b(ix.TotalSize(i)), ix.Path(i))
	}
	tw.Flush()
	fmt.Fprintln(w)

	section(p.T("top.dirs"))
	fmt.Fprintf(tw, "%s\t%s\t %s\n", p.T("col.alloc"), p.T("col.files"), p.T("col.path"))
	for _, d := range ix.TopDirs(n, index.ByAlloc) {
		fmt.Fprintf(tw, "%s\t%s\t %s\n", b(d.Alloc), units.Count(d.Files, lang), ix.Path(d.Dir))
	}
	tw.Flush()
	fmt.Fprintln(w)

	section(p.T("top.exts"))
	fmt.Fprintf(tw, "%s\t%s\t %s\n", p.T("col.alloc"), p.T("col.files"), p.T("col.ext"))
	for k, e := range ix.Extensions() {
		if k >= n {
			break
		}
		name := "." + e.Ext
		if e.Ext == "" {
			name = p.T("ext.none")
		}
		fmt.Fprintf(tw, "%s\t%s\t %s\n", b(e.Alloc), units.Count(e.Files, lang), name)
	}
	return tw.Flush()
}
