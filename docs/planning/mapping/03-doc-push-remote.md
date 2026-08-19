---
type: Decision
title: "Remote the pipeline pushes to"
description: "The repo has two remotes, origin (kern-ia) and perso (YoLaub) — which receives pipeline branches, PRs and milestones?"
tags: [decision, mapping]
timestamp: 2026-08-19T09:16:15Z
phase: mapping
decision: 03
slug: doc-push-remote
status: decided
verdict: "origin - kern-ia/Kern-Orch"
decided_via: triage
depends_on: []
---

# Question

`git remote -v` shows two:

- `origin` → `git@github.com:kern-ia/Kern-Orch.git` (org, SSH)
- `perso`  → `https://github.com/YoLaub/Kern-Orch.git`

Every lx skill creates GitHub state — milestones, tracking issues, sub-issues, PRs — and
commits and pushes the bundle. The `gh` CLI is authenticated as `YoLaub` with `repo` and
`read:org` scopes. Nothing in the repository states which remote owns the work.

# Options

- **`origin` (kern-ia/Kern-Orch)** — the org repo, consistent with the CONVENTIONS.md
  rollout and where the other `kern-ia` bricks live. Requires the authenticated account to
  hold issue/milestone write access on the org repo.
- **`perso` (YoLaub/Kern-Orch)** — the personal fork, guaranteed writable, but the epic and
  its issues then live away from the rest of the ecosystem.

# Recommendation

`origin`. The whole point of the `kern-*` brick family is that the work is visible in one
place, and the conventions being rolled out are org-level. If `gh` turns out to lack
milestone or issue write access on `kern-ia/Kern-Orch`, that is a permissions fix, not a
reason to relocate the epic.

# Verdict

`origin` - `git@github.com:kern-ia/Kern-Orch.git`.

Branches, milestones, tracking issues, sub-issues and Pull Requests all land on the org
repository, consistent with the rest of the `kern-*` brick family and with the org-level
conventions rollout. A missing issue or milestone write permission for the authenticated account
is a permissions fix, not a reason to relocate the epic.
