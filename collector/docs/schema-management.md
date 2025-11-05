# Schema Management

This document describes the database schema management system for the pgEdge AI
Workbench Collector.

## Overview

The collector uses a migration-based schema management system that:

- Automatically creates and updates database schemas at startup
- Tracks which migrations have been applied
- Ensures migrations are applied in the correct order
- Supports idempotent migrations that can be run multiple times safely
- Creates tables, indexes, constraints, and foreign keys

## Architecture

### Components

The schema management system consists of the following components:

#### SchemaManager

The `SchemaManager` struct manages all database migrations. It:

- Maintains a registry of all available migrations
- Determines which migrations need to be applied
- Applies pending migrations in order
- Tracks migration status in the database

#### Migration

Each `Migration` struct represents a single schema change and contains:

- `Version`: A unique integer identifying the migration (sequential)
- `Description`: A human-readable description of what the migration does
- `Up`: A function that applies the migration

#### schema_version Table

The `schema_version` table tracks which migrations have been applied:

```sql
CREATE TABLE schema_version (
    version INTEGER PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
)
```

### Migration Process

When the collector starts up:

1. The `Datastore.initializeSchema()` method is called
2. A new `SchemaManager` is created with all registered migrations
3. The `SchemaManager.Migrate()` method:
   - Queries the current schema version from the database
   - Sorts migrations by version number
   - Applies each pending migration in a transaction
   - Records successful migrations in `schema_version`
   - Rolls back on errors

## Current Migrations

The following migrations are currently defined:

### Migration 1: Create schema_version Table

Creates the `schema_version` table used to track migrations.

**Objects created:**

- `schema_version` table with `version`, `description`, and `applied_at` columns

### Migration 2: Create monitored_connections Table

Creates the table that stores PostgreSQL server connection information.

**Objects created:**

- `monitored_connections` table
- Check constraint on `port` (1-65535)
- Check constraint on `owner_token` (required for non-shared connections)

**Table structure:**

- `id`: Serial primary key
- `name`: Connection name (VARCHAR(255))
- `host`: Hostname (VARCHAR(255))
- `hostaddr`: IP address (VARCHAR(255), optional)
- `port`: Port number (INTEGER, default 5432)
- `database_name`: Database name (VARCHAR(255))
- `username`: Username (VARCHAR(255))
- `password_encrypted`: Encrypted password (TEXT, optional)
- SSL/TLS configuration fields
- `is_shared`: Whether the connection is shared (BOOLEAN, default FALSE)
- `owner_token`: Token identifying the connection owner (VARCHAR(255))
- `is_monitored`: Whether the connection is actively monitored (BOOLEAN,
    default FALSE)
- Timestamp fields (`created_at`, `updated_at`)

### Migration 3: Create Indexes on monitored_connections

Creates indexes to optimize common queries.

**Objects created:**

- `idx_monitored_connections_owner_token`: Index on `owner_token` for fast
    ownership lookups
- `idx_monitored_connections_is_monitored`: Partial index on actively monitored
    connections
- `idx_monitored_connections_name`: Index on connection name for fast lookups

### Migration 4: Create probes Table

Creates the table that defines monitoring probes.

**Objects created:**

- `probes` table
- Check constraint on `collection_interval` (must be > 0)
- Check constraint on `retention_days` (must be > 0)
- Unique constraint on `name`

**Table structure:**

- `id`: Serial primary key
- `name`: Probe name (VARCHAR(255), unique)
- `description`: Probe description (TEXT, optional)
- `sql_query`: SQL query to execute (TEXT)
- `collection_interval`: How often to run the probe in seconds (INTEGER,
    default 60)
- `retention_days`: How long to retain data (INTEGER, default 7)
- `enabled`: Whether the probe is enabled (BOOLEAN, default TRUE)
- Timestamp fields (`created_at`, `updated_at`)

### Migration 5: Create Indexes on probes

Creates indexes to optimize probe queries.

**Objects created:**

- `idx_probes_enabled`: Partial index on enabled probes
- `idx_probes_name`: Index on probe name for fast lookups

### Migration 22: Add connection_id to probe_configs

Adds per-server probe configuration support by adding a `connection_id` column
to the `probe_configs` table.

**Objects created/modified:**

- `connection_id` column added to `probe_configs` table
- Foreign key constraint to `connections(id)` with CASCADE delete
- Composite unique index `probe_configs_name_connection_key` on
  `(name, COALESCE(connection_id, 0))`

**Purpose**: Enables customizing probe collection intervals and retention
periods for individual monitored connections while maintaining global defaults.

**Behavior**: When `connection_id IS NULL`, the configuration acts as a global
default. When set, it overrides the default for that specific connection.

### Migration 23: Fix unique constraint for global probe configs

Fixes a duplicate key constraint issue by replacing the global unique
constraint on `probe_configs.name` with a partial unique index.

