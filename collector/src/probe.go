/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// ProbeConfig represents the configuration for a probe
type ProbeConfig struct {
	ID                        int
	Name                      string
	Description               string
	CollectionIntervalSeconds int
	RetentionDays             int
	IsEnabled                 bool
}

// MetricsProbe represents a monitoring probe that collects metrics
type MetricsProbe interface {
	// GetName returns the probe name
	GetName() string

	// GetTableName returns the metrics table name (without schema)
	GetTableName() string

	// GetQuery returns the SQL query to execute on the monitored connection
	GetQuery() string

	// Execute runs the probe against a monitored connection and returns metrics
	Execute(ctx context.Context, monitoredDB *sql.DB) ([]map[string]interface{}, error)

	// Store stores the collected metrics in the datastore using COPY protocol
	Store(ctx context.Context, datastoreDB *sql.DB, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error

	// EnsurePartition ensures a partition exists for the given timestamp
	EnsurePartition(ctx context.Context, datastoreDB *sql.DB, timestamp time.Time) error

	// GetConfig returns the probe configuration
	GetConfig() *ProbeConfig

	// IsDatabaseScoped returns true if the probe should be executed for each database
	IsDatabaseScoped() bool
}

// BaseMetricsProbe provides common probe functionality
type BaseMetricsProbe struct {
	config *ProbeConfig
}

// GetConfig returns the probe configuration
func (bp *BaseMetricsProbe) GetConfig() *ProbeConfig {
	return bp.config
}

// EnsurePartition creates a partition for the given week if it doesn't exist
func EnsurePartition(ctx context.Context, db *sql.DB, tableName string, timestamp time.Time) error {
	// Calculate the start and end of the week containing the timestamp
	// Use Monday as the start of week
	weekday := timestamp.Weekday()
	daysFromMonday := int(weekday)
	if weekday == time.Sunday {
		daysFromMonday = 6
	} else {
		daysFromMonday--
	}

	weekStart := timestamp.AddDate(0, 0, -daysFromMonday).Truncate(24 * time.Hour)
	weekEnd := weekStart.AddDate(0, 0, 7)

	// Format partition name as tablename_YYYYMMDD (start of week)
	partitionName := fmt.Sprintf("%s_%s", tableName, weekStart.Format("20060102"))
	fullTableName := fmt.Sprintf("metrics.%s", tableName)
	fullPartitionName := fmt.Sprintf("metrics.%s", partitionName)

	// Check if partition already exists
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_tables
			WHERE schemaname = 'metrics'
			AND tablename = $1
		)
	`, partitionName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if partition exists: %w", err)
	}

	if exists {
		return nil
	}

	// Create the partition
	// #nosec G201 - table names are not user-provided, they come from probe definitions
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
		PARTITION OF %s
		FOR VALUES FROM ('%s') TO ('%s')
	`, fullPartitionName, fullTableName,
		weekStart.Format("2006-01-02 15:04:05"),
		weekEnd.Format("2006-01-02 15:04:05"))

	_, err = db.ExecContext(ctx, createSQL)
	if err != nil {
		return fmt.Errorf("failed to create partition %s: %w", partitionName, err)
	}

	log.Printf("Created partition %s for table %s", partitionName, tableName)
	return nil
}

