---
projet: Cadastre
statut: cadrage terminé
famille: "[[Arpent]] · [[Aplomb]] · [[Jalon]]"
tags: [dev, windows, ntfs, go, open-source]
---

# Cadastre — Analyseur d'empreinte disque et de fichiers fantômes

> *Le cadastre recense qui occupe quel terrain, et combien.* Après l'arpentage du réseau (Arpent), la mise d'aplomb du poste (Aplomb) et le jalonnement de la connectivité (Jalon), Cadastre fait le relevé de l'occupation du disque.

**Nom de travail : Storage Profiler** → **nom proposé : Cadastre** (CLI : `cadastre`, dépôt `maxb1906/cadastre`)
Autres pistes si besoin : *Relevé*, *Tamis*, *Emprise*.

---

## 1. Pitch

Un exe portable qui, lancé sur un poste ou un serveur Windows, lit directement la MFT d'un volume NTFS et cartographie **en quelques secondes** l'occupation réelle du disque. Il fait mieux que WinDirStat/WizTree sur trois points utiles à un admin :

1. **Il trouve les fichiers fantômes.** Il connaît les gros consommateurs habituels de Windows et des serveurs : dumps, caches Windows Update, logs IIS/Apache, `Windows.old`, VHDX WSL/Docker…
2. **Il explique l'espace manquant.** Il fait le rapprochement entre l'espace utilisé du volume et la somme des fichiers visibles : clichés VSS, métafichiers NTFS, flux ADS, fichiers aux ACL restrictives.
3. **Il agit et il trace.** Nettoyage intégré et sécurisé (simulation, corbeille, mécanismes officiels pour les zones système), journal de suppression. Il produit aussi un rapport HTML autonome, des exports CSV/JSON et un script `.ps1` pour qui préfère exécuter lui-même.

**Public :** support IT, admins sys, MSP. On le lance sur un serveur dont le disque C: est plein et on sait en 30 secondes quoi faire.

---

## 2. Point technique clé : la MFT ne se lit pas d'une seule façon

⚠️ **`FSCTL_ENUM_USN_DATA` ne renvoie pas la taille des fichiers.** Les enregistrements `USN_RECORD_V2/V3` donnent le FRN, le FRN parent, le nom, les attributs et un horodatage, mais **ni taille logique ni taille allouée**. C'est ce que fait *Everything* (indexation de noms). WizTree, lui, va plus loin et parse la `$MFT` brute.

Cadastre utilise donc **trois moteurs**, du plus rapide au plus universel :

| Moteur | Source | Donne | Droits | Usage |
|---|---|---|---|---|
| **A. MFT brute** (principal) | Lecture séquentielle de `$MFT` via le handle volume `\\.\C:` | Tout : nom, parent, tailles logique/allouée, horodatages, attributs, ADS, liens durs | Admin | Scan complet NTFS |
| **B. USN enum** | `FSCTL_ENUM_USN_DATA` | Arborescence + noms (sans tailles) | Admin | Contrôle de cohérence ; base pour les rescans incrémentaux |
| **C. Parcours classique** (repli) | `FindFirstFileExW` (`FindExInfoBasic` + `FIND_FIRST_EX_LARGE_FETCH`) multithreadé | Tailles via l'énumération de répertoire | Utilisateur | Pas admin, ReFS (Dev Drive), FAT/exFAT, partages réseau |

**Incrémental (v2) :** après un scan, on garde l'USN courant. Au scan suivant, `FSCTL_READ_USN_JOURNAL` fournit seulement les changements. Ça permet un rescan quasi instantané et un « qu'est-ce qui a grossi depuis hier ».

