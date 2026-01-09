# Claude Standing Instructions

> This document provides standing instructions for Claude Code when working on
> this project. It supplements the design in DESIGN.md.

## Project Structure

The pgEdge AI DBA Workbench consists of the following sub-projects:

- The data collector is implemented in the `/collector` directory.

- The web client application is implemented in the `/client` directory.

- The MCP server is implemented in the `/server` directory.

- The command-line interface is implemented in the `/cli` directory.

All sub-projects should follow the following base structure:

- Comprehensive documentation files should be created in markdown format
  under the `/docs` directory, for each sub-project (e.g. `/docs/collector`,
  `/docs/cli`, `/docs/client`, and `/docs/server`), with a top level index
  in `/docs`.

- Documentation files in the `/docs` directory should always use lower case
  filenames.

- Unit and integration tests should be created under the `/tests` subdirectory
  of each project (e.g. `/client/tests`, `/collector/tests`, and
  `/server/tests`), except where the language convention is to include unit
  tests in the same directory as the code they are testing.

- Source code should be created under the `/src` subdirectory of each project
  (e.g. `/client/src`, `/collector/src`, and `/server/src`).

## Documentation

### General Guidelines

- A `README.md` file in the sub-project top level directory should provide a
  very high level overview of the sub-project; it should include basic getting
  started information for developers and users.

- The documentation for each sub-project should have an `index.md` file acting
  as the entry point for the reader; this should be linked from the
  `README.md` file for the sub-project.

- The top-level `README.md` file should link to the `README.md` file for each
  sub-project.

- Wrap all markdown files at 79 characters or less.

- `LICENSE.md` should live in the `/docs` folder and in the root of the repo.

### Writing Style

- Write in active voice.

- Use full and grammatically correct sentences that are between 7 and 20 words
  long.

- Use a semicolon to link similar ideas or manage sentences that are getting
  over-long.

- Use articles (a, an, and the) when appropriate.

- Do not refer to an object as "it" unless the object "it" refers to is in the
  same sentence; this avoids ambiguity.

### Document Structure

- Each file should have one first level heading, and multiple second level
  headings; use third and fourth level headings for prominent content only.

- Each heading should have an introductory sentence or paragraph that explains
  the feature shown/discussed in the following section.

- If the page has a `Features` or `Overview` section following the
  introductory paragraph, it should not start with a heading; instead use a
  sentence in the form: "The MCP Server includes the following features:",
  followed by a bulleted list of the features.

### Lists

- Always leave a blank line before the first item in any list or sub-list
  (a sub-list may be code or indented bullets under a bullet item); this
  ensures the lists render properly in tools such as mkdocs.

- Each entry in a bulleted list should be a complete sentence with articles.

- Do not use bold font for bullet items.

- Do not use a numbered list unless the steps in the list need to be performed
  in order.

### Code Snippets

- If a section contains code or a code snippet, there should be an explanatory
  sentence before the code in the form: "In the following example, the
  `command_name` command uses a column named `my_column` to accomplish
  description-of-what-the-code-does."

- Use backticks around a single command or line of code: `SELECT * FROM code;`

- Use block quotes around multi-line code samples and include the code type in
  the format tag:

  ```sql
  SELECT * FROM code;
  SELECT * FROM code;
  ```

- `stdio`, `stdin`, `stdout`, and `stderr` should be in backticks.

- Capitalise command keywords; lowercase variables.

### Links and References

- Links to files outside of `/docs` should link to the copy on GitHub.

- Include links to third-party software installation/documentation pages in
  the Prerequisites section.

- Include links to our GitHub repo when we refer to cloning the repo, or
  working on the project.

- Do not create links to github.io.

### README.md Files

At the top of each README file:

- Include GitHub Action badges for important actions in use by the repository.

- Include test deployment links (if used for the project).

- Include a Table of Contents that mimics the nav section of the `mkdocs.yaml`
  file.

- After the TOC include a link to the online docs, hosted at docs.pgedge.com.

README files should contain:

- The steps required to get started with the project.

- The commands to satisfy prerequisites, commands to build/install the
  binary/project, and notes about the minimal configuration changes required
  to deploy.

- The prerequisites section should link to download/documentation links for
  third-party software when possible.

- In the deployment section, include links to the Installation, Configuration,
  and Usage pages in the `/docs` folder.

At the end of each README:

- Include a link to the Issues page for the project: "To report an issue with
  the software, visit:"

- Include a section/link for Developers/Project contributors that links to
  developer documentation if available (and if developer documentation is not
  available, link to the GH site): "We welcome your project contributions;
  for more information, see docs/developers.md."

- Include a link to the online documentation at: "For more information, visit
  [docs.pgedge.com](https://docs.pgedge.com)"

- Last thing in the file, include the sentence: "This project is licensed
  under the [PostgreSQL License](LICENSE.md)."

### Additional Documentation Requirements

- Ensure all sample output matches what would actually be output.

- Ensure all command line options are documented.

- Ensure all configuration examples for all configuration files contain well
  commented examples of all options.

- Ensure documentation on command line options, configuration options,
  environment variables, and other user-facing controls ALWAYS match the code.

- Ensure `changelog.md` has been updated to include notable changes made since
  the last release.

## Tests

- Unit and integration tests should be provided for each sub-project.

- Tests should all be executable using `go test` or `npm test`, as appropriate
  for the specific sub-project.

- All code functions and features should have automated tests to the extent
  possible, using mocking where required.

- All tests should be run following any changes being made; take care not to
  miss any error messages or warnings due to output redirection or truncation.

- Temporary files created during test execution must be cleaned up when the
  test run completes, except where they contain useful debugging information
  (for example, log files).

- Existing tests should never be modified unless the functionality they are
  exercising has been changed, or to fix bugs or refactor code.

- Ensure linting tests are included, and run under the standard test suites
  utilising locally installable tools.

- Ensure coverage can be checked, using the standard test suites utilising
  locally installable tools.

- Ensure `gofmt` has been run on all Go files.

- Ensure ALL test suites are run by `make test`.

- DO NOT skip DB tests when testing new changes.

- ALWAYS test new changes with `make test-all` in the top level project
  directory before completing a task.

## Security

- Always ensure isolation is maintained between user sessions.

- Always ensure that database connections are only accessible to the users or
  tokens that own them.

- Protect against injection attacks of any kind, at both the client and
  server; the only exception is in an MCP Tool allowing arbitrary SQL queries
  to be executed.

- Always follow industry best practices for defensive secure coding.

- Always review any changes for security implications and report any potential
  issues found.

## Code Style

- Always use four spaces for indentation.

- Always ensure the code is written in a way that is readable, extensible, and
  is appropriately modularised.

- Always ensure code duplication is minimised, refactoring where needed.

- Always ensure ALL code follows best practices for the language used.

- Perform searches for unused code and remove any found.

- When creating database migrations, always use `COMMENT ON` to describe the
  objects created.

- Include the following copyright notice at the top of every source file (but
  not configuration files); adjust the comment style and project name as
  appropriate for the language:

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
