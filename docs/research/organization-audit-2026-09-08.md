# Organisation — audit fonctionnel et UX du 8 septembre 2026

## Verdict et périmètre

La confusion agent/équipe est structurelle. Les boutons, le modèle de données et le dessin ne décrivent pas toujours la même opération. Une amélioration graphique seule ne la corrige pas.

Deux versions ont été examinées séparément :

| Version | Source | Situation |
|---|---|---|
| App installée `0.4.43-30-g5b68cc711` | `/Users/jeff/orca/vigil-org-people`, commit `5b68cc711` | Vue Personnes, vue Équipes, assistant, testeur et modèles composites déjà présents. |
| Nouveau package `0.2.1-8-g8a3e1212b-dirty` | `/Users/jeff/orca/vigil`, commit `8a3e1212b` et modifications locales | Nouvel éditeur, catalogue et restauration de révision, mais plusieurs parcours de l’app installée absents. |

**Le nouveau package ne doit pas remplacer l’app installée en l’état.** Les deux branches divergent. Le numéro de version n’était pas seulement un détail de présentation : la comparaison du code révèle des fonctionnalités absentes. Les progrès du nouveau travail doivent être réconciliés avec les parcours existants.

Méthode : lecture du code des deux versions, examen des handlers de validation/routage/enregistrement, revue visuelle indépendante du nouvel aperçu avec données fictives, scan statique et six diagnostics exécutables. Aucun agent réel lancé, aucune organisation réelle modifiée. Les observations sur la version installée s’appuient sur son code et sur les écrans observés ; ses mutations n’ont pas été exécutées contre les données utilisateur.

Cet audit est terminé. **Les défauts ci-dessous ne sont pas annoncés comme corrigés.** Les tests antérieurs validaient des parcours trop limités pour justifier une livraison finale.

## Corrections réalisées dans le checkout Vigil

Les constats qui suivent décrivent l’état audité, avant correction. Le code local a depuis été modifié :

- Création d’agent via le parcours Agents ; création d’équipe avec nom, responsable et rattachement choisis explicitement ; ajout et déplacement de membres limités à leur appartenance.
- Deux vues distinctes : organigramme des équipes et répertoire des personnes/agents. Les responsabilités humaines sont affichées séparément des appartenances.
- Membres visibles en premier ; mission, responsable, squad d’exécution, objectif, autonomie et permissions regroupés dans les réglages de fonctionnement.
- Attribution des rôles par membre avec identité typée ; choix séparé de l’agent destinataire ; toutes les règles de routage éditables, y compris étiquettes, chemins et priorité.
- Confirmation de suppression avec inventaire des effets. Suppression bloquée si elle invalide le quorum d’un comité ; aucun abaissement automatique du quorum.
- Assistant et testeur récupérés depuis la branche installée. La création ne remplace plus une organisation existante ; choix du projet, exclusion de participants, noms des équipes proposés en français et validation avant envoi.
- Simulation par le même moteur serveur ; résultat masqué si la demande ou la définition change. Missions et modèles locaux conservés, héritage et refus transmis au contexte d’exécution.
- Brouillons de composition et de création persistés avec le stockage partagé par workspace. Révision attendue conservée, conflit signalé, aperçu avant publication d’une modification active. Historique incluant les changements de relations, règles, comités et marché.
- Catalogue distinct de la création d’équipe : un modèle produit une équipe à trois membres/rôles, avec aperçu des autres objets importés ; installation réservée aux administrateurs/propriétaires.
- Contrôles de zoom qui tiennent à 390 px, recentrage du bloc choisi et états de chargement/erreur améliorés.

Validation : 155 tests core/API et 26 tests d’écran passent ; tests Go ciblés de l’organisation, des refus et du contexte d’exécution passent ; contrôles TypeScript core/views, lint des écrans Organisation et compilation du serveur passent. Inspection visuelle dans un navigateur à largeur normale et à 390 px. Création d’équipe puis ajout de membre vérifiés sur données fictives : le nombre d’équipes ne change pas lors de l’ajout. Rechargement réel : le nom saisi dans un brouillon est restauré.

### Limites qui restent explicites

