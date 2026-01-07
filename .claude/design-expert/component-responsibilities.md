# Component Responsibilities: pgEdge AI DBA Workbench

This document defines the clear boundaries and responsibilities for each
component in the pgEdge AI DBA Workbench architecture.

## Architecture Overview

The pgEdge AI DBA Workbench consists of four main components:

1. **Collector**: Metrics collection and storage
2. **Server**: MCP protocol server
3. **CLI**: Command-line interface for testing and administration
4. **Client**: Web-based user interface (future)

Each component has distinct responsibilities and clear boundaries.

## Shared Infrastructure

### PostgreSQL Datastore

**Responsibility**: Persistent storage for all system data.

**Owned By**: None (shared infrastructure)

**Accessed By**:
- Collector: Read/write to metrics schema, write to config tables
- Server: Read from metrics schema, read/write to config/auth tables
- CLI: Admin operations (user creation, service token management)
- Client: None (accesses via Server)

**Schema Organization**:
- `public` schema: User accounts, tokens, connections, groups, privileges
- `metrics` schema: Time-series probe data (partitioned tables)

**Access Pattern**: Connection pooling via pgx/v5

---

## Collector Component

**Location**: `/collector`

**Binary**: `collector`

**Purpose**: Continuously collect metrics from monitored PostgreSQL servers
and store them in the datastore.

### Core Responsibilities

#### 1. Metrics Collection

**What**: Execute configured probes against monitored PostgreSQL connections
on scheduled intervals.

**How**:
- Scheduler maintains probe execution schedule
- Each probe defines SQL query to execute
- Execute against monitored connection(s)
- Parse and normalize results
- Store in probe-specific partitioned table

**Probe Types**:
- Database-scoped: Execute per accessible database
- Server-scoped: Execute once per connection

**Data Flow**:
```
Monitored PostgreSQL -> Probe.Execute() -> metrics.{probe_table}
```

#### 2. Schema Management

**What**: Ensure datastore schema is current by applying pending migrations.

**How**:
- SchemaManager loads all registered migrations
- On startup, check current schema version
- Apply pending migrations in order
- Each migration runs in transaction
- Record applied migration in schema_version table

**Migration Format**:
```go
Migration{
    Version: 6,
    Description: "User groups and privilege management",
    Up: func(conn *pgxpool.Conn) error { ... }
}
```

**Constraints**:
- Migrations are immutable (never change after applied)
- Migrations execute in order by version number
- Failed migration rolls back and stops startup

#### 3. Partition Management

**What**: Ensure partitions exist for time-series data and drop expired
partitions.

**How**:
- Before storing metrics, ensure partition exists for week
- Partition naming: `{table_name}_YYYYMMDD` (Monday of week)
- Garbage collector runs daily
- Drop partitions where all data exceeds retention period
- Exception: Change-tracked probes preserve latest partition

**Partition Structure**:
```
metrics.pg_stat_database
  ├── metrics.pg_stat_database_20251103  (Nov 3-9)
  ├── metrics.pg_stat_database_20251110  (Nov 10-16)
  └── metrics.pg_stat_database_20251117  (Nov 17-23)
```

#### 4. Connection Pool Management

**What**: Maintain connection pools to monitored PostgreSQL servers.

**How**:
- MonitoredConnectionPool manages connections
- Pools created on-demand for enabled connections
- Credentials decrypted from database
- Pool size configurable per connection
- Automatic reconnection on failure

**Pool Lifecycle**:
1. Load connection config from database
2. Decrypt password using server_secret + owner
3. Create pgxpool.Pool with config
4. Maintain pool for probe execution
5. Close pool on shutdown or connection deletion

#### 5. Probe Configuration

**What**: Load probe configurations from database and manage probe lifecycle.

**How**:
- Each probe type registers with ProbeRegistry
- Configuration stored in probe_configs table
- Per-connection overrides supported
- Enabled/disabled flag per probe per connection
- Collection interval and retention days configurable

**Configuration Priority**:
1. Connection-specific config (if exists)
2. Global default config

### What Collector Does NOT Do

