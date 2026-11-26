<#
.SYNOPSIS
  Crée un volume NTFS de test (VHDX monté) au contenu connu pour Cadastre.

.DESCRIPTION
  Utilise diskpart (pas besoin du module Hyper-V). À lancer en administrateur.
  Contenu : liens durs, ADS, fichier sparse, fichiers compressés (NTFS et WOF),
  jonction, lien symbolique, noms Unicode, chemin > 260 caractères, petits
  fichiers en nombre (option -Files) pour forcer la fragmentation de la MFT.

.EXAMPLE
  .\scripts\new-testvhd.ps1 -Letter T -SizeMB 2048 -Files 200000
  $env:CADASTRE_TEST_VOLUME = 'T:'; go test ./internal/engine -run Integration -v
  .\scripts\new-testvhd.ps1 -Letter T -Remove
#>
param(
    [string]$Letter = 'T',
    [int]$SizeMB = 1024,
    [int]$Files = 20000,
    [string]$Path = "$env:TEMP\cadastre-test.vhdx",
    [switch]$Remove
)
$ErrorActionPreference = 'Stop'

function Invoke-Diskpart([string[]]$Lines) {
    $f = New-TemporaryFile
    Set-Content -LiteralPath $f -Value $Lines -Encoding ASCII
    diskpart /s $f | Out-Host
    Remove-Item $f
}

if ($Remove) {
    Invoke-Diskpart @("select vdisk file=`"$Path`"", 'detach vdisk')
    Remove-Item -LiteralPath $Path -Force
    return
}

if (Test-Path $Path) { throw "$Path existe déjà (utilisez -Remove)" }
Invoke-Diskpart @(
    "create vdisk file=`"$Path`" maximum=$SizeMB type=expandable",
    "select vdisk file=`"$Path`"",
    'attach vdisk',
    'create partition primary',
    "format fs=ntfs quick label=CADTEST",
    "assign letter=$Letter"
)
$r = "${Letter}:\"
Start-Sleep -Seconds 2

New-Item -ItemType Directory "$r\Windows\Temp", "$r\Users\alice\AppData\Local\Temp", "$r\data", "$r\many" -Force | Out-Null
fsutil file createnew "$r\data\big.bin" 52428800 | Out-Null                       # 50 Mio
Set-Content -LiteralPath "$r\data\notes.txt" -Value ('x' * 10000) -NoNewline
Set-Content -LiteralPath "$r\data\notes.txt" -Stream secret -Value ('s' * 200000) -NoNewline   # ADS
New-Item -ItemType HardLink -Path "$r\Users\alice\big-link.bin" -Target "$r\data\big.bin" | Out-Null
fsutil file createnew "$r\data\sparse.bin" 1073741824 | Out-Null
fsutil sparse setflag "$r\data\sparse.bin"
fsutil sparse setrange "$r\data\sparse.bin" 0 1073741824                           # 1 Gio logique, ~0 alloué
New-Item -ItemType Directory "$r\compressed" | Out-Null
compact /c /q "$r\compressed" | Out-Null
Set-Content -LiteralPath "$r\compressed\zeros.txt" -Value ('0' * 5000000) -NoNewline
Set-Content -LiteralPath "$r\data\wof.txt" -Value ('w' * 3000000) -NoNewline
compact /c /exe:lzx /q "$r\data\wof.txt" | Out-Null                                 # WOF
cmd /c mklink /J "$r\data\junction" "$r\Windows" | Out-Null
cmd /c mklink "$r\data\symlink.bin" "$r\data\big.bin" | Out-Null
Set-Content -LiteralPath "$r\data\日本語 😀 été.txt" -Value 'unicode'
# Path > 260 characters. .NET (PowerShell 7) handles long paths natively;
# New-Item -Path would read the "?" of a \\?\ prefix as a wildcard.
$deep = $r + ((1..25 | ForEach-Object { "niveau-$_-abcdefghij" }) -join '\')
[System.IO.Directory]::CreateDirectory($deep) | Out-Null
[System.IO.File]::WriteAllText("$deep\fond.txt", 'deep')
$old = (Get-Date).AddDays(-30)
foreach ($t in "$r\Windows\Temp\old.tmp", "$r\Users\alice\AppData\Local\Temp\old.tmp") {
    fsutil file createnew $t 1048576 | Out-Null
    (Get-Item $t).LastWriteTime = $old
}
fsutil file createnew "$r\Windows\Temp\fresh.tmp" 4096 | Out-Null
for ($i = 1; $i -le $Files; $i++) {
    [IO.File]::WriteAllText("$r\many\f$i.txt", "$i")
    if ($i % 5000 -eq 0) {
        fsutil file createnew "$r\many\filler$i.bin" 4194304 | Out-Null             # fragmente la MFT
        Write-Progress -Activity 'Petits fichiers' -PercentComplete (100 * $i / $Files)
    }
}
Write-Host "Volume de test prêt : $r ($Path)"
