# Benchmarks du moteur A (MFT brute)

Mesures sur un volume NTFS **synthétique** généré à la volée (`ntfstest.Synthetic` :
1 dossier pour 20 enregistrements, noms de 19 caractères), donc sans coût d'I/O disque.
Elles mesurent le pipeline lecteur → parseurs × N → fusion → `Finalize` (agrégation).

```
go test ./internal/mftscan -run '^$' -bench Scan -benchtime 1x
```

| Entrées | Temps (2 cœurs ARM64, devbox) | Octets/entrée (noms compris) | Cycles GC |
|---|---|---|---|
| 1 M  | 0,41 s | 63,9 | 6 |
| 5 M  | 2,36 s | 63,0 | 7 |
| 10 M | 5,13 s | 63,0 | 7 |

Objectifs du cadrage : 1 M < 3 s, 10 M < 30 s, ≤ 64 octets/entrée.

## Répartition mémoire par entrée

| Champ | Octets |
|---|---|
| `Parent` | 4 |
| `Size`, `Alloc` | 16 |
| `MTime` (secondes Unix, uint32) | 4 |
| `Attr` (attributs Windows + drapeaux Cadastre) | 4 |
| `NameOff`, `NameLen` | 6 |
| `Seq` (numéro de séquence MFT, revalidation) | 2 |
| Enfants en CSR (`ChildStart`, `Children`) | 8 |
| **Fixe** | **44** |
| Nom (UTF-8/WTF-8, arène sans marge) | longueur du nom |

Sur un vrai volume système, la longueur moyenne des noms (WinSxS en particulier) est plutôt
de 20 à 30 caractères : compter 65 à 75 octets/entrée. Les extensions ne sont pas stockées :
elles sont calculées à la demande (en parallèle, puis mises en cache).

Sur un vrai disque, le temps est dominé par la lecture séquentielle de la `$MFT`
(≈ 1 Go pour 1 M de fichiers) : 1 à 2 s sur SSD NVMe, 5 à 10 s sur HDD.
À mesurer sur le PC Windows avec `cadastre scan C: --engine mft`.