- Does NOT implement MCP protocol
- Does NOT handle user authentication
- Does NOT serve HTTP/HTTPS endpoints
- Does NOT provide query interfaces
- Does NOT perform authorization checks (trusts database state)

### Configuration

**Configuration File**: `ai-workbench.conf` (shared with Server)

**Collector-Specific Options**:
- None (all options are datastore connection parameters)

**Required Configuration**:
- pg_host: Datastore hostname
- pg_database: Datastore database name
- pg_username: Datastore username
- pg_password_file: Path to password file
- server_secret: Encryption key material

### Startup Sequence

1. Load configuration from file and command-line
2. Connect to PostgreSQL datastore
3. Run schema migrations (SchemaManager.Migrate())
4. Initialize probe registry
5. Load probe configurations from database
6. Create connection pools for enabled monitored connections
7. Start scheduler for probe execution
8. Start garbage collector thread
9. Enter main loop (wait for shutdown signal)

### Shutdown Sequence

1. Receive SIGINT or SIGTERM
2. Stop accepting new probe executions
3. Wait for in-flight probes to complete
4. Close monitored connection pools
5. Close datastore connection pool
6. Exit cleanly

### Error Handling

**Probe Execution Failures**:
- Log error
- Continue with next scheduled execution
- Don't crash collector

**Connection Failures**:
- Log error
- Retry on next scheduled execution
- Automatic reconnection via pgxpool

