# Qualité globale — suite séquentielle — 7 septembre 2026

Complète [quality-global-2026-09-07.md](quality-global-2026-09-07.md) : même checkout / env, **sans** `make test ∥ pnpm test`.

## Ordre exécuté

1. `pnpm typecheck`
2. `pnpm --filter @multica/core test`
3. `pnpm --filter @multica/views test`
4. `pnpm --filter @multica/web|desktop|docs test`
5. Retry ciblé des fichiers views en échec (`vitest --maxWorkers=2`, puis solo)
6. `make test` (seul ; un premier lancement concurrent a été tué)

Logs : `/tmp/vigil-quality-seq-*.log`

## Résultats

| Contrôle | Résultat |
| --- | --- |
| `pnpm typecheck` | **OK** 9/9 (~76 s) |
| `@multica/core` Vitest | **OK** 160 / 1756 |
| `@multica/views` Vitest (plein paquet) | **38 failed / 4863 passed** — presque tous `Test timed out in 5000ms` |
| Retry 17 fichiers views (`maxWorkers=2`) | **16/17 OK** ; 1 timeout (`issue-detail` source-context) |
| `issue-detail.test.tsx` solo `maxWorkers=1` | **OK** 66/66 |
| `@multica/web` | **OK** 32 fichiers / 266 tests |
| `@multica/desktop` | **OK** 55 / 621 |
| `@multica/docs` | **OK** 3 / 15 |
| `make test` | **FAIL** : `TestWebhookHandler_DedupeViaIdempotencyKey` (handler) |
| Même test webhook `-count=1` isolé | **PASS** |

## Lecture

- Le run **séquentiel par paquet** confirme typecheck + core/web/desktop/docs verts.
- Les échecs views / Go observés ici se comportent comme de la **contention / timeout**, pas comme des régressions stables : ils passent en rejeu isolé.
- La machine hébergeait d’autres `go test` (autres worktrees) pendant la passe — signal encore bruité hors de ce checkout.
- **Ne pas** traiter un `make test` ou un Vitest views « full » unique comme oracle unique sur cette machine chargée.

## Limites (inchangées)

- Pas de `pnpm build` / Playwright / mobile typecheck / Xcode sim.
- Pas de preuve que 418 fichiers views passent *en un seul process* sous charge concurrente hôte.

## Verdict ledger

Qualité globale : toujours **partiel** — suite séquentielle documentée ; oracle « full suite one-shot vert » **non** atteint sur cet hôte.
