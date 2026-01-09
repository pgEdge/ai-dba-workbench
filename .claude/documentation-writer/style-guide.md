# Documentation Style Guide

This is the authoritative style guide for all documentation in the pgEdge AI
DBA Workbench project. These rules are derived from CLAUDE.md and must be
followed for all documentation.

## Writing Style

### Voice and Tone

**Use active voice throughout.**

```markdown
<!-- BAD - Passive voice -->
The connection is created by the server.
The query was executed.

<!-- GOOD - Active voice -->
The server creates the connection.
The system executed the query.
```

### Sentence Structure

**Use full, grammatically correct sentences between 7 and 20 words.**

```markdown
<!-- BAD - Too short -->
Creates user.

<!-- BAD - Too long -->
The system creates a new user account in the database with the specified
username and password after validating all input parameters and checking
for duplicate usernames.

<!-- GOOD - Appropriate length -->
The system creates a new user account with the specified credentials.
The server validates all input parameters before creating the account.
```

### Linking Ideas

**Use semicolons to link similar ideas or manage sentences that are
getting too long.**

```markdown
<!-- GOOD -->
The collector gathers metrics from PostgreSQL; it stores them in the
workbench database.

The server handles authentication; the client manages user sessions;
the collector focuses on data gathering.
```

### Articles

**Use articles (a, an, the) when appropriate.**

```markdown
<!-- BAD - Missing articles -->
Server creates connection.
User provides password.

<!-- GOOD - Proper articles -->
The server creates a connection.
A user provides the password.
```

### Pronoun Clarity

**Do not refer to an object as "it" unless the object is in the same
sentence.**

```markdown
<!-- BAD - Ambiguous "it" -->
The server validates the token. It returns an error if invalid.

<!-- GOOD - Clear reference -->
The server validates the token. The server returns an error if the
token is invalid.

<!-- ALSO GOOD - Same sentence -->
The server validates the token and returns an error if it is invalid.
```

### Emojis

**Do not use emojis unless explicitly requested.**

## Document Structure

### Headings

Each file should have:

- **One first-level heading** (`#`) - the document title
- **Multiple second-level headings** (`##`) - main sections
- **Third and fourth level headings** (`###`, `####`) - use sparingly for
  prominent content only

```markdown
# Document Title

Introduction paragraph here.

## First Main Section

Content here.

### Subsection (use sparingly)

More specific content.

## Second Main Section

More content.
```

### Introductions

**Each heading should have an introductory sentence or paragraph.**

```markdown
## Authentication

The authentication system manages user identity and access control.

### Token Types

The system supports three types of tokens for different use cases.
```

### Features/Overview Sections

**If a page has a Features or Overview section following the intro,
do not start with a heading.**

Use this format:

```markdown
# MCP Server

The MCP Server provides the API for LLM clients to interact with
the workbench.

The MCP Server includes the following features:

- Token-based authentication for secure access.
- Role-based access control for fine-grained permissions.
- Connection management for multiple PostgreSQL databases.
```

### Line Wrapping

**Wrap all markdown files at 79 characters or less.**

This ensures readability in terminals and editors without horizontal
scrolling.

## Lists

### Blank Lines Before Lists

**Always leave a blank line before the first item in any list or
sub-list.**

This includes:

- Bulleted lists
- Numbered lists
- Sub-lists within lists
- Code blocks within list items

```markdown
<!-- BAD - No blank line -->
The system supports:
- Feature one
- Feature two

<!-- GOOD - Blank line before list -->
The system supports:

- Feature one
- Feature two
```

### List Item Format

**Each entry in a bulleted list should be a complete sentence with
articles.**

```markdown
<!-- BAD - Incomplete items -->
- Authentication
- Database connections
- Query execution

<!-- GOOD - Complete sentences -->
- The system provides token-based authentication.
- Users can manage multiple database connections.
- The query interface supports SQL execution.
```

### No Bold in Lists

**Do not use bold font for bullet items.**

```markdown
<!-- BAD -->
- **Authentication** - handles user login

<!-- GOOD -->
- Authentication handles user login.
```

### Numbered Lists

**Only use numbered lists when steps must be performed in order.**

