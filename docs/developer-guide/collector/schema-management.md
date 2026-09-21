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
- `Up` is a function of the form `func(pgx.Tx) error`
  that applies the migration on the transaction opened
  for it.

### schema_version Table

The `schema_version` table tracks which migrations
have been applied.

```sql
CREATE TABLE schema_version (
    version INTEGER PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL
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
3. The `SchemaManager.Migrate()` method sorts the
   migrations by version number, queries the current
   schema version, applies each pending migration in a
   transaction, records successful migrations in
   `schema_version`, and rolls back on errors.

## Current Migrations

The migrations are defined in Go rather than in SQL
files. The `registerMigrations()` method in
`collector/src/database/schema.go` is the authoritative
list, and this document does not repeat it, because a
second copy drifts as soon as someone adds a migration.

Each entry is a `Migration` value carrying a `Version`, a
`Description` and an `Up` function of the form
`func(pgx.Tx) error` that runs the migration's statements
on the transaction the `SchemaManager` opens for it.

Migration 1 is the consolidated baseline; the `Up`
function creates the complete schema, including every
table, index and constraint, along with the seed data for
probe configurations and alert rules, so a fresh
installation reaches the current schema in a single step.
Each later migration is an incremental change, such as a
new column, a new index or a corrected alert rule, and is
applied in version order to an installation that already
holds the earlier schema.

Versions are allocated sequentially, so the highest
`Version` registered in `schema.go` is the schema version
a current collector converges to. The
`SchemaManager.LatestVersion()` method returns that value
at run time, without needing a database connection.

The `schema_version` table records what has been applied.
The `Migrate()` method inserts a row holding the version
number and the description of each migration it commits,
and reads the highest version back at the next start-up to
decide which migrations are still pending.

## Adding New Migrations

To add a new migration, follow the steps below.

1. Edit `collector/src/database/schema.go` by appending a
   new `Migration` to the `registerMigrations()` method.
2. Set the version to the next free number, one higher
   than the last migration registered in the file.
3. Provide a clear, concise description of the migration;
   the description is stored in `schema_version`.
4. Implement the `Up` function, which receives the
   transaction the migration runs in.
5. Make the migration idempotent by using
   `IF NOT EXISTS` clauses where possible.
6. Describe every new object with `COMMENT ON`.

The examples below use `16` as the version number; replace
it with the next free number at the time you write the
migration.

### Example: Adding a New Table

In the following example, the migration creates a new
table:

```go
sm.migrations = append(sm.migrations, Migration{
    Version:     16,
    Description: "Add probe_events table",
    Up: func(tx pgx.Tx) error {
        ctx := context.Background()

        _, err := tx.Exec(ctx, `
            CREATE TABLE IF NOT EXISTS probe_events (
                id BIGSERIAL PRIMARY KEY,
                connection_id INTEGER NOT NULL
                    REFERENCES connections(id)
                    ON DELETE CASCADE,
                collected_at TIMESTAMPTZ NOT NULL
                    DEFAULT CURRENT_TIMESTAMP,
                event_data JSONB NOT NULL
            );

            COMMENT ON TABLE probe_events IS
                'Discrete events reported by a probe.';
        `)
        if err != nil {
            return fmt.Errorf(
                "failed to create probe_events: %w", err,
            )
        }

        return nil
    },
})
```

### Example: Adding an Index

In the following example, the migration creates an index
on the `collected_at` column:

```go
sm.migrations = append(sm.migrations, Migration{
    Version:     16,
    Description: "Add index on probe_events.collected_at",
    Up: func(tx pgx.Tx) error {
        ctx := context.Background()

        _, err := tx.Exec(ctx, `
            CREATE INDEX IF NOT EXISTS
                idx_probe_events_collected_at
                ON probe_events(collected_at DESC);
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

PostgreSQL cannot build an index with
`CREATE INDEX CONCURRENTLY` inside a transaction, and the
`SchemaManager` wraps every migration in one, so an index
added by a migration is built with a plain `CREATE INDEX`
that blocks writes to the table whilst it runs.

### Example: Adding a Constraint

PostgreSQL has no `ADD CONSTRAINT IF NOT EXISTS`, so
`schema.go` provides the `addConstraintIfMissing` helper,
which checks `pg_constraint` first and leaves an existing
constraint untouched. In the following example, the
migration adds a foreign key:

```go
if err := addConstraintIfMissing(ctx, tx,
    "probe_events",
    "fk_probe_events_connection_id",
    "FOREIGN KEY (connection_id) "+
        "REFERENCES connections(id) ON DELETE CASCADE",
); err != nil {
    return err
}
```

### Example: Modifying an Existing Column

In the following example, the migration adds a new column
to an existing table:

```go
sm.migrations = append(sm.migrations, Migration{
    Version:     16,
    Description: "Add priority column to probe_configs",
    Up: func(tx pgx.Tx) error {
        ctx := context.Background()

        _, err := tx.Exec(ctx, `
            ALTER TABLE probe_configs
                ADD COLUMN IF NOT EXISTS priority INTEGER
                NOT NULL DEFAULT 5;

            COMMENT ON COLUMN probe_configs.priority IS
                'Relative scheduling priority, 1 to 10.';
        `)
        if err != nil {
            return fmt.Errorf(
                "failed to add priority column: %w", err,
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
  auto-incrementing IDs, TIMESTAMPTZ for timestamps, and
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
only the migration tests:

```bash
go test -v -run TestMigrate
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
func TestProbeEventsTable(t *testing.T) {
    pool, conn := getTestConnection(t)
    defer pool.Close()
    defer conn.Release()

    cleanupTestSchema(t, pool)
    sm := NewSchemaManager()
    if err := sm.Migrate(conn); err != nil {
        t.Fatalf("Failed to migrate: %v", err)
    }

    var count int
    err := pool.QueryRow(context.Background(), `
        SELECT COUNT(*)
        FROM information_schema.tables
        WHERE table_name = 'probe_events'
    `).Scan(&count)
    if err != nil {
        t.Fatalf("Failed to check for table: %v", err)
    }
    if count != 1 {
        t.Fatal("probe_events table was not created")
    }

    cleanupTestSchema(t, pool)
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
