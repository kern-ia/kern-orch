# CONVENTIONS.md — kern-orch

Local authority for this repo, as announced by the org-wide
[CONTRIBUTING.md](https://github.com/kern-ia/.github/blob/main/CONTRIBUTING.md). The rules
shared by all `kern-ia` repos are restated below; the "Specifics" sections cover what belongs
only to `kern-orch`.

## Language

Code, identifiers, and comments are written in English — no exceptions. This applies to
source files, docstrings, commit diffs, and test names. Internal documentation such as this
file, `README.md`, or `CLAUDE.md` stays in whatever language the team works in day to day.

## Branches

- `main`: stable branch, always deployable. Protected — no direct pushes.
- `dev`: integration branch. Protected — no direct pushes.
- Working branches: `feature/<slug>`, `fix/<slug>`, `chore/<slug>`, `docs/<slug>`, `test/<slug>`.
- Any change to `main` or `dev` goes through a Pull Request, never a direct push or a local
  `git merge` followed by a push.
- Merging into `dev`: `--no-ff` merge commit.

> **Current gap to fix**: the GitHub repo's default branch is `dev` today, not `main`. Change
> it in Settings → Branches once `main` is genuinely current and protected (see the
> compliance report).

## Commits

Conventional Commits: `type(scope): short summary`. Types already used here: `feat`, `fix`,
`docs`, `test`, `chore`, `merge`. The body explains the *why*. No tool signature
(`Co-Authored-By`, `Claude-Session`, or equivalent trailer) in commit messages — the git
author is enough.

## Pull Requests

- One subject per PR, linked to the issue or RFC it resolves.
- PR template inherited from `kern-ia/.github`.
- States the semver impact.
- No real personal data.

> **Current gap**: only one GitHub PR exists on this repo (#1). The real flow is a local
> `git merge` pushed straight to `dev` — so no review ever happens on GitHub. To fix: every
> change now opens a PR, even solo, so CI (once in place) and a review history actually exist.

## Style and lint

- `go vet ./...` is mandatory.
- No `.golangci.yml` yet — add one, based on `linters.default: standard` (see `kern-anon` or
  `kern-link` as a reference).

## Tests

- `go test ./...` must be green before any PR.
- Targeted unit test: `go test ./internal/cmd/ -run <TestName> -v`.

> **Current gap — the most important one**: this repo has **no GitHub Actions workflow at
> all**. Nothing checks build/vet/test/lint at PR time. Priority: add a
> `.github/workflows/ci.yml` modeled on `kern-anon`'s (`go build`, `go test -race -cover ./...`,
> `golangci-lint`).

## Go module

- Current path: `github.com/yoann/kern-orch` — same gap as `kern-ui`, a decision to make at
  the org level rather than repo by repo.

## Architecture

- One-way dependencies: `graph` defines the ports (`AgentRunner`, `StepFunc`); `agentrunner`
  and `checkpoint` depend on `graph`, never the reverse. Any PR that reverses this direction
  must justify it explicitly in its description.
- `agentrunner`'s JSON-lines protocol is an accepted placeholder (spec §6.4) — do not harden
  it into a stable API without revisiting the spec.

## Documentation

- `README.md` at the root.
- `CLAUDE.md` — agent context; keep the "Commands" section synchronized with the actual
  Makefile / CLI commands.
- Feature index under `docs/index/` (OKF pattern), to consult before rereading all the code.
- No `CHANGELOG.md`: release notes live in the annotated tag (org convention).

## Security / privacy

See the org-inherited `SECURITY.md`. No real PII in code, fixtures, or logs — particularly
sensitive here since `kern-orch` orchestrates runs that can carry user content.
