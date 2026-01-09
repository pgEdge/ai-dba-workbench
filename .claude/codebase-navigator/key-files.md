# Key Files

This document identifies critical files in the AI DBA Workbench codebase and
their purposes.

## Entry Points

| File | Purpose |
|------|---------|
| `/collector/src/main.go` | Collector service entry point |
| `/server/src/main.go` | MCP server entry point |
| `/cli/src/main.go` | CLI tool entry point |
| `/client/src/index.tsx` | React application entry point |
| `/client/src/App.tsx` | React root component |

## Configuration Files

### Build & Development

| File | Purpose |
|------|---------|
| `/Makefile` | Top-level build commands |
| `/collector/Makefile` | Collector build, test, lint |
| `/server/Makefile` | Server build, test, lint |
| `/client/package.json` | Client dependencies and scripts |
| `/tests/Makefile` | Integration test commands |

### Go Modules

| File | Purpose |
|------|---------|
| `/collector/go.mod` | Collector dependencies |
| `/server/go.mod` | Server dependencies |
| `/cli/go.mod` | CLI dependencies |
| `/tests/go.mod` | Integration test dependencies |

### Linting & Quality

| File | Purpose |
|------|---------|
| `/collector/.golangci.yml` | Collector linter config |
| `/server/.golangci.yml` | Server linter config |
| `/client/.eslintrc` | Client linter config |
| `/client/tsconfig.json` | TypeScript configuration |

## Database Schema

| File | Purpose |
|------|---------|
| `/collector/src/database/schema.go` | **Primary schema definitions** |
| `/collector/src/database/migrations/` | Migration documentation |

The schema.go file is the **source of truth** for all database migrations.
It contains:

- All table definitions
- Index definitions
- Migration version tracking
- Schema upgrade logic

## Authentication & Authorization

### Server Auth

| File | Purpose |
|------|---------|
| `/server/src/auth/tokens.go` | Token generation and validation |
| `/server/src/auth/sessions.go` | Session lifecycle management |
| `/server/src/auth/rbac.go` | Role-based access control |
| `/server/src/auth/middleware.go` | Auth HTTP middleware |

### Client Auth

| File | Purpose |
|------|---------|
| `/client/src/contexts/AuthContext.tsx` | Auth state management |
| `/client/src/services/auth.ts` | Auth API calls |

## MCP Protocol

| File | Purpose |
|------|---------|
| `/server/src/mcp/handler.go` | Main MCP request handler |
| `/server/src/mcp/protocol.go` | Protocol types and constants |
| `/server/src/mcp/errors.go` | MCP error definitions |
| `/server/src/mcp/tools/*.go` | Individual tool implementations |
| `/server/src/mcp/resources/*.go` | Individual resource implementations |

## Database Operations

### Collector Database

| File | Purpose |
|------|---------|
| `/collector/src/database/pool.go` | Connection pool management |
| `/collector/src/database/queries.go` | Database query functions |
| `/collector/src/database/schema.go` | Schema and migrations |

### Server Database

| File | Purpose |
|------|---------|
| `/server/src/database/pool.go` | Connection pool management |
| `/server/src/database/connections.go` | Connection CRUD operations |
| `/server/src/database/users.go` | User account operations |
| `/server/src/database/tokens.go` | Token storage operations |

## Data Collection

| File | Purpose |
|------|---------|
| `/collector/src/collector/collector.go` | Main collection loop |
| `/collector/src/probes/probe.go` | Probe interface definition |
| `/collector/src/probes/*.go` | Individual probe implementations |

## Client Components

### Core Components

| File | Purpose |
|------|---------|
| `/client/src/App.tsx` | Application root |
| `/client/src/theme/theme.ts` | MUI theme configuration |
| `/client/src/components/Layout.tsx` | Page layout wrapper |
| `/client/src/components/Navigation.tsx` | App navigation |

### Feature Components

| File | Purpose |
|------|---------|
| `/client/src/pages/Dashboard.tsx` | Dashboard page |
| `/client/src/pages/Connections.tsx` | Connection management |
| `/client/src/pages/Query.tsx` | Query interface |
| `/client/src/components/QueryEditor.tsx` | SQL editor |
| `/client/src/components/ResultsTable.tsx` | Query results display |

## Test Utilities

| File | Purpose |
|------|---------|
| `/tests/testutil/database.go` | Database test helpers |
| `/tests/testutil/services.go` | Service management for tests |
| `/tests/testutil/cli.go` | CLI execution helpers |
| `/tests/testutil/config.go` | Test configuration |
| `/tests/testutil/common.go` | Common test utilities |

## Documentation

| File | Purpose |
|------|---------|
| `/DESIGN.md` | Architecture and design document |
| `/CLAUDE.md` | Claude Code instructions |
| `/README.md` | Project overview |
| `/docs/index.md` | Documentation entry point |
| `/docs/collector/` | Collector documentation |
| `/docs/server/` | Server documentation |
| `/docs/client/` | Client documentation |

## CI/CD

| File | Purpose |
|------|---------|
| `/.github/workflows/test-collector.yml` | Collector CI |
| `/.github/workflows/test-server.yml` | Server CI |
| `/.github/workflows/test-integration.yml` | Integration test CI |
| `/.github/workflows/test-client.yml` | Client CI |

## Files to Check When...

### Adding a New MCP Tool

1. `/server/src/mcp/tools/` - Add tool implementation
2. `/server/src/mcp/handler.go` - Register tool (if needed)
3. `/.claude/mcp-expert/tools-catalog.md` - Update documentation
4. `/docs/server/` - Update API documentation

### Adding a New Database Table

1. `/collector/src/database/schema.go` - Add migration
2. `/.claude/postgres-expert/schema-overview.md` - Update docs
3. `/.claude/postgres-expert/migration-history.md` - Document migration

### Adding a New Probe

1. `/collector/src/probes/` - Add probe implementation
2. `/collector/src/database/schema.go` - Add metrics table if needed
3. `/docs/collector/` - Document the probe

### Adding a New React Component

1. `/client/src/components/` - Add component
2. `/client/tests/` - Add tests
3. `/.claude/react-expert/` - Update if new pattern

### Modifying Authentication

1. `/server/src/auth/` - Core auth logic
2. `/server/src/database/` - Token/session storage
3. `/.claude/golang-expert/authentication-flow.md` - Update docs
4. `/docs/server/` - Update API documentation
