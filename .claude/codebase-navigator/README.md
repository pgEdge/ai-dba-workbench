# Codebase Navigator Knowledge Base

This directory contains documentation to help navigate the pgEdge AI DBA
Workbench codebase efficiently.

## Purpose

This knowledge base provides:

- Project structure and directory organization
- Feature implementation locations
- Data flow between components
- Key files and their purposes
- Common search patterns

## Documents

### [project-structure.md](project-structure.md)

High-level overview of the project organization:

- Directory layout for each sub-project
- Source code organization patterns
- Test file locations
- Configuration file locations

### [feature-locations.md](feature-locations.md)

Where specific features are implemented:

- Authentication and authorization
- Database connection management
- MCP tools and resources
- UI components and pages
- Data collection and metrics

### [data-flow.md](data-flow.md)

How data moves through the system:

- Collector to server communication
- Server to client API patterns
- MCP request/response flow
- Database query patterns

### [key-files.md](key-files.md)

Critical files and their purposes:

- Entry points for each sub-project
- Configuration files
- Schema definitions
- Core business logic locations

## Quick Reference

### Sub-Project Roots

- `/collector` - Go data collector
- `/server` - Go MCP server
- `/client` - React web application
- `/cli` - Go command-line interface
- `/tests` - Integration tests
- `/docs` - Documentation

### Common Search Patterns

**Find MCP tool implementations:**
```
/.claude/mcp-expert/tools-catalog.md
/server/src/mcp/tools/
```

**Find React components:**
```
/client/src/components/
/client/src/pages/
```

**Find database operations:**
```
/collector/src/database/
/server/src/database/
```

**Find test files:**
```
/collector/src/**/*_test.go
/server/src/**/*_test.go
/client/tests/
/tests/integration/
```

## Document Updates

Update these documents when:

- New features are added
- File locations change
- New patterns are established
- Data flow changes significantly

Last Updated: 2026-01-09
