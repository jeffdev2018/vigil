# Pilote mémoire connecté — 6 septembre 2026

Les trois volets sont implémentés : environnement local actif, comparaison sur un
fournisseur réel, et critères JSON/tests de fonctions JavaScript. L'acceptation de
code reste soumise à la revue indépendante décrite en fin de document.

## Résultat observé

Comparaison finale : **0/2 sans candidate, 2/2 avec candidate**, aucune régression
ni erreur d'exécution. Durée murale du rapport : 22,94 s. Modèle demandé :
`gpt-5.6-luna`, effort `low`, via le runtime Codex connecté. Les deux variantes
utilisent les mêmes instructions, modèle, prompts et exécutable ; seule la mémoire
candidate varie. Les observations d'adaptateur ne sont pas une attestation du modèle.

| Cas | Contrôle | Sans mémoire | Avec mémoire |
| --- | --- | --- | --- |
| Facture client standard | JSON structuré | `general / 48 h`, échec | `billing / 24 h`, réussite |
| Fonction de routage d'incident | Deux tests JavaScript indépendants | Retour générique, échec | `incident / 1 h` et maintien du défaut, réussite |

Le pilote utilise une règle organisationnelle synthétique, connue seulement via la
mémoire. Il démontre son utilisation sur ces deux cas ; ce n'est ni un benchmark
représentatif, ni une estimation statistique, ni une preuve d'avantage concurrentiel.
Les critères sont figés avant lancement ; aucune modification de la mémoire entre
les deux tentatives. Les réponses attendues sont exclues des jobs du runtime.

Premier essai conservé : trois réponses produites, puis erreur du vérificateur de
code, arrêt de la file et adoption refusée. Le diagnostic initial était générique ;
il ne permet pas d'attribuer cette panne avec certitude. Les vérifications locales
ont montré un démarrage Docker lent. Le délai total a été porté de 5 à 15 secondes
et les erreurs détaillées sont désormais conservées. Une nouvelle comparaison
explicite a produit les quatre réponses finales. Aucun résultat du premier essai
n'a été recyclé dans le second et aucun retry payant automatique n'a été ajouté.

Consommation rapportée **sur les sept réponses, essai interrompu inclus** :
84 723 tokens en entrée, dont 69 888 en cache, et 783 tokens en sortie ; aucun
appel d'outil rapporté. Les tokens en cache sont un sous-ensemble de l'entrée.
**Coût facturé, corrections humaines, temps humain et acceptation humaine :
inconnus**, non remplacés par zéro. L'exercice automatique du bouton de revue
n'est pas compté comme une revue humaine mesurée.

## Preuves conservées

- [Rapport exporté depuis le navigateur](evidence/memory-pilot-2026-09-06/exported-report.json).
- [Premier essai interrompu](evidence/memory-pilot-2026-09-06/attempt1-report.json).
- [Consommation des deux essais](evidence/memory-pilot-2026-09-06/usage-summary.json).
- [Adoption et restauration par l'interface](evidence/memory-pilot-2026-09-06/lifecycle.json).

Le CLI `agent memory pilot-summary` relit l'export et conserve les mesures humaines
et monétaires à `null`. Les rapports contiennent les cas, sorties et contrôles,
versions, empreintes et image immuable, sans identifiants d'authentification.

Captures inspectées : `/tmp/vigil-memory-pilot/form.png`,
`checked-results.png` et `restored.png` dans le même dossier. L'adoption est d'abord
désactivée tant que la case de revue n'est pas cochée. L'automatisation du navigateur
l'a exercée sur la seule mémoire du pilote : adoption en révision 3, puis restauration
de la révision 2 sous une nouvelle révision 4 en attente de revue. Le reçu historique
reste conservé. Aucune autre mémoire n'a été adoptée.

Environnement : `vigil-482`, base `multica_vigil_482`, API `localhost:18562`,
web `localhost:13482`, profil daemon `dev-vigil-482`. Démarrage séquentiel, limite
locale d'une tâche concurrente. Le lanceur Turbo ne satisfaisait pas la vérification
de groupe de processus ; Next a été démarré directement depuis `apps/web` avec
l'environnement du checkout et ses PID vérifiés/enregistrés. Le navigateur a aussi
rencontré une erreur de chargement transitoire, absente des sessions fraîches finales.

## Portée des contrôles

Le choix `check` est `exact`, `json` ou `javascript`. JSON ignore la mise en forme et
l'ordre des clés, mais conserve types, clés requises, ordre des tableaux et précision
numérique. Les tests JavaScript portent sur des fonctions CommonJS, pas sur un dépôt
complet. Ils réutilisent le runner Docker sans réseau, sans identifiants fournisseur,
avec image immuable choisie par l'opérateur, un CPU et 512 MiB. Seuls le code, le
runner fixe et les arguments sont montés ; les valeurs attendues restent côté serveur.

Le serveur conserve les sorties des tests avec une empreinte liée au code, aux
critères et à l'image. Lecture, adoption et callback dupliqué ne réexécutent pas le
code. Erreur de conteneur, timeout ou résultat invalide bloquent l'adoption. Les
rapports importés restent des preuves humaines non signées. Voir le
[contrat complet](../development/memory-evaluation.md).

Les tests de handler utilisent exclusivement la base dédiée
`multica_memory_review_test_20260904` dans les commandes de validation finales.
Une première commande a omis `DATABASE_URL` et a atteint la base par défaut
`multica` : elle a échoué pendant le nettoyage des fixtures nommées
`handler-tests` / `handler-test@multica.ai`, sur une contrainte de clé étrangère.
Ce nettoyage n'étant pas transactionnel, son absence totale d'effet ne peut pas être
affirmée. Aucune tentative de nettoyage correctif n'a été faite sur cette base.

## Vérification du lot

- Go avec `-race -p 1 -parallel 1` : `memoryeval`, handlers, daemon et CLI,
  tests mémoire ciblés ; tests Docker de fonction correcte/erronée, exception,
  sortie vide, boucle infinie et absence de réseau/réponses attendues.
- Handlers : texte, JSON et JavaScript ; claim, contrôle d'identité, exclusion des
  réponses attendues, callbacks dupliqués, adoption et refus de modification.
- 16 tests TypeScript passent avec les configurations des packages : 15 views,
  1 core ; validation des réponses malformées et compatibilité avec ancien serveur.
- Typecheck core/views/web et dépendances : six tâches réussies, quatre en cache.
- Lints ciblés core/views, conformité des skills intégrés et `git diff --check`.
- Navigateur avec API et runtime réels : lancement, progression, résultats,
  export, adoption et restauration ; formulaire à 390 px, document de 390 px,
  police Inter chargée. Les noms accessibles des champs restent stables après saisie.

Journaux et harness locaux : `/tmp/vigil-memory-pilot/`. Les commandes du pilote
ne font pas partie des tests par défaut et ne doivent pas être relancées comme des
tests ordinaires : elles utilisent le compte du runtime. Aucun commit ni déploiement
production ; aucun nouveau worker concurrent ni nouvelle dépendance.

Revue indépendante : en attente. Les tokens de la session parente ne sont pas
exposés par les outils natifs ; reçu API équivalent indisponible pour cette partie,
sans estimation de coût ni économie revendiquée. Les tokens rapportés du pilote
ci-dessus constituent un périmètre séparé.
