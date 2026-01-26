# Development Guide

This guide explains how to set up a development environment for the
alerter and describes the project structure and development workflow.

## Prerequisites

Before developing the alerter, ensure you have:

- [Go 1.21](https://go.dev/doc/install) or higher installed.
- A PostgreSQL 14+ instance for the datastore.
- Git for version control.
- Optionally, [Ollama](https://ollama.ai) for local LLM testing.

## Project Structure

The alerter source code is organized as follows:

```
alerter/
├── src/
│   ├── cmd/
│   │   └── ai-dba-alerter/
│   │       └── main.go           # Entry point
│   └── internal/
│       ├── config/
│       │   ├── config.go         # Configuration handling
│       │   └── config_test.go    # Configuration tests
│       ├── cron/
│       │   ├── cron.go           # Cron expression parsing
│       │   └── cron_test.go      # Cron tests
│       ├── database/
│       │   ├── datastore.go      # Database connection
│       │   ├── types.go          # Type definitions
│       │   ├── queries.go        # Alert queries
│       │   └── notification_queries.go  # Notification queries
│       ├── engine/
│       │   ├── engine.go         # Core alert engine
│       │   └── engine_test.go    # Engine tests
│       ├── llm/
│       │   ├── llm.go            # Provider interfaces
│       │   ├── ollama.go         # Ollama implementation
│       │   ├── openai.go         # OpenAI implementation
│       │   ├── anthropic.go      # Anthropic implementation
│       │   ├── voyage.go         # Voyage implementation
│       │   └── retry.go          # Retry logic
│       └── notifications/
│           ├── manager.go        # Notification manager
│           ├── slack.go          # Slack notifier
│           ├── mattermost.go     # Mattermost notifier
│           ├── webhook.go        # Webhook notifier
│           ├── email.go          # Email notifier
│           └── template.go       # Template rendering
└── docs/                         # Documentation
```

## Setting Up the Development Environment

Clone the repository and navigate to the alerter directory:

```bash
git clone https://github.com/pgEdge/ai-dba-workbench.git
cd ai-dba-workbench/alerter
```

Install Go dependencies:

```bash
go mod download
```

Set up a development datastore with the AI DBA Workbench schema. You can
use the migrations from the collector to create the schema.

## Building the Alerter

Build the alerter binary:

```bash
go build -o bin/ai-dba-alerter ./src
```

Build with race detection for development:

```bash
go build -race -o bin/ai-dba-alerter ./src
```

## Running in Development Mode

Create a development configuration file `dev-config.yaml`:

```yaml
datastore:
  host: localhost
  database: ai_workbench_dev
  username: postgres
  password: postgres

threshold:
  evaluation_interval_seconds: 30

anomaly:
  enabled: true
  tier1:
    enabled: true
    default_sensitivity: 3.0
  tier2:
    enabled: false  # Disable for faster development iteration
  tier3:
    enabled: false
```

Run the alerter with debug logging:

```bash
./bin/ai-dba-alerter -config dev-config.yaml -debug
```

## Code Organization

### Configuration Package

The `config` package handles all configuration loading and validation.
Configuration sources are applied in order: defaults, file, environment
variables, and command-line flags.

### Database Package

The `database` package provides datastore access. The `Datastore` struct
manages the connection pool. Query functions follow a consistent pattern:

- `Get*` functions retrieve single records.
- `Get*s` functions retrieve multiple records.
- `Create*` functions insert new records.
- `Update*` functions modify existing records.
- `Delete*` functions remove records.

### Engine Package

The `engine` package contains the core alerter logic. The `Engine` struct
coordinates all background workers. Each worker runs in its own goroutine
and uses a ticker for periodic execution.

### LLM Package

The `llm` package defines provider interfaces and implementations. The
`EmbeddingProvider` interface generates vector embeddings. The
`ReasoningProvider` interface performs LLM classification.

### Notifications Package

The `notifications` package handles alert delivery. The `Manager` struct
coordinates notification processing. Each channel type has a dedicated
`Notifier` implementation.

## Development Workflow

### Making Changes

1. Create a feature branch from `main`.
2. Make changes following the code style guidelines.
3. Write or update tests for the changes.
4. Run tests locally to verify correctness.
5. Submit a pull request for review.

### Code Style

Follow these code style guidelines:

- Use four spaces for indentation.
- Format code with `gofmt` before committing.
- Write clear, descriptive function and variable names.
- Include the copyright header in all source files.
- Add comments for exported functions and types.

### Testing Changes

Run unit tests before submitting changes:

```bash
go test ./src/...
```

Run tests with coverage:

```bash
go test -cover ./src/...
```

Run tests with race detection:

```bash
go test -race ./src/...
```

### Adding New Metrics

To add support for a new metric:

1. Add the metric query in `database/queries.go`.
2. Add the historical query in `GetHistoricalMetricValues`.
3. Create an alert rule in the database.
4. Test the metric evaluation.

### Adding New LLM Providers

To add a new LLM provider:

1. Create a new file in the `llm` package.
2. Implement the `EmbeddingProvider` or `ReasoningProvider` interface.
3. Add configuration options in `config/config.go`.
4. Register the provider in `llm/llm.go`.
5. Document the configuration options.

### Adding New Notification Channels

To add a new notification channel:

1. Define the channel type in `database/notification_types.go`.
2. Create a notifier implementation in the `notifications` package.
3. Register the notifier in `manager.go`.
4. Add configuration fields as needed.
5. Update the documentation.

## Debugging

### Debug Logging

Enable debug logging with the `-debug` flag. Debug output includes:

- Rule evaluation progress and results.
- Baseline calculation details.
- Anomaly detection tier results.
- Notification processing status.

### Database Queries

Use the PostgreSQL logs to trace database queries. Set `log_statement`
to `all` in the development database for full query logging.

### LLM Debugging

Enable debug logging to see LLM requests and responses. Check the LLM
provider logs for additional debugging information.

## Contributing

Before contributing, review the project's contribution guidelines in
`docs/developers.md`. Ensure all tests pass and the code follows the
style guidelines before submitting a pull request.
