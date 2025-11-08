# Architecture Decisions: pgEdge AI Workbench

This document records major architectural decisions and their rationales.

## Component Architecture

### Decision: Three-Component Separation

**Decision**: Separate the system into three independent components: Collector,
Server, and CLI (plus future Client).

**Rationale**:
1. **Independent Scaling**: Collector can run on different infrastructure than
   Server
2. **Deployment Flexibility**: Can deploy collector-only for monitoring,
   server-only for querying historical data
3. **Testing Isolation**: Each component tests independently
4. **Future High Availability**: Multiple collectors possible without
   affecting server
5. **Clear Responsibilities**: Each component has a single, well-defined
   purpose

**Alternatives Considered**:
- Monolithic binary with flags: Rejected due to tight coupling and complex
  testing
- Separate binaries sharing libraries: Rejected to maintain true independence

**Trade-offs**:
- **Cost**: More complex deployment (multiple processes to manage)
- **Benefit**: Superior flexibility and maintainability

**Status**: IMPLEMENTED - Core architecture

---

## Data Storage

### Decision: PostgreSQL as Primary Datastore

**Decision**: Use PostgreSQL for all persistent storage (configuration,
metrics, user accounts, session state).

**Rationale**:
1. **Product Domain Alignment**: Monitoring PostgreSQL with PostgreSQL
2. **ACID Guarantees**: Configuration changes and user management require
   transactions
3. **Rich Data Types**: JSON support for flexible metrics storage
4. **Partitioning**: Native support for time-series partitioning
5. **Backup/Recovery**: Standard PostgreSQL tools apply
6. **Query Capabilities**: SQL for complex analytics and reporting

**Alternatives Considered**:
- Time-series databases (InfluxDB, TimescaleDB): Rejected to avoid additional
  dependencies
- File-based storage: Rejected due to lack of ACID and query capabilities
- SQLite: Rejected due to lack of partitioning and network access

**Trade-offs**:
- **Cost**: Requires PostgreSQL availability
- **Benefit**: Leverage PostgreSQL strengths for PostgreSQL monitoring

**Status**: IMPLEMENTED - Migration 1

---

### Decision: Weekly Partitioning for Metrics

**Decision**: Partition all metrics tables by week, with Monday as the start
of the week.

**Rationale**:
1. **Balance**: Weekly partitions balance partition count vs. partition size
2. **Reporting Alignment**: Weeks are common reporting periods
3. **Retention Efficiency**: Drop entire weeks of data at once
4. **Query Performance**: Partition pruning for time-range queries

**Alternatives Considered**:
- Daily partitioning: Rejected due to excessive partition count
- Monthly partitioning: Rejected due to large partition size and inflexible
  retention
- No partitioning: Rejected due to vacuum and retention inefficiency

**Implementation Details**:
- Partitions named: `{table_name}_YYYYMMDD` (start of week)
- Partition range: Monday 00:00:00 to following Monday 00:00:00
- Automatic partition creation on first write to new week

**Trade-offs**:
- **Cost**: Retention days are approximate (rounded to week boundaries)
- **Benefit**: Efficient query and storage management

**Status**: IMPLEMENTED - BaseMetricsProbe.EnsurePartition()

---

### Decision: Change-Tracked Probe Retention

**Decision**: For configuration probes (pg_settings, pg_hba_file_rules,
pg_ident_file_mappings), never drop the partition containing the most recent
data for each connection.

**Rationale**:
1. **Point-in-Time Recovery**: Enable "what was the configuration" queries
2. **Compliance**: Configuration changes must be auditable
3. **Security**: Track security-relevant configuration over time
4. **Change Detection**: Support "what changed" analysis

**Implementation**:
- Garbage collector identifies most recent partition per connection
- Protected partitions excluded from retention policy
- Only applies to specific probe types

**Trade-offs**:
- **Cost**: Storage grows indefinitely for active connections
- **Benefit**: Complete configuration history preserved

