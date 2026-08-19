---
type: Decision
title: "How the 'reconstructable from the journal' invariant is enforced"
description: "Runtime assertion, test-only check, or documented rule?"
tags: [decision, change]
timestamp: 2026-08-19T09:16:15Z
phase: change
decision: 08
slug: invariant-enforcement
status: decided
verdict: "Replay-equivalence check in tests over real engine runs, plus an opt-in runtime check"
decided_via: triage
depends_on: ['resume-replay-vs-snapshot', 'non-node-state-mutations']
change: 1
change_slug: event-journal-source-of-truth
---

# Question
The second half of this change is the invariant: anything a node can read must be reconstructable
from the journal. An invariant nobody executes is a comment, and the failure it guards against is
silent — a state key that appears with no event explaining it does not crash anything, it just makes
every later replay quietly wrong.

The repo has no lint configuration and no CI, so a rule that lives only in prose has nothing
enforcing it. It does have a strong existing habit of proving invariants through tests, including
fixture-pinned contract tests.

# Options
- **A replay-equivalence check in tests, plus an optional runtime check behind configuration.**
  Every engine test asserts that replaying the journal yields exactly the state the run ended with;
  the runtime check compares projection against replay at level boundaries and is off by default.
- **Tests only.** Cheapest and catches every case the test suite covers — and nothing a graph does
  in production that no test anticipated.
- **Runtime assertion always on.** Strongest guarantee, but it doubles the state work on every level
  of every run to defend against a class of bug that is introduced in code, not in data.

# Recommendation
The first. The test-side check is the one that must exist, and making it an assertion over real
engine runs rather than a unit test of the journal package is what gives it teeth. Exposing the same
comparison as an opt-in runtime check costs one configuration field, matches CONVENTIONS.md's rule
that deployment-varying choices are configuration rather than constants, and gives a way to
diagnose a suspect run without rebuilding.

# Verdict

Replay-equivalence check in tests over real engine runs, plus an opt-in runtime check.

The first. The test-side check is the one that must exist, and making it an assertion over real
engine runs rather than a unit test of the journal package is what gives it teeth. Exposing the same
comparison as an opt-in runtime check costs one configuration field, matches CONVENTIONS.md's rule
that deployment-varying choices are configuration rather than constants, and gives a way to
diagnose a suspect run without rebuilding.
