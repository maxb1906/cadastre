# Cadastre

> *Le cadastre recense qui occupe quel terrain, et combien.*

Analyseur d'empreinte disque et de fichiers fantômes pour Windows, de la même famille qu'Arpent, Aplomb et Jalon.
Un seul exécutable portable, sans installation : interface graphique par double-clic, ligne de commande dès qu'on lui passe des arguments.

- **Rapide** : en administrateur, Cadastre lit directement la `$MFT` du volume NTFS. Quelques secondes suffisent pour des millions de fichiers.
- **Il trouve les fichiers fantômes** : dumps, cache Windows Update, journaux IIS/Apache, `Windows.old`, disques virtuels WSL/Docker, caches de développement et de navigateurs, corbeilles par utilisateur, profils fantômes, Windows Installer orphelins, flux ADS volumineux… (24 règles, extensibles en YAML).
- **Il explique l'espace manquant** : l'espace utilisé du volume est rapproché de la somme des fichiers visibles, et l'écart est décomposé en métafichiers NTFS, clichés VSS, ADS, index de dossiers et reste non attribué.
- **Il agit et il trace** : nettoyage avec simulation obligatoire, corbeille par défaut, revalidation de chaque fichier juste avant sa suppression, liste noire en dur et journal JSONL. Il peut aussi générer un script `.ps1` équivalent pour qui préfère l'exécuter lui-même.
- **Il rapporte** : rapport HTML autonome avec treemap interactive, exports CSV, JSON et Markdown, snapshots `.cad` réouvrables sans rescan.

Interface en français et en anglais. Licence GPL-3.0.

## Démarrage

