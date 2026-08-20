---
type: Issue
title: "Capture real Claude Code and OpenCode transcripts as test fixtures"
description: "Record one real stream-json transcript from Claude Code and one real /message response from OpenCode, committed as fixtures - no translation logic is written against an assumed shape."
tags: [epic-2]
timestamp: 2026-08-20T15:10:00Z
epic: 2
issue: 01
slug: capture-real-cli-transcripts
size: S
status: done
gh_issue: 35
gh_pr: 42
resource: https://github.com/kern-ia/kern-orch/issues/35
depends_on: []
---

# Capture real Claude Code and OpenCode transcripts as test fixtures

## Summary

The epic's Notes name the real risk directly: neither CLI's exact message schema was captured
from a real transcript during scoping. `internal/agentrunner`'s current placeholder protocol
was invented the same way -- never reconciled against a real CLI -- and this issue exists so the
mistake is not repeated one layer down. Every later issue that writes translation logic
(04, 05) reads a fixture this issue produces; none of them invents a shape from documentation
alone.

## Scope

- Run `claude -p --input-format=stream-json --output-format=stream-json` with a real prompt
  against a real API key, capture the raw JSON-lines stdout verbatim, save as
  `internal/agentrunner/testdata/claude-code-stream.jsonl`. Include at minimum: an assistant
  text response, and (if reachable in one capture) a tool-call round trip -- the two shapes a
  translator must distinguish.
- Run `opencode serve`, issue a real `POST /session/:id/message` call, capture the raw JSON
  response body verbatim, save as `internal/agentrunner/testdata/opencode-message.json`.
- A short `testdata/README.md` recording exactly how each fixture was captured (command, CLI
  version, date) so a later re-capture (a CLI version bump changing its wire shape) has a
  reproducible procedure rather than tribal knowledge.
- Redact only what must be redacted (API keys, any real file paths from the capturing
  machine) -- the message *content* and *structure* must stay real, since a sanitized-to-the-
  point-of-fictional fixture defeats the point.

## Out of scope

- Writing any translation logic against these fixtures -- issues 04 and 05.
- OpenCode's async completion path (`prompt_async`) -- explicitly out of scope for the whole
  epic (Epic 2's Out of scope, decision 06).
- Automating the capture (a script that re-captures on demand) -- a manual, documented
  procedure is sufficient for v1; automate only if a real CLI version bump forces a re-capture
  during this epic.

## Acceptance criteria

- [ ] `internal/agentrunner/testdata/claude-code-stream.jsonl` exists, contains real captured
      output (not hand-written), and is valid JSON-lines.
- [ ] `internal/agentrunner/testdata/opencode-message.json` exists, contains a real captured
      response body (not hand-written), and is valid JSON.
- [ ] `testdata/README.md` names the exact command and CLI version used for each capture.
- [ ] No credential or real local path survives redaction -- reviewed explicitly before commit.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` are green.
- [ ] An OKF fiche is added under `docs/index/` for this branch, per CONVENTIONS.md.

## Relevant files / areas

- New directory `internal/agentrunner/testdata/`.
- `internal/agentrunner/protocol.go` -- the placeholder this epic replaces, for reference on
  what shape assumptions to specifically avoid repeating.

## Dependencies

None. Blocks issues 04 and 05.

## PR size note

Target ~500 changed lines; if this grows past ~1000, split it before opening the PR.
