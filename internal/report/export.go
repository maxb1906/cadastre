package report

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/units"
)

// WriteJSON writes the report data as indented JSON.
func WriteJSON(w io.Writer, d *Data) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

// CSVOptions tunes the CSV export.
type CSVOptions struct {
	Sep     rune   // default ';' for French, ',' otherwise (Excel conventions)
	MinSize uint64 // skip entries smaller than this (allocated)
	Lang    string
}

// attrLetters renders the Windows attributes like the dir /a command.
func attrLetters(a uint32) string {
	var b strings.Builder
	for _, x := range []struct {
		bit uint32
		c   byte
	}{{index.AttrReadOnly, 'R'}, {index.AttrHidden, 'H'}, {index.AttrSystem, 'S'}, {index.AttrArchive, 'A'},
		{index.AttrCompressed, 'C'}, {index.AttrEncrypted, 'E'}, {index.AttrSparse, 'P'}, {index.AttrReparsePoint, 'L'},
		{index.AttrOffline, 'O'}, {index.AttrTemporary, 'T'}} {
		if a&x.bit != 0 {
			b.WriteByte(x.c)
		}
	}
	return b.String()
}

// WriteCSV writes every entry of the index (files and folders), streaming.
// A UTF-8 BOM makes Excel read accents correctly.
func WriteCSV(w io.Writer, ix *index.Index, opt CSVOptions) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	bw.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(bw)
	if opt.Sep == 0 {
		opt.Sep = ','
		if opt.Lang == "fr" {
			opt.Sep = ';'
		}
	}
	cw.Comma = opt.Sep
	p := i18n.Printer{Lang: opt.Lang}
	cw.Write([]string{p.T("csv.path"), p.T("csv.type"), p.T("csv.size"), p.T("csv.alloc"), p.T("csv.files"), p.T("csv.modified"), p.T("csv.ext"), p.T("csv.attrs"), p.T("csv.flags")})
	var path []byte
	var err error
	ix.Walk(ix.Root, func(i uint32) bool {
		if err != nil || !ix.Valid(i) {
			return false
		}
		alloc := ix.TotalAlloc(i)
		if ix.Has(i, index.FlagFolded) {
			return true
		}
		if opt.MinSize > 0 && alloc < opt.MinSize {
			return false // below the threshold: skip the whole subtree
		}
		path = ix.AppendPath(path[:0], i)
		typ, files, ext := "file", "", ix.ExtName(i)
		if ix.IsDir(i) {
			typ = "dir"
			f, _ := ix.SubtreeCounts(i)
			files = strconv.FormatUint(f, 10)
		}
		err = cw.Write([]string{string(path), typ, strconv.FormatUint(ix.TotalSize(i), 10), strconv.FormatUint(alloc, 10),
			files, fmtTime(ix.MTime[i]), ext, attrLetters(ix.Attr[i]), strings.Join(flagsOf(ix, i), "|")})
		return true
	})
	if err != nil {
		return err
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	return bw.Flush()
}

func flagsOf(ix *index.Index, i uint32) []string {
	var out []string
	a := ix.Attr[i]
	for _, x := range []struct {
		bit  uint32
		name string
	}{{index.FlagHardlink, "hardlink"}, {index.FlagADS, "ads"}, {index.FlagMeta, "meta"}, {index.FlagCloud, "cloud"},
		{index.FlagWOF, "wof"}, {index.FlagDenied, "denied"}, {index.FlagOrphan, "orphan"}} {
		if a&x.bit != 0 {
			out = append(out, x.name)
		}
	}
	return out
}

// WriteMarkdown writes a Markdown summary (tickets, wikis, chat).
func WriteMarkdown(w io.Writer, d *Data) error {
	p := i18n.Printer{Lang: d.Lang}
	b := func(v uint64) string { return units.Bytes(v, d.Lang) }
	n := func(v uint64) string { return units.Count(v, d.Lang) }
	esc := func(s string) string { return strings.NewReplacer("|", `\|`, "\n", " ", "`", "'").Replace(s) }
	fmt.Fprintf(w, "# %s\n\n", p.T("rep.title", d.Volume.Root))
	fmt.Fprintf(w, "_%s · %s · %s · Cadastre %s_\n\n", p.T("rep.generated", d.Generated.Local().Format("2006-01-02 15:04")),
		p.T("engine."+d.Engine), p.T("rep.duration", units.Duration(secs(d.DurationSec), d.Lang)), d.Version)
	if rc := d.Recon; rc != nil && rc.Used > 0 {
		fmt.Fprintf(w, "%s\n\n", p.T("recon.summary", b(rc.Used), b(rc.Total), b(rc.Get("files").Bytes)))
	}
	fmt.Fprintf(w, "%s\n\n", p.T("summary.files", n(d.Totals.Files), n(d.Totals.Dirs)))
	for _, warn := range d.Warnings {
		fmt.Fprintf(w, "> ⚠ %s\n", esc(warn))
	}
	if len(d.Warnings) > 0 {
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "## %s\n\n", p.T("ghosts.title"))
	if len(d.Findings) == 0 {
		fmt.Fprintf(w, "%s\n\n", p.T("ghosts.none"))
	} else {
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n|---|---|--:|--:|---|\n", p.T("rep.risk"), p.T("col.rule"), p.T("col.alloc"), p.T("col.files"), p.T("rep.advice"))
		for _, f := range d.Findings {
			fmt.Fprintf(w, "| %s %s | %s (`%s`) | %s | %s | %s |\n", RiskMark(f.Risk), p.T("risk."+f.Risk), esc(f.Title), f.ID, b(f.Alloc), n(f.Files), esc(f.Advice))
		}
		fmt.Fprintln(w)
	}
	if rc := d.Recon; rc != nil && rc.Used > 0 {
		fmt.Fprintf(w, "## %s\n\n| %s | %s | %% |\n|---|--:|--:|\n", p.T("recon.title"), p.T("rep.component"), p.T("col.alloc"))
		for _, c := range rc.Components {
			if c.Bytes > 0 {
				fmt.Fprintf(w, "| %s | %s | %s |\n", p.T("recon."+c.Key), b(c.Bytes), units.Percent(c.Bytes, rc.Used, d.Lang))
			}
		}
		fmt.Fprintln(w)
		for _, note := range rc.Notes {
			fmt.Fprintf(w, "> %s\n", p.T(note))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "## %s\n\n| %s | %s |\n|--:|---|\n", p.T("top.files"), p.T("col.alloc"), p.T("col.path"))
	for k, f := range d.TopFiles {
		if k >= 20 {
			break
		}
		fmt.Fprintf(w, "| %s | `%s` |\n", b(f.Alloc), esc(f.Path))
	}
	fmt.Fprintf(w, "\n## %s\n\n| %s | %s | %s |\n|--:|--:|---|\n", p.T("top.dirs"), p.T("col.alloc"), p.T("col.files"), p.T("col.path"))
	for k, f := range d.TopDirs {
		if k >= 20 {
			break
		}
		fmt.Fprintf(w, "| %s | %s | `%s` |\n", b(f.Alloc), n(f.Files), esc(f.Path))
	}
	if c := d.Cleanup; c != nil {
		fmt.Fprintf(w, "\n## %s\n\n%s\n", p.T("rep.cleanup"), p.T("rep.cleanup.summary", c.Done, c.Failed, c.Skipped, b(c.Freed)))
	}
	return nil
}