- L’app installée et les organisations réelles n’ont pas été modifiées. Aucun nouvel installateur n’est livré ici : les branches divergent aussi en dehors d’Organisation ; une livraison globale doit partir d’une base réconciliée pour ne pas perdre des fonctions indépendantes.
- Les anciens groupes créés pour représenter des postes ne sont pas fusionnés automatiquement. Leur regroupement demande un choix métier ; les nouveaux outils permettent de déplacer leurs membres puis de retirer les groupes devenus inutiles.
- La hiérarchie représentée est celle des équipes. Le répertoire n’invente pas de relation hiérarchique individuelle.
- L’attestation d’activation est une déclaration manuelle, pas la preuve d’une campagne automatique de 30 essais. Le test de routage ne lance pas d’agents réels.
- Une équipe vide peut être préparée localement. Les contraintes propres au modèle restent appliquées avant sauvegarde : une squad d’exécution exige un agent ou une squad liée, un cercle exige un rôle.
- Le diagnostic historique `.mjs` reste lié à l’ancien commit installé ; ses échecs documentent cet état ancien. Les tests colocalisés vérifient le nouveau comportement.

## 1. Les objets et les actions à distinguer

| Objet | Signification attendue | Action distincte |
|---|---|---|
| Agent | Identité IA existant dans le workspace, avec instructions, outils, runtime et permissions propres | **Créer un agent** réutilise le parcours Agents. |
| Collaborateur humain | Membre du workspace | **Inviter une personne** réutilise les invitations. |
| Équipe | Groupe nommé, mission, membres et responsabilité de décision | **Créer une équipe** crée ce groupe, même si sa composition reste à compléter. |
| Appartenance | Participation d’un agent ou d’une personne à une équipe | **Ajouter à l’équipe** ajoute une relation ; ne crée ni agent, ni équipe. |
| Rôle | Responsabilité portée dans une équipe | **Attribuer un rôle** ne crée pas un groupe et ne change pas implicitement le supérieur. |
| Organisation | Structure qui assemble les équipes et définit sa portée | **Créer une organisation** ne remplace pas silencieusement une organisation active. |
| Relation hiérarchique | Qui rend compte à qui, au niveau annoncé | **Changer le rattachement** respecte exactement la cible choisie. |
| Modèle métier complet | Ensemble de plusieurs objets préconfigurés | **Démarrer depuis un modèle** annonce agents, projet, organisation, procédure et routine créés. |

Une équipe organisationnelle et une Squad d’exécution ne sont pas automatiquement le même objet. Le backend possède un lien `squad_id` : quand il existe, le routage peut cibler la Squad. L’UI doit rendre ce lien explicite et réutiliser les Squads existantes, sans suggérer que tout groupe de trois agents s’exécute collectivement.

## 2. Défauts prioritaires confirmés

Les identifiants A désignent l’app installée ; N le nouveau build ; C un problème transversal.

### C1 — Bloquant livraison : base fonctionnelle divergente

Le nouveau checkout ne reprend pas `org-people-chart`, `org-wizard`, `org-tester`, les validations métier immédiates, les missions libres et les modèles hérités par unité de la version installée. Il ajoute des fonctions utiles mais ne constitue pas une évolution complète de cette dernière.

Preuves : [page installée](/Users/jeff/orca/vigil-org-people/packages/views/org/components/org-page.tsx:30), [nouvelle page](/Users/jeff/orca/vigil/packages/views/org/components/org-page.tsx:35), [types installés](/Users/jeff/orca/vigil-org-people/packages/core/types/org.ts:31).

Le backend de ce checkout ne déclare pas non plus `mission` et `model` sur `OrgUnit`. Un remplacement de backend suivi d’une réécriture de définition présente donc un risque de perte de ces champs et de changement de comportement. Ce risque est établi par les types et le décodage, pas par une mutation sur les données réelles. Les schémas frontend `.loose()` conservent les champs inconnus : il ne faut pas leur attribuer cette perte.

Correction : établir une base commune avant tout nouveau package, avec une matrice de non-régression et la conservation des champs existants.

### A1 — P1 : le supérieur choisi peut être ignoré

Dans « Ajouter un équipier », toutes les personnes sont proposées comme supérieurs. `orgAddTeammate` ajoute pourtant le nouveau membre à l’unité du supérieur choisi. Ensuite `orgPeople` rattache tous les membres au responsable déduit de cette unité.

