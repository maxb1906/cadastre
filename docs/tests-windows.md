# Tests sur le PC Windows

Le devbox est sous Linux ARM64 : le parseur, l'index, les règles, les rapports et le nettoyage y sont testés,
le parseur NTFS sur une vraie image NTFS. Ce qui touche à l'API Windows se valide ici :
handle `\\.\C:`, `FindFirstFileExW`, corbeille `IFileOperation`, suppression par handle, WMI/VSS, registre, WebView2.

Exécutable : `build\bin\cadastre.exe` (produit par `wails build -platform windows/amd64 -o cadastre.exe`).
Ouvrir **PowerShell en administrateur** dans le dossier de l'exe.

## 1. Interface graphique

1. Double-clic sur `cadastre.exe` (sans admin) : l'accueil signale « Sans droits administrateur ». Relever `C:` doit passer par le parcours classique.
2. Clic droit → *Exécuter en tant qu'administrateur* : les volumes NTFS affichent « MFT ⚡ ». Relever `C:`. Points à vérifier :
   - durée affichée (objectif : quelques secondes pour 1 M de fichiers) ;
   - onglet **À nettoyer** : règles classées en Sans risque / Avec précaution / Action officielle / Pour information ; WinSxS en 🔴 dans Action officielle, Windows Update marqué « via Arrêt des services… », temporaires marqués « définitif uniquement », corbeilles avec nom d'utilisateur. Sans admin : les règles système portent « admin requis » ;
   - cases à cocher (règle et groupe entier), dépliage d'une règle, ajout d'un fichier par clic droit → groupe « Sélection manuelle » ; le pied du panneau suit la sélection et reste visible en défilant ;
   - onglet **Espace manquant** : décomposition (métafichiers `$MFT`, `$LogFile`, `$UsnJrnl:$J`, VSS…) et écart non attribué faible ;
   - navigation : double-clic dans la liste, clic dans la treemap, `Retour arrière` pour remonter, clic droit → *Afficher dans l'Explorateur* ;
   - **Exporter** → Rapport HTML (s'ouvre dans le navigateur), CSV (s'ouvre dans Excel avec les accents) ;
   - **Snapshot** puis glisser-déposer le `.cad` sur la fenêtre.
3. Langue : bouton 🌐.

## 2. Ligne de commande

```powershell
.\cadastre.exe scan C: --report $env:TEMP\c.html --open | Out-Host
.\cadastre.exe scan C: D: --json $env:TEMP\vol.json --snapshot $env:TEMP\vol.cad | Out-Host
.\cadastre.exe open $env:TEMP\vol-C.cad --md $env:TEMP\c.md | Out-Host
.\cadastre.exe scan C:\Users --engine walk | Out-Host          # dossier : moteur C
.\cadastre.exe scan \\serveur\partage | Out-Host               # partage réseau
```

## 3. Oracle croisé MFT / parcours (validation du moteur A)

```powershell
.\cadastre.exe check C: | Out-Host
```

Attendu : « Vus seulement par A » presque vide (fichiers créés pendant le scan).
« Vus seulement par C » contient les liens durs que le parcours compte deux fois, plus les dossiers que le parcours voit et que l'oracle exclut.
Les différences de taille concernent des fichiers en cours d'écriture (journaux, `pagefile.sys`).

## 4. Volume de test et tests d'intégration (recommandé)

Nécessite Go. Le script crée un VHDX de 2 Go avec liens durs, ADS, sparse, compression NTFS et WOF, jonction, symlink, Unicode, chemin long et 50 000 petits fichiers :

```powershell
.\scripts\new-testvhd.ps1 -Letter T -SizeMB 2048 -Files 50000
$env:CADASTRE_TEST_VOLUME = 'T:'
go test ./internal/engine ./internal/clean -run Integration -v
.\cadastre.exe check T: | Out-Host
.\scripts\new-testvhd.ps1 -Letter T -Remove
```

`TestIntegrationClean` supprime de vrais fichiers sur `T:` uniquement, avec les deux moteurs et les deux modes (corbeille et définitif). Il vérifie qu'un fichier modifié après la simulation n'est pas supprimé, que la cible d'une jonction est conservée et qu'un fichier en lecture seule est supprimé en mode définitif.

## 5. Nettoyage (sur le volume de test d'abord)

```powershell
.\cadastre.exe clean --list-rules | Out-Host
.\cadastre.exe clean T: --rule temp | Out-Host                                     # simulation : « mode définitif requis »
.\cadastre.exe clean T: --rule temp --permanent --script $env:TEMP\t.ps1 | Out-Host
powershell -File $env:TEMP\t.ps1                                                   # le script, en simulation
.\cadastre.exe clean T: --rule temp --permanent --apply | Out-Host                 # demande « oui »
Get-Content "$env:ProgramData\Cadastre\journal\$(Get-Date -f yyyy-MM-dd).jsonl" -Tail 5
.\cadastre.exe clean T: --path T:\data\big.bin --apply --yes-orange | Out-Host    # corbeille → vérifier la Corbeille
```

Dans la GUI (relevé de `T:`) :
1. Onglet **À nettoyer** : cocher une règle 🟢, choisir Corbeille, *Nettoyage* → l'écran liste chaque action concrète → *Supprimer maintenant*. Vérifier la Corbeille Windows et le journal.
2. Carte **Récupérable** : un clic coche exactement les règles qui composent le chiffre (le total du pied doit être égal à la carte) et ouvre l'écran de nettoyage. Chaque octet du total doit y apparaître dans une ligne d'action : fichiers supprimés par Cadastre, ou action officielle (ex. `powercfg /h off` pour `hiberfil.sys`) avec ses commandes. Sans la case orange, les actions orange sont grisées et le total le dit. En mode Corbeille, les temporaires sont écartés et un bandeau propose *Passer en mode définitif* : vérifier qu'ils sont alors inclus.
3. Sans cocher la confirmation orange, lancer : seuls les éléments 🟢 sont supprimés, les 🟠 apparaissent « ignorés » dans le résultat et le journal.

Actions officielles (sur C:, avec prudence) :
`cadastre clean C: --rule delivery-optimization --official --apply` lance `Delete-DeliveryOptimizationCache`.
`--rule component-store --official --restore-point --apply` lance DISM et peut prendre plusieurs minutes.

## 6. Cas particuliers à vérifier

- **WebView2 absent** (Windows Server 2016/2019 sans Edge) : au double-clic, une boîte propose d'installer le runtime ou de basculer en CLI avec le rapport HTML dans le navigateur.
- **Server Core** : sans argument, l'exe affiche l'aide de la CLI.
- **BitLocker** : aucun impact attendu, la lecture MFT passe par le volume déchiffré.
- **Antivirus** : la lecture brute de `\\.\C:` peut déclencher une alerte. À noter le cas échéant.
