# Hypothèse killer feature — actualisation 7 septembre 2026

Mise à jour de [switching-value-paperclip-2026-09-04.md](switching-value-paperclip-2026-09-04.md) et de la [carte concurrentielle](competitor-map-2026-09-04.md) après les pilotes Multica du 6–7 septembre. Sources primaires reconsultées le **2026-09-07**. Aucune exclusivité ni volonté de payer affirmée.

## Hypothèse (inchangée dans l’intention)

**Promesse à éprouver :** Multica transforme une correction humaine en règle/compétence **comparée** (baseline vs candidate), sur des cas de **replay et holdout** fixés avant exécution, puis **promotion réversible** — et cela réduit le temps de supervision sur une famille de tâches réelles, sans dégrader les cas réservés.

Ce n’est **pas** « avoir de la mémoire », « approuver une écriture » ou « journaliser des leçons ». Ces capacités existent déjà chez des concurrents (ci-dessous).

## Ce que Multica a démontré (preuve produit, pas commerciale)

| Preuve | Portée | Limite |
| --- | --- | --- |
| Comparaison mémoire **connectée** Claude produit | 0/2 → 2/2, `eligible=true`, adopt rev3 / restore rev4 | Cas **synthétiques** ; revue humaine 18 s / 2 acceptations ; USD `null` ; observations ≠ attestation ([pilote](memory-connected-claude-pilot-2026-09-07.md)) |
| Pilote Codex connecté 6 sept. | Même famille de preuve | Idem |
| Mémoire projet + `memory/usage` | DEV-2 : révision consommée sur un run | Pas de mesure « moins de reprises » |
| Recette bug → PR → Accept | DEV-1 réel | Pas le cycle correction→compétence ; GitHub App absent |

**Conclusion honnête :** le **mécanisme** de la promesse (candidate → gate replay/holdout → adopt/restore) est désormais un parcours produit local. La **promesse commerciale** (temps humain ÷2 sur tâches récurrentes d’une équipe) n’est **pas** démontrée.

## Bar concurrentielle (sources du 7 sept. 2026)

| Concurrent | Ce qu’ils documentent | Écart vs notre hypothèse |
| --- | --- | --- |
| [Paperclip — Good Enough to Great](https://paperclip.ing/blog/agents-good-enough-to-great/) | Leçons après revue, mémoire d’entreprise versionnée, propriétaire humain | Promotion humaine **sans** (dans ces pages) comparaison baseline/candidate sur holdout avant activation |
| [Paperclip #3326 LEARNINGS.md](https://github.com/paperclipai/paperclip/issues/3326) | Journal fichier auto-injecté | Injection continue ≠ gate d’éligibilité Multica |
| [Paperclip trust / quarantine](https://docs.paperclip.ing/administration/trust-and-low-trust-review/) | Quarantaine puis promotion manuelle d’artefacts | Contrôle de confiance, pas replay métier |
| [Paperclip agent behavior evals](https://deepwiki.com/paperclipai/paperclip/10.3-agent-behavior-evals) | Promptfoo / conformité protocole | Évalue le **comportement plateforme**, pas l’effet d’une règle métier sur cas réservés |
| [Hermes memory/skills write_approval](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory) | Gate approve/deny avant écriture | Empêche une mauvaise sauvegarde ; **ne compare pas** l’effet sur tâches |
| [Letta Context-Bench / memory eval](https://www.letta.com/blog/evaluating-memory-in-production-agents/) | Benchmark **modèles** (usage/génération de mémoire) | Sélection de modèle, pas économie d’équipe sur corrections réelles |

**Inférence (confiance moyenne) :** le discours « apprendre des revues » est saturé. Un acheteur peut déjà obtenir approbation d’écriture (Hermes) ou leçons versionnées (Paperclip). Notre différentiel **candidat** reste : **preuve exécutable avant promotion**, sur les tâches de *cette* équipe, avec coût humain total et retour arrière. Ce différentiel n’est pas encore une raison d’acheter mesurée.

## Hypothèse falsifiable (test proposé, non exécuté)

**Population :** 3 équipes de 3–15 personnes qui répètent déjà des corrections du même type aux agents.

**Protocole :**
1. Figurer 10 tâches historiques autorisées + 5 holdouts **avant** toute lecture des sorties Multica.
2. Extraire **une** règle candidate depuis une vraie correction (pas synthétique).
3. Lancer la comparaison connectée (Claude ou Codex) ; n’adopter que si `eligible` et revue humaine.
4. Mesurer sur 10 nouvelles tâches de la même famille : temps humain de revue/reprise, défauts récurrents, coût fournisseur (connu / estimé / absent séparés).

**Seuils (choix de test, pas science) :**
- **Poursuivre** si ≥2 équipes voient ≥30 % de baisse du temps de revue **sans** régression holdout critique, et ≥1 accepte un pilote payant (ordre de grandeur déjà proposé : 200 € — non validé).
- **Itérer** si gain mesuré mais refus d’achat pour motif nommé.
- **Abandonner ce positionnement** si aucun gain reproductible ou zéro paiement.

**Falsification immédiate déjà partiellement ouverte :** si sur tâches réelles le gain disparaît dès qu’on compte la configuration des cas et la revue d’éval, le pari principal échoue même si le gate technique reste vert.

## Ce qu’il ne faut pas dire

- Que Multica est le seul à avoir mémoire, skills ou revue humaine.
- Qu’un pilote synthétique 0/2→2/2 prouve un avantage concurrentiel.
- Que les tokens d’adaptateur = coût facturé ou attestation serveur.
- Qu’une exclusivité découle du code livré cette semaine.

## État commercial (actualisé 7 sept. 2026)

**Autorisation :** Jeff a autorisé le contact / pilote équipe réelle (2026-09-07).

| Ouvert | Bloqué / en attente |
| --- | --- |
| Outreach + protocole falsifiable | **Nom + contact de l’équipe #1** |
| Runbook terrain | Mesure supervision ÷2 sur tâches métier |
| | Volonté de payer (≥1 pilote payant) |

Runbook et brouillon d’email : [killer-feature-field-pilot-2026-09-07.md](killer-feature-field-pilot-2026-09-07.md).

**Prochaine action :** nommer l’équipe #1 (ou coller un email / lead), figer 10+5 cas **avant** toute sortie Multica, puis extraire une règle depuis une vraie correction.
