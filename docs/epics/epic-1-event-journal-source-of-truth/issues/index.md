# Issues — Epic 1: Event journal as the source of truth

* [Add a monotonic SQLite schema version and refuse unknown versions](./01-sqlite-schema-versioning.md) - S, in-progress (#5)
* [Define the internal journal event vocabulary](./02-journal-event-vocabulary.md) - S, open (#6)
* [Persist and read the journal in SQLite](./03-journal-store-append-read.md) - M, open (#7)
* [Project a run state by replaying its journal](./04-state-projection-from-events.md) - M, open (#8)
* [Emit per-node and level-boundary events from the engine](./05-engine-event-emission.md) - M, open (#9)
* [Record nudge, freeze and the combination rule as events](./06-non-node-mutations-as-events.md) - M, open (#10)
* [Write the journal and the projection cache in one transaction](./07-atomic-journal-and-projection.md) - M, open (#11)
* [Resume by replaying the journal, closing an interrupted tail](./08-replay-based-resume.md) - M, open (#12)
* [Give each nested subgraph run its own journal](./09-nested-subgraph-journals.md) - S, open (#13)
* [Enforce replay equivalence as an executed invariant](./10-replay-equivalence-invariant.md) - M, open (#14)
* [Rewire the reporter to project the journal](./11-reporter-consumes-journal.md) - M, open (#15)
