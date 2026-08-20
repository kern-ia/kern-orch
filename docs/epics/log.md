# Log

## 2026-08-19

* **Creation**: bundle established; Epic 1 written from change ledger `change-1-event-journal-source-of-truth`.
* **Update**: Epic 1 opened — milestone 1 and tracking issue #4 created.
* **Update**: Epic 1 broken into 11 issues (#5-#15), all created on milestone 1 and linked as native sub-issues of #4.
* **Update**: Issue 01 (#5, SQLite schema versioning) implemented on `feature/sqlite-schema-versioning`; PR #17 open against `dev`.
* **Update**: Issue 01 (#5, PR #17) merged into `dev`; issue closed.
* **Update**: Issue 02 (#6, journal event vocabulary) implemented on `feature/journal-event-vocabulary`; PR #18 open against `dev`.
* **Update**: Issue 02 (#6, PR #18) merged into `dev`; issue closed.
* **Update**: Issue 03 (#7, journal store append/read) started on `feature/journal-store`.
* **Update**: Issue 03 (#7, journal store append/read) implemented on `feature/journal-store`; PR #19 open against `dev`.
* **Update**: Issue 03 (#7, PR #19) merged into `dev`; issue closed.
* **Update**: Issue 04 (#8, state projection from events) implemented on `feature/state-projection`; PR #20 open against `dev`.
* **Update**: Issue 04 (#8, PR #20) merged into `dev`; issue closed.
* **Update**: Issue 05 (#9, engine event emission) started on `feature/engine-event-emission`.
* **Update**: Issue 05 (#9, engine event emission) implemented on `feature/engine-event-emission`; PR #21 open against `dev`.
* **Update**: Issue 05 (#9, PR #21) merged into `dev`; issue closed.
* **Update**: Issue 07 (#11, atomic journal and projection) started on `feature/atomic-journal-projection`.
* **Update**: Issue 07 (#11, atomic journal and projection) implemented on `feature/atomic-journal-projection`; PR #22 open against `dev`.
* **Update**: Issue 07 (#11, PR #22) merged into `dev`; issue closed.
* **Update**: Issues 08 and 10 narrowed and split — issue 12 (#23, interrupted-tail closing) and issue 13 (#24, opt-in runtime equivalence check) created. Six of six issues so far overshot their size band because the no-table-driven-tests convention makes tests two to three times the implementation; the remaining work is cut finer to compensate.

* **Update**: Issue 08 (#12, replay-based resume) implemented on `feature/replay-based-resume`; PR #27 open against `dev`.
* **Update**: Issue 08 (#12, PR #27) merged into `dev`; issue closed.
* **Update**: Issue 12 (#23, interrupted tail closing) started on `feature/interrupted-tail-closing`.
* **Update**: Issue 12 (#23, interrupted tail closing) implemented on `feature/interrupted-tail-closing`; PR #28 open against `dev`. Both interruption shapes are closed — the hard kill mid-level and the stop path issue 08 found, where the terminal events are refused on the run's own cancelled context.
* **Update**: Issue 12 (#23, PR #28) merged into `dev`; issue closed.
* **Update**: Issue 09 (#13, nested subgraph journals) started on `feature/nested-subgraph-journals`.
* **Update**: Issue 09 (#13, nested subgraph journals) implemented on `feature/nested-subgraph-journals`; PR #29 open against `dev`.
* **Update**: Issue 06 (#10, non-node mutations as events) implemented on `feature/non-node-mutations-as-events`; PR #26 merged into `dev`; issue closed.
* **Update**: Issue 09 (#13, PR #29) merged into `dev`; issue closed.
* **Update**: Issue 10 (#14, replay equivalence over real engine runs) started on `feature/replay-equivalence`.
* **Update**: Issue 10 (#14, replay equivalence over real engine runs) implemented on `feature/replay-equivalence`; PR #30 open against `dev`. Test-only: the eight cases are asserted through the engine and the recorder, and each is proven non-vacuous by a named mutation.
* **Update**: Issue 10 (#14, PR #30) merged into `dev`; issue closed.
* **Update**: Issue 11 (#15, rewire the reporter to project the journal) started on `feature/reporter-consumes-journal`.
* **Update**: Issue 11 (#15, rewire the reporter to project the journal) implemented on `feature/reporter-consumes-journal`; PR #31 open against `dev`. `internal/report` has zero diff; the rewiring lives entirely in `internal/cmd` (`reportHook`, `journalRecorder.flush` for nested runs).
* **Update**: Issue 11 (#15, PR #31) merged into `dev`; issue closed. (Reconciled during issue 13's run — status had not been updated after merge.)
* **Update**: Issue 13 (#24, opt-in runtime replay-equivalence check) started on `feature/runtime-equivalence-check`.
* **Update**: Issue 13 (#24, opt-in runtime replay-equivalence check) implemented on `feature/runtime-equivalence-check`; PR #32 open against `dev`. Wires issue 10's own comparison (full-state JSON encoding) into a `Config.RuntimeEquivalenceCheck` field, off by default; enabled it names the divergent keys, disabled it adds zero projection calls (proven by call-count instrumentation, not timing). `FromEnv()` now returns `(Config, error)` so an invalid value fails loud at load. This is the epic's last issue — Epic 1 has no more open issues once this merges.
* **Update**: close-epic reconciled bookkeeping drift — issues 10, 12 and 13 were done on GitHub but still recorded pr-open in their frontmatter (issue 13 also in the index bullet); all corrected to done.
* **Update**: Epic 2 opened — milestone 2 and tracking issue #33 created.
* **Update**: Epic 2 broken into 6 issues (#35-#40), created on milestone 2 and linked as native sub-issues of #33. The wrapper-script documentation item was filed separately as kern-exec#1, outside this epic’s implement-epic scope (different repository).
* **Update**: reconciled issue 01 (#35, PR #42) to done — merged and closed on GitHub since 2026-08-20, still recorded pr-open in the bundle.
* **Update**: Issue 02 (#36, adapter registry and KERN_AGENT_KIND selection) started on `feature/adapter-registry-and-kind-selection`.
* **Update**: Issue 02 (#36, adapter registry and KERN_AGENT_KIND selection) implemented on `feature/adapter-registry-and-kind-selection`; PR #44 open against `dev`. No kind selects the old `Subprocess` JSON-lines placeholder any more, so `TestRunBracketsAgentActivity` was repurposed to assert the new loud refusal; the activity chain's end-to-end coverage returns with the first real adapter (issue 04).
