# Évolution future — interop Agent2Agent (cross-app)

**Statut :** backlog stratégique / non planifié pour livraison immédiate  
**Date :** 11 septembre 2026  
**Contexte :** évaluation de [`a2aproject/a2a-cli`](https://github.com/a2aproject/a2a-cli) et du protocole ouvert [Agent2Agent (A2A)](https://a2a-protocol.org/latest/) pour Multica.

## Verdict

L’interop **agents ↔ agents entre applications** est une destination produit crédible et, à terme, plus que nécessaire. Ce n’est **pas** un chantier near-term : le CLI officiel A2A ne remplace rien dans la boucle Multica actuelle. Quand un partenaire / agent externe réel apparaîtra, Multica doit y aller en **gateway + gouvernance**, pas en adoption du CLI pour le principe.

## Homonymie critique

| Terme | Signification chez Multica aujourd’hui | Protocole Agent2Agent (a2aproject) |
| --- | --- | --- |
| « A2A » | Messaging **interne** agent→agent sur une issue (`POST …/agent-messages`, intents `question` / `review` / `handoff`, `a2a_depth`, budget/heure, originator) | Protocole ouvert HTTP (JSON-RPC / REST / gRPC) : agent card, send, tasks, streaming |
| Runtime | Daemon local qui spawn Claude / Codex / etc. | Agents découvrables via URL + card |
| Gouvernance | Workspace, membership, Access agent, depth/budget | Auth transport (mTLS, OIDC, tokens) + cards |

**Règle de naming :** garder (ou renommer progressivement) le A2A interne en termes produit (`agent-message`, `delegation`) pour éviter la collision avec le protocole Agent2Agent. Ne pas confondre les deux dans les issues / docs / skills.

## Ce que Multica a déjà (à conserver)

La couche métier interne reste le filet de sécurité à **mapper** sur tout appel cross-app :

- Endpoint dédié + intents fermés (`server/internal/service/a2a.go`, `handler/agent_message.go`)
- Circuit breakers : profondeur max (`MULTICA_A2A_MAX_DEPTH`, défaut 4) et budget/issue/fenêtre
- Jugement d’accès A2A par **originator** humain en tête de chaîne (pas par l’agent immédiat)
- Issues / comments / runs comme surface d’audit

Ces invariants ne disparaissent pas avec le protocole externe — ils deviennent la politique d’admission du gateway.

## Ce que Multica n’a pas (écarts)

- Pas d’**agent card** Agent2Agent exposée
- Pas de serveur / client protocole A2A (aucun usage de `a2aproject` dans le code)
- Runtime = processus locaux, pas endpoints HTTP d’agents tiers
- `a2a-cli` = client de smoke/debug uniquement (v0.2, écosystème encore jeune)

## Fit de `a2a-cli`

| Cas d’usage | Fit |
| --- | --- |
| Remplacer `multica` CLI / daemon / spawn providers | Non |
| Remplacer l’A2A interne Multica | Non |
| Smoke / debug d’agents externes conformes A2A | Oui (outil opérateur) |
| Intégration produit (outbound / inbound) | Non — préférer SDK Go (`a2a-go`) côté server/daemon |

## Destination produit

Multica comme **orchestrateur gouverné** :

1. Agents Multica (runtimes locaux) restent first-class dans le workspace.
2. Agents d’**autres apps** (HTTP + card A2A) peuvent être invoqués ou, plus tard, invoquer Multica.
3. Chaque hop cross-app traverse les mêmes gates : workspace, originator, depth, budget, audit sur l’issue.

Positionnement : pas « un client A2A de plus », mais le **board + gouvernance** au-dessus du protocole.

## Phases proposées (quand déclencher)

### Phase 0 — maintenant (sans dépendance protocole)

- Consolider l’A2A interne (breakers, naming clair).
- Surveiller la spec Agent2Agent / `a2a-cli` (encore v0.2).
- Ne pas ajouter `a2a-cli` comme dépendance produit.

### Phase 1 — déclencheur : 1er partenaire / agent externe utile

- Client outbound via SDK Go derrière les gates existants.
- Traduction : message A2A externe ↔ comment / task Multica (originator + depth conservés).
- `a2a-cli` uniquement pour smoke CI / support.

### Phase 2 — Multica adressable depuis l’extérieur

- Publier une agent card pour des agents / skills Multica choisis.
- Auth (OIDC / tokens workspace / mTLS) + allowlists.
- Même mapping audit / budget / originator.

### Phase 3 — écosystème

- Catalogue d’agents externes, marketplace / partners.
- Transport plugins si besoin (hors JSON-RPC/REST/gRPC built-in).

## Non-objectifs (explicitement hors scope immédiat)

- Réécrire le daemon pour parler uniquement A2A.
- Remplacer les providers CLI (Claude, Codex, …) par des agents HTTP.
- Dual-write ou compat shims « au cas où » sans partenaire concret.

## Critères de démarrage Phase 1

Tous doivent tenir :

1. Un agent externe **réel** (partenaire ou app tierce) avec card A2A stable.
2. Un parcours produit clair (ex. : agent Multica délègue une revue à un agent SaaS externe, résultat visible sur l’issue).
3. Décision d’auth / trust model documentée (qui fait confiance à qui).

## Références code (état actuel)

- `server/internal/service/a2a.go` — intents, depth, budget
- `server/internal/handler/agent_message.go` — endpoint agent→agent
- `server/internal/handler/a2a_breaker.go` — breakers
- Daemon / `pkg/agent` — spawn runtimes locaux (inchangé pour Phase 0)

## Références externes

- Protocole : https://a2a-protocol.org/latest/
- CLI : https://github.com/a2aproject/a2a-cli
- Spec CLI : dans le dépôt `a2a-cli` (`specification/SPEC.md`)

## Patterns produit connexes (kokpit)

Évals Scenario/Latitude, ADR « intent before tool », finish-first, Workspine, Superpowers :  
[kokpit-product-patterns-2026-09-11.md](kokpit-product-patterns-2026-09-11.md).

## Lien Linear

**Bloqué en session agent :** le MCP Linear (`plugin-linear-linear`) exige une authentification interactive dans Cursor Desktop (non disponible dans cet environnement). Corps d’issue prêt dans `/tmp/linear-a2a-future-issue.md`. Après auth Linear dans Cursor, relancer la création (titre suggéré ci-dessous) et coller l’URL ici.

**Titre suggéré :** `Future: cross-app Agent2Agent interop (governed gateway)`  
**Labels suggérés :** `evolution`, `agents` (si existants) · **State :** Backlog / Icebox · **Priority :** Low / None
