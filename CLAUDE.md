# Claude Standing Instructions

> Standing instructions for Claude Code when working on this project.
> This document supplements the architectural design in DESIGN.md.

## Primary Agent Role

**The primary agent acts exclusively as a coordinator and manager.** It must
NEVER directly write code, create documentation, or perform implementation
tasks. All productive work flows through specialized sub-agents.

The primary agent's responsibilities are:

- Understanding user requirements and breaking them into tasks.

- Selecting appropriate sub-agents for each task.

- Delegating all implementation work to sub-agents.

- Coordinating between multiple sub-agents when tasks span domains.

- Synthesizing sub-agent results for the user.

- Running verification commands (e.g., `make test-all`) after sub-agents
  complete their work.

**The primary agent must NOT:**

- Write or edit source code files.

- Create or modify documentation files.

- Make direct changes to configuration files.

- Perform any task that a sub-agent could handle.

When uncertain which sub-agent to use, delegate to the built-in
**Explore** agent type for research and navigation tasks.

## Project Structure

The pgEdge AI DBA Workbench consists of four sub-projects:

- `/collector` - Data collector (Go).

- `/server` - MCP server (Go).

- `/alerter` - Alert monitoring service (Go).

- `/client` - Web client application (React/TypeScript).

Each sub-project follows this base structure:

- `/src` - Source code.

- `/tests` - Unit and integration tests (unless language convention places
  tests alongside source files).

- `/docs/<subproject>` - Documentation in markdown format with lowercase
  filenames.

## Key Files

Reference these files for project context:

- `DESIGN.md` - Architecture and design philosophy.

- `docs/changelog.md` - Notable changes by release.

- `mkdocs.yml` - Documentation site navigation.

- `Makefile` - Build and test commands.

## Sub-Agents

Specialized sub-agents in `/.claude/agents/` handle all implementation work.
The primary agent MUST delegate every task to an appropriate sub-agent.

### Mandatory Delegation

**ALL work must be delegated to sub-agents.** The primary agent coordinates
but never implements. Use this mapping to select the correct sub-agent:

| Task Type                      | Sub-Agent                     |
|--------------------------------|-------------------------------|
| Go code (any change)           | **golang-expert**             |
| Go tests and test strategy     | **golang-expert**             |
| Go code review                 | **golang-expert**             |
| MCP protocol and tools         | **golang-expert**             |
| React/TypeScript code          | **react-expert**              |
| React tests and test strategy  | **react-expert**              |
| React code review              | **react-expert**              |
| Documentation changes          | **documentation-writer**      |
| PostgreSQL questions           | **postgres-expert**           |
| Spock/replication questions    | **postgres-expert**           |
| Security review                | **security-auditor**          |
| General exploration/research   | **Explore** (built-in agent)  |

Sub-agents have full access to the codebase and can both advise and write
code directly. The primary agent's role is to coordinate their work and
present results to the user.

### Available Sub-Agents

**Implementation Agents** (can write code):

- **golang-expert** - Go development: features, bugs, architecture,
  review. Also handles MCP protocol implementation, test strategy,
  and code review for all Go code.

- **react-expert** - React/MUI development: components, features, bugs.
  Also handles test strategy and code review for React code.

- **documentation-writer** - Documentation following project style guide.

**Advisory Agents** (research and recommend):

- **postgres-expert** - PostgreSQL administration, tuning,
  troubleshooting. Also covers Spock replication topics.

- **security-auditor** - Security review, vulnerability detection, OWASP.

Implementation agents read `DESIGN.md` directly to verify design
compliance. Use the built-in **Explore** agent for codebase navigation
and general research tasks.

Each sub-agent has a knowledge base in `/.claude/<agent-name>/` containing
domain-specific patterns and project conventions.

## Plans

Store all plans in the `.claude/plans/` directory. Use descriptive
filenames that reflect the task or feature being planned.

## Task Workflow

The primary agent follows this workflow for all tasks:

1. **Understand** - Clarify requirements with the user if needed.

2. **Plan** - Break the task into sub-tasks and identify required sub-agents.

3. **Delegate** - Dispatch each sub-task to the appropriate sub-agent.
   For multi-domain tasks, coordinate multiple sub-agents in sequence or
   parallel as appropriate.

4. **Verify** - After sub-agents complete their work, run `make test-all`
   to ensure all tests pass.

5. **Review** - For security-sensitive changes (auth, input handling,
   queries), delegate to **security-auditor** for review.

6. **Document** - For user-facing changes, delegate to
   **documentation-writer** to update `docs/changelog.md`.

