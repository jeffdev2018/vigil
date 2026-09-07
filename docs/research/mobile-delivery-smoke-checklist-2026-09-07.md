# Smoke checklist — livraison mobile v1.5

Preuve humaine (simulateur / device). Automatisé déjà : schémas/clés (6 tests) + `tsc` mobile.

## Sur une issue avec run terminé + critères

1. Ouvrir la fiche issue → carte **Delivery review** visible sous description.
2. Lire le texte d’honnêteté (board ≠ accept ≠ merge/deploy).
3. Si statut board In Review → bandeau « workflow signal only ».
4. **Review delivery** → cocher critères + evidence → **Accept delivery** → Alert Done optionnel (Keep / Mark Done).
5. Autre run : **Request corrections** → **Start correction** → message run créé.
6. Conflit 409 : draft conservé + message conflict (si possible via 2 clients).

## Hors scope de cette checklist

Historique paginé, dialogue coût, teach-memory, transcript — coupures v1.5 volontaires.
