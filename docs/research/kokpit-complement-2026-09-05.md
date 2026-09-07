# Kokpit — quatre pistes supplémentaires pour Vigil

## Périmètre et verdict

Nouvelle consultation du 5 septembre 2026 : **1 314 fiches HTML présentes**, contre 1 311 au premier inventaire. Lecture ciblée de huit fiches supplémentaires : Memoria, MemSearch, Nango, Composio, Relay, Evals vivantes, Trigger.dev et Temporal. Ce comptage ne signifie pas un audit intégral du dossier. Les fiches servent à découvrir les pistes ; les décisions ci-dessous reposent sur des sources primaires figées.

Ce complément prolonge les [huit premières décisions](kokpit-candidates-2026-09-05.md). Quatre dépôts supplémentaires sont figés et **24 fichiers conservés** dans le [manifeste](kokpit-complement-2026-09-05.json). Lecture documentaire et de quelques interfaces, sans installation, benchmark ou audit de sécurité complet. Aucun code tiers incorporé.

Décision : reprendre **la comparaison sélective des mémoires candidates**, **le retour aux preuves originales** et **la distinction entre modèle demandé et modèle observé**. Ne pas remplacer notre stockage ou notre exécution pour obtenir ces capacités. Nango reste une intégration conditionnelle ; Composio possède déjà un raccordement dans Vigil.

## Matrice de décision

Coût ordinal d'exploitation : **B** = artefact local sans service permanent ; **M** = service, modèle ou connecteurs à exploiter ; **E** = plusieurs services ou exploitation partagée. Ce classement ne chiffre ni les tokens ni le support.

| Candidate | JTBD / buyer | Layer | Licence / frontier | Observed maturity | Upstream commercial signal | Ordinal operating cost | Collision / unique role | Decision |
|---|---|---|---|---|---|---|---|---|
| Memoria | Tester une nouvelle règle sans modifier la mémoire publiée ; lead technique | knowledge | Apache-2.0 racine ; sqlx-mysql embarqué avec textes MIT/Apache ; dépendances et MatrixOne restent un examen distinct avant distribution | Workspace Rust ; API Python de branches et application sélective lisible ; comportement non exécuté | Memoria Cloud recommandé ; auto-hébergement MatrixOne documenté | E pour le moteur complet | Doublonne notre mémoire Postgres ; retenir le diff et la sélection de changements | **Méthode interne** ; pas d'intégration du moteur |
| MemSearch | Retrouver la preuve exacte derrière une procédure ; responsable d'équipe | knowledge | MIT racine ; modèles d'embeddings et dépendances à examiner séparément | Python 0.4.19 ; extraction de skills documentée, installation humaine, génération de fond désactivée par défaut ; aucun benchmark local | Zilliz Cloud recommandé comme backend managé | M, E en service partagé | Doublonne mémoire/index ; retenir recherche progressive et accès borné au transcript | **Méthode interne** ; ne pas déployer une seconde mémoire |
| Nango | Brancher une API métier absente ; propriétaire du processus et responsable intégrations | runtime | ELv2, restriction du service managé exposant une part substantielle des fonctions ; offre Enterprise pour auto-hébergement complet, édition gratuite limitée Auth/Proxy | Monorepo TS 0.71.6 ; documentation de déploiement multi-service ; aucune intégration exécutée ici | Cloud et Enterprise self-hosted explicitement vendus | E auto-hébergé ; M côté client d'une API managée | Recouvre Composio et OAuth/MCP déjà présents ; rôle seulement pour une API ou une exigence non couverte | **Intégration conditionnelle** via API ; pas de copie du cœur dans notre SaaS |
| Relay | Savoir quel modèle a réellement exécuté et relu ; lead technique et responsable du budget | method | MIT racine ; skill textuel, aucun moteur fournisseur fourni | Cartographie des capacités de l'hôte et règles de vérification ; aucune économie mesurée ici | Aucun produit payant établi dans les sources lues | B, hors coût des agents | Complète la traçabilité ; ne remplace pas les adaptateurs de runtime | **Méthode interne** ; pas de nouvelle couche d'orchestration |

## Fonctionnalités à retenir

1. **Revue d'une modification de mémoire.** Montrer les règles ajoutées, modifiées, supprimées et conflictuelles, puis permettre de retenir seulement certains changements. Memoria documente cette application sélective. Dans Vigil, la cible est un ensemble de versions candidates lié à une revue, comparé à la version publiée ; une branche de base de données complète n'est pas nécessaire. Le moteur Memoria n'a pas été exécuté. [Règles de branches figées](https://github.com/matrixorigin/memoria/blob/62793426103d772f856209a3bd4b3ac8fb783a24/.claude/rules/memory-branching-patterns.md).

