# Cadastre

Analyseur d'empreinte disque et de fichiers fantômes pour Windows. Même famille qu'Arpent, Aplomb et Jalon.
**Référence : docs/cadrage.md.** À lire avant toute tâche. En cas de doute, le cadrage fait foi.

## Conventions
- Go + Wails v2 (frontend vanilla TypeScript dans frontend/), licence GPL-3.0, interface FR + EN (internal/i18n).
- Module : github.com/maxb1906/cadastre. Un seul exe : GUI Wails par défaut, CLI si des arguments sont passés.
- Code et commentaires en anglais, messages de commit en français.

## Environnement de dev
- Devbox Linux ARM64 : le code Windows ne s'exécute PAS ici.
- Tout code qui appelle l'API Windows va dans des fichiers `*_windows.go`.
- Le parseur NTFS travaille sur des `[]byte`, sans appel système : il est testable et fuzzable sous Linux
  avec des enregistrements MFT synthétiques (testdata/).
- Vérifications obligatoires avant chaque commit :
  - `go test ./...`
  - `GOOS=windows GOARCH=amd64 go vet ./...`
  - `wails build -platform windows/amd64 -o cadastre.exe`
- Les tests réels (scan MFT, suppression) se font sur le PC Windows de Maxime : quand une étape le nécessite, fournis la commande de test.

## Règles impératives
- Index en Structure of Arrays, sans pointeur dans les tableaux. Objectif : ≤ 64 octets par entrée.
- Nettoyage : simulation par défaut, corbeille par défaut, revalidation juste avant suppression, liste noire en dur,
  aucune suppression des règles 🔴, journal JSONL (cadrage § 5 bis). Aucun raccourci sur ces points.
- Avancer jalon par jalon (cadrage § 10) : un jalon = une branche + des commits atomiques.
  Montre le plan avant de coder chaque jalon.