**Status**: IMPLEMENTED - DropExpiredPartitions()

---

## Authentication & Authorization

### Decision: Token-Based Authentication

**Decision**: Use bearer tokens for all API authentication rather than
sessions, cookies, or basic auth.

**Rationale**:
1. **Stateless**: Tokens enable stateless authentication for MCP API
2. **Multiple Clients**: Users can have multiple active tokens
3. **Scoping**: Tokens can be scoped to subset of owner's privileges
4. **Programmatic Access**: Natural fit for CLI and automation
5. **Revocation**: Individual token revocation without affecting other
   sessions

**Token Types**:
1. **Session Tokens** (user_sessions): 24-hour interactive sessions
2. **User Tokens** (user_tokens): Long-lived API credentials owned by users
3. **Service Tokens** (service_tokens): Standalone automation credentials

**Alternatives Considered**:
- HTTP Basic Auth: Rejected due to password transmission on every request
- Session cookies: Rejected as not suitable for MCP protocol
- OAuth 2.0: Rejected as overly complex for this use case

**Trade-offs**:
- **Cost**: Token management complexity
- **Benefit**: Flexibility for diverse access patterns

**Status**: IMPLEMENTED - Migrations 1, 2, 5

---

### Decision: Role-Based Access Control (RBAC) with Groups

**Decision**: Implement hierarchical group-based RBAC rather than direct
user-to-resource permissions.

**Rationale**:
1. **Scalability**: Grant permissions to groups, not individual users
2. **Organizational Modeling**: Groups model real organizational structures
3. **Privilege Inheritance**: Nested groups inherit parent privileges
4. **Administrative Efficiency**: Modify group membership instead of
   re-granting permissions
5. **Audit Trail**: Clear visibility into who has access through which group

**Design**:
- Groups contain users and/or other groups
- Privileges granted to groups, never directly to users
- Recursive CTEs resolve transitive membership
- Two privilege types: connection access and MCP item access

**Alternatives Considered**:
- Direct user-to-resource permissions: Rejected due to administrative overhead
- Flat groups (no nesting): Rejected as doesn't model real organizations
- Role-based without groups: Rejected as less flexible

**Trade-offs**:
- **Cost**: CTE performance for deeply nested groups
- **Benefit**: Natural organizational modeling and efficient administration

**Status**: IMPLEMENTED - Migration 6

---

### Decision: MCP Privilege Identifiers

**Decision**: Register every MCP tool, resource, and prompt with a unique
string identifier that must be granted to access.

**Rationale**:
1. **Explicit Enumeration**: All privileged operations explicitly listed
2. **Least Privilege**: Can grant access to specific tools, not all-or-nothing
3. **Audit Trail**: Clear record of what each group can access
4. **Token Scoping**: API tokens can be limited to specific operations
5. **Default Deny**: Unregistered identifiers automatically denied

**Implementation**:
- Server startup seeds all known identifiers
- Groups granted access via group_mcp_privileges table
- Tokens optionally scoped via token_mcp_scope table

**Alternatives Considered**:
- Implicit all-access after login: Rejected as insufficiently granular
- Capability-based security: Rejected as overly complex
- Resource-based only: Rejected as doesn't cover tool/prompt access

**Trade-offs**:
- **Cost**: Maintenance overhead to register new tools/resources/prompts
- **Benefit**: Fine-grained access control with clear audit trail

**Status**: IMPLEMENTED - Migration 6, privileges.Seed()

---

### Decision: Superuser Bypass

**Decision**: Users and tokens with is_superuser=true bypass all privilege
checks.

**Rationale**:
1. **Bootstrap**: Initial setup requires unrestricted access
2. **Emergency Access**: Break-glass scenario for locked-out administrators
3. **Simplicity**: Eliminates special-case privilege grants for admins
4. **Compatibility**: Aligns with PostgreSQL's superuser concept

