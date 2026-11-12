# Adding New Probes

This guide explains how to add custom probes to the Collector.

## When to Add a Probe

Consider adding a probe when you need to accomplish one of the following goals:

- Monitor a PostgreSQL extension's views such as pgBouncer or Citus.
- Collect custom application metrics stored in PostgreSQL.
- Monitor specific queries or patterns.
- Gather data from custom functions.

## Probe Implementation Steps

Follow these steps to create a new probe.

### 1. Create Probe File

Create a new file in `/collector/src/probes/`. In the following example, the
commands create a new probe file:

```bash
cd collector/src/probes
touch pg_stat_custom_probe.go
```

### 2. Implement the Probe

The following code shows a complete probe implementation:

```go
/*-----------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under
 * The PostgreSQL License
 *
 *-----------------------------------------------
 */

package probes

import (
    "context"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/pgedge/ai-workbench/collector/src/utils"
)

// PgStatCustomProbe collects metrics from a
// custom view
type PgStatCustomProbe struct {
    BaseMetricsProbe
}

// NewPgStatCustomProbe creates a new custom probe
func NewPgStatCustomProbe(
    config *ProbeConfig) *PgStatCustomProbe {
    return &PgStatCustomProbe{
        BaseMetricsProbe: BaseMetricsProbe{
            config: config,
        },
    }
}

// GetName returns the probe name
func (p *PgStatCustomProbe) GetName() string {
    return "pg_stat_custom"
}

// GetTableName returns the metrics table name
func (p *PgStatCustomProbe) GetTableName() string {
    return "pg_stat_custom"
}

// IsDatabaseScoped returns true if this probe
// runs per-database
func (p *PgStatCustomProbe) IsDatabaseScoped() bool {
    return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatCustomProbe) GetQuery() string {
    return `
        SELECT
            metric_name,
            metric_value,
            recorded_at
        FROM custom_metrics_view
        WHERE recorded_at > NOW()
            - INTERVAL '5 minutes'
    `
}

// Execute runs the probe against a monitored
// connection
func (p *PgStatCustomProbe) Execute(
    ctx context.Context,
    monitoredConn *pgxpool.Conn,
) ([]map[string]interface{}, error) {
    rows, err := monitoredConn.Query(
        ctx, p.GetQuery())
    if err != nil {
        return nil, fmt.Errorf(
            "failed to execute query: %w", err)
    }
    defer rows.Close()

    return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the
// datastore
func (p *PgStatCustomProbe) Store(
    ctx context.Context,
    datastoreConn *pgxpool.Conn,
    connectionID int,
    timestamp time.Time,
    metrics []map[string]interface{},
) error {
    if len(metrics) == 0 {
        return nil
    }

    if err := p.EnsurePartition(
        ctx, datastoreConn, timestamp); err != nil {
        return fmt.Errorf(
            "failed to ensure partition: %w", err)
    }

    columns := []string{
        "connection_id", "collected_at",
        "metric_name", "metric_value",
        "recorded_at",
    }

    var values [][]interface{}
    for _, metric := range metrics {
        row := []interface{}{
            connectionID,
            timestamp,
            metric["metric_name"],
            metric["metric_value"],
            metric["recorded_at"],
        }
        values = append(values, row)
    }

    if err := StoreMetricsWithCopy(
        ctx, datastoreConn, p.GetTableName(),
        columns, values); err != nil {
        return fmt.Errorf(
            "failed to store metrics: %w", err)
    }

    return nil
}

// EnsurePartition ensures a partition exists for
// the given timestamp
func (p *PgStatCustomProbe) EnsurePartition(
    ctx context.Context,
    datastoreConn *pgxpool.Conn,
    timestamp time.Time,
) error {
    return EnsurePartition(
        ctx, datastoreConn,
        p.GetTableName(), timestamp)
}
```

### 3. Add Constant

Add the probe name to `/collector/src/probes/constants.go`. In the following
example, the constant defines the probe name:

```go
const (
    // ... existing constants ...
    ProbeNamePgStatCustom = "pg_stat_custom"
)
```

Add the same name to the `knownProbes` set in
`/server/src/internal/metrics/known_probes.go` in the same change. The server
lists and serves only the probes in that set, and the
`TestKnownProbesMatchCollectorSchema` test fails when the set and the
collector's metrics tables differ.

### 4. Register in Scheduler

Add probe creation to `/collector/src/scheduler/scheduler.go` in the
`createProbe` method. In the following example, the case statement registers
the new probe:

```go
func (ps *ProbeScheduler) createProbe(
    config *probes.ProbeConfig,
) probes.MetricsProbe {
    switch config.Name {
    // ... existing cases ...
    case probes.ProbeNamePgStatCustom:
        return probes.NewPgStatCustomProbe(config)
    default:
        log.Printf(
            "Unknown probe: %s", config.Name)
        return nil
    }
}
```

### 5. Add Database Migration

Add a migration to `/collector/src/database/schema.go`, and create the metrics
table through `sm.execMetricsDDL` rather than `tx.Exec`. In the default mode,
`execMetricsDDL` runs the DDL block as written; when the collector stores
metrics in pgEdge ColdFront, the function creates the table through
`coldfront.create_iceberg_table` instead. In the following example, the
migration creates the metrics table:

