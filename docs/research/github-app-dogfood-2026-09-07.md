# GitHub App — dogfood vigil-482 — 7 septembre 2026

## Constat

| Check | Résultat |
| --- | --- |
| `GET …/github/installations` | `configured: false`, `repository_browse_configured: false`, `installations: []` |
| Process API (pid sur :18562) | clés `GITHUB_APP_*` présentes mais **vides** |
| `.env` | `GITHUB_APP_SLUG` / `WEBHOOK_SECRET` / `APP_ID` vides ; `PRIVATE_KEY` tronquée (~73 chars, pas un PEM utilisable) |
| DEV-1 `pull_requests[]` | toujours `[]` |
| PR réelle | [jeffdev2018/multica-bugfix-pilot#1](https://github.com/jeffdev2018/multica-bugfix-pilot/pull/1) ouverte, titre/body citent DEV-1 |

## Ce qui bloque (honnête)

Créer / installer une GitHub App exige :

1. App ID + PEM + slug + webhook secret **non vides** dans l’env de l’API
2. Une installation GitHub sur le compte/repo (flux navigateur Settings → Connect GitHub)
3. Webhook joignable (`http://localhost:18562/api/github/webhook` en local — GitHub cloud ne peut pas pousser vers localhost sans tunnel)

Aucune de ces trois étapes n’est contournable sans secret d’App ou tunnel. Un faux insert SQL de PR **ne** compte **pas** comme intégration GitHub App.

## Manifeste préparé

`/tmp/vigil-gh-app-manifest.json` — App privée « Multica Dogfood vigil-482 », permissions read-only PR/checks/contents, webhook → API locale.

Prochaine action humaine courte : créer l’App via [manifest flow](https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest), coller les secrets dans `.env`, `make down && make up C=api`, Connect GitHub, puis re-déclencher un événement PR sur DEV-1 (ou sync).

## Verdict ledger

Recette bug→PR : toujours **partiel** — preuve GitHub URL inchangée ; `pull_requests[]` Multica toujours vide.