**Security Considerations**:
- Superuser status is explicit in database schema (auditable)
- Cannot be granted through groups (must be set on account/token)
- Should be granted sparingly (principle of least privilege)

**Trade-offs**:
- **Cost**: Potential for superuser abuse
- **Benefit**: Operational simplicity and emergency access

**Status**: IMPLEMENTED - All privilege checks

---

### Decision: Shared vs. Private Connections

**Decision**: Support both shared connections (accessible by multiple users)
and private connections (owned by single user/token).

**Rationale**:
1. **Multi-Tenancy**: Different users monitor different databases
2. **Security**: Prevent cross-user credential access
3. **Flexibility**: Shared connections for teams, private for individuals
4. **Privilege Control**: Only superusers create shared connections

**Access Rules**:
- Private connections accessible only by owner (or superusers)
- Shared connections with no groups: DENIED (fail-safe default)
- Shared connections with groups: accessible by group members

**Alternatives Considered**:
- Shared-only: Rejected as requires superuser for all connections
- Private-only: Rejected as doesn't support team collaboration

**Trade-offs**:
- **Cost**: More complex access control logic
- **Benefit**: Flexibility for diverse organizational needs

**Status**: IMPLEMENTED - Migration 1, privileges.CanAccessConnection()

---

### Decision: Token Scoping

**Decision**: Allow tokens to be scoped to subsets of owner's privileges
for both connections and MCP items.

**Rationale**:
1. **Least Privilege**: Create tokens with minimal necessary permissions
2. **Security**: Limit blast radius of compromised token
3. **Compliance**: Auditable separation of duties
4. **Flexibility**: Different tokens for different automation tasks

**Implementation**:
- Empty scope tables (no rows) = full owner access
- Rows in scope tables = restricted to listed items
- Separate tables for connection scope and MCP scope

**Alternatives Considered**:
- All-or-nothing tokens: Rejected as insufficiently secure
- Separate permission grants: Rejected as duplicate of group system

**Trade-offs**:
- **Cost**: Additional complexity in privilege checking
- **Benefit**: Precise control over token capabilities

**Status**: IMPLEMENTED - Migration 6, token_connection_scope,
token_mcp_scope

---

## Protocol & API Design

### Decision: Model Context Protocol (MCP)

**Decision**: Implement the MCP specification for AI assistant integration.

**Rationale**:
1. **Industry Standard**: Anthropic-backed protocol for AI-to-service
   communication
2. **Structured Abstractions**: Tools, resources, and prompts are natural
   fit for database operations
3. **Extensibility**: Easy to add new tools as capabilities expand
4. **Client Support**: Claude Desktop and other clients support MCP natively
5. **Future-Proof**: Active development and expanding ecosystem

**Alternatives Considered**:
- REST API: Rejected as less structured for AI consumption
- GraphQL: Rejected as overly complex for this use case
- Custom protocol: Rejected to avoid reinventing the wheel

**Trade-offs**:
- **Cost**: Tied to MCP specification evolution
- **Benefit**: Industry standard with broad client support

**Status**: IMPLEMENTED - server/src/mcp/

---

### Decision: JSON-RPC 2.0 with SSE Transport

**Decision**: Use JSON-RPC 2.0 message format over Server-Sent Events for
MCP transport.

**Rationale**:
1. **MCP Compliance**: Required by MCP specification
2. **Structured Messages**: JSON-RPC provides request/response/error
   structure
3. **Firewall Friendly**: SSE over HTTP/HTTPS works through corporate
   firewalls
4. **Simpler than WebSockets**: Easier to implement and debug
5. **TLS Support**: HTTPS provides encryption in production

**Implementation**:
- HTTP endpoint (/sse) for development and testing
- HTTPS endpoint (production) with configurable TLS certificates
- Health check endpoint (/health) for monitoring

**Alternatives Considered**:
- WebSockets: Rejected as more complex without clear benefit
- gRPC: Rejected as not part of MCP spec
- Plain HTTP: Rejected as doesn't support streaming responses

