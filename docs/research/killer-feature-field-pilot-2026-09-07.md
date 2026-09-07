# Pilote terrain killer feature — ouvert 7 septembre 2026

Mandat : **Jeff autorise** le contact / pilote équipe réelle pour éprouver [l’hypothèse](killer-feature-hypothesis-2026-09-07.md). Seuils inchangés.

## Statut

| Élément | État |
| --- | --- |
| Autorisation outreach / pilote | **Oui** (2026-09-07) |
| Équipe #1 nommée | **Oui (dogfood)** — [Multica dogfood Jeff/vigil-482](killer-feature-team1-dogfood-2026-09-07.md) ; **ne compte pas** pour ≥2 équipes externes. Northline = sim séparée |
| 10 tâches + 5 holdouts figés avant sorties Multica | Suite dogfood + Northline documentée ; pas des tâches client externe |
| Règle candidate depuis vraie correction | Partiel : famille DEV-1 + tokens opaques inventés |
| Comparaison connectée + adopt | Mini 1+1 **eligible** (tent. 3) ; adopt non fait |
| Mesure 10 tâches suivantes (temps revue / reprises / coût) | Fiction sim + 18 s dogfood mémoire ; **instrument** `human_effort_seconds` livré sur Accept |
| Pilote payant (~200 € hypothèse) | **non** |

## Protocole (rappel, non modifié)

1. Figurer **10** tâches historiques autorisées + **5** holdouts **avant** toute lecture des sorties Multica.
2. Extraire **une** règle candidate depuis une **vraie** correction.
3. Comparaison connectée ; n’adopter que si `eligible` + revue humaine chronométrée.
4. Mesurer sur **10** nouvelles tâches de la même famille : temps humain de revue/reprise, défauts récurrents, coût fournisseur (connu / estimé / absent séparés).

**Seuils :** ≥2 équipes avec ≥30 % baisse du temps de revue sans régression holdout critique, et ≥1 acceptation de pilote payant → poursuivre. Sinon itérer ou abandonner le positionnement.

## Broaderillon outreach (à envoyer par Jeff)

**Objet :** Essai Multica — mesurer si une correction d’agent évite les reprises

Bonjour {{prénom}},

On teste une hypothèse précise : après une correction humaine, Multica compare la règle candidate sur des cas de replay + des cas réservés (fixés avant exécution), puis promotion réversible — et on veut savoir si ça réduit vraiment votre temps de revue sur une famille de tâches récurrentes.

Proposition d’essai borné (ordre de grandeur **200 €**, tarif d’essai non validé — à confirmer avec vous) :

- 1 famille de corrections que vous refaites déjà
- 10 tâches historiques + 5 cas réservés fournis par vous
- 1 comparaison mesurée ; vous gardez le droit de refuser l’adoption
- Mesure : temps de revue/reprise, défauts, coût fournisseur connu vs estimé vs absent

Pas de merge/deploy hors de vos autorisations. Pas de promesse d’exclusivité.

Si intéressé·e, un créneau de 30 min pour choisir la famille de tâches suffit.

Merci,  
Jeff

## Preuves à déposer

`docs/research/evidence/killer-feature-field-pilot-2026-09-07/` — contacts (sans secrets), suite figée, export éval, `pilot-summary` avec `human_seconds`, journal de mesure post-adoption.

## Limite honnête

Un dogfood fondateur seul (n=1) **ne** compte **pas** pour le seuil « ≥2 équipes ». Il peut servir de **répétition** du protocole, pas de preuve commerciale.
