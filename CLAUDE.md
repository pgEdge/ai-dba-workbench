# Claude Standing Instructions

> Standing instructions for Claude Code on this project.

## Required Tooling

A `SessionStart` hook runs `.claude/hooks/check-tooling.sh`. If the session
context contains a line starting `TOOLING MISSING:`, stop and tell the user
before doing any other work. Installing tooling is managed centrally; do not
document or attempt installs here.

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
base describes, the sub-agent updates the affected file in the same change,
and the primary agent confirms the entries remain accurate.

Sub-agents run in the background and cannot ask the user questions. When a
requirement is ambiguous, they state their assumptions and report them.

## Plans and Specs

Plans go in `.claude/plans/` and design specs in `.claude/specs/`; both are
git-ignored and must never be committed.

## Development Worktrees

Every task that modifies the repository runs in its own git worktree, unless
the developer explicitly asks for the main checkout; confirm any such
override before proceeding.

Dispatch implementation sub-agents with `isolation: "worktree"`; advisory
and research agents do not need it. Remove the task's worktree with
`git worktree remove` once the work is merged or abandoned.

## Pull Request Workflow

Before pushing a PR branch, whether opening a pull request or adding commits
to one, rebase onto `origin/main` if the branch is behind, so that history
stays linear and CI runs against the current `main`. Resolve conflicts
rather than aborting or overwriting, asking the developer only when one
cannot be resolved cleanly. Push with `git push --force-with-lease`, never a
plain `git push --force`, and never force-push `main` itself.

## Task Workflow

Understand the requirement, break it into sub-tasks and delegate each to the
sub-agent above, in parallel where they are independent, then synthesise and
report. Each implementation sub-agent runs `make test-all` in the
sub-project it touched before handing back; the primary agent runs
`make test-all` from the repository root once they have all finished, and
rejects any delivery below the coverage floor in the Tests section.
Security-sensitive changes (auth, input handling, queries) go to
**security-auditor**, and user-facing changes to **documentation-writer**,
including `docs/changelog.md`.

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
validate them visually in a browser session.

## Code Style

- Use four spaces for indentation.

- Write readable, modular code; minimise duplication and remove unused
  code.

- Use `COMMENT ON` to describe objects in database migrations.

- Start every source file (not configuration files) with the copyright
  notice given under "Copyright Header" in
  `docs/developer-guide/contributing.md`, adjusting the comment style for
  the language.
