/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package probes provides metrics collection probes for PostgreSQL monitoring
package probes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// ProbeConfig represents the configuration for a probe
type ProbeConfig struct {
	ID                        int
	Name                      string
	Description               string
	CollectionIntervalSeconds int
	RetentionDays             int
	IsEnabled                 bool
	ConnectionID              *int // NULL means global default
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
	// pgVersion is the PostgreSQL major version (e.g., 14, 15, 16, 17, 18)
	Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error)

	// Store stores the collected metrics in the datastore using COPY protocol
	Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error

	// EnsurePartition ensures a partition exists for the given timestamp
	EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error

	// GetConfig returns the probe configuration
	GetConfig() *ProbeConfig

	// IsDatabaseScoped returns true if the probe should be executed for each database
	IsDatabaseScoped() bool
}

// ExtensionProbe is implemented by probes that require a PostgreSQL
// extension. The scheduler uses this to record why a probe is unavailable.
type ExtensionProbe interface {
	GetExtensionName() string
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
func EnsurePartition(ctx context.Context, conn *pgxpool.Conn, tableName string, timestamp time.Time) error {
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
	err := conn.QueryRow(ctx, `
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

	_, err = conn.Exec(ctx, createSQL)
	if err != nil {
		// Check if this is a "relation already exists" error (42P07)
		// This can happen due to race conditions when multiple goroutines
		// try to create the same partition simultaneously
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P07" {
			// Partition was created by another goroutine, that's fine
			return nil
		}
		return fmt.Errorf("failed to create partition %s: %w", partitionName, err)
	}

	logger.Infof("Created partition %s for table %s", partitionName, tableName)
	return nil
}

// DropExpiredPartitions drops partitions that contain only expired data
func DropExpiredPartitions(ctx context.Context, conn *pgxpool.Conn, tableName string, retentionDays int) error {
	// Calculate the cutoff timestamp
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	// For change-tracked probes (pg_settings, pg_hba_file_rules, pg_ident_file_mappings, pg_server_info),
	// find the most recent partition with data for each connection
	// These partitions must never be dropped, regardless of age
	protectedPartitions := make(map[string]bool)
	if tableName == "pg_settings" || tableName == "pg_hba_file_rules" || tableName == "pg_ident_file_mappings" || tableName == "pg_server_info" {
		// #nosec G201 - table name is from probe definition
		protQuery := fmt.Sprintf(`
			SELECT DISTINCT
				c.relname AS partition_name
			FROM (
				SELECT connection_id, MAX(collected_at) as max_collected_at
				FROM metrics.%s
				GROUP BY connection_id
			) latest
			JOIN metrics.%s tbl ON tbl.connection_id = latest.connection_id
				AND tbl.collected_at = latest.max_collected_at
			JOIN pg_class c ON c.oid = tbl.tableoid
		`, tableName, tableName)
		protRows, err := conn.Query(ctx, protQuery)
		if err != nil {
			return fmt.Errorf("failed to query protected partitions for %s: %w", tableName, err)
		}
		defer protRows.Close()

		for protRows.Next() {
			var partitionName string
			if err := protRows.Scan(&partitionName); err != nil {
				return fmt.Errorf("failed to scan protected partition name: %w", err)
			}
			protectedPartitions[partitionName] = true
		}

		if len(protectedPartitions) > 0 {
			logger.Infof("Protected %d partition(s) for %s containing most recent data per connection", len(protectedPartitions), tableName)
		}
	}

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

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query partitions: %w", err)
	}
	defer rows.Close()

	var droppedCount int
	for rows.Next() {
		var partitionName, partitionBound string
		if err := rows.Scan(&partitionName, &partitionBound); err != nil {
			return fmt.Errorf("failed to scan partition info: %w", err)
		}

		// Check if this partition is protected (for pg_settings)
		if protectedPartitions[partitionName] {
			logger.Infof("Skipping protected partition %s (contains most recent data for pg_settings)", partitionName)
			continue
		}

		// Parse the partition bound to get the end timestamp
		// Format is: FOR VALUES FROM ('2025-11-03 00:00:00+00') TO ('2025-11-04 00:00:00+00')
		// We need to extract the TO timestamp
		toIdx := strings.Index(partitionBound, "TO ('")
		if toIdx == -1 {
			logger.Errorf("Warning: failed to find TO clause in partition bound for %s: %s", partitionName, partitionBound)
			continue
		}

		// Extract the timestamp string after "TO ('"
		timestampStart := toIdx + 5 // len("TO ('")
		timestampEnd := strings.Index(partitionBound[timestampStart:], "'")
		if timestampEnd == -1 {
			logger.Errorf("Warning: failed to find end quote in partition bound for %s: %s", partitionName, partitionBound)
			continue
		}

		timestampStr := partitionBound[timestampStart : timestampStart+timestampEnd]

		// Try timezone formats: with minutes (+05:30), without minutes (+05), then legacy (no tz)
		var endTimestamp time.Time
		tzFormats := []string{
			"2006-01-02 15:04:05-07:00",
			"2006-01-02 15:04:05-07",
			"2006-01-02 15:04:05",
		}
		var parseErr error
		for _, layout := range tzFormats {
			endTimestamp, parseErr = time.Parse(layout, timestampStr)
			if parseErr == nil {
				break
			}
		}
		if parseErr != nil {
			logger.Errorf("Warning: failed to parse timestamp in partition bound for %s: %v", partitionName, parseErr)
			continue
		}

		// If the partition end time is before the cutoff, drop it
		if endTimestamp.Before(cutoff) {
			dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", pgx.Identifier{"metrics", partitionName}.Sanitize())
			if _, err := conn.Exec(ctx, dropSQL); err != nil {
				logger.Errorf("Warning: failed to drop partition %s: %v", partitionName, err)
				continue
			}
			logger.Infof("Dropped expired partition %s (end: %s, cutoff: %s)",
				partitionName, endTimestamp.Format("2006-01-02"), cutoff.Format("2006-01-02"))
			droppedCount++
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating partitions: %w", err)
	}

	if droppedCount > 0 {
		logger.Infof("Dropped %d expired partition(s) for table %s", droppedCount, tableName)
	}

	return nil
}

// StoreMetricsWithCopy stores metrics using batched INSERT statements
// Note: Originally used COPY protocol, but pq.CopyIn() doesn't support partitioned tables
func StoreMetricsWithCopy(ctx context.Context, conn *pgxpool.Conn, tableName string, columns []string, values [][]interface{}) error {
	if len(values) == 0 {
		return nil // Nothing to store
	}

	fullTableName := fmt.Sprintf("metrics.%s", tableName)

	// Begin transaction
	txn, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			if rerr := txn.Rollback(ctx); rerr != nil {
				logger.Errorf("Error rolling back transaction: %v", rerr)
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
		if _, err := txn.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to execute INSERT: %w", err)
		}
	}

	// Commit transaction
	if err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// LoadProbeConfigs loads all enabled probe configurations from the database
// Returns a map of connection ID to probe configs, where connection ID 0 represents global defaults
func LoadProbeConfigs(ctx context.Context, conn *pgxpool.Conn) (map[int][]ProbeConfig, error) {
	rows, err := conn.Query(ctx, `
		SELECT id, name, description, collection_interval_seconds, retention_days, is_enabled, connection_id
		FROM probe_configs
		WHERE is_enabled = TRUE
		ORDER BY COALESCE(connection_id, 0), name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query probe configs: %w", err)
	}
	defer rows.Close()

	configsByConnection := make(map[int][]ProbeConfig)
	for rows.Next() {
		var config ProbeConfig
		if err := rows.Scan(&config.ID, &config.Name, &config.Description,
			&config.CollectionIntervalSeconds, &config.RetentionDays, &config.IsEnabled,
			&config.ConnectionID); err != nil {
			return nil, fmt.Errorf("failed to scan probe config: %w", err)
		}

		// Use connection ID 0 for global defaults (NULL connection_id)
		connID := 0
		if config.ConnectionID != nil {
			connID = *config.ConnectionID
		}

		configsByConnection[connID] = append(configsByConnection[connID], config)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating probe configs: %w", err)
	}

	return configsByConnection, nil
}

// EnsureProbeConfig ensures a probe configuration exists for the given connection and probe
// If no config exists, it creates one with default values based on the probe name
func EnsureProbeConfig(ctx context.Context, conn *pgxpool.Conn, connectionID int, probeName string) (*ProbeConfig, error) {
	// First, try to find an existing config for this connection and probe
	var config ProbeConfig
	err := conn.QueryRow(ctx, `
		SELECT id, name, description, collection_interval_seconds, retention_days, is_enabled, connection_id
		FROM probe_configs
		WHERE name = $1 AND connection_id = $2
	`, probeName, connectionID).Scan(
		&config.ID, &config.Name, &config.Description,
		&config.CollectionIntervalSeconds, &config.RetentionDays, &config.IsEnabled,
		&config.ConnectionID)

	if err == nil {
		// Config exists
		return &config, nil
	}

	// If error is not "no rows", return the error
	if err.Error() != "no rows in result set" {
		return nil, fmt.Errorf("failed to query probe config: %w", err)
	}

	// Config doesn't exist, check if there's a global default
	var defaultConfig ProbeConfig
	err = conn.QueryRow(ctx, `
		SELECT id, name, description, collection_interval_seconds, retention_days, is_enabled, connection_id
		FROM probe_configs
		WHERE name = $1 AND connection_id IS NULL
	`, probeName).Scan(
		&defaultConfig.ID, &defaultConfig.Name, &defaultConfig.Description,
		&defaultConfig.CollectionIntervalSeconds, &defaultConfig.RetentionDays, &defaultConfig.IsEnabled,
		&defaultConfig.ConnectionID)

	// If no global default exists either, we'll use hardcoded defaults
	interval := defaultConfig.CollectionIntervalSeconds
	retention := defaultConfig.RetentionDays
	description := defaultConfig.Description

	if err != nil {
		// No global default exists, use hardcoded defaults from constants
		interval = getDefaultInterval(probeName)
		retention = 28 // Default retention
		description = fmt.Sprintf("Configuration for %s probe", probeName)
	}

	// Insert a new config for this connection
	err = conn.QueryRow(ctx, `
		INSERT INTO probe_configs (name, description, collection_interval_seconds, retention_days, is_enabled, connection_id)
		VALUES ($1, $2, $3, $4, TRUE, $5)
		RETURNING id, name, description, collection_interval_seconds, retention_days, is_enabled, connection_id
	`, probeName, description, interval, retention, connectionID).Scan(
		&config.ID, &config.Name, &config.Description,
		&config.CollectionIntervalSeconds, &config.RetentionDays, &config.IsEnabled,
		&config.ConnectionID)

	if err != nil {
		return nil, fmt.Errorf("failed to insert probe config: %w", err)
	}

	logger.Infof("Created probe config for %s on connection %d (interval: %ds, retention: %dd)",
		probeName, connectionID, interval, retention)

	return &config, nil
}

// getDefaultInterval returns the default collection interval for a probe based on its name
func getDefaultInterval(probeName string) int {
	// These constants need to be imported from the main package
	// For now, we'll use a map to avoid circular dependencies
	defaultIntervals := map[string]int{
		"pg_stat_replication":        30,   // IntervalReplication
		"pg_stat_wal_receiver":       30,   // IntervalWALReceiver
		"pg_stat_activity":           60,   // IntervalActivity
		"pg_stat_database":           300,  // IntervalDatabase
		"pg_stat_all_tables":         300,  // IntervalTables
		"pg_stat_all_indexes":        300,  // IntervalIndexes
		"pg_statio_all_tables":       300,  // IntervalTables
		"pg_statio_all_indexes":      300,  // IntervalIndexes
		"pg_statio_all_sequences":    300,  // IntervalDefault
		"pg_stat_user_functions":     300,  // IntervalFunctions
		"pg_stat_statements":         300,  // IntervalDefault
		"pg_stat_archiver":           600,  // IntervalArchiver
		"pg_stat_bgwriter":           600,  // IntervalBgwriter
		"pg_stat_checkpointer":       600,  // IntervalCheckpointer
		"pg_stat_wal":                600,  // IntervalWAL
		"pg_stat_slru":               600,  // IntervalSLRU
		"pg_stat_io":                 900,  // IntervalIO
		"pg_stat_subscription":       300,  // IntervalSubscription
		"pg_stat_subscription_stats": 300,  // IntervalDefault
		"pg_stat_replication_slots":  300,  // IntervalReplicationSlots
		"pg_replication_slots":       300,  // IntervalReplicationSlots
		"pg_stat_recovery_prefetch":  600,  // IntervalRecoveryPrefetch
		"pg_stat_database_conflicts": 300,  // IntervalDefault
		"pg_stat_ssl":                300,  // IntervalDefault
		"pg_stat_gssapi":             300,  // IntervalDefault
		"pg_server_info":             3600, // IntervalServerInfo (hourly, change-tracked)
		"pg_node_role":               300,  // IntervalNodeRole (every 5 minutes)
	}

	if interval, ok := defaultIntervals[probeName]; ok {
		return interval
	}

	return 300 // Default 5 minutes
}

// GetLastCollectionTime queries the last collection timestamp for a probe/connection pair
// Returns the timestamp of the most recent metrics collection, or zero time if no data exists
func GetLastCollectionTime(ctx context.Context, conn *pgxpool.Conn, probeName string, connectionID int) (time.Time, error) {
	tableName := fmt.Sprintf("metrics.%s", probeName)

	// Query the maximum collected_at timestamp for this probe and connection
	var lastCollected *time.Time
	query := fmt.Sprintf(`
		SELECT MAX(collected_at)
		FROM %s
		WHERE connection_id = $1
	`, tableName)

	err := conn.QueryRow(ctx, query, connectionID).Scan(&lastCollected)
	if err != nil {
		// If the table doesn't exist yet, that's okay - return zero time
		if strings.Contains(err.Error(), "does not exist") {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("failed to query last collection time for %s: %w", probeName, err)
	}

	// If no rows found (NULL), return zero time
	if lastCollected == nil {
		return time.Time{}, nil
	}

	return *lastCollected, nil
}

// CheckExtensionExists checks if a PostgreSQL extension is installed
// Returns true if the extension exists, false otherwise
func CheckExtensionExists(ctx context.Context, connectionName string, conn *pgxpool.Conn, extensionName string) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pg_extension WHERE extname = $1
		)
	`, extensionName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check if extension %s exists: %w", extensionName, err)
	}

	// Log extension check result for debugging
	if !exists {
		// Get connection info for better logging
		config := conn.Conn().Config()
		database := config.Database
		logger.Infof("Extension %s not found in the %s database on %s. Skipping probe.", extensionName, database, connectionName)
	}

	return exists, nil
}

// ComputeMetricsHash computes a canonical hash of metrics for change detection.
// This function normalizes the data to ensure consistent hashing regardless of
// map iteration order or minor type differences between database drivers.
func ComputeMetricsHash(metrics []map[string]interface{}) (string, error) {
	// Build a canonical representation by sorting keys and normalizing values
	var canonicalData []map[string]interface{}
	for _, m := range metrics {
		normalized := make(map[string]interface{})
		for k, v := range m {
			normalized[k] = normalizeValue(v)
		}
		canonicalData = append(canonicalData, normalized)
	}

	// Sort the slice by a deterministic key (first key alphabetically, then value)
	// This ensures consistent ordering even if rows come in different order
	sort.Slice(canonicalData, func(i, j int) bool {
		// Get sorted keys for comparison
		keysI := getSortedKeys(canonicalData[i])
		keysJ := getSortedKeys(canonicalData[j])

		// Compare by first key's value, then second, etc.
		for idx := 0; idx < len(keysI) && idx < len(keysJ); idx++ {
			if keysI[idx] != keysJ[idx] {
				return keysI[idx] < keysJ[idx]
			}
			valI := fmt.Sprintf("%v", canonicalData[i][keysI[idx]])
			valJ := fmt.Sprintf("%v", canonicalData[j][keysJ[idx]])
			if valI != valJ {
				return valI < valJ
			}
		}
		return len(keysI) < len(keysJ)
	})

	// Marshal to JSON (Go's json.Marshal sorts map keys)
	jsonBytes, err := json.Marshal(canonicalData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal metrics: %w", err)
	}

	hash := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(hash[:]), nil
}

// normalizeValue converts a value to a canonical form for comparison
func normalizeValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case []interface{}:
		// Normalize array elements
		result := make([]interface{}, len(val))
		for i, elem := range val {
			result[i] = normalizeValue(elem)
		}
		return result
	case []string:
		// Convert []string to []interface{} for consistent comparison
		result := make([]interface{}, len(val))
		for i, elem := range val {
			result[i] = elem
		}
		return result
	case []byte:
		// Convert byte arrays to string
		return string(val)
	default:
		return v
	}
}

// getSortedKeys returns the keys of a map in sorted order
func getSortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
