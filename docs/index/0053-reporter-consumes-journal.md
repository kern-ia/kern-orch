---
id: 0053
feature: reporter-consumes-journal
branch: feature/reporter-consumes-journal
status: done
files:
  - internal/cmd/runtime.go
  - internal/cmd/journal_recorder.go
  - internal/cmd/serve.go
tests:
  - internal/cmd/report_journal_equivalence_test.go
  - internal/cmd/journal_recorder_test.go
  - internal/cmd/nested_journal_test.go
decisions:
  - "2026-08-19 : internal/report reste intact — zéro ligne changée sous internal/report, y compris ses tests fixture-pinned (contract_test.go, v2_test.go). Le rewiring vit entièrement dans internal/cmd : reportHook (runtime.go) enveloppe le StepFunc que Hook/NestedHook renvoie déjà et lui substitue, au lieu de l'état vivant que l'engine passe à OnStep, l'état que projection.Project reconstruit à partir de store.Read(runID) au même instant. La signature de Hook (StepEvent, flatten, la queue à 64 emplacements, DefaultTimeout/DefaultFlushTimeout) n'a donc aucune raison de bouger."
  - "2026-08-19 : un run imbriqué n'avait, avant cette issue, aucun hook qui vidangeait son journal niveau par niveau — record() ne l'écrit qu'une fois, à l'événement terminal (closesTheRun). reportHook lisant le store à chaque niveau aurait donc vu un journal vide pour toute la durée du run et rattrapé son retard seulement après coup. journalRecorder.flush (Append sans ligne de projection, un run imbriqué n'en a pas) est ajouté et chaîné avant reportHook dans buildChildRunHooks — testé par TestReportedSequenceIsIdenticalBeforeAndAfterTheJournalRewiring et confirmé par TestRunReportsANestedRunForASubgraph (déjà existant, inchangé, toujours vert)."
  - "2026-08-19 : conséquence assumée — un run imbriqué dont le store ne peut pas s'ouvrir ne journalise ni ne reporte plus du tout, alors qu'avant cette issue il pouvait encore reporter sur l'état vivant sans aucun journal. C'est la conséquence directe de « le reporter dérive du journal » : sans journal, il n'y a rien à projeter. buildChildRunHooks documente le changement ; aucun test existant ne couvrait la combinaison store-en-échec + reporter-actif, donc rien ne casse."
  - "2026-08-19 : les événements synthétiques de fermeture de queue (issue 12, Synthetic bool) n'atteignent jamais le fil. Ils ne changent le contenu d'aucun état projeté (projection.Project ignore le niveau ouvert non refermé) et ReportFailure ne dépend d'aucun état — seulement de step/frontier/failed, dérivés de graph.LevelError, inchangés par cette issue. Ajouter un signal d'interruption sur le fil aurait été un changement de contrat (kern.step-event) explicitement hors périmètre ; le risque « un consommateur n'apprend jamais qu'un run a été interrompu » reste donc entier, mais préexistant à cette issue et non aggravé par elle."
  - "2026-08-19 : la preuve d'équivalence fait tourner le même graphe déterministe (un niveau replace, un niveau merge) deux fois — une fois avec Hook câblé directement sur OnStep (l'ancien câblage), une fois avec checkpointHook puis reportHook chaînés (le nouveau, dans le même ordre que serve.go) — et compare les corps JSON reçus par deux sinks HTTP, hors champ « at ». Rejouer un seul run capturé après coup était insuffisant : reportHook lit le journal *au moment où le niveau se ferme*, donc un rejeu découplé de l'exécution laisse les niveaux suivants contaminer la projection d'un niveau antérieur — piège découvert et corrigé pendant l'écriture de ce test."
---

**Quoi** : le reporter HTTP (internal/report) devient un lecteur du journal plutôt qu'un
second observateur du moteur. Rien ne change sur le fil : `kern.step-event/v1` et `/v2`
gardent leurs fixtures octet pour octet, la queue à 64 emplacements et son comportement de
dépôt plutôt que blocage restent identiques, `flatten` continue de n'exposer que la donnée
métier. Le seul changement est *quelle* `*graph.State` le câblage de internal/cmd passe à
`Hook`/`NestedHook` : celle que `journal/projection.Project` reconstruit à partir du store,
lue au même point du cycle de vie que l'ancien état vivant de l'engine.

**Pourquoi** : onzième issue de l'Epic 1 (#15), décision 02. C'est la preuve que kern-ui n'a
jamais été dans le rayon d'impact de cet epic : le contrat externe ne bouge pas d'un octet
pendant que sa source interne change complètement.

**Vérifié en réel** : `go build ./...`, `go vet ./...`, `go test ./...` et
`go test -race -count=1 ./...` verts sur les 14 paquets. `git diff --stat origin/dev -- contracts/`
et `git diff --stat origin/dev -- internal/report/` sont tous deux vides. La séquence émise
avant/après est comparée directement (voir décisions) plutôt que supposée identique parce que
« ça compile ».
