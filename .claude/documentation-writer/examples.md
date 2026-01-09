# Documentation Examples

This document provides examples of well-written documentation and
references to good documentation in the project.

## Good Documentation in This Project

### Project-Level Documentation

| Document | Location | Why It's Good |
|----------|----------|---------------|
| DESIGN.md | `/DESIGN.md` | Clear architecture explanation |
| CLAUDE.md | `/CLAUDE.md` | Comprehensive instructions |

### Knowledge Base Examples

The knowledge bases in `/.claude/` demonstrate good technical documentation:

- `/.claude/postgres-expert/README.md` - Good index structure
- `/.claude/testing-expert/README.md` - Clear organization
- `/.claude/golang-expert/README.md` - Comprehensive coverage

## Style Guide Compliance Examples

### Good Sentence Structure

```markdown
<!-- GOOD: Active voice, 7-20 words -->
The server validates incoming requests before processing them.
Users must authenticate before accessing protected resources.
The collector stores metrics in partitioned PostgreSQL tables.
```

### Good List Formatting

```markdown
<!-- GOOD: Blank line before list, complete sentences -->
The authentication system supports multiple token types:

- Service tokens provide long-lived access for automated systems.
- User tokens allow scoped access to specific resources.
- Session tokens manage temporary user sessions.
```

### Good Code Documentation

```markdown
<!-- GOOD: Explanatory sentence before code -->
In the following example, the `CreateConnection` function establishes
a new database connection with the specified parameters:

` ` `go
func CreateConnection(ctx context.Context, cfg Config) (*Connection, error) {
    pool, err := pgxpool.New(ctx, cfg.ConnectionString())
    if err != nil {
        return nil, fmt.Errorf("failed to create pool: %w", err)
    }
    return &Connection{pool: pool}, nil
}
` ` `
```

### Good Heading Structure

```markdown
<!-- GOOD: One H1, multiple H2, limited H3 -->
# MCP Server

The MCP Server provides the API for LLM clients.

## Authentication

The server uses token-based authentication.

### Token Types

Three token types are supported for different use cases.

## Tools

Tools provide operations that LLMs can invoke.

## Resources

Resources provide read-only data access.
```

## Anti-Pattern Examples

### Poor Sentence Structure

```markdown
<!-- BAD: Passive voice -->
The request is processed by the server.

<!-- BAD: Too short, no article -->
Creates user.

<!-- BAD: Too long -->
The server processes the incoming request by first validating the
authentication token and then checking the user's permissions before
finally executing the requested operation and returning the result to
the client.
```

### Poor List Formatting

```markdown
<!-- BAD: No blank line before list -->
The system supports:
- Feature one
- Feature two

<!-- BAD: Incomplete items, bold text -->
- **Auth** - login stuff
- **DB** - database
```

### Poor Code Documentation

```markdown
<!-- BAD: No explanation -->
` ` `go
func CreateConnection(ctx context.Context, cfg Config) (*Connection, error)
` ` `

<!-- BAD: Code without language tag -->
` ` `
SELECT * FROM users
` ` `
```

## Real-World Good Examples

### Good README Section

```markdown
## Prerequisites

Before installing the MCP Server, ensure you have the following:

- [Go 1.21](https://go.dev/doc/install) or higher installed.
- [PostgreSQL 14](https://www.postgresql.org/download/) or higher running.
- Network access to the workbench database.

## Installation

Clone the repository and build the server:

` ` `bash
git clone https://github.com/pgEdge/ai-dba-workbench.git
cd ai-dba-workbench/server
make build
` ` `

The build process creates the `ai-workbench-server` binary in the
current directory.
```

### Good API Documentation

```markdown
## create_connection

The `create_connection` tool creates a new database connection entry.

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Display name for the connection |
| `host` | string | Yes | PostgreSQL server hostname |
| `port` | integer | No | Server port (default: 5432) |
| `database` | string | Yes | Database name to connect to |
| `username` | string | Yes | PostgreSQL username |
| `password` | string | Yes | PostgreSQL password |

### Example

In the following example, the tool creates a connection to a production
database:

` ` `json
{
    "name": "Production DB",
    "host": "db.example.com",
    "port": 5432,
    "database": "production",
    "username": "app_user",
    "password": "secure_password"
}
` ` `

### Response

The tool returns the created connection details:

` ` `json
{
    "id": 42,
    "name": "Production DB",
    "host": "db.example.com",
    "status": "created"
}
` ` `
```

### Good Troubleshooting Section

```markdown
## Troubleshooting

### Connection Refused Error

**Symptom:** The server fails to start with "connection refused" error.

**Cause:** The PostgreSQL server is not running or not accepting
connections on the configured port.

**Solution:** Verify PostgreSQL is running:

` ` `bash
pg_isready -h localhost -p 5432
` ` `

If the server is not running, start it:

` ` `bash
sudo systemctl start postgresql
` ` `

### Authentication Failed Error

**Symptom:** The server starts but returns "authentication failed"
when clients connect.

**Cause:** The configured database credentials are incorrect or the
user lacks necessary permissions.

**Solution:** Verify the credentials in your configuration match a
valid PostgreSQL user with appropriate permissions.
```

## Checklist for Good Documentation

Use this checklist when writing documentation:

- [ ] Active voice throughout
- [ ] Sentences are 7-20 words
- [ ] Proper articles (a, an, the) used
- [ ] No ambiguous pronouns ("it" in different sentence)
- [ ] Blank line before every list
- [ ] List items are complete sentences
- [ ] Code blocks have language tags
- [ ] Explanatory text before code examples
- [ ] Line length under 79 characters
- [ ] One H1, multiple H2, limited H3/H4
- [ ] Each heading has introductory text
- [ ] Links to external resources where appropriate
- [ ] No emojis (unless requested)
