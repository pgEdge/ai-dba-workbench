/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
// Package probes provides metrics collection probes for PostgreSQL monitoring
package probes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// WrapQuery wraps a SQL query with a probe marker column so the server
// can identify and filter collector queries from monitoring panels.
func WrapQuery(probeName, query string) string {
	return fmt.Sprintf(
		"SELECT '%s' AS ai_dba_wb_probe, subq.* FROM (%s) AS subq",
		probeName, query,
	)
}

// featureCache stores boolean feature-detection results keyed by
// "connectionName:checkName". View and column existence checks never
// change during the lifetime of a PostgreSQL connection, so caching
// them avoids repeated catalog queries on every collection cycle.
var featureCache sync.Map

// cachedCheck returns a cached boolean result for a feature-detection
// check identified by connectionName and checkName. If no cached value
// exists, it calls checkFn, caches the result, and returns it.
func cachedCheck(connectionName, checkName string, checkFn func() (bool, error)) (bool, error) {
	key := connectionName + ":" + checkName
	if val, ok := featureCache.Load(key); ok {
		boolVal, ok2 := val.(bool)
		if !ok2 {
			return false, fmt.Errorf("cached value for %s is not a bool", key)
		}
		return boolVal, nil
	}
	result, err := checkFn()
	if err != nil {
		return false, err
	}
	featureCache.Store(key, result)
	return result, nil
}

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
	Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error)

	// Store stores the collected metrics in the datastore using COPY protocol
	Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error

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
	config         *ProbeConfig
	databaseScoped bool
}

// GetName returns the probe name
func (bp *BaseMetricsProbe) GetName() string {
	return bp.config.Name
}

// GetTableName returns the metrics table name (without schema)
func (bp *BaseMetricsProbe) GetTableName() string {
	return bp.config.Name
}

// IsDatabaseScoped returns true if the probe should be executed for each database
func (bp *BaseMetricsProbe) IsDatabaseScoped() bool {
	return bp.databaseScoped
}

// GetConfig returns the probe configuration
func (bp *BaseMetricsProbe) GetConfig() *ProbeConfig {
	return bp.config
}

// StoreMetricsWithCopy stores metrics using batched INSERT statements
// Note: Originally used COPY protocol, but pq.CopyIn() doesn't support partitioned tables
func StoreMetricsWithCopy(ctx context.Context, conn *pgxpool.Conn, tableName string, columns []string, values [][]any) error {
	if len(values) == 0 {
		return nil // Nothing to store
	}

	fullTableName := pgx.Identifier{"metrics", tableName}.Sanitize()

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

		// Build column list with quoted identifiers
		columnList := ""
		for idx, col := range columns {
			if idx > 0 {
				columnList += ", "
			}
			columnList += pgx.Identifier{col}.Sanitize()
		}

		// Build VALUES clause with placeholders
		valuesClause := ""
		args := make([]any, 0, len(batch)*len(columns))
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

// EnsureProbeConfig ensures a probe configuration exists for the given connection and probe.
// Resolution priority: server -> cluster -> group -> global -> hardcoded defaults.
// If no server-level config exists, the function creates one by copying values
// from the first parent scope that resolves.
func EnsureProbeConfig(ctx context.Context, conn *pgxpool.Conn, connectionID int, probeName string) (*ProbeConfig, error) {
	// Step 1: Check for server-level config (scope='server' AND connection_id matches)
	var config ProbeConfig
	err := conn.QueryRow(ctx, `
		SELECT id, name, description, collection_interval_seconds, retention_days, is_enabled, connection_id
		FROM probe_configs
		WHERE scope = 'server' AND name = $1 AND connection_id = $2
	`, probeName, connectionID).Scan(
		&config.ID, &config.Name, &config.Description,
		&config.CollectionIntervalSeconds, &config.RetentionDays, &config.IsEnabled,
		&config.ConnectionID)

	if err == nil {
		return &config, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to query server probe config: %w", err)
	}

	// Step 2: Check for cluster-level config (via connection -> cluster join)
	var parentConfig ProbeConfig
	found := false

	err = conn.QueryRow(ctx, `
		SELECT pc.id, pc.name, pc.description, pc.collection_interval_seconds, pc.retention_days, pc.is_enabled, pc.connection_id
		FROM probe_configs pc
		JOIN connections c ON c.cluster_id = pc.cluster_id
		WHERE pc.scope = 'cluster' AND c.id = $1 AND pc.name = $2
	`, connectionID, probeName).Scan(
		&parentConfig.ID, &parentConfig.Name, &parentConfig.Description,
		&parentConfig.CollectionIntervalSeconds, &parentConfig.RetentionDays, &parentConfig.IsEnabled,
		&parentConfig.ConnectionID)

	if err == nil {
		found = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to query cluster probe config: %w", err)
	}

	// Step 3: Check for group-level config (via connection -> cluster -> group join)
	if !found {
		err = conn.QueryRow(ctx, `
			SELECT pc.id, pc.name, pc.description, pc.collection_interval_seconds, pc.retention_days, pc.is_enabled, pc.connection_id
			FROM probe_configs pc
			JOIN clusters cl ON cl.group_id = pc.group_id
			JOIN connections c ON c.cluster_id = cl.id
			WHERE pc.scope = 'group' AND c.id = $1 AND pc.name = $2
		`, connectionID, probeName).Scan(
			&parentConfig.ID, &parentConfig.Name, &parentConfig.Description,
			&parentConfig.CollectionIntervalSeconds, &parentConfig.RetentionDays, &parentConfig.IsEnabled,
			&parentConfig.ConnectionID)

		if err == nil {
			found = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("failed to query group probe config: %w", err)
		}
	}

	// Step 4: Check for global config (scope='global' AND connection_id IS NULL)
	if !found {
		err = conn.QueryRow(ctx, `
			SELECT id, name, description, collection_interval_seconds, retention_days, is_enabled, connection_id
			FROM probe_configs
			WHERE scope = 'global' AND name = $1 AND connection_id IS NULL
		`, probeName).Scan(
			&parentConfig.ID, &parentConfig.Name, &parentConfig.Description,
			&parentConfig.CollectionIntervalSeconds, &parentConfig.RetentionDays, &parentConfig.IsEnabled,
			&parentConfig.ConnectionID)

		if err == nil {
			found = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("failed to query global probe config: %w", err)
		}
	}

	// Determine values for the new server-level config
	var interval, retention int
	var description string

	if found {
		interval = parentConfig.CollectionIntervalSeconds
		retention = parentConfig.RetentionDays
		description = parentConfig.Description
	} else {
		// Step 5: No parent config found; use hardcoded defaults
		interval = getDefaultInterval(probeName)
		retention = 28
		description = fmt.Sprintf("Configuration for %s probe", probeName)
	}

	// Insert a new server-level config for this connection
	err = conn.QueryRow(ctx, `
		INSERT INTO probe_configs (name, description, collection_interval_seconds, retention_days, is_enabled, connection_id, scope)
		VALUES ($1, $2, $3, $4, TRUE, $5, 'server')
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
		"pg_connectivity":            30,   // IntervalConnectivity (every 30 seconds)
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