### Parser la MFT brute : points d'attention
- `FSCTL_GET_NTFS_VOLUME_DATA` → `MftStartLcn`, `BytesPerFileRecordSegment` (souvent 1024), `BytesPerCluster`, `MftValidDataLength`.
- La `$MFT` est elle-même **fragmentée** : lire la runlist de son attribut `$DATA` (enregistrement 0), ou appeler `FSCTL_GET_RETRIEVAL_POINTERS` sur `C:\$MFT`.
- **Fixups** (Update Sequence Array) à appliquer sur chaque enregistrement `FILE` avant le parsing.
- Enregistrements non utilisés (flag `IN_USE`) et enregistrements d'extension (base record ≠ 0) → fusionner avec le record de base.
- `$ATTRIBUTE_LIST` : attributs répartis sur plusieurs enregistrements (fichiers très fragmentés). C'est rare mais obligatoire.
- `$FILE_NAME` multiples : ignorer l'espace de noms DOS 8.3 et garder Win32/POSIX. Les **liens durs** donnent plusieurs parents, d'où une règle de comptage à choisir (taille comptée une seule fois, liens signalés).
- `$DATA` résident vs non résident ; **taille logique ≠ taille allouée** pour les fichiers compressés, sparse ou dédupliqués (WOF/CompactOS). Afficher les deux.
- **Flux ADS** (`$DATA` nommés) : à comptabiliser et à signaler, ce sont de bons candidats « fantômes ».
- Points de reparse (jonctions, symlinks, placeholders OneDrive/Cloud Files) : ne pas les suivre, mais les marquer.
- Lecture par gros blocs (1 à 4 Mo) alignés sur le secteur ; I/O séquentielles, c'est ce qui rend l'approche rapide sur HDD comme sur SSD.
- BitLocker : pas d'impact, le handle volume lit des données déchiffrées.

---

## 3. Architecture

```
            ┌───────────────┐
 volume ──► │ Lecteur I/O   │  1 goroutine, gros blocs séquentiels
            └──────┬────────┘
                   │ chunks de records FILE
            ┌──────▼────────┐
            │ Parseurs × N  │  N = nb cœurs ; fixups + attributs
            └──────┬────────┘
                   │ entrées compactes
            ┌──────▼────────┐
            │ Index (SoA)   │  tableaux par champ, indices uint32
            └──────┬────────┘
                   │
     ┌─────────────┼──────────────┬──────────────┐
     ▼             ▼              ▼              ▼
 Agrégation    Règles        Rapprochement   Snapshot
 bottom-up     « fantômes »  espace volume   (.cad, zstd)
     │             │              │              │
     └──────► GUI Wails / Rapport HTML / CSV / JSON / script .ps1
```

### Gestion mémoire (des millions d'entrées)
- **Structure of Arrays** au lieu d'un objet par fichier : `parent []uint32`, `size []uint64`, `alloc []uint64`, `mtime []int64`, `attr []uint32`, `nameOff []uint32`, `nameLen []uint16`, `ext []uint16`.
- **Arène de noms** : un seul gros `[]byte` UTF-8 ; extensions internées dans une table.
- **Index FRN → indice dense** : le numéro d'enregistrement MFT sert directement d'index (tableau plat, pas de map).
- Pas de pointeurs dans les tableaux, donc **un GC Go quasi inactif** même avec 10 M d'entrées.
- Agrégation des répertoires en un seul passage inverse (enfants avant parents), sans récursion.
- **Objectifs :** ≤ 64 octets/entrée en moyenne (noms compris), soit environ 640 Mo pour 10 M de fichiers. 1 M de fichiers en < 3 s sur SSD, 10 M en < 30 s.

### Multithreading
- Un lecteur, N parseurs, puis une fusion. Les parseurs écrivent dans des plages disjointes de l'index (indexées par numéro de record), ce qui évite tout verrou.
- Les règles « fantômes » et l'agrégation par extension tournent en parallèle sur des tranches de l'index.
- Plusieurs volumes sont scannés en parallèle (un pipeline par volume).

---

## 4. Catalogue « fantômes » (règles v1)

Chaque règle a une **catégorie**, un **niveau de risque** (🟢 sûr / 🟠 avec précaution / 🔴 ne jamais supprimer à la main) et une **action recommandée**.

