package report

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxb1906/cadastre/internal/analysis"
	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/ntfs/ntfstest"
	"github.com/maxb1906/cadastre/internal/rules"
)

func fixtureAnalysis(t *testing.T) *analysis.Analysis {
	img := filepath.Join(t.TempDir(), "v.img")
	if err := os.WriteFile(img, ntfstest.Fixture(t), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := engine.ScanImage(context.Background(), img, engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return analysis.Run(res, rules.MustBuiltin(), analysis.Options{NoSystem: true})
}

func TestHTMLReport(t *testing.T) {
	js, err := os.ReadFile("../../frontend/src/shared/treemap.js")
	if err != nil {
		t.Fatal(err)
	}
	TreemapJS = string(js)
	a := fixtureAnalysis(t)
	for _, lang := range []string{"fr", "en"} {
		var buf bytes.Buffer
		if err := WriteHTML(&buf, Build(a, Options{Lang: lang})); err != nil {
			t.Fatal(err)
		}
		h := buf.String()
		for _, want := range []string{"CadastreTreemap", "notes.txt", "cad-treemap", "</html>"} {
			if !strings.Contains(h, want) {
				t.Errorf("%s: missing %q", lang, want)
			}
		}
		if strings.Contains(h, "%!") || strings.Contains(h, "rep.") && strings.Contains(h, ">rep.") {
			t.Errorf("%s: formatting or missing translation in report", lang)
		}
		// The embedded treemap JSON must be valid.
		i := strings.Index(h, `id="cad-treemap">`) + len(`id="cad-treemap">`)
		j := strings.Index(h[i:], "</script>")
		var node map[string]any
		if err := json.Unmarshal([]byte(h[i:i+j]), &node); err != nil {
			t.Errorf("%s: treemap JSON: %v", lang, err)
		}
	}
}

func TestJSONCSVMarkdown(t *testing.T) {
	a := fixtureAnalysis(t)
	d := Build(a, Options{Lang: "en"})
	var buf bytes.Buffer
	if err := WriteJSON(&buf, d); err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if back["engine"] != "mft" || back["reconciliation"] == nil {
		t.Errorf("json: %v", back["engine"])
	}
	buf.Reset()
	if err := WriteCSV(&buf, a.Result.Index, CSVOptions{Lang: "fr"}); err != nil {
		t.Fatal(err)
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(buf.Bytes(), []byte{0xEF, 0xBB, 0xBF})))
	r.Comma = ';'
	recs, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) < 6000 || recs[0][0] != "chemin" {
		t.Errorf("csv rows %d header %v", len(recs), recs[0])
	}
	found := false
	for _, rec := range recs {
		if strings.HasSuffix(rec[0], `\notes.txt`) && rec[2] == "210030" && strings.Contains(rec[8], "ads") {
			found = true
		}
	}
	if !found {
		t.Error("notes.txt row not found in CSV")
	}
	buf.Reset()
	if err := WriteMarkdown(&buf, d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "## Used space: reconciliation") {
		t.Errorf("markdown:\n%s", buf.String())
	}
}