**Objects removed:**

- `probe_configs_name_key` constraint (previously created as inline UNIQUE
  constraint)

**Objects created:**

- `probe_configs_name_global_key` partial unique index on `name WHERE
  connection_id IS NULL`

**Purpose**: Ensures global probe configurations (where `connection_id IS
NULL`) remain unique by name, while allowing multiple per-server configurations
with the same probe name but different connection IDs.

**Background**: Migration 22 added a composite unique index but didn't remove
the old inline unique constraint, causing duplicate key violations when
inserting per-server configs.

## Adding New Migrations

To add a new migration:

1. **Edit schema.go**: Add a new migration to the `registerMigrations()`
    method in `schema.go`

2. **Increment version**: Use the next sequential version number (current max
    version + 1)

3. **Provide description**: Write a clear, concise description of what the
    migration does

4. **Implement Up function**: Write the function that applies the migration

5. **Make it idempotent**: Use `IF NOT EXISTS` clauses where possible so the
    migration can be run multiple times safely

### Example: Adding a New Table

```go
// Migration 6: Create metrics table
sm.migrations = append(sm.migrations, Migration{
    Version:     6,
    Description: "Create metrics storage table",
    Up: func(db *sql.DB) error {
        _, err := db.Exec(`
            CREATE TABLE IF NOT EXISTS metrics (
                id BIGSERIAL PRIMARY KEY,
                probe_id INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
                connection_id INTEGER NOT NULL
                    REFERENCES monitored_connections(id) ON DELETE CASCADE,
                collected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
                metric_data JSONB NOT NULL,
                CONSTRAINT chk_metric_data CHECK (metric_data IS NOT NULL)
            )
        `)
        if err != nil {
            return fmt.Errorf("failed to create metrics table: %w", err)
        }
        return nil
    },
})
```

### Example: Adding an Index

```go
// Migration 7: Create index on metrics
sm.migrations = append(sm.migrations, Migration{
    Version:     7,
    Description: "Create index on metrics.collected_at",
    Up: func(db *sql.DB) error {
        _, err := db.Exec(`
            CREATE INDEX IF NOT EXISTS idx_metrics_collected_at
            ON metrics(collected_at DESC)
        `)
        if err != nil {
            return fmt.Errorf("failed to create index: %w", err)
        }
        return nil
    },
})
```

### Example: Adding a Foreign Key

```go
// Migration 8: Add foreign key constraint
sm.migrations = append(sm.migrations, Migration{
    Version:     8,
    Description: "Add foreign key from metrics to probes",
    Up: func(db *sql.DB) error {
        // First check if constraint already exists
        var count int
        err := db.QueryRow(`
            SELECT COUNT(*)
            FROM information_schema.table_constraints
            WHERE constraint_name = 'fk_metrics_probe_id'
            AND table_name = 'metrics'
        `).Scan(&count)
        if err != nil {
            return fmt.Errorf("failed to check constraint: %w", err)
        }

        if count > 0 {
            return nil // Constraint already exists
        }

        _, err = db.Exec(`
            ALTER TABLE metrics
            ADD CONSTRAINT fk_metrics_probe_id
            FOREIGN KEY (probe_id) REFERENCES probes(id) ON DELETE CASCADE
        `)
        if err != nil {
            return fmt.Errorf("failed to add foreign key: %w", err)
        }
        return nil
    },
})
```

### Example: Modifying an Existing Column

```go
// Migration 9: Add new column to existing table
sm.migrations = append(sm.migrations, Migration{
    Version:     9,
    Description: "Add priority column to probes",
    Up: func(db *sql.DB) error {
        // Check if column already exists
        var count int
        err := db.QueryRow(`
            SELECT COUNT(*)
            FROM information_schema.columns
            WHERE table_name = 'probes'
            AND column_name = 'priority'
        `).Scan(&count)
        if err != nil {
            return fmt.Errorf("failed to check column: %w", err)
        }

        if count > 0 {
            return nil // Column already exists
        }

        _, err = db.Exec(`
            ALTER TABLE probes
            ADD COLUMN priority INTEGER NOT NULL DEFAULT 5
            CHECK (priority >= 1 AND priority <= 10)
        `)
        if err != nil {
            return fmt.Errorf("failed to add column: %w", err)
        }
        return nil
    },
})
```

## Best Practices

### Migration Design

1. **One logical change per migration**: Each migration should represent a
    single logical schema change (e.g., creating one table, adding one set of
    related indexes)

2. **Never modify applied migrations**: Once a migration has been applied in
    production, never modify it. Create a new migration instead.

3. **Make migrations idempotent**: Use `IF NOT EXISTS`, `IF EXISTS`, and
    existence checks to ensure migrations can be run multiple times safely

4. **Use transactions**: The SchemaManager wraps each migration in a
    transaction that will be rolled back on error