Reproduction : choisir l’agent `manager` dans une équipe dont le responsable est `human`. Le nouvel agent apparaît sous `human`, pas sous `manager`. Le contrôle laisse donc exprimer une relation que les données ne stockent pas.

Preuves : [ajout](/Users/jeff/orca/vigil-org-people/packages/core/org/people.ts:234), [projection](/Users/jeff/orca/vigil-org-people/packages/core/org/people.ts:58), [sélecteur](/Users/jeff/orca/vigil-org-people/packages/views/org/components/org-people-chart.tsx:355). Diagnostic exécuté : échec confirmé.

Correction : séparer appartenance à l’équipe et supérieur. Tant qu’une relation individuelle n’est pas représentable, le formulaire doit proposer un rattachement d’équipe explicite ; il ne doit pas accepter puis remplacer le choix d’une personne.

### A2 — P1 : ajouter un agent crée implicitement une équipe

Sans supérieur, `orgAddTeammate` crée une nouvelle unité nommée d’après le poste ou le nom du nouvel arrivant. Ajouter un agent fait donc passer le nombre d’unités de 1 à 2. L’unité créée pour un agent n’a pas de responsable humain et peut rester inéligible au routage.

Preuve : [création implicite](/Users/jeff/orca/vigil-org-people/packages/core/org/people.ts:249). Diagnostic exécuté : échec confirmé.

Correction : l’ajout d’un agent doit demander une équipe existante, ou proposer séparément « Créer une équipe ». Un agent sans affectation doit être identifiable comme tel, sans groupe inventé.

### N1 — P1 : tout bloc est nommé « équipe », même quand il représente un poste

Le catalogue crée trois unités, chacune nommée d’après un rôle et contenant un seul agent. Le nouvel éditeur les affiche et les compte comme trois équipes. En parallèle, « Installer cette équipe » crée aussi un projet, une organisation, des agents et une routine.

Preuves : [catalogue backend](/Users/jeff/orca/vigil/server/internal/handler/org_catalog.go:36), [carte et compteur](/Users/jeff/orca/vigil/packages/views/org/components/org-editor.tsx:96), [installation](/Users/jeff/orca/vigil/packages/views/org/components/org-team-catalog.tsx:38).

Correction : un modèle « Support client » doit annoncer sa composition réelle. Trois rôles dans une équipe ne doivent pas être comptés comme trois équipes. Séparer le catalogue d’organisations du bouton d’ajout d’équipe.

### N2 — P1 : un ajout dépend d’une sélection préalable invisible dans son libellé

« Ajouter une équipe » copie le responsable et le type de l’unité sélectionnée, puis lui rattache la nouvelle unité avec `reports_to`, même pour un réseau plat ou un marché. La première unité est sélectionnée automatiquement.

Preuve : [fonction add](/Users/jeff/orca/vigil/packages/views/org/components/org-editor.tsx:49).

Correction : nom et rattachement explicites avant création. L’action contextuelle peut s’appeler « Ajouter une sous-équipe », avec la cible nommée. Aucun lien hiérarchique implicite dans les autres modèles.

### C2 — P1 : responsable humain, chef d’équipe et destinataire réel sont confondus

L’humain responsable peut être affiché sur la carte sans faire partie du roster. Le moteur cible une Squad si `squad_id` existe, sinon un agent dont `role === "lead"`, sinon le premier agent, sinon le responsable humain. Affecter un `role_id` intitulé « Responsable » ne définit pas nécessairement ce destinataire.

Preuves : [routage actuel](/Users/jeff/orca/vigil/server/internal/handler/org.go:1221), [rôles dans l’éditeur](/Users/jeff/orca/vigil/packages/views/org/components/org-editor.tsx:121), [responsable visuel installé](/Users/jeff/orca/vigil-org-people/packages/core/org/people.ts:36).

Correction : distinguer « Responsable humain » et « Reçoit les demandes ». Afficher le destinataire réel calculé. Ne pas présenter un titre libre comme une règle d’exécution.

### A3 — P1 : une fiche de personne contient la suppression de son unité entière

