# Intégrations et code réutilisable — 5 septembre 2026

**Décision : garder Multica comme orchestrateur ; prioriser Agent Vault pour les secrets, évaluer CubeSandbox comme infrastructure d’exécution distante, reprendre sélectivement les idées d’Agent Orchestrator.** Les CRM restent des systèmes auxquels connecter les missions. Aucune dépendance ajoutée, aucun code externe incorporé, aucun service externe démarré par cet audit.

Les huit dépôts ont été figés sur un commit. Le [manifeste](integration-candidates-2026-09-05.json) conserve les SHA complets, dates, licences observées, chemins et empreintes des fichiers consultés. Les verdicts portent sur leur adoption **dans Multica**, pas sur leur qualité générale. `ADAPTER_ONLY` autorise une intégration ciblée ; ce n’est pas une intégration déjà livrée. `EXCLUDE` écarte le remplacement du moteur actuel. Aucun `FORK_SHORTLIST` retenu.

## Ce que nous exigeons

- **Exécution durable** : run identifié, état hors mémoire du processus, reprise après redémarrage sur le même stockage. Une conversation fournisseur ou un snapshot de VM ne suffit pas.
- **Approbation gouvernée** : arrêt avant l’effet, politique/version et décision persistées, identité, motif, expiration et reçu indépendant de l’effet.
- **Isolation entre clients** : autorisation sur lecture, écriture, exécution, annulation et événements, vérifiée par des tests négatifs entre clients.
- **Sandbox** : frontière processus/VM/conteneur avec montages, réseau, secrets et limites explicites. Un répertoire de travail ou une instruction dans le prompt ne suffit pas.
- **Exploitation entreprise** : ces propriétés plus sauvegarde/restauration, migrations, supervision, responsabilité opérationnelle et frontière cloud documentée.

Dans les tableaux, « documenté » désigne une déclaration amont ; « code » une lecture d’implémentation. Aucun test de charge, d’intrusion ou de reprise du runtime externe n’a été exécuté. Aucun candidat n’est qualifié « enterprise-ready » par cet audit.

## Provenance et licences

Tous les dépôts étaient non archivés lors de la consultation. Une activité récente est un signal de maintenance, pas une preuve de fiabilité. Dates UTC.

