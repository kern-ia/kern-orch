---
type: Decision
title: "PR flow: prescribed rule or observed practice"
description: "CONVENTIONS.md mandates a PR per change; git history shows local --no-ff merges pushed straight to dev."
tags: [decision, mapping]
timestamp: 2026-08-19T09:16:15Z
phase: mapping
decision: 02
slug: pr-flow-vs-observed-practice
status: decided
verdict: "Record the prescribed rule: one Pull Request per change"
decided_via: triage
depends_on: []
---

# Question

The mapped conventions must be descriptive, not aspirational. Here the two sources disagree:

- **Prescribed** — `CONVENTIONS.md`: every change to `main` or `dev` goes through a Pull
  Request, never a direct push or a local `git merge` followed by a push.
- **Observed** — `git log` on `dev` shows exclusively local `--no-ff` merge commits
  (`Merge branch 'feature/...' into dev`), and the file itself records the gap: "only one
  GitHub PR exists on this repo (#1). The real flow is a local `git merge` pushed straight
  to `dev` — so no review ever happens on GitHub."

This is not cosmetic: `implement-issue` opens a real PR per issue and waits on CI. If the
recorded standard is the local merge, the pipeline will fight the conventions on every
issue.

# Options

- **Record the prescribed rule (PR per change) as the standard**, and note the historical
  local-merge practice as the gap the repository is closing. The lx pipeline then matches
  the conventions rather than contradicting them.
- **Record the observed practice (local `--no-ff` merge into `dev`)** and configure the
  pipeline not to open PRs. Honest to the history, but abandons the review trail the
  conventions were written to establish.

# Recommendation

Record the prescribed rule. The conventions file does not merely aspire — it names the
current practice a gap and commits to closing it, which makes the PR flow a decided
standard rather than a wish. Adopting the lx pipeline is precisely the occasion to close it.
Note explicitly that no CI workflow exists yet, so "checks pass before merge" currently
means locally-run `go build`, `go vet`, `go test`.

# Verdict

Record the prescribed rule: one Pull Request per change.

`CONVENTIONS.md` does not merely aspire to the PR flow - it names the local-merge practice a gap
and commits to closing it, which makes the PR flow a decided standard. Adopting the lx pipeline
is the occasion to close it. CONVENTIONS.md records explicitly that no CI workflow exists yet, so
"checks pass before merge" currently means locally-run `go build ./...`, `go vet ./...` and
`go test ./...`.
