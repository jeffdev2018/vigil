# Omnigent — audit de pertinence pour Multica

**Décision : référence uniquement ; ne pas absorber comme plan de contrôle.** Omnigent est un méta-harness Python (sessions, politiques, sandboxes distantes, UI multi-device) qui chevauche largement le daemon Multica et ses backends Claude/Codex/Cursor. Aucune dépendance ajoutée, aucun code amont incorporé, aucun service Omnigent démarré par cet audit.

Consultation du 6 septembre 2026. Dépôt cloné en `/tmp/omnigent` depuis [omnigent-ai/omnigent](https://github.com/omnigent-ai/omnigent.git), figé sur le commit `381bf638fb31e6a51990d9dab54ea9ef4b933711` (2026-09-06). Les verdicts portent sur l’adoption **dans Multica**, pas sur la qualité générale du projet. Ce document complète l’[audit des huit candidats](integration-candidates-2026-09-05.md) et le [registre d’implémentation](implementation-goal-ledger.md), sans les remplacer.

## Provenance

| Champ | Valeur observée |
|---|---|
| Remote | `https://github.com/omnigent-ai/omnigent.git` |
| Commit | `381bf638fb31e6a51990d9dab54ea9ef4b933711` |
| Date du commit | 2026-09-06 17:39:45 +0200 |
| Version dans le code | `0.13.0.dev0` (`pyproject.toml`, `omnigent/version.py`) |
| Dernière release GitHub lue | `v0.12.0` (2026-09-01), non prerelease |
| Licence racine | Apache-2.0 (`LICENSE`) |
| NOTICE | Copyright (2026) Databricks, Inc. ; composants tiers listés |
| Statut déclaré | badge README « alpha » ; classifier PyPI `Development Status :: 3 - Alpha` |
| Signaux GitHub (API, instantané) | ~9744 stars, ~1517 forks, ~1333 issues ouvertes, non archivé, `pushed_at` 2026-09-06 |
| Langage / stack | Python ≥3.12 (cœur FastAPI/Starlette, SQLAlchemy, Alembic) ; front Vite/React (`web/`) ; Electron + shells mobiles sous `web/` ; SDKs Python/UI sous `sdks/` |
| Tests observés | ~1681 fichiers `tests/**/*.py` ; suites `e2e`, `e2e_ui`, `integration`, `host`, politiques, sandboxes |
| OpenAPI | `openapi.json` — ~82 paths, titre « Omnigent Server 0.1.0 » |

Les stars et le volume d’issues ne suffisent pas à qualifier la maturité produit pour Multica. Signaux structurels retenus : monorepo actif, releases `v0.8`→`v0.12` en août–septembre 2026, CHANGELOG généré, CI avec gate de scan sécurité sur PRs (`SECURITY.md`), mais version de développement `0.13.0.dev0` et statut alpha explicite.

### Empreintes SHA-256 des sources primaires lues

| Fichier | SHA-256 |
|---|---|
| `README.md` | `3ac51d089d922697a426cae758f8abc3d9b0bcb4333886f12e8ec7b048a5ff0b` |
| `LICENSE` | `a6cba85bc92e0cff7a450b1d873c0eaa2e9fc96bf472df0247a26bec77bf3ff9` |
| `NOTICE` | `dcaca71244cbebf5af46da7e36a50c442b27a7bfd6e2dce23a814dbd2ebc17de` |
| `docs/POLICIES.md` | `ae33b8eba22a467d849113c027b52a3896b93093fdfa7a15ea1abd995ef4f507` |
| `docs/AGENT_YAML_SPEC.md` | `f53c3aaf983a541e7dfbcd6be8022eecaade58b5fd83dbe0720bcd444d4d397e` |
| `SECURITY.md` | `8ed33186d60fe5842907de25948206743e6abbb78e968dd6dc4233f87a8243ad` |
| `pyproject.toml` | `50e199eb8afb1b6b37aac7948c90dfee384c23807c33254085e4ed90c1d316e7` |
| `omnigent/version.py` | `7cdbea0932a0757dc5e9852e15fad2b775efbec4b2b8490b908f4ae744cc0bfd` |
| `omnigent/runtime/README.md` | `487dd833a4c9903f9c7908d35547a387ae10319538eb0e230701810cc3330661` |
| `omnigent/host/local_server.py` | `450362c507c64b7079c54a5e75e08aefc25ee1adb0a06a98b6e08e6c550ee1b9` |
| `omnigent/cli_sandbox.py` | `290b0575251934a84ec4efc953d82fbf777f312c43763ca25d06021aac925e20` |
| `omnigent/onboarding/secrets.py` | `8373bc2a551e74c6ede53b3d17ed40a59f478a5d8d78623d0c2e3f606459e0f3` |
| `omnigent/inner/bwrap_sandbox.py` | `0940a45edadf7c3e6cb5f7ec49aaba1cf2b9a7b6983868cd91eff418ec883dea` |
| `omnigent/policies/builtins/safety.py` | `a87f9818ecc27a5c57eded584f710d81a2152f4d42eaf36b3b15f865c7a0a216` |
| `omnigent/onboarding/sandboxes/registry.py` | `01b1659442d9a1481fccaf63361637242283820473db812c14e6ed244b3efe59` |
| `AGENTS.md` | `629f4b8a5685e249d193464beac83c31472b5e95ea1924126239f1fb8bd70945` |

Aucun probe de restart/resume, d’intrusion sandbox ni d’appel modèle n’a été exécuté.

## Ce qu’Omnigent est réellement

D’après le README et le code, Omnigent se présente comme un **méta-harness open-source** : couche d’orchestration commune au-dessus de Claude Code, Codex, Cursor, OpenCode, Hermes, Pi et d’agents YAML custom. Le cœur n’est pas un gestionnaire de tickets d’équipe ; c’est un **serveur de sessions agent** avec CLI, UI web/desktop/mobile, politiques, et lancement de hosts dans des sandboxes cloud.

Capacités **vérifiées dans les sources** (pas inventées) :

1. **Runtime de session** — `omnigent/runtime/README.md` : boucle LLM/outils/skills ; « library, not a service », hébergée surtout par le serveur.
2. **Host / daemon local** — `omnigent/host/local_server.py` : serveur local détaché (pidfile, `~/.omnigent` ou `OMNIGENT_DATA_DIR`), réutilisé entre invocations CLI.
3. **Harness adapters** — nombreux modules `*_native*.py` / `inner/*_harness.py` (Claude, Codex, Cursor, Antigravity, Hermes, Goose, etc.) ; YAML `executor.harness` documenté dans `docs/AGENT_YAML_SPEC.md`.
4. **Politiques ALLOW / DENY / ASK** — `docs/POLICIES.md` ; builtins coût, shell, outils natifs dans `omnigent/policies/builtins/` ; évaluation CEL (`cel-python` dans les deps). Niveaux server / agent / session.
5. **Sandboxes distantes** — `omnigent/cli_sandbox.py` + registry `omnigent/onboarding/sandboxes/` (Modal, Daytona, E2B, Blaxel, Islo, Kubernetes, Boxlite, microsandbox, OpenShell, CoreWeave/cwsandbox, etc.) via extras PyPI.
6. **Sandbox locale Linux** — `omnigent/inner/bwrap_sandbox.py` : bubblewrap + seccomp documentés (montages RO, déni de namespaces, allowlist sockets).
7. **Secrets locaux** — `omnigent/onboarding/secrets.py` : keyring OS ou fichier `0600` `secrets.json` ; refs `keychain:<name>`.
8. **Client API** — `sdks/python-client` (HTTP + SSE, sessions/tours) ; surface OpenAPI large (~82 paths) centrée sessions/politiques/approvals.
9. **Télémetrie** — activée par défaut (README), opt-out documenté côté amont.

Ce que le marketing promet et que **cet audit ne valide pas** : isolation « enterprise » multi-tenant, non-répétition d’effets externes, continuité de mission Multica (issue → claim → livraison → revue), ni performance de sandbox. Le README liste beaucoup de fournisseurs de sandbox ; la présence d’un launcher dans le registry n’équivaut pas à une preuve d’isolation mesurée ici.

## Maturité (au-delà des stars)

| Signal | Observation |
|---|---|
| Structure | Monorepo dense (`omnigent/` ~760 `.py`, `web/`, `tests/`, `deploy/`, `docs/`, `examples/`) |
| Releases | Chaîne récente `v0.8`–`v0.12` ; trunk en `0.13.0.dev0` |
| Tests | Volume élevé et e2e présents ; non exécutés dans cet audit |
| Alpha | Badge + classifier PyPI : produit encore en alpha |
| Ownership | NOTICE Databricks ; extras et docs Databricks présents — signal d’écosystème, pas une obligation d’intégration Multica |
| Surface | OpenAPI « 0.1.0 » alors que le paquet est `0.12`/`0.13.dev` : contrat API encore jeune relatif au packaging |

Conclusion maturité pour Multica : **code vivant et sérieux, pas abandonné**, mais **trop jeune et trop large** pour en faire une dépendance de contrôle ou un fork de runtime.

## Chevauchements avec Multica

| Domaine | Omnigent (sources) | Multica déjà (sources locales) | Collision |
|---|---|---|---|
| Orchestration d’agents CLI | Host + harness wrappers + sessions | Daemon claim/heartbeat/runTask ; backends Claude/Codex (`server/pkg/agent/`, `server/internal/daemon/`) | **Forte** — second plan de contrôle |
| Providers Claude/Codex/Cursor | Harness natifs + SDK | `buildClaudeArgs`, `codexBackend.executeOnce`, Cursor MCP/approvals dans `execenv` | **Forte** |
| Sandbox / isolation | bwrap local ; Modal/Daytona/E2B… | Politique Codex workspace-write / danger-full-access ; isolation souvent « boundary du daemon » (`codex_sandbox.go`) | Partielle — idées utiles, runtime différent |
| Politiques / approvals | ALLOW/DENY/ASK, CEL, elicitation API | Inbox decisions, delivery review humaine, MCP approvals Cursor, `bypassPermissions` Claude en mode daemon | Partielle — vocabulaire intéressant, produit distinct |
| Multi-agent | Sub-agents YAML / child sessions | Assignees polymorphes, tasks, multi-agent Codex géré/désactivé (`codex_multi_agent.go`) | Partielle |
| Collab temps réel | Sessions partagées, UI multi-device (README) | WebSocket + React Query ; produit issues/inbox | Faible chevauchement produit ; UI sessions ≠ issues |
| Secrets / outils | keyring local ; MCP dans agent YAML | Agent Vault (audit précédent), Composio, plugins Multica | Ne pas superposer |
| Mémoire / skills / livraison | Skills runtime mentionnés ; pas de revue de livraison issue | Skills builtin, mémoire, `ReviewIssueDelivery`, décisions inbox | Multica plus avancé sur le JTBD « tâche acceptée » |

**Ce que Multica couvre déjà — ne pas dupliquer :**

- Identité workspace, membership, `X-Workspace-ID`, assignees agent/membre.
- Daemon durable : claim, préparation `execenv`, exécution provider, résultats.
- Issues, commentaires déclencheurs, revue de livraison avec snapshot/token.
- Inbox décisions, Composio, plugins, skills.
- Clients web/desktop/mobile sur le même modèle produit.

Absorber Omnigent entiers reviendrait à introduire un **deuxième serveur de sessions**, une **deuxième base** (`~/.omnigent` / SQLAlchemy), une **deuxième UI**, et une **deuxième file d’approvals** — sans gain clair pour le JTBD Multica (mission corrigée et acceptée).

## Cas d’usage concrets retenus (uniquement)

Verdict d’intégration produit : **`REFERENCE_ONLY`**. Trois pistes **réelles**, bornées, sans absorber le contrôle :

### 1. Vocabulaire et empilement de politiques (référence de conception)

Reprendre **comme méthode**, pas comme bibliothèque Python : verdicts ALLOW/DENY/ASK, ordre session → agent → admin (`docs/POLICIES.md`), builtins « ask before shell/write » et plafond de coût (`omnigent/policies/builtins/safety.py`, `cost.py`).

**Utilité Multica :** enrichir la cartographie inbox décision / gate outil / budget run **dans le serveur Go**, avec reçus persistés déjà exigés par l’audit d’intégrations (arrêt avant effet, identité, expiration).

**Ne pas faire :** importer `cel-python`, exécuter des handlers `omnigent.policies.*`, ni déléguer l’approbation à un serveur Omnigent.

### 2. Catalogue de fournisseurs de sandbox distante (référence d’adaptateur)

Le registry (`omnigent/onboarding/sandboxes/registry.py`) et les launchers Modal/Daytona/E2B illustrent une frontière « provider name → launcher » avec extras optionnels.

**Utilité Multica :** si un pilote d’infra distante est ouvert (déjà envisagé via CubeSandbox dans l’audit du 5 septembre), comparer **leur** découpage d’extras/registry aux nôtres — puis brancher **un worker Multica** dans la sandbox, pas un host Omnigent.

**Ne pas faire :** `omnigent sandbox --provider …` comme path de production Multica (second host + second protocole de session).

### 3. Hooks natifs PreToolUse / DENY observable (référence harness)

Omnigent documente l’évaluation de politiques via hooks vendeur et un événement de stream quand un DENY natif a lieu (schémas OpenAPI / politiques natives dans `safety.py`). Multica lance déjà Claude avec `--permission-mode bypassPermissions` et gère des configs sandbox Codex.

**Utilité Multica :** si l’on veut un « ask before risky tool » **sans** quitter le daemon, s’inspirer du point d’injection hook + signal observable — implémenté dans `server/pkg/agent` / `execenv`, branché sur inbox décisions.

**Ne pas faire :** wrapper chaque CLI derrière le runner Omnigent.

### Exclu explicitement

- Intégration du serveur Omnigent comme runtime des agents Multica.
- Import du client Python `omnigent-client` dans le monorepo TS/Go.
- Remplacement de la collaboration issues/inbox par des sessions chat Omnigent.
- Fork du monorepo Omnigent comme base produit.

## Critères de pilote (si une piste référence devient un essai)

Aucun pilote d’**intégration binaire** n’est recommandé aujourd’hui. Si l’équipe veut **prototyper la piste 1 (politiques)** en interne :

1. Un seul type d’action à risque (ex. shell ou écriture hors workdir) ; verdict ASK persisté avec acteur humain, expiration, refus = DENY.
2. Le daemon Multica reste le seul claim/heartbeat/result ; aucun processus `omnigent server` dans le chemin critique.
3. Tests négatifs : autre workspace, jeton expiré, double soumission, timeout sans auto-ALLOW.
4. Kill condition : besoin d’héberger Omnigent, divergence de session, ou double UI d’approbation.

Pour la piste 2 (sandbox cloud) : réutiliser les critères CubeSandbox déjà écrits (création/exec/destruction, quotas, nettoyage, isolation workspace, pas de fuite de jeton) — Omnigent n’est alors qu’une **lecture de design**, pas une dépendance.

## Drapeaux de risque

| Risque | Détail sourcé |
|---|---|
| **Double plan de contrôle** | Host + sessions Omnigent vs daemon + tasks Multica ; même familles de CLI |
| **Licence / notices** | Apache-2.0 permissive ; conserver NOTICE/attribution si un extrait était un jour copié. Examen des dépendances optionnelles (Modal, E2B, …) requis avant toute copie |
| **Sandbox claims** | bwrap + seccomp documentés dans le code ; fournisseurs cloud = launchers. Isolation réelle **non testée** ici. Ne pas citer le README comme preuve d’isolation Multica |
| **Credentials** | Keyring + fallback fichier `0600` ; variables `OMNIGENT_*` ; télémetrie on by default. Risque de confusion des stores de secrets si un utilisateur lance les deux stacks sur la même machine |
| **Alpha / drift API** | `0.13.0.dev0`, OpenAPI `0.1.0`, surface énorme (~82 paths) — coût de suivi élevé |
| **Stack mismatch** | Python/FastAPI vs daemon Go Multica — pas de frontière bibliothèque naturelle |
| **Charge opérationnelle** | ~1333 issues ouvertes : signal de vélocité et de backlog ; pas une métrique de stabilité pour Multica |
| **Databricks gravity** | Auteur NOTICE + extras Databricks : pertinent pour des déploiements Databricks, pas un argument d’absorption Multica |

## Recommandation finale

| Option | Verdict |
|---|---|
| Absorber / forker Omnigent comme runtime | **EXCLUDE** |
| Dépendance PyPI / sous-processus `omnigent server` | **EXCLUDE** |
| Adapter HTTP vers API Omnigent pour exécuter les issues Multica | **EXCLUDE** (second contrôle) |
| S’inspirer politiques / registry sandbox / hooks natifs | **REFERENCE_ONLY** |
| Priorité relative aux audits existants | Inférieure à Agent Vault et à la reprise correction→revue ; sandbox cloud : continuer la comparaison Cube (et éventuellement l’idée de registry Omnigent), pas le host Omnigent |

**Phrase de décision :** Multica reste l’orchestrateur des missions ; Omnigent est un projet voisin mature-en-alpha utile à **lire**, inutile à **absorber**.

## Vérification de cet audit

- Lecture seule sous `/tmp/omnigent` au commit ci-dessus ; empreintes listées.
- Contexte Multica via `graft ask` / `graft grep` sur daemon, delivery, Composio, sandbox Codex (aucune modification de code Multica).
- Aucun `pip install omnigent`, aucun serveur démarré, aucun test amont exécuté.
- Aucune exclusivité commerciale affirmée ; aucun prix inventé.
