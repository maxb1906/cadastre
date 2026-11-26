package clean

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/maxb1906/cadastre/internal/i18n"
)

// WriteScript writes a commented PowerShell script performing the same plan,
// for those who prefer to run it themselves. Without -Apply it only shows
// what it would do. It keeps the same safeguards: each file is checked
// (size, modification time) right before deletion, links are never
// followed. UTF-8 with BOM so that Windows PowerShell 5.1 reads accents.
func WriteScript(w io.Writer, p *Plan, lang string) error {
	tr := i18n.Printer{Lang: lang}
	var b strings.Builder
	b.WriteString("\uFEFF")
	fmt.Fprintf(&b, "# Cadastre — %s\n", tr.T("script.title"))
	fmt.Fprintf(&b, "# %s : %s\n", tr.T("script.generated"), time.Now().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "# %s : %s · %s : %s · plan %s\n", tr.T("script.root"), p.Root, tr.T("script.mode"), p.Mode, p.ID)
	fmt.Fprintf(&b, "# %s\n", tr.T("script.usage"))
	for _, l := range strings.Split(tr.T("script.safety"), "\n") {
		fmt.Fprintf(&b, "# %s\n", l)
	}
	b.WriteString(`
[CmdletBinding()]
param(
    [switch]$Apply,
    [switch]$Official
)
$ErrorActionPreference = 'Continue'
Add-Type -AssemblyName Microsoft.VisualBasic
$script:freed = 0; $script:done = 0; $script:skipped = 0; $script:failed = 0

function Test-Unchanged([string]$Path, [long]$Length, [long]$UnixTime, [bool]$IsDir) {
    $i = Get-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
    if (-not $i) { return 'gone' }
    if ($i.PSIsContainer -ne $IsDir) { return 'type_changed' }
    if ($i.Attributes -band [IO.FileAttributes]::ReparsePoint) { return 'ok' }
    if ($IsDir) { return 'ok' }
    $t = [DateTimeOffset]::new($i.LastWriteTimeUtc).ToUnixTimeSeconds()
    if ($i.Length -ne $Length -or $t -ne $UnixTime) { return 'changed' }
    return 'ok'
}

function Remove-Checked([string]$Path, [long]$Length, [long]$UnixTime, [bool]$IsDir, [long]$Bytes, [bool]$Permanent) {
    $state = Test-Unchanged $Path $Length $UnixTime $IsDir
    if ($state -ne 'ok') { Write-Host "  [skip:$state] $Path"; $script:skipped++; return }
    if (-not $Apply) { Write-Host "  [simulation] $Path"; return }
    try {
        $item = Get-Item -LiteralPath $Path -Force
        if ($IsDir -and -not ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
            if (@(Get-ChildItem -LiteralPath $Path -Force).Count -gt 0) { $script:skipped++; return }
            Remove-Item -LiteralPath $Path -Force -ErrorAction Stop
        } elseif ($Permanent -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
            # A link or junction is removed itself, never its target.
            $item.Delete()
        } else {
            [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile($Path, 'OnlyErrorDialogs', 'SendToRecycleBin')
        }
        $script:done++; $script:freed += $Bytes
        Write-Host "  [ok] $Path"
    } catch {
        $script:failed++
        Write-Warning "  [failed] $Path : $($_.Exception.Message)"
    }
}

`)
	perm := "$false"
	if p.Mode == ModePermanent {
		perm = "$true"
	}
	if len(p.Items) > 0 {
		fmt.Fprintf(&b, "# --- %s (%d) ---\n", tr.T("script.items"), len(p.Items))
		rule := "\x00"
		for _, it := range p.Items {
			if it.Rule != rule {
				rule = it.Rule
				name := rule
				if name == "" {
					name = tr.T("script.manual")
				}
				fmt.Fprintf(&b, "\nWrite-Host '%s'\n", strings.ReplaceAll(name, "'", "''"))
			}
			fmt.Fprintf(&b, "Remove-Checked %s %d %d $%t %d %s\n", PSQuote(it.Path), it.Size, it.MTime, it.Dir && !it.Reparse, it.Alloc, perm)
		}
	}
	for _, o := range p.Official {
		fmt.Fprintf(&b, "\n# --- %s : %s (%s) ---\n", tr.T("script.official"), o.Title.In(lang), o.Rule)
		for _, t := range o.Targets {
			fmt.Fprintf(&b, "#     %s\n", t)
		}
		b.WriteString("if ($Apply -and $Official) {\n")
		if o.RestorePoint {
			b.WriteString("    Checkpoint-Computer -Description 'Cadastre' -RestorePointType MODIFY_SETTINGS\n")
		}
		for _, c := range o.Commands {
			if c == nil {
				for _, it := range o.Items {
					fmt.Fprintf(&b, "    Remove-Checked %s %d %d $%t %d $true\n", PSQuote(it.Path), it.Size, it.MTime, it.Dir && !it.Reparse, it.Alloc)
				}
				continue
			}
			parts := make([]string, len(c))
			for k, a := range c {
				parts[k] = PSQuote(a)
			}
			fmt.Fprintf(&b, "    & %s\n", strings.Join(parts, " "))
		}
		b.WriteString("} else {\n")
		for _, c := range o.Commands {
			if c != nil {
				fmt.Fprintf(&b, "    Write-Host %s\n", PSQuote("  [simulation] "+strings.Join(c, " ")))
			}
		}
		b.WriteString("}\n")
	}
	if len(p.Skipped) > 0 {
		fmt.Fprintf(&b, "\n# --- %s (%d) ---\n", tr.T("script.skipped"), len(p.Skipped))
		for k, s := range p.Skipped {
			if k >= 500 {
				fmt.Fprintf(&b, "#   … +%d\n", len(p.Skipped)-k)
				break
			}
			fmt.Fprintf(&b, "#   [%s] %s\n", s.Reason, s.Path)
		}
	}
	b.WriteString("\nWrite-Host ''\nWrite-Host (\"done=$($script:done) skipped=$($script:skipped) failed=$($script:failed) freed=\" + [math]::Round($script:freed / 1MB, 1) + ' MB')\n")
	b.WriteString("if (-not $Apply) { Write-Host '" + strings.ReplaceAll(tr.T("script.simulated"), "'", "''") + "' }\n")
	_, err := io.WriteString(w, strings.ReplaceAll(b.String(), "\n", "\r\n"))
	return err
}
