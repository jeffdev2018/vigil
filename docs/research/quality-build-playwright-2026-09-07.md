# Qualité — build + Playwright — 7–8 septembre 2026

## `pnpm build`

**OK** — Tasks 5/5 (~7m35s). `/tmp/vigil-quality-build.log`.

## Playwright smoke (auth + navigation + onboarding)

Après restauration Docker :

| Lot | Résultat |
| --- | --- |
| `e2e/auth.spec.ts` | **4/4 passed** (~15s) |
| `e2e/navigation.spec.ts` + `onboarding-smoke.spec.ts` | **6/6 passed** (~50s) |

Base URL : `http://127.0.0.1:13482`. API : `http://127.0.0.1:18562`.

### Conditions locales nécessaires

1. **IPv4** — `localhost` → hang Chromium/`page.goto` sur cette machine ; binder Next `--hostname 127.0.0.1`.
2. **CORS** — `FRONTEND_ORIGIN=http://localhost:13482` seul refuse l’Origin `http://127.0.0.1:13482`. Dogfood `.env` : `CORS_ALLOWED_ORIGINS=http://localhost:13482,http://127.0.0.1:13482` (+ `NEXT_PUBLIC_API_URL`/`WS` en `127.0.0.1`).
3. **`make up C=web`** reste fragile (ownership :13482) — web démarré manuellement `next dev --port 13482 --hostname 127.0.0.1`.

Logs : `/tmp/vigil-pw-auth-final.log`, `/tmp/vigil-pw-rest2.log`.

## Verdict

Build + smoke Playwright **10/10** sous config IPv4/CORS dogfood. Pas un oracle CI one-shot via `make up C=web` seul.
