# Claude Standing Instructions

> Standing instructions for Claude Code when working on this project.
> This document supplements the architectural design in DESIGN.md.

## Project Structure

The pgEdge AI DBA Workbench consists of four sub-projects:

- `/collector` - Data collector (Go).

- `/client` - Web client application (React/TypeScript).

- `/server` - MCP server (Go).

- `/cli` - Command-line interface (Go).

Each sub-project follows this base structure:

- `/src` - Source code.

- `/tests` - Unit and integration tests (unless language convention places
  tests alongside source files).

- `/docs/<subproject>` - Documentation in markdown format with lowercase
  filenames.

## Key Files

Reference these files for project context:

- `DESIGN.md` - Architecture and design philosophy.

- `CHANGELOG.md` - Notable changes by release.

- `mkdocs.yml` - Documentation site navigation.

- `Makefile` - Build and test commands.

## Sub-Agents

Specialized sub-agents in `/.claude/agents/` handle complex domain tasks.
Most sub-agents research and recommend; they do not edit code directly.
The documentation-writer is the exception and writes documentation files.

Use the appropriate sub-agent for these domains:

- **postgres-expert** - PostgreSQL administration, tuning, troubleshooting.

- **spock-expert** - pgEdge Spock replication, Snowflake, Lolor extensions.

- **golang-expert-advisor** - Go architecture, best practices, code review.

- **react-mui-advisor** - React/MUI component design, frontend patterns.

- **mcp-server-expert** - MCP protocol, tool implementation, debugging.

- **testing-framework-architect** - Test strategies for Go and React.

- **security-auditor** - Security review, vulnerability detection, OWASP.

- **code-reviewer** - Code quality, bug detection, anti-patterns.

- **codebase-navigator** - Finding code, tracing data flow, structure.

- **documentation-writer** - Documentation following project style guide.

- **design-compliance-validator** - Ensuring changes align with DESIGN.md.

Each sub-agent has a knowledge base in `/.claude/<agent-name>/` containing
domain-specific patterns and project conventions.

## Task Workflow

Follow this workflow for implementation tasks:

1. Read relevant code before proposing changes.

2. Use sub-agents for complex domain questions.

3. Run `make test-all` before marking implementation complete.

4. Review security implications for auth, input handling, or query changes.

5. Update `CHANGELOG.md` for user-facing changes.

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
   * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
   * This software is released under The PostgreSQL License
   *
   *-------------------------------------------------------------------------
   */
  ```