**Schema Migration Failures**:
- Roll back transaction
- Log error
- Exit with error code (don't start with wrong schema)

**Partition Creation Failures**:
- Log error
- Retry on next metrics collection
- Don't lose metrics data

### Testing Responsibilities

**Unit Tests**: `/collector/tests` (Go convention: `*_test.go` in same dir)

**Test Coverage**:
- Probe execution logic
- Schema migration application
- Partition creation/deletion
- Connection pool management
- Configuration loading

**Test Database**: Temporary database with timestamp

---

## Server Component

**Location**: `/server`

**Binary**: `mcp-server`

**Purpose**: Implement MCP protocol to provide tools, resources, and prompts
for AI assistants to interact with PostgreSQL systems.

### Core Responsibilities

#### 1. MCP Protocol Implementation

**What**: Implement Model Context Protocol specification for AI assistant
integration.

**How**:
- HTTP/HTTPS endpoints with Server-Sent Events
- JSON-RPC 2.0 message format
- Request routing to appropriate handlers
- Error handling per MCP spec

**Endpoints**:
- `/sse`: Server-Sent Events endpoint for MCP protocol
- `/health`: Health check endpoint (JSON)

**MCP Methods**:
- `initialize`: Session initialization
- `ping`: Health check
- `resources/list`: List available resources
- `resources/read`: Read resource content
- `tools/list`: List available tools
- `tools/call`: Execute tool
- `prompts/list`: List available prompts

#### 2. Authentication

**What**: Validate bearer tokens and establish user identity.

**How**:
- Extract bearer token from Authorization header
- Hash token and look up in database
- Check token expiry
- Resolve user identity and privileges
- Store UserInfo in handler context

**Token Types Supported**:
- Session tokens (user_sessions): 24-hour interactive sessions
- User tokens (user_tokens): Long-lived user-owned API tokens
- Service tokens (service_tokens): Standalone automation tokens

**Authentication Flow**:
```
1. Client sends request with Authorization: Bearer <token>
2. Server extracts token from header
3. Server hashes token (SHA-256)
4. Server queries user_sessions, user_tokens, service_tokens
5. Server checks expiry
6. Server resolves user/token identity
7. Server proceeds with authenticated request
```

**Unauthenticated Methods**:
- `initialize`: Protocol handshake
- `ping`: Health check
- `tools/call` with `authenticate_user`: Login

#### 3. Authorization

**What**: Enforce privilege checks for all protected operations.

**How**:
- Use privileges package to check access
- Validate connection access before database operations
- Validate MCP item access before tool/resource/prompt execution
- Check token scoping restrictions
- Superuser bypass

**Authorization Points**:
1. **Connection Access**: Before querying monitored database
2. **MCP Item Access**: Before executing tool/resource/prompt
3. **Token Scope**: After owner access but before execution
4. **Administrative Operations**: Superuser-only operations

**Check Functions**:
- `privileges.CanAccessConnection()`: Connection access validation
- `privileges.CanAccessMCPItem()`: MCP item access validation
- Token scope: Manual check against token_connection_scope,
  token_mcp_scope

#### 4. Tool Implementation

**What**: Implement MCP tools for database operations and management.

**Tool Categories**:
1. **Authentication**: User login, token management
2. **Connection Management**: Create/list/modify connections
3. **Database Operations**: Query execution, configuration reading
4. **Metrics Access**: Historical and real-time metrics
5. **User Management**: Account and group management
6. **Privilege Management**: Grant/revoke access

**Tool Handler Pattern**:
```go
func (h *Handler) handleToolFoo(params FooParams) (*ToolResponse, error) {
    // 1. Validate parameters
    // 2. Check privileges (if needed)
    // 3. Execute operation
    // 4. Format response
    return response, nil
}
```

**Tool Registration**: Each tool registers privilege identifier on startup.

#### 5. Resource Implementation

**What**: Provide MCP resources for read-only data access.

**Resource Categories**:
1. **Historical Metrics**: Time-series data from probes
2. **Real-time Snapshots**: Current state from monitored servers
3. **Configuration**: PostgreSQL configuration files
4. **Logs**: Database server logs

**Resource Pattern**:
```
resource://{type}/{connection_id}/{specifics}

Examples:
resource://metrics/pg_stat_database/5?start=2025-11-01&end=2025-11-08
resource://realtime/pg_stat_activity/5
resource://config/postgresql.conf/5
```

#### 6. User Management

**What**: Manage user accounts, groups, and memberships.

**Operations**:
- Create/update/delete users
- Create/update/delete groups
- Add/remove group members (users and nested groups)
- List groups and memberships
- Resolve transitive group membership

**Module**: `usermgmt` package

**Database Tables**:
- user_accounts
- user_groups
- group_memberships

#### 7. Group Management

**What**: Manage hierarchical groups and privilege grants.

**Operations**:
- Create/update/delete groups
- Add/remove members (users and groups)
- Grant/revoke connection privileges
- Grant/revoke MCP privileges
- List group privileges

**Module**: `groupmgmt` package

**Database Tables**:
- user_groups
- group_memberships
- connection_privileges
- group_mcp_privileges

#### 8. Privilege Seeding

**What**: Ensure all MCP privilege identifiers are registered in database.

**How**:
- On startup, call privileges.SeedMCPPrivileges()
- Insert any missing identifiers
- Idempotent operation (safe to run multiple times)

**Identifiers Include**:
- All tools: `tool:{tool_name}`
- All resources: `resource:{resource_type}`
- All prompts: `prompt:{prompt_name}`

### What Server Does NOT Do

- Does NOT collect metrics (Collector's job)
- Does NOT create partitions (Collector's job)
- Does NOT run schema migrations (Collector's job)
- Does NOT garbage collect expired partitions (Collector's job)
- Does NOT provide user interface (Client's job)

### Configuration

**Configuration File**: `server.conf` or `ai-workbench.conf`

**Server-Specific Options**:
- port: HTTP/HTTPS listening port (default: 8080)
- tls: Enable HTTPS mode
- tls_cert: Path to TLS certificate
- tls_key: Path to TLS key
- tls_chain: Path to TLS certificate chain

**Shared Options**:
- pg_*: Datastore connection parameters
- server_secret: Encryption key material

### Startup Sequence

1. Load configuration from file and command-line
2. Connect to PostgreSQL datastore
3. Seed MCP privilege identifiers (privileges.SeedMCPPrivileges())
4. Create MCP handler
5. Start HTTP/HTTPS server with /sse and /health endpoints
6. Enter event loop (handle requests)

### Shutdown Sequence

1. Receive SIGINT or SIGTERM
2. Stop accepting new connections
3. Wait for in-flight requests to complete (graceful shutdown)
4. Close datastore connection pool
5. Exit cleanly

### Error Handling

**Protocol Errors**:
- Return JSON-RPC error response per MCP spec
- Log error details
- Continue serving requests

**Authentication Failures**:
- Return "Authentication required" error
- Log attempt
- Don't leak user existence

**Authorization Failures**:
- Return "Access denied" error
- Log attempt with user and resource
- Don't leak privilege information

**Database Errors**:
- Return internal error to client
- Log full error details server-side
- Don't expose schema details to client

### Testing Responsibilities

**Unit Tests**: `/server/src/*_test.go`

**Integration Tests**: `/server/src/integration/*_test.go`

**Test Coverage**:
- MCP protocol handling
- Authentication/authorization
- Tool implementations
- Resource implementations
- User/group management
- Privilege checking

**Test Database**: Temporary database with timestamp

---

## CLI Component

**Location**: `/cli`

**Binary**: `ai-workbench-cli`

**Purpose**: Command-line interface for testing MCP server and administrative
operations.

### Core Responsibilities

#### 1. Administrative Operations

**What**: Provide command-line tools for operations not suitable for MCP API.

**Operations**:
- Create initial user account
- Create service tokens
- Test MCP server connectivity

**Why CLI vs. MCP**:
- Bootstrap problem: Can't create first user via MCP
- Service tokens shouldn't be created via web UI
- Direct database access for emergency admin

#### 2. MCP Testing

**What**: Enable testing of MCP server tools, resources, and prompts.

**Capabilities**:
- List available tools
- Call tools with parameters
- List available resources
- Read resources
- List available prompts
- Execute prompts

**Use Cases**:
- Development testing
- Integration test authoring
- Troubleshooting MCP server

#### 3. User Account Creation

**What**: Create initial user account for bootstrapping system.

**Command**:
```bash
ai-workbench-cli create-user --username admin --email admin@example.com
```

**Process**:
1. Connect directly to datastore
2. Prompt for full name
3. Prompt for password (twice for confirmation)
4. Prompt for superuser status
5. Hash password (SHA-256)
6. Insert into user_accounts table
7. Display success message

**Security**:
- Password not echoed to terminal
- Password not logged
- Direct database access (bypass MCP auth)

#### 4. Service Token Creation

**What**: Create service tokens for automation.

**Command**:
```bash
ai-workbench-cli create-service-token --name backup_automation
```

**Process**:
1. Connect directly to datastore
2. Prompt for note
3. Prompt for superuser status
4. Prompt for expiry (optional)
5. Generate cryptographically random token
6. Hash token (SHA-256)
7. Insert into service_tokens table
8. Display plaintext token ONCE

**Security**:
- Token displayed once, never stored in plaintext
- User must save token immediately
- No token recovery (only regeneration)

### What CLI Does NOT Do

- Does NOT collect metrics (Collector's job)
- Does NOT serve MCP protocol (Server's job)
- Does NOT provide GUI (Client's job)
- Does NOT run continuously (one-shot commands)

### Configuration

**Configuration File**: `ai-workbench.conf` (shared)

**CLI-Specific Options**:
- None (uses datastore connection parameters)

**Command-Line Only**:
- Connection parameters for MCP server testing

### Testing Responsibilities

**Unit Tests**: `/cli/tests` or `/cli/src/*_test.go`

**Test Coverage**:
- Command parsing
- MCP client implementation
- User creation logic
- Token generation logic

**Test Database**: Temporary database with timestamp

---

## Client Component (Future)

**Location**: `/client`

**Technology**: React, Node.js, MUI

**Purpose**: Web-based user interface for interacting with AI DBA Workbench.

### Planned Responsibilities

#### 1. User Interface

**What**: Provide graphical interface for all MCP operations.

**Features**:
- Connection management
- Metrics visualization
- Query builder
- User/group administration
- Token management

#### 2. AI Integration

**What**: Integrate with Claude API or Ollama for natural language
interaction.

**How**:
- Connect to MCP server as MCP client
- Send user queries to LLM
- LLM uses MCP tools to gather information
- Display AI responses and results

#### 3. Authentication

**What**: Handle user login and session management.

**How**:
- Login form calls authenticate_user tool
- Store session token in browser
- Include token in all MCP requests
- Logout calls logout tool

### What Client Does NOT Do

- Does NOT collect metrics (Collector's job)
- Does NOT implement business logic (Server's job)
- Does NOT store data (PostgreSQL's job)
- Does NOT create users (CLI's job for bootstrap)

### Status

**Current**: Placeholder directory, not implemented

**Future**: React application consuming MCP server

---

## Integration Tests

**Location**: `/tests`

**Purpose**: End-to-end testing across all components.

### Responsibilities

#### 1. Multi-Component Workflows

**What**: Test scenarios involving Collector, Server, and CLI together.

**Examples**:
- Collector collects metrics -> Server retrieves via MCP
- CLI creates user -> Server authenticates user
- Collector creates partitions -> Server queries historical data

#### 2. Real Database Testing

**What**: Test against real PostgreSQL database, not mocks.

**How**:
- Create temporary database
- Run Collector to set up schema
- Start Server
- Execute CLI commands
- Verify results via SQL queries
- Clean up temporary database

#### 3. Security Testing

**What**: Verify authentication and authorization work across components.

**Examples**:
- Non-superuser cannot create shared connection
- User cannot access other user's private connection
- Token scoping correctly restricts access
- Expired tokens are rejected

### Test Database Management

**Naming**: `ai_workbench_test_YYYYMMDD_HHMMSS`

**Lifecycle**:
1. Create database via PostgreSQL connection
2. Run Collector to apply migrations
3. Execute tests
4. Drop database (unless TEST_AI_WORKBENCH_KEEP_DB=1)

**Environment Variables**:
- TEST_AI_WORKBENCH_SERVER: Custom PostgreSQL connection
- TEST_AI_WORKBENCH_KEEP_DB: Keep database for debugging
- SKIP_INTEGRATION_TESTS: Skip integration tests

---

## Communication Between Components

### Collector <-> Datastore

**Protocol**: PostgreSQL wire protocol (pgx/v5)

**Direction**: Bidirectional

**Collector Actions**:
- Read: probe_configs, connections, monitored connection parameters
- Write: metrics.* tables, schema_version

**Access Pattern**: Connection pool

### Server <-> Datastore

**Protocol**: PostgreSQL wire protocol (pgx/v5)

**Direction**: Bidirectional

**Server Actions**:
- Read: All tables (authentication, metrics, config)
- Write: user_accounts, user_sessions, user_tokens, service_tokens,
  connections, user_groups, group_memberships, privileges tables

**Access Pattern**: Connection pool

### CLI <-> Datastore

**Protocol**: PostgreSQL wire protocol (pgx/v5)

**Direction**: Bidirectional

**CLI Actions**:
- Read: Validation queries
- Write: user_accounts, service_tokens (bootstrap only)

**Access Pattern**: Single connection (one-shot commands)

### CLI <-> Server

**Protocol**: HTTP/HTTPS with MCP over SSE

**Direction**: CLI -> Server (request/response)

**CLI Actions**:
- Call MCP tools
- Read MCP resources
- Execute MCP prompts

**Access Pattern**: One-shot HTTP requests

### Client <-> Server (Future)

**Protocol**: HTTP/HTTPS with MCP over SSE

**Direction**: Bidirectional (SSE)

**Client Actions**:
- Authenticate user
- Call MCP tools
- Read MCP resources
- Stream responses

**Access Pattern**: Persistent SSE connection

### Collector <-> Monitored PostgreSQL

**Protocol**: PostgreSQL wire protocol (pgx/v5)

**Direction**: Collector -> Monitored (mostly read-only)

**Collector Actions**:
- Execute probe queries (read-only)
- Discover databases (read-only)

**Access Pattern**: Connection pools (one per monitored connection)

---

## Boundary Rules

### Data Ownership

**Rule**: Each component owns specific data and is authoritative for that data.

**Ownership**:
- Collector owns: Metrics data (metrics.* tables)
- Server owns: Authentication state (user_sessions)
- CLI owns: Bootstrap operations
- Datastore owns: All persistent state

**Violations**:
- Server should NOT create partitions (Collector's job)
- Collector should NOT handle authentication (Server's job)
- CLI should NOT serve MCP protocol (Server's job)

### Functionality Boundaries

**Rule**: Components should not duplicate functionality.

**Good**:
- Collector collects, Server retrieves
- Server authenticates, CLI uses authentication
- Datastore stores, components query

**Bad**:
- Server collecting metrics (duplicates Collector)
- Collector handling MCP requests (duplicates Server)
- Multiple components managing schema migrations

### Configuration Sharing

**Rule**: Configuration file is shared, but components only use relevant
options.

**Shared Config**:
- Datastore connection parameters (pg_*)
- server_secret

**Component-Specific**:
- Collector: (none currently)
- Server: port, tls, tls_cert, tls_key, tls_chain
- CLI: (none currently)

**How to Add Config**:
1. Add field to Config struct
2. Add command-line flag if appropriate
3. Add configuration file parser
4. Document in README and sample config

### Database Schema Evolution

**Rule**: Only Collector runs schema migrations.

**Rationale**:
- Single source of truth for schema version
- Prevents race conditions (multiple components migrating)
- Collector starts first in deployment

**Other Components**:
- Server assumes schema is current
- CLI assumes schema is current
- If schema is wrong, fail fast with error message

---

## Deployment Patterns

### Single Server Deployment

**Components**:
- 1 Collector instance
- 1 Server instance
- PostgreSQL datastore (can be same as monitored)
- CLI on-demand

**Process**:
1. Start PostgreSQL
2. Configure ai-workbench.conf
3. Start Collector (runs migrations)
4. Start Server
5. Use CLI to create initial user
6. Access via MCP client

### Distributed Deployment

**Components**:
- 1 Collector instance (future: multiple for HA)
- Multiple Server instances (behind load balancer)
- Shared PostgreSQL datastore
- CLI on admin workstation

**Benefits**:
- Server can scale horizontally
- Collector can run closer to monitored databases
- PostgreSQL datastore can be dedicated

### High Availability (Future)

**Components**:
- Multiple Collector instances with leader election
- Multiple Server instances behind load balancer
- PostgreSQL HA cluster (replication or Spock)

**Design Considerations**:
- Collector leader election (not yet implemented)
- Probe assignment across collectors
- Shared metrics storage (already supported)

---

## Component Interaction Examples

### Example 1: User Authenticates and Queries Metrics

**Flow**:
1. **Client** -> **Server**: POST /sse with authenticate_user tool
2. **Server** queries **Datastore**: SELECT from user_accounts
3. **Server** validates password hash
4. **Server** writes **Datastore**: INSERT into user_sessions
5. **Server** -> **Client**: Return session token
6. **Client** -> **Server**: POST /sse with read_metrics resource
7. **Server** validates token against **Datastore**
8. **Server** checks privileges via **Datastore**
9. **Server** queries **Datastore**: SELECT from metrics.pg_stat_database
10. **Server** -> **Client**: Return metrics data

### Example 2: Collector Collects Metrics

**Flow**:
1. **Collector** scheduler triggers probe execution
2. **Collector** queries **Datastore**: SELECT from probe_configs,
   connections
3. **Collector** decrypts monitored connection password
4. **Collector** -> **Monitored PostgreSQL**: Execute probe query
5. **Monitored PostgreSQL** -> **Collector**: Return metrics rows
6. **Collector** ensures partition exists in **Datastore**
7. **Collector** -> **Datastore**: COPY metrics into partition
8. **Collector** schedules next execution

### Example 3: Admin Creates User via CLI

**Flow**:
1. Admin runs: `ai-workbench-cli create-user --username alice`
2. **CLI** prompts for password, email, full name
3. **CLI** hashes password (SHA-256)
4. **CLI** connects to **Datastore**
5. **CLI** -> **Datastore**: INSERT into user_accounts
6. **CLI** displays success message
7. Alice can now authenticate via **Server**

---

**Version**: 1.0
**Last Updated**: 2025-11-08
**Status**: Living Document
