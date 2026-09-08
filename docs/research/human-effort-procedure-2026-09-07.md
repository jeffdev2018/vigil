# Comparaison de procédures — human_effort vs review_delay — 7 septembre 2026

Instrument : `human_effort_seconds` (timer client) ≠ `review_delay_seconds` (horloge murale fin de run → revue).

## Protocole

Même critère sur deux issues du pilote mémoire `mast-9` :

> Agent comment equals exactly the token mast-9 (no other text).

| Procédure | Issue | Résultat agent | Décision humaine |
| --- | --- | --- | --- |
| A — miss mémoire | DEV-11 | `keel-4` | `changes_requested` |
| B — hit mémoire | DEV-15 | `mast-9` | `accepted` |

Revue humaine chronométrée via API (relecture livraison + commentaires + décision), avec pauses actives plus longues sur A.

## Résultats

| Métrique | A (miss) | B (hit) |
| --- | --- | --- |
| `human_effort_seconds` | **4** | **1** |
| `review_delay_seconds` | **874** | **512** |
| Coût snapshot (`available_usd`, estimated) | ~0.24 | ~0.18 |

Delta effort : **+3 s** sur la procédure miss (correction).  
`review_delay` est plus grand sur A aussi, mais pour une raison **non procédurale** (runs plus anciens) — il ne mesure pas le travail de revue.

## Lecture

- L’instrument distingue bien effort actif et attente murale.
- Sur ce couple, la procédure « hit mémoire → Accept » demande moins d’effort chronométré que « miss → corriger ».
- **n=1 paire**, dogfood fondateur — pas une preuve commerciale de réduction du temps de revue.

## Preuves

[evidence/human-effort-procedure-2026-09-07/](evidence/human-effort-procedure-2026-09-07/)
