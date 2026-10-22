# Environment Variables

The server reads environment variables at startup; those server properties
specified in an environment variable are overridden by properties specified on
the command line, but take precedence over properties written in a
configuration file. The exception is `PGEDGE_POSTGRES_CONNECTION_STRING`,
which does not configure the datastore. The server supports the following
variables:

- [`PGEDGE_POSTGRES_CONNECTION_STRING`](#pgedge_postgres_connection_string)
- [`PGEDGE_DB_LOG_LEVEL`](#pgedge_db_log_level)
- [`PGEDGE_MEMORY_ENABLED`](#pgedge_memory_enabled)
- [`PGEDGE_AUDIT_RETENTION_DAYS`](#pgedge_audit_retention_days)

## `PGEDGE_POSTGRES_CONNECTION_STRING`

The `PGEDGE_POSTGRES_CONNECTION_STRING` variable provides a fallback
PostgreSQL connection string that the server's database client uses only when
no database user is configured. The variable does not satisfy the datastore
configuration; the server does not start unless the `database` section of the
configuration file or the
[`-db-*` command-line options](command_line_options.md) supply the datastore
settings, including a user.

In the following example, the variable specifies a remote database:

```bash
export PGEDGE_POSTGRES_CONNECTION_STRING=\
"postgres://user:pass@db.example.com:5432/\
ai_workbench?sslmode=require"
```

## `PGEDGE_DB_LOG_LEVEL`

The `PGEDGE_DB_LOG_LEVEL` variable controls the verbosity of database operation
logging. The server writes database log messages to `stderr` with a
`[DATABASE]` prefix.

The following values are supported:

- `none` disables all database logging (default).
- `info` logs connections, queries, and errors.
- `debug` logs metadata, pool details, and queries.
- `trace` logs full queries, row counts, and timings.

An empty or unrecognized value defaults to `none`.

In the following example, the variable enables debug logging:

```bash
export PGEDGE_DB_LOG_LEVEL=debug
```

## `PGEDGE_MEMORY_ENABLED`

The `PGEDGE_MEMORY_ENABLED` variable controls the chat memory feature for Ask
Ellie. The server reads this variable at startup and applies the value as an
override to the `memory.enabled` configuration option.

The following values are supported:

- `true` enables chat memory (default).
- `false` disables chat memory.

In the following example, the variable disables chat memory:

```bash
export PGEDGE_MEMORY_ENABLED=false
```

## `PGEDGE_AUDIT_RETENTION_DAYS`

The `PGEDGE_AUDIT_RETENTION_DAYS` variable controls how many days of RBAC audit
events the server keeps. The server reads this variable at startup and applies
the value as an override to the `http.auth.audit_retention_days` configuration
option. A value of `0` keeps audit events forever.

In the following example, the variable keeps a year of audit events:

```bash
export PGEDGE_AUDIT_RETENTION_DAYS=365
```
