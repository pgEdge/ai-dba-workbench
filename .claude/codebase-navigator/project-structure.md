# Project Structure

This document describes the directory organization of the pgEdge AI DBA
Workbench.

## Top-Level Layout

```
ai-dba-workbench/
├── .claude/              # Claude Code agent definitions and knowledge bases
├── .github/              # GitHub Actions workflows
├── client/               # React web application
├── cli/                  # Command-line interface (Go)
├── collector/            # Data collector service (Go)
├── docs/                 # Project documentation
├── server/               # MCP server (Go)
├── tests/                # Integration tests
├── CLAUDE.md             # Claude Code instructions
├── DESIGN.md             # Architecture and design document
├── Makefile              # Top-level build commands
└── README.md             # Project overview
```

## Collector Structure (`/collector`)

The data collector gathers metrics from PostgreSQL databases.

```
collector/
├── src/
│   ├── main.go           # Entry point
│   ├── config/           # Configuration loading
│   ├── database/         # Database connection and schema
│   │   ├── schema.go     # Migration definitions
│   │   └── pool.go       # Connection pooling
│   ├── probes/           # Metric collection probes
│   │   ├── probe.go      # Probe interface
│   │   └── *.go          # Individual probe implementations
│   └── collector/        # Core collector logic
├── tests/                # Unit tests (if separate from src)
├── Makefile              # Build and test commands
└── README.md             # Collector documentation
```

## Server Structure (`/server`)

The MCP server provides the API for LLM clients.

```
server/
├── src/
│   ├── main.go           # Entry point
│   ├── config/           # Configuration loading
│   ├── database/         # Database operations
│   ├── mcp/              # MCP protocol implementation
│   │   ├── handler.go    # Request handler
│   │   ├── tools/        # MCP tool implementations
│   │   ├── resources/    # MCP resource implementations
│   │   └── prompts/      # MCP prompt implementations
│   ├── auth/             # Authentication and authorization
│   │   ├── tokens.go     # Token management
│   │   ├── rbac.go       # Role-based access control
│   │   └── sessions.go   # Session management
│   └── http/             # HTTP server setup
├── tests/                # Unit tests (if separate from src)
├── Makefile              # Build and test commands
└── README.md             # Server documentation
```

## Client Structure (`/client`)

The React web application for user interaction.

```
client/
├── src/
│   ├── index.tsx         # Entry point
│   ├── App.tsx           # Root component
│   ├── components/       # Reusable UI components
│   │   ├── common/       # Shared components
│   │   └── feature/      # Feature-specific components
│   ├── pages/            # Page components (routes)
│   ├── hooks/            # Custom React hooks
│   ├── services/         # API service functions
│   ├── stores/           # State management
│   ├── theme/            # MUI theme configuration
│   ├── types/            # TypeScript type definitions
│   └── utils/            # Utility functions
├── tests/                # Test files
│   ├── unit/             # Unit tests
│   └── integration/      # Integration tests
├── public/               # Static assets
├── package.json          # Dependencies
└── README.md             # Client documentation
```

## CLI Structure (`/cli`)

Command-line interface for the workbench.

```
cli/
├── src/
│   ├── main.go           # Entry point
│   ├── commands/         # CLI command implementations
│   └── config/           # CLI configuration
├── Makefile              # Build commands
└── README.md             # CLI documentation
```

## Tests Structure (`/tests`)

Integration tests spanning multiple components.

```
tests/
├── integration/          # Integration test files
├── testutil/             # Test utilities
│   ├── database.go       # Database test helpers
│   ├── services.go       # Service management helpers
│   ├── cli.go            # CLI execution helpers
│   ├── config.go         # Configuration helpers
│   └── common.go         # Common utilities
├── logs/                 # Test execution logs
├── Makefile              # Test execution commands
└── README.md             # Test documentation
```

## Documentation Structure (`/docs`)

Project documentation organized by sub-project.

```
docs/
├── index.md              # Documentation entry point
├── collector/            # Collector documentation
├── server/               # Server documentation
├── client/               # Client documentation
├── cli/                  # CLI documentation
└── LICENSE.md            # Project license
```

## Configuration Files

Key configuration files and their locations:

| File | Location | Purpose |
|------|----------|---------|
| `Makefile` | Each sub-project | Build, test, lint commands |
| `go.mod` | Go sub-projects | Go module dependencies |
| `package.json` | `/client` | Node.js dependencies |
| `tsconfig.json` | `/client` | TypeScript configuration |
| `.golangci.yml` | Go sub-projects | Linter configuration |
| `jest.config.js` | `/client` | Jest test configuration |

## Source Code Conventions

All sub-projects follow these conventions:

- Source code in `/src` subdirectory
- Tests in `/tests` or co-located with source
- Four-space indentation
- Documentation in `/docs/<subproject>/`
