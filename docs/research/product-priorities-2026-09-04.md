# Priorités produit Multica — 4 septembre 2026

> Décision utilisateur : les améliorations, la mémoire et les fonctionnalités proposées sont acceptées comme travail à réaliser. Elles ne sont pas validées comme facteurs suffisants de migration ou d'achat. La recherche de différenciation commerciale se poursuit ; cette note ne constitue pas une preuve d'implémentation.

## Périmètre et recommandation

Analyse de la documentation et de points ciblés du code du checkout local, complétée par des sources concurrentes officielles. Aucun test fonctionnel, audit visuel, entretien utilisateur ou accès aux métriques de production. Les capacités présentes dans le code ne sont pas nécessairement déployées. Les absences évoquées sont des parcours non établis par les éléments examinés, pas une preuve exhaustive d'absence.

Hypothèse de cible : petites équipes de développement, conformément à [CLAUDE.md](../../CLAUDE.md) et au [README](../../README.md). Recommandation : concentrer le produit sur une livraison vérifiable et une supervision courte. Promesse à tester : « Confiez une tâche à vos agents ; recevez un résultat vérifiable, avec les décisions et le coût au même endroit. »

## Ce qui existe déjà

- Agents assignables, squads, skills, chat, projets, runtimes, intégrations Git et canaux de discussion : [README](../../README.md).
- Sous-tâches organisées en étapes et réveil du parent : [Issues](../../apps/docs/content/docs/issues.mdx).
- Autopilots sur horaires et webhooks, historique, déduplication et pauses après échecs répétés : [Autopilots](../../apps/docs/content/docs/autopilots.mdx).
- Logs, reprises et distinction entre fin d'exécution et objectif atteint : [Runs](../../apps/docs/content/docs/tasks.mdx).
- Statistiques d'usage, de durée et d'échecs : [dashboard.go](../../server/internal/handler/dashboard.go).
- Mémoire éditable par agent : [memory-tab.tsx](../../packages/views/agents/components/tabs/memory-tab.tsx). Extraction après exécution : [agent_memory_extract.go](../../server/internal/service/agent_memory_extract.go). Le type mémoire comporte déjà une provenance `source_task_id` : [agent.ts](../../packages/core/types/agent.ts).

Ne pas présenter ces fonctions comme de nouvelles idées à construire.

## Fonctionnalités à privilégier

| Priorité | Proposition | Première version utile | Valeur et mesure | Point faible |
|---|---|---|---|---|
| 1 | Livraison vérifiable dans l'issue | Carte avec critères d'acceptation, livrables, preuve par critère, réserves, coût disponible, accepter/demander une correction | Réduire le temps de revue et les réouvertures | Fiabilité et fraîcheur des preuves ; relier les tests au commit concerné |
| 2 | File de décisions dans l'Inbox | Filtre « décision requise », question précise, contexte, options et reprise de la tâche après réponse | Réduire le temps bloqué et les interruptions par résultat accepté | Reprise fiable et absence de déclenchement en double |
| 3 | Une recette complète prête à l'emploi | Bug reproductible → correction → test → PR → revue, en réutilisant skills, projets et Autopilots | Accélérer le premier résultat accepté et mesurer la réutilisation | Accès au dépôt, authentification et qualité du cas initial |
| 4 | Économie par résultat | Ajouter coût cumulé des reprises et acceptation aux statistiques ; alertes puis plafonds selon capacités | Coût par tâche acceptée, comparaison sur tâches comparables | Couverture hétérogène des coûts ; distinguer connu, estimé et indisponible |
| 5 | Mémoire de décisions du projet | Promouvoir une correction humaine en règle partagée, approuvée, sourcée et révocable | Réduire les corrections répétées et le temps de recontextualisation | Pertinence, portée et vieillissement des règles |

La première version de la livraison vérifiable doit agréger les traces et intégrations existantes. Un résumé généré par l'agent ne constitue pas une preuve de réussite ; l'acceptation reste distincte du statut technique du run. Pas besoin d'un nouvel orchestrateur pour commencer.

La mémoire proposée étend la mémoire par agent existante et sa provenance. Commencer par une promotion manuelle explicite des décisions au niveau projet, avant d'automatiser l'apprentissage partagé.

### Scoring exploratoire

Scores de jugement, pas mesures de demande ou estimations de planning. Impact : temps gagné, valeur du temps libéré, réduction d'erreurs, extensibilité, chacun /5. Faisabilité : standardisation et accès aux données, chacun /5. Effort inverse : /5, élevé = plus simple. Les contrôles de budget et les agrégations de statistiques relèvent de code déterministe ; réserver l'agent à l'investigation et à la rédaction contextuelles.

| Candidat | Impact /20 | Faisabilité /10 | Effort inverse /5 | Total /35 | Catégorie | Décision |
|---|---:|---:|---:|---:|---|---|
| Livraison vérifiable | 19 | 8 | 4 | 31 | Gain rapide, périmètre réduit | Premier chantier |
| File de décisions | 18 | 8 | 4 | 30 | Gain rapide si reprise existante réutilisable | Ensuite |
| Recette bug → PR | 17 | 9 | 5 | 31 | Gain rapide | Cas pilote des deux premières |
| Coûts et plafonds | 16 | 6 | 3 | 25 | Projet à cadrer | Résoudre la couverture des mesures |
| Mémoire de projet | 17 | 6 | 2 | 25 | Projet stratégique | Tester la promotion manuelle d'abord |

