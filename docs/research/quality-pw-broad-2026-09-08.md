# Playwright suite élargie — 8 septembre 2026

Base : `http://127.0.0.1:13482` (Next `--hostname 127.0.0.1`).

| Lot | Résultat |
| --- | --- |
| auth + navigation + onboarding-smoke + settings + issues + comments + property-icons + quick-actions | **PASS** (inclus dans le total) |
| `issue-table` server grouping (3 tests) | **FAIL** — `Loaded 60 of 60` / grouping assertions |
| **Total run** | **24 passed / 3 failed** (~2.5m) |

`e2e/settings.spec.ts` mis à jour pour l’auto-save workspace (blur, plus de bouton Save) — **2/2 PASS**.

Log : `/tmp/pw-broad.log`.
