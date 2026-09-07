# Différenciation commerciale face à Paperclip — 4 septembre 2026

## Décision et limites

Les améliorations et la mémoire proposées dans [la première analyse](product-priorities-2026-09-04.md) sont acceptées comme travail à réaliser. Leur réalisation n'est pas suffisante pour démontrer une raison de changer de produit ou de payer. Cette note poursuit le cadrage commercial ; aucun de ces chantiers n'est présenté comme implémenté.

Hypothèse provisoire : petites équipes SaaS/développement. Le choix de l'acheteur a été demandé à l'utilisateur et reste ouvert au moment de cette note. Recherche sur sources officielles, sans installation concurrente ni validation client. Les idées ci-dessous sont des paris, pas des exclusivités démontrées.

## Correction du diagnostic concurrentiel

Paperclip présente déjà organisation d'agents, budgets, routines, artefacts, workspaces et gouvernance dans son [dépôt officiel](https://github.com/paperclipai/paperclip). Sa [politique d'exécution](https://docs.paperclip.ing/guides/power/execution-policy/) décrit des étapes de review imposées au runtime. Sa [gestion de budgets](https://docs.paperclip.ing/guides/day-to-day/costs/) couvre plusieurs périmètres. Ces capacités constituent un niveau concurrentiel à atteindre.

La [collecte de décisions](https://docs.paperclip.ing/reference/api/decision-training/) capture contexte et annotations humaines, avec export JSONL pour un pipeline externe. Un [LLM Wiki officiel en alpha](https://docs.paperclip.ing/reference/plugins/llm-wiki/) propose déjà des sources, de la provenance et un agent mainteneur. Il serait faux de dire que Paperclip n'a pas de mémoire. L'[apprentissage organisationnel automatique figure dans sa roadmap](https://github.com/paperclipai/paperclip/blob/master/ROADMAP.md) : le créneau est aussi susceptible d'être concurrencé rapidement.

Deux raccourcis sont également à écarter : [Sentry Seer](https://docs.sentry.io/product/ai-in-sentry/seer) documente déjà analyse de cause et proposition de correction jusqu'à la PR ; [Devin](https://cognition.com/blog/devin-can-now-manage-devins) documente déjà délégation parallèle, migrations et vérification. « Corriger automatiquement » et « lancer beaucoup d'agents » ne démontrent donc pas une différence à eux seuls.

## Pari principal : enseigner un savoir-faire, puis démontrer sa réutilisation

Promesse proposée : « Multica transforme vos corrections en compétences testées que vos agents réutilisent. »

Exemple : une équipe doit rappeler régulièrement une règle métier dans les corrections de PR. Après un rejet, Multica propose une modification du skill et un contrôle exécutable lorsqu'il est possible. Il compare l'ancienne et la nouvelle version sur des tâches passées puis des cas réservés, montre les erreurs corrigées et les régressions éventuelles, et propose l'activation d'une version réversible.

La mémoire par agent déjà présente est un point de départ. Le produit supplémentaire porte sur le cycle correction → candidat → vérification indépendante → promotion → observation des résultats futurs. Un texte ajouté au prompt, une auto-évaluation de l'agent ou un score sur les seuls exemples ayant servi à écrire la règle ne suffisent pas.

Acheteur : équipe qui confie régulièrement une même famille de tâches aux agents et consacre un temps significatif à répéter ses corrections. Raison d'achat à tester : diminution mesurée du temps de supervision et des défauts récurrents. Facturation à explorer : abonnement d'équipe avec volume d'exécution/replay borné ; distinguer plateforme et coûts des fournisseurs.

Premier périmètre : une catégorie de tâches, un skill, une vingtaine de cas historiques correctement autorisés et quelques cas réservés. Rejouer dans des environnements isolés, sans effets de bord en production. Déployer une modification après validation, avec version précédente disponible. Ce pilote ne nécessite ni fine-tuning ni apprentissage entre entreprises.

L'avantage durable potentiel vient de la qualité du cycle et de son corpus métier validé. Les clients doivent conserver l'export de leurs règles et cas ; la rétention recherchée vient de la valeur accumulée. Aucun transfert entre clients sans autorisation explicite.

## Deux autres paris selon l'acheteur

| Pari | Acheteur | Résultat vendu | Limite à valider |
|---|---|---|---|
| Démonstration → procédure prise en charge | Opérations d'une agence/PME | Une procédure montrée sur deux outils devient une exécution récurrente avec contrôles et gestion des exceptions | Concurrence RPA/computer-use ; [Lindy](https://docs.lindy.ai/skills/by-lindy/computer-use) a déjà le contrôle d'ordinateur. La différence doit être la mise en service et l'entretien d'une procédure précise |
| Une campagne de maintenance sur un portefeuille client | Agence qui entretient plusieurs applications | Appliquer un changement aux projets concernés, tenir compte des exceptions de chaque client, produire previews, validation et bilan par client | Devin fait déjà des migrations parallèles ; la valeur à tester porte sur le traitement complet du portefeuille client et son économie |

Ces paris sont des alternatives de positionnement, pas trois gros chantiers à lancer simultanément.

## Preuve d'achat et chemin d'adoption

Proposer un pilote payant à quelques équipes avec leurs vraies tâches récurrentes. Comparer leur configuration actuelle à Multica en gardant autant que possible mêmes agents/modèles, données et budgets. Mesurer le temps humain total, y compris configuration et entretien des règles, les erreurs, les réouvertures et les coûts de replay.

Un seuil ambitieux à tester pour le pari principal serait de diviser par deux le temps de correction sur la famille de tâches retenue, sans dégrader la qualité des cas réservés. C'est un critère de poursuite proposé, pas un gain observé ou garanti. Arrêter ou revoir le pari si les utilisateurs ne paient pas ou si le gain disparaît quand on compte la configuration.

Permettre de commencer sur un projet en conservant les outils existants. L'[export/import de Paperclip](https://docs.paperclip.ing/guides/power/export-import/) peut faciliter une migration ultérieure ; il ne crée pas sa motivation. Un tarif définitif attend la preuve de valeur et les coûts réels de service.
