# Claude Standing Instructions

> Standing instructions for Claude Code on this project.

## Required Tooling

A `SessionStart` hook in `.claude/settings.json` runs
`.claude/hooks/check-tooling.sh`, which reports any required plugin skill
or binary that is missing from the developer's environment. If the session
context contains a line starting `TOOLING MISSING:`, stop and tell the user
before doing any other work. As a fallback, before delegating documentation
or browser work, confirm that `pgedge-skills:pgedge-docs` and
`playwright-cli` appear in the available skills list; if either is absent,
stop and report it. Installing tooling is the developer environment's
concern and is managed centrally; do not document or attempt installs here.

## Primary Agent Role

The primary agent acts as a coordinator. Implementation flows through the
sub-agents listed below; the primary agent understands requirements, breaks
them into tasks, delegates, coordinates across domains, synthesises results
and runs the final verification. It edits files directly only within the
carve-out below.

### Trivial-Edit Carve-Out

The primary agent MAY edit directly only when ALL of the following hold:

- The change touches a single file and is at most 20 lines (added plus
  removed).

- Identifying the change took at most two tool calls.

- The change does not touch security-sensitive code: anything under
  `server/src/internal/auth/`, code that hashes passwords, manipulates
  tokens, performs RBAC checks, parses untrusted input or constructs SQL.

- The change needs no knowledge-base update and does not add or modify a
  public API or exported identifier.

Typical cases: a typo or comment fix, a single-line bug fix at a known call
site, whitespace cleanup, adding or removing one import. If any condition
fails, delegate; when in doubt, delegate.

Named exception: bumping the project version for a release is a
primary-agent task even though it touches several files (version strings in
`package.json`, Go constants, mkdocs config) plus the `docs/changelog.md`
entry that records the bump itself. Deeper changelog content still flows
through **documentation-writer**.

## Project Structure

The pgEdge AI DBA Workbench consists of five sub-projects:

- `/collector` - Data collector (Go).

- `/server` - MCP server (Go).

- `/alerter` - Alert monitoring service (Go).

- `/client` - Web client application (React/TypeScript).

- `/e2e` - Playwright end-to-end smoke tests, run with `make test-e2e`;
  they need Docker and are deliberately not part of `make test-all`.

Go and TypeScript tests sit beside the source they cover. Documentation
lives under `docs/` and is organised by audience (`getting-started/`,
`user-guide/`, `admin-guide/`, `developer-guide/`), with per-sub-project
pages nested one level below; the navigation is defined in `mkdocs.yml`.

Key files: `docs/changelog.md` (notable changes by release), `mkdocs.yml`
(documentation navigation) and `Makefile` (build and test commands).

## Sub-Agents

Sub-agents in `/.claude/agents/` do the implementation work. Anything
outside the carve-out is delegated using this mapping:

| Task Type                         | Sub-Agent                    |
|-----------------------------------|------------------------------|
| Go code, tests, review, MCP tools | **golang-expert**            |
| React/TypeScript code, tests      | **react-expert**             |
| Documentation changes             | **documentation-writer**     |
| PostgreSQL and Spock questions    | **postgres-expert**          |
| Security review                   | **security-auditor**         |
| Exploration and research          | **Explore** (built-in agent) |

**golang-expert**, **react-expert** and **documentation-writer** write code
and documentation directly. **postgres-expert** and **security-auditor**
are advisory only: they cannot edit files and return self-contained
reports for the primary agent to act on.

Each implementation agent has a knowledge base in `/.claude/<agent-name>/`
holding repo-specific patterns. When a task changes code that a knowledge
base describes, the sub-agent must update the affected file in the same
change, and the primary agent should confirm the entries remain accurate.
A stale entry is worse than none; delete or correct anything that no longer
matches the code.

Sub-agents run in the background and cannot ask the user questions. When a
requirement is ambiguous, they state their assumptions and report them.

## Plans and Specs

Store plans in `.claude/plans/` and design specs in `.claude/specs/`, with
descriptive filenames. Both directories are git-ignored and must never be
committed.

## Development Worktrees

All development work happens in a dedicated git worktree so that the main
checkout stays clean and concurrent tasks cannot contaminate one another.
The default applies to every task unless the developer explicitly asks for
work in the main checkout; confirm any such override before proceeding.

Dispatch implementation sub-agents with worktree isolation where the Agent
tool supports it (`isolation: "worktree"`). Advisory and research agents do
not need isolation. Remove the task's worktree with `git worktree remove`
once the work is merged or abandoned; stale worktrees accumulate quickly.

## Pull Request Workflow

Before pushing a PR branch, check whether the branch is behind `main` and
rebase if it is. This applies both when opening a pull request and when
pushing further commits to one, so that history stays linear and CI runs
against the current `main`.

- Fetch the latest `main` with `git fetch origin main`.

