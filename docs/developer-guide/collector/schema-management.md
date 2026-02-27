# Schema Management

This document describes the database schema management
system for the pgEdge AI Workbench Collector.

## Overview

The Collector uses a migration-based schema management
system that provides the following capabilities:

- The system automatically creates and updates database
  schemas at startup.
- The system tracks which migrations have been applied.
- The system ensures migrations are applied in the
  correct order.
- The system supports idempotent migrations that can
  run multiple times safely.
- The system creates tables, indexes, constraints, and
  foreign keys.

## Architecture

The schema management system consists of several
components that work together to maintain the
database schema.

### SchemaManager

The `SchemaManager` struct manages all database
migrations. The manager maintains a registry of all
available migrations, determines which migrations
need to be applied, applies pending migrations in
order, and tracks migration status in the database.

### Migration

Each `Migration` struct represents a single schema
change and contains the following fields:

- `Version` is a unique integer identifying the
  migration in sequential order.
- `Description` is a human-readable description of
  the migration.
- `Up` is a function that applies the migration.

### schema_version Table

The `schema_version` table tracks which migrations
have been applied.

```sql
CREATE TABLE schema_version (
    version INTEGER PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at TIMESTAMP NOT NULL
        DEFAULT CURRENT_TIMESTAMP
)
```

### Migration Process

When the Collector starts up, the system executes
the following steps:

1. The `Datastore.initializeSchema()` method is
   called.
2. A new `SchemaManager` is created with all
   registered migrations.
3. The `SchemaManager.Migrate()` method queries the
   current schema version, sorts migrations by version
   number, applies each pending migration in a
   transaction, records successful migrations in
   `schema_version`, and rolls back on errors.

## Current Migrations

The following migrations are currently defined in
the system.

### Migration 1: Create schema_version Table

This migration creates the `schema_version` table
used to track migrations. The table contains
`version`, `description`, and `applied_at` columns.

### Migration 2: Create monitored_connections Table

This migration creates the table that stores
PostgreSQL server connection information.

The migration creates the following objects:

- The `monitored_connections` table stores connection
  details.
- A check constraint on `port` ensures values between
  1 and 65535.
- A check constraint on `owner_token` requires the
  token for non-shared connections.

### Migration 3: Create Indexes on monitored_connections

This migration creates indexes to optimize common
queries.

The migration creates the following indexes:

- `idx_monitored_connections_owner_token` indexes
  `owner_token` for fast ownership lookups.
- `idx_monitored_connections_is_monitored` is a
  partial index on actively monitored connections.
- `idx_monitored_connections_name` indexes connection
  name for fast lookups.

### Migration 4: Create probes Table

This migration creates the table that defines
monitoring probes. The table includes check
constraints on `collection_interval` and
`retention_days`, and a unique constraint on `name`.

### Migration 5: Create Indexes on probes

This migration creates indexes to optimize probe
queries. The migration creates
`idx_probes_enabled` as a partial index on enabled
probes and `idx_probes_name` for fast lookups by
probe name.

### Migration 22: Add connection_id to probe_configs

This migration adds per-server probe configuration
support by adding a `connection_id` column to the
`probe_configs` table. The migration creates a
foreign key constraint to `connections(id)` with
CASCADE delete and a composite unique index on
`(name, COALESCE(connection_id, 0))`.

When `connection_id IS NULL`, the configuration
acts as a global default. When set, the
configuration overrides the default for that
specific connection.

### Migration 23: Fix unique constraint for global probe configs

This migration fixes a duplicate key constraint
issue by replacing the global unique constraint on
`probe_configs.name` with a partial unique index.
The migration removes `probe_configs_name_key` and
creates `probe_configs_name_global_key` as a partial
unique index on `name WHERE connection_id IS NULL`.

## Adding New Migrations

To add a new migration, follow the steps below.

1. Edit `schema.go` by adding a new migration to the
   `registerMigrations()` method.
2. Increment the version using the next sequential
   version number.
3. Provide a clear, concise description of the
   migration.
4. Implement the `Up` function that applies the
   migration.
