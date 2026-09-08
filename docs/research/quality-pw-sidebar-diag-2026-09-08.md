# Qualité — diagnostic sidebar-resize + PW post-login — 8 septembre 2026

## `sidebar-resize` (views)

**Cause :** sous happy-dom, `vi.spyOn(localStorage, "setItem")` n’interceptait pas les écritures ; le produit committait bien (`--sidebar-width` → 300).  
**Fix :** spy sur `Storage.prototype.setItem` + assert `localStorage.getItem`.  
**Vérif :** 2/2 PASS.

## Playwright post-login (issues/settings)

**Symptôme :** `waitForIssuesPage` timeout ; `innerText` vide alors que le HTML RSC contient « New Issue ».  
**Cause :** `next dev` écoutait `*:13482` (IPv6). Playwright sur `http://127.0.0.1:13482` → HMR WS `ERR_INVALID_HTTP_RESPONSE` → hydratation client bloquée.  
**Repro fix :** `next dev --hostname 127.0.0.1` → `e2e/issues.spec.ts:47` **PASS** (~15s).  
**Produit :** `apps/web` `dev` utilise désormais `--hostname ${FRONTEND_HOSTNAME:-127.0.0.1}` (Docker/self-host : `FRONTEND_HOSTNAME=0.0.0.0`).

Smoke auth (page `/login` seule) restait vert car pas d’hydratation dashboard.
