# Latitude-lite : traces Multica → rejeu du même brief — 11 septembre 2026

Objectif kokpit : brancher la télémétrie run (tool calls, `failure_reason`, `wait_reason`) comme traces, cas d’usage « ce run a foiré → rejouer le même brief » — complète le Retry du journal d’exécution, ne le remplace pas.

**Décision PoC :** ne pas installer Latitude. Multica a déjà la boucle ops native ; documenter le mapping et le gap produit.

## Ce que Latitude apporte (job)

| Étape Latitude | Équivalent Multica déjà livré |
| --- | --- |
| Ingest traces prod | Audit + replay hash-chain (K70) + `run.mcp_tool_call` + usage |
| Signaler une panne | `failure_reason` / `wait_reason` sur `agent_task_queue` + past row |
| Dispatch fix | Commentaire / steer / décision (gates) — humain |
| Rejeu du même brief | `POST …/replay/simulate` (safe) · `POST …/replay/resume` · `rerunIssue` |

## Mapping télémétrie

| Signal Multica | Où | Rôle « Latitude » |
| --- | --- | --- |
| Messages / tools / effects | `GET /api/tasks/{id}/replay` | Trace ordonnée |
| MCP tool · class · result · gate | `GET /api/tasks/{id}/mcp-calls` + kind `mcp_call` | Trace outils gouvernés |
| Snapshot trust/effect/model/plan | `run.started` audit → `run.snapshot` | Contexte du brief |
| Seal head hash | `run.sealed` | Intégrité post-run |
| Failure | `failure_reason`, statut `failed` | Signal de panne |
| Retry | Execution log Retry → `rerunIssue` | Rejeu même issue/agent |
| Safe rejeu | Replay « Replay in safe mode » | Rejeu sans effets externes |

## Boucle ops recommandée (sans Latitude)

```text
failed past row
  → open Replay (scrub + MCP panel)
  → diagnose (mcp result / drift / failure_reason)
  → either Retry (same brief) or Resume from seq (new instruction)
  → optional Simulate (safe mode) first
```

## Gap vs Latitude plateform

- Pas de workspace Latitude / prompt registry / eval UI externe.
- Pas d’export OTLP/OpenInference vers un SaaS tiers (non-objectif ici).
- `memoryeval` reste le canon pour suites mémoire ; Scenario reste le PoC dialogue skill.

## Critère de done (cette note)

- Mapping documenté.
- UI list MCP + API `GET …/mcp-calls` livrés (voir ADR-lite).
- Pas de dépendance `latitude-llm` dans le monorepo.

## Non-objectifs

- Héberger Latitude self-hosted dans Multica.
- Remplacer `memoryeval` ou Scenario.
- Nodeterm / Claw (skip kokpit).
