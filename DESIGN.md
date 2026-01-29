# pgEdge AI DBA Workbench Design

> This document provides architectural context for Claude Code when working on
> this project. It supplements the instructions in CLAUDE.md.

pgEdge AI DBA Workbench is a monitoring and management solution for pgEdge
Enterprise Postgres, supporting single nodes, single nodes with read replicas
(using PostgreSQL's binary replication), or multi-master distributed clusters
using the Spock replication engine, optionally with read replicas on the Spock
nodes.

The functionality we can provide without integrated monitoring is severely
limited in nature; it can only operate on data obtained in realtime from the
managed systems, which prevents us from understanding usage and load patterns
which are often critical in diagnosing imminent problems, or diagnosing
problems that occurred in the past, for example overnight.

It is therefore critical that any system we build should have the ability to
collect, store, retrieve, and age out metrics from the servers we are managing.

## Components

The workbench consists of five components:

- **Collector** (`/collector`) - Data collection service that monitors
  PostgreSQL servers and stores metrics in a PostgreSQL datastore.

- **Server** (`/server`) - MCP protocol server providing tools, resources, and
  prompts for PostgreSQL interaction via HTTP/HTTPS or stdio.

- **CLI** (`/cli`) - Interactive command-line interface for interacting with
  the MCP server, supporting multiple LLM providers.

- **Alerter** (`/alerter`) - Background service for threshold-based and
  anomaly-detection alerting on collected metrics.

- **Client** (`/client`) - Web-based UI (React/TypeScript) for cluster
  navigation and monitoring.

Supporting infrastructure includes:

- **Shared packages** (`/pkg`) - Logging and embedding utilities.

- **Documentation** (`/docs`) - Comprehensive Markdown documentation.

- **Examples** (`/examples`) - Sample configurations.

## Technology

All server-side components are written in Go. This allows us to build each
component into a single binary that can easily be packaged and distributed
without runtime dependencies. Go also offers memory management and a language
design that lends itself to secure and less bug prone code.

The web client is written in React with Vite as the build tool, and uses the
MUI library for a professional, clean design. It connects to the MCP server
for cluster topology data and connection management, and will connect to
either Anthropic's Claude or Ollama APIs (using the server's proxy) to
transform textual user requests into responses based on information retrieved
from the MCP server.

## Key Architecture

The following sections describe key architectural points of the design.

### Configuration

Each component reads configuration from a YAML file. A command line option
provides the path to the configuration file. If not provided, the binary looks
for the configuration file in the directory where the binary is stored, or in
`/etc/pgedge/`.

Configuration priority (highest to lowest):

1. Command line flags
2. Environment variables
3. Configuration file
4. Hardcoded defaults

#### Datastore Options

The collector and server connect to a PostgreSQL datastore using these options:

- `pg_host` - The hostname or IP address of the PostgreSQL server.
- `pg_hostaddr` - The IP address (to avoid DNS lookups).
- `pg_database` - The name of the database to use.
- `pg_username` - The username for the connection.
- `pg_password_file` - Path to a file containing the password.
- `pg_port` - The port on which PostgreSQL is listening.
- `pg_sslmode` - The SSL mode for the connection.
- `pg_sslcert` - Path to the client SSL certificate.
- `pg_sslkey` - Path to the client SSL key.
- `pg_sslrootcert` - Path to the root SSL certificate.

#### Server Options

- `tls` - Enable HTTPS mode.
- `tls_cert` - Path to the TLS certificate.
- `tls_key` - Path to the TLS key.
- `tls_chain` - Path to the certificate chain.
- `server_secret` - Per-installation string used in encryption keys
  (configuration file only).
- `auth_db_path` - Path to the SQLite authentication database.

### Authentication

Authentication uses bearer tokens stored in a SQLite database separate from
the PostgreSQL datastore. This separation allows the server to authenticate
requests before establishing database connections.

#### User Accounts

User accounts are stored in SQLite with the following attributes:

- Username
- Email address
- Full name
- Hashed password (bcrypt)
- Password expiry timestamp
- Superuser status

Command line options allow creating initial accounts:

```bash
ai-dba-server --create-user <username>
```

#### Tokens

Two types of tokens exist:

**User Tokens**: Issued upon successful login via the `/api/v1/auth/login`
endpoint. Each user may have multiple tokens. Tokens are deleted upon logout
or after 24 hours by default.

**Service Tokens**: Not associated with user accounts. Include a name, expiry
timestamp, superuser status, and optional note describing the token's purpose.
Created via command line:

```bash
ai-dba-server --create-token <name>
```

#### Groups and Privileges

User groups enable permissions management. Superusers define a hierarchy of
groups. Users and groups can be members of other groups, inheriting privileges
from all groups in which they have direct or indirect membership.