Le panneau avancé d’une personne monte `OrgUnitSheet`. Son `onDelete` supprime l’unité et ses relations, y compris les autres membres de l’unité dans cette structure. Cela ne supprime pas les identités globales des agents, mais dépasse largement le retrait d’une personne.

Preuve : [suppression depuis une fiche personne](/Users/jeff/orca/vigil-org-people/packages/views/org/components/org-people-chart.tsx:253).

Correction : « Retirer de l’équipe » sur une personne ; « Supprimer l’équipe » seulement sur sa fiche, avec résumé de l’impact. L’identité de l’agent reste intacte.

### A4 — P1 : créer peut réécrire une organisation active

L’assistant cherche une structure non dissoute au périmètre workspace. S’il en trouve une, la soumission appelle `update` sur celle-ci ; le backend conserve son statut. Une note signale le remplacement, mais le parcours reste présenté comme la création et l’ouverture d’un brouillon.

Preuves : [choix de l’organisation existante](/Users/jeff/orca/vigil-org-people/packages/views/org/components/org-wizard.tsx:142), [conservation du statut](/Users/jeff/orca/vigil-org-people/server/internal/handler/org.go:981).

Correction : séparer « Créer », « Modifier la structure active » et « Préparer une nouvelle version ». Une création ne doit pas réorganiser immédiatement le fonctionnement actif.

### A5 — P1 : résultats de test périmés présentés avec le contexte courant

`OrgTester` conserve `simulate.data` lorsqu’on modifie la demande ou la définition. Son libellé de base et certains éléments affichés sont recalculés depuis les props courantes. Un ancien résultat peut donc rester à côté d’une nouvelle demande ou d’une structure modifiée sans avertissement de péremption.

Preuve : [état du testeur](/Users/jeff/orca/vigil-org-people/packages/views/org/components/org-tester.tsx:35).

Correction : lier chaque résultat à la demande et à la définition soumises. Toute modification doit marquer le résultat « À recalculer » ; la simulation ne doit jamais constituer une preuve d’activation pour une autre révision.

### N3 — P1 : « Composer et vérifier » a perdu son testeur

Le nouvel assistant propose l’éditeur puis « Créer le brouillon ». Aucune simulation n’y est branchée. Le backend local `/org/resolve` résout la structure applicable au projet ; il ne simule pas une demande. La version installée utilise un autre endpoint, `/org/simulate`.

Preuves : [création](/Users/jeff/orca/vigil/packages/views/org/components/org-page.tsx:119), [resolve](/Users/jeff/orca/vigil/server/internal/handler/org.go:696), [testeur existant](/Users/jeff/orca/vigil-org-people/packages/views/org/components/org-tester.tsx:53).

Correction : reprendre le vrai testeur et son endpoint, avec la correction A5. Distinguer simulation sans effets et exécution réelle.

### C3 — P1 : l’attestation d’activation n’est pas une preuve attachée à la révision

Le contrôle d’activation demande un texte non vide et vérifie certaines préconditions. Lors d’une modification active, l’ancienne attestation peut être réutilisée. L’UI autorise aussi l’envoi sans attendre un preflight réussi. Les exigences affichées dans le nouveau preflight sont une liste générique, pas une liste de contrôles effectivement passés.

Preuves : [activation](/Users/jeff/orca/vigil/server/internal/handler/org.go:586), [réutilisation](/Users/jeff/orca/vigil/server/internal/handler/org.go:889), [liste générique](/Users/jeff/orca/vigil/server/internal/handler/org.go:1118), [bouton](/Users/jeff/orca/vigil/packages/views/org/components/org-page.tsx:186).

Correction : readiness visible et contextualisée, blocages reliés aux champs, attestation explicitement déclarative tant qu’aucune preuve de run n’est stockée. Un changement de version doit invalider les preuves qui ne s’y appliquent plus.

### N4 — P1 : la suppression ajuste silencieusement un quorum

Retirer une équipe d’un comité de deux équipes avec quorum 2 produit un quorum 1. La suppression modifie ainsi la règle de décision, sans décision explicite de l’utilisateur. L’ancienne version laisse quant à elle des comités invalides : aucune des deux ne résout correctement le parcours.

