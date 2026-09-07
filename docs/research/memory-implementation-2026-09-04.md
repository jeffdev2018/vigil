# Première tranche — mémoire et corrections

Implémenté le 4 septembre 2026, dans les couches partagées web/desktop et le backend Go.

- Les suggestions extraites des runs sont en attente de validation. Les chats privés sont exclus. Seules les mémoires actives entrent dans les nouveaux briefs, avec la limite existante de 50.
- Un humain autorisé peut approuver, refuser, désactiver ou remettre en revue. Chaque modification incrémente la révision ; une approbation périmée renvoie 409. Les identifiants et dates de revue sont conservés. Ce numéro protège des conflits, ce n’est pas un historique complet des versions.
- « Teach a correction », dans l’activité, crée une proposition liée à un run terminé, échoué ou annulé. La mémoire permet de consulter le résultat source et son transcript, si ces données sont encore accessibles.
- Le plafond de 200 entrées inclut les refus. Aucun remplacement automatique de mémoire validée. Le verrou parent sérialise les créations concurrentes ; à capacité, aucun appel d’extraction LLM supplémentaire.
- Les erreurs de chargement sont explicites. Le progrès projet est renommé « Scope closed » et précise qu’il compte les tâches terminées et annulées. Les statuts d’exécution emploient « Finished » au lieu de « Succeeded ».
- Interface et documentation mises à jour dans les quatre langues existantes.

## Vérification

- Compilation du serveur Go réussie (`go build ./cmd/server`).
- Tests ciblés Go : handlers mémoire, injection au claim, extraction et règles de migrations. Base PostgreSQL isolée `multica_memory_review_test_20260904`, migrations jusqu’à 471.
- Tests Vitest : 151 tests de schémas API ; 18 tests couvrant mémoire, activité et pages projet.
- ESLint : aucune erreur ; avertissement préexistant de dépendance useEffect dans project-detail.tsx.
- Rendu du composant MemoryTab avec les styles et polices du projet, API simulée : bureau, largeur 390 px, formulaire, source et état vide. Approbation avec contrôle de révision vérifiée dans le navigateur ; aucun débordement horizontal, aucune erreur JavaScript. Captures dans `/tmp/vigil-memory-review/`.
- Le typecheck global reste bloqué par le travail CLI auth préexistant (`RuntimeCliAuthRequest`, `initiateCliAuth`, `initiateCliLogout`, `getCliAuthResult`).

## Migration et intégration

La migration 471 ajoute les états et métadonnées de revue. Les anciennes mémoires extraites passent en attente ; les mémoires manuelles restent actives. Elle a été appliquée à la base de test isolée uniquement. Un rollback supprime ces métadonnées et l’ancien code recommencerait à utiliser toutes les entrées : conserver une sauvegarde avant tout rollback.

`make sqlc` a régénéré globalement les bindings. Certains fichiers générés étaient déjà modifiés au départ (`agent.sql.go`, `autopilot.sql.go`, `chat.sql.go`, `models.go`) ; leurs écarts initiaux n’ont pas été sauvegardés avant régénération. Leur résultat actuel correspond aux requêtes et migrations présentes, mais la conservation exacte de ces anciens écarts ne peut pas être garantie.

## Suite encore à implémenter

Transformation en compétences versionnées ; replay comparatif sur cas historiques et cas réservés ; mesure des régressions et promotion réversible d’une version. La présente tranche permet la correction et la validation humaine, sans démontrer encore un apprentissage mesuré. Les autres améliorations proposées restent à traiter séparément.

## Deuxième tranche — mémoire partagée de projet

- Section « Project memory » dans la barre latérale partagée web/desktop. Les membres lisent ; seuls les humains owner/admin publient. Aucun agent, ni un simple responsable de projet, ne peut publier.
- Liste de 20 règles maximum, 500 caractères par règle, stockée sur le projet. Publication atomique avec révision attendue, auteur et date ; un conflit conserve le brouillon dans l’éditeur. Vider puis publier supprime les règles des futurs runs. La révision protège les écritures concurrentes, sans historique de restauration pour l’instant.
- Le point commun de résolution du projet enrichit uniquement le contexte envoyé au run. Tâches, chats explicitement liés, automatisations et créations rapides bénéficient des mêmes règles ; les autres projets et espaces de travail n’en reçoivent aucune. La description stockée reste intacte. Les champs déjà transmis au daemon transportent les règles, y compris avec les clients installés.
- Migration 472 appliquée à la seule base de test isolée. Les fichiers générés ont été sauvegardés cette fois dans `/tmp/vigil-project-memory-before/` ; seuls `models.go`, `project.sql.go` et le nouveau `project_memory.sql.go` diffèrent après génération.
- Vérifications : tests Go de publication, droits, isolation et contexte projet dans les chemins de claim ; test de schéma ; 7 tests d’interface mémoire/projet ; parcours navigateur de publication, conflit, effacement et lecture seule. Captures vérifiées dans `/tmp/vigil-project-memory/`. Le typecheck global reste bloqué par les seules références CLI auth préexistantes.
- Aucune extraction automatique ni promotion entre mémoire d’agent et mémoire de projet. Le replay comparatif, la mesure des gains et la promotion réversible restent à construire.
