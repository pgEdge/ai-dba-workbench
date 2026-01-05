# Design Philosophy: pgEdge AI DBA Workbench

This document captures the core design philosophy, goals, and principles that
guide the pgEdge AI DBA Workbench architecture.

## Primary Mission

The pgEdge AI DBA Workbench exists to enable intelligent monitoring, management,
and interaction with pgEdge Enterprise Postgres deployments through AI
assistants. The system bridges the gap between database administration and AI
reasoning by exposing PostgreSQL operational data, configuration, and
management capabilities through the Model Context Protocol (MCP).

## Core Design Goals

### 1. Historical Context is Critical

**Goal**: Provide comprehensive historical metrics to understand usage patterns,
diagnose past problems, and predict future issues.

**Rationale**: Real-time-only monitoring severely limits diagnostic and
predictive capabilities. Many critical problems occur overnight or manifest
through patterns visible only over time.

**Implementation Impact**: This drives the entire collector/datastore
architecture and the requirement for time-series data storage with configurable
retention.

### 2. Single Binary Distribution

**Goal**: Package each component as a single, self-contained binary with no
runtime dependencies.

**Rationale**: Simplifies deployment, packaging, and distribution across
diverse environments.

**Implementation Impact**: Go is chosen as the implementation language
specifically for this capability. The project explicitly avoids runtime
dependencies like Python interpreters or Node.js for backend components.

### 3. AI Assistant Integration First

**Goal**: Design the system to be consumed by AI assistants rather than
directly by humans.

**Rationale**: AI assistants transform natural language into actionable
insights and operations. The system should expose capabilities in a way that
AI can effectively leverage.

**Implementation Impact**: MCP protocol adoption, structured tool/resource/
prompt design, comprehensive metadata in responses.

### 4. Security Through Isolation

**Goal**: Maintain strict isolation between user sessions and enforce
least-privilege access control.

**Rationale**: Multi-user systems handling database credentials and sensitive
operational data require defense-in-depth security.

**Implementation Impact**: RBAC system (Migration 6), connection ownership
model, token scoping, superuser privilege checks throughout.

### 5. Defensive Secure Coding

**Goal**: Follow industry best practices for secure coding to prevent
injection attacks and privilege escalation.

**Rationale**: Database management tools are high-value targets for attackers.

**Implementation Impact**: Parameterized queries, input validation at both
client and server, password encryption with per-installation secrets,
authentication at API boundaries.

## Architectural Principles

### Separation of Concerns

**Principle**: Cleanly separate data collection, protocol serving, and user
interface into independent components.

**Benefits**:
- Each component can be developed, tested, and deployed independently
- Enables different deployment topologies (single server, distributed, HA)
- Components can evolve at different rates
- Easier testing and maintenance

**Components**:
1. **Collector**: Metrics collection and storage
2. **Server**: MCP protocol implementation
3. **CLI**: Command-line testing/admin interface
4. **Client**: Web-based user interface (future)

### Configuration Hierarchy

**Principle**: Support configuration through multiple sources with clear
precedence.

**Precedence Order**:
1. Command-line options (highest priority)
2. Configuration file settings
3. Hard-coded defaults (lowest priority)

**Rationale**: Enables both permanent configuration and runtime overrides for
testing and troubleshooting.

### Database as Source of Truth

**Principle**: Store all configuration, state, and operational data in
PostgreSQL.

**Rationale**:
- Leverages PostgreSQL's ACID properties for consistency
- Natural fit for time-series metrics data
- Enables SQL-based configuration queries and auditing
- Simplifies backup and disaster recovery

**Exception**: Installation-level secrets and database connection parameters
must be stored in configuration files.

### Progressive Enhancement

**Principle**: Core functionality should work everywhere; enhanced features
can leverage optional extensions.

**Examples**:
- system_stats extension provides enhanced OS metrics if available
- pg_stat_statements provides query-level insights if available
- Spock provides replication status if installed

