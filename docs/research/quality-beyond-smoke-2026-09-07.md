# Qualité suite — 7 septembre 2026 (soir 2)

## Push

Branche `jeffdev2025/jef-triage-m1-shadow` poussée (`f130d4c50..55bdb4955`).

## `@multica/views` vitest

| | |
| --- | --- |
| Résultat | **1 failed / 417 files**, **1 failed / 4901 tests** |
| Échec | `layout/sidebar-resize.test.tsx` — `setItem` attendu 1×, reçu 0× |
| Solo re-run | voir log `/tmp/vigil-sidebar-resize.log` |
| Log full | `/tmp/vigil-seq-views-test.log` |

## Playwright au-delà du smoke

| Lot | Résultat |
| --- | --- |
| Smoke auth+nav+onboarding | déjà **10/10** plus tôt |
| `settings` + `issues` + `comments` | **11 failed** — timeout `waitForIssuesPage` après login (`/tmp/vigil-seq-pw-beyond.log`) |
| Lot léger (auth-callback / property-icons / quick-actions) | **4 failed** — timeouts UI post-auth (`/tmp/vigil-seq-pw-beyond2.log`) |

Hypothèse : parcours post-login issues fragile sur cette config IPv4/CORS ; pas une régression ownership web (login smoke OK).

## `dev-env.test.sh`

Flake destroy réparé : ambient `DESKTOP_USER_DATA_DIR` du dogfood empoisonnait `load_manifest`. Suite **PASS** avec ambient vigil-482 exporté.
