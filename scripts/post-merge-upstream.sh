#!/bin/bash
# post-merge-upstream.sh — Ré-applique le rebrand Vigil après chaque merge/rebase upstream
# Utilisation : après un `git merge upstream/main` dans la branche vigil/rebrand
# Ce script doit être lancé manuellement (ou via un alias git) après chaque merge.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# Vérifie qu'on est dans la branche de production (vigil/rebrand ou autre)
BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [[ "$BRANCH" == "main" || "$BRANCH" == "HEAD" ]]; then
    echo "[post-merge-upstream] Attention : tu es sur '$BRANCH', pas sur la branche de production."
    echo "                   Si tu veux appliquer le rebrand sur main, ajoute 'main' au workflow C."
fi

echo "[post-merge-upstream] Suppression de LICENSE et NOTICE (ramenés par upstream) ..."
if git rm -f LICENSE NOTICE 2>/dev/null; then
    echo "[post-merge-upstream] Fichiers supprimés."
else
    echo "[post-merge-upstream] Aucune suppression nécessaire (fichiers déjà absents)."
fi

echo "[post-merge-upstream] Amende le message du merge précédant si nécessaire ..."
echo "NOTE : le rebrand Vigil a été ré-appliqué après le merge upstream."
echo "      Le commit suivant doit inclure : 'vigil: re-apply rebrand after upstream merge'"
