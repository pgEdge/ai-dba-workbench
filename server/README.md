# pgEdge AI DBA Workbench MCP Server

The MCP (Model Context Protocol) Server provides AI assistants with standardized
access to PostgreSQL systems through HTTP/HTTPS endpoints with authentication.

## Features

- HTTP/HTTPS transport with JSON-RPC 2.0
- SQLite-based authentication with users, sessions, and service tokens
- Multi-database support with access control
- MCP tools for database operations
- MCP resources for schema and data access
- MCP prompts for common workflows
- LLM proxy support for Anthropic, OpenAI, and Ollama
- Conversation history management

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

### Authentication Storage

Authentication data is stored in a SQLite database (`auth.db`) within the data
directory. By default, this is `./data/auth.db` relative to the server binary.

The auth store contains:

- **Users**: Username/password accounts for interactive authentication
- **Session tokens**: Temporary tokens issued after user authentication (24h
  validity)
- **Service tokens**: Long-lived tokens for machine-to-machine communication

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
# Add a new service token (interactive)
./bin/ai-dba-server -add-token

# Add a new service token (non-interactive)
./bin/ai-dba-server -add-token -token-note "Production API" -token-expiry "90d"

# List all service tokens
./bin/ai-dba-server -list-tokens

# Remove a service token by ID or hash prefix
./bin/ai-dba-server -remove-token <token-id-or-hash>
```

**Token Expiry Formats:**

- `30d` - 30 days
- `1y` - 1 year
- `2w` - 2 weeks
- `12h` - 12 hours
- `never` - Token never expires

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
