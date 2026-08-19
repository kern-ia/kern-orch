---
id: 0048
feature: non-node-mutations-as-events
branch: feature/freeze-events
status: done
files:
  - internal/graph/event.go
  - internal/graph/engine.go
  - internal/cmd/journal_recorder.go
tests:
  - internal/graph/event_test.go
  - internal/graph/zones_test.go
  - internal/cmd/journal_recorder_test.go
  - internal/cmd/freeze_journal_test.go
  - internal/journal/projection/projection_test.go
decisions:
  - "2026-08-19 : le nudge est adopté tel quel plutôt que réémis. L'issue 07 avait dû l'anticiper (voir la fiche de dérive `docs/epics/epic-1-event-journal-source-of-truth/drift/07-nudge-event-recorded-early.md`) : `steer.Mailbox.DrainNudges` retourne déjà ce qu'il a appliqué, et `journalRecorder.recordNudge` écrit déjà `journal.NudgeApplied` avant le niveau qu'il précède. Réémettre un second événement ici aurait rejoué le nudge deux fois. `nudgeOrigin` reste la constante `\"steer\"` (la surface, pas une personne) : `steer.Mailbox` ne porte pas le requester, et l'inventer aurait été une attribution fabriquée."
  - "2026-08-19 : la détection d'un Freeze reste côté `internal/graph`, pas côté de l'outil `freeze` (`internal/cmd/runtime.go`). `Engine.runLevel` compare déjà `branch.Frozen` à `s.Frozen` après chaque `Execute` pour construire `EventNodeProduced` ; comparer aussi les deux compteurs répond à la question \"un Freeze a-t-il eu lieu\" sans changer la signature de `State.Freeze` ni instrumenter chaque outil qui pourrait l'appeler. `runtime.go` n'a donc pas été touché."
  - "2026-08-19 : `FreezeApplied.CarriedOver` est le contenu **entier** de la branche après `Execute`, pas un diff comme `NodeProduced.Data`. `State.Freeze` remplace l'état en bloc ; un diff perdrait silencieusement toute clé conservée dont la valeur n'a pas changé. `Dropped` est calculé en comparant les clés d'avant (l'état partagé, non muté pendant le niveau) à celles d'après — c'est une information d'audit, `projection.Project` (issue 04) ne s'en sert jamais pour reconstruire."
  - "2026-08-19 : un Freeze dans un fan-out (>1 nœud) fait maintenant échouer le niveau en direct (nouveau comportement de `runLevel`), au lieu de laisser passer un run que le replay aurait ensuite refusé (`projection.Project` erreurait déjà sur ce cas, issue 04). `State.Merge` ne fusionne ni `Frozen` ni les clés supprimées : un run que l'exécution en direct accepte silencieusement mais que le replay refuse est une incohérence, pas une limite de la projection."
  - "2026-08-19 : la combinaison a été vérifiée bout en bout plutôt qu'en isolation — trois tests dans `internal/cmd/freeze_journal_test.go` font tourner un vrai `graph.Engine` câblé au `journalRecorder`, puis comparent `projection.Project(journal)` à l'état vivant : gel avec `DefaultCarryOver`, gel avec un carry-over non par défaut (renomme une valeur, invente une clé, garde une clé éphémère), et un run nudgé. Non-vacuité vérifiée en réimplémentant temporairement `projector.freeze` en \"état précédent moins Dropped\" (sans jamais lire CarriedOver) : le test au carry-over par défaut reste vert, celui au carry-over non par défaut échoue exactement comme prévu — confirmant que seule l'installation intégrale de CarriedOver reconstruit l'état correctement."
---

**Quoi** : le Freeze devient un événement de premier ordre. `graph.Engine.runLevel` détecte
qu'un nœud a appelé `State.Freeze` (le compteur `Frozen` a bougé) et émet
`EventFreezeApplied{CarriedOver, Dropped}` à la place de `EventNodeProduced` — une
installation en bloc, pas un diff. `internal/cmd/journal_recorder.go` fait traverser ça vers
`journal.FreezeApplied` (déjà déclaré par l'issue 02, déjà rejoué par l'issue 04). Un Freeze
dans un fan-out fait désormais échouer le niveau en direct plutôt que produire un journal que
le replay refuserait. Le nudge n'a pas bougé : il était déjà correctement enregistré par
l'issue 07 sous la pression de la même invariante.

**Pourquoi** : sixième issue de l'Epic 1 (#10), décision 06. L'invariante "tout ce qu'un nœud
peut lire doit être reconstructible depuis le journal" n'a de sens que si les trois mutations
hors-nœud (nudge, freeze, règle de combinaison) sont toutes enregistrées ; le nudge l'était
déjà, la règle de combinaison l'était depuis l'issue 05, restait le Freeze.

**Vérifié en réel** : `go build ./...`, `go vet ./...`, `go test ./...` et
`go test -race -count=1 ./...` verts sur les 14 paquets. `internal/graph/event_test.go`
prouve red→green en désactivant temporairement (`if false &&`) la détection de Freeze et le
refus en fan-out : les deux tests dédiés échouent alors exactement comme attendu. Le run bout
en bout au carry-over non par défaut a été rendu non-vacuous en réimplémentant temporairement
`projector.freeze` en modèle delta — voir la décision ci-dessus.
