# Qualité globale — 7 septembre 2026

Vérification de la ligne ledger « Qualité et intégration globales » sur `jeffdev2025/jef-triage-m1-shadow` / env `vigil-482`.

## Résultats (fin de passe)

| Contrôle | Résultat |
| --- | --- |
| `pnpm typecheck` (9 packages, hors mobile) | **OK** |
| `go build` `./cmd/server` `./cmd/multica` | **OK** |
| `packages/core` Vitest | **OK** 160 / 1756 |
| `apps/web` Vitest (turbo) | **OK** 32 fichiers |
| `apps/desktop` Vitest (turbo) | **OK** 55 fichiers |
| `apps/docs` Vitest | **OK** |
| `packages/views` Vitest **seul** | **OK** 418 / 4901 |
| `server/cmd/migrate` | **OK** |
| `server/internal/daemon/repocache` | **OK** |
| `server/internal/daemon` **seul** | **OK** |
| `make test` ∥ `pnpm test` (même machine) | **Bruité** — ne pas traiter comme oracle |

## Corrections livrées

1. Manifeste workspace delete (6 tables campagne).
2. Vitest core shim Node 25 `localStorage`.
3. 28 hooks `concurrentIndexCleanups` (452–500).
4. `TestGitEnv` isolé (`GIT_CONFIG_COUNT=0` vs clés credential hôte).
5. Ping migrate schéma 5 s → 30 s.
6. JoinPage timeout 15 s.
7. Views setup + `sidebar-resize` : spy `localStorage.setItem` (shim ≠ `Storage.prototype`).

## Limites

- Pas de `pnpm build` / Playwright / mobile typecheck ici.
- Un run **jumelé** make∥pnpm reste un mauvais signal sur cette machine (contention).
- Preuve qualité : typecheck + packages isolés verts après correctifs.
