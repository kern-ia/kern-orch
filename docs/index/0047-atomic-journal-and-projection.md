---
id: 0047
feature: atomic-journal-and-projection
branch: feature/atomic-journal-projection
status: done
files:
  - internal/checkpoint/atomic.go
  - internal/checkpoint/sqlite.go
  - internal/checkpoint/journal.go
  - internal/cmd/journal_recorder.go
  - internal/cmd/runtime.go
  - internal/cmd/serve.go
  - internal/steer/mailbox.go
tests:
  - internal/checkpoint/atomic_test.go
  - internal/cmd/journal_recorder_test.go
  - internal/cmd/atomic_run_test.go
  - internal/steer/mailbox_test.go
decisions:
  - "2026-08-19 : l'état de la ligne `checkpoints` est recalculé par `projection.Project` en relisant le journal *à travers la transaction en cours* — les lignes qu'elle vient d'insérer comprises — et non marshalé depuis le `*graph.State` vivant. Marshaler l'état vivant laissait une écriture indépendante qui *ressemble* à une projection ; accumuler un état courant en mémoire n'est qu'une seconde vérité à durée de vie plus courte. Coût assumé : O(événements) par niveau."
  - "2026-08-19 : les événements d'un niveau sont tamponnés jusqu'à son hook `OnStep`, parce que la ligne n'est connaissable qu'à ce moment-là (`graph.StepInfo` n'existe que là). Les écrire au fil de l'eau mettrait les événements en table avant la ligne qui les clôt — soit très exactement l'un observable sans l'autre, ce que l'issue interdit. Les événements terminaux du run, émis après le dernier hook, s'écrivent eux-mêmes via `AppendAndReproject`."
  - "2026-08-19 : `Engine.OnEvent` est câblé ici pour la première fois (issue 05 l'avait laissé nil, donc rien n'émettait en production), et l'adaptateur `graph.Event` -> `journal.Event` vit dans `internal/cmd` : c'est le seul endroit qui détient les trois champs que le moteur n'a pas — run id, numéro de séquence, nom du graphe. La règle de combinaison est traduite par un `switch` explicite et non par une conversion de chaîne : les deux constantes partagent leur texte aujourd'hui, une conversion continuerait de compiler le jour où l'une diverge."
  - "2026-08-19 : `newJournalRecorder` lit `NextSeq` avant d'enregistrer quoi que ce soit. Un enregistreur repartant de `FirstSeq` ferait refuser chaque append d'un run repris (`ErrSeqMismatch`) sur un chemin (`resume`) qui fonctionne aujourd'hui. Rejouer correctement cette queue reste l'issue 08 ; ne pas la corrompre était celle-ci."
  - "2026-08-19 : l'événement `nudge_applied` est enregistré ici alors qu'il relève nominalement de l'issue 06 — voir la fiche de dérive `docs/epics/epic-1-event-journal-source-of-truth/drift/07-nudge-event-recorded-early.md`. Dès que la ligne dérive du journal, une mutation que le journal ne porte pas est une mutation que la ligne perd, et deux tests préexistants du daemon l'ont prouvé en échouant."
---

**Quoi** : la ligne `checkpoints` cesse d'être une seconde vérité. `AppendAndProject` écrit
les événements d'un niveau et la ligne qui en dérive dans **une seule transaction** ; un échec
de l'une des deux moitiés annule l'autre et interrompt le run. `AppendAndReproject` fait de
même pour les événements terminaux, qui arrivent après le dernier hook sans nouvelle ligne à
écrire. `checkpointHook` ne prend plus le store ni le run id : les deux appartiennent à
`journalRecorder`, qui porte aussi l'adaptateur `graph.Event` -> `journal.Event`.

**Pourquoi** : septième issue de l'Epic 1 (#11), décision 01 de l'epic. La ligne ne peut pas
dériver puisqu'elle n'est jamais écrite indépendamment. Les huit sites de lecture existants
(les cinq `Latest` de `serve.go`, les deux types de fonction de `daemon/router.go`, `status`)
lisent exactement ce qu'ils lisaient — c'est ce qui garde le rayon d'action de l'epic sur les
chemins d'écriture et de reprise.

**Vérifié en réel** : `go build ./...`, `go vet ./...`, `go test ./...` et
`go test -race -count=1 ./...` verts sur les 14 paquets. Deux mutations confirment que les
tests portent : écrire l'état vivant au lieu de la projection fait échouer
`TestTheHookIgnoresTheLiveStateAndWritesTheProjectionOfTheEvents` sur une clé qu'aucun
événement ne portait ; scinder la transaction en deux fait échouer
`TestAFailedProjectionUpsertLeavesNoEventsForThatLevel` avec les événements du niveau restés
en table. Un run réel bout en bout (`seed` -> `double`) vérifie que la ligne stockée égale
`Project(journal du run)` et que le marqueur `queued` au pas `-1` reste lisible juste après
l'acceptation.
