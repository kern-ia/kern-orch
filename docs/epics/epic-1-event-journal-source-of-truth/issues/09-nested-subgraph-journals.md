---
type: Issue
title: "Give each nested subgraph run its own journal"
description: "A subgraph writes its own journal linked by parent run id and node id, mirroring the reporting decision already made."
tags: [epic-1]
timestamp: 2026-08-19T21:30:00Z
epic: 1
issue: 09
slug: nested-subgraph-journals
size: S
status: done
gh_issue: 13
gh_pr: 29
resource: https://github.com/kern-ia/kern-orch/issues/13
depends_on: [5, 7]
---

# Give each nested subgraph run its own journal

## Summary

Decision 07 reuses an answer this codebase already reached. `internal/report/http.go` records why a
nested run reports as a run of its own rather than folding into its parent's stream: "the parent's
level counter is a sequence a sink rejects out-of-order writes against, and two graphs advancing at
once would corrupt it", and pointing back "composes at any depth, where embedding a child's
topology inside its parent's would need a recursive schema".

That argument applies with more force to the durable record: on the reporting side a corrupted
sequence costs a confused UI; here it costs a run that cannot be replayed.

## Scope

- A `SubgraphNode`'s child run gets its own run id and its own journal.
- Child journal events carry the parent run id and the parent node id.
- A fresh run id per execution, so the same node running twice — a retry, a loop — is two nested
  runs rather than one run journalled twice. `nestedRuns` in `internal/cmd/runtime.go` already
  mints ids this way for reporting; match it.
- The parent's journal continues to see the subgraph as one atomic step, as its checkpoint view
  already does.

## Out of scope

- Changing the parent-side checkpoint granularity for subgraphs.
- Any change to how nested runs report on the wire — that shape already exists and stays.

## Acceptance criteria

- [ ] Running `examples/parent.yaml` produces two journals: the parent's, and the child's carrying
      the parent run id and the node id.
- [ ] Each journal's sequence is independently monotonic.
- [ ] A subgraph node executed twice in one run produces two child journals with distinct run ids.
- [ ] Replaying the parent's journal yields the parent's final state, including what the subgraph
      merged back.
- [ ] Replaying a child's journal yields the child's final state.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- `internal/graph/subgraph.go` — `SubgraphNode`, `WithInput`, `WithOutput`.
- `internal/cmd/runtime.go` — `nestedRuns`, `newRunID`, `reg.OnChildStep`.
- `internal/report/http.go` — the `Parent` type and the comment recording this decision.
- `examples/parent.yaml`, `examples/child.yaml`.

## Dependencies

Blocked by 05 and 07. Blocks 10.


## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