5. Make the migration idempotent by using
   `IF NOT EXISTS` clauses where possible.

### Example: Adding a New Table

In the following example, the migration creates a
new metrics table:

```go
// Migration 6: Create metrics table
sm.migrations = append(sm.migrations, Migration{
    Version:     6,
    Description: "Create metrics storage table",
    Up: func(db *sql.DB) error {
        _, err := db.Exec(`
            CREATE TABLE IF NOT EXISTS metrics (
                id BIGSERIAL PRIMARY KEY,
                probe_id INTEGER NOT NULL
                    REFERENCES probes(id)
                    ON DELETE CASCADE,
                connection_id INTEGER NOT NULL
                    REFERENCES
                    monitored_connections(id)
                    ON DELETE CASCADE,
                collected_at TIMESTAMP NOT NULL
                    DEFAULT CURRENT_TIMESTAMP,
                metric_data JSONB NOT NULL,
                CONSTRAINT chk_metric_data
                    CHECK (metric_data IS NOT NULL)
            )
        `)
        if err != nil {
            return fmt.Errorf(
                "failed to create metrics table: %w",
                err,
            )
        }
        return nil
    },
})
```

### Example: Adding an Index

In the following example, the migration creates an
index on the `collected_at` column:

```go
// Migration 7: Create index on metrics
sm.migrations = append(sm.migrations, Migration{
    Version:     7,
    Description: "Create index on metrics.collected_at",
    Up: func(db *sql.DB) error {
        _, err := db.Exec(`
            CREATE INDEX IF NOT EXISTS
                idx_metrics_collected_at
            ON metrics(collected_at DESC)
        `)
        if err != nil {
            return fmt.Errorf(
                "failed to create index: %w", err,
            )
        }
        return nil
    },
})
```

### Example: Adding a Foreign Key

In the following example, the migration adds a
foreign key constraint:

```go
// Migration 8: Add foreign key constraint
sm.migrations = append(sm.migrations, Migration{
    Version:     8,
    Description: "Add foreign key from metrics to probes",
    Up: func(db *sql.DB) error {
        var count int
        err := db.QueryRow(`
            SELECT COUNT(*)
            FROM information_schema.table_constraints
            WHERE constraint_name =
                'fk_metrics_probe_id'
            AND table_name = 'metrics'
        `).Scan(&count)
        if err != nil {
            return fmt.Errorf(
                "failed to check constraint: %w",
                err,
            )
        }

        if count > 0 {
            return nil
        }

        _, err = db.Exec(`
            ALTER TABLE metrics
            ADD CONSTRAINT fk_metrics_probe_id
            FOREIGN KEY (probe_id)
            REFERENCES probes(id)
            ON DELETE CASCADE
        `)
        if err != nil {
            return fmt.Errorf(
                "failed to add foreign key: %w",
                err,
            )
        }
        return nil
    },
})
```

### Example: Modifying an Existing Column

In the following example, the migration adds a new
column to an existing table:

```go
// Migration 9: Add priority column to probes
sm.migrations = append(sm.migrations, Migration{
    Version:     9,
    Description: "Add priority column to probes",
    Up: func(db *sql.DB) error {
        var count int
        err := db.QueryRow(`
            SELECT COUNT(*)
            FROM information_schema.columns
            WHERE table_name = 'probes'
            AND column_name = 'priority'
        `).Scan(&count)
        if err != nil {
            return fmt.Errorf(
                "failed to check column: %w", err,
            )
        }

        if count > 0 {
            return nil
        }

        _, err = db.Exec(`
            ALTER TABLE probes
            ADD COLUMN priority INTEGER
                NOT NULL DEFAULT 5
            CHECK (priority >= 1
                AND priority <= 10)
        `)
        if err != nil {
            return fmt.Errorf(
                "failed to add column: %w", err,
            )
        }
        return nil
    },
})
```

## Best Practices

Follow these best practices when designing
migrations and schema changes.

### Migration Design

The following guidelines apply to migration design:

- Include one logical change per migration; each
  migration should represent a single logical schema
  change.
- Never modify applied migrations; create a new
  migration instead.
