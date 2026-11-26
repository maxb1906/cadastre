package report

import (
	_ "embed"
	"encoding/json"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/reclaim"
	"github.com/maxb1906/cadastre/internal/units"
)

//go:embed report.html.tmpl
var htmlTemplate string

type htmlPage struct {
	D           *Data
	TreemapJSON template.JS
	I18NJSON    template.JS
	TreemapJS   template.JS
	Reclaimable uint64
}

// Comp returns a reconciliation component.
func (h *htmlPage) Comp(key string) reclaim.Component {
	if h.D.Recon == nil {
		return reclaim.Component{}
	}
	return h.D.Recon.Get(key)
}

// Funcs returns the template helpers for lang.
func Funcs(lang string) template.FuncMap {
	return template.FuncMap{
		"t": func(key string, args ...any) string { return i18n.T(lang, key, args...) },
		"b": func(v uint64) string { return units.Bytes(v, lang) },
		"n": func(v uint64) string { return units.Count(v, lang) },
		"u": func(v int64) uint64 {
			if v < 0 {
				return 0
			}
			return uint64(v)
		},
		"pct": func(part, total uint64) string { return units.Percent(part, total, lang) },
		"share": func(part, total uint64) string {
			if total == 0 {
				return "0"
			}
			v := 100 * float64(part) / float64(total)
			if v > 100 {
				v = 100
			}
			return strings.TrimRight(strings.TrimRight(template.HTMLEscapeString(ftoa(v)), "0"), ".")
		},
		"date": func(t time.Time) string {
			if lang == "fr" {
				return t.Local().Format("02/01/2006 15:04")
			}
			return t.Local().Format("2006-01-02 15:04")
		},
		"shortdate": func(rfc string) string {
			t, err := time.Parse(time.RFC3339, rfc)
			if err != nil {
				return ""
			}
			if lang == "fr" {
				return t.Local().Format("02/01/2006")
			}
			return t.Local().Format("2006-01-02")
		},
		"dur": func(s float64) string { return units.Duration(time.Duration(s*float64(time.Second)), lang) },
	}
}

func ftoa(v float64) string {
	b, _ := json.Marshal(float64(int(v*100)) / 100)
	return string(b)
}

// reclaimable sums the space of deletable findings or those with an
// official action.
func reclaimable(d *Data) uint64 {
	var total uint64
	for _, f := range d.Findings {
		if f.Deletable || f.Clean == "official" && f.Risk != "red" {
			total += f.Alloc
		}
	}
	return total
}

// WriteHTML writes a standalone HTML report (one file, treemap included).
func WriteHTML(w io.Writer, d *Data) error {
	tm, err := json.Marshal(d.Treemap) // json escapes <, > and & as \u00XX
	if err != nil {
		return err
	}
	tr := map[string]string{"others": i18n.T(d.Lang, "tm.others"), "hint": i18n.T(d.Lang, "tm.hint")}
	for _, f := range []string{"video", "image", "audio", "archive", "disk", "binary", "code", "logs", "data", "docs", "other"} {
		tr["fam."+f] = i18n.T(d.Lang, "fam."+f)
	}
	trj, _ := json.Marshal(tr)
	js := strings.ReplaceAll(TreemapJS, "</script", `<\/script`)
	page := &htmlPage{D: d, TreemapJSON: template.JS(tm), I18NJSON: template.JS(trj), TreemapJS: template.JS(js)}
	page.Reclaimable = reclaimable(d)
	t, err := template.New("report").Funcs(Funcs(d.Lang)).Parse(htmlTemplate)
	if err != nil {
		return err
	}
	return t.Execute(w, page)
}