| Catégorie | Emplacements | Risque | Action recommandée |
|---|---|---|---|
| Dumps mémoire | `C:\Windows\MEMORY.DMP`, `Minidump\`, `LiveKernelReports\`, `%LOCALAPPDATA%\CrashDumps` | 🟢 | Supprimer après analyse éventuelle |
| Rapports d'erreurs | `C:\ProgramData\Microsoft\Windows\WER\` | 🟢 | Supprimer |
| Cache Windows Update | `C:\Windows\SoftwareDistribution\Download` | 🟠 | Arrêter `wuauserv`/`bits`, vider, relancer |
| Optimisation de distribution | `C:\Windows\ServiceProfiles\NetworkService\AppData\Local\Microsoft\Windows\DeliveryOptimization` | 🟠 | `Delete-DeliveryOptimizationCache` |
| Magasin de composants | `C:\Windows\WinSxS` | 🔴 | `DISM /Online /Cleanup-Image /StartComponentCleanup` (jamais à la main) |
| Restes de mise à niveau | `C:\Windows.old`, `C:\$Windows.~BT`, `C:\$Windows.~WS` | 🟠 | Nettoyage de disque / Paramètres de stockage |
| Installer orphelins | `C:\Windows\Installer\*.msp/.msi` non référencés | 🔴 | Signalement seulement (v1) |
| Logs IIS | `C:\inetpub\logs\LogFiles\W3SVC*` | 🟢 | Supprimer au-delà de N jours + conseiller une rotation |
| Logs Apache / Nginx | `*\Apache*\logs\`, `*\nginx\logs\` (détection par motif) | 🟢 | Idem |
| Logs Windows | `C:\Windows\Logs\CBS\`, `C:\Windows\Panther\`, `*.etl` obèses | 🟠 | Archiver puis supprimer |
| Journaux d'événements | `C:\Windows\System32\winevt\Logs\*.evtx` démesurés | 🟠 | `wevtutil` (export puis clear) |
| Temporaires | `C:\Windows\Temp`, `%TEMP%` de chaque profil | 🟢 | Supprimer les fichiers de plus de 7 jours |
| Corbeilles | `C:\$Recycle.Bin\<SID>` (par utilisateur) | 🟢 | Vider (nom d'utilisateur résolu à partir du SID) |
| Fichiers système | `hiberfil.sys`, `pagefile.sys`, `swapfile.sys` | 🟠 | `powercfg /h off` / revoir le dimensionnement |
| Disques virtuels | `ext4.vhdx` (WSL), `docker_data.vhdx`, `*.vhd(x)`, `*.iso` | 🟠 | `Optimize-VHD` / compacter |
| Caches dev | `node_modules`, `%LOCALAPPDATA%\npm-cache`, `.nuget\packages`, `pip\cache`, `.gradle` | 🟢 | Supprimer (régénérables) |
| Caches navigateurs | Profils Chrome/Edge/Firefox `Cache*` | 🟢 | Supprimer navigateur fermé |
| Sauvegardes SQL / Outlook | `*.bak`, `*.trn`, `*.ost` anciens | 🟠 | Signalement |
| Profils fantômes | `C:\Users\*` dont le SID n'existe plus | 🟠 | Signalement (+ commande CIM) |
| Flux ADS volumineux | Tout fichier avec un `$DATA` nommé > seuil | 🟠 | Signalement |
| Doublons (v2) | Mêmes taille + hash (xxh3 partiel puis complet) | 🟠 | Signalement |

Le niveau de risque pilote le nettoyage intégré : 🟢 sélectionnable directement, 🟠 sélectionnable après confirmation explicite (ou exécuté via l'action officielle de la règle), 🔴 jamais supprimable par Cadastre : seule l'action officielle est proposée (ex. DISM pour WinSxS) ou un simple signalement.

Les règles sont décrites en **YAML embarqué**. Les règles maison s'ajoutent avec `--rules perso.yaml`.

### Rapprochement de l'espace « inexpliqué »
`Espace utilisé (GetDiskFreeSpaceEx) − Σ fichiers vus = écart`, décomposé en :
- Métafichiers NTFS (`$MFT`, `$LogFile`, `$UsnJrnl:$J`, `$Bitmap`…), visibles directement dans la MFT brute ;
- Clichés instantanés VSS (`vssadmin list shadowstorage` / WMI `Win32_ShadowStorage`) ;
- Flux ADS ;
- Reste non attribué → signalé comme anomalie (fichier supprimé mais encore ouvert, corruption, quota…).

C'est le différenciateur face à WizTree : **« 38 Go manquent sur C: → 31 Go de clichés VSS, 4 Go de journal USN, 3 Go non attribués »**.

---

## 5. Fonctionnalités par version

### v1 — Relevé
- Scan NTFS par MFT brute (moteur A) + repli parcours classique (moteur C)
- Vue arbre triée par taille, top N fichiers/dossiers, répartition par extension et par catégorie
- **GUI native Wails** avec **treemap** (squarified), le même composant étant réutilisé dans le rapport HTML
- Catalogue fantômes + rapprochement de l'espace inexpliqué
- Rapport HTML autonome (un seul fichier, treemap incluse), exports CSV / JSON / Markdown
- **Nettoyage intégré** (voir § 5 bis), depuis la GUI et la CLI
- **Script `.ps1` de nettoyage commenté** en alternative, généré depuis la même sélection
- Snapshot `.cad` (binaire compressé zstd) pour rouvrir un scan sans rescanner
- CLI scriptable : `cadastre scan C: --report rapport.html --json out.json`

### v2 — Suivi
- Comparaison entre deux snapshots : « ce qui a grossi depuis le dernier scan »
- Rescan incrémental via le journal USN
- Détection de doublons (et nettoyage des doublons)
- Règles de nettoyage planifiées (ex. logs IIS > 30 jours, chaque semaine)
- Mode serveur : scan planifié, rapport déposé dans un dossier ou un partage

### Plus tard
- Scan d'un poste distant (exe poussé via un partage admin, rapport récupéré)
- Intégration avec Arpent (inventaire réseau → état disque par machine)

---

## 5 bis. Nettoyage intégré (v1)

Supprimer dès la v1 impose que la suppression soit **plus sûre qu'à la main**. Principes :

**Déroulé**
1. **Sélection** : depuis la treemap, l'arbre, le panneau fantômes, ou en CLI par règle (`--rule iis-logs --older-than 30d`).
2. **Simulation (dry-run) systématique** : liste exacte des éléments, volume libéré estimé, éléments ignorés et pourquoi. Aucune suppression sans passer par cet écran (en CLI : `cadastre clean … --dry-run` par défaut, `--apply` requis pour agir).
3. **Confirmation** : récapitulatif, choix du mode, puis validation. Les éléments 🟠 demandent une case à cocher dédiée.
4. **Exécution** avec progression, annulable entre deux éléments.
5. **Journal** : chaque action est consignée (horodatage, chemin, taille, mode, résultat) dans `%ProgramData%\Cadastre\journal\<date>.jsonl`, plus un résumé dans le rapport.

**Modes de suppression**
- **Corbeille par défaut** via `IFileOperation` (annulable par l'utilisateur).
- **Définitive** sur option explicite (obligatoire pour les gros volumes ou les zones où la corbeille n'a pas de sens, comme `Windows\Temp`), avec un avertissement.
- **Action officielle** pour les règles qui en ont une : arrêt/relance de `wuauserv` et `bits` autour du cache Windows Update, `Delete-DeliveryOptimizationCache`, `DISM /StartComponentCleanup`, `wevtutil` (export avant purge), `Optimize-VHD`, `powercfg /h off`. Cadastre lance la commande, en capture la sortie dans le journal et rescanne la zone.

**Garde-fous**
- **Revalidation juste avant suppression** : le fichier doit avoir la même taille, la même date de modification et le même numéro de fichier que lors du scan, sinon il est ignoré (pas de suppression d'un fichier remplacé entre-temps).
- **Jamais de suivi des points de reparse** : on supprime la jonction ou le lien, jamais sa cible.
- **Liste noire codée en dur**, non désactivable : racines de volume, `C:\Windows` (hors sous-dossiers autorisés par une règle), `System32`, `WinSxS`, `Program Files`, profils entiers, `pagefile.sys`/`hiberfil.sys` en suppression directe, métafichiers NTFS.
- Fichiers verrouillés : ignorés et listés (option v2 : suppression au redémarrage via `MoveFileEx(MOVEFILE_DELAY_UNTIL_REBOOT)`).
- Placeholders OneDrive/Cloud Files : exclus par défaut (les supprimer supprime le fichier dans le cloud).
- Zones système → droits admin requis, sinon l'action est grisée avec la raison.
- **Point de restauration** proposé avant une action officielle système (si la protection système est active).
- Aucune suppression lancée à partir d'un snapshot `.cad` rouvert sans rescan préalable des éléments concernés.

---

## 6. Interface

Un seul exe, deux modes :
- **GUI native avec Wails (WebView2)** : double-clic sur `cadastre.exe` (ou `cadastre gui`) → vraie fenêtre Windows. Contenu : treemap en canvas, arbre navigable, panneau fantômes, panneau « espace inexpliqué », sélection → script `.ps1`. Aucun port ouvert, aucun navigateur.
- **CLI** : complète et scriptable, pour Server Core ou une session PowerShell distante. Lancé avec des arguments, l'exe reste en mode console.
- **TUI** (optionnelle, v2) : arbre façon `ncdu` pour les sessions SSH/console.

### Pourquoi Wails
- **Un seul moteur de rendu** : la treemap et les vues sont écrites une fois en HTML/JS (TypeScript), puis réutilisées telles quelles dans le **rapport HTML exporté**.
- Fenêtre native : barre de titre, icône, menu, dialogues d'ouverture/enregistrement Windows, glisser-déposer d'un snapshot `.cad`.
- Le backend Go est appelé directement par les bindings Wails (pas d'API HTTP, pas de port en écoute).

### Dépendance WebView2
- Présent d'office sur Windows 10/11 récents et Windows Server 2022+. Il peut manquer sur Server 2016/2019 ou sur un poste non mis à jour.
- **Détection au lancement** : si WebView2 est absent → message clair et proposition de :
  1. basculer en mode CLI avec rapport HTML ouvert dans le navigateur par défaut (repli sans installation),
  2. ou installer le runtime WebView2 (lien Microsoft officiel, pas de bootstrapper embarqué pour rester portable).
- Server Core : pas de GUI → CLI uniquement, détecté automatiquement.

### Échelle (10 M d'entrées)
L'index reste côté Go. L'interface ne reçoit jamais l'arbre complet : elle demande **le niveau affiché** (enfants du dossier courant, treemap limitée à ~5 000 rectangles, les petits éléments sont regroupés en « autres »). La navigation se fait par appels Go ↔ JS à la demande, et les listes longues utilisent la virtualisation.

---

## 7. Stack et conventions (famille Arpent / Aplomb / Jalon)

- **Go** : `golang.org/x/sys/windows` pour `CreateFile`/`DeviceIoControl`, compilation croisée facile, exe unique statique. Le modèle SoA neutralise le principal défaut de Go (le GC sur des millions d'objets).
- **Wails v2** pour la GUI (backend Go + frontend TypeScript léger, sans framework lourd ; Svelte ou vanilla). Le frontend est compilé dans l'exe. Un build GUI (`-H windowsgui`) détache la console mais réattache celle du parent (`AttachConsole`) quand l'exe est lancé avec des arguments, pour que la CLI fonctionne depuis le même binaire.
- **Licence GPL-3.0** (Wails est sous licence MIT, compatible), interface **français + anglais**, dépôt public **`maxb1906/cadastre`**.
- Pas de dépendance runtime, pas d'installation. Manifeste `requireAdministrator` optionnel : sans droits admin, l'outil bascule sur le moteur C et le signale.
- Signature de code à envisager : un exe qui ouvre `\\.\C:` en lecture brute peut alerter certains antivirus.

---

## 8. Plan de tests

- **Volumes de test VHDX** créés par script (`New-VHD` + `Format-Volume NTFS`) avec un contenu connu : liens durs, ADS, fichiers sparse et compressés, fragmentation forcée, 1 M de petits fichiers. Exécution en CI sur un runner GitHub Actions `windows-latest`.
- **Oracle croisé** : comparer les totaux du moteur A avec le moteur C sur le même volume.
- **Fuzzing** du parseur d'enregistrements `FILE` (`go test -fuzz`), parce qu'un enregistrement corrompu ne doit jamais faire planter le scan.
- **Benchmarks** : 1 M / 5 M / 10 M d'entrées, avec mesure du temps et de la RAM pic.

---

## 9. Risques

| Risque | Mitigation |
|---|---|
| Bugs du parseur NTFS (cas exotiques) | Fuzzing + oracle croisé + repli automatique sur le moteur C en cas d'erreur |
| Droits admin indisponibles | Moteur C, avertissement clair dans le rapport |
| ReFS / Dev Drive / partages réseau | Moteur C (pas de MFT sur ReFS) |
| Faux positifs antivirus | Signature, binaire reproductible, README transparent |
| Nettoyage destructeur | Dry-run obligatoire, corbeille par défaut, revalidation avant suppression, liste noire en dur, actions officielles pour le système, règles 🔴 jamais supprimées, journal complet |
| Bug de suppression (chemin mal résolu) | Tests dédiés sur VHDX (liens durs, jonctions, noms longs > 260 car., Unicode), suppression par chemin `\\?\` reconstruit depuis l'index et revérifié |
| WebView2 absent (Server 2016/2019) | Détection au lancement → repli CLI + rapport HTML dans le navigateur, ou lien vers le runtime officiel |
| Interface lente sur gros arbres | Données servies par niveau, treemap plafonnée, listes virtualisées |
| RAM sur très gros volumes | SoA, objectif ≤ 64 o/entrée, option `--min-size` pour ne pas indexer les fichiers minuscules individuellement |

---

## 10. Jalons de développement

1. **Lecture MFT** : handle volume, `FSCTL_GET_NTFS_VOLUME_DATA`, runlist de `$MFT`, fixups, parsing `$STANDARD_INFORMATION` / `$FILE_NAME` / `$DATA`
2. **Index SoA + agrégation**, avec sortie texte top N, puis validation contre le moteur C
3. **Pipeline multithreadé** et benchmarks
4. **Moteur C** (repli) + détection automatique du moteur
5. **Catalogue fantômes** (YAML) + rapprochement de l'espace
6. **Exports** HTML / CSV / JSON / Markdown + snapshot `.cad`
7. **GUI Wails** : treemap (composant partagé avec le rapport), arbre, panneaux, détection WebView2
8. **Nettoyage intégré** : dry-run, corbeille/définitif, actions officielles, garde-fous, journal ; plus le générateur de script `.ps1`
9. i18n FR/EN, CI, release v1.0

---

## 11. Décisions à valider

- [x] Nom : **Cadastre**
- [x] Nettoyage v1 : **suppression intégrée dès la v1** (avec garde-fous § 5 bis), script `.ps1` conservé en alternative
- [x] Interface v1 : **GUI native Wails (WebView2) + CLI** dans le même exe ; TUI en v2
- [x] Périmètre v1 : **universel**, MFT brute sur NTFS + parcours classique pour ReFS/Dev Drive, FAT/exFAT, partages réseau et mode non-admin

**Cadrage terminé.**
