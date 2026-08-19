---
type: Decision
title: "Subgraph runs: own journal or folded into the parent's"
description: "Does a nested graph write into its parent's journal, or its own linked by parent run id?"
tags: [decision, change]
timestamp: 2026-08-19T09:16:15Z
phase: change
decision: 07
slug: subgraph-journaling
status: decided
verdict: "Own journal per nested run, linked by parent run id and node id"
decided_via: triage
depends_on: ['journal-grain']
change: 1
change_slug: event-journal-source-of-truth
---

# Question
`graph.SubgraphNode` runs a child graph with its own state, seeded from the parent and merged
back. From the parent's checkpoint view the whole sub-run is one atomic step.

The reporting side already faced this exact question and answered it, with the reasoning recorded in
`internal/report/http.go`: a nested run reports as a run of its own pointing back at its parent
node, because "the parent's level counter is a sequence a sink rejects out-of-order writes against,
and two graphs advancing at once would corrupt it", and because pointing back "composes at any
depth, where embedding a child's topology inside its parent's would need a recursive schema".
`nestedRuns` in `internal/cmd/runtime.go` mints a fresh run id per execution, so the same node
running twice is two nested runs.

# Options
- **Own journal, linked by parent run id and node id**, mirroring the existing reporting decision
  exactly. One sequence per run stays monotonic and composition at depth needs no recursive schema.
- **Fold child events into the parent's journal** with a nesting marker. One journal per top-level
  run reads more simply, but it reintroduces the interleaved-sequence problem the reporting side
  already rejected, at the layer where ordering actually has to be correct.

# Recommendation
Own journal with a parent link. The argument the report package recorded applies with more force
here than it did there: on the reporting side a corrupted sequence costs a confused UI, and in the
durable record it costs a run that cannot be replayed. Reusing the same shape also means the
journal and the wire contract describe nesting identically, which keeps the projection in decision
02 straightforward.

# Verdict

Own journal per nested run, linked by parent run id and node id.

Own journal with a parent link. The argument the report package recorded applies with more force
here than it did there: on the reporting side a corrupted sequence costs a confused UI, and in the
durable record it costs a run that cannot be replayed. Reusing the same shape also means the
journal and the wire contract describe nesting identically, which keeps the projection in decision
02 straightforward.
