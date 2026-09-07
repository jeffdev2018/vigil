# Pilote mémoire connectée Claude — 7 septembre 2026

Comparaison **connected** sur le runtime produit Claude (daemon `dev`, capability `memory-evaluation-v1`), pas le banc Docker hors ligne.

## Résultat

| Cas | Split | Sans mémoire | Avec candidate |
| --- | --- | --- | --- |
| Org color code | replay | `unknown` (échec) | `teal-7` (réussite) |
| Org badge word | holdout | `unknown` (échec) | `lighthouse` (réussite) |

- `execution_status=completed`, `eligible=true`
- Raison serveur : *checks passed with improvement on replay and holdout; human review required*
- Adoption API → révision **3** active (`evaluation_id` lié) ; restauration → révision **4** pending (contenu de la révision 2)
- `adopted_revision=3` conservé sur l’évaluation `9e17d3cf-…`

Agent dédié **Memory connected Claude pilot** (`5711f250-…`) : instructions « answer only / do not use tools », modèle `claude-sonnet-4-5`, effort `low`, runtime `0c344870-…`. Mémoire candidate `05d52d65-…` révision 2 au lancement.

## Premiers échecs (conservés comme limite opérationnelle)

1. Agent **Bugfix recipe pilot** : holdout `Runtime did not complete` avec `tool_calls=1` — `MaxTurns: 1` dans le worker connecté ; instructions bug-fix poussent aux outils.
2. Même agent d’éval + question « triage queue » : même panne outil.
3. Replay « reply ok » + holdout mémoire : comparaison complète mais **non éligible** (gain holdout seulement ; le gate exige un gain sur *replay et* holdout).

Mitigation du pilote : prompts « Do not use tools / Do not read files » + deux faits synthétiques distincts dans la même candidate.

## Consommation rapportée (4 réponses)

Voir [usage-summary.json](evidence/memory-connected-claude-pilot-2026-09-07/usage-summary.json) : 36 input, 1102 output, 54396 cache_read, 56802 cache_write (tokens d’adaptateur).

**Effort humain (mesuré 2026-09-07, opérateur campagne) :** revue des 4 sorties exportées vs réponses attendues — [human-reviews.json](evidence/memory-connected-claude-pilot-2026-09-07/human-reviews.json) + [human-reviews-meta.json](evidence/memory-connected-claude-pilot-2026-09-07/human-reviews-meta.json). `pilot-summary` : baseline 6 s / 0 accepté ; candidate 12 s / 2 acceptés ; total attribué **18 s**. **USD facturé : toujours inconnu** (`null`). Portée : revue d’artefacts synthétiques, **pas** temps de supervision d’équipe.

## Attestation

Les observations runtime (provider, modèle demandé, effort, hashes exécutable/prompt/brief, usage) sont des **observations d’adaptateur**, pas une attestation fournisseur d’identité modèle ni une preuve de facturation. Depuis le correctif du 7 sept. 2026, l’API évaluation expose `report_hash` (reçu SHA-256 des octets stockés côté serveur). L’empreinte [export-meta.json](evidence/memory-connected-claude-pilot-2026-09-07/export-meta.json) de ce pilote porte encore sur les octets du fichier exporté passé à `agent memory pilot-summary` (export UI = corps `report` seul).

## Preuves

- [exported-report.json](evidence/memory-connected-claude-pilot-2026-09-07/exported-report.json)
- [pilot-summary.json](evidence/memory-connected-claude-pilot-2026-09-07/pilot-summary.json)
- [human-reviews.json](evidence/memory-connected-claude-pilot-2026-09-07/human-reviews.json)
- [human-reviews-meta.json](evidence/memory-connected-claude-pilot-2026-09-07/human-reviews-meta.json)
- [lifecycle.json](evidence/memory-connected-claude-pilot-2026-09-07/lifecycle.json) (adopt 3 → restore 4)
- Env : `vigil-482`, API `:18562`, profil `dev-vigil-482`, daemon pid local `server/bin/multica` version `dev`

## Portée

Démonstration du chemin produit Claude + gate + adoption/restauration sur cas synthétiques. Ce n’est ni un benchmark métier représentatif, ni une preuve d’avantage commercial, ni une attestation cryptographique serveur.