Preuves : [nouveau retrait](/Users/jeff/orca/vigil/packages/core/org/editor.ts:23), [ancien retrait](/Users/jeff/orca/vigil-org-people/packages/views/org/components/org-canvas.tsx:91). Diagnostic exécuté : passage 2 → 1 confirmé.

Correction : montrer les règles touchées et demander leur résolution avant enregistrement ; ne pas abaisser automatiquement une exigence d’approbation.

## 3. Autres incohérences et manques

| ID / priorité | Constat et impact | Correction |
|---|---|---|
| N5 / P1 | Historique : comparaison résumée limitée aux unités ; changer seulement une relation donne zéro changement. Le JSON complet reste disponible mais exige une expertise. `editor.ts:67`. Diagnostic confirmé. | Comparer aussi relations, règles, comités et paramètres de marché ; annoncer clairement l’impact de restauration. |
| N6 / P1 | Le champ de routage ne lit que l’ID `keywords-${unit.id}`. Les règles existantes `r1` ou `default` restent actives et invisibles. `org-editor.tsx:123`. | Afficher toutes les règles applicables, leurs critères et priorités ; modification de la vraie règle, pas création d’une règle parallèle. |
| N7 / P1 | Le parser accepte une équipe sans nom ; le bouton de sauvegarde du détail vérifie seulement le nom de la structure. Le serveur rejette plus tard. `editor.ts:42`, `org-page.tsx:409`. Diagnostic confirmé. | Validation métier immédiate, erreur sur la bonne équipe ; serveur toujours autoritaire. |
| N8 / P2 | Matrice : `orgLayout` conserve un seul parent dans une Map. Inverser l’ordre des mêmes relations déplace une carte de y=552 à y=304. `editor.ts:5`. Diagnostic confirmé. | Algorithme prenant en compte les relations multiples et dessin correspondant au modèle. |
| C4 / P1 | Les contrôles diffèrent selon les modèles : la nouvelle UI n’édite pas les comités, le lien Squad, les décideurs externes, les refus ou les paramètres de marché hors JSON. | Matrice des paramètres indispensables par modèle ; affichage progressif de ceux qui s’appliquent. |
| A6 / P1 | L’assistant affecte tous les acteurs du workspace, sans option « Ne pas inclure ». La suggestion devient une affectation obligatoire. `org-wizard.tsx:126`. | Sélection explicite des participants, aucune équipe remplie avec tout le workspace par défaut. |
| A7 / P1 | Autosave : la définition est marquée comme envoyée avant le succès serveur. Après échec, le même contenu ne déclenche pas automatiquement un nouvel essai. `org-page.tsx:362`. | Statut sauvegardé uniquement après succès ; échec visible, reprise explicite ; conserver le brouillon. |
| A8 / P2 | `dirty` observe seulement la définition, pas le nom, propriétaire, budget ou fin. `org-page.tsx:300`. | Un seul calcul couvrant tous les champs ; protection de navigation commune. |
| N9 / P1 | La fermeture de création abandonne la composition sans garde ; le détail ne protège que son propre retour et `beforeunload`, pas toutes les navigations internes. | Brouillon conservé et protection couvrant les sorties de la page, y compris sidebar et changement de workspace. |
| N10 / P2 | Une affectation de rôle remplace silencieusement le rôle précédent ; « Sans responsable » sert aussi à dire « Rôle vacant ». | Afficher les postes occupés/vacants et l’effet de la réaffectation. |
| N11 / P2 | Liste globale des personnes avant le roster sélectionné ; rôles sous la ligne de flottaison ; humain responsable affiché séparément sans distinction. | Roster d’abord, recherche d’ajout séparée, rôle sur chaque ligne. |
| N12 / P2 | Répertoire vide en cas d’échec ; santé et preflight peuvent afficher un chargement sans fin. | Distinguer vide, chargement, indisponible et retry. |
| N13 / P2 | Catalogue accessible aux membres alors que téléchargement/import exigent owner/admin. | Affordances cohérentes avec les autorisations serveur ; expliquer l’accès requis avant une erreur. |
| C5 / P2 | Des chiffres prévisionnels sont présentés avec précision sans distinguer estimation et mesure ; coût nul possible sans historique, temps humain issu de constantes. | Marquer les estimations et leur base ; afficher « données insuffisantes » plutôt qu’une gratuité implicite. |

