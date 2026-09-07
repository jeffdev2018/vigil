# Mémoire agent — validation sur cas indépendants — 7 septembre 2026

Preuve que une mémoire **adoptée** (après éval connectée éligible) est préparée dans un **run d’issue réel**, hors worker d’évaluation mémoire.

## Chaîne

1. Mémoire `fc3905fd-…` éligible (Northline sim tent. 3 : 0/2→2/2) puis **adopt** → révision **4** active.
2. **DEV-3** — question = token de replay d’éval (`harbor-17`), surface = issue assignée à l’agent.
3. **DEV-4** — question = token de holdout d’éval (`NL-ADD-9`), surface = issue.

## Résultats

| Issue | Attendu | Commentaire agent | `memory_context` |
| --- | --- | --- | --- |
| DEV-3 `01a07de1-…` | `harbor-17` | `harbor-17` | `agent_status=loaded`, versions incl. `fc3905fd` rev **4** |
| DEV-4 | `NL-ADD-9` | `NL-ADD-9` | chargé (voir evidence) |

Usage agent (fenêtre 30 j) : `started_runs=2`, `runs_with_agent_memory=2`, `prepared_runs` pour `fc3905fd` rev 4.

Env : vigil-482, agent **Memory connected Claude pilot** `5711f250-…`, runtime Claude `0c344870-…`.

## Limites

- Tokens déjà vus en éval (replay/holdout) — l’indépendance est la **surface produit** (issue run), pas un 3ᵉ fait jamais évalué.
- Issue peut rester `todo` si l’agent commente sans changer le statut.
- Pas une preuve de réduction des reprises métier ni d’apprentissage autonome projet.
- Northline reste une famille de simulation ; pas un client payant.

## Preuves

[evidence/agent-memory-independent-2026-09-07/](evidence/agent-memory-independent-2026-09-07/)
