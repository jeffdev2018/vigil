# Mémoire projet — réutilisation cross-agent — 7 septembre 2026

Prouve qu’une règle **publiée par un humain** sur le projet est préparée et utilisée par un **autre** agent que celui des pilotes mémoire agent, sans que ce token existe dans sa mémoire agent.

## Produit vs « apprentissage autonome »

| Attendu marketing | Réalité produit |
| --- | --- |
| L’agent « apprend seul » et propage | Non : PUT `/api/projects/{id}/memory` (owner/admin) ou promotion depuis une correction de livraison |
| Règles projet auto-extraites | Non : draft → publish / restore, révision optimistic |
| Réduction garantie des reprises | Non mesurée ici |

L’UI `ProjectMemorySection` expose **Edit → publish** ; pas d’écriture autonome daemon→projet.

## Protocole (vigil-482)

1. Projet Bugfix `517f91bf-…` : mémoire rev **1** (2 règles existantes).
2. Humain PUT → rev **2** ajoute : dock code opaque `quay-77`.
3. Agent **Bugfix recipe pilot** `c267cded-…` : **aucune** mémoire agent contenant `quay-77` (1 pending sans ce token).
4. Issue **DEV-6** assignée à cet agent ; consigne : commenter uniquement le dock code, pas d’outils.

## Résultat

| Étape | Détail |
| --- | --- |
| Publication | rev **2**, règle `quay-77` |
| Task | `memory_context.project_version = { id: 517f91bf-…, revision: 2 }` ; `agent_status=loaded` (mémoire agent vide de versions) |
| Commentaire agent | `quay-77` exact |
| Usage projet | `started_runs=6`, `runs_with_project_memory=5` ; rev2 `prepared_runs=1` |

## Limites

- Synthétique (famille Northline / dock code), pas un client.
- Ne prouve pas l’apprentissage autonome (hors produit) ni moins de reprises métier.
- Un agent / un token ; pas une matrice multi-exécuteurs.

## Preuves

[evidence/project-memory-cross-agent-2026-09-07/](evidence/project-memory-cross-agent-2026-09-07/)
