# Périmètre complet — campagne d’implémentation

Objectif utilisateur : terminer les améliorations, la mémoire et les fonctionnalités acceptées ; poursuivre la recherche d’un avantage décisif. Ce registre conserve le périmètre complet. Une case n’est terminée qu’avec une preuve correspondant à toute sa portée.

État initial de campagne : les tours précédents ont produit du code et des tests, donc constituent du progrès. L’objectif global reste actif.

| Exigence | État constaté | Preuve nécessaire pour terminer |
|---|---|---|
| Livraison vérifiable dans l’issue | Partiel implémenté et testé | Critères, livrables, preuve par critère, réserves, coût disponible ; acceptation humaine et demande de correction ; provenance/fraîcheur des preuves ; tests API/UI et parcours rendu |
| File de décisions dans Inbox | Implémenté et vérifié sur web/desktop ; revue indépendante ship | Objet durable question/contexte/options, destinataire humain, réponse finale concurrente, reprise explicite avec reçu idempotent ; API/CLI, PostgreSQL, UI et rendu vérifiés. Mobile natif traité séparément |
| Recette bug → reproduction → correction → test → PR → revue | Partiel vérifié (pilote agent réel) | Readiness + fixture `prove.mjs` ; pilote DEV-1 Claude 57s + [PR #1](https://github.com/jeffdev2018/multica-bugfix-pilot/pull/1) + Accept livraison ([bugfix-recipe-pilot-2026-09-07.md](bugfix-recipe-pilot-2026-09-07.md)) ; `pull_requests[]` vide — credentials App locaux vides ([github-app-dogfood-2026-09-07.md](github-app-dogfood-2026-09-07.md)) ; pas de merge/deploy |
| Coût par résultat accepté | Partiel vérifié web/desktop + mobile wire | Cumul des reprises figé ; **`human_effort_seconds`** web/desktop + mobile (timer client, ≠ `review_delay_seconds`) ; reste comparaison de procédures |
| Alertes et plafonds | Partiel vérifié | Couverture documentée ([budget-ceilings-audit-2026-09-06.md](budget-ceilings-audit-2026-09-06.md)) ; plafonds durs issues/autopilot/sièges sur frontières Multica + alertes UI ~80 % ; pas de plafond USD fournisseur (hors contrôle) |
| Mémoire par agent | Partiel vérifié (indépendant + never-eval) | DEV-3/4 (surface issue) + **DEV-5** : token `fern-1` **jamais** dans l’éval, correct après adopt ([agent-memory-unseen-fact-2026-09-07.md](agent-memory-unseen-fact-2026-09-07.md)) ; reste apprentissage autonome / moins de reprises métier |
| Mémoire partagée de projet | Partiel vérifié (promo + reuse + cross-agent + retry↓) | Publication/scoping/expiration/historique, promotion `source_review`, usage API/UI ; DEV-6 cross-agent ; pilote `keel-4` baseline 2/2 reprises → treatment 0/2 (−100 % relatif, n=2+2 synthétique) ([memory-retry-reduction-2026-09-07.md](memory-retry-reduction-2026-09-07.md)) ; apprentissage autonome hors produit ; reste familles métier réelles / n plus large |
| Compétence candidate → replay → cas réservés → promotion réversible | Partiel vérifié (pilote Claude + reçu + coût estimé) | Hors ligne + UI + pilotes Codex/Claude ; revue humaine 18 s ; `report_hash` API ; **estimation catalogue** `cost_status`/`estimated_cost_usd` sur évals connectées (`catalog_estimate`, pas facture fournisseur) ; reste facture fournisseur réelle si jamais disponible |
| Revue honnête / autorisation d’action | Partiel vérifié (contrat soft vs hard) | Soft signals ≠ merge/deploy ; hard gates Multica listés et testés (`authorization-frontiers`) ; docs security-model en/zh/ja/ko ; UI livraison déjà honnête ; reste bloqueurs externes VCS/deploy hors Multica |
| Activation jusqu’au premier résultat | Partiel vérifié (checklist + télémétrie + rendu + self-pilote) | Préparation lisible, `activation_checklist_viewed`, N/A CLI honnête, harness `/tmp/vigil-activation/` ; self-pilote ~4 min jusqu’au premier run ([activation-self-pilot-2026-09-07.md](activation-self-pilot-2026-09-07.md)) ; reste pilote froid &lt;10 min avec équipes externes |

| Causes d’attente et reprises | Partiel vérifié (cause+action + honesty retry) | Cause/action sur le journal d’exécution (wait_reason + guidance queued/dispatched/waiting) ; tooltip retry aligné MUL-4869 (workdir / session / pas d’undo des effets externes) ; docs troubleshooting ; reste revue humaine sur parcours réel |
| Activité ≠ livraison ≠ réussite | Partiel vérifié | Acceptation des preuves distinctes ; délai fin→revue + **effort auto-chronométré** web/desktop/mobile ; reste revue globale |
| Progression projet | Vérifié pour le renommage | « Scope closed », explication terminé+annulé, tests déjà passés ; revue globale finale |
| Surfaces orientées décision, dont mobile | Partiel : décisions + livraison mobile v1.5 | Inbox Décisions + accept/corriger sur fiche ; checklist [mobile-delivery-smoke-checklist-2026-09-07.md](mobile-delivery-smoke-checklist-2026-09-07.md) ; tests mobile delivery 6/6 + capture honesty 390px ; **simu iOS bloquée** (pas de Xcode/`simctl`) — [mobile-delivery-smoke-pilot-2026-09-07.md](mobile-delivery-smoke-pilot-2026-09-07.md) |
| Qualité et intégration globales | Partiel vérifié (build + smoke PW) | `pnpm build` 5/5 ; Playwright auth+nav+onboarding **10/10** via `127.0.0.1` + CORS dual ([quality-build-playwright-2026-09-07.md](quality-build-playwright-2026-09-07.md)) ; `make up C=web` ownership encore fragile |
| Recherche de LA killer feature | En cours (dogfood #1 nommé ; sim ≠ preuve) | Outreach autorisé ; équipe #1 = dogfood Jeff/vigil-482 ([killer-feature-team1-dogfood-2026-09-07.md](killer-feature-team1-dogfood-2026-09-07.md)) — **hors** seuil ≥2 équipes ; Northline sim séparée ; reste contact externe joignable |

## Ordre de travail

1. Rendre les parcours existants vérifiables et terminer l’activation CLI auth qui bloque le typecheck.
2. Livraison/acceptation et boucle de correction ; décisions et économie par résultat.
3. Mémoire sourcée, versions, expiration et promotion ; expérimentation baseline/candidat en environnement isolé.
4. Recette complète et surfaces web/desktop/mobile ; vérification du périmètre entier.
5. Recherche actualisée à chaque étape, puis pilote mesurable prêt à être confronté aux équipes cibles.

## Recherche actualisée

Complément Kokpit du 5 septembre : [quatre nouvelles décisions documentées](kokpit-complement-2026-09-05.md), 24 fichiers primaires figés. Memoria inspire l'application sélective de changements de mémoire ; MemSearch le retour aux preuves originales ; Relay la distinction modèle demandé/observé. Nango reste conditionnel, car Composio est déjà raccordé dans le code. Aucun moteur ajouté. Les branches, skills candidats et validations humaines existent chez ces projets : aucune exclusivité revendiquée. Priorité conservée : correction → cas de contrôle → comparaison indépendante → adoption réversible.

La source officielle Paperclip du 18 août 2026 décrit déjà des leçons sourcées et une promotion humaine en mémoire versionnée : https://www.paperclip.app/blog/agents-good-enough-to-great/ (consultée le 4 septembre 2026). Cela invalide l’idée que cette simple boucle constituerait une exclusivité. La différence à investiguer porte sur la preuve indépendante avant promotion : replay comparable, cas réservés, coût total et régressions. Il s’agit encore d’une hypothèse commerciale.


### Périmètre concurrentiel élargi

L’utilisateur a précisé AionUI, Traycer, NoteGen (notegen.top), Grok Bot (x.ai/bot), Buzz (block/buzz) et Cumora (yetone/cumora), en plus de Paperclip, et demandé de chercher d’autres alternatives. Voir [la comparaison élargie](competitor-map-2026-09-04.md). Intent, OpenHands et Factory ont été ajoutés à la première passe ; la couverture communautaire et la validation commerciale restent ouvertes.


### Progression — raccordement CLI auth

- Types/API/schéma de réponse complétés, routes login/logout/polling/rapport daemon branchées ; Redis configuré et commandes transmises via heartbeats HTTP et WebSocket.
- Connexion, déconnexion et lecture du code réservées à l’opérateur humain autorisé ; les lecteurs ordinaires d’un runtime public n’accèdent pas au code. Réponses malformées, succès sans booléen et URL non HTTP(S) échouent explicitement.
- Typecheck core/views, build serveur, 8 tests core et 2 tests UI passés ; tests Go CLI auth/daemon et non-régression heartbeat passés. Dialogue correctement nommé et décrit. Rendu réel du composant avec HTTP simulé, Inter chargé, largeur 390 px sans débordement, aucune erreur de page ; captures `/tmp/vigil-cli-auth/`.
- Aucun compte fournisseur réel utilisé ; commandes daemon testées avec un exécutable factice. Le parcours réel d’un fournisseur et l’intégralité de l’activation ne sont pas validés par ces tests.
- Audit de concurrence restant sur l’implémentation CLI auth préexistante : sérialisation des connexions/déconnexions d’un même compte machine, objets mutables exposés par le store mémoire, mises à jour Redis terminales concurrentes et cohérence Redis/métadonnées SQL. Les tests séquentiels ne prouvent pas ces invariants ; ne pas déclarer cette partie prête à déployer sans les résoudre.

Vérification globale supplémentaire terminée au passage au 5 septembre : `pnpm typecheck` réussi (9 tâches Turbo, mobile exclu par le script du dépôt). ESLint ciblé réussi. `git diff --check` réussi. Ces résultats ne valent pas validation de tous les tests ni achèvement de l’objectif global.


### 5 septembre — concurrence CLI auth

Les quatre problèmes identifiés ont reçu des corrections et des tests ciblés : snapshots indépendants du store mémoire ; transitions Redis atomiques et refus des réclamations obsolètes ; exclusion des connexions/déconnexions concurrentes par fournisseur/compte OS avec les verrous fichiers existants ; projection SQL du résultat gagnant réparée par reprise/polling après échec de transaction. L’inscription du runtime préserve désormais ce statut pour les runtimes standards et personnalisés. La détection de courses Go et le build serveur ont passé ; aucun fournisseur réel n’a été exécuté.

Limites du contrat : le verrou coordonne les processus Multica du même compte OS, pas les commandes fournisseur lancées manuellement hors Multica ; le statut est la dernière vérification rapportée, pas une attestation permanente d’authentification. La disponibilité d’un vrai fournisseur et l’activation complète restent distinctes. Le store Redis demeure la source des requêtes temporaires ; le statut SQL est une projection récupérable, pas une transaction distribuée.

Recherche : ajout de deux signaux historiques sourcés (AionUI #3216 et retour technique Cumora du 3 juin), explicitement hors de la fenêtre récente. Ils motivent la mesure des mauvaises habitudes propagées sans démontrer une volonté de payer.

Prochaine exigence principale : livraison vérifiable dans l’issue et acceptation humaine ; ne pas continuer à élargir le sous-projet CLI auth à la place de ce périmètre produit.

Point d’entrée vérifié pour la livraison : `listIssuePullRequests` / `issuePullRequestsOptions` existent déjà. `ListPullRequestsByIssue` fournit le SHA de tête, le SHA du snapshot, sa date et les comptes de checks ; réutiliser ces preuves et leur fraîcheur. `issue-usage-dialog.tsx` est la surface existante pour les coûts. Ne pas recréer une collecte parallèle de PR/CI/usage.

Vérifications finales de cette étape : tests Go avec `-race` CLI auth/store/daemon/verrou OS passés ; tests de réinscription standard et profil, reprise après échec de commit, polling réparateur et protection contre un ancien résultat passés ; build serveur et `git diff --check` réussis. La régénération sqlc a été comparée à `/tmp/vigil-auth-concurrency-generated-before` : seul `runtime.sql.go` a changé.


### 5 septembre — première tranche de livraison vérifiable

API et carte partagée web/desktop : critères révisés, dernier run hors chat privé, preuves PR avec SHA, décision humaine et preuve par critère, feedback de correction. Snapshots immuables, reprise idempotente et concurrence entre décisions contrôlées. Les critères sont injectés au claim dans les instructions du run. Le coût affiché réutilise les calculs existants et distingue estimation/données incomplètes ; il couvre les runs actuels, pas encore un coût figé par résultat accepté.

Preuves déjà obtenues : migrations 473–476 appliquées uniquement à la base de test isolée ; tests API Go avec détection de courses ; 3 tests de frontière core ; 69 tests UI incluant le conteneur IssueDetail. Rendu réel du composant avec HTTP simulé, ordinateur et 390 px, aucune erreur de page ni débordement ; captures et harness conservés dans `/tmp/vigil-delivery/`.

**À terminer au terme de cette première tranche :** reprise actionnable, attribution et transcript (traités dans la tranche suivante ci-dessous), navigation dans l’historique complet, métriques par résultat accepté, parité mobile native et vérifications globales. La revue n’interdit pas une fusion ou un déploiement hors de son périmètre. L’objectif global reste actif.

L’utilisateur a ajouté six sources d’inspiration/intégration : trycompai/crm, twentyhq/twenty, margince/margince, buildkite/agent, agentscope-ai/AgentTeams et Untrivial-ai/agent-orchestrator. Évaluation des versions, licences et interfaces en cours ; ne pas absorber un second plan de contrôle par défaut.

### 5 septembre — huit candidats d’intégration audités

Ajouts utilisateur : Infisical/agent-vault et TencentCloud/CubeSandbox. L’[audit figé des huit dépôts](integration-candidates-2026-09-05.md) précise licences, frontières, code intéressant, doublons et critères de pilote. Priorités retenues : Agent Vault pour un pilote de secrets temporaires ; CubeSandbox comme infrastructure Linux distante à comparer à sa propre offre CubeEgress ; Agent Orchestrator comme référence de garde-fous et de revue par tête de PR. Son backend est bien en Go/Apache-2.0, mais sa déduplication de relances accepte un doublon après échec de persistance et redémarrage : ne pas la présenter comme un effet unique garanti.

Aucun code externe incorporé et aucun runtime externe validé par exécution. Les licences mixtes de Twenty/Agent Vault, la BUSL de Margince et le sous-module cloud privé d’Agent Orchestrator sont documentés. Les intégrations restent à implémenter après leurs pilotes ; elles ne sont pas cochées terminées par la lecture des sources. La reprise depuis correction reste la prochaine tranche produit principale.


### 5 septembre — correction actionnable et sourcée

La revue de correction enregistrée propose maintenant un lancement humain distinct. Création du run et liaison à la revue dans la même transaction ; contrôle des permissions, de la fraîcheur et du travail déjà en attente. Deux appels concurrents ou une reprise après redémarrage renvoient le même run. Le run reçoit le feedback et les critères examinés, conserve la filiation et réutilise le travail précédent quand disponible. Le résultat doit être revu à nouveau. Attribution humaine lisible et boutons vers le composant transcript existant ajoutés à la carte partagée.

Vérifications : migration 477 uniquement en base isolée ; 9 tests Go livraison avec détection de courses, 4 tests core, 70 tests UI carte/conteneur passés ; typecheck global puis typecheck views après les derniers raccordements ; ESLint ciblé et build Go réussis. Les deux fichiers générés sqlc ont été comparés au snapshot préalable. Rendu du vrai composant avec API simulée : attribution, échec de lancement, feedback préservé, retry, reçu conservé après rechargement ; aucune erreur de page et aucun débordement à 390 px. Captures inspectées et harness : `/tmp/vigil-delivery-correction/`. Aucun fournisseur ou agent CLI réel utilisé. Le parcours transcript n’a pas reçu de nouveau test navigateur dans ce harness.

Restent notamment historique complet, métriques par résultat accepté, mémoire versionnée/expiration/promotion, évaluation des procédures, décisions Inbox, recette complète et mobile natif. La campagne reste incomplète.

### 5 septembre — nouvelles pistes tirées de Kokpit

[Décisions sur huit nouveaux candidats](kokpit-candidates-2026-09-05.md), après inventaire de 1 311 fiches et lecture ciblée. 60 sources figées ; aucun code incorporé. LocalRecall : référence Go documentaire conditionnelle, avec défaut UTF-8 reproduit avant toute reprise. Headroom : essai interne de compression avec témoin. OpenViking/self-learning-skills/adversarial-contract-gate : chargement progressif et procédures validées. Shepherd/Smithers : propositions avant application et reprise selon les effets, sans remplacement du moteur. Cognee reste conditionnel : le graphe Postgres de production relève d’une offre sous licence selon le README actuel.

Nouvelles exigences proposées pour les tranches existantes : raison de rappel d’une mémoire et version réellement utilisée ; invalidation des procédures quand leurs sources changent ; blocage structuré et action humaine attendue ; dossier de procédure exportable entre agents. Hypothèse commerciale : conservation d’un savoir-faire vérifié et réduction mesurée des reprises quand l’exécutant change. Ni exclusivité ni volonté de payer démontrée.


### 5 septembre — mémoire de projet versionnée et expiration

Historique persistant paginé, restauration d’une version en nouvelle publication, expiration optionnelle et exclusion des règles expirées au claim. Les publications préexistantes sont conservées par migration ; l’état vide initial est conservé à la première publication des nouveaux projets. Les dates de restauration restent celles de la version source : restaurer ne renouvelle pas implicitement une règle expirée. Les anciens clients qui omettent l’expiration la préservent. Publication et snapshots sont atomiques ; suppression projet/workspace et publication partagent leurs verrous puis nettoient explicitement l’historique.

Carte web/desktop : consultation et pagination de l’historique, aperçu avant restauration, conservation du brouillon en conflit, expiration en heure locale et état expiré. Documentation utilisateur dans les quatre langues et skill projet actualisés. Le serveur utilise l’heure de claim ; l’interface rafraîchit l’état d’expiration au plus toutes les 30 secondes tant qu’une échéance est active. Les runs déjà démarrés gardent le contexte reçu. L’expiration porte sur le lot de règles du projet, pas sur chacune séparément.

Preuves : migrations 478–479 appliquées uniquement à `multica_memory_review_test_20260904` ; cinq tests mémoire projet et deux régressions suppression passés avec `-race`, incluant publication concurrente, échec de commit, restauration expirée, pagination et nettoyages. Deux tests core et huit tests UI carte/conteneur passés. Typecheck global 9/9 (mobile exclu par le script), ESLint ciblé, build Go et diff whitespace passés. Quatre fichiers sqlc comparés au snapshot : modifications limitées aux nouveaux champs et requêtes attendus. Rendu du composant avec API simulée, police Inter, aucune erreur et aucun débordement à 390 px ; captures inspectées dans `/tmp/vigil-project-memory-history/` et harness archivé dans ce dossier.

Cette tranche ne termine pas la mémoire par agent : il reste son historique/expiration, la promotion sourcée depuis correction, les mesures de réutilisation et le replay des procédures sur cas réservés. La parité mobile native et l’objectif global restent ouverts. Les pistes Kokpit orientent ces travaux ; aucun avantage commercial mesuré n’est revendiqué à partir de cette infrastructure.

### 5 septembre — expiration des mémoires par agent

Échéance optionnelle par leçon, conservée lors des modifications et approbations. Une date effacée explicitement supprime l’expiration ; toute modification de date exige la révision attendue. Les suggestions restent soumises à la revue humaine. Le serveur exclut les leçons expirées avant la limite d’injection de 50 ; elles restent visibles et comptent dans le plafond de 200. Les runs déjà démarrés gardent leur contexte. L’interface utilise l’heure locale et actualise les échéances au plus toutes les 30 secondes tant qu’une échéance est active.

Lecture et écriture API : les réponses mémoire malformées produisent une erreur explicite, sans afficher une fausse liste vide ni fermer l’éditeur sur un faux succès. Une leçon expirée n’affiche plus « Active » ou « Stop using ». Documentation utilisateur actualisée dans quatre langues.

Preuves : migration 480 uniquement en base isolée ; tests Go agent/projet avec `-race` et extraction factice réussis ; 152 tests core (schémas et expiration), neuf tests UI, typecheck global 9/9 puis core après le dernier durcissement API, ESLint ciblé et build Go réussis. Régénération sqlc comparée au snapshot préalable : seuls models.go et agent_memory.sql.go ont changé. Rendu du composant réel avec HTTP simulé, édition d’une leçon expirée conservant sa date et sa révision attendue, Inter chargé, aucune erreur et aucun débordement à 390 px. Captures inspectées dans `/tmp/vigil-agent-memory-expiry/`.

Restent l’historique réversible par agent, la promotion sourcée depuis correction, la mesure de réutilisation et l’évaluation des procédures ; aucun apprentissage autonome validé ni parité mobile native revendiqués. Recherche : CopilotKit Intelligence annonce déjà apprentissage par portée et provenance. La réduction mesurée des reprises, conservée lors d’un changement d’exécutant, reste une hypothèse à éprouver, pas une exclusivité établie.


### 5 septembre — historique réversible des mémoires par agent

Versions conservées à la création manuelle et à l’extraction, avant/après chaque modification, approbation ou restauration. Migration de l’état courant préexistant, sans inventer les anciens changements. Restauration humaine du texte, du statut et de l’échéance en une nouvelle révision ; acteur actuel enregistré, source du run préservée, lien vers la révision restaurée. Une suggestion restaurée reste en attente ; une règle active expirée reste exclue des nouveaux runs. Historique paginé par curseur de révision, mêmes frontières agent/espace que la mémoire actuelle et middleware d’appartenance de la route.

Écriture et versions dans la même transaction, ordre de verrouillage espace puis agent partagé avec l’extraction. Suppression permanente d’une mémoire supprime ses versions ; nettoyages explicites lors des suppressions d’espace et de carrier agent. Interface web/desktop : historique consultable sans droit d’édition, aperçu de restauration, conflit conservant la sélection, retour explicite à l’historique actualisé, erreurs API malformées signalées. Les échéances de l’historique ouvert sont rafraîchies toutes les 30 secondes si nécessaire. Documentation dans quatre langues.

Preuves : migrations 481–482 appliquées uniquement à la base isolée ; tests Go mémoire agent, suppression carrier et régression builder passés avec `-race` (2,359 s), extraction factice et enregistrement de ses versions passés (0,763 s). Deux tests core et dix tests UI réussis ; typecheck global 9/9 puis contrôle views après les derniers ajustements ; ESLint ciblé et build serveur réussis. Trois fichiers sqlc comparés au snapshot préalable : models.go, agent_memory.sql.go et workspace_delete.sql.go uniquement. Rendu réel avec HTTP simulé : conflit dû à une nouvelle révision, sélection conservée, retour à l’historique puis restauration sur la révision actualisée ; aucune erreur de page ni débordement à 390 px. Captures inspectées et harness conservés dans `/tmp/vigil-agent-memory-history/`.

Recherche : Letta Context-Bench V2 sépare génération/entretien, récupération et respect de la mémoire. Benchmark privé de l’éditeur, non reproduit. Notre future évaluation doit distinguer la procédure retrouvée du résultat amélioré ; la portabilité seule n’est pas une exclusivité démontrée. Restent promotion sourcée depuis les corrections de livraison, mesures de réutilisation, replay/cas réservés, métriques par résultat accepté, Inbox, recette complète et mobile natif. L’objectif global n’est pas terminé.


### 5 septembre — coût figé et historique des revues

Chaque nouvelle revue conserve une observation du coût cumulatif des runs non privés de l’issue, reprises comprises. Les montants fournisseur restent des décimaux exacts ; les estimations conservent les tokens et les tarifs réellement utilisés. Les absences de rapports, prix inconnus et runs non terminés sont explicites. Les revues anciennes sans snapshot restent sans coût. Une reprise de la même requête retourne le montant sauvegardé, même après modification du rapport d’usage. La transaction enregistre ensemble revue et coût ; les chats privés sont exclus.

L’historique partagé web/desktop charge les décisions par pages de 20, avec auteur, date, feedback, résultat et preuves conservées. Les compteurs dédupliquent les acceptations d’une même preuve et enregistrent les corrections après acceptation. Le délai fin de run→revue est du temps écoulé, pas des heures de travail humain. Le coût exclut aussi infrastructure et abonnements ; les snapshots cumulatifs ne doivent pas être additionnés. Cette observation ne constitue ni une facture ni un retour sur investissement complet.

Vérifications exécutées : migration 483 uniquement sur la base isolée `multica_memory_review_test_20260904` ; tests Go livraison/coût avec `-race` réussis, incluant montants au-delà des entiers sûrs JavaScript, retries, confidentialité, rollback, pagination et frontière workspace ; cinq tests core et cinq tests UI de la carte réussis. Typecheck global (9 tâches, hors mobile), ESLint ciblé et build Go réussis. Génération sqlc comparée au snapshot préalable : seuls `models.go` et `issue_delivery.sql.go` changent. Documentation des issues dans les quatre langues et skill intégré actualisés.

Rendu du vrai composant avec API HTTP simulée : coût partiel et détail, deux pages d’historique, ancien coût indisponible et résultat antérieur consultable sans remplacer le résultat actuel. Captures inspectées dans `/tmp/vigil-delivery-economics/`, aucune erreur de page, Inter chargé et largeur 390 px sans débordement. Le harness est archivé avec les captures ; aucun fournisseur réel appelé. La parité mobile native et les vérifications de l’ensemble de la campagne restent à faire.

Recherche poursuivie : le [rapport Kokpit](kokpit-candidates-2026-09-05.md) confirme par nouvelle lecture primaire que Headroom documente déjà mémoire partagée entre agents et extraction de corrections vers les consignes. La différenciation par ce seul assemblage reste non démontrée. Les procédures candidates doivent gagner sur les cas de l’équipe avec leur coût et leurs régressions ; cette étape reste à implémenter.

Contrôle du catalogue : l’annonce officielle [Sonnet 5](https://www.anthropic.com/research/claude-sonnet-5), changelog du 10 août, confirme que la hausse prévue au 1er septembre a été annulée. Les valeurs numériques existantes étaient correctes ; seuls les commentaires obsolètes Go/TS ont été corrigés.


### 5 septembre — correction humaine vers mémoire candidate

Le parcours existant de création de mémoire accepte une revue de correction comme source. Le serveur vérifie l’humain, le droit de gérer l’agent, le workspace et le run original hors chat privé ; il copie les critères, les évaluations, le feedback, l’auteur, la date et l’identité du snapshot. Il crée une mémoire `pending`, jamais une approbation implicite. Une même revue ne possède qu’une candidate vivante par agent ; la même requête retourne cette mémoire sans écraser une décision ou modification ultérieure. Un autre contenu doit passer par l’édition. La suppression explicite de la mémoire retire aussi son historique et permet une nouvelle proposition si la source existe encore.

Les preuves sont conservées sur la mémoire et ses versions, même si le run ou la revue source disparaît. Seul le contenu court approuvé est transmis par `LoadAgentMemories` ; les preuves ne grossissent pas le contexte injecté. La mémoire reste au niveau de l’agent, sans restriction automatique au projet d’origine. Le partage projet et les procédures validées par replay ne sont pas réalisés par cette tranche.

UI partagée : bouton depuis la correction courante ou l’historique quand le run est encore disponible ; réutilisation du formulaire mémoire, conservation du brouillon en échec et reçu après sauvegarde. La source affiche les preuves de la correction même si le run ne se charge plus. L’approbation, l’expiration et la restauration utilisent les contrôles existants. Documentation des issues dans les quatre langues et skill intégré actualisés.

Preuves obtenues : migrations 484–485 appliquées uniquement en base de test isolée ; génération sqlc comparée au snapshot préalable, uniquement `models.go` et `agent_memory.sql.go`. Tests Go mémoire/livraison avec `-race` réussis, incluant concurrence, provenance, rollback, source privée/étrangère, approbation et restauration ; régression extraction du service avec `-race` réussie. Trois tests core mémoire passent. Les trois suites UI mémoire/carte/conteneur passent (81 tests avant ajout du nouveau cas de parcours), puis les 11 tests mémoire incluant le nouveau parcours passent. Build Go et lint ciblé réussis. Un défaut de typage dans le nouveau test UI a été corrigé ; le résultat du typecheck global final est consigné ci-dessous.

Rendu réel avec HTTP simulé : proposition depuis une issue, panne puis retry conservant texte et source, candidate inactive sur la page mémoire, preuves encore visibles sans le run. Captures inspectées `/tmp/vigil-correction-memory/`, Inter chargé, aucune erreur de page ni débordement à 390 px. Aucun fournisseur réel ni CLI agent utilisé.

Recherche : ajout d’Hermes Agent au [périmètre concurrentiel](competitor-map-2026-09-04.md). Sa documentation propose déjà approbation des écritures de mémoire/skills ; la PR du 4 septembre traite l’accumulation de journaux dans les procédures. Ce constat soutient la séparation règle/provenance, sans prouver notre exclusivité ni une volonté de payer. Prochaine étape produit : version effectivement consommée, métriques de réutilisation et préparation de la validation des procédures. Les exigences Inbox, budgets, activation, recette complète et mobile restent ouvertes.

Vérification finale de cette tranche : typecheck global réussi (9 tâches, hors mobile natif). Le test de cycle de candidate exerce maintenant `TaskService.LoadAgentMemories` et confirme que seul le texte court approuvé est chargé ; les deux tests de correction passent avec `-race`. `git diff --check` passe. Harness archivé avec les captures et serveur temporaire arrêté. La campagne globale reste active et incomplète.

### 5 septembre — versions mémoire sélectionnées au départ du run

Le départ simple et le départ groupé (donc le RPC WS qui réutilise ce dernier) capturent les IDs/révisions des mêmes lignes que les 50 contenus agent et la révision des mêmes règles projet rendues dans le contexte. La finalisation écrit `memory_context` avec les jetons dans une transaction, protégée par runtime, date exacte de claim et absence de démarrage. Une reprise remplace le dernier snapshot ; un appel ancien ou arrivé après démarrage est rejeté. Les snapshots ne constituent pas un historique immuable de tous les claims.

Les réponses task existantes exposent ce contexte ; les détails de transcription web/desktop affichent les références, la date et les états distincts vide/chargement impossible/non enregistré. Les anciens runs restent inconnus. Le texte des souvenirs n’est pas dupliqué dans les runs et sa suppression reste effective. Il s’agit du contenu sélectionné pour la réponse, pas d’une preuve de réception daemon, d’adhérence du modèle ou de réussite de l’apprentissage. Aucune mesure de réutilisation agrégée n’est encore publiée.

Migration 486 appliquée uniquement à `multica_memory_review_test_20260904` via pgx (psql indisponible). Génération sqlc comparée au snapshot `/tmp/vigil-memory-context-generated-before` : nouveaux champs task dans les scans/retours et une requête CAS, sans autres modifications. Tests Go mémoire agent/projet et rollback existants passent avec `-race`; nouveau parcours simple/groupé couvre cap et ordre, scopes, expiration, échec de lecture non bloquant, rollback des jetons, reprise et run déjà démarré. Test core : les reçus invalides deviennent inconnus sans faire disparaître le run. Tests UI du contexte et de la transcription : 40 réussis. Build Go, lint ciblé core/views et premier typecheck global (9/9) réussis ; vérification visuelle et vérification finale consignées ci-dessous.

Documentation agents dans les quatre langues et skill intégré actualisés. Une insertion précédente du paragraphe History au milieu du tableau Access anglais a été déplacée dans la section Memory.

Recherche continue : DSPy/GEPA ajouté comme piste documentaire au rapport Kokpit, sans modifier les huit décisions de l’audit de code. Méthode retenue : contrat indépendant du modèle, candidats issus des traces et jeu final séparé des données de sélection. Pas d’installation ni de benchmark fournisseur. Hypothèse à tester : changement d’exécutant accompagné d’une comparaison sur les cas de l’équipe. Les métriques, procédures/replay, Inbox, budgets, activation, recette complète et mobile restent ouverts.

Vérification finale de cette tranche : typecheck global 9/9 en 36 s après correction d’un tuple readonly dans le nouveau test UI. Rendu du dialogue de transcription réel avec HTTP simulé et CSS actuel compilé : captures desktop, 390 px et bas de liste projet inspectées dans `/tmp/vigil-memory-context/`, police Inter chargée, aucune erreur de page, largeur 390/390 et liste de 50 références limitée à 256 px avec défilement. Le harness exige les providers Query, workspace et navigation ; ils sont présents dans la preuve finale. Aucun run fournisseur réel n’a été lancé.

**Défaut d’interface restant identifié pendant la vérification** : après réduction d’une fenêtre déjà ouverte de 1200 à 390 px, le popover existant des détails peut rester décalé et déborder à droite. Un chargement initial à 390 px positionne correctement le même popover. La correction du repositionnement du composant partagé reste à auditer ; ne pas déclarer la réactivité globale terminée. Le serveur de preview est arrêté et le harness archivé hors dépôt après les captures.

### 5 septembre — correction du popover après redimensionnement

Le défaut précédent est maintenant corrigé à sa cause : `DialogContent` portait `duration-100` sans restriction de propriété, ce qui activait une transition CSS de toute sa géométrie. Le calcul du popover pouvait conserver la position intermédiaire du bouton pendant la réduction de la fenêtre. Un événement scroll relançait le calcul correctement ; retirer temporairement la transition du dialogue supprimait le défaut sans toucher au verrouillage du scroll.

Le composant partagé utilise désormais `transition-none`. Les animations d’ouverture/fermeture par keyframes sont conservées. Aucun écouteur resize ni mécanisme supplémentaire n’a été ajouté. Les dialogues web/desktop héritent de ce comportement ; les transitions explicitement passées par les appelants restent des choix propres à ces appelants.

Régression exécutable : `node scripts/check-dialog-resize.cjs`. Elle compose les vrais Dialog/Popover, compile le CSS web actuel, lance Chromium sans API ni base, ouvre une liste de 50 versions, conserve un brouillon, redimensionne 1200→390→1200→500→390 et vérifie le rectangle, le scroll horizontal et le retour de focus via Échap. Elle échouait avant correction (popover de 320 px placé à x=343 dans une fenêtre de 390 px), puis passe après. Les fichiers temporaires et le serveur sont nettoyés par le script.

Le dialogue de transcription complet a aussi été reproduit après correction : largeur 390/390 après redimensionnement sans rechargement, aucun débordement, aucun pageerror et capture inspectée dans `/tmp/vigil-memory-context/`. Les 51 tests UI mémoire/transcription passent, ainsi que le lint du composant et `git diff --check`. Le défaut de repositionnement signalé à la tranche précédente est donc clos pour le cas reproduit.

### 5 septembre — statistiques d’usage de mémoire

Les statistiques sont maintenant exposées par `GET /api/agents/{id}/memories/usage`. Une agrégation SQL unique compte les runs démarrés sur 30 jours, distingue contextes enregistrés/inconnus, échec de chargement et mémoire d’agent préparée, puis détaille chaque ID/révision. Le contexte est estampillé côté base avec une identité de chat durable avant que la suppression d’un chat puisse détacher sa clé ; les anciens runs d’origine ambiguë sont exclus. Le workspace, l’agent et la même gate d’accès que l’historique des runs sont appliqués.

La carte web/desktop affiche la couverture, l’état vide, l’erreur avec reprise, les références de versions et réutilise le dialogue d’historique existant. Elle se rafraîchit chaque minute et précise que le contexte préparé ne prouve ni l’utilisation par le modèle ni une amélioration. Tests ciblés Go avec `-race`, core, views, build Go, typecheck 9/9 et rendu Chromium desktop/mobile passent. Le rendu inspecté est conservé dans `/tmp/vigil-memory-usage/`. La suite globale lancée en parallèle a été interrompue après des échecs de concurrence et de migrations sur d’autres tests ; elle n’est pas une preuve de régression de cette tranche. La validation sur cas indépendants reste ouverte.

### 6 septembre — vérification et revue indépendante du lot mémoire

Le workflow `astra-advisor:orchestration` a été appliqué au contexte du dernier dispatch et aux statistiques mémoire. Tests Go ciblés avec race detector, 16 tests core/views, compilation serveur, typecheck (10 tâches dont 7 en cache), diff et parcours Chromium ont été revérifiés, avec une seule commande lourde à la fois. La revue indépendante en lecture seule a rendu `ship`, sans problème bloquant ; aucune correction de code supplémentaire. Voir [le compte rendu et ses limites](astra-memory-review-2026-09-06.md).

Cette acceptation porte sur le code du lot, pas sur l'efficacité de la mémoire sur des tâches indépendantes. Les autres lignes du registre restent ouvertes dans leur périmètre actuel ; aucune acceptation globale du worktree ni exclusivité commerciale n'est déduite de cette revue.

### 6 septembre — filtre « Action required » dans Inbox

L'inspection confirme que l'Inbox expose des notifications avec une sévérité, mais pas un contrat de décision structurée/réponse/reprise. Le filtre ajouté réutilise `severity=action_required` sur la notification la plus récente affichée par issue. Il conserve les notifications déjà lues et celles sans issue, se combine avec les filtres existants, isole les préférences par workspace et fonctionne aussi dans Archives. Le compteur tient compte des autres filtres. Aucun changement de droits, d'API, de base ou d'exécution d'agent.

67 tests ciblés réussis (18 core, 49 views), lint ciblé, typecheck en série (10 tâches, dont 3 en cache) et `git diff --check` passent. Le harness Chromium `/tmp/vigil-inbox-action/check.cjs` vérifie le menu réel avec une liste fixture et une API simulée : filtre, conservation des notifications lues, combinaison avec les non-lues, compteurs, effacement et état vide. Capture inspectée : `/tmp/vigil-inbox-action/phone.png` ; police chargée, aucune erreur JavaScript, document de 390 px pour un viewport de 390 px. Le raccordement de la page Inbox et d'Archives est couvert par les tests de composant. Ce n'est pas un parcours E2E avec le backend réel. Documentation et libellés mis à jour dans les quatre langues ; le filtre concerne web/desktop, pas l'application mobile native.

Revue indépendante terminée : `/root/inbox_action_review`, verdict `ship`, aucun problème signalé ; modèle demandé `gpt-5.6-luna`, effort `high`, lecture seule. Les 13 fichiers du lot sont restés identiques pendant la revue (empreintes dans `/tmp/vigil-inbox-action/review-files.json`). Le modèle et l'effort réalisés ne sont pas exposés par les métadonnées publiques. La consommation en tokens du parent/reviewer n'est pas observable : reçu de coût API indisponible, aucune économie revendiquée.

La file de décisions reste ouverte : un commentaire ultérieur peut remplacer une notification d'action sans résoudre sa demande. Pour fermer cette exigence, il faut une identité de décision durable, une réponse protégée contre les réponses concurrentes et un accusé de reprise vérifiable ; lire ou archiver une notification ne doit pas servir d'approbation implicite.

### 6 septembre — décisions durables et reprise explicite

Les agents peuvent désormais adresser une question structurée à un humain via `multica issue decision request`. La demande conserve son identité, sa source et ses options indépendamment des notifications. L'Inbox web/desktop sépare les demandes à traiter de l'historique ; seuls les destinataires humains peuvent répondre ou annuler. La réponse est finale, attribuée et protégée contre deux écritures concurrentes. Une relance distincte écrit le nouveau run et son reçu dans la même transaction ; les essais répétés récupèrent ce reçu sans doubler la reprise. Les erreurs runtime gardent la réponse.

La reprise prépare une nouvelle session avec le contexte et la réponse, sans suspendre/réveiller un processus CLI en cours ni approuver ses outils ou sa livraison. Les droits et la provenance hors chat sont vérifiés. La suppression des issues/workspaces retire les demandes explicitement, sans nouveau FK. Les index sont concurrents dans des migrations séparées.

Vérifications et périmètre exact : [rapport de la tranche](astra-decisions-review-2026-09-06.md). Go avec détection de concurrence, 53 tests TS ciblés, typecheck des dix packages, lint, compilation serveur/CLI, migrations nouvelles aller/retour et Chromium passent. La capture 390 px et les états historique/vide ont été inspectés. Le navigateur utilise une API simulée ; PostgreSQL et le handoff du claim sont testés séparément. La première revue a identifié une fuite de réponse par retry de création humaine vers un autre destinataire ; correction et test de non-régression passent. La seconde revue fraîche (`decision_review_final`, Terra/high demandés, exécution effective non observable) rend **ship** sans finding. Aucun appel fournisseur, commit ou déploiement.

Cette tranche ferme le manque d'identité de décision signalé après le filtre Inbox. La validation indépendante des procédures reste le prochain axe différenciant à construire ; la recette complète, les budgets, la promotion mémoire de projet et le mobile restent ouverts. Aucune exclusivité commerciale n'est revendiquée.

### 6 septembre — première comparaison indépendante de mémoire, hors ligne

`multica agent memory evaluate` fige une mémoire en attente, les mémoires actives et les fixtures d’une suite humaine. Chaque cas est exécuté successivement sans puis avec la candidate ; un conteneur neuf vérifie le résultat avec des tests non montés chez l’exécutant. Le moteur exige des cas de replay et de validation distincts et ne démarre aucun agent fournisseur. Les sorties, délais, empreintes et versions sont conservés dans un répertoire local privé. Les erreurs techniques et régressions empêchent l’adoption ; un gain est requis dans chaque groupe, avec tous les contrôles candidats réussis.

`agent memory adopt --reviewed` relit le contexte courant et utilise l’API humaine existante avec la révision attendue. `agent memory restore` restaure une version antérieure. Le rapport est local et non signé ; il n’est pas une attestation serveur. Les modifications concurrentes d’autres mémoires après la lecture préalable ne sont pas verrouillées atomiquement. Le coût et les interventions humaines restent inconnus, car non instrumentés par ce runner hors ligne.

Tests Go avec `-race`, comparaison réelle dans Docker avec exécutant shell déterministe, limite de sortie, timeout, contrôle HTTP d’adoption, compilation CLI et conformité de tous les skills intégrés passent. Le test Docker démontre le fonctionnement du mécanisme, pas l’amélioration d’un LLM. Le dépassement antérieur de la limite de longueur du skill issues est résolu en déplaçant le détail des décisions dans sa référence. [Mode d’emploi](../development/memory-evaluation.md) et [preuves de revue](astra-memory-evaluation-review-2026-09-06.md).

La validation du runtime produit sur des cas métier, les mesures de coût/effort, la conservation serveur et l’interface de comparaison restent ouvertes ; la campagne complète n’est pas déclarée terminée.

Revue fraîche `/root/memory_evaluation_review` terminée : **ship**, aucun finding bloquant ou avertissement. Terra/high demandés ; modèle, effort réalisés et tokens non observables. Acceptation limitée à ce banc d’essai hors ligne ; aucune preuve de gain commercial ou d’efficacité LLM déduite des tests.


### 6 septembre — conservation et revue des évaluations

Les rapports du banc hors ligne peuvent être conservés côté serveur, importés et examinés dans Mémoire → Évaluations sur web/desktop. Le détail expose résultats appariés, sorties, contrôles et empreintes ; export et suppression sont disponibles. Le serveur réserve ces opérations aux gestionnaires humains, recalcule les critères et vérifie les versions enregistrées. La limite est de dix rapports de 2 MiB par mémoire. Les rapports incomplets restent consultables et non adoptables.

L’adoption depuis un rapport vérifie désormais la candidate et tout le contexte actif sous le verrou commun des écritures mémoire, puis enregistre nouvelle version et reçu dans la même transaction. Cela ferme la course entre la lecture préalable du CLI et une modification de baseline signalée dans la tranche précédente. Supprimer une mémoire supprime aussi les rapports qui contiennent son texte comme baseline. La suppression agent/workspace nettoie explicitement les rapports.

Ces résultats restent fournis par un humain, sans attestation d’exécution. Les exécutions du runtime produit, les coûts et l’effort humain réels, la parité mobile et la validation commerciale restent ouverts. Voir [le rapport de vérification de cette tranche](astra-memory-evaluation-storage-review-2026-09-06.md).

Validation de cette tranche : Go avec détection de courses, 17 tests TS ciblés, lints core/views, typage global en série, compilation serveur/CLI, migrations aller-retour en base isolée et rendu Chromium avec export vérifié passent. Revue fraîche `/root/memory_evaluation_storage_review` : **ship**, aucun défaut bloquant ; rapports supprimables, donc pas de rétention d’audit immuable. Les 41 fichiers examinés sont inchangés pendant la revue. Modèle/effort demandés Terra/high ; réalisation et consommation non observables. Aucun fournisseur réel, commit ou déploiement.


### 6 septembre — raccordement runtime et préparation du pilote

Implémentation vérifiée et revue indépendante acceptée : réutilisation du prompt run-only, du contexte runtime et de l’adaptateur Claude dans les conteneurs du banc d’essai. Les tests utilisent uniquement un faux CLI. Le rapport conserve les empreintes, les réglages demandés, les événements outils et les tokens rapportés. Un bilan séparé lie les acceptations humaines, temps explicitement relevés et coûts sourcés à l’empreinte exacte du rapport ; aucune conversion de données manquantes en zéro.

Cette étape prépare le pilote ; elle ne valide pas un LLM réel et ne reproduit pas le scheduler, les API authentifiées, les outils MCP ou la reprise de sessions. L’exécution connectée aux fournisseurs et le parcours entièrement lancé depuis l’interface restent ouverts.


Go avec détection de courses, comparaison réelle dans Docker avec faux CLI, validation et conservation serveur, 14 tests TS ciblés, typage global (9 tâches dont 5 en cache), lints, compilation CLI et parcours Chromium passent. Captures inspectées à 390 px : aucun débordement, police chargée, aucun pageerror. Le navigateur utilise une API simulée ; les vérifications PostgreSQL sont séparées. Aucun appel fournisseur ni changement de migration. [Preuves et limites de cette tranche](astra-memory-runtime-review-2026-09-06.md).

La première revue a trouvé un rapport potentiellement accepté mais illisible : des empreintes hexadécimales en majuscules passaient le serveur et échouaient au parsing du client. La validation commune exige désormais la forme minuscule canonique. Le test d’import refuse les trois empreintes concernées ; les tests Go avec détection de courses et la compilation CLI repassent. Seconde revue fraîche `/root/memory_runtime_review_final` terminée : **ship**, aucun finding. Les 23 fichiers du lot sont inchangés après la revue. Terra/high demandés ; modèle, effort réalisés et consommation non observables. Aucun gain de coût ou de qualité LLM revendiqué.

### 6 septembre — comparaisons mémoire connectées depuis l’interface

La fenêtre Évaluations web/desktop lance désormais une comparaison textuelle sur le compte d’un runtime Claude ou Codex connecté et à jour. Un gestionnaire humain ayant aussi les droits sur le runtime choisit deux questions distinctes et leurs réponses attendues. Les quatre runs s’exécutent successivement, sans puis avec la candidate pour chaque cas ; les réponses attendues sont exclues des runs et conservées dans les rapports humains pour revue et export. Le serveur calcule les résultats, conserve les résultats partiels et réutilise le contrôle atomique des versions lors de l’adoption humaine.

La file durable réutilise la table des rapports. L’identifiant de requête récupère un lancement incertain ; un claim n’est jamais rejoué automatiquement après un crash. Les doublons de résultats identiques sont reconnus, les modifications refusées. Annulation, délais, contrôles d’accès et limitation à une évaluation par daemon sont implémentés. Ce mode utilise les permissions locales et les adaptateurs réels dans des dossiers temporaires ; il ne constitue pas une sandbox OS ni un benchmark de dépôt. Les tests utilisent des exécutables simulés et ne mesurent pas la qualité d’un LLM.

Typage global, 17 tests TS ciblés, tests Go avec détection de courses, lints, compilation serveur/CLI, migrations 496–498 aller-retour dans la base isolée, conformité des skills et parcours Chromium passent. Aucun appel fournisseur payant, déploiement ou migration de production. Les runtimes et le serveur devront être mis à jour pour exposer ce parcours. La première revue a identifié un contrôle d’identité de daemon manquant sur claim/report. Les deux routes vérifient désormais le daemon du runtime, ou les droits propriétaire/admin pour les anciens tokens utilisateur. Le test refuse également un daemon sans identité accompagné d’un faux en-tête utilisateur. Les tests avec détection de courses et la compilation repassent ; la deuxième revue a relevé une formulation ambiguë sur les réponses attendues. Les libellés précisent leur exclusion des runs et leur présence volontaire dans les rapports humains ; les vérifications de ce contrat passent. Troisième revue fraîche `/root/memory_connected_acceptance` : **ship**, aucun problème bloquant. Les 43 fichiers du lot sont inchangés après revue. Modèle/effort demandés Sol/high ; réalisation et consommation non observables. Aucun gain de qualité LLM ou coût réel revendiqué. [Preuves et limites](astra-memory-connected-review-2026-09-06.md).


### 6 septembre — pilote réel et critères métier

Le serveur, le web et le runtime locaux sont actifs. L'interface lance des comparaisons avec texte exact, JSON structuré ou tests de fonctions JavaScript dans le runner Docker isolé. Le serveur conserve les observations liées au code, aux critères et à l'image ; les doublons et lectures ne réexécutent pas le code. Aucun schéma de base supplémentaire.

Sur deux cas synthétiques, la comparaison complète produit 0/2 réussites sans mémoire et 2/2 avec mémoire, sans erreur. Un premier essai interrompu par le vérificateur est conservé : sept réponses fournisseur au total. L'export, l'adoption en révision 3 et la restauration en révision 4 en attente de revue ont été exercés dans le navigateur sur la seule mémoire de test. Le coût facturé et les corrections/temps humains restent inconnus. Ce petit pilote ne démontre ni généralisation ni avantage commercial. [Rapport, preuves et limites](memory-pilot-2026-09-06.md).

### 7 septembre — livraison mobile v1.5 (accept / corrections)

Sur la fiche issue native : carte Delivery review avec honnêteté board ≠ accept ≠ merge/deploy, critères, résultat, PR liées, accept avec preuves par critère, demande de corrections, lancement de correction, Done optionnel via Alert après accept. Coupures volontaires vs web : pas d’historique paginé, pas de dialogue coût, pas de teach-memory / transcript dans cette tranche. Clés et schémas alignés sur `packages/core/issues/delivery.ts`. Tests clés/schémas + messages d’erreur 409 : 6/6. `pnpm exec tsc --noEmit` dans `apps/mobile` : OK. Captures simulateur : checklist humaine [mobile-delivery-smoke-checklist-2026-09-07.md](mobile-delivery-smoke-checklist-2026-09-07.md).

### 7 septembre — activation : télémétrie checklist (abandons / délai)

Signal client `activation_checklist_viewed` quand la carte Runtimes s’affiche encore bloquée : étapes requises bloquées + `minutes_since_onboarding` si `onboarded_at` connu. Helpers `blockedRequiredStepIds` / `minutesSinceIso`. Jointure PostHog : [activation-abandon-funnel-2026-09-07.md](activation-abandon-funnel-2026-09-07.md).

Parcours rendu Chromium (API simulée) : `node docs/research/activation-readiness-preview-2026-09-07.cjs` → captures inspectées `/tmp/vigil-activation/` (blocked desktop/phone 390 px sans débordement, Inter chargé, checklist masquée quand prêt). Copie N/A pour CLI auth quand aucun Claude/Codex en ligne. Tests core/views passés. Pilote réel &lt;10 min avec équipes cibles reste ouvert.

### 7 septembre — causes d’attente et reprises (journal d’exécution)

`wait_reason` est désormais sérialisé sur `AgentTaskResponse` (même gate que le chat : uniquement en `waiting_local_directory`). Helpers `liveWaitReason` / `activeRunGuidanceKind` / `MANUAL_RETRY_PRESERVES` dans `packages/core/issues/run-guidance.ts`. Le journal d’exécution affiche la cause sur la ligne active et l’action en title ; le tooltip Retry dit explicitement ce qui est conservé (workdir si disponible, session si non empoisonnée) et que les effets externes ne sont pas annulés. Troubleshooting en/zh/ja/ko mis à jour.

### 7 septembre — revue honnête / autorisation d’action

Contrat soft signal vs hard gate figé dans `authorization-frontiers.ts` + tests ; section dédiée dans security-model (4 langues). Preuve : [honest-review-authz-2026-09-07.md](honest-review-authz-2026-09-07.md). Merge/deploy restent hors registre Multica (unknown). L’UI livraison et issues.mdx étaient déjà honnêtes sur Accept ≠ merge.

### 7 septembre — recette bug → PR (composition initiale)

Checklist pure `deriveBugFixRecipeReadiness` + stages `reproduce → fix → test → open_pr → delivery_review`. Guide [bugfix-recipe.md](../development/bugfix-recipe.md). Fixture Node `docs/development/bugfix-recipe-fixture/` avec `prove.mjs` (échec puis succès, zéro fournisseur). Tests unitaires readiness/stages.

### 7 septembre — pilote agent réel bug → PR → Accept

Dépôt privé `jeffdev2018/multica-bugfix-pilot`, projet + `local_directory`, agent Claude **Bugfix recipe pilot**, issue **DEV-1**. Run réel 57s (11 tools), PR [#1](https://github.com/jeffdev2018/multica-bugfix-pilot/pull/1), Accept livraison humaine avec preuves par critère (~$0.65 estimé). Preuve : [bugfix-recipe-pilot-2026-09-07.md](bugfix-recipe-pilot-2026-09-07.md). Limite : `pull_requests[]` Multica vide sans GitHub App ; Accept ≠ merge (PR laissée ouverte). Self-pilote activation ~4 min : [activation-self-pilot-2026-09-07.md](activation-self-pilot-2026-09-07.md). Mobile simu bloquée sans Xcode : [mobile-delivery-smoke-pilot-2026-09-07.md](mobile-delivery-smoke-pilot-2026-09-07.md).

### 7 septembre — mémoire projet sourcée depuis correction

`source_review_id` sur PUT project memory (owner/admin), même preuve figée que l’agent memory, issue liée au projet, décision `changes_requested`, identité unique par (projet, review). Migrations 499–500. Bouton **Promote to project memory** sur la carte livraison. Tests Go `TestProjectMemoryPromotionFromDeliveryCorrection`, core schema, UI promote (échec puis reçu). Source-map + SKILL projects mis à jour.

### 7 septembre — mesure de réutilisation mémoire projet

GET `/api/projects/{id}/memory/usage` (couverture 30 j, `prepared_runs` par révision) + carte UI. Preuve pilote DEV-2 : `memory_context.project_version.revision=1`, agent cite les règles, usage `runs_with_project_memory=1`. Note : [project-memory-reuse-2026-09-07.md](project-memory-reuse-2026-09-07.md). Reste : réduction mesurée des reprises (l’« apprentissage autonome » n’est pas une feature produit — voir DEV-6).

### 7 septembre — pilote mémoire connectée Claude (runtime produit)

Agent dédié sans outils, runtime Claude local `dev`, deux cas synthétiques (replay + holdout). Comparaison connected : **0/2 sans candidate, 2/2 avec**, `eligible=true`, adoption révision 3 puis restauration révision 4 pending. Premiers essais échoués documentés (`MaxTurns: 1` + tool call ; gain holdout seul insuffisant pour le gate). Tokens d’adaptateur relevés. Revue humaine opérateur : **18 s** (2× baseline rejetées, 2× candidate acceptées) via `pilot-summary --reviews` ; USD facturé toujours `null`. Observations ≠ attestation serveur. Preuve : [memory-connected-claude-pilot-2026-09-07.md](memory-connected-claude-pilot-2026-09-07.md).

### 7 septembre — qualité globale (passage typecheck + correctifs)

`pnpm typecheck` 9/9. Correctifs campagne + flakes Node 25 / agent host (manifeste delete, localStorage, concurrent index hooks, GitEnv, ping migrate, JoinPage, sidebar-resize spy). Rejeux isolés : daemon OK, migrate/repocache OK, core/web/desktop OK en turbo ; views 418/4901 OK en run seul. Run jumelé make∥pnpm bruité. Note : [quality-global-2026-09-07.md](quality-global-2026-09-07.md).

### 7 septembre — hypothèse killer feature (recalage)

Sources Paperclip / Hermes / Letta reconsultées. Le mécanisme Multica (éval connectée + adopt/restore) existe en produit local ; la promesse commerciale (temps de supervision sur tâches réelles) reste **non démontrée**. Concurrent : leçons + approve-write saturés ; l’écart candidat reste la preuve exécutable avant promotion. Test falsifiable et seuils : [killer-feature-hypothesis-2026-09-07.md](killer-feature-hypothesis-2026-09-07.md).

### 7 septembre — killer feature : blocage commercial explicite

Aucun outreach / pilote équipe / paiement n’est autorisé dans le mandat campagne. La recherche passe à **en attente de terrain** (pas incomplete technique). Effort humain synthétique du pilote Claude enregistré (18 s). Déblocage : autoriser un contact + exécuter le protocole falsifiable inchangé.

### 7 septembre — killer feature : terrain autorisé

Jeff autorise outreach/pilote. Runbook + brouillon email : [killer-feature-field-pilot-2026-09-07.md](killer-feature-field-pilot-2026-09-07.md). Prochaine étape humaine : **nommer l’équipe #1** (contact), puis figer 10+5 cas avant toute sortie Multica.

### 7 septembre — killer feature : simulation Northline (inventée)

À la demande « invente », persona **Northline Labs** + suite figée + 3 tentatives d’éval connectée. Tent. 3 (tokens opaques) : **0/2→2/2, eligible**. Revue sim 24 s. **Ce n’est pas un pilote terrain** — pas d’outreach, pas de paiement, hors seuils ≥2 équipes. Note : [killer-feature-sim-northline-2026-09-07.md](killer-feature-sim-northline-2026-09-07.md).

### 7 septembre — mémoire agent sur cas indépendants (issue runs)

Adoption rev4 de la mémoire Northline éligible. **DEV-3** → commentaire `harbor-17` ; **DEV-4** → `NL-ADD-9`. Les deux runs : `memory_context.agent_status=loaded`, version `fc3905fd` rev4 préparée ; usage `runs_with_agent_memory=2`. Indépendance = surface issue (pas le worker d’éval) ; tokens déjà vus en replay/holdout. Preuve : [agent-memory-independent-2026-09-07.md](agent-memory-independent-2026-09-07.md).

### 7 septembre — mémoire agent : fait never-eval (DEV-5)

Nouvelle mémoire `9595267c-…` : éval sur `moss-2`/`pine-8` seulement (0/2→2/2, eligible), adopt rev3 ; **DEV-5** demande `fern-1` (jamais dans la suite) → commentaire exact, `agent_status=loaded`. Preuve : [agent-memory-unseen-fact-2026-09-07.md](agent-memory-unseen-fact-2026-09-07.md).

### 7 septembre — reçu serveur `report_hash` sur les évaluations mémoire

Les réponses list/detail/import/run d’évaluations exposent `report_hash` (SHA-256 des octets de rapport stockés). UI web/desktop l’affiche ; schéma client optionnel pour compat backends anciens. Ce reçu prouve l’identité du rapport côté Multica ; ce n’est **pas** une attestation fournisseur de modèle ni de facturation.

### 7 septembre — coût catalogue estimé sur évals mémoire connectées

Quand le runtime rapporte de l’usage et que chaque modèle est dans le catalogue Multica, chaque outcome reçoit `cost_usd` + `cost_source=catalog_estimate` ; la réponse API agrège `estimated_cost_usd` / `cost_status` (`unavailable` | `estimated` | `partial`). Affichage UI honnête (pas une facture). Les imports hors ligne refusent toujours un coût inventé.

### 7 septembre — mémoire projet cross-agent (DEV-6) + honesty « apprentissage autonome »

Publication humaine rev **2** (`quay-77`) sur le projet Bugfix. Agent **Bugfix recipe pilot** (pas le pilote mémoire agent) : aucune mémoire agent avec ce token. **DEV-6** → commentaire `quay-77`, `project_version.revision=2`, usage projet `runs_with_project_memory=5`. Conclusion produit : partage cross-agent via publish humain **oui** ; apprentissage autonome daemon→projet **non** (hors scope UI/API). Preuve : [project-memory-cross-agent-2026-09-07.md](project-memory-cross-agent-2026-09-07.md).

### 7 septembre — qualité globale séquentielle

Suite ordonnée typecheck → core → views → web/desktop/docs → make test (sans jumelage). Typecheck + core/web/desktop/docs verts. Views full : 38 timeouts ; retry fichiers + solo → verts. Go : flake `TestWebhookHandler_DedupeViaIdempotencyKey` puis PASS isolé. Verdict : signal hôte encore bruité ; pas d’oracle one-shot. Note : [quality-global-sequential-2026-09-07.md](quality-global-sequential-2026-09-07.md).

### 7 septembre — dogfood « faire soi-même » (équipe #1 + effort + GitHub)

Équipe #1 nommée en répétition fondateur : [killer-feature-team1-dogfood-2026-09-07.md](killer-feature-team1-dogfood-2026-09-07.md). Instrument `human_effort_seconds` (migration 501, API/UI web-desktop, timer à l’ouverture du formulaire Accept). GitHub App : credentials `.env` vides → `pull_requests[]` toujours vide ; diagnostic [github-app-dogfood-2026-09-07.md](github-app-dogfood-2026-09-07.md).

### 7 septembre — finalisation 4–7 (mobile effort, retry↓, build, commit)

Parité mobile `human_effort_seconds` (API body + timer + affichage). Pilote reprises projet `keel-4` : 2/2→0/2 (−100 % relatif) — [memory-retry-reduction-2026-09-07.md](memory-retry-reduction-2026-09-07.md). `pnpm build` OK ; Playwright non exécuté (Docker Desktop indisponible après `make down`).