5. **Test migrations thoroughly**: Always test migrations on a development
    database before deploying to production

### Schema Design

1. **Use constraints**: Define constraints (CHECK, NOT NULL, UNIQUE, FOREIGN
    KEY) to enforce data integrity at the database level

2. **Create indexes strategically**: Add indexes for:
   - Foreign key columns
   - Columns used in WHERE clauses
   - Columns used in ORDER BY clauses
   - Columns used in JOIN conditions

3. **Use appropriate data types**: Choose the right data type for each column:
   - `SERIAL` or `BIGSERIAL` for auto-incrementing IDs
   - `TIMESTAMP` for date/time values
   - `BOOLEAN` for true/false values
   - `TEXT` for unlimited-length strings
   - `VARCHAR(n)` for limited-length strings
   - `INTEGER` or `BIGINT` for numbers

4. **Include audit columns**: Add `created_at` and `updated_at` columns to
    track when records are created and modified

5. **Plan for partitioning**: For large tables (like metrics), consider
    partitioning strategies early in the design

## Testing

### Running Schema Tests

Run schema tests with:

```bash
make test
```

Or directly with Go:

```bash
go test -v -run TestSchema
```

### Test Environment

Tests require a PostgreSQL database. Configure the test database with:

```bash
export TEST_DB_CONN="host=localhost port=5432 user=testuser
    dbname=testdb sslmode=disable"
```

To skip database tests:

```bash
export SKIP_DB_TESTS=1
```

### Writing Migration Tests

When adding a new migration, add corresponding tests:

1. **Test migration applies successfully**: Verify the migration runs without
    errors

2. **Test idempotency**: Verify running the migration twice doesn't cause
    errors

3. **Test constraints**: Verify constraints work as expected

4. **Test indexes**: Verify indexes are created

Example test:

```go
func TestMigration6Metrics(t *testing.T) {
    db := getTestConnection(t)
    if db == nil {
        return
    }
    defer db.Close()

    // Clean up and migrate
    cleanupTestSchema(t, db)
    sm := NewSchemaManager()
    if err := sm.Migrate(db); err != nil {
        t.Fatalf("Failed to migrate: %v", err)
    }

    // Verify metrics table exists
    var count int
    err := db.QueryRow(`
        SELECT COUNT(*)
        FROM information_schema.tables
        WHERE table_name = 'metrics'
    `).Scan(&count)
    if err != nil {
        t.Fatalf("Failed to check for metrics table: %v", err)
    }
    if count != 1 {
        t.Fatal("metrics table was not created")
    }

    // Test inserting data
    _, err = db.Exec(`
        INSERT INTO metrics (probe_id, connection_id, metric_data)
        VALUES (1, 1, '{"cpu": 50}')
    `)
    // Test should handle error appropriately based on whether
    // test data exists

    // Clean up
    cleanupTestSchema(t, db)
}
```

## Troubleshooting

### Migration Fails to Apply

If a migration fails:

1. **Check the error message**: The error will indicate what went wrong
2. **Verify database connection**: Ensure the database is accessible
3. **Check migration logic**: Review the migration code for errors
4. **Check for conflicts**: Ensure no manual schema changes conflict with the
    migration

### Migration Applied but Schema Incorrect

If a migration was applied but the schema is not as expected:

1. **Verify the migration version**: Check `schema_version` table to see which
    migrations were applied
2. **Check for partial application**: The migration may have partially applied
    before failing
3. **Create a fix-up migration**: Add a new migration to correct the schema

### Rolling Back Migrations

The current system does not support automatic rollback of applied migrations.
To roll back:

1. **Manually revert changes**: Use SQL to undo the migration changes
2. **Update schema_version**: Remove the migration record from `schema_version`
3. **Consider forward-only approach**: Instead of rolling back, create a new
    migration that reverts the changes

## Security Considerations

### Secure Migration Practices

1. **Validate inputs**: If migrations use any configuration values, validate
    them first
2. **Use parameterized queries**: When migration logic includes dynamic values,
    use parameterized queries
3. **Limit permissions**: Run migrations with a database user that has only
    the necessary privileges
4. **Review carefully**: Always review migrations for security implications
    before applying

### Data Protection

1. **Backup before migrating**: Always backup the database before applying
    migrations in production
2. **Test on copy**: Test migrations on a copy of production data before
    applying to production
3. **Avoid data exposure**: Ensure migrations don't inadvertently expose
    sensitive data

## Future Enhancements

Potential future improvements to the schema management system:

1. **Rollback support**: Add down migrations to support rollback
2. **Migration verification**: Add checksum verification to detect modified
    migrations
3. **Dry run mode**: Add ability to preview migrations without applying them
4. **Parallel migrations**: Support applying independent migrations in parallel
5. **Migration dependencies**: Allow migrations to declare dependencies on
    other migrations
6. **External migration files**: Support loading migrations from external SQL
    files
