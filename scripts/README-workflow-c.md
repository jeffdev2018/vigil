# Workflow C — Fork Vigil (rebrand permanent de Multica)

## Principe

Ce repo (`jeffdev2018/vigil`) est un fork dur du projet upstream
`multica-ai/multica`. Le rebrand supprime deux fichiers d'upstream (`LICENSE`,
`NOTICE`) et amende `README.md` / `README.zh.md`. Comme upstream continue de
publier ces deux fichiers, **chaque merge/rebase ramène automatiquement la
licence** dans le working tree.

Pour éviter que la licence revienne sans être notée, le workflow est :

```
main                 = branche de suivi upstream (propre, toujours synchrone)
   │
   ▼
upstream/main         ← git fetch upstream
   │
   ▼ (merge dans main)
main                 ← git checkout main; git merge upstream/main
   │
   ▼ (merge dans production)
vigil/rebrand        ← git checkout vigil/rebrand; git merge main
                       → ramène LICENSE + NOTICE dans working tree
                       → lancer scripts/post-merge-upstream.sh
                       → git commit -am "vigil: re-apply rebrand after upstream merge"
                       → git push origin vigil/rebrand
```

## Après chaque merge upstream

```bash
# 1. Mettre main à jour
git checkout main
git merge upstream/main
git push origin main

# 2. Propager dans la branche de production (vigil/rebrand)
git checkout vigil/rebrand
git merge main

# 3. Ré-appliquer le rebrand (supprimer LICENSE/NOTICE revenus du merge)
bash scripts/post-merge-upstream.sh

# 4. Commettre le résultat
git commit -am "vigil: re-apply rebrand after upstream merge ($date)"

# 5. Pousser
git push origin vigil/rebrand
```

## Fichiers supprimés du working tree (récupérables dans git)

- `LICENSE` — la licence Multica complète (327 lignes, Apache 2.0 + conditions BSL)
- `NOTICE` — l'avis d'attribution upstream (14 lignes)

Ces fichiers sont **conservés dans l'historique git** de la branche `main` (et
du commit upstream `64ec7f541`). Ils peuvent être récupérés à tout moment :

```bash
git show upstream/main:LICENSE
git show upstream/main:NOTICE
```

## Remarque juridique (non juridique — note de contexte)

Le propriétaire du repo (`@jeffdev2018`) a pris la décision de supprimer ces
deux fichiers du working tree de la branche `vigil/rebrand` et assume la
responsabilité de cette action. Aucune autorisation écrite de l'équipe
Multica n'a été reçue. Cette documentation est là pour que la décision reste
visible et traçable dans chaque commit.
