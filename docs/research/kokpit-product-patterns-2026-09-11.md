# Patterns kokpit → Multica — 11 septembre 2026

Sélection Jeff : évals Scenario/Latitude, ADR Uber « intent before tool », Mythos finish-first, Workspine, Superpowers, Nodeterm/FastClaw/Claw.

Sources kokpit (`~/Downloads/kokpit/data.js`) + code Multica actuel.  
Voir aussi : [future-agent2agent-interop-2026-09-11.md](future-agent2agent-interop-2026-09-11.md).

---

## 1. Évals agents : Scenario vs Latitude

| | [langwatch/scenario](https://github.com/langwatch/scenario) (~960★) | [latitude-dev/latitude-llm](https://github.com/latitude-dev/latitude-llm) (~4.6k★) |
| --- | --- | --- |
| Kokpit | Tester **10** | Tester **10** |
| Job | Simulation multi-tour + assertions / jury | Traces → signal de panne → dispatch fix → **rejeu** |
| Langages | Python, TS, **Go** | TypeScript (plateforme) |
| Fit Multica | Tester une **skill / mémoire** comme un agent | Boucle ops sur runs réels (proche journal d’exécution) |

### Ce que Multica a déjà

- Package `server/internal/memoryeval` : suites replay/holdout, baseline vs candidate, workers offline + `ConnectedProtocol` / `multica_runtime_v1`.
- Daemon : `memory_evaluation.go` + capability `memory-evaluation-v1`.
- API : create/list memory evaluations, claim/report côté runtime.
- Pas d’équivalent générique « skill eval » hors mémoire ; les tests skills restent ad hoc / builtin_skills_test.

### Recommandation

1. **Scenario d’abord** pour une skill Multica (ex. `multica-working-on-issues` ou une skill dogfood) :
   - Adapter Scenario `call()` → spawn via daemon / CLI profile de test (pas le LLM live de prod).
   - Assertions = critères d’acceptance déjà produit (issue ouverte, commentaire, status).
   - Garder `memoryeval` comme **canon** pour la mémoire ; Scenario = couche simulation conversationnelle **à côté**, pas un remplacement.
2. **Latitude ensuite** (ou en parallèle ops) :
   - Brancher la télémétrie run (tool calls, failure_reason, wait_reason) comme traces.
   - Cas d’usage : « ce run a foiré → rejouer le même brief » — complète le Retry journal d’exécution, ne le remplace pas.
3. **Ne pas** fusionner Scenario et `memoryeval` en un seul moteur avant un PoC isolé.

### PoC minimal (1–2 jours)

```text
Scenario suite
  → agent under test = Multica agent + skill X (runtime fake ou connected smoke)
  → 3 scénarios : happy path, refus Access, hop A2A refusé (depth)
  → assertions sur API Multica (issue/comment/task status)
Comparer coût + flakiness vs memoryeval connected actuel.
```

Verdict attendu : Scenario gagne si on veut des **dialogues** ; Latitude si on veut des **traces prod → fix**. Pour « skill Multica », Scenario est le meilleur premier pas.

---

## 2. Uber ADR — audit « intent before tool » sur le daemon

| | [uber/ADR](https://github.com/uber/ADR) (~1.5k★) |
| --- | --- |
| Kokpit | Publier **9** |
| Job | Observabilité + benchmark d’attaques + détecteur à 2 niveaux ; **prévention non publiée** |
| Fit | Inspiration politique / télémétrie, **pas** dépendance Python dans le daemon |

### Ce que Multica a déjà (proche)

`server/internal/daemon/mcp_gateway.go` — avant l’appel MCP :

1. `classify` → `ClassNever` / `ClassAsk` / allow  
2. `gate.AskWithID(..., "mcp_tool_call", …)` avec `params` scannés secrets  
3. `reportCall` (server, tool, risk, class, result, duration)  
4. Substitution secrets run-scoped **après** approval  

Hooks shell (`shell_hook_test.go`) + `permissionprofile` pour commandes.  
Remote MCP : tools pinnés / approved schema digest.

### Écart ADR

ADR insiste sur : **intention + outil + trace observables avant** de parler « détection ».  
Multica gate déjà les classes Ask/Never, mais :

- Pas de modèle d’« intent » explicite (pourquoi l’agent appelle cet outil) séparé du nom d’outil.
- Pas de benchmark d’attaques agent publié dans le repo.
- Les backends coding CLI (Claude/Codex ACP) ont leurs propres bypass permissions — hors gateway MCP.

### Recommandation (inspiration, pas install)

1. **Étendre le rapport MCP** (et idéalement tool events ACP) avec un champ `intent_summary` optionnel quand le provider l’expose — sinon dériver depuis le dernier message agent (best-effort, documenté comme soft).
2. **Conserver** Never/Ask comme hard gates ; ADR ne remplace pas `ClassNever`.
3. **Suite de fixtures** « tool abuse » (style ADR benchmark) en Go contre `mcp_gateway` + remote MCP pins — dans `daemon/*_test.go`, pas Python Uber.
4. Ne pas importer le détecteur ML d’ADR ; réutiliser risk class + secretscan existants.

### PoC minimal

```text
1. Documenter la matrice actuelle Never/Ask/Allow par tool class (déjà mcpgov).
2. Ajouter un test table « intent-like » : même tool name, params hostiles → refuse (paths, secrets).
3. Export optionnel des mcpCallReport vers une vue run (UI) : tool · class · result · gate_id.
```

---

## 3. Mythos finish-first

| | [Magma1321/mythos-agent-pipe](https://github.com/Magma1321/mythos-agent-pipe) |
| --- | --- |
| Kokpit | Explorer |
| Idée | Clôturer / valider une sous-tâche à 100 % avant d’en ouvrir une autre |

### Mapping Multica

Déjà partiel :

- A2A depth + budget (`service/a2a.go`) = plafond de hops, pas finish-first.
- Acceptance criteria + delivery review = validation humaine **après** le run.
- Squads / sous-issues = décomposition, sans obligation « finish before fan-out ».

### Produit possible

- **Policy agent / squad** : `finish_first=true` → l’agent ne peut pas `@` / agent-message tant que l’issue courante n’a pas de delivery acceptée ou status terminal autorisé.
- Ou soft : skill builtin qui impose le protocole (comme Superpowers) sans hard gate serveur.

Priorité : **skill + docs** d’abord ; hard gate seulement si dogfood le demande.

---

## 4. Workspine — plans / preuves dans Git

| | [PatrickSys/workspine](https://github.com/PatrickSys/workspine) |
| --- | --- |
| Kokpit | Explorer |
| Idée | Plans, statuts, vérifs **dans le repo**, pas seulement le chat |

### Mapping Multica

- Project resources + worktrees / `in_place` : isolation Git déjà là.
- Delivery evidence + PR links : preuves côté Multica, pas forcément fichiers `.workspine/` dans le repo.
- Brain / mémoire agent : hors Git.

### Produit possible

- Convention skill : écrire `docs/agent-plan/<issue-id>.md` (ou sous `.multica/`) avant code ; citer le path dans le commentaire de run.
- Option UI : « preuves repo » = fichiers listés dans le workdir + hash — sans adopter le framework Workspine.

Ne pas forker Workspine ; **voler la convention fichier**.

---

## 5. obra/superpowers — skills = protocole obligatoire

| | [obra/superpowers](https://github.com/obra/superpowers) |
| --- | --- |
| Kokpit | Tester **9** |
| Idée | Chaîne imposée : design → worktree → plan → subagents revus → TDD → revue bloquante |

### Mapping Multica

- Builtin skills + agent skill bindings.
- Delivery review / acceptance = revue humaine bloquante côté board.
- Pas de skill qui **bloque** le merge VCS (hors Multica — honest authz).

### Produit possible

1. Pack skill Multica « working-on-issues protocol » (déjà partiel) renforcé façon Superpowers : étapes nommées + checklists.
2. Autopilot / business rules : exiger `acceptance_criteria_count >= 1` avant assign agent (déjà des prédicats).
3. Ne pas promettre « bloque la branche Git » depuis Multica.

---

## 6. Nodeterm / FastClaw / Claw — inspiration UI / runtime

| Projet | Kokpit | Pour Multica |
| --- | --- | --- |
| [Nodeterm](https://github.com/eneskirca/nodeterm) | Explorer | Canvas terminaux — Multica = board cloud + execution log, pas tmux |
| [FastClaw](https://github.com/fastclaw-ai/fastclaw) | Explorer | Bus multi-agents — concurrent runtime, pas à installer |
| [Claw](https://github.com/fastclaw-ai/claw) | Explorer | Runtime distribué flottes — Multica daemon ≠ ça |

**Action :** aucune intégration. Inspiration visuelle pour une future vue « flotte de runs » (déjà partiel : agent list workload + execution log). Différenciation : Multica = gouvernance workspace/issue, pas canvas tmux.

---

## Ordre de travail proposé

| # | Chantier | Effort | Dépend de |
| --- | --- | --- | --- |
| 1 | PoC **Scenario** sur 1 skill Multica | S | **fait** : [scenario-skill-poc/](scenario-skill-poc/) (+ dogfood daemon/LLM DEV-27/30) |
| 2 | **ADR-lite** : tests abuse + matrice Never/Ask | S | **fait** : [adr-lite-mcp-gateway-2026-09-11.md](adr-lite-mcp-gateway-2026-09-11.md) + abuse tests + **UI** `GET …/mcp-calls` / panel replay |
| 3 | Skill **finish-first / Superpowers-lite** | S | **fait** : builtin `multica-finish-first` + dogfood **DEV-31** (Claude, Workspine file, 0 children) |
| 4 | Convention **Workspine-lite** | XS | **fait** : `references/workspine.md` + fichier réel `.multica/plans/DEV-31.md` |
| 5 | Latitude traces → replay (si Scenario OK) | M | **fait (lite)** : [latitude-traces-replay-2026-09-11.md](latitude-traces-replay-2026-09-11.md) — mapping Multica, pas d’install Latitude |
| 6 | Nodeterm/Claw | — | skip |

---

## Non-objectifs

- Remplacer `memoryeval` par Scenario.
- Dépendance Python Uber ADR / FastClaw dans `server/`.
- Hard-block Git merge depuis Multica.
- Adopter Nodeterm comme UI officielle.

---

## Liens kokpit

Fiches générées sous `~/Downloads/kokpit/grille-11-*.html` ; index `data.js` (2026-09-08). Digest récent : `digest-2026-09-06.md` (CubeSandbox, Agent Vault, Workspine, Mythos, FastClaw, Nodeterm).
