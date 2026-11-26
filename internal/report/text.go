// Package report renders scan results: console text, HTML, CSV, JSON and
// Markdown.
package report

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/units"
)

// WriteText prints a console summary with the top n files, folders and
// extensions.
func WriteText(w io.Writer, r *engine.Result, n int, lang string) error {
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