- Make migrations idempotent; use `IF NOT EXISTS`,
  `IF EXISTS`, and existence checks.
- Use transactions; the SchemaManager wraps each
  migration in a transaction.
- Test migrations thoroughly on a development
  database before deploying.

### Schema Design

The following guidelines apply to schema design:

- Use constraints by defining CHECK, NOT NULL,
  UNIQUE, and FOREIGN KEY to enforce data integrity.
- Create indexes strategically for foreign key
  columns, WHERE clause columns, ORDER BY columns,
  and JOIN conditions.
- Use appropriate data types such as SERIAL for
  auto-incrementing IDs, TIMESTAMP for dates, and
  TEXT for unlimited-length strings.
- Include `created_at` and `updated_at` audit columns
  to track record modifications.
- Plan for partitioning early for large tables such
  as metrics tables.

## Testing

This section covers testing schema migrations.

### Running Schema Tests

In the following example, the `make` command runs
all tests:

```bash
make test
```

In the following example, the `go test` command runs
only schema tests:

```bash
go test -v -run TestSchema
```

### Test Environment

Tests require a PostgreSQL database. In the following
example, the environment variable configures the test
database:

```bash
export TEST_DB_CONN="host=localhost port=5432 \
    user=testuser dbname=testdb sslmode=disable"
```

To skip database tests, set the following variable:

```bash
export SKIP_DB_TESTS=1
```

### Writing Migration Tests

When adding a new migration, add corresponding tests
that verify the following:

- The migration applies successfully without errors.
- Running the migration twice does not cause errors.
- Constraints work as expected.
- Indexes are created correctly.

In the following example, the test verifies that a
migration creates a table:

```go
func TestMigration6Metrics(t *testing.T) {
    db := getTestConnection(t)
    if db == nil {
        return
    }
    defer db.Close()

    cleanupTestSchema(t, db)
    sm := NewSchemaManager()
    if err := sm.Migrate(db); err != nil {
        t.Fatalf("Failed to migrate: %v", err)
    }

    var count int
    err := db.QueryRow(`
        SELECT COUNT(*)
        FROM information_schema.tables
        WHERE table_name = 'metrics'
    `).Scan(&count)
    if err != nil {
        t.Fatalf(
            "Failed to check for table: %v", err,
        )
    }
    if count != 1 {
        t.Fatal("metrics table was not created")
    }

    cleanupTestSchema(t, db)
}
```

## Troubleshooting

This section covers common schema management issues.

### Migration Fails to Apply

If a migration fails, follow these steps:

1. Check the error message for details about the
   failure.
2. Verify that the database connection is accessible.
3. Review the migration code for logic errors.
4. Check for manual schema changes that conflict
   with the migration.

### Migration Applied but Schema Incorrect

If a migration was applied but the schema is
incorrect, follow these steps:

1. Check the `schema_version` table to verify which
   migrations were applied.
2. Investigate whether the migration partially
   applied before failing.
3. Create a new fix-up migration to correct the
   schema.

### Rolling Back Migrations

The current system does not support automatic
rollback. To roll back manually, follow these steps:

1. Use SQL to undo the migration changes manually.
2. Remove the migration record from
   `schema_version`.
3. Consider creating a new forward migration that
   reverts the changes instead.

## Security Considerations

Follow these security practices when writing
migrations.

### Secure Migration Practices

The following guidelines apply to secure migrations:

- Validate inputs if migrations use any configuration
  values.
- Use parameterized queries when migration logic
  includes dynamic values.
- Run migrations with a database user that has only
  the necessary privileges.
- Review all migrations for security implications
  before applying.

### Data Protection

The following guidelines apply to data protection:

- Always back up the database before applying
  migrations in production.
- Test migrations on a copy of production data before
  applying to production.
- Ensure migrations do not inadvertently expose
  sensitive data.

## See Also

The following resources provide additional details.

- [Database Schema](schema.md) covers the schema
  structure and design.
- [Probes](probes.md) explains how probes collect
  and store data.
- [Architecture](architecture.md) describes the
  overall system design.
