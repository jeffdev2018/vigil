# Comparaison élargie : où chercher une raison de changer de produit

État au **4 septembre 2026**. Sources officielles vivantes consultées à cette date. Fenêtre visée pour les signaux utilisateurs : 6 août–4 septembre 2026. Étude en cours : cette passe vérifie le positionnement et les fonctions documentées ; elle ne constitue pas un benchmark exécuté ni une preuve de demande commerciale.

## Verdict provisoire

La mémoire, les agents présentés comme des collègues, le choix du fournisseur, les workflows et les validations ne suffisent pas individuellement à défendre une exclusivité. Les références fournies par l’utilisateur élargissent fortement la comparaison : il faut inclure les espaces de travail généralistes, les outils de connaissances et les plateformes de développement. La concurrence comprend aussi les abonnements et outils que l’équipe possède déjà.

## Carte et registre des sources

Toutes les capacités ci-dessous sont **documentées par leur éditeur**, pas vérifiées en conditions réelles. Confiance élevée sur ce que la source déclare ; confiance non établie sur la fiabilité opérationnelle ou le résultat économique. Chaque lien a été consulté le 2026-09-04.

| Produit | Position et capacités documentées | Déploiement / contrôle | Économie et limite de couverture | Source |
|---|---|---|---|---|
| Paperclip | Organisation du travail d’agents ; leçons sourcées, revue humaine, mémoire versionnée | Le billet décrit responsabilités, preuves et conditions d’arrêt | Coût et demande non évalués dans cette passe ; le billet ne prouve pas une supériorité mesurée | [Billet du 18 août 2026](https://www.paperclip.app/blog/agents-good-enough-to-great/) |
| AionUI | Espace de travail avec agent intégré et plusieurs CLI ; automatisation planifiée, accès distant, livrables bureautiques | Application locale et WebUI ; données locales annoncées, à distinguer des appels aux fournisseurs | Gratuit/open source ; consommation fournisseur séparée. Fiabilité des routines et contrôle équipe non testés | [Dépôt officiel](https://github.com/iOfficeAI/AionUi) |
| Traycer | Tâches réunissant agents, discussions/terminaux, spécifications, tickets, revues et différences git | Choix du dossier ou worktree ; contrôles de permissions documentés | Comptes/crédits/équipes présents dans les docs ; prix et efficacité non vérifiés ici. Ne pas confondre avec traycer.co | [Documentation officielle](https://docs.traycer.ai/) |
| NoteGen | Capture de sources, agent utilisant les connaissances locales, notes Markdown et canvas | Stockage local par défaut et fichiers portables ; sources et appels d’outils visibles | Fonctions principales gratuites, sans abonnement ; dons sans contrepartie de priorité. Concurrent adjacent pour la connaissance, pas preuve d’orchestration complète | [Produit](https://notegen.top/en), [financement fourni par l’utilisateur](https://notegen.top/en/donate) |
| Grok Bot | Délégation de travail dans les applications, routines apprises par démonstration, mémoire et collaboration entre bots | Ordinateur du bot, usage desktop/mobile ; approbations présentées | Accès inclus dans des offres éligibles : menace du produit déjà payé. Ne pas déduire les quotas ni garanties de fiabilité de la page marketing | [Lien utilisateur](https://x.ai/bot) |
| Buzz (Block) | Espace partagé humains/agents ; conversations, événements git et workflows dans un journal signé et interrogeable | Auto-hébergement ; identités propres aux agents ; CLI et intégration ACP | Apache 2.0. Le README distingue explicitement fonctions disponibles, raccordement en cours et vision : validations de workflow et mobile sont dans la colonne en cours. Aucun chiffre commercial vérifié | [Lien utilisateur / README](https://github.com/block/buzz) |
| Cumora | Chat d’équipe, agents, mémoire, Kanban et calendrier ; revendication de tâches et registre de coûts | Cloud par agent ou machine propre avec Claude Code/Codex ; limites de sécurité décrites ; iOS bêta, Android à compiler selon README | MIT ; coût de l’offre cloud et demande non vérifiés. Les protections décrites n’ont pas été auditées par cette étude | [Lien utilisateur / README](https://github.com/yetone/cumora) |
| Intent | Spécification partagée, coordinateur et spécialistes, choix des CLI ; revue et git dans le même espace | Tâches isolées dans des espaces de travail ; enchaînement autonome présenté | Prix, déploiement équipe et résultats indépendants non évalués | [Site officiel, redirection depuis Augment](https://intentapp.dev/) |
| OpenHands | Agents de développement, automatisations partageables, déclencheurs GitHub/Slack/Jira/CI | Exécution locale, VM et environnement d’entreprise ; isolation, audit et budgets documentés | Offre développeur et vente entreprise présentées. Déclarations de performance non reprises sans validation indépendante | [Site officiel](https://www.openhands.dev/) |
| Factory | Plateforme d’autonomie pour équipes de développement en entreprise | La page renvoie vers documentation, sécurité et SLA ; limites effectives pas encore étudiées | Vente entreprise explicite. Couverture minimale : ne pas attribuer des fonctions détaillées depuis une page d’accueil courte | [Site officiel](https://factory.ai/) |

Autres substituts à approfondir : Devin, Cursor, Claude Cowork, outils Codex, GitHub Copilot/Agent HQ, Goose/OpenClaw, gestion de projet existante associée à un CLI. Leur présence ici est une file de recherche, pas une assertion d’équivalence fonctionnelle. Ne pas s’arrêter à la liste initiale.

## Ce que cette passe change pour Multica

**Inférence de marché, confiance moyenne :** ajouter une interface multi-agent, de la mémoire et des traces devient une base de comparaison plutôt qu’un motif suffisant pour quitter un outil. Cette inférence vient du recoupement des déclarations ci-dessus, pas de mesures de migration.

**Mécanismes de risque d’abandon, hypothèses :** une interface supplémentaire peut augmenter le travail de supervision ; un concurrent gratuit ou inclus dans un abonnement réduit la valeur d’une simple commodité ; recréer la connaissance de l’équipe constitue un coût de migration. Nous n’avons pas de données de résiliation ni de revenus confirmant ces mécanismes.

**Piste à tester :** prouver, sur les tâches réelles d’une équipe, qu’une correction devient une règle qui évite une récidive, sans introduire de régression et avec un coût total moindre. Cela implique une comparaison avant/après sur le même état initial, des cas réservés à l’évaluation, des preuves consultables et une promotion réversible. Ce parcours complet n’est pas démontré par les seules sources lues ; cela ne signifie pas qu’aucun concurrent ne le réalise.

Ne pas confondre une deuxième opinion d’agent avec une preuve indépendante : il faut des critères fixés avant l’exécution, un environnement comparable et un résultat observable. Ne pas ajouter de mécanisme de replay sans frontière d’exécution réelle.

## Couverture manquante

Aucun essai de produit, achat ou connexion à un compte fournisseur n’a été effectué. Pour chaque produit du tableau, les incidents communautaires datés, la rétention, le temps de revue et la volonté de payer restent non établis. Les témoignages sélectionnés par un éditeur et les étoiles GitHub ne sont pas des preuves de demande. Les tarifs dynamiques et protections opérationnelles nécessitent une vérification dédiée avant une comparaison commerciale chiffrée. La recherche n’est donc pas terminée.

## Test de décision sur deux semaines

Proposition de test, **non exécutée**, aucune prise de contact autorisée à ce stade : cibler cinq équipes de 3–15 développeurs utilisant déjà des agents, avec le responsable technique comme acheteur potentiel. Préparer dix sollicitations et cinq revues de travail réel ; obtenir dix tâches historiques autorisées par équipe et définir les critères d’acceptation avant de voir les résultats.

Offre expérimentale : audit comparatif avec cinq tâches pour établir une correction et cinq cas réservés pour mesurer son effet. Hypothèse de prix à confronter, pas tarif validé : 200 € pour le pilote. Mesurer temps humain de revue, reprises et coût fournisseur, en séparant données connues/estimées/absentes.

Seuils proposés : poursuivre si au moins trois équipes constatent une baisse de 30 % du temps de revue sans régression critique sur les cas réservés et si deux acceptent effectivement le pilote payant. Itérer si le bénéfice est mesuré mais l’achat refusé pour une raison précise. Abandonner cette proposition commerciale si aucun gain reproductible n’apparaît ou si aucune équipe ne paie. Ces seuils sont des choix de test, pas des constats scientifiques.


## Complément du 5 septembre — mémoire utile ou mauvaise habitude

Deux sources historiques pertinentes ont été vérifiées le **2026-09-05** ; elles sont hors de la fenêtre des trente derniers jours et ne doivent pas être présentées comme des incidents actuels.

- [AionUI #3216, ouverte le 6 juin 2026](https://github.com/iOfficeAI/AionUi/issues/3216) : un utilisateur demande le partage des conventions et préférences entre agents/projets pour éviter de les répéter. Classe : signal utilisateur isolé ; confiance moyenne sur la demande exprimée, aucune preuve de volonté de payer ni d’absence actuelle de la fonction. Engagement non évalué.
- [Cumora, notes de coordination](https://github.com/yetone/cumora/blob/main/docs/COORDINATION.md) : le retour d’expérience des essais du 3 juin décrit des agents enregistrant des règles inadaptées à partir d’un scénario, puis réutilisant ces règles. Le document rapporte aussi les corrections et les essais réussis ; certains commits cités appartiennent à une histoire privée non consultable. Classe : retour technique de l’éditeur, confiance moyenne, non reproduit ici. Il ne prouve pas une défaillance actuelle du produit.

**Inférence limitée :** partager davantage de mémoire ne garantit pas un meilleur résultat. Le test proposé doit mesurer aussi les mauvaises habitudes propagées et le coût de leur correction. Cela renforce la justification du replay et du retour arrière dans Multica ; cela ne valide ni l’exclusivité du parcours, ni sa rentabilité.


## Cumora — livraison documentée, vérification du 5 septembre

La [documentation Shipping](https://github.com/yetone/cumora/blob/main/docs/SHIPPING.md), consultée le 2026-09-05, décrit déjà des contrats, des preuves attribuées à des vérificateurs distincts des auteurs, des approbations et un suivi après déploiement. Elle décrit aussi la création de régressions rejouables après un échec. Ce sont des affirmations documentaires de l’éditeur, pas des tests exécutés ici.

Conséquence : la livraison vérifiable et la boucle de correction sont des exigences de produit, pas une exclusivité défendable. La comparaison doit porter sur des résultats mesurés, le coût d’adoption et le temps humain nécessaire. L’avantage éventuel d’une promotion de mémoire évaluée sur des cas réservés reste à démontrer face à ce périmètre.

## CopilotKit Intelligence — apprentissage, vérification du 5 septembre

La [page officielle CopilotKit Intelligence](https://www.copilotkit.ai/copilotkit-intelligence), consultée le 2026-09-05, présente un apprentissage à partir des interactions, approbations, modifications et reprises, avec des conteneurs par utilisateur, groupe ou organisation. Elle annonce des compétences auto-adoptées, une provenance consultable, l’export de jeux de données et un déploiement auto-hébergé. Il s’agit de capacités annoncées par l’éditeur ; ni code d’implémentation ni performances validés dans cette consultation.

Conséquence : apprentissage, portée et traçabilité ne suffisent pas à différencier Multica. Notre hypothèse doit porter sur le gain vérifié avant promotion et conservé lors d’un changement d’exécutant. Aucun résultat comparatif, engagement d’achat ou avantage exclusif n’est établi.


## Letta — évaluation de la mémoire, vérification du 5 septembre

L’article [Evaluating Memory in Production Agents](https://www.letta.com/blog/evaluating-memory-in-production-agents/), publié le 28 juillet 2026 et consulté le 5 septembre, décrit Context-Bench V2 comme un benchmark privé. Il sépare création/généralisation et entretien des mémoires, puis récupération et respect de leurs instructions. Les scénarios synthétiques s’appuient sur des traces de production, avec simulation utilisateur et juge LLM ; les profils sont évalués avant et après nettoyage. Résultats de l’éditeur, non reproduits ici ; ne pas transformer son classement en recommandation de modèle.

Conséquence pour notre protocole : conserver séparément les preuves de récupération d’une procédure et celles du résultat obtenu en la suivant. Un export ou une injection réussie ne prouve pas le transfert d’un savoir-faire. La portabilité et l’évaluation existent déjà dans le discours concurrentiel ; notre avantage éventuel doit être mesuré sur les corrections réelles de l’équipe, leur coût humain et leurs régressions. Aucun avantage exclusif ni achat validé.


## 5 septembre — Hermes Agent : approbation et mémoire procédurale

Sources primaires consultées le 5 septembre 2026 : [mémoire persistante](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory), [système de skills](https://hermes-agent.nousresearch.com/docs/user-guide/features/skills/) et [PR #102920 fusionnée le 4 septembre](https://github.com/NousResearch/hermes-agent/pull/102920).

**Documenté par le projet** : la revue en arrière-plan peut tirer des corrections des sessions. `memory.write_approval` et `skills.write_approval` permettent de préparer les écritures pour approbation ; ces contrôles sont désactivés par défaut. Les changements de skills préparés sont persistants et consultables en diff. Mémoire courte et procédures chargées selon la pertinence sont deux objets distincts. Ces capacités n’ont pas été exécutées localement dans cet audit. La mise en attente et l’approbation humaine ne sont donc pas une exclusivité de Multica.

**Signal récent concret, rapporté par le mainteneur** : la PR du 4 septembre décrit une procédure qui avait grossi en accumulant des références par session. Le correctif distingue les règles réutilisables des journaux d’incident et ajoute deux avertissements de forme ; ces avertissements ne bloquent pas les écritures. Les tests annoncés dans la PR ne deviennent pas des tests exécutés ici, ni une mesure de la qualité générale de l’agent.

**Conséquence pour notre produit** : conserver les preuves détaillées avec la proposition, mais n’injecter que la leçon approuvée et bornée. Mesurer ensuite son usage et ses régressions sur des cas indépendants. La candidate de mémoire ajoutée dans cette tranche répond au premier point ; elle n’est pas encore une procédure validée par replay.

**Décision de recherche** : ajouter Hermes au périmètre concurrentiel. Pas d’adoption de runtime ou de copie proposée ; audit de licence et de code d’intégration non réalisé. L’hypothèse commerciale reste une réduction mesurée des corrections et du travail humain, portable entre exécutants. Aucun signal de paiement n’est établi par ces sources.
