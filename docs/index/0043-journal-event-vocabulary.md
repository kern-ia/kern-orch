---
id: 0043
feature: journal-event-vocabulary
branch: feature/journal-event-vocabulary
status: done
files:
  - internal/journal/event.go
  - internal/journal/payload.go
tests:
  - internal/journal/event_test.go
  - internal/journal/isolation_test.go
decisions:
  - "2026-08-19 : le journal reçoit son propre type Event (RunID, Seq, At, Payload) au lieu de réutiliser kern.step-event/v1 ou /v2 — StepEvent aplatit volontairement le State en données métier et ne porte ni zones, ni compteur de freeze, ni règle de combinaison ; le rejouable a besoin exactement de ce que le contrat refuse de transporter."
  - "2026-08-19 : union fermée via une méthode non exportée journalPayload() sur l'interface Payload — restreint les implémentations au package, donc le switch de kindOf/decodePayload (event.go) est exhaustif par construction : un nouveau type de payload sans cas ajouté échoue à la compilation dès qu'il est encodé une fois."
  - "2026-08-19 : LevelClosed porte la CombinationRule appliquée (replace | merge) comme donnée explicite plutôt que de la laisser se déduire des clés — un frontier à un nœud (replace) et un fan-out qui touche les mêmes clés (merge) produisent le même ensemble de clés en sortie ; seul l'événement peut distinguer les deux."
  - "2026-08-19 : FreezeApplied porte CarriedOver et Dropped comme deux champs indépendants, pas un delta — State.Freeze remplace le contenu de l'état plutôt que de le modifier, donc modéliser Freeze comme des suppressions de clés reconstruirait un état que le run n'a jamais eu sous tout carry-over non par défaut."
  - "2026-08-19 : décodage d'un kind inconnu ou absent refusé explicitement plutôt qu'ignoré — même règle fail-loud que le versionnement de schéma (fiche 0042), appliquée ici au point où un événement non reconnu passerait silencieusement à travers le replay."
---

**Quoi** : nouveau package `internal/journal` déclarant le vocabulaire interne du journal —
11 types d'événements (cycle de vie du run, ouverture/fermeture de niveau, faits par nœud,
nudge, freeze), un `Event` enveloppe avec numéro de séquence monotone par run, encodage/
décodage JSON round-trip, et une union fermée sur `Payload` (méthode non exportée +
switch exhaustif). Types seulement : aucun store, aucun câblage moteur, aucune projection.

**Pourquoi** : deuxième issue de l'Epic 1 (#6), la décision la plus structurante du ledger —
`kern.step-event/v1`/`v2` reste inchangé et devient une projection du journal, jamais
l'inverse. `StepEvent` aplatit délibérément l'état ("marshalling the State itself would ship
kern-orch's envelope ... across the contract, which is nobody else's business") ; le rejeu
exact a besoin de zones, du compteur de freeze et de l'attribution par nœud, que le contrat
refuse de porter par conception. Un seul type pour les deux usages aurait forcé l'un des deux
à céder.

**Vérifié en réel** : `go build ./...`, `go vet ./...` et `go test ./...` verts sur les
13 packages. 14 tests dans `internal/journal` : un round-trip JSON par type d'événement, la
distinction `replace`/`merge` après round-trip, `carried_over`/`dropped` comme champs
indépendants après round-trip, un `nudge_applied` qui porte son origine, un décodage de kind
inconnu et un décodage de kind absent qui échouent tous deux explicitement, et un test qui
parse les fichiers source du package pour garantir qu'aucun n'importe
`internal/report` (vérifié dans les deux sens via `go list -deps`). Les fixtures
`contracts/*.json` et les tests d'`internal/report` sont inchangés.
