# Revue Astra Advisor — contexte et statistiques mémoire

## Périmètre et ordre

Cette passe applique le skill installé `astra-advisor:orchestration` au lot déjà implémenté : contexte du dernier dispatch, statistiques des runs conservés, exclusion des chats, API et interface web/desktop. Elle ne valide pas les autres modifications présentes dans le worktree ni l'efficacité de la mémoire sur de nouvelles tâches.

1. Relire les contrats, les écritures transactionnelles, l'agrégation et le raccordement UI.
2. Vérifier le lot avec une seule commande lourde à la fois, sur la base de test isolée.
3. Obtenir une revue indépendante en lecture seule ; corriger puis revérifier chaque problème bloquant avant une nouvelle revue.
4. Conserver les preuves et les limites dans le registre de campagne.

Le modèle exact et l'effort du parent ne sont pas observables dans les métadonnées disponibles. Revue demandée : `gpt-5.6-terra`, effort `high`, contexte neuf. La demande de routage ne prouve pas le modèle réellement exécuté. Aucun autre worker n'est lancé en parallèle.

## Vérifications du parent

Exécutées le 6 septembre 2026 :

| Vérification | Résultat |
|---|---|
| Go `Test(AgentMemoryUsage\|TaskMemoryContext)` avec `-race -p 1 -parallel 1 -timeout 90s -count=1`, `GOMAXPROCS=2` | Réussi, 2,173 s |
| Core : `agents/memory-usage.test.ts`, `api/task-memory-context.test.ts`, Vitest avec un worker | 2 tests réussis |
| Views : `memory-usage-section.test.tsx`, `memory-tab.test.tsx`, `memory-context-details.test.tsx`, Vitest avec un worker | 14 tests réussis |
| `pnpm exec turbo run typecheck --concurrency=1` | 10 tâches réussies, dont 7 issues du cache |
| `GOMAXPROCS=2 go build -p 1 -o /tmp/vigil-memory-review-server ./cmd/server` | Réussi |
| `git diff --check` | Réussi |
| Chromium : `/tmp/vigil-memory-usage/check.cjs` | Erreur/reprise, couverture, références supprimées, historique, état vide ; aucune erreur JavaScript, largeur de document 390 px pour un viewport de 390 px |

Base utilisée : `multica_memory_review_test_20260904` sur PostgreSQL local. Les tests utilisent des fixtures, sans appel fournisseur réel. Les captures sont dans `/tmp/vigil-memory-usage/` ; la capture mobile a été inspectée. Le harness temporaire utilise des réponses HTTP simulées : il vérifie le rendu et les interactions, pas une connexion navigateur → backend réel. La suite globale Go/TS n'a pas été relancée.

## Revue indépendante

Le reviewer `/root/memory_usage_review` a terminé :

```text
ASTRA REVIEW
VERDICT: ship
REASON: Écriture atomique du contexte et des jetons ; protection contre les
claims anciens ou déjà démarrés ; scoping et accès privé ; exclusion des
chats supprimés et des origines ambiguës ; états inconnus préservés dans l'UI.
FINDINGS: none
RESIDUAL RISK: Contexte préparé seulement, sans preuve de réception par le
daemon, d'adhérence du modèle ou d'apprentissage. Aucun test/build exécuté
par le reviewer ; vérifications exécutées par le parent avant la revue.
```

Le lot est accepté pour ce périmètre. Aucune correction supplémentaire n'a été demandée et aucun code produit n'a été modifié pendant cette passe. Modèle/effort réalisés : non observables ; statut terminé et verdict proviennent du retour natif du reviewer. Lecture seule demandée par instruction, sans isolation technique attestée. Les empreintes des 40 fichiers concernés sont conservées dans `/tmp/vigil-memory-usage/review-before.json` pour contrôler leur stabilité pendant la revue.

## Suite de la campagne

Le [registre complet](implementation-goal-ledger.md) reste la source du périmètre. La preuve d'efficacité reste distincte de cette revue de code : définir une baseline, des cas indépendants autorisés, des critères de réussite et des plafonds avant toute expérimentation. Les statistiques de contexte préparé ne mesurent ni l'adhérence du modèle ni un gain de qualité. La décision commerciale sur une killer feature reste ouverte.

## API-EQUIVALENT COST RECEIPT

Indisponible : les outils natifs n'exposent pas de consommation observée en tokens pour le parent ou le reviewer. Coût et comparaison de prix non calculables ; aucune économie de délégation revendiquée. Aucun montant nul ne doit être déduit de cette absence de télémétrie.