7. **Report** - Synthesize sub-agent results and present a summary to
   the user.

**Remember:** The primary agent coordinates but never implements. Every
file change must come from a sub-agent.

## Documentation

### General Guidelines

- Place a `README.md` in each sub-project directory with a high-level
  overview and getting started information.

- Create an `index.md` as the entry point for each sub-project's docs;
  link to this file from the sub-project README.

- Link the top-level `README.md` to each sub-project README.

- Wrap all markdown files at 79 characters or less.

- Place `LICENSE.md` in both `/docs` and the repository root.

### Writing Style

- Use active voice.

- Write grammatically correct sentences between 7 and 20 words.

- Use semicolons to link related ideas or manage long sentences.

- Use articles (a, an, the) appropriately.

- Avoid ambiguous pronoun references; only use "it" when the referent is
  in the same sentence.

### Document Structure

- Use one first-level heading per file with multiple second-level headings.

- Limit third and fourth-level headings to prominent content only.

- Include an introductory sentence or paragraph after each heading.

- For Features or Overview sections, use the format: "The MCP Server
  includes the following features:" followed by a bulleted list.

### Lists

- Leave a blank line before the first item in any list or sub-list.

- Write each bullet as a complete sentence with articles.

- Do not bold bullet items.

- Use numbered lists only for sequential steps.

### Code Snippets

- Precede code with an explanatory sentence: "In the following example,
  the `command_name` command uses..."

- Use backticks for inline code: `SELECT * FROM table;`

- Use fenced code blocks with language tags for multi-line code:

  ```sql
  SELECT * FROM code;
  ```

- Format `stdio`, `stdin`, `stdout`, and `stderr` in backticks.

- Capitalise SQL keywords; use lowercase for variables.

### Links and References

- Link files outside `/docs` to their GitHub location.

- Include third-party installation/documentation links in Prerequisites.

- Link to the GitHub repo when referencing cloning or project work.

- Do not link to github.io.

### README.md Files

At the top of each README:

- GitHub Action badges for repository actions.

- Test deployment links (if applicable).

- Table of Contents mirroring the `mkdocs.yml` nav section.

- Link to online docs at docs.pgedge.com.

README body content:

- Getting started steps.

- Prerequisites with commands and third-party links.

- Build/install commands and minimal configuration notes.

- Deployment section linking to Installation, Configuration, and Usage
  pages in `/docs`.

At the end of each README:

- Issues link: "To report an issue with the software, visit:"

- Developer link: "We welcome your project contributions; for more
  information, see docs/developers.md."

- Online docs link: "For more information, visit
  [docs.pgedge.com](https://docs.pgedge.com)"

- License (final line): "This project is licensed under the
  [PostgreSQL License](LICENSE.md)."

### Additional Documentation Requirements

- Match all sample output to actual output.

- Document all command-line options.

- Include well-commented examples for all configuration options.

- Keep documentation synchronized with code for CLI options, configuration,
  and environment variables.

- Update `changelog.md` with notable changes since the last release.

## Tests

- Provide unit and integration tests for each sub-project.

- Execute tests with `go test` or `npm test` as appropriate.

- Write automated tests for all functions and features; use mocking where
  needed.

- Run all tests after any changes; check for errors and warnings that may
  be hidden by output redirection or truncation.

- Clean up temporary test files on completion; retain log files for
  debugging.

- Modify existing tests only when the tested functionality changes or to
  fix bugs.

- Include linting in standard test suites using locally installable tools.

- Enable coverage checking in standard test suites.

- Run `gofmt` on all Go files.

- Ensure `make test` runs all test suites.

- Do not skip database tests when testing changes.

- **After any code change, always run `gofmt` (for Go) and all relevant
  linters before considering the task complete.** A task is not finished
  until formatting and linting pass with no errors or warnings.

- Run `make test-all` in the top-level directory before completing a task.

## Security

- Maintain isolation between user sessions.

- Restrict database connections to their owning users or tokens.

- Protect against injection attacks at client and server; the exception is
  MCP tools that execute arbitrary SQL queries.

- Follow industry best practices for defensive secure coding.

- Review all changes for security implications; report potential issues.

## Code Style

- Use four spaces for indentation.

- Write readable, extensible, and appropriately modularised code.

- Minimise code duplication; refactor as needed.

- Follow language-specific best practices.

- Remove unused code.

- Use `COMMENT ON` to describe objects in database migrations.

- Include this copyright notice at the top of every source file (not
  configuration files); adjust comment style for the language:

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
