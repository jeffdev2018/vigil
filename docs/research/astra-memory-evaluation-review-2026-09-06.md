# Comparaison de mémoire — tranche CLI hors ligne, 6 septembre 2026

## Périmètre

Nouveau package Go `internal/memoryeval`, commandes `agent memory evaluate/adopt/restore`,
tests ciblés et documentation. Le moteur fige la candidate, les autres mémoires actives,
l’image et les cas avant de comparer. Vérification indépendante dans des conteneurs
neufs, baseline/candidate en série, replay et holdout distincts, rapport local avec
preuves et contrôle conservateur avant adoption humaine. Les mises à jour réutilisent
les permissions et versions de l’API mémoire existante. Aucune migration ni UI ajoutée.

Les skills intégrés de création d’agent et leurs références documentent les nouvelles
commandes. La conformité des skills a aussi révélé un dépassement antérieur de 15 lignes
du budget du skill issues : son détail API/lifecycle des décisions a été déplacé vers
sa référence existante, sans retirer les consignes.

## Vérification parent

- Inspection complète des fichiers du lot et des diffs documentaires.
- `GOMAXPROCS=2 MULTICA_EVAL_TEST_IMAGE=sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc go test -p 1 -parallel 1 -race ./internal/memoryeval ./cmd/multica -run 'TestAdoptionGate|TestSnapshotRejects|TestDockerMemoryComparison|TestMemoryAdoptionCLI' -count=1 -v` : réussi.
- Docker exécute un shell déterministe, aucun CLI d’agent ou fournisseur. Cas vérifiés : tests cachés au worker, mémoire absente du vérificateur, fixture montée en lecture seule, aucune route réseau par défaut, amélioration par paire, erreur d’infrastructure du vérificateur, timeout, plafond de sortie.
- Le test de sortie a détecté un contournement via `bytes.Buffer.ReadFrom` hérité : remplacement de l’embarquement par un champ nommé et non-régression `io.Copy`, puis test Docker repassé.
- `GOMAXPROCS=2 go build -p 1 -o /tmp/vigil-memory-eval-cli ./cmd/multica` : réussi ; aide des trois commandes inspectée.
- `GOMAXPROCS=2 go test -p 1 -parallel 1 ./internal/service -run TestBuiltinSkillsConformToTemplate -count=1` : réussi après déplacement documentaire.
- `git diff --check` : réussi. Aucun conteneur `multica-eval-container-*` restant après les tests.

## Revue indépendante

Demandée à `/root/memory_evaluation_review`, `gpt-5.6-terra` / `high`, contexte neuf,
lecture seule par consigne. Modèle et effort effectivement exécutés non exposés par
les métadonnées de délégation. Verdict reçu : **ship**, aucun finding bloquant ou
avertissement. La revue confirme l’usage de l’autorisation humaine et du CAS existants,
la séparation exécutant/vérificateur et le recalcul du contrôle d’adoption. Elle conserve
comme limites les rapports locaux non signés, le contexte hors ligne distinct du runtime
produit et la course documentée sur les autres mémoires actives. Aucun changement de
code n’a été effectué après cette revue ; seuls ce reçu et le registre sont finalisés.

## Limites

Ce lot vérifie le worker hors ligne configuré, pas le runtime de production complet.
Une réussite du shell fixture ne démontre ni gain LLM, ni exclusivité commerciale.
Les données représentatives et l’indépendance sémantique du holdout restent à établir
par l’humain ; des empreintes différentes n’en sont pas la preuve. Le coût et l’effort
humain restent `null`, jamais zéro. Le rapport local non signé n’est pas une attestation.
Le CAS serveur protège la candidate, pas les modifications concurrentes d’autres
mémoires après la lecture CLI. UI, conservation serveur et exécutions fournisseur
contrôlées ne sont pas livrées dans ce lot.

## API-EQUIVALENT COST RECEIPT

Usage parent et reviewer indisponible : les outils natifs n’exposent pas de tokens
observés. Coût routé, repricing Astra et différence de prix non calculables. Aucun
montant nul, économie de crédits ou de souscription revendiqué. Périmètre : cette
tranche seulement, hors travaux des tours précédents.