Two types of privileges exist:

- **Connection Privileges**: A group may be assigned read or read/write access
  to shared connections. Connections with no groups assigned are accessible to
  all users. Connections with groups assigned are only accessible to members
  of those groups.

- **MCP Privileges**: Each MCP resource, tool, and prompt (except login) has
  an identifier that can be assigned to a group. Users must be direct or
  indirect members of a group with that identifier to access it.

Superusers have unrestricted access regardless of group membership.

#### Token Scope

Both service tokens and user tokens can be limited in scope to any subset of
connections or MCP items that the owning user can access. Tokens without
explicit scope restrictions inherit full access from their owner.

### Session Management

The server maintains per-token isolation:

- `ClientManager` maps token hashes to database clients.
- Each token gets its own database connection and metadata cache.
- Connections are created lazily on first use.
- Rate limiting applies per IP address and per token.

### Monitored Server Connections

MCP tools allow creating connections to monitored servers. Connections may be
shared (accessible to groups) or private to the creating user or service
token. Only superusers can create shared connections.

Each connection uses the same options as the datastore connection, except
`pg_password_file` is replaced with `pg_password`. Passwords are encrypted
using a combination of the server secret and token name.

The database name serves as a maintenance database (similar to pgAdmin). The
server uses it for the initial connection, then discovers additional
accessible databases using the connection parameters.

Each connection has:

- Unique numeric identifier (primary key)
- Flag indicating whether to actively monitor
- Shared/private status
- Owner (user or service token)

### Collector

The collector monitors PostgreSQL servers and stores metrics in the datastore.

#### Probe System

A probe consists of a SQL query returning metrics, along with identifiers for
the PostgreSQL server, database name, and any other required identifiers such
as table names.

The `MetricsProbe` interface defines:

- `GetName()`, `GetTableName()`, `GetQuery()` - Probe metadata
- `Execute()` - Run the probe query on a monitored connection
- `Store()` - Write metrics using the COPY protocol
- `EnsurePartition()` - Create weekly partitions
- `GetConfig()` - Return probe configuration
- `IsDatabaseScoped()` - Whether probe runs per-database

`BaseMetricsProbe` provides common functionality for all probes.

#### Available Probes

The collector includes 46 probes covering:

**System-level**: OS info, CPU info, memory info, disk info, load average,
process info, network info.

**PostgreSQL statistics**: pg_stat_activity, pg_stat_database,
pg_stat_statements, pg_stat_replication, pg_stat_bgwriter, pg_stat_wal,
pg_stat_archiver, pg_stat_user_tables, pg_stat_user_indexes, and more.

**Configuration**: pg_settings, pg_hba_file_rules, pg_ident_file_mappings.

**Replication**: pg_node_role (cluster topology detection), replication
slot status, subscription status.

#### Probe Configuration

Users configure the collection interval and retention period for each probe.
Data is stored in per-probe tables partitioned by week. A garbage collector
thread runs daily to drop partitions containing only expired data.

#### Scheduler

The `ProbeScheduler` coordinates probe execution:

- Manages timing for each probe based on configured intervals
- Runs probes in separate goroutines to avoid blocking
- Handles connection pooling for monitored servers
- Creates weekly partitions automatically

#### Cluster Topology Detection

The `pg_node_role` probe automatically detects the replication topology of
monitored PostgreSQL servers. It queries system catalogs and extension tables
to determine each server's role in the cluster architecture.

The probe detects the following node roles:

- **standalone** - Single server with no replication
- **binary_primary** - Primary server with streaming standbys
- **binary_standby** - Standby server receiving binary replication
- **binary_cascading** - Cascading standby (standby with its own standbys)
- **logical_publisher** - Server publishing logical replication changes
- **logical_subscriber** - Server subscribing to logical replication
- **logical_bidirectional** - Server with both publications and subscriptions
- **spock_node** - Node in a pgEdge Spock multi-master cluster
- **bdr_node** - Node in a BDR cluster

The probe collects additional topology metadata:

- Upstream host and port for standbys
- Publisher host and port for logical subscribers
- Standby count for primary servers
- Spock node name and subscription count
- Replication lag (received vs replayed LSN)

This topology information enables the server and web client to display
hierarchical views of cluster relationships, showing primary servers with
their standbys nested beneath them.

### Server

The MCP server provides tools, resources, and prompts for PostgreSQL
interaction. It exposes three types of interfaces: MCP tools for LLMs, HTTP
APIs for client applications, and CLI commands for server administration.

#### MCP Protocol

- Protocol version: `2024-11-05`
- Server name: `pgedge-postgres-mcp`
- Server version: `1.0.0-alpha1`
- Transport modes: stdio and HTTP/HTTPS (SSE)

