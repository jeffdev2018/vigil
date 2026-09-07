# Activation — mesure des abandons / délais (7 septembre 2026)

## Funnel PostHog (jointure)

| Étape | Event | Où |
|---|---|---|
| Onboarding terminé | `onboarding_completed` | serveur (`server/internal/analytics`) |
| Checklist Runtimes vue encore bloquée | `activation_checklist_viewed` | client (`ActivationReadinessCard`) |
| Premier run utile | `issue_executed` | serveur |

Propriétés utiles sur `activation_checklist_viewed` :

- `workspace_id`
- `blocked_required_count`
- `blocked_steps` (ids requis encore `blocked` / `unknown`)
- `minutes_since_onboarding` (si `onboarded_at` connu)

## Lectures abandon / délai

1. **Abandon avant premier résultat** : personnes avec `onboarding_completed` sans `issue_executed` dans une fenêtre (ex. 10 / 60 / 1440 min).
2. **Vu bloqué puis disparu** : `activation_checklist_viewed` sans `issue_executed` ensuite — abandon après avoir vu la checklist.
3. **Délai** : médiane `issue_executed.timestamp − onboarding_completed.timestamp` ; comparer à l’objectif pilote &lt; 10 minutes.
4. **Étape dominante** : top `blocked_steps` sur les vues sans `issue_executed` dans la fenêtre.

Ce n’est pas un cron serveur : la jointure se fait dans PostHog (ou export). Pas d’appel fournisseur requis.

## Preuve rendu

Harness : `docs/research/activation-readiness-preview-2026-09-07.cjs`  
Captures : `/tmp/vigil-activation/` (`blocked-desktop.png`, `blocked-phone.png`, `ready-hidden-desktop.png`).

## Hors tranche

Pilote réel &lt; 10 min avec équipes cibles — observation humaine, pas automatisable ici.
