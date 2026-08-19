---
id: 0052
feature: replay-equivalence-invariant
branch: feature/replay-equivalence
status: done
files: []
tests:
  - internal/cmd/replay_equivalence_test.go
  - internal/cmd/freeze_journal_test.go
  - internal/cmd/nested_journal_test.go
decisions:
  - "2026-08-19 : l'invariant est asserté **sur de vrais runs du moteur**, dans `internal/cmd` — le seul paquet qui tient à la fois le moteur, l'adaptateur `graph.Event -> journal.Event` et le store. Alternative écartée : un test unitaire de `projection` sur des tranches d'événements écrites à la main ; il prouve la projection cohérente avec elle-même et ne dit rien des **sites d'émission**, or c'est de là que vient un événement manquant. Aucun code de production n'est touché par cette issue : la vérification à l'exécution (opt-in) est l'issue 13."
  - "2026-08-19 : la comparaison passe par l'encodage JSON de `graph.State` plutôt que champ par champ. Deux raisons : `MarshalJSON` est le seul endroit qui rend déjà l'état entier (data, zones, `Step`, `Frozen`), donc un champ ajouté plus tard est comparé sans toucher à l'assertion ; et le journal fait un aller-retour JSON, si bien qu'un entier écrit par un nœud revient en `float64` — comparer les encodages porte sur ce que l'état contient, pas sur le type Go qui l'a transporté."
  - "2026-08-19 : **le gel dans un fan-out est couvert comme un refus, pas comme une équivalence.** `Merge` ne transporte ni les suppressions de clés ni le compteur `Frozen` : il n'existe aucun état que ce niveau pourrait combiner. Le moteur fait donc échouer le niveau en direct (issue 06), et le test vérifie les deux moitiés du contrat — le run est refusé, *et* ce que le journal a quand même enregistré se rejoue. La mutation qui retire ce garde-fou le montre exactement : le run ne meurt plus au niveau, il meurt à l'écriture, sur un journal que le rejeu refuse."
  - "2026-08-19 : un **run repris** est ajouté aux cas, parce qu'il traverse la règle de réouverture de l'issue 12 : le second `RunStarted` rouvre le record et jette le niveau resté ouvert. L'équivalence y dit quelque chose de plus fort que la seule reprise : le niveau abandonné n'a rien contribué, donc les écritures partielles de la tentative ratée doivent être absentes de l'état vivant **et** du rejeu."
  - "2026-08-19 : pas de test table-driven (CONVENTIONS.md) et pas d'helper qui masque l'assertion. Le seul facteur commun est `assertReplayEquivalent(t, store, runID, live)` ; chaque cas garde son graphe, ses préconditions sur l'état vivant et sa raison d'être discriminant — la clé contestée du fan-out, le report d'ordre de frontière, la zone qui survit, le `Step` qui compte les nœuds."
---

**Quoi** : sept cas, sur de vrais runs, asserent que `Project(journal du run)` est exactement
l'état que le run a laissé — frontière à un nœud, fan-out, gel (carry-over par défaut et non
défaut), nudge, approbation, sous-graphe imbriqué, run repris ; plus un huitième cas qui est un
**refus** (gel dans un fan-out). Trois d'entre eux existaient depuis l'issue 06 et passent
désormais par la même assertion.

**Pourquoi** : dixième issue de l'Epic 1 (#14), décision 08. Un invariant que personne n'exécute
est un commentaire, et la panne qu'il garde est silencieuse : une clé d'état qui apparaît sans
événement pour l'expliquer ne casse rien, elle rend juste faux tous les rejeux suivants.

**Vérifié en réel** : `go build ./...`, `go vet ./...`, `go test ./...` et
`go test -race -count=1 ./...` verts sur les 14 paquets (446 tests). Chaque cas est prouvé
**non vide** par une mutation nommée, appliquée puis annulée : zones perdues à la projection
(tombe : frontière à un nœud), ordre de repli inversé (fan-out), `Data` supprimé par
l'adaptateur (approbation), nudge ignoré au rejeu (nudge), gel reconstruit en « état
précédent moins `Dropped` » (tombe : **uniquement** le carry-over non défaut — le cas par
défaut passe sous ce modèle faux, ce qui est précisément pourquoi il ne prouve rien seul),
niveau ouvert conservé à la réouverture (run repris), garde-fou du gel-en-fan-out retiré
(refus), avancement du `Step` supprimé (tombe : les huit cas, dont le parent imbriqué).