## Améliorations du produit actuel

1. **Rendre la review explicite et honnête.** Les [statuts](../../apps/docs/content/docs/issues.mdx) sont modifiables directement par membres et agents. Le [modèle de sécurité](../../apps/docs/content/docs/security-model.mdx) précise que les runs héritent des droits du compte du daemon et que les approbations des outils sont automatisées. Un statut `in_review` n'est donc pas un verrou technique sur publication, merge ou déploiement. Distinguer revue du livrable et autorisation d'action. Si un blocage fort est promis, l'appliquer à une frontière contrôlée avec des identifiants restreints ou une isolation externe ; un prompt ou un bouton ne suffit pas.
2. **Améliorer l'activation jusqu'au premier résultat utile.** L'onboarding, la connexion runtime et un parcours Mika existent déjà dans [onboarding-flow.tsx](../../packages/views/onboarding/onboarding-flow.tsx). Mesurer les abandons, puis rendre la préparation lisible : machine connectée, CLI présent, authentification valide, dépôt accessible, tâche de démonstration prête. Réduire la configuration avancée avant ce résultat, sans reconstruire l'onboarding. Objectif pilote à tester : un premier résultat vérifiable en moins de dix minutes.
3. **Expliquer les attentes et les reprises.** La [documentation de dépannage](../../apps/docs/content/docs/troubleshooting.mdx) distingue runtime hors ligne, capacité occupée, verrou de répertoire, authentification et quota. Montrer la cause et l'action adaptée à côté du run. Les retries existent déjà ; expliciter ce qu'une reprise conserve et éviter les effets externes dupliqués.
4. **Distinguer activité, livraison et réussite.** Les [runs](../../apps/docs/content/docs/tasks.mdx) terminés ne prouvent pas l'objectif atteint. Ajouter acceptation, réouverture et temps humain de revue aux indicateurs techniques existants. Ne pas assimiler une PR fusionnée à une qualité démontrée sans contexte.
5. **Clarifier la progression des projets.** La [formule actuelle](../../apps/docs/content/docs/projects.mdx) inclut les issues annulées dans la progression. Ce choix mesure une clôture de périmètre. Afficher séparément livré et annulé, ou nommer le chiffre « périmètre clôturé », pour éviter d'interpréter 100 % comme 100 % livré.
6. **Optimiser les surfaces pour décider.** À valider par des essais utilisateurs, pas par une affirmation d'audit visuel : résultat avant logs détaillés ; Inbox orientée action ; sur mobile, lecture du livrable, réponse aux blocages et validation en priorité. Conserver la parité des permissions et des statuts.

## Repères concurrentiels

Sources officielles consultées en ligne le 4 septembre 2026. Elles décrivent des capacités documentées, sans essai comparatif.

- Linear propose délégation aux agents, instructions et suivi : [Agents in Linear](https://linear.app/docs/agents-in-linear). Son assistant travaille sur le contexte du workspace avec skills et automatisations : [Linear Agent](https://linear.app/docs/linear-agent).
- GitHub documente une exécution qui examine le dépôt, prépare une branche, exécute tests et linters et produit une PR, avec des métriques de PR : [Copilot cloud agent](https://docs.github.com/en/copilot/concepts/agents/cloud-agent/about-cloud-agent). Des agents tiers sont également documentés en public preview : [Third-party coding agents](https://docs.github.com/en/copilot/concepts/agents/about-third-party-coding-agents).
- Cursor documente des agents distants et des preuves visuelles, notamment captures, vidéos et logs : [Cloud Agents](https://cursor.com/docs/cloud-agent).

Interprétation : un chat supplémentaire, le choix de plusieurs agents, des automatisations ou des captures isolées ne suffisent pas à établir une différence forte. La piste proposée porte sur la cohérence entre critères, preuves, décisions et coût, pour réduire la supervision. Cette recherche ne démontre ni l'unicité de ces idées ni leur demande commerciale.

## Séquence de validation

1. Observer quelques équipes sur le parcours bug → PR et relever la situation initiale : délai vers premier résultat, durée de revue, corrections, réouvertures, coût disponible.
2. Livrer la carte de résultat et l'utiliser sur cette recette unique ; ajouter la file de décisions si les blocages humains dominent effectivement les délais.
3. Comparer des tâches semblables avant/après. Indicateur principal : résultats acceptés par équipe active et par semaine. Garde-fous : réouvertures, temps humain et coûts cumulés des échecs et reprises.
4. Étendre aux budgets ou à la mémoire partagée selon les problèmes observés. Différer nouvel agent provider, marketplace ou constructeur visuel généraliste tant qu'ils ne lèvent pas un obstacle concret.

Aucun code applicatif modifié ; aucun test exécuté pour cette note.
