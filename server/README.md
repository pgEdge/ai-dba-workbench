# pgEdge AI DBA Workbench MCP Server

[![Build Server](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/build-server.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/build-server.yml)
[![Test Server](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/test-server.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/test-server.yml)
[![Lint Server](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/lint-server.yml/badge.svg)](https://github.com/pgEdge/ai-dba-workbench/actions/workflows/lint-server.yml)

The MCP (Model Context Protocol) Server provides AI assistants with standardized
access to PostgreSQL systems through HTTP/HTTPS endpoints with authentication.

For complete documentation, visit [docs.pgedge.com](https://docs.pgedge.com).

## Table of Contents

- [Features](#features)
- [Building](#building)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Documentation](#documentation)

## Features

- HTTP/HTTPS transport with JSON-RPC 2.0.
- SQLite-based authentication with users, sessions, and API
  tokens.
- Role-based access control with groups, privileges, and
  token scopes.
- Admin panel for managing users, groups, and tokens.
- Multi-database support with per-connection access levels.
- MCP tools for database operations.
- MCP resources for schema and data access.
- MCP prompts for common workflows.
- LLM proxy support for Anthropic, OpenAI, and Ollama.
- Conversation history management.

## Building

```bash
# Build the server
make build

# Run tests
make test

# Run linting
make lint
```

## Quick Start

1. Build the server:
   ```bash
   make build
   ```

2. Create a user:
   ```bash
   ./bin/ai-dba-server -add-user -username admin
   ```

3. Create a service token:
   ```bash
   ./bin/ai-dba-server -add-token
   ```

4. Start the server:
   ```bash
   ./bin/ai-dba-server
   ```

## Configuration

The server is configured via YAML configuration file and/or command line flags.
See [`examples/ai-dba-server.yaml`](../examples/ai-dba-server.yaml) for a
complete example configuration.

### Command Line Options

**General Options:**

- `-config string` - Path to configuration file
- `-addr string` - HTTP server address (default: `:8080`)
- `-tls` - Enable TLS/HTTPS
- `-cert string` - Path to TLS certificate file
- `-key string` - Path to TLS key file
- `-chain string` - Path to TLS certificate chain file
- `-debug` - Enable debug logging
- `-data-dir string` - Data directory for auth database and conversations

**Database Connection Options:**

- `-db-host string` - Database host
- `-db-port int` - Database port
- `-db-name string` - Database name
- `-db-user string` - Database user
- `-db-password string` - Database password
- `-db-sslmode string` - Database SSL mode (disable, require, verify-ca,
  verify-full)

**Tracing Options:**

- `-trace-file string` - Path to trace file for logging MCP requests/responses

### Authentication Storage

Authentication data is stored in a SQLite database (`auth.db`) within the data
directory. By default, this is `./data/auth.db` relative to the server binary.

The auth store contains:

- Users and service accounts with password hashes,
  superuser flags, and group memberships.
- API tokens with expiry dates, owner references, and
  optional scope restrictions.
- Groups with nested membership and assigned connection,
  MCP, and admin privileges.
- Token scopes that restrict tokens to specific
  connections, MCP privileges, and admin permissions.

### User Management

```bash
# Add a new user (interactive)
./bin/ai-dba-server -add-user

# Add a new user (non-interactive)
./bin/ai-dba-server -add-user -username alice -password "SecurePass123!"

# List all users
./bin/ai-dba-server -list-users

# Update a user
./bin/ai-dba-server -update-user -username alice

# Enable a user (also resets failed login attempts)
./bin/ai-dba-server -enable-user -username alice

# Disable a user
./bin/ai-dba-server -disable-user -username alice

# Delete a user
./bin/ai-dba-server -delete-user -username alice
```

### Token Management

```bash
# Add a new token (interactive)
./bin/ai-dba-server -add-token

# Add a new token (non-interactive, specifying owner)
./bin/ai-dba-server -add-token \
  -user alice \
  -token-note "Production API" \
  -token-expiry "90d"

# List all tokens
./bin/ai-dba-server -list-tokens

# Remove a token by ID or hash prefix
./bin/ai-dba-server -remove-token <token-id-or-hash>
```

**Token Expiry Formats:**

- `30d` - 30 days
- `1y` - 1 year
- `2w` - 2 weeks
- `12h` - 12 hours
- `never` - Token never expires

### Group Management

```bash
# Add a new RBAC group
./bin/ai-dba-server -add-group -group developers

# List all groups
./bin/ai-dba-server -list-groups

# Add a user to a group
./bin/ai-dba-server -add-member -username alice -group developers

# Remove a user from a group
./bin/ai-dba-server -remove-member -username alice -group developers

# Delete a group
./bin/ai-dba-server -delete-group -group developers

# Set superuser status for a user
./bin/ai-dba-server -set-superuser -username admin

# Remove superuser status from a user
./bin/ai-dba-server -unset-superuser -username admin
```

### Privilege Management

```bash
# List all registered MCP privileges
./bin/ai-dba-server -list-privileges

# Grant a privilege to a group
./bin/ai-dba-server -grant-privilege -group developers -privilege query_database

# Revoke a privilege from a group
./bin/ai-dba-server -revoke-privilege -group developers -privilege query_database

# Grant connection access to a group
./bin/ai-dba-server -grant-connection -group developers -connection 1 \
  -access-level read_write

# Show privileges for a group
./bin/ai-dba-server -show-group-privileges -group developers
```

### Token Scope Management

Token scopes restrict a token to a subset of the owner's
permissions. The system supports three scope types:
connections (with access levels), MCP privileges, and admin
permissions.

```bash
# Show current scope for a token
./bin/ai-dba-server -show-token-scope -token-id 1

# Restrict token to specific connections
./bin/ai-dba-server -scope-token-connections -token-id 1 \
  -scope-connections "1,2,3"

# Restrict token to specific MCP tools
./bin/ai-dba-server -scope-token-tools -token-id 1 \
  -scope-tools "query_database,get_schema_info"

# Clear all scope restrictions from a token
./bin/ai-dba-server -clear-token-scope -token-id 1
```

### Security Features

- **Password hashing**: Bcrypt with cost factor 12
- **Token hashing**: SHA256 for secure storage
- **Rate limiting**: Configurable per-IP rate limiting
- **Account lockout**: Automatic disabling after failed login attempts
- **Session isolation**: Per-token database connection pools

### Configuration File

The server searches for configuration in the following order:

1. Path specified via `-config` flag
2. `/etc/pgedge/ai-dba-server.yaml` (system-wide)
3. `./ai-dba-server.yaml` (binary directory)

Key configuration sections:

```yaml
http:
  address: ":8080"
  tls:
    enabled: false
  auth:
    enabled: true
    max_failed_attempts_before_lockout: 5
    max_user_token_days: 90
    rate_limit_window_minutes: 15
    rate_limit_max_attempts: 10

database:
  host: "localhost"
  port: 5432
  database: "ai_workbench"
  user: "postgres"
  sslmode: "prefer"
```

## Documentation

See the [Server Documentation](../docs/server/index.md) for detailed information.

---

To report an issue with the software, visit:
[GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)

We welcome your project contributions; for more information, see
[docs/developers.md](../docs/developers.md).

For more information, visit [docs.pgedge.com](https://docs.pgedge.com)

This project is licensed under the [PostgreSQL License](../LICENSE.md).