**Trade-offs**:
- **Cost**: SSE is uni-directional (server-to-client)
- **Benefit**: Simplicity and firewall compatibility

**Status**: IMPLEMENTED - server/src/server/server.go

---

### Decision: Probe-Based Metrics Collection

**Decision**: Implement metrics collection as independent "probes" with
configurable intervals and retention.

**Rationale**:
1. **Modularity**: Each metric type is self-contained
2. **Configurability**: Different metrics need different collection
   frequencies
3. **Retention Policies**: Different metrics have different retention needs
4. **Extensibility**: Adding new probes doesn't affect existing ones
5. **Testability**: Each probe tests independently

**Probe Interface**:
- GetName(): Probe identifier
- GetTableName(): Metrics table name
- GetQuery(): SQL to execute on monitored connection
- Execute(): Run probe and return metrics
- Store(): Store metrics in datastore
- EnsurePartition(): Create partition if needed
- IsDatabaseScoped(): Whether to run per-database or per-server

**Alternatives Considered**:
- Single query for all metrics: Rejected as inflexible
- Configuration-driven queries: Rejected as less type-safe
- Plugin system: Rejected as overly complex for Go

**Trade-offs**:
- **Cost**: Each probe requires dedicated code
- **Benefit**: Type safety and testability

**Status**: IMPLEMENTED - collector/src/probes/

---

### Decision: Database-Scoped vs. Server-Scoped Probes

**Decision**: Support both per-database probes (e.g., pg_stat_database) and
per-server probes (e.g., pg_stat_bgwriter).

**Rationale**:
1. **PostgreSQL Model**: Some statistics are per-database, some are server-
   wide
2. **Efficiency**: Don't collect server-wide stats multiple times
3. **Accuracy**: Database-scoped stats must be collected per-database

**Implementation**:
- IsDatabaseScoped() method on probe interface
- Scheduler executes database-scoped probes for each accessible database
- Server-scoped probes execute once per connection

**Trade-offs**:
- **Cost**: Scheduler complexity
- **Benefit**: Accurate metrics with minimal overhead

**Status**: IMPLEMENTED - Probe interface, scheduler

---

## Configuration Management

### Decision: Layered Configuration System

**Decision**: Support configuration through command-line flags, configuration
files, and defaults with explicit precedence.

**Precedence**: CLI flags > config file > defaults

**Rationale**:
1. **Flexibility**: Different modes for different use cases
2. **Testing**: Override production config for testing
3. **Documentation**: Configuration file serves as documentation
4. **Operations**: Command-line flags for one-off changes

**Configuration Scope**:
- Shared config (ai-workbench.conf) for collector and server
- Component-specific options clearly documented
- Sensitive data in separate files (pg_password_file)

**Alternatives Considered**:
- Environment variables: Rejected as less discoverable
- Config file only: Rejected as inflexible for testing
- CLI only: Rejected as unwieldy for production

**Trade-offs**:
- **Cost**: More complex configuration resolution
- **Benefit**: Flexibility for diverse deployment scenarios

**Status**: IMPLEMENTED - collector/src/config.go, server/src/config/

---

### Decision: Passwords in Separate Files

**Decision**: Require passwords to be stored in separate files referenced by
pg_password_file configuration option.

**Rationale**:
1. **Security**: Passwords not visible in process listings
2. **File Permissions**: Password files can have restrictive permissions
3. **Secrets Management**: Easier integration with secret management tools
4. **Audit Trail**: Configuration file can be version controlled without
   exposing passwords

**Alternatives Considered**:
- Passwords in config file: Rejected due to visibility in version control
- Environment variables: Rejected as visible in process listings
- Keyring integration: Rejected as overly complex

**Trade-offs**:
- **Cost**: Additional file to manage
- **Benefit**: Better security and secrets management integration

**Status**: IMPLEMENTED - Configuration system

---

### Decision: Per-Installation Server Secret

