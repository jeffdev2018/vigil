# Réduction mesurée des reprises — mémoire projet — 7 septembre 2026

Proxy métier : si l’agent ne produit pas le token opaque attendu, une reprise humaine / re-ask est **nécessaire**. Ce n’est pas un comptage de `changes_requested` UI, mais le même signal « résultat inutilisable ».

## Protocole (vigil-482)

| Bras | Mémoire projet | Issues | Attendu |
| --- | --- | --- | --- |
| Baseline | rev **3** — pas de règle dock / `keel-4` | 2 | miss → correction_needed |
| Traitement | rev **4** — règle dock `keel-4` | 2 | hit → pas de reprise |

Agent : Bugfix recipe pilot. Consigne identique (commentaire = dock code seul, pas d’outils).

## Résultats

| Métrique | Baseline | Traitement |
| --- | --- | --- |
| n | 2 | 2 |
| correction_needed | **2** (rate 1.0) | **0** (rate 0.0) |
| Relative reduction | — | **100 %** `(2−0)/2` |

Commentaires observés :

- Baseline-1 : digression bugfix/PR (pas `keel-4`)
- Baseline-2 : UUID projet (pas `keel-4`)
- Treatment-1/2 : `keel-4` exact

## Limites

- Synthétique, n=2+2, une famille, un agent — **pas** une preuve commerciale ≥30 % sur équipes externes.
- Proxy « miss token ⇒ reprise » ; pas de boucle Accept/corriger chronométrée ici.
- Ne prouve pas l’apprentissage autonome (publish humain avant le bras traitement).

## Preuves

[evidence/memory-retry-reduction-2026-09-07/](evidence/memory-retry-reduction-2026-09-07/)