| Dépôt / commit observé | Date du commit | Langage principal | Licence et conséquence pour notre choix |
|---|---|---|---|
| [trycompai/crm · 6d4793dd6d7a](https://github.com/trycompai/crm/tree/6d4793dd6d7aeea91aa6a034e00b17d7408a2d08) | 2026-08-21 14:25 | TypeScript | [MIT](https://github.com/trycompai/crm/blob/6d4793dd6d7aeea91aa6a034e00b17d7408a2d08/LICENSE) ; code réutilisable avec notices |
| [twentyhq/twenty · 69e46b20fb3c](https://github.com/twentyhq/twenty/tree/69e46b20fb3c896a0255b1635e00e6d18e581790) | 2026-09-04 18:37 | TypeScript | [AGPLv3, exception applicative, paquets MIT et fichiers Enterprise](https://github.com/twentyhq/twenty/blob/69e46b20fb3c896a0255b1635e00e6d18e581790/LICENSE) ; préférer API/webhooks |
| [margince/margince · fa73e7bcaf9c](https://github.com/margince/margince/tree/fa73e7bcaf9c1a8606081e12480296e4f275e071) | 2026-09-05 04:27 | Go | [BUSL-1.1](https://github.com/margince/margince/blob/fa73e7bcaf9c1a8606081e12480296e4f275e071/LICENSE) ; restrictions de production/hébergement, pas une base permissive aujourd’hui |
| [buildkite/agent · 6991ebb30c15](https://github.com/buildkite/agent/tree/6991ebb30c15269cc540a0ddcca7d94b2c84be2d) | 2026-09-04 05:58 | Go | [MIT](https://github.com/buildkite/agent/blob/6991ebb30c15269cc540a0ddcca7d94b2c84be2d/LICENSE.txt) ; agent CI lié à son service |
| [agentscope-ai/AgentTeams · acaed255b32b](https://github.com/agentscope-ai/AgentTeams/tree/acaed255b32beed3c31e21a598d8f143d488ead0) | 2026-09-04 10:18 | Go | [Apache-2.0](https://github.com/agentscope-ai/AgentTeams/blob/acaed255b32beed3c31e21a598d8f143d488ead0/LICENSE) |
| [Untrivial-ai/agent-orchestrator · d2c88dae7d96](https://github.com/Untrivial-ai/agent-orchestrator/tree/d2c88dae7d968df26c990ac5dd42ed2fc17513b1) | 2026-09-04 23:21 | Go | [Apache-2.0](https://github.com/Untrivial-ai/agent-orchestrator/blob/d2c88dae7d968df26c990ac5dd42ed2fc17513b1/LICENSE) ; réutilisation possible avec licence/notices et modifications signalées |
| [Infisical/agent-vault · bd1a325d7912](https://github.com/Infisical/agent-vault/tree/bd1a325d79129644487f3e5b4f18c51adbc64638) | 2026-09-03 23:08 | Go | [MIT Expat hors `ee/` et composants tiers](https://github.com/Infisical/agent-vault/blob/bd1a325d79129644487f3e5b4f18c51adbc64638/LICENSE) ; périmètre Enterprise à distinguer |
| [TencentCloud/CubeSandbox · fe3852025ca8](https://github.com/TencentCloud/CubeSandbox/tree/fe3852025ca802cd99a0f682536e1632e1cc711d) | 2026-09-04 12:30 | Go | [Apache-2.0 avec notices de composants tiers](https://github.com/TencentCloud/CubeSandbox/blob/fe3852025ca802cd99a0f682536e1632e1cc711d/LICENSE) ; distribution complète à examiner par composant |

Twenty autorise explicitement les applications utilisant ses interfaces publiées sans les placer, de ce seul fait, sous AGPL. Cela ne couvre pas une copie/modification de son cœur. Margince accorde la production jusqu’à dix personnes au total ; l’hébergement pour des tiers nécessite un accord distinct. Sa licence indique un changement vers Apache-2.0 au 4 juillet 2028 pour cette version. Ces distinctions viennent des fichiers LICENSE figés ci-dessus.

## Capacités et coût d’adoption

| Candidat | Sessions / persistance | Équipes / outils | API / approbations | Isolation / cloud | Travail restant pour Multica | Verdict |
|---|---|---|---|---|---|---|
| trycompai/crm | CRM Postgres ; sessions eve et leases documentés | Agents spécialisés et outils métier | tRPC ; publication humaine des agents documentée | Application volontairement mono-client ; sandbox deny-all documentée ; eve/Vercel à auditer séparément | Moyen pour un connecteur ; élevé pour absorber CRM et runtime | `ADAPTER_ONLY` |
| twentyhq/twenty | Persistance métier CRM ; reprise de nos runs non évaluée | Plateforme d’applications | REST/GraphQL/webhooks couverts par l’exception applicative ; nos décisions restent dans Multica | Service CRM distinct ; tests croisés d’accès non exécutés | Moyen : mapping contacts/sociétés/opportunités, autorisations, retries et reçus d’écriture | `ADAPTER_ONLY` |
| margince/margince | Données CRM ; suspension/reprise sur approbation documentée | Outils MCP et REST sous une même politique documentée | Identité humaine déléguée, périmètre et approbation | Budget agent déclaré non encore appliqué dans la documentation auditée ; licence d’hébergement distincte | Moyen pour connecter une instance autorisée ; élevé et contraint pour intégrer le cœur | `ADAPTER_ONLY` |
| buildkite/agent | Exécution de jobs ; état et annulation coordonnés avec Buildkite | Pipeline CI, pas un orchestrateur LLM | API Buildkite ; pas de preuve de notre acceptation métier | Agent auto-hébergé, service de contrôle externe ; isolation selon déploiement | Faible/moyen pour importer CI et artefacts ; ne pas en faire notre bibliothèque runtime | `ADAPTER_ONLY` |
| agentscope-ai/AgentTeams | Ressources Kubernetes, rooms et fichiers partagés documentés ; reprise de mission non prouvée | Manager/workers, Matrix, MCP documentés | Intervention humaine dans les rooms ; reçu d’effet complet non vérifié | Kubernetes + Matrix + MinIO ; AgentLoop optionnel | Élevé : seconde autorité de coordination et nouvelle infrastructure | `EXCLUDE` |
| Untrivial-ai/agent-orchestrator | Daemon et SQLite ; signatures de relance persistées dans le code | Workers/worktrees ; réactions PR/CI et revue | API locale ; garde-fous de saisie ; aucune preuve d’isolation SaaS dans les composants audités | Desktop/local ; sous-module cloud privé distinct | Moyen pour adapter un mécanisme isolé ; élevé pour fusionner les moteurs et stockages | `ADAPTER_ONLY` |
| Infisical/agent-vault | Identités et sessions de proxy persistées ; pas des runs de travail | Proxy HTTP(S), compatible avec les clients configurés pour lui | Jetons limités à un coffre et au rôle proxy dans le code | Service séparé ; filtrage réseau du proxy ; bypass direct à fermer au niveau sandbox | Moyen : mapping workspace/coffre, TTL, révocation, certificats, redaction et tests de séparation | `ADAPTER_ONLY` |
| TencentCloud/CubeSandbox | Cycle de vie et pause/reprise de sandbox dans le code ; pas la reprise métier | Infrastructure de VM ; SDK Go | API de création/exécution/réseau ; approbations métier restent dans Multica | Linux, micro-VM et CubeEgress ; sécurité et reprise réelles non testées ici | Élevé côté exploitation ; adaptateur + templates + réseau + nettoyage + quotas | `ADAPTER_ONLY` |

### Agent Orchestrator : ce que nous pouvons reprendre

Oui, son [module backend](https://github.com/Untrivial-ai/agent-orchestrator/blob/d2c88dae7d968df26c990ac5dd42ed2fc17513b1/backend/go.mod) est en Go, avec Chi. Les composants examinés sont sous `backend/internal` : ce ne sont pas des bibliothèques directement importables depuis notre module. Copier un fichier choisi impose d’en conserver la provenance et les obligations de licence. Le [sous-module privé ao-cloud](https://github.com/Untrivial-ai/agent-orchestrator/blob/d2c88dae7d968df26c990ac5dd42ed2fc17513b1/.gitmodules) est hors du périmètre de code public audité.

Trois références utiles :

1. [Session guard](https://github.com/Untrivial-ai/agent-orchestrator/blob/d2c88dae7d968df26c990ac5dd42ed2fc17513b1/backend/internal/sessionguard/guard.go) : recontrôler l’état juste avant d’envoyer à l’agent ; ne pas confondre disponibilité de saisie et permission humaine en attente.
2. [Autoreview coordinator](https://github.com/Untrivial-ai/agent-orchestrator/blob/d2c88dae7d968df26c990ac5dd42ed2fc17513b1/backend/internal/autoreview/coordinator.go) : décisions liées à la tête de PR, annulations respectées et nombre borné d’échecs automatiques sur la même tête.
3. [Readiness](https://github.com/Untrivial-ai/agent-orchestrator/blob/d2c88dae7d968df26c990ac5dd42ed2fc17513b1/backend/internal/domain/agent_readiness.go) : installation, authentification et fraîcheur distinctes ; un résultat inconnu ne devient pas un succès.

**Limite précise :** `sendOnce` effectue l’envoi avant la persistance de sa signature. Son commentaire reconnaît une relance supplémentaire possible après échec de persistance puis redémarrage. Il ne constitue donc pas une garantie d’effet unique. Ne pas copier cette fonction pour justifier l’idempotence d’une action métier. [Code des réactions](https://github.com/Untrivial-ai/agent-orchestrator/blob/d2c88dae7d968df26c990ac5dd42ed2fc17513b1/backend/internal/lifecycle/reactions.go#L961-L1027).

Multica possède déjà les tâches persistées, les déclenchements de commentaires, les worktrees et les snapshots PR. La prochaine adaptation utile est la **correction demandée → reprise durable → nouveau résultat → nouvelle revue**, en conservant les frontières actuelles. Le code externe n’a pas encore été incorporé : aucune lacune ne justifie à ce stade de dupliquer ces mécanismes.

### Agent Vault : premier pilote d’intégration

Le [résolveur de session](https://github.com/Infisical/agent-vault/blob/bd1a325d79129644487f3e5b4f18c51adbc64638/internal/brokercore/session.go) vérifie expiration et coffre cible. Le [handler de création](https://github.com/Infisical/agent-vault/blob/bd1a325d79129644487f3e5b4f18c51adbc64638/internal/server/handle_sessions.go) limite les nouveaux jetons au rôle `proxy` ; un titulaire de ce rôle ne peut pas créer d’autres jetons. Ce sont de bonnes frontières pour des runs temporaires.

Raccordement proposé : Multica associe un coffre à un workspace, crée un jeton temporaire pour le run côté contrôle, transmet uniquement jeton de proxy et certificat au worker, puis révoque à la fin/annulation. Les véritables clés restent dans le service séparé. Ne pas placer le jeton administrateur dans `custom_env` ou les instructions. Les logs ne doivent pas capturer les en-têtes proxy.

Le [README figé](https://github.com/Infisical/agent-vault/blob/bd1a325d79129644487f3e5b4f18c51adbc64638/README.md) annonce SQLite/Postgres et précise que les destinations sans règle sont autorisées par défaut : choisir explicitement `unmatched_host_policy=deny`. Les variables `HTTPS_PROXY` ne forcent pas à elles seules un programme à utiliser le proxy. Le contrôle réseau du worker doit interdire les sorties directes. La [documentation sécurité](https://github.com/Infisical/agent-vault/blob/bd1a325d79129644487f3e5b4f18c51adbc64638/docs/learn/security.mdx) détaille les exceptions et le périmètre des secrets protégés ; ce service ne garantit pas qu’une requête autorisée soit pertinente.

Critères du pilote : API factice avec secret sentinelle ; secret absent du processus, disque et logs de l’agent ; hôte et chemin interdits rejetés ; coffre d’un autre workspace inaccessible ; expiration/révocation effectives sur connexions neuves **et déjà ouvertes** ; arrêt du proxy sans repli direct. Tester les vrais clients HTTP des CLI avec des fournisseurs factices, sans compte ni coût modèle. Ces tests ne sont pas encore exécutés.

### CubeSandbox : infrastructure distante à comparer, pas dépendance locale

Le [SDK Go](https://github.com/TencentCloud/CubeSandbox/blob/fe3852025ca802cd99a0f682536e1632e1cc711d/sdk/go/sandbox.go) et le [code pause/reprise](https://github.com/TencentCloud/CubeSandbox/blob/fe3852025ca802cd99a0f682536e1632e1cc711d/CubeMaster/pkg/service/sandbox/sandbox_resume_pause.go) offrent des points d’entrée concrets. Il faut conserver une correspondance persistée workspace/run/sandbox, traiter les timeouts ambigus de création, annuler et nettoyer les ressources orphelines. Une VM restaurée ne prouve pas qu’une action externe ne sera pas répétée.

Le [déploiement PVM documenté](https://github.com/TencentCloud/CubeSandbox/blob/fe3852025ca802cd99a0f682536e1632e1cc711d/docs/guide/pvm-deploy.md) exige un serveur Linux x86_64, des droits root et un noyau adapté lorsque KVM n’est pas disponible. Il ne s’agit pas d’un composant à lancer dans notre application macOS. Aucun changement de noyau ni installation de cette infrastructure n’a été effectué.

**Éviter le doublon :** [CubeEgress](https://github.com/TencentCloud/CubeSandbox/blob/fe3852025ca802cd99a0f682536e1632e1cc711d/docs/guide/security-proxy.md) propose déjà filtrage, injection d’en-têtes et journalisation. Sa documentation précise que le trafic interne et les ports non couverts par les règles L7 ne passent pas dans ce proxy. Comparer « Cube seul avec CubeEgress » à « Cube + Agent Vault » ; retenir le second uniquement si sa gestion des coffres et jetons apporte un bénéfice mesuré. Ne pas superposer deux interceptions TLS sans vérifier le trajet réel.

Critères du pilote Linux : création/exec/destruction, quotas CPU/RAM/disque, nettoyage après crash, redémarrage du contrôle, pause/reprise sans fuite de jeton expiré, échec fermé du réseau, isolation entre deux workspaces, refus d’accès aux métadonnées cloud et au contrôle. Mesurer démarrage, latence et coût ; aucune valeur marketing de performance n’est reprise comme mesure Multica.

### Autres inspirations, sans absorber leurs produits

- **Comp AI CRM** : [preuves, leases et déploiement humain de versions](https://github.com/trycompai/crm/blob/6d4793dd6d7aeea91aa6a034e00b17d7408a2d08/docs/agent.md). Utile pour notre promotion de mémoire sourcée. Son choix mono-client et son runtime eve ne doivent pas devenir implicitement notre architecture.
- **Margince** : [même règle sous MCP et REST, refus d’auto-approbation, reprise du même appel](https://github.com/margince/margince/blob/fa73e7bcaf9c1a8606081e12480296e4f275e071/docs/explanation/agent-surface.md). La page signale aussi que le budget de volume agent n’est pas encore appliqué. S’inspirer des invariants, connecter une instance autorisée ; ne pas copier son cœur sous une hypothèse MIT.
- **Buildkite** : [supervision et annulation de jobs](https://github.com/buildkite/agent/blob/6991ebb30c15269cc540a0ddcca7d94b2c84be2d/agent/job_runner.go). Son [README](https://github.com/buildkite/agent/blob/6991ebb30c15269cc540a0ddcca7d94b2c84be2d/README.md) ne promet pas la stabilité comme bibliothèque Go. Importer les preuves CI d’un client déjà équipé est plus simple que maintenir un fork.
- **AgentTeams** : [Manager/workers, Matrix et stockage partagé documentés](https://github.com/agentscope-ai/AgentTeams/blob/acaed255b32beed3c31e21a598d8f143d488ead0/README.md), avec dépendances Kubernetes dans le [module controller](https://github.com/agentscope-ai/AgentTeams/blob/acaed255b32beed3c31e21a598d8f143d488ead0/agentteams-controller/go.mod). Un connecteur Matrix pourrait avoir un intérêt demandé ; absorber cette coordination dupliquerait aujourd’hui nos squads et notre daemon.

## Raccordements locaux et prochaine tranche

Sources Multica inspectées : [préparation du worker](../../server/internal/daemon/execenv/execenv.go), [politique sandbox Codex](../../server/internal/daemon/execenv/codex_sandbox.go), [déclenchement depuis les commentaires](../../server/internal/handler/comment.go), [revue de livraison](../../server/internal/handler/issue_delivery.go). La préparation actuelle gère des environnements de travail ; elle n’atteste pas une isolation micro-VM. La politique Linux Codex renvoie explicitement l’isolation à la frontière dans laquelle tourne le daemon.

Ordre recommandé : finir la reprise depuis correction sur les tâches existantes ; ensuite pilote Agent Vault ; comparer les options Cube sur infrastructure Linux dédiée ; puis connecter un CRM choisi par une mission réelle. Pour le premier pilote Cube, héberger un worker Multica dans la sandbox garde nos protocoles de claim/heartbeat/result ; la création et le nettoyage des sandboxes nécessitent encore un raccordement explicite. Ce n’est pas livré par l’audit.

L’hypothèse commerciale reste **une mission corrigée, acceptée et reproductible avec moins de temps humain, un coût observable et des accès bornés**. Ces briques réduisent le travail de construction ; leur assemblage ne prouve ni unicité ni volonté de payer. Voir la [comparaison concurrentielle](competitor-map-2026-09-04.md) et le [registre complet](implementation-goal-ledger.md).

## Vérification de cet audit

Commande : `rtk proxy python3 docs/research/validate-integration-audit.py`.

Résultat exécuté : code de sortie 0, huit candidats, 36 sources figées, liens locaux valides. Vérification parallèle de la tranche livraison : trois tests du composant réussis, ESLint ciblé et `git diff --check` réussis.

Le contrôle vérifie les huit candidats, les verdicts, les SHA, les sources figées, l’UTF-8 et les liens locaux. Il ne teste pas les services externes. **Aucun probe de restart/resume de runtime externe exécuté** : aucun remplacement du runtime Multica n’est retenu. Les pilotes de secrets et de sandbox restent des travaux distincts, avec critères ci-dessus. Les empreintes permettent de retrouver les fichiers exacts consultés sans dépendre du contenu futur de `main`.