#### MCP Tools

MCP tools are advertised to and callable by LLMs. The server provides tools
for database interaction:

**Query Execution**:

- `query_database` - Execute arbitrary SQL queries
- `execute_explain` - Run EXPLAIN on queries
- `count_rows` - Count rows in tables

**Probe Management**:

- `list_probes` - List available probes and their status
- `describe_probe` - Get detailed probe metadata

**Schema Information**:

- `get_schema_info` - Introspect database schemas
- `show_table` - Display table structure
- `show_index` - Display index information

**Connection Discovery**:

- `list_connections` - List available database connections

**Resources**:

- `read_resource` - Read MCP resources
- `list_resources` - List available resources

#### HTTP APIs

HTTP APIs serve client applications (CLI, web client). These are not MCP tools
and are not accessible to LLMs. All REST API endpoints are versioned with the
`/api/v1/` prefix.

The API implements RFC 8631 for API discovery. All JSON responses include a
Link header pointing to the OpenAPI specification at `/api/v1/openapi.json`.

**Authentication**:

- `POST /api/v1/auth/login` - Authenticate user and obtain session token

**Connection Management**:

- `GET /api/v1/connections` - List all connections
- `GET /api/v1/connections/{id}/databases` - List databases for a connection
- `GET /api/v1/connections/current` - Get current connection
- `POST /api/v1/connections/current` - Set current connection
- `DELETE /api/v1/connections/current` - Clear current connection

**Cluster Topology**:

- `GET /api/v1/clusters` - Get auto-detected cluster topology hierarchy
- `GET /api/v1/cluster-groups` - List cluster groups
- `POST /api/v1/cluster-groups` - Create a cluster group
- `GET /api/v1/cluster-groups/{id}/clusters` - List clusters in a group
- `POST /api/v1/cluster-groups/{id}/clusters` - Create a cluster in a group
- `GET /api/v1/clusters/{id}/servers` - List servers in a cluster

**User Information**:

- `GET /api/v1/user/info` - Get authenticated user information

**LLM Proxy**:

- `GET /api/v1/llm/providers` - List available LLM providers
- `GET /api/v1/llm/models` - List models for a provider
- `POST /api/v1/llm/chat` - Proxy chat requests to LLM providers

**Conversation History**:

- Endpoints for storing and retrieving conversation history

#### CLI Commands

Server administration uses CLI commands executed before the server starts.
These manage users, groups, privileges, and tokens.

**User Management**:

- `-add-user`, `-update-user`, `-delete-user`, `-list-users`
- `-enable-user`, `-disable-user`
- `-set-superuser`, `-unset-superuser`

**Group Management**:

- `-add-group`, `-delete-group`, `-list-groups`
- `-add-member`, `-remove-member`

**Privilege Management**:

- `-grant-privilege`, `-revoke-privilege`
- `-grant-connection`, `-revoke-connection`
- `-list-privileges`, `-show-group-privileges`
- `-register-privilege`

**Token Management**:

- `-add-token`, `-remove-token`, `-list-tokens`
- `-scope-token-connections`, `-scope-token-tools`, `-clear-token-scope`

#### MCP Resources

Resources return data from probes for given time periods and key information
(server ID, database name, etc.). Additional resources return realtime
snapshots from monitored servers.

### CLI

The CLI provides an interactive interface for working with the MCP server.

#### Features

- Interactive REPL with readline support
- Multiple LLM providers: Anthropic, OpenAI, Ollama
- Conversation history management
- User preferences persistence
- Connection selection (database context)
- Tool, resource, and prompt listing
- Platform-specific UI enhancements (macOS, Linux, Windows)

#### Configuration

- YAML configuration file
- CLI flags override configuration
- MCP server URL and credentials
- LLM provider and model selection

### Web Client

The web client provides a browser-based interface for cluster monitoring and
management.

#### Features

- User authentication with session token management
- Light and dark theme support with preference persistence
- Cluster navigation panel with hierarchical group/cluster/server display
- Server selection for context-based operations
- Responsive design using Material-UI components

#### Cluster Navigator

The cluster navigator displays monitored servers in a hierarchical tree:

- **Groups** - Top-level organizational containers
- **Clusters** - Named clusters containing related servers
- **Servers** - Individual PostgreSQL instances with status indicators

The navigator automatically detects and displays cluster types based on server
roles:

- **Binary replication clusters** - Primary with nested standby servers
- **Logical replication clusters** - Publishers and subscribers
- **Spock clusters** - Multi-master nodes with amber visual styling
- **Standalone servers** - Single servers without replication

Each server displays:

- Connection status indicator (online, warning, offline)
- Role badge (Primary, Standby, Cascade, Spock, Publisher, Subscriber)
- Tree lines showing parent-child relationships for cascading standbys