```go
// Migration N: Create pg_stat_custom table
sm.migrations = append(sm.migrations, Migration{
    Version:     N,
    Description: "Create pg_stat_custom table",
    Up: func(tx pgx.Tx) error {
        ctx := context.Background()
        err := sm.execMetricsDDL(ctx, tx, "pg_stat_custom", `
            CREATE TABLE IF NOT EXISTS
                metrics.pg_stat_custom (
                connection_id INTEGER NOT NULL,
                collected_at TIMESTAMP NOT NULL,
                metric_name TEXT NOT NULL,
                metric_value DOUBLE PRECISION,
                recorded_at TIMESTAMP,
                PRIMARY KEY (
                    connection_id,
                    collected_at,
                    metric_name
                )
            ) PARTITION BY RANGE (collected_at);

            COMMENT ON TABLE
                metrics.pg_stat_custom IS
                'Custom metrics from servers';
        `)
        if err != nil {
            return fmt.Errorf(
                "failed to create table: %w", err)
        }
        return nil
    },
})
```

In the same change, add the table's column specification to the `tables` list
in `/collector/src/coldfront/spec.go`. The specification lists every column in
order with the type that `format_type()` reports, and `CreatedIn` names the
migration that creates the table. In the following example, the entry matches
the migration above:

```go
{Name: "pg_stat_custom", CreatedIn: N, Columns: []Column{
    {Name: "connection_id", PGType: "integer"},
    {Name: "collected_at", PGType: "timestamp without time zone"},
    {Name: "metric_name", PGType: "text"},
    {Name: "metric_value", PGType: "double precision"},
    {Name: "recorded_at", PGType: "timestamp without time zone"},
}},
```

Under ColdFront, `execMetricsDDL` checks the `CREATE TABLE` statement against
the specification and refuses a table that has none, and the
`TestColdFrontSpecMatchesMigratedSchema` test fails when the two differ.
ColdFront stores an unbounded `NUMERIC` column as `numeric(38,0)`, which
discards any fractional part, so use `DOUBLE PRECISION` or a bounded
`NUMERIC(p,s)` for a value that is not a whole number. A later migration that
adds a column must give the column a type that ColdFront stores natively, and
must set the column's `AddedIn` field to that migration's version.

### 6. Insert Probe Configuration

After deploying, insert the probe configuration into the database. In the
following example, the INSERT statement adds the probe configuration:

```sql
INSERT INTO probe_configs (
    name,
    description,
    collection_interval_seconds,
    retention_days,
    is_enabled
) VALUES (
    'pg_stat_custom',
    'Collects custom application metrics',
    300,
    7,
    TRUE
);
```

### 7. Test the Probe

Write tests in `/collector/src/probes/pg_stat_custom_probe_test.go`. In the
following example, the test verifies the probe configuration:

```go
package probes

import (
    "context"
    "testing"
)

func TestPgStatCustomProbe(t *testing.T) {
    config := &ProbeConfig{
        Name:                      "pg_stat_custom",
        CollectionIntervalSeconds: 300,
        RetentionDays:             7,
        IsEnabled:                 true,
    }

    probe := NewPgStatCustomProbe(config)

    if probe.GetName() != "pg_stat_custom" {
        t.Errorf(
            "Expected name 'pg_stat_custom', got '%s'",
            probe.GetName())
    }

    if probe.IsDatabaseScoped() != false {
        t.Error("Expected server-scoped probe")
    }

    query := probe.GetQuery()
    if query == "" {
        t.Error("Query is empty")
    }
}
```

## Database-Scoped Probes

For probes that run per-database, set the `IsDatabaseScoped` method to return
`true`. In the following example, the method indicates a database-scoped probe:

```go
func (p *PgStatCustomProbe) IsDatabaseScoped() bool {
    return true
}
```

The scheduler will automatically query `pg_database` for a list of databases,
execute the probe against each database, and store all collected metrics
together.

## Best Practices

Follow these best practices when designing probes.

### Query Design

The following guidelines apply to query design:

- Limit result set size by using WHERE clauses to filter results.
- Avoid expensive operations by minimizing sorts or aggregates if possible.
- Handle NULL values by using COALESCE where appropriate.
- Use appropriate types by matching PostgreSQL types.

### Performance

The following guidelines apply to performance:

- Set appropriate intervals to balance freshness against load.
- Consider result set size because large results require more memory.
- Test query performance by running EXPLAIN on the query.
- Ensure the views have appropriate indexes.

### Error Handling

The following guidelines apply to error handling:

- Handle missing tables or views by checking whether the extension is
  installed.
- Implement graceful degradation so the probe does not fail when the extension
  is missing.
- Log errors clearly by including the probe name and connection.

### Storage

The following guidelines apply to storage:

- Match column order because the columns list must match the values order.
- Handle all data types by testing with various data.
- Consider partition size, which is retention multiplied by interval and result
  size.

## Troubleshooting

This section covers common issues when adding probes.

### Probe Not Executing

If the probe is not executing, check the following:

1. Verify the probe is enabled in the `probes` table.
2. Restart the Collector after adding the probe.
3. Check the logs for a probe registration message.
4. Verify the probe name matches everywhere.

### No Data Collected

If no data is collected, check the following:

1. Verify the query returns data on the monitored server.
2. Verify the view or table exists on the monitored server.
3. Check the user permissions.
4. Review the logs for execution errors.

### Storage Errors

If storage errors occur, check the following:

1. Verify the table exists in the metrics schema.
2. Check that the column names match.
3. Verify the data types are compatible.
4. Check that a partition exists.

### High Memory Usage

If memory usage is high, consider the following:

1. Reduce the result set size by adding a WHERE clause.
2. Increase the collection interval.
3. Check for memory leaks in the probe code.

## See Also

The following resources provide additional details.

- [Probes](probes.md) explains how probes work internally.
- [Probe Reference](probe-reference.md) lists all existing probes.
- [Testing and Development](testing.md) covers the development setup.
