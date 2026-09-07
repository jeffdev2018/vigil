# Revue honnête / autorisation d’action — 7 septembre 2026

## Contrat figé

| Soft signal (pas une autorisation) | Hard gate Multica (refus réel) |
| --- | --- |
| `board_status_in_review` / `done` | `workspace_membership` |
| `delivery_acceptance` / `changes_requested` | `private_agent_invoke` |
| `coding_tool_approval_prompts` (bypass) | `run_scoped_multica_token` |
| | `delivery_review_cas` |
| | `decision_destinee_only` |
| | `cli_auth_operator_only` |
| | `memory_evaluation_human_adopt` |

Registre code : `packages/core/issues/authorization-frontiers.ts`.
UI livraison déjà honnête : `delivery.honesty`, `board_status_note`, `review_hint`, `propose_done_hint`.
Docs : section « Review vs action authorization » dans `security-model.{mdx,zh,ja,ko}.mdx` ; issues.mdx disait déjà la même chose pour `in_review`.

`in_review` conserve des effets Multica uniquement (`finalize_autopilot_run`, `archive_run_failure_notifications`) — jamais merge/deploy.

## Preuves

- Vitest `authorization-frontiers.test.ts` (registre disjoint, ids inconnus → unknown).
- Merge/deploy restent `unknown` : Multica ne les revendique pas comme gates.

## Limites

Les tests d’intégration déjà présents (MUL-4525 invoke, CAS livraison, décisions) ne sont pas rejoués dans cette note. Un bloqueur merge/deploy réel doit vivre hors Multica (VCS, credentials daemon, policy externe).
