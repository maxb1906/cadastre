package i18n

func init() {
	add("fr", map[string]string{
		"cli.flag.report":   "écrire le rapport HTML autonome dans ce fichier",
		"cli.flag.json":     "écrire la synthèse JSON dans ce fichier",
		"cli.flag.csv":      "écrire la liste complète des entrées en CSV",
		"cli.flag.csvmin":   "CSV : ignorer les entrées plus petites (ex. 1M)",
		"cli.flag.md":       "écrire la synthèse Markdown dans ce fichier",
		"cli.flag.snapshot": "enregistrer un snapshot .cad (réouvrable sans rescan)",
		"cli.flag.open":     "ouvrir le rapport HTML dans le navigateur",
		"cli.written":       "→ %s",
		"cli.cmd.open":      "rouvre un snapshot .cad et produit les exports",
		"cli.open.usage":    "Utilisation : cadastre open <scan.cad> [--report r.html] [--json o.json] [--csv o.csv] [--md o.md]",
	})
	add("en", map[string]string{
		"cli.flag.report":   "write the standalone HTML report to this file",
		"cli.flag.json":     "write the JSON summary to this file",
		"cli.flag.csv":      "write the complete list of entries as CSV",
		"cli.flag.csvmin":   "CSV: skip smaller entries (e.g. 1M)",
		"cli.flag.md":       "write the Markdown summary to this file",
		"cli.flag.snapshot": "save a .cad snapshot (reopenable without rescanning)",
		"cli.flag.open":     "open the HTML report in the browser",
		"cli.written":       "→ %s",
		"cli.cmd.open":      "reopens a .cad snapshot and produces the exports",
		"cli.open.usage":    "Usage: cadastre open <scan.cad> [--report r.html] [--json o.json] [--csv o.csv] [--md o.md]",
	})
}