```markdown
<!-- Use numbers for sequential steps -->
1. Install the collector binary.
2. Configure the database connection.
3. Start the collector service.

<!-- Use bullets for unordered items -->
- The server handles authentication.
- The collector gathers metrics.
- The client provides the user interface.
```

## Code Snippets

### Explanatory Text

**Include an explanatory sentence before code.**

Use this format: "In the following example, the `command_name` command
uses..."

```markdown
In the following example, the `createUser` function validates the
username before creating the account:

` ` `go
func createUser(username string) error {
    if err := validateUsername(username); err != nil {
        return err
    }
    return db.Insert(username)
}
` ` `
```

### Inline Code

**Use backticks around single commands or lines of code.**

```markdown
Run `make test` to execute the test suite.
The function returns `nil` on success.
```

### Code Blocks

**Use fenced code blocks with language tags for multi-line code.**

```markdown
` ` `sql
SELECT * FROM users WHERE active = true;
` ` `

` ` `go
func main() {
    fmt.Println("Hello")
}
` ` `
```

### Special Terms in Backticks

These should always be in backticks:

- `stdio`
- `stdin`
- `stdout`
- `stderr`

### SQL Formatting

**Capitalize SQL keywords; use lowercase for identifiers.**

```markdown
` ` `sql
SELECT id, username FROM users WHERE status = 'active';
INSERT INTO connections (name, host) VALUES ('prod', 'db.example.com');
` ` `
```

## Links and References

### External File Links

**Links to files outside /docs should link to the GitHub copy.**

```markdown
See the [schema definition](https://github.com/pgEdge/ai-dba-workbench/
blob/main/collector/src/database/schema.go).
```

### Third-Party Links

**Include links to installation/documentation pages in Prerequisites
sections.**

```markdown
## Prerequisites

- [Go 1.21+](https://go.dev/doc/install)
- [Node.js 18+](https://nodejs.org/)
- [PostgreSQL 14+](https://www.postgresql.org/download/)
```

### GitHub References

**Link to GitHub when referring to cloning or contributing.**

```markdown
Clone the repository:

` ` `bash
git clone https://github.com/pgEdge/ai-dba-workbench.git
` ` `
```

### Prohibited Links

**Do not create links to github.io.**

## README.md Requirements

### Top Section

Include at the top of each README:

1. **GitHub Action badges** for important actions
2. **Test deployment links** (if applicable)
3. **Table of Contents** mirroring mkdocs.yaml nav
4. **Link to online docs** at docs.pgedge.com

### Body Content

README files must contain:

- Steps to get started with the project
- Commands to satisfy prerequisites
- Commands to build/install
- Minimal configuration changes for deployment
- Links to detailed documentation in /docs

### Prerequisites Section

- Link to download/documentation for third-party software
- Include version requirements
- Provide installation commands

### Deployment Section

Include links to:

- Installation page in /docs
- Configuration page in /docs
- Usage page in /docs

### End Section

Include at the end of each README:

1. **Issues link:**

   ```markdown
   To report an issue with the software, visit:
   [GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)
   ```

2. **Developer link:**

   ```markdown
   We welcome your project contributions; for more information, see
   [docs/developers.md](docs/developers.md).
   ```

3. **Online docs link:**

   ```markdown
   For more information, visit
   [docs.pgedge.com](https://docs.pgedge.com)
   ```

4. **License (last line):**

   ```markdown
   This project is licensed under the
   [PostgreSQL License](LICENSE.md).
   ```

## Additional Requirements

### Sample Output

**Ensure all sample output matches actual output.**

Test commands and verify output before documenting.

### Command Line Options

**Document all command line options.**

Include:

- Option name (short and long form)
- Description
- Default value
- Example usage

### Configuration Examples

**Ensure all configuration examples contain well-commented examples
of all options.**

```yaml
# Server configuration
server:
  # Address to listen on (default: :8080)
  address: ":8080"

  # Enable debug logging (default: false)
  debug: false
```

### Keeping Documentation Current

**Ensure documentation ALWAYS matches the code.**

This applies to:

- Command line options
- Configuration options
- Environment variables
- API endpoints
- Function signatures

### Changelog

**Update changelog.md with notable changes since the last release.**

Format:

```markdown
## [Version] - YYYY-MM-DD

### Added

- New feature description.

### Changed

- Modified behavior description.

### Fixed

- Bug fix description.

### Removed

- Removed feature description.
```
