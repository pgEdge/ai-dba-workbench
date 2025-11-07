# Database Migrations

This directory is reserved for future standalone SQL migration files if needed
for manual database administration.

## Important Note

**No SQL files are required for deployment or runtime operation.**

All migrations are embedded directly in the compiled binary via
[schema.go](../schema.go). The migration system reads the embedded SQL from the
`registerMigrations()` function, not from external files.

## Deployment

When deploying the pgEdge AI Workbench, you only need to ship the compiled
binary. No SQL files need to be included in the distribution package.

The collector binary contains all migration SQL embedded within it and will
automatically apply pending migrations when it starts.

## Adding New Migrations

When adding a new migration:

**Required:**
- Edit [schema.go](../schema.go) and add your migration to the
  `registerMigrations()` function
- Embed the SQL directly in the Go code as a string literal
- Use `IF NOT EXISTS` clauses to make migrations idempotent

**Optional:**
- If desired for documentation purposes, you may create a standalone
  `NNN_description.sql` file in this directory
- Such files are purely for reference and will not be used by the system

## Migration Execution

Migrations are executed in order by version number when the collector starts.
The migration system:

1. Checks the current schema version in the `schema_version` table
2. Applies all migrations with version numbers higher than the current version
3. Updates the `schema_version` table after each successful migration

All migration SQL must be idempotent (safe to run multiple times) using
`IF NOT EXISTS` clauses.
