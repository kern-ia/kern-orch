---
type: Decision
title: "Internal journal vocabulary versus the kern.step-event wire contract"
description: "Does the journal reuse kern.step-event, or define its own vocabulary with kern.step-event as a projection?"
tags: [decision, change]
timestamp: 2026-08-19T09:16:15Z
phase: change
decision: 02
slug: event-vocabulary-vs-wire-contract
status: decided
verdict: "Internal journal vocabulary; kern.step-event/v2 stays unchanged as a projection of it"
decided_via: triage
depends_on: ['persistence-approach']
change: 1
change_slug: event-journal-source-of-truth
---

# Question
An event vocabulary already exists: `kern.step-event/v1` and `/v2`, pinned as fixtures in
`contracts/` and consumed by kern-ui. But it is shaped for reporting, not for truth:

- It is **level-grained** — one event per completed frontier, carrying the whole flattened state.
- It is **lossy by design** — `internal/report/http.go` delivers behind a 64-slot queue that drops
  rather than blocks, on the stated grounds that "a sink that misses one level is corrected by the
  next".
- It **flattens** the state, deliberately dropping zones, the frozen counter and the internal step:
  "marshalling the State itself would ship kern-orch's envelope across the contract, which is
  nobody else's business".
- It carries consumer-facing concerns (topology, requester, dossier) that a durable record has no
  reason to hold per event.

A journal needs the opposite properties: complete, ordered, never dropped, and fine-grained enough
that replay reconstructs the state exactly.

# Options
- **Define an internal journal vocabulary; keep `kern.step-event/v2` unchanged as a projection of
  it.** The reporter becomes a consumer of the journal instead of a parallel observer. The external
  contract and its fixtures do not move, so kern-ui cannot break.
- **Promote `kern.step-event` to the durable record and extend it with what replay needs.** One
  vocabulary instead of two — but every field replay needs (zones, frozen counter, per-node
  attribution) is a field the contract deliberately refused to carry, and adding them exports
  kern-orch's internals to every consumer.

# Recommendation
The first, and it is the load-bearing decision of this change. CONVENTIONS.md states the rule
directly — kern-orch emits contracts and does not know who reads them — and `kern.step-event`'s own
comments record the flattening as a deliberate boundary. Making the durable record a separate,
richer, internal vocabulary preserves that boundary and turns the reporter into just another
projection. The fixture tests in `internal/report/` then become the proof that the external
contract survived the refactor untouched.

# Verdict

Internal journal vocabulary; kern.step-event/v2 stays unchanged as a projection of it.

The first, and it is the load-bearing decision of this change. CONVENTIONS.md states the rule
directly — kern-orch emits contracts and does not know who reads them — and `kern.step-event`'s own
comments record the flattening as a deliberate boundary. Making the durable record a separate,
richer, internal vocabulary preserves that boundary and turns the reporter into just another
projection. The fixture tests in `internal/report/` then become the proof that the external
contract survived the refactor untouched.