Cluster containers use color-coded borders to indicate replication type:

- Cyan for binary replication
- Amber for Spock multi-master
- Purple for logical replication
- Gray for standalone servers

#### Architecture

The client uses React contexts for state management:

- `AuthContext` - Authentication state and session token
- `ClusterContext` - Cluster hierarchy data and server selection

The client fetches cluster topology from the `/api/v1/clusters` endpoint and
falls back to the `/api/v1/connections` endpoint with client-side
transformation if the cluster API is unavailable.

### Alerter

The alerter provides background monitoring and notification.

#### Components

- **Threshold Evaluator** - Compares metrics against configured thresholds
- **Baseline Calculator** - Establishes normal patterns for metrics
- **Anomaly Detector** - AI-powered deviation detection
- **Blackout Scheduler** - Suppresses alerts during maintenance windows
- **Alert Cleaner** - Auto-resolves cleared alerts
- **Retention Manager** - Cleans old alert data

#### Configuration

- YAML-based threshold definitions
- CRON expressions for scheduling
- Anomaly detection sensitivity settings
- Alert retention policies

### Additional Functionality

If available, the `system_stats` and `pg_stat_statements` extensions are used
as additional data sources for probes and realtime data.

If Spock is installed, its status tables provide additional data sources for
probes and realtime data.

### Database Schema

#### Datastore (PostgreSQL)

```
schema_version       - Migration tracking
connections          - Monitored PostgreSQL servers
probe_configs        - Probe settings (interval, retention, enabled)
metrics.*            - One table per probe, partitioned by week
```

#### Auth Database (SQLite)

```
users                - User accounts with password hashes
tokens               - Bearer tokens (user and service)
groups               - User groups for RBAC
group_members        - Group membership
privileges           - MCP tool/resource privileges
token_scopes         - Token scope restrictions
connection_privileges - Connection access per group
```

#### Design Rationale

The server stores authentication data in SQLite rather than
in the PostgreSQL datastore for three reasons.

**Privilege separation.** The SQLite database is a local file
that only the server process can access. The PostgreSQL
datastore is shared by the collector, alerter, and server.
Storing auth data in SQLite prevents a compromised collector
or alerter from reading user credentials, token hashes, or
RBAC rules.

**No network dependency.** Authentication checks run on every
API request through middleware. SQLite serves token validation
and RBAC lookups locally without network round-trips. Moving
auth to PostgreSQL would make every request depend on network
connectivity to the remote datastore. A network partition or
datastore outage would lock out all users.

**Failure isolation.** Datastore failures such as a full disk
from metrics, a crash, or corruption do not affect
authentication. The auth database operates in an independent
failure domain.

**Trade-offs.** This separation prevents foreign-key
relationships between auth tables and the datastore
`connections` table. Operations that span both databases
require application-level coordination rather than database
transactions. The application uses connection IDs as shared
identifiers to bridge the two databases.

### Testing

Each sub-project contains a test suite with unit and regression tests. Unit
tests exercise individual functions; regression tests verify built code
functionality. Tests run under `go test` or `npm test` as appropriate.

A `/tests` directory at the top level provides end-to-end integration testing.
The collector and server start against a temporary test database, and the
system is exercised using both the CLI and (eventually) the web client.

Tests requiring a database create a temporary database with a timestamp in the
name. The default PostgreSQL connection parameters apply unless overridden:

```bash
TEST_AI_WORKBENCH_SERVER=postgres://user:password@hostname/postgres
```

Setting `TEST_AI_WORKBENCH_KEEP_DB=1` preserves the test database after tests
complete. Otherwise, the test database is automatically dropped.

### Makefiles

Each sub-project includes a Makefile with these targets:

- `all` - Compile/build the code (default target)
- `test` - Run the sub-project's test suite
- `coverage` - Run coverage checks
- `lint` - Run linter tests
- `test-all` - Run tests, coverage, and linting
- `clean` - Remove build artifacts
- `killall` - Kill running instances that may interfere with tests

The top-level Makefile calls targets in sub-projects and implements:

- `all` - Call `all` for all sub-projects
- `test` - Call `test` for all sub-projects
- `coverage` - Call `coverage` for all sub-projects
- `lint` - Call `lint` for all sub-projects
- `test-integration` - Run the integration test suite
- `test-all` - Run all sub-project `test-all` targets, then integration tests
- `clean` - Call `clean` for all sub-projects
- `killall` - Call `killall` for all sub-projects

### Future Enhancements

- AI-powered chat interface in the web client
- Support for multiple collectors (high availability and load balancing)
- Enhanced anomaly detection algorithms
- Additional MCP tools for database administration