## 4. Critique du design actuel

Le contexte donné par l’utilisateur est clair : un public non développeur, une interface attirante, interactive et facile à remplir. Le nouvel écran est propre, mais son titre-slogan, ses compteurs et ses cartes répétitives occupent l’espace avant que l’utilisateur puisse accomplir une action précise.

Revue indépendante de l’aperçu actuel : **18/40** selon les dix heuristiques de Nielsen. Ce score est un jugement de revue, pas une mesure instrumentée.

| Heuristique | /4 | Point principal |
|---|---:|---|
| Visibilité de l’état | 2 | Enregistrement visible, erreurs de chargement mal distinguées. |
| Correspondance avec le monde réel | 1 | Agent, personne, rôle, unité et équipe confondus. |
| Contrôle et liberté | 2 | Protection partielle, créations jetables, annulation locale manquante. |
| Cohérence | 1 | « Équipe » recouvre plusieurs portées d’action. |
| Prévention des erreurs | 2 | Cycles bloqués mais rattachements et modifications de quorum implicites. |
| Reconnaissance | 2 | Noms et avatars utiles ; composition effective noyée dans le répertoire. |
| Efficacité | 2 | Recherche et zoom ; pas de lien profond, remplissage laborieux. |
| Esthétique et sobriété | 2 | Espaces généreux mais trop de cartes, compteurs et défilements imbriqués. |
| Récupération | 2 | Erreurs tardives, comparaison incomplète. |
| Aide | 2 | Quelques indications ; activation et choix de modèle trop techniques. |

Charge cognitive : cinq difficultés sur huit critères examinés. Les sept modèles, cinq métiers, quatre onglets de page, trois onglets d’inspecteur et listes globales se cumulent.

Pour un premier utilisateur, l’obstacle est l’absence d’un bouton correspondant à « ajouter cet agent ». Pour un utilisateur expérimenté, c’est le manque de contrôle sur le destinataire, les relations et les révisions. Pour le clavier/lecteur d’écran, c’est l’absence d’un équivalent complet du graphe et d’une gestion cohérente du focus.

Autres défauts visuels :

- Les couleurs vert/ambre/bleu sont attribuées par index, sans sens métier ; retirer une unité peut recolorer les autres.
- Trois types de relations partagent le même pointillé gris ; ni légende ni libellé directement lisible.
- « Brouillon » désigne à la fois l’état de l’organisation et un niveau d’autonomie.
- À 1486 × 745, le roster déborde déjà et les rôles sont hors écran. Sur mobile, le canvas de hauteur fixe repousse l’inspecteur en dessous.
- Les miniatures ont une vraie topologie, mais leurs traits de faux texte n’aident pas à reconnaître une équipe.
- Les onglets utilisent des boutons pressés ; leur sémantique et leur navigation clavier doivent être cohérentes avec leur comportement réel.

Le scan `impeccable 4.1.0 detect --json packages/views/org/components` a retourné `[]` : zéro anomalie détectée automatiquement sur six fichiers TSX. **Cela ne contredit pas l’audit : ce scan statique ne vérifie ni la logique métier ni le résultat des actions.** Aucun overlay navigateur n’a été injecté. Les captures fraîches ont été examinées dans l’onglet d’aperçu isolé ; les anciennes captures sur disque ne sont pas présentées comme de nouvelles preuves.

## 5. Parcours cible concret

L’entrée Organisation doit afficher le travail réel : nom, périmètre, état, équipes et personnes non affectées. Les modèles de création deviennent secondaires lorsqu’une organisation existe.

Trois vues complémentaires, sur les mêmes données : **Organigramme**, **Équipes**, **Personnes et agents**. Un agent garde son identité lorsqu’il change d’équipe ; une présence dans plusieurs équipes est affichée comme plusieurs appartenances, pas comme plusieurs agents distincts.

Exemple de parcours :

