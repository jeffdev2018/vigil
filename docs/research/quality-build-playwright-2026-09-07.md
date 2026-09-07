# Qualité — build + Playwright — 7 septembre 2026

## `pnpm build`

**OK** — Tasks 5/5 successful (~7m35s). Logs: `/tmp/vigil-quality-build.log`.

## Playwright

Tentative smoke `auth` + `onboarding-smoke` + `navigation` contre `http://localhost:13482` :

- Web vigil-482 **non démarré** (`make up C=web` : port non owned / puis `make down` a stoppé api+daemon)
- Relance bloquée : **Docker Desktop unable to start** → pas de Postgres/API/web pour e2e
- 10 échecs Playwright = connexion refusée, **pas** des régressions produit prouvées

## Verdict

Build produit : vert. E2E : **non conclu** sur cet hôte tant que Docker/web ne sont pas stables.