**Decision**: Require a server_secret configuration value unique per
installation used for encryption.

**Rationale**:
1. **Key Material**: Provides unique encryption key material per installation
2. **Defense in Depth**: Compromised database doesn't expose all passwords
3. **Separation**: Different installations use different encryption keys

**Implementation**:
- Combined with username/token name to encrypt monitored connection passwords
- Stored only in configuration file (never in database)
- Required for both collector and server

**Alternatives Considered**:
- Fixed key: Rejected as all installations would share keys
- Per-password keys: Rejected as key storage problem remains
- No encryption: Rejected as unacceptable security risk

**Trade-offs**:
- **Cost**: Lost server_secret means lost passwords
- **Benefit**: Defense in depth against database compromise

**Status**: IMPLEMENTED - database/crypto.go

---

## Migration Strategy

### Decision: Version-Controlled Schema Migrations

**Decision**: Evolve database schema through numbered, immutable migrations
managed by SchemaManager.

**Rationale**:
1. **Repeatability**: Same migration sequence on all installations
2. **Version Tracking**: schema_version table tracks applied migrations
3. **Immutability**: Migrations never change once applied
4. **Documentation**: Migration description documents intent

**Implementation**:
- Each migration is a Go function with version number and description
- SchemaManager applies pending migrations in order
- Transaction per migration with rollback on error
- COMMENT ON statements document schema purpose

**Alternatives Considered**:
- Manual schema management: Rejected as error-prone
- Migration files (SQL): Rejected to keep schema in one place
- Automatic ORM migrations: Rejected as less explicit

**Trade-offs**:
- **Cost**: Cannot change historical migrations
- **Benefit**: Predictable, auditable schema evolution

**Status**: IMPLEMENTED - collector/src/database/schema.go

---

### Decision: Migration 1 Consolidation

**Decision**: Consolidate original 43 migrations into single Migration 1
with reorganized column order.

**Rationale**:
1. **Maintainability**: Single authoritative schema easier to understand
2. **Column Organization**: Logical ordering (PK, FK, flags, data, timestamps)
3. **Fresh Installations**: New installations apply clean schema
4. **Performance**: Fewer migration steps for new installations

**Column Order Pattern**:
1. Primary key columns
2. Foreign key columns
3. Control/status indicators (is_enabled, is_superuser, is_shared)
4. Important to less important data fields
5. Timestamps (created_at, updated_at, etc.)

**Alternatives Considered**:
- Keep all 43 migrations: Rejected as unnecessarily complex
- Squash periodically: Rejected to maintain immutability

**Trade-offs**:
- **Cost**: Lost detailed history of schema evolution
- **Benefit**: Cleaner, more maintainable initial schema

**Status**: IMPLEMENTED - Migration 1

---

## Testing Strategy

### Decision: Comprehensive Multi-Level Testing

**Decision**: Implement unit tests, component tests, and integration tests
targeting 100% coverage.

**Test Levels**:
1. **Unit Tests**: Individual functions, mocked dependencies
2. **Component Tests**: Full component with real database
3. **Integration Tests**: Multi-component workflows

**Requirements**:
- All tests via standard tooling (go test, npm test, make test)
- Database tests use temporary databases with timestamps
- Tests clean up temporary files (except debug logs)
- Linting and coverage integrated into test suites

**Rationale**:
1. **Regression Prevention**: Catch breaking changes early
2. **Refactoring Confidence**: Tests enable safe refactoring
3. **Documentation**: Tests document expected behavior
4. **Quality Gate**: CI/CD requires passing tests

**Alternatives Considered**:
- Manual testing only: Rejected as insufficient
- Unit tests only: Rejected as missing integration issues
- Integration tests only: Rejected as slow and imprecise

**Trade-offs**:
- **Cost**: Significant test development effort
- **Benefit**: High confidence in code correctness

**Status**: IMPLEMENTED - All components

---

### Decision: Temporary Test Databases

