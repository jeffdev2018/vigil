# Mémoire agent — fait jamais vu en éval — 7 septembre 2026

Ferme le trou « faits jamais vus en éval » laissé après [DEV-3/DEV-4](agent-memory-independent-2026-09-07.md).

## Protocole

1. Mémoire pending avec **3** tokens opaques : `moss-2` (color chip), `pine-8` (badge seal), `fern-1` (workdir alias).
2. Éval connectée **uniquement** sur `moss-2` (replay) + `pine-8` (holdout) → **0/2 → 2/2, `eligible=true`**.
3. Adopt → révision **3** active (`9595267c-…`).
4. **DEV-5** demande **seulement** `fern-1` — **absent** de la suite d’éval.

## Résultat

| Étape | Détail |
| --- | --- |
| Éval `cc1a5a80-…` | eligible ; baseline 0, candidate 2 |
| Adopt | active rev 3 |
| DEV-5 | commentaire agent `fern-1` |
| Task | `memory_context.agent_status=loaded` |
| Usage | `started_runs=3`, `runs_with_agent_memory=3` (cumul agent) |

Env : vigil-482, agent Memory connected Claude pilot.

## Limites

- Toujours une famille synthétique Northline, pas un client.
- Ne prouve pas la réduction des reprises ni l’apprentissage projet autonome.
- Un seul fait « never-eval » exercé (pas une matrice large).

## Preuves

[evidence/agent-memory-unseen-fact-2026-09-07/](evidence/agent-memory-unseen-fact-2026-09-07/)
