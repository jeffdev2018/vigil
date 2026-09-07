# Audit — alertes et plafonds budget (6 septembre 2026)

Objectif ledger : couverture documentée, budget sur une frontière réellement contrôlée, refus explicite et tests ; ne pas promettre de bloquer des actions externes hors contrôle.

## Frontières contrôlées (refus Multica)

| Plafond | Frontière | Refus | Alerte |
|---|---|---|---|
| Compteur d’issues (`issue_count`) | Création d’issue (API, triage, quick-create) | Oui (`IssueLimitReachedError` / `issue_limit_reached`) | UI Billing ≥ ~80 % |
| Exécutions autopilot (`autopilot_runs`) | Admission d’un run (réservation période) | Oui (`AutopilotQuotaExceededError`) + Inbox 1re rejet/période | UI Billing ≥ ~80 % |
| Sièges humains | Ajout membre / invitation | Oui (`seat_capacity_*`) | Compteurs Billing |

Sources : `server/internal/service/issue_limit.go`, `autopilot_quota.go`, `server/internal/entitlement/`, Billing Settings.

## Hors contrôle (ne pas promettre un blocage)

- CLI fournisseur déjà lancé (tokens / coût modèle pendant le run)
- Quota / facturation Anthropic, OpenAI, etc. hors Multica
- Merge GitHub, déploiement, actions cloud externes
- Concurrence daemon : file d’attente, pas un refus d’enqueue plan

## Tranche livrée

- Helper pur `deriveUsageBudgetLevel` (`ok` / `alert` / `blocked`)
- Billing : barre + libellés d’approche pour issues et automation
- Docs workspaces (+ renvoi autopilots) : tableau honnête des plafonds
- Pas de nouveau plafond USD : couverture coût hétérogène ; réserver un gate Cloud dédié si produit l’exige

## Preuve

Tests ciblés core `usage-budget` + views `billing-state` ; refus durs déjà couverts par les suites Go issue_limit / autopilot_quota.