1. **Créer une équipe** → « Support client » → mission → responsable humain → rattachement explicite ou aucun.
2. **Ajouter à l’équipe** → choisir deux agents existants ; un lien séparé ouvre « Créer un agent » si nécessaire.
3. **Répartir les responsabilités** → rôle de chacun, destinataire principal des demandes, décideur humain.
4. **Organiser les relations** → rattachement, remplacement, consultation ou escalade, chacun nommé et expliqué.
5. **Tester une demande** → afficher qui reçoit, qui prépare, qui décide, les limites et les étapes d’escalade sur le graphe.
6. **Enregistrer le brouillon** ou **Publier les modifications** selon l’état ; montrer ce qui va changer avant d’appliquer une version active.

La fiche Agent expose identité, présence, affectations et rôle local. La fiche Équipe expose mission, responsable, roster, destinataire, règles et budget. Une action sur l’une ne doit pas modifier l’autre implicitement.

Direction visuelle : noms et visages au centre du graphe, équipes clairement délimitées, sélection stable, liens lisibles, panneaux contextuels courts et champ d’ajout immédiatement découvrable. L’interactivité sert la compréhension : sélection, déplacement avec aperçu, retour arrière, simulation du trajet. Les réglages de gouvernance se révèlent lorsqu’ils s’appliquent.

## 6. Ordre de correction et critères de livraison

1. **Réconcilier les versions.** Préserver les vues, assistant, simulateur, missions et modèles composites de l’app installée ; conserver les apports utiles du nouveau travail, dont sauvegarde atomique et restauration.
2. **Corriger les opérations et identités.** Ajout d’agent, création d’équipe, affectation, rattachement, retrait et changement de rôle doivent produire exactement les objets annoncés.
3. **Aligner affichage et exécution.** Destinataire réel, Squad liée, responsables, modèles et règles visibles doivent refléter le moteur.
4. **Sécuriser les modifications.** Brouillon durable, conflits explicites, résultats de simulation versionnés, restauration complète et aperçu d’impact sur les règles de décision.
5. **Refaire la composition visuelle.** Vue personnes lisible, équipes regroupées, roster compact, inspection adaptée aux petites fenêtres et navigation clavier complète.
6. **Tester puis packager la même base.** Vérifier les parcours avec données de test représentatives, puis compatibilité frontend/backend et conservation du profil lors du lancement.

Critères obligatoires :

- Ajouter un agent à une équipe laisse inchangés les nombres d’agents globaux et d’équipes ; seule l’appartenance change.
- Créer un agent crée une identité ; créer une équipe crée un groupe ; chaque confirmation le dit clairement.
- Choisir un supérieur ne peut pas sélectionner silencieusement une autre personne.
- Déplacer ou retirer un membre conserve l’identité et les autres affectations ; les rôles devenus invalides sont traités explicitement.
- Supprimer une équipe ne diminue pas silencieusement les seuils de décision.
- Une règle active est consultable et modifiable ; une règle cachée ne doit pas contredire l’explication donnée.
- Une simulation devient périmée dès que sa demande ou sa définition change.
- Une modification active a une portée explicite et conserve une révision restaurable.
- Nom vide, agent indisponible, absence de propriétaire, rôle vacant, équipe vide, conflit de révision et panne réseau ont des états compréhensibles.
- Vérifier hiérarchie, réseau plat, squads, matrice, cercles, task force et marché ; puis structure composite, 30 agents, plusieurs équipes, noms longs, clavier, petite fenêtre et thème sombre.

## 7. Diagnostics exécutés

Fichier : [organization-audit-2026-09-08.mjs](organization-audit-2026-09-08.mjs).

```bash
rtk proxy node docs/research/organization-audit-2026-09-08.mjs
```

Les six invariants ont échoué comme attendu sur le code audité : supérieur ignoré, unité créée implicitement, quorum abaissé, changement de relation absent du résumé, nom vide accepté localement, placement matriciel dépendant de l’ordre des liens. Le code de sortie est **1**, volontairement : il indique des défauts encore présents, pas des tests verts.

Les fonctions de la version installée sont lues au commit fixe `5b68cc711` pour rendre le diagnostic reproductible. Après correction sur une base commune, ces scénarios doivent devenir des tests de comportement sur la nouvelle implémentation ; le diagnostic de l’ancienne révision reste une preuve historique.

Pas de nouvelle installation ni de mise à jour du serveur pendant cet audit. Les fichiers applicatifs existants n’ont pas été modifiés par cette passe ; seuls ce rapport et son diagnostic ont été ajoutés.
