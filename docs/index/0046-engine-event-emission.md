---
id: 0046
feature: engine-event-emission
branch: feature/engine-event-emission
status: done
files:
  - internal/graph/event.go
  - internal/graph/engine.go
  - internal/cmd/runtime.go
tests:
  - internal/graph/event_test.go
  - internal/cmd/runtime_hooks_test.go
decisions:
  - "2026-08-19 : le port est déclaré sur des types possédés par `graph` (`Event`, `EventKind`, `CombinationRule`) et non sur `journal.Event` — la projection de l'issue 04 reconstruit un `*graph.State` à partir des événements, donc `journal` importe `graph` ; l'import inverse fermerait un cycle que le compilateur refuse. Coût assumé : un vocabulaire miroir tenu par convention plus un switch de mapping dans l'adaptateur ; le coût de l'alternative était de déplacer `State` là où le cycle cesse de faire mal."
  - "2026-08-19 : une struct taguée par `Kind` plutôt qu'une interface à une méthode par fait — l'issue 06 ajoute nudge et freeze ; une interface aurait alors gagné une méthode et cassé toutes les implémentations, une constante de plus ne casse personne. La forme suit `StepInfo`/`StepFunc` déjà en place."
  - "2026-08-19 : `node_produced` porte les clés que le nœud a écrites sur sa branche, comparées à l'état d'entrée du niveau (`reflect.DeepEqual`, valeurs non comparables comprises), et pas l'état complet — sinon le journal grossit avec l'état plutôt qu'avec ce qui s'est passé. Une clé re-taguée par `SetZoned` sans changer de valeur compte comme écrite : le carry-over d'un futur `Freeze` agit dessus."
  - "2026-08-19 : un niveau qui échoue n'émet pas `level_closed` — il n'a combiné aucune branche, donc nommer une règle qu'il n'a jamais appliquée inventerait un fait ; c'est `run_failed` qui ferme le journal, avec les nœuds de `LevelError`."
  - "2026-08-19 : une erreur du hook interrompt le run (comme `StepFunc`) — celui qui enregistre les événements *est* le récit du run ; continuer après un refus produirait un journal silencieusement incomplet. L'erreur d'émission de l'événement terminal est jointe (`errors.Join`) à l'erreur du run, jamais avalée."
---

**Quoi** : `internal/graph` déclare `EventFunc`, le port par lequel le moteur raconte un run —
`run_started/finished/failed`, `level_opened/closed` avec la règle de combinaison appliquée,
`node_started/produced/failed` émis depuis la goroutine de chaque nœud. `Engine.OnEvent`
l'enregistre ; `multiEvent` (internal/cmd) chaîne plusieurs hooks comme `multiStep` le fait
pour `OnStep`. Aucun store, aucun événement nudge/freeze (issues 06 et 07).

**Pourquoi** : cinquième issue de l'Epic 1 (#9). Le moteur calculait déjà quels nœuds d'un
niveau avaient échoué et lesquels avaient abouti — la doc de `LevelError` le dit noir sur
blanc — puis jetait tout sauf l'erreur. Un niveau où `x` échoue et `y` aboutit émet désormais
`node_failed` pour l'un et `node_produced` pour l'autre. C'est le port sur lequel les issues
06, 07 et 09 se branchent.

**Vérifié en réel** : `go build ./...`, `go vet ./...` et `go test ./...` verts sur les
13 packages. `go test -race ./internal/graph/...` vert — critère explicite, l'émission part
des goroutines par nœud. 54 tests dans `internal/graph` (dont 17 nouveaux) et 4 nouveaux dans
`internal/cmd`. Un hook nil reste un no-op qui n'alloue rien : le test mesure 0 allocation par
`emit` via `testing.AllocsPerRun`, la garde vivant dans `emit` pour que l'événement ne
s'échappe pas sur le tas. Deux tests ont été vérifiés par mutation (état complet au lieu des
clés écrites ; règle `merge` forcée) et échouent bien quand le comportement change.
