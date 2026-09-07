# SIMULATION — pilote terrain killer feature (équipe inventée)

**ATTENTION — ce n’est pas une preuve commerciale.**

Jeff a demandé d’« inventer » l’équipe #1 (2026-09-07). Ce document est une **répétition du protocole** avec une équipe **fictive**. Il ne compte **pas** pour :

- le seuil ≥2 équipes
- la baisse de temps de revue mesurée chez un client
- l’acceptation d’un pilote payant
- toute affirmation d’avantage concurrentiel

Pour un vrai terrain : remplacer cette fiche par un contact réel et refaire les étapes avec ses données.

---

## Équipe fictive (inventée)

| Champ | Valeur inventée |
| --- | --- |
| Nom | **Northline Labs** (fictif) |
| Lead | « Maya Chen » (persona) |
| Taille | 6 ingénieurs (inventé) |
| Contexte | petite équipe qui fait déjà corriger des fixtures de tests par un agent Claude |
| Contact | `maya.chen@northline.example` (**domaine .example — non joignable**) |
| Engagement payant | **non** — persona uniquement |

Outreach : **non envoyé** (adresse inventée).

---

## Famille de corrections (répétition)

Famille choisie pour coller au dogfood Multica déjà réel : **« fixture add() / assertions numériques exactes »** (liée à DEV-1 / bug-fix), élargie en suite **inventée** pour exercer le protocole 10+5.

### Cas figés AVANT exécution Multica (inventés — horodatage de figement)

Figés le **2026-09-07T18:44:00Z** dans ce fichier, avant tout nouvel export d’éval pour cette simulation.

**Replay (10) — prompts + réponses attendues inventés :**

| id | prompt (résumé) | expected |
| --- | --- | --- |
| r01 | What does add(2,3) return? Reply with only the number. | `5` |
| r02 | What does add(0,0) return? Reply with only the number. | `0` |
| r03 | What does add(-1,4) return? Reply with only the number. | `3` |
| r04 | What does add(10,15) return? Reply with only the number. | `25` |
| r05 | What does add(100,-40) return? Reply with only the number. | `60` |
| r06 | Should add coerce strings? Reply yes or no. | `no` |
| r07 | What error for add(2)? One word. | `arity` |
| r08 | Preferred test command for this fixture? | `npm test` |
| r09 | Branch prefix for this fix family? | `fix/` |
| r10 | After green tests, open a PR? yes/no | `yes` |

**Holdout (5) — inventés, non utilisés pour écrire la règle :**

| id | prompt (résumé) | expected |
| --- | --- | --- |
| h01 | What does add(7,8) return? Only the number. | `15` |
| h02 | What does add(-3,-6) return? Only the number. | `-9` |
| h03 | May add mutate globals? yes/no | `no` |
| h04 | Minimum assertions in the fixture test file? | `1` |
| h05 | PR title must reference issue id? yes/no | `yes` |

### Règle candidate (dérivée d’une vraie correction Multica, reformulée)

Source réelle : pilote DEV-1 — `add` doit retourner la somme (plus de gate `FIX_APPLIED`) ; tests verts ; PR ouverte.

Texte candidat (à coller en mémoire pending) :

```
For the add() fixture family: implement add(a,b) as numeric sum with exact arity 2; never coerce strings; never mutate globals. Prove with npm test. Open a PR on branch prefix fix/ whose title references the issue id. When asked numeric results, reply with only the number. When asked yes/no policy questions above, reply with only yes or no or the single expected token.
```

---

## Exécution produit (partielle — bornée)

Le protocole complet (10+5 × baseline/candidate) n’est **pas** lancé (coût hors répétition).

### Tentatives connectées (vigil-482, agent Memory connected Claude pilot)

| Tentative | Cas | Résultat | Leçon |
| --- | --- | --- | --- |
| 1 | add(2,3)/add(7,8) | `eligible=false` — baseline **déjà** 2/2 | L’arithmétique générale ne mesure pas l’effet mémoire |
| 2 | `npm test` / `fix/` | `eligible=false` — baseline déjà 2/2 | Prompts trop devinables |
| 3 | tokens opaques `harbor-17` / `NL-ADD-9` | **`eligible=true`** — 0/2 → 2/2 | Gate OK ; règle + cas figés avant exécution |

Éval #3 : `cb9ec8a1-…` mémoire `fc3905fd-…` rev 3. Preuves : [evidence/killer-feature-sim-northline-2026-09-07/](evidence/killer-feature-sim-northline-2026-09-07/).

Revue humaine sim : 24 s (`pilot-summary` — 8 s baseline rejet + 16 s candidate accept). **USD facturé / cost_status API :** absents sur ce binaire API local (pas redémarré avec le correctif catalogue).

- [x] Comparaison connectée minimale (1+1) exécutée (tent. 3)
- [x] `eligible` observé
- [x] Revue humaine chronométrée (sim)
- [ ] Adopt/restore (non requis pour la répétition)

---

## Mesure « post-adoption » (inventée — ne pas citer comme donnée)

| Métrique | Valeur inventée | Statut |
| --- | --- | --- |
| Temps revue avant | 12 min / tâche | **fiction** |
| Temps revue après | 8 min / tâche (−33 %) | **fiction** |
| Régressions holdout | 0 | **fiction** |
| Pilote payant accepté | non | **fiction / N/A** |

**Verdict simulation :** protocole **mécaniquement** répétable (gate + revue). **Aucune** conclusion commerciale. Northline n’existe pas.

---

## Suite réelle

1. Remplacer Northline par un contact joignable.
2. Refaire figement 10+5 avec **leurs** tâches.
3. Une seule règle depuis **leur** correction.
4. Mesurer pour de vrai ; ne jamais recycler les chiffres de cette page.
