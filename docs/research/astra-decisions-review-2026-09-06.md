# Décisions durables — vérification du 6 septembre 2026

Périmètre : nouvelle tranche sur le worktree existant, sans commit ni déploiement.
Les changements mémoire, livraison et CLI auth déjà présents sont conservés.
Le manifeste de revue local est `/tmp/vigil-decisions/review-scope.txt`.

## Contrat implémenté

- Demande identifiée par UUID, question/contexte/options fournis explicitement, run source et destinataire humain. Même identifiant/contenu : même demande ; collision différente : 409. Maximum 20 demandes ouvertes par issue.
- Source limitée à l'issue et au workspace, avec provenance hors chat positive. Les marqueurs privés, inconnus ou mal formés ne sont pas interprétés comme publics.
- Un token de run peut demander/lire pour son propre run. Les tokens machine ne peuvent pas répondre, annuler ou reprendre. Le destinataire doit être membre et autorisé à voir l'agent à la création. Une création humaine doit être adressée à ce même humain, y compris lors du rejeu d'une requête ancienne.
- Réponse ou annulation finale attribuée au destinataire, sérialisée sous verrou d'issue. Deux réponses distinctes concurrentes ont un seul gagnant. Le reçu appartient toujours au destinataire, même si la visibilité de l'agent change ensuite.
- Enregistrement de la réponse et reprise explicite séparés. Le run de reprise et son identifiant sont écrits dans une transaction ; répétition/restart ne crée pas une seconde reprise. La réponse survit aux erreurs. Droits d'invocation, source terminale et travail concurrent sont revérifiés avant la première reprise.
- Reprise par nouveau run/session via le mécanisme existant de correction, avec attribution humaine, lignée et handoff immuable. Ce n'est pas une suspension/reprise du processus CLI en cours. Aucune approbation d'outil, de livraison, de merge ou de déploiement implicite.
- Inbox web/desktop : demandes à traiter, réponses attendant reprise, historique des annulations/reprises, pagination 50, actualisation 15 s. Lire/archiver une notification ne répond pas. Le mobile natif n'est pas modifié.
- CLI `multica issue decision request/get`, skill embarqué, libellés et documentation dans les quatre langues. Nettoyage explicite à la suppression de l'issue/workspace ; aucun nouveau FK.

## Preuves parent

- PostgreSQL isolé : `multica_memory_review_test_20260904` uniquement. Les migrations 488–491 ont été appliquées directement. Le runner complet rencontre le décalage préexistant de la migration 486 : colonne `memory_context` déjà présente sans entrée correspondante dans le registre. Le registre n'a pas été falsifié ni le schéma de développement touché. Rollback 491→488 puis réapplication 488→491 validés sur la table vide : trois index valides et aucun FK.
- `GOMAXPROCS=2 go test -race -p 1 -parallel 1 ./internal/handler ./cmd/multica -run 'TestIssueDecision|TestIssueDelivery' -count=1` : réussi. Couverture : concurrence réponse/reprise, destinataire/workspace/token, chat et provenance ambiguë, annulation, erreur runtime, reçu après suppression du run, pagination, nettoyage et réponse dans le prompt réellement préparé au claim. Les tests de livraison existants protègent le helper partagé.
- `go build -p 1 -o /tmp/vigil-decisions/build/ ./cmd/server ./cmd/multica` : réussi.
- Vitest : 8 tests core (contrats décision/livraison) et 45 tests views (Inbox/décisions) réussis, un worker.
- Typecheck Turbo : 10/10, concurrence 1. ESLint ciblé core/views et `git diff --check` : réussis.
- Chromium : `/tmp/vigil-decisions/check.cjs`, composant réel avec API simulée. Choix sans envoi, réponse sans lancement, 409 conservant la réponse, retry, historique et état vide. Police Inter chargée, aucune erreur JavaScript ; largeur document/viewport = 390/390.
- Captures inspectées : `/tmp/vigil-decisions/phone.png`, `history.png`, `empty.png`. Autres captures : `desktop.png`, `retry.png`. Ce n'est pas un E2E connecté au backend ; la frontière serveur/DB est vérifiée séparément. Aucun appel à un fournisseur IA ni lancement réel d'agent.

## Revue indépendante

Première revue : `/root/decision_review`, `gpt-5.6-terra / high` demandés, verdict **fix-first**. Le reviewer a confirmé une fuite de réponse par retry de création humaine vers un autre destinataire. Correction parent : restriction des créations humaines à soi-même avant toute lecture idempotente, sans modifier les demandes adressées par un token de run. Test ajouté sur une ligne ancienne simulée et répondue par un autre humain. Modèle/effort effectifs et tokens non observables. Les tests Go ciblés avec `-race` ont été rejoués avec succès après la correction. Aucun changement TS/UI depuis les vérifications correspondantes. Compilation serveur/CLI et contrôle du diff rejoués avec succès. Seconde revue fraîche : `/root/decision_review_final`, `gpt-5.6-terra / high` demandés ; verdict **ship**, aucun finding. Le reviewer a retracé routes, autorisations, transactions, SQL/migrations/nettoyage, CLI et UI ; la restriction humaine précède le lookup et les effets de reprise restent après commit. Modèle/effort effectifs et usage non observables. Les deux reviewers travaillent successivement, sans autres workers.

## Limites et suite

Cette tranche fournit une demande durable et une reprise traçable. Elle ne prouve ni une supériorité concurrentielle ni la volonté de payer. La suite reste la validation indépendante des améliorations de mémoire/procédures, la recette complète, les budgets et la parité mobile selon le registre global. La campagne complète n'est pas terminée.

API-EQUIVALENT COST RECEIPT : indisponible ; les outils natifs n'exposent pas l'usage de tokens observé du parent ni des deux reviewers. Périmètre : cette tranche, corrections et deux revues incluses ; aucun coût USD ni comparaison de prix calculable. Aucun montant ou gain n'est inféré.
