# Qualité séquentielle — 7 septembre 2026 (soir)

Suite du smoke build/Playwright, après fix ownership `make up C=web`.

| Check | Résultat | Log |
| --- | --- | --- |
| `pnpm typecheck` | **9/9** OK (~66s) | `/tmp/vigil-seq-typecheck.log` |
| `@multica/core` vitest | **160 files / 1756 tests** OK (~6.5s) | `/tmp/vigil-seq-core-test.log` |
| `go test ./internal/handler/` | **OK** (~52s) | `/tmp/vigil-seq-go-handler.log` |
| Playwright smoke re-run | login spot-check **1/1** OK (~1.7s) ; suite 10/10 déjà prouvée plus tôt | `/tmp/vigil-seq-pw-one.log` + [quality-build-playwright-2026-09-07.md](quality-build-playwright-2026-09-07.md) |
| `scripts/dev-env.test.sh` | ownership helpers OK isolés ; suite complète encore fragile sur `destroy` retry / fake psql | — |

Env au moment des checks : vigil-482, commit `f209fef13` (ownership fix).