// DropExpiredPartitions drops partitions that contain only expired data
func DropExpiredPartitions(ctx context.Context, db *sql.DB, tableName string, retentionDays int) error {
	// Calculate the cutoff timestamp
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	// Find partitions that are entirely before the cutoff
	// #nosec G201 - table name is not user-provided, it comes from probe definitions
	query := fmt.Sprintf(`
		SELECT
			c.relname AS partition_name,
			pg_get_expr(c.relpartbound, c.oid) AS partition_bound
		FROM pg_class c
		JOIN pg_namespace n ON c.relnamespace = n.oid
		JOIN pg_inherits i ON c.oid = i.inhrelid
		JOIN pg_class p ON i.inhparent = p.oid
		WHERE n.nspname = 'metrics'
		AND p.relname = '%s'
		AND c.relkind = 'r'
		ORDER BY c.relname
	`, tableName)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query partitions: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("Error closing rows: %v", cerr)
		}
	}()

	var droppedCount int
	for rows.Next() {
		var partitionName, partitionBound string
		if err := rows.Scan(&partitionName, &partitionBound); err != nil {
			return fmt.Errorf("failed to scan partition info: %w", err)
		}

		// Parse the partition bound to get the end timestamp
		// Format is: FOR VALUES FROM ('...') TO ('...')
		// We need to extract the TO timestamp
		var year, month, day, hour, minute, second int
		_, err := fmt.Sscanf(partitionBound, "FOR VALUES FROM ('%*[^']') TO ('%d-%d-%d %d:%d:%d')",
			&year, &month, &day, &hour, &minute, &second)
		if err != nil {
			log.Printf("Warning: failed to parse partition bound for %s: %v", partitionName, err)
			continue
		}

		// Construct time.Time from parsed components
		endTimestamp := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)

		// If the partition end time is before the cutoff, drop it
		if endTimestamp.Before(cutoff) {
			dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS metrics.%s", partitionName)
			if _, err := db.ExecContext(ctx, dropSQL); err != nil {
				log.Printf("Warning: failed to drop partition %s: %v", partitionName, err)
				continue
			}
			log.Printf("Dropped expired partition %s (end: %s, cutoff: %s)",
				partitionName, endTimestamp.Format("2006-01-02"), cutoff.Format("2006-01-02"))
			droppedCount++
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating partitions: %w", err)
	}

	if droppedCount > 0 {
		log.Printf("Dropped %d expired partition(s) for table %s", droppedCount, tableName)
	}

	return nil
}

// StoreMetricsWithCopy stores metrics using batched INSERT statements
// Note: Originally used COPY protocol, but pq.CopyIn() doesn't support partitioned tables
func StoreMetricsWithCopy(ctx context.Context, db *sql.DB, tableName string, columns []string, values [][]interface{}) error {
	if len(values) == 0 {
		return nil // Nothing to store
	}

	fullTableName := fmt.Sprintf("metrics.%s", tableName)

	// Begin transaction
	txn, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			if rerr := txn.Rollback(); rerr != nil {
				log.Printf("Error rolling back transaction: %v", rerr)
			}
		}
	}()

	// Build multi-value INSERT statement
	// INSERT INTO table (col1, col2, ...) VALUES ($1, $2, ...), ($N+1, $N+2, ...), ...
	const batchSize = 100 // Insert up to 100 rows per statement

	for i := 0; i < len(values); i += batchSize {
		end := i + batchSize
		if end > len(values) {
			end = len(values)
		}
		batch := values[i:end]

		// Build column list
		columnList := ""
		for idx, col := range columns {
			if idx > 0 {
				columnList += ", "
			}
			columnList += col
		}

		// Build VALUES clause with placeholders
		valuesClause := ""
		args := make([]interface{}, 0, len(batch)*len(columns))
		for rowIdx, row := range batch {
			if rowIdx > 0 {
				valuesClause += ", "
			}
			valuesClause += "("
			for colIdx := range columns {
				if colIdx > 0 {
					valuesClause += ", "
				}
				placeholderNum := rowIdx*len(columns) + colIdx + 1
				valuesClause += fmt.Sprintf("$%d", placeholderNum)
				args = append(args, row[colIdx])
			}
			valuesClause += ")"
		}

		// Execute INSERT
		query := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", fullTableName, columnList, valuesClause)
		if _, err := txn.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to execute INSERT: %w", err)
		}
	}

	// Commit transaction
	if err := txn.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// LoadProbeConfigs loads all enabled probe configurations from the database
func LoadProbeConfigs(ctx context.Context, db *sql.DB) ([]ProbeConfig, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, description, collection_interval_seconds, retention_days, is_enabled
		FROM probe_configs
		WHERE is_enabled = TRUE
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query probe configs: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("Error closing rows: %v", cerr)
		}
	}()

	var configs []ProbeConfig
	for rows.Next() {
		var config ProbeConfig
		if err := rows.Scan(&config.ID, &config.Name, &config.Description,
			&config.CollectionIntervalSeconds, &config.RetentionDays, &config.IsEnabled); err != nil {
			return nil, fmt.Errorf("failed to scan probe config: %w", err)
		}
		configs = append(configs, config)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating probe configs: %w", err)
	}

	return configs, nil
}