**Decision**: Each test run creates a timestamped temporary database,
runs tests, then drops database.

**Format**: `ai_workbench_test_YYYYMMDD_HHMMSS`

**Rationale**:
1. **Isolation**: Tests don't interfere with each other
2. **Cleanup**: Automatic cleanup prevents database bloat
3. **Debugging**: Timestamp enables correlation with test logs
4. **Parallel Tests**: Multiple test runs can execute simultaneously

**Environment Variables**:
- TEST_AI_WORKBENCH_SERVER: Custom PostgreSQL connection string
- TEST_AI_WORKBENCH_KEEP_DB=1: Keep database for debugging

**Alternatives Considered**:
- Shared test database: Rejected due to test interference
- In-memory database: Rejected as PostgreSQL doesn't support
- Docker containers: Rejected as additional dependency

**Trade-offs**:
- **Cost**: Test startup time to create database
- **Benefit**: Perfect test isolation

**Status**: IMPLEMENTED - All test suites

---

## Build & Development

### Decision: Makefile-Based Build System

**Decision**: Use Makefiles for build, test, lint, and cleanup tasks.

**Rationale**:
1. **Standardization**: Common interface across components
2. **Discoverability**: `make help` lists available targets
3. **Composition**: Top-level Makefile delegates to component Makefiles
4. **IDE Integration**: Most IDEs support Makefile targets
5. **CI/CD**: GitHub Actions easily invoke make targets

**Standard Targets**:
- all: Build the component
- test: Run tests
- coverage: Run tests with coverage report
- lint: Run linter
- test-all: Run all quality checks
- clean: Remove build artifacts
- killall: Kill running processes
- help: Show available targets

**Alternatives Considered**:
- Go tooling only: Rejected as doesn't span all components
- Custom scripts: Rejected as less discoverable
- Task runners (just, make): Considered but Make is ubiquitous

**Trade-offs**:
- **Cost**: Makefile syntax complexity
- **Benefit**: Universal tooling support

**Status**: IMPLEMENTED - All components

---

### Decision: Go 1.23 Requirement

**Decision**: Require Go 1.23 or later for all Go components.

**Rationale**:
1. **Modern Features**: Access to latest language improvements
2. **Security**: Latest security fixes
3. **Performance**: Improved compiler optimizations
4. **Compatibility**: pgx v5 requires recent Go version

**Trade-offs**:
- **Cost**: Users must have recent Go installation
- **Benefit**: Access to latest language capabilities

**Status**: IMPLEMENTED - go.mod files

---

## Future Architecture

### Decision: Multi-Collector Support (Future)

**Decision**: Design supports multiple collector instances for HA/load
balancing but not yet implemented.

**Design Considerations**:
1. **Probe Assignment**: Each probe instance assigned to specific collector
2. **Leader Election**: Coordination for probe scheduling
3. **Failover**: Automatic takeover if collector fails
4. **Load Balancing**: Distribute probe execution across collectors

**Rationale**: Future-proof architecture without premature implementation.

**Status**: DESIGNED NOT IMPLEMENTED

---

### Decision: React Client (Future)

**Decision**: Web client will use React, Node.js, and MUI library.

**Rationale**:
1. **Ecosystem**: Rich component library (MUI)
2. **Developer Experience**: Excellent tooling and community
3. **AI Integration**: Easy integration with Claude/Ollama APIs
4. **MCP Client**: Can consume MCP server directly

**Status**: DESIGNED NOT IMPLEMENTED

---

## Rationale Documentation

All architectural decisions should be documented with:
1. **Context**: What problem does this solve?
2. **Decision**: What was decided?
3. **Rationale**: Why was this the best choice?
4. **Alternatives**: What else was considered and why rejected?
5. **Trade-offs**: What are the costs and benefits?
6. **Status**: Implemented, designed, or superseded

This ensures future maintainers understand not just what was built but why.

---

**Version**: 1.0
**Last Updated**: 2025-11-08
**Status**: Living Document
