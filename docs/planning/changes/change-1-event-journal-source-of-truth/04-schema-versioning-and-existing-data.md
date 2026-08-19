---
type: Decision
title: "Schema versioning and what happens to existing checkpoint databases"
description: "There is no migration mechanism. Do existing checkpoints get backfilled into events, or refused?"
tags: [decision, change]
timestamp: 2026-08-19T09:16:15Z
phase: change
decision: 04
slug: schema-versioning-and-existing-data
status: decided
verdict: "Monotonic schema_version with a refusal naming the path; no backfill"
decided_via: triage
depends_on: ['persistence-approach']
change: 1
change_slug: event-journal-source-of-truth
---

# Question
`internal/checkpoint/sqlite.go` creates its table with `CREATE TABLE IF NOT EXISTS` and carries a
single ad-hoc `ALTER TABLE checkpoints ADD COLUMN dossier` whose error is deliberately ignored,
with the comment: "no migration mechanism exists yet in this project's dev stage, so this one ALTER
TABLE covers the gap." That gap is now load-bearing — this change adds tables and redefines the
meaning of an existing one.

Existing data is local development runs under `./data/kern-orch.db` (the default). No production
deployment exists: SPECS.md records no Dockerfile, no IaC, no deployment configuration of any kind.

# Options
- **Introduce a real `schema_version` and refuse a database the build does not understand**,
  naming the file path in the error. No backfill: an old `checkpoints` row has no per-node facts to
  recover, so any synthesized journal would be fiction presented as record.
- **Backfill old snapshots into one synthetic event per level.** Preserves run history, but invents
  a per-node record that never existed, which is precisely the kind of plausible fabrication the
  journal is meant to eliminate.
- **Keep relying on `CREATE TABLE IF NOT EXISTS` and additive columns.** Free today, and it defers a
  mechanism the next schema change will need anyway.

# Recommendation
The first. CONVENTIONS.md's rule is that misconfiguration fails loud, and silently reinterpreting
a database written under different semantics is the opposite. A monotonic `schema_version` plus a
refusal that names the path is a small, self-contained piece of work this change needs regardless,
and it is the thing every later schema change will build on. Backfilling is rejected on principle:
a record that cannot be reconstructed honestly should be absent, not guessed.

# Verdict

Monotonic schema_version with a refusal naming the path; no backfill.

The first. CONVENTIONS.md's rule is that misconfiguration fails loud, and silently reinterpreting
a database written under different semantics is the opposite. A monotonic `schema_version` plus a
refusal that names the path is a small, self-contained piece of work this change needs regardless,
and it is the thing every later schema change will build on. Backfilling is rejected on principle:
a record that cannot be reconstructed honestly should be absent, not guessed.