| Action | Commande |
|---|---|
| Interface graphique | double-clic sur `cadastre.exe` (clic droit → *Exécuter en tant qu'administrateur* pour lire la MFT) |
| Relevé en console | `cadastre scan C:` |
| Rapport HTML ouvert dans le navigateur | `cadastre scan C: --report C:\temp\c.html --open` |
| Plusieurs volumes en parallèle | `cadastre scan C: D: --report rapport.html` (produit `rapport-C.html`, `rapport-D.html`) |
| Snapshot puis réouverture | `cadastre scan C: --snapshot c.cad` puis `cadastre open c.cad --report c.html` |
| Liste des règles | `cadastre clean --list-rules` |
| Simulation de nettoyage | `cadastre clean C: --rule temp --rule iis-logs --older-than 30d` |
| Nettoyage réel | ajouter `--apply` (et `--permanent`, `--yes-orange`, `--official`, `--restore-point` selon les cas) |
| Script PowerShell équivalent | `cadastre clean C: --rule temp --script nettoyage.ps1` |
| Nettoyage suggéré sur plusieurs disques | `cadastre clean --suggested C: D:` (simulation), puis `--apply` ; `--yes-orange` ajoute les règles orange |
| Comparer les deux moteurs | `cadastre check C:` (en administrateur) |

Aide d'une commande : `cadastre <commande> -h`. Langue : `--lang fr|en` (sinon la langue de Windows, ou la variable `CADASTRE_LANG`).

> L'exécutable est compilé pour le sous-système graphique. En ligne de commande, il se rattache à la console qui l'a lancé.
> Dans PowerShell, l'invite peut réapparaître avant la fin de la sortie. Pour l'attendre, utilisez `cadastre.exe scan C: | Out-Host`
> ou `Start-Process -Wait -NoNewWindow cadastre.exe 'scan C:'`.

## Moteurs

| Moteur | Quand | Remarques |
|---|---|---|
| **A. MFT brute** | volume NTFS entier + droits administrateur | lit `$MFT` par blocs de 4 Mo sur un pipeline multithreadé ; voit métafichiers, ADS, liens durs (comptés une fois), fichiers sparse, compressés et WOF |
| **C. Parcours classique** | sans droits admin, dossier, partage réseau, ReFS/Dev Drive, FAT/exFAT | `FindFirstFileExW` multithreadé ; les dossiers inaccessibles sont signalés |

Le choix est automatique (`--engine auto`). Si la lecture MFT échoue, Cadastre bascule sur le moteur C et le signale.

## Nettoyage : garde-fous

Aucun de ces garde-fous n'est désactivable :

1. **Simulation obligatoire.** Le plan liste exactement ce qui serait supprimé, ce qui est écarté et pourquoi, et revérifie chaque élément sur le disque. Sans simulation, rien ne s'exécute. En CLI, `--apply` est requis.
2. **Corbeille par défaut.** Le mode définitif est une option explicite. Il est obligatoire là où la corbeille n'a pas de sens (`Windows\Temp`, corbeilles, cache Windows Update).
3. **Revalidation juste avant suppression.** Taille, date de modification et numéro de fichier doivent être identiques à ceux du relevé. En mode définitif, la vérification et la suppression passent par le même handle.
4. **Liste noire en dur** : racines de volume, métafichiers NTFS, `Windows` (hors sous-dossiers autorisés par une règle), `System32`, `SysWOW64`, `WinSxS`, `Program Files`, `ProgramData`, profils entiers, `pagefile.sys`/`hiberfil.sys`/`swapfile.sys`, System Volume Information, Boot, Recovery…
5. **Règles 🔴 jamais supprimées**, même redéfinies dans un YAML perso : seule l'action officielle est proposée (ex. `DISM /StartComponentCleanup` pour WinSxS).
6. **Points de reparse** : la jonction ou le lien est supprimé, jamais sa cible.
7. **Placeholders OneDrive/Cloud Files exclus.** Zones système grisées sans droits admin. Navigateurs à fermer avant de vider leurs caches.
8. **Snapshot rouvert** : la simulation revérifie chaque élément sur le disque avant d'autoriser quoi que ce soit.
9. **Journal JSONL** : chaque action (y compris la sortie des commandes officielles) est écrite dans `%ProgramData%\Cadastre\journal\<date>.jsonl`.

## Règles maison

Ajoutez `--rules perso.yaml` (répétable) ; voir [`rules/exemple-perso.yaml`](rules/exemple-perso.yaml). Une règle portant l'identifiant d'une règle existante la remplace.

## Développement

```
go test ./...                                         # tests (parseur NTFS testé sur une vraie image NTFS)
GOOS=windows GOARCH=amd64 go vet ./...
wails build -platform windows/amd64 -o cadastre.exe   # → build/bin/cadastre.exe
go test ./internal/mftscan -run '^$' -bench Scan -benchtime 1x   # benchmarks 1M/5M/10M
./scripts/oracle-linux.sh                             # oracle croisé A/C sur l'image NTFS (sudo, ntfs-3g)
```

- Référence fonctionnelle : [`docs/cadrage.md`](docs/cadrage.md). Mesures : [`docs/benchmarks.md`](docs/benchmarks.md). Tests sur Windows : [`docs/tests-windows.md`](docs/tests-windows.md).
- Le parseur NTFS (`internal/ntfs`) travaille sur des `[]byte`, sans appel système. Il est fuzzé (`go test -fuzz FuzzParse ./internal/ntfs`) et validé sur une image NTFS réelle produite par `scripts/make-ntfs-fixture.sh` (mkntfs + ntfs-3g) : liens durs, ADS, sparse, compression, jonction, Unicode, chemins de plus de 260 caractères.
- L'index est en Structure of Arrays : 44 octets fixes par entrée plus le nom, sans pointeur dans les tableaux.

### Antivirus

Un exécutable qui ouvre `\\.\C:` en lecture brute peut alerter certains antivirus. Cadastre n'écrit jamais sur le volume en accès brut : la lecture de la MFT est strictement en lecture seule. Les suppressions passent par les API Windows standard (`IFileOperation`, `SetFileInformationByHandle`).
