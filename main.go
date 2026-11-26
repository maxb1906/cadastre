package main

import (
	"embed"
	"os"

	"github.com/maxb1906/cadastre/internal/cli"
	"github.com/maxb1906/cadastre/internal/console"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] != "gui" {
		// CLI mode: the exe is built for the GUI subsystem, so reattach to
		// the parent console (or keep redirected handles) before printing.
		console.Attach()
		os.Exit(cli.Run(args, os.Stdout, os.Stderr))
	}
	runGUI(assets)
}
