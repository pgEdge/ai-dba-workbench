# Adding New Probes

This guide explains how to add custom probes to the Collector.

## When to Add a Probe

Consider adding a probe when you need to:

- Monitor a PostgreSQL extension's views (e.g., pgBouncer, Citus)
- Collect custom application metrics stored in PostgreSQL
- Monitor specific queries or patterns
- Gather data from custom functions

## Probe Implementation Steps

### 1. Create Probe File

Create a new file in `/collector/src/probes/`:

```bash
cd /Users/dpage/git/ai-workbench/collector/src/probes
touch pg_stat_custom_probe.go
```

### 2. Implement the Probe

```go
/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package probes

import (
    "context"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/pgedge/ai-workbench/collector/src/utils"
)

// PgStatCustomProbe collects metrics from a custom view
type PgStatCustomProbe struct {
    BaseMetricsProbe
}

// NewPgStatCustomProbe creates a new custom probe
func NewPgStatCustomProbe(config *ProbeConfig) *PgStatCustomProbe {
    return &PgStatCustomProbe{
        BaseMetricsProbe: BaseMetricsProbe{config: config},
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

// IsDatabaseScoped returns true if this probe runs per-database
func (p *PgStatCustomProbe) IsDatabaseScoped() bool {
    return false  // Change to true for database-scoped probes
}

// GetQuery returns the SQL query to execute
func (p *PgStatCustomProbe) GetQuery() string {
    return `
        SELECT
            metric_name,
            metric_value,
            recorded_at
        FROM custom_metrics_view
        WHERE recorded_at > NOW() - INTERVAL '5 minutes'
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatCustomProbe) Execute(ctx context.Context,
                                   monitoredConn *pgxpool.Conn) ([]map[string]interface{}, error) {
    rows, err := monitoredConn.Query(ctx, p.GetQuery())
    if err != nil {
        return nil, fmt.Errorf("failed to execute query: %w", err)
    }
    defer rows.Close()

    return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatCustomProbe) Store(ctx context.Context,
                                  datastoreConn *pgxpool.Conn,
                                  connectionID int,
                                  timestamp time.Time,
                                  metrics []map[string]interface{}) error {
    if len(metrics) == 0 {
        return nil
    }

    // Ensure partition exists
    if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
        return fmt.Errorf("failed to ensure partition: %w", err)
    }

    // Define columns in order (must match table definition)
    columns := []string{
        "connection_id", "collected_at",
        "metric_name", "metric_value", "recorded_at",
    }

    // Build values array
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

    // Use COPY protocol to store metrics
    if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(),
                                   columns, values); err != nil {
        return fmt.Errorf("failed to store metrics: %w", err)
    }

    return nil
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgStatCustomProbe) EnsurePartition(ctx context.Context,
                                            datastoreConn *pgxpool.Conn,
                                            timestamp time.Time) error {
    return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
```

### 3. Add Constant

Add the probe name to `/collector/src/probes/constants.go`:

```go
const (
    // ... existing constants ...
    ProbeNamePgStatCustom = "pg_stat_custom"
)
```

### 4. Register in Scheduler

Add probe creation to `/collector/src/scheduler/scheduler.go` in the
`createProbe` method:

```go
func (ps *ProbeScheduler) createProbe(config *probes.ProbeConfig) probes.MetricsProbe {
    switch config.Name {
    // ... existing cases ...
    case probes.ProbeNamePgStatCustom:
        return probes.NewPgStatCustomProbe(config)
    default:
        log.Printf("Unknown probe: %s", config.Name)
        return nil
    }
}
```

### 5. Add Database Migration

Add a migration to `/collector/src/database/schema.go`:

```go
// Migration N: Create pg_stat_custom table
sm.migrations = append(sm.migrations, Migration{
    Version:     N,  // Use next sequential version
    Description: "Create pg_stat_custom metrics table",
    Up: func(conn *pgxpool.Conn) error {
        ctx := context.Background()
        _, err := conn.Exec(ctx, `
            CREATE TABLE IF NOT EXISTS metrics.pg_stat_custom (
                connection_id INTEGER NOT NULL,
                collected_at TIMESTAMP NOT NULL,
                metric_name TEXT NOT NULL,
                metric_value NUMERIC,
                recorded_at TIMESTAMP,
                PRIMARY KEY (connection_id, collected_at, metric_name)
            ) PARTITION BY RANGE (collected_at);

            COMMENT ON TABLE metrics.pg_stat_custom IS
                'Custom metrics from monitored servers';
        `)
        if err != nil {
            return fmt.Errorf("failed to create pg_stat_custom table: %w", err)
        }
        return nil
    },
})
```

### 6. Insert Probe Configuration

After deploying, insert the probe configuration into the database:

```sql
INSERT INTO probes (
    name,
    description,
    collection_interval_seconds,
    retention_days,
    is_enabled
) VALUES (
    'pg_stat_custom',
    'Collects custom application metrics',
    300,  -- 5 minutes
    7,    -- 7 days retention
    TRUE
);
```

### 7. Test the Probe

Write tests in `/collector/src/probes/pg_stat_custom_probe_test.go`:

```go
package probes

import (
    "context"
    "testing"
)

func TestPgStatCustomProbe(t *testing.T) {
    config := &ProbeConfig{
        Name: "pg_stat_custom",
        CollectionIntervalSeconds: 300,
        RetentionDays: 7,
        IsEnabled: true,
    }

    probe := NewPgStatCustomProbe(config)

    if probe.GetName() != "pg_stat_custom" {
        t.Errorf("Expected name 'pg_stat_custom', got '%s'", probe.GetName())
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

For probes that run per-database (like pg_stat_database):

```go
func (p *PgStatCustomProbe) IsDatabaseScoped() bool {
    return true
}
```

The scheduler will automatically:

1. Query `pg_database` for a list of databases
2. Execute the probe against each database
3. Store all collected metrics together

## Best Practices

### Query Design

1. **Limit result set size**: Use WHERE clauses to filter
2. **Avoid expensive operations**: No sorts or aggregates if possible
3. **Handle NULL values**: Use COALESCE where appropriate
4. **Use appropriate types**: Match PostgreSQL types

### Performance

1. **Set appropriate intervals**: Balance freshness vs. load
2. **Consider result set size**: Large results = more memory
3. **Test query performance**: Run EXPLAIN on your query
4. **Index appropriately**: Ensure views have good indexes

### Error Handling

1. **Handle missing tables/views**: Check if extension is installed
2. **Graceful degradation**: Don't fail if extension missing
3. **Log errors clearly**: Include probe name and connection

### Storage

1. **Match column order**: Columns list must match values order
2. **Handle all data types**: Test with various data
3. **Consider partition size**: Retention × interval × result size

## Example: Extension Monitoring

### Monitoring pg_stat_statements

Already included, but here's the pattern:

```go
func (p *PgStatStatementsProbe) GetQuery() string {
    return `
        SELECT
            userid, dbid, toplevel, queryid, query,
            plans, total_plan_time, min_plan_time, max_plan_time,
            mean_plan_time, stddev_plan_time,
            calls, total_exec_time, min_exec_time, max_exec_time,
            mean_exec_time, stddev_exec_time,
            rows, shared_blks_hit, shared_blks_read,
            shared_blks_dirtied, shared_blks_written,
            local_blks_hit, local_blks_read,
            local_blks_dirtied, local_blks_written,
            temp_blks_read, temp_blks_written,
            blk_read_time, blk_write_time,
            temp_blk_read_time, temp_blk_write_time,
            wal_records, wal_fpi, wal_bytes, jit_functions,
            jit_generation_time, jit_inlining_count,
            jit_inlining_time, jit_optimization_count,
            jit_optimization_time, jit_emission_count,
            jit_emission_time
        FROM pg_stat_statements
        ORDER BY total_exec_time DESC
        LIMIT 1000
    `
}
```

## Troubleshooting

### Probe not executing

1. Check if probe is enabled in `probes` table
2. Restart collector after adding probe
3. Check logs for probe registration message
4. Verify probe name matches everywhere

### No data collected

1. Check if query returns data on monitored server
2. Verify view/table exists on monitored server
3. Check user permissions
4. Review logs for execution errors

### Storage errors

1. Verify table exists in metrics schema
2. Check column names match
3. Verify data types are compatible
4. Check partition exists

### High memory usage

1. Reduce result set size (add WHERE clause)
2. Increase collection interval
3. Check for memory leaks in probe code

## See Also

- [Probes System](probes.md) - How probes work
- [Probe Reference](probe-reference.md) - List of existing probes
- [Development Guide](development.md) - Development setup
- [Testing Guide](testing.md) - Writing tests
