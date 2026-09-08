# Revue globale multi-issues — livraison + effort — 7 septembre 2026

Batch dogfood sur **14** issues `DEV-*` avec run completed et pas encore de revue.

## Protocole

Pour chaque issue : critère « commentaire agent = token exact attendu », puis Accept ou `changes_requested` avec `human_effort_seconds` chronométré.

| Décision | Issues | Tokens |
| --- | --- | --- |
| accepted (9) | DEV-3,4,5,6,9,10,16,17,18 | harbor-17, NL-ADD-9, fern-1, quay-77, keel-4×2, mast-9×3 |
| changes_requested (5) | DEV-7,8,12,13,14 | miss vs keel-4 / mast-9 |

## Effort

| | Accept | Correction |
| --- | --- | --- |
| n | 9 | 5 |
| `human_effort_seconds` avg | **1.0** | **2.0** |

Cohérent avec la paire A/B antérieure : corriger coûte plus d’effort actif que accepter un hit.

## Preuves

[evidence/delivery-review-batch-2026-09-07/](evidence/delivery-review-batch-2026-09-07/)