**Benefits**: System remains useful across diverse PostgreSQL configurations.

### Fail-Safe Defaults

**Principle**: Default to denying access rather than permitting it.

**Examples**:
- Shared connections with no group assignments are DENIED by default
- MCP privilege identifiers not assigned to any group are DENIED
- Expired tokens are rejected
- Non-superusers require explicit privilege grants

**Rationale**: Security errors should fail closed, not open.

### Testability is Non-Negotiable

**Principle**: All code must have automated tests; 100% coverage is the target.

**Requirements**:
- Unit tests for individual functions
- Integration tests for component interactions
- End-to-end tests for full workflows
- Tests must be runnable via standard tooling (go test, npm test)
- Database tests use temporary databases with cleanup
- Mocking where necessary to test error paths

**Rationale**: High-quality testing prevents regressions and enables confident
refactoring.

## Design Patterns

### Probe-Based Monitoring

**Pattern**: Each type of metric is collected by a dedicated "probe" with its
own configuration.

**Structure**:
- Each probe defines a SQL query to execute
- Probes have configurable collection intervals and retention periods
- Metrics stored in per-probe partitioned tables
- Garbage collection automatically drops expired partitions

**Benefits**:
- Easy to add new metrics by creating new probes
- Each metric type has appropriate retention policy
- Partitioning optimizes query performance and storage management

### Weekly Partitioning

**Pattern**: Metrics tables are partitioned by week (Monday-Sunday).

**Rationale**:
- Weekly partitions balance query performance and management overhead
- Natural alignment with common reporting periods
- Enables efficient partition dropping for expired data
- Partition size is predictable and manageable

### Change-Tracked Probes

**Pattern**: Certain probes (pg_settings, pg_hba_file_rules,
pg_ident_file_mappings) track configuration state over time.

**Special Handling**:
- Latest partition for each connection must never be dropped
- Enables "point-in-time" configuration recovery
- Supports change auditing and compliance

**Rationale**: Configuration changes are critical security and compliance
events that must be preserved.

### Token-Based Authentication

**Pattern**: All API access uses bearer tokens; sessions and user tokens
are distinct.

**Types**:
1. **Session tokens** (user_sessions): 24-hour lifetime, deleted on logout
2. **User tokens** (user_tokens): Optional expiry, user-owned API credentials
3. **Service tokens** (service_tokens): Standalone tokens for automation

**Benefits**:
- Stateless authentication for MCP API
- Fine-grained access control through token scoping
- Supports both interactive and programmatic access

### Hierarchical Group Membership

**Pattern**: Groups can contain both users and other groups, creating
inheritance hierarchies.

**Implementation**: Recursive Common Table Expressions (CTEs) resolve
transitive group membership.

**Benefits**:
- Models real organizational structures
- Privilege grants flow naturally down hierarchies
- Reduces administrative overhead (grant once at top level)

### MCP Privilege Identifiers

**Pattern**: Each MCP tool, resource, and prompt has a registered string
identifier that can be granted to groups.

**Workflow**:
1. Server startup seeds all known identifiers
2. Groups are granted access to specific identifiers
3. User access is resolved through group membership
4. Superusers bypass all checks

**Benefits**:
- Explicit enumeration of all privileged operations
- Audit trail of who can access what
- Enables least-privilege API token creation

## Technology Choices

### Go Language

**Choice**: Go 1.23+ for backend components (collector, server, CLI).

**Rationale**:
- Single binary compilation
- Excellent concurrency primitives for probe scheduling and monitoring
- Strong standard library
- Memory safety without garbage collection pauses
- Extensive PostgreSQL driver support

### PostgreSQL as Datastore

**Choice**: PostgreSQL 12+ for all persistent storage.

**Rationale**:
- Native support for partitioned tables
- JSON types for flexible metrics storage
- Robust transaction support
- Natural fit for time-series data
- pgEdge's core competency

### Model Context Protocol

**Choice**: MCP specification for AI assistant integration.

