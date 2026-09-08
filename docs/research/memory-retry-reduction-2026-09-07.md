# Réduction mesurée des reprises — mémoire projet — 7 septembre 2026

Proxy métier : si l’agent ne produit pas le token opaque attendu, une reprise humaine / re-ask est **nécessaire**. Ce n’est pas un comptage de `changes_requested` UI, mais le même signal « résultat inutilisable ».

## Vague 1 — `keel-4` (n=2+2)

| Bras | Mémoire projet | Issues | Attendu |
| --- | --- | --- | --- |
| Baseline | rev **3** — pas de règle dock / `keel-4` | DEV-7, DEV-8 | miss → correction_needed |
| Traitement | rev **4** — règle dock `keel-4` | DEV-9, DEV-10 | hit → pas de reprise |

| Métrique | Baseline | Traitement |
| --- | --- | --- |
| n | 2 | 2 |
| correction_needed | **2** (rate 1.0) | **0** (rate 0.0) |
| Relative reduction | — | **100 %** `(2−0)/2` |

Preuves : [evidence/memory-retry-reduction-2026-09-07/](evidence/memory-retry-reduction-2026-09-07/)

## Vague 2 — `mast-9` (n=4+4)

Nouveau token pour éviter la contamination `keel-4` déjà en mémoire.

| Bras | Mémoire projet | Issues | Attendu |
| --- | --- | --- | --- |
| Baseline | rev **4** — pas de règle warehouse / `mast-9` | DEV-11…DEV-14 | miss → correction_needed |
| Traitement | rev **5** — règle warehouse bay `mast-9` | DEV-15…DEV-18 | hit → pas de reprise |

Agent : Bugfix recipe pilot. Consigne identique (commentaire = bay code seul, pas d’outils).

| Métrique | Baseline | Traitement |
| --- | --- | --- |
| n | 4 | 4 |
| correction_needed | **4** (rate 1.0) | **0** (rate 0.0) |
| Relative reduction | — | **100 %** `(4−0)/4` |

Commentaires observés :

- Baseline DEV-11…14 : chacun a répondu `keel-4` (autre règle mémoire) — **miss** pour `mast-9`
- Treatment DEV-15…18 : `mast-9` exact

Preuves : [evidence/memory-retry-reduction-2026-09-07-wave2/](evidence/memory-retry-reduction-2026-09-07-wave2/)

## Cumul vagues 1+2

| Métrique | Baseline | Traitement |
| --- | --- | --- |
| n | **6** | **6** |
| correction_needed | **6** | **0** |
| Relative reduction | — | **100 %** |

## Limites

- Synthétique, une famille de consignes, un agent — **pas** une preuve commerciale ≥30 % sur équipes externes.
- Proxy « miss token ⇒ reprise » ; pas de boucle Accept/corriger chronométrée ici.
- Ne prouve pas l’apprentissage autonome (publish humain avant chaque bras traitement).
- Baseline vague 2 montre un faux-ami (`keel-4`) : sans la règle cible, l’agent invente / recycle une autre règle — toujours une reprise métier.
