# Change 1 — Event journal as source of truth

* [Approach: how the journal replaces the snapshot](01-persistence-approach.md) - decided
* [Internal journal vocabulary versus the kern.step-event wire contract](02-event-vocabulary-vs-wire-contract.md) - decided
* [Journal grain: per level or per node](03-journal-grain.md) - decided
* [Schema versioning and what happens to existing checkpoint databases](04-schema-versioning-and-existing-data.md) - decided
* [Resume: replay the journal or trust the snapshot](05-resume-replay-vs-snapshot.md) - decided
* [Non-node state mutations: nudge, freeze, and the combination rule](06-non-node-state-mutations.md) - decided
* [Subgraph runs: own journal or folded into the parent's](07-subgraph-journaling.md) - decided
* [How the 'reconstructable from the journal' invariant is enforced](08-invariant-enforcement.md) - decided
* [Scope boundary: what this change explicitly does not do](09-scope-boundary.md) - decided
* [Acceptance criteria](10-acceptance-criteria.md) - decided