**Rationale**:
- Industry-standard protocol for AI-to-service communication
- Well-defined resource, tool, and prompt abstractions
- SSE transport enables real-time streaming
- Anthropic backing ensures long-term viability

### Server-Sent Events (SSE)

**Choice**: SSE over HTTP/HTTPS for MCP transport.

**Rationale**:
- Simpler than WebSockets
- Works through HTTP proxies and firewalls
- Supports both HTTP (testing) and HTTPS (production)
- Natural fit for MCP's request-response pattern

## Evolution and Extensibility

### Extensibility Points

The design anticipates evolution in these areas:

1. **New Probes**: Easy to add by implementing the MetricsProbe interface
2. **New MCP Tools**: Define handler function and register privilege identifier
3. **Additional Components**: CLI, web client, monitoring dashboards
4. **High Availability**: Multiple collector instances (future enhancement)
5. **Additional Data Sources**: Spock, system_stats, cloud metrics

### Migration Strategy

**Principle**: Database schema evolves through versioned migrations.

**Implementation**:
- Each migration is a numbered, immutable Go function
- SchemaManager applies pending migrations in order
- Migration history tracked in schema_version table
- Migrations include comprehensive COMMENT ON statements

**Consolidation**: Migration 1 consolidated 43 original migrations into a
single initial schema with better column organization.

## Quality Standards

### Code Organization

**Standards**:
- Four spaces for indentation (ALWAYS)
- Modularization to minimize duplication
- Copyright notice in every source file
- Descriptive comments for exported functions

### Documentation

**Standards**:
- Markdown docs in /docs with subdirectories per component
- Lowercase filenames for documentation
- 79-character line wrapping
- Blank line before lists to ensure proper rendering

### Testing

**Standards**:
- Tests run via standard tooling (make test)
- Temporary files cleaned up except debug logs
- Database tests use timestamped temporary databases
- Linting integrated into test suites
- Coverage reporting integrated

## Constraints and Tradeoffs

### Constraint: Go-Only Backend

**Tradeoff**: Limited ecosystem compared to Python/JavaScript, but gains
deployment simplicity and performance.

**Mitigation**: React/Node.js for client UI where ecosystem richness matters.

### Constraint: PostgreSQL Dependency

**Tradeoff**: Requires PostgreSQL availability even for simple operations.

**Mitigation**: Inherent in the product domain (monitoring PostgreSQL).

### Constraint: Weekly Partitions

**Tradeoff**: Cannot drop data more granularly than weekly.

**Mitigation**: Retention policies specified in days are approximate.

### Constraint: Recursive CTEs for Groups

**Tradeoff**: Performance cost for deeply nested groups.

**Mitigation**: Typical group hierarchies are shallow (3-5 levels).

## Anti-Patterns to Avoid

1. **User-Controlled SQL Strings**: Use parameterized queries exclusively
   (exception: MCP tool for arbitrary SQL execution)

2. **Direct User-to-User Access**: Never allow users to access connections
   or data belonging to other users without explicit privilege grants

3. **Authentication Bypass**: Every MCP method except initialize, ping, and
   authenticate_user must validate tokens

4. **Modifying Existing Tests**: Tests should only change when functionality
   changes or to fix bugs/improve code quality

5. **Relative File Paths**: Use absolute paths in all tooling and code

6. **Runtime Dependencies**: Avoid requiring Python, Node.js, or other
   runtimes for backend components

7. **Empty Commit Messages**: All commits must explain the "why" not just
   the "what"

## Success Criteria

The design succeeds when:

1. AI assistants can effectively monitor and manage PostgreSQL through MCP
2. Users can safely grant least-privilege access to team members
3. Historical metrics enable proactive problem detection
4. The system deploys as easily as copying binaries
5. Security audits find no injection vulnerabilities
6. Test coverage approaches 100%
7. New probes can be added in hours, not days
8. The system scales to hundreds of monitored connections

---

**Version**: 1.0
**Last Updated**: 2025-11-08
**Status**: Living Document