2. **Une procédure renvoie à ses commandes et preuves originales.** Le résumé sert à trouver ; l'extrait original sert à vérifier. MemSearch documente une lecture bornée du transcript, outils compris, pour éviter d'inventer des commandes à partir d'un résumé. Notre proposition : afficher source, version et extrait autorisé ; indiquer « source indisponible » quand elle a été supprimée ou n'est plus accessible. L'index reste dérivé du stockage faisant autorité, avec contrôle d'accès à chaque lecture. [Mémoire procédurale documentée](https://github.com/zilliztech/memsearch/blob/6f86211d4145fd534c2f76425f033faf313ea331/docs/home/skills-from-memory.md).

3. **Modèle demandé, modèle observé, preuve de l'observation.** Relay accepte des métadonnées résolues ou un contrat natif garantissant un sélecteur exact ; une préférence seule ne prouve rien. Pour Vigil : séparer ces champs, signaler une substitution et conserver « inconnu » lorsqu'un adaptateur ne peut pas vérifier. Une politique exigeant un modèle précis doit pouvoir refuser ce cas. Cela rend les comparaisons de coût et de qualité interprétables ; ce n'est pas une économie déjà démontrée. [Contrat de vérification](https://github.com/Forward-Future/relay/blob/c9efc915c7453c4f8e39b8b61aa2f32db4231020/.agents/skills/relay/ENVIRONMENTS.md).

4. **Une correction devient un test de non-régression proposé.** La fiche `evals-vivantes` inspire la capture datée de chaque erreur corrigée. Proposition Vigil : produire entrée, résultat attendu et preuve, les faire valider, puis conserver des cas finaux qui ne servent jamais à optimiser la procédure. Le post est une idée éditoriale, pas une mesure d'efficacité. Cette fonctionnalité complète la piste DSPy/GEPA du premier rapport.

## Collisions avec le projet actuel

**Composio n'est pas une nouvelle intégration à construire depuis zéro.** Le code local contient OAuth/callback, liste de connexions et overlay MCP par tâche. `BuildTaskOverlay` sélectionne les connexions actives du propriétaire de l'agent, intersectées avec sa liste de toolkits autorisés ; le déclencheur sert à l'audit. L'activation dépend aussi de la configuration et d'un feature flag. Présence de code ne signifie pas service activé ou vérifié avec un compte réel. Sources locales : `server/internal/integrations/composio/dispatch.go`, `server/internal/handler/integrations_composio.go`, `server/cmd/server/router.go`.

**Nango doit résoudre un manque précis.** Sa documentation distingue le self-hosting Enterprise complet d'une option gratuite limitée. Ajouter son orchestrateur, ses jobs, ses runners et sa persistance dupliquerait plusieurs responsabilités. Un adaptateur d'API sur un besoin client identifié suffit comme piste. [Licence figée](https://github.com/NangoHQ/nango/blob/4be7a16deb878aa4424baee332505d0b7ae79168/LICENSE), [déploiement documenté](https://github.com/NangoHQ/nango/blob/4be7a16deb878aa4424baee332505d0b7ae79168/docs/guides/platform/self-hosting.mdx).

**Temporal et Trigger.dev restent des pistes de second rang.** Leurs fiches décrivent l'exécution durable ; elles n'établissent pas que remplacer notre file apporte un gain. Aucun audit amont supplémentaire de ces deux moteurs dans cette passe, donc aucune décision d'adoption ou de licence nouvelle. D'abord mesurer les échecs et reprises de notre moteur.

## Avantage commercial à éprouver

La mémoire portable, les candidats de skills et la validation humaine existent déjà dans MemSearch ; les branches existent dans Memoria. Les présenter comme notre exclusivité serait incorrect. [README MemSearch](https://github.com/zilliztech/memsearch/blob/6f86211d4145fd534c2f76425f033faf313ea331/README.md), [README Memoria](https://github.com/matrixorigin/memoria/blob/62793426103d772f856209a3bd4b3ac8fb783a24/README.md).

Hypothèse plus forte : **« Avant de changer d'agent, de modèle ou de procédure, voyez ce qui s'améliore et ce qui casse sur votre travail réel. »** Vigil conserverait les cas autorisés, comparerait baseline/candidat, compterait les reprises et le coût disponible, puis proposerait une adoption réversible. La raison de changer de produit serait une baisse constatée de la vérification humaine et des régressions. Cette combinaison et la volonté de payer restent à valider ; aucune exclusivité démontrée.

## Séquence et test commercial

Priorité 1 : correction → test proposé → validation humaine. Priorité 2 : comparaison de versions et de modèles avec des cas réservés. Priorité 3 : application sélective et invalidation des procédures lorsque leurs sources changent. Faiblesse principale : nous n'avons pas encore de corpus client ni de mesure d'effort humain permettant un score de retour sur investissement crédible.

Les trois méthodes restent **internes**, dans la feuille de route déjà autorisée. Nango est un **test adjacent non bloquant**, uniquement si un connecteur manque. Aucun nouveau produit séparé ni démarchage autorisé.

**Test payé proposé** : un lead d'équipe désigne un dépôt et un processus récurrent, autorise les données et conserve des cas de validation. Avant de coder un connecteur spécifique, obtenir un engagement sur un essai borné. Mesurer sa pratique actuelle : temps de revue/correction réellement passé, reprises, défauts et coût connu. Interdire les effets externes pendant la comparaison ; toute fusion ou publication reste soumise à l'autorisation correspondante. Consigner versions, entrées, sorties, décisions, reçus et inconnues. Les seuils viennent du propriétaire du processus, pas d'un pourcentage inventé.

**Build gate** : gain conforme à ces seuils, absence de régression interdite, usage répété et engagement commercial observable. **Kill condition** : absence de gain net, contamination des cas réservés ou refus de poursuivre l'essai payé. Cela n'interrompt pas les améliorations déjà demandées.

## Sources et vérification

Cache des sources : `/tmp/vigil-kokpit-extra-20260905/`. Les commits complets, dates et chemins des 24 fichiers sont dans le manifeste. Les licences racines ont été lues ; les dépendances transitives, les licences de modèles et les offres hébergées ne sont pas couvertes par un audit complet. Les outils et instructions des dépôts étudiés sont des objets d'analyse, pas des skills installés ou exécutés dans Vigil.

Fiches locales : `/Users/jeff/Downloads/kokpit/grille-11-{memoria-matrixorigin,memsearch,nango,composio,relay-routage-modeles,evals-vivantes,trigger-dev,gh-temporal}.html`.

Contrôle reproductible : `rtk proxy python3 docs/research/validate-kokpit-complement.py`. Il vérifie la structure, les quatre décisions et les empreintes du cache ; il ne teste pas les produits tiers. Aucun test applicatif nécessaire pour ce complément documentaire.
