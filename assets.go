package main

import (
	_ "embed"

	"github.com/maxb1906/cadastre/internal/cli"
	"github.com/maxb1906/cadastre/internal/report"
)

// treemapJS is the treemap component shared by the GUI and the HTML report.
//
//go:embed frontend/src/shared/treemap.js
var treemapJS string

func init() {
	report.TreemapJS = treemapJS
	report.Version = cli.Version
}