- Compare with `git rev-list --left-right --count origin/main...HEAD`.

- If behind, run `git rebase origin/main` and resolve any conflicts rather
  than aborting or overwriting; ask the developer only when a conflict
  cannot be resolved cleanly.

- Push with `git push --force-with-lease`; never use a plain
  `git push --force`, and never force-push `main` itself.

## Task Workflow

1. **Understand** the requirements, clarifying with the user if needed.

2. **Plan** the sub-tasks and the sub-agents each needs.

3. **Delegate** each sub-task; coordinate multiple sub-agents in sequence
   or in parallel as appropriate. Each implementation sub-agent runs
   `make test-all` in the sub-project it touched before handing back.

4. **Verify** by running `make test-all` from the repository root once all
   sub-agents have finished. Reject any code delivery that does not meet
   the coverage floor in the Tests section.

5. **Review** security-sensitive changes (auth, input handling, queries)
   with **security-auditor**.

6. **Document** user-facing changes through **documentation-writer**,
   including `docs/changelog.md`.

7. **Report** a synthesis of the results to the user.

## Documentation

Use the `pgedge-skills:pgedge-docs` skill for all documentation work; it
holds the pgEdge style guide, templates and MkDocs conventions. The
repo-specific points below take precedence where they differ:

- Each README's developer link points at
  `docs/developer-guide/contributing.md`, and each README's table of
  contents mirrors the `mkdocs.yml` nav section.

- Record notable changes since the last release in `docs/changelog.md`.

- The 79-character wrap applies to prose only; long URLs and `mkdocs.yml`
  nav paths may exceed it.

- Keep `LICENSE.md` in both `/docs` and the repository root.

- Keep documentation synchronised with code for CLI options, configuration
  and environment variables, and match sample output to actual output.

## Tests

- Provide unit and integration tests for each sub-project, using mocking
  where needed, and do not skip database tests.

- All new and modified code must reach at least 90% line coverage. This is
  a hard floor, not a target, and it applies however small the change is:
  when you touch a file or package below 90%, raise the touched unit to
  90% in the same change.

- Measure coverage per sub-project with `cd <subproject> && make coverage`.
  Each coverage target fails when the sub-project total drops below a
  ratchet pinned at the current total (`COVERAGE_MIN` in each Go Makefile,
  `coverage.thresholds` in `client/vitest.config.js`); raise the ratchet
  whenever a change lifts the total, and never lower it. Inspect Go results
  with `go tool cover -func=coverage.out`; the client's Vitest text
  reporter prints per-file coverage.

- Run `gofmt` on Go files and the relevant linters after any code change;
  a task is not finished until formatting and linting pass cleanly.

- Run `make test-all` from the repository root before completing a task,
  checking for errors or warnings hidden by output truncation. A task is
  complete only when tests, linting and the coverage floor all pass.

- Modify existing tests only when the tested behaviour changes or to fix a
  bug; clean up temporary test files and retain log files for debugging.

## API Specification

- Keep the OpenAPI specification in `server/src/internal/api/openapi.go`
  synchronised with the server code at all times.

- When adding, modifying or removing an HTTP endpoint, update the
  corresponding path entry and schemas in `BuildOpenAPISpec`, and update
  the endpoint summary table in `docs/admin-guide/api/reference.md`.

- Regenerate the static file with `cd server && make openapi` and run
  `cd server/src && go test ./internal/api/ -run OpenAPI -v`.

- The interactive API browser renders from
  `docs/admin-guide/api/openapi.json`; verify new endpoints appear
  correctly in the ReDoc output.

## Security

- Maintain isolation between user sessions, and restrict database
  connections to their owning users or tokens.

- Protect against injection attacks at client and server; the exception is
  MCP tools that execute arbitrary SQL by design.

- Follow defensive secure-coding practice and review every change for
  security implications, reporting potential issues.

## Browser Automation

Use the `playwright-cli` skill (`.claude/skills/playwright-cli/SKILL.md`)
for browser testing, screenshots and web interaction, via
`Bash(playwright-cli:*)`. Never use the Playwright MCP server,
`npx playwright` or `npm install playwright`. After completing UI changes,
validate them visually in a browser session; this catches rendering and
navigation issues that unit tests miss.

## Code Style

- Use four spaces for indentation.

- Write readable, modular code; minimise duplication and remove unused
  code.

- Use `COMMENT ON` to describe objects in database migrations.

- Include this copyright notice at the top of every source file (not
  configuration files), adjusting the comment style for the language:

  ```
  /*-------------------------------------------------------------------------
   *
   * pgEdge AI DBA Workbench
   *
   * Copyright (c) 2025 - 2026, pgEdge, Inc.
   * This software is released under The PostgreSQL License
   *
   *-------------------------------------------------------------------------
   */
  ```
