/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package probes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/logger"
)

// loadProbeConfigsQuery is the SELECT used by LoadProbeConfigs.
//
// The WHERE clause restricts the result set to scope IN ('global',
// 'server'). Cluster- and group-scoped rows also have connection_id
// IS NULL and would otherwise collapse into the global-default bucket
// (key 0) when grouped by COALESCE(connection_id, 0). Cluster and
// group inheritance is resolved separately by EnsureProbeConfig when
// a server-level row is materialized for each connection.
const loadProbeConfigsQuery = `
        SELECT id, name, description, collection_interval_seconds, retention_days, is_enabled, connection_id
        FROM probe_configs
        WHERE is_enabled = TRUE
          AND scope IN ('global', 'server')
        ORDER BY COALESCE(connection_id, 0), name
    `

// LoadProbeConfigs loads enabled global and server-scoped probe
// configurations from the database. The returned map keys server-scoped
// rows by their connection_id and global rows by 0.
//
// Cluster- and group-scoped rows are intentionally excluded because they
// also have connection_id IS NULL and would otherwise collapse into the
// global-default bucket (key 0); cluster and group inheritance is
// resolved separately by EnsureProbeConfig when a server-level row is
// materialized for each connection.
func LoadProbeConfigs(ctx context.Context, conn *pgxpool.Conn) (map[int][]ProbeConfig, error) {
	rows, err := conn.Query(ctx, loadProbeConfigsQuery)
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

	// Determine values for the new server-level config from the
	// resolved parent (or hardcoded defaults if no parent matched).
	interval, retention, description, isEnabled := resolveProbeConfigDefaults(probeName, &parentConfig, found)

	// Insert a new server-level config for this connection
	err = conn.QueryRow(ctx, `
        INSERT INTO probe_configs (name, description, collection_interval_seconds, retention_days, is_enabled, connection_id, scope)
        VALUES ($1, $2, $3, $4, $5, $6, 'server')
        RETURNING id, name, description, collection_interval_seconds, retention_days, is_enabled, connection_id
    `, probeName, description, interval, retention, isEnabled, connectionID).Scan(
		&config.ID, &config.Name, &config.Description,
		&config.CollectionIntervalSeconds, &config.RetentionDays, &config.IsEnabled,
		&config.ConnectionID)

	if err != nil {
		return nil, fmt.Errorf("failed to insert probe config: %w", err)
	}

	logger.Infof("Created probe config for %s on connection %d (interval: %ds, retention: %dd, enabled: %t)",
		probeName, connectionID, interval, retention, isEnabled)

	return &config, nil
}

// resolveProbeConfigDefaults returns the (interval, retention,
// description, isEnabled) tuple to use when materializing a new
// server-level probe_configs row.
//
// When found is true, all four values are inherited from parentConfig
// so a disabled cluster/group/global override is not silently
// re-enabled at the server scope. When found is false, the function
// falls back to hardcoded defaults: the per-probe default interval,
// 28 days of retention, a generated description, and is_enabled=TRUE.
//
// The function is pure (no I/O, no shared state); callers must pass a
// non-nil parentConfig only when found is true. When found is false
// parentConfig is ignored and may be nil.
func resolveProbeConfigDefaults(probeName string, parentConfig *ProbeConfig, found bool) (int, int, string, bool) {
	if found && parentConfig != nil {
		return parentConfig.CollectionIntervalSeconds,
			parentConfig.RetentionDays,
			parentConfig.Description,
			parentConfig.IsEnabled
	}

	return getDefaultInterval(probeName),
		28,
		fmt.Sprintf("Configuration for %s probe", probeName),
		true
}

// getDefaultInterval returns the default collection interval for a probe based on its name
func getDefaultInterval(probeName string) int {
	defaultIntervals := map[string]int{
		// Server-wide probes
		ProbeNamePgStatActivity:           60,
		ProbeNamePgStatReplication:        30,
		ProbeNamePgReplicationSlots:       300,
		ProbeNamePgStatRecoveryPrefetch:   600,
		ProbeNamePgStatSubscription:       300,
		ProbeNamePgStatConnectionSecurity: 300,
		ProbeNamePgStatIO:                 900,
		ProbeNamePgStatCheckpointer:       600,
		ProbeNamePgStatWAL:                600,
		ProbeNamePgSettings:               3600,
		ProbeNamePgHbaFileRules:           3600,
		ProbeNamePgIdentFileMappings:      3600,
		ProbeNamePgServerInfo:             3600,
		ProbeNamePgNodeRole:               300,
		ProbeNamePgConnectivity:           30,
		ProbeNamePgDatabase:               300,

		// Database-scoped probes
		ProbeNamePgStatDatabase:          300,
		ProbeNamePgStatDatabaseConflicts: 300,
		ProbeNamePgStatAllTables:         300,
		ProbeNamePgStatAllIndexes:        300,
		ProbeNamePgStatioAllSequences:    300,
		ProbeNamePgStatUserFunctions:     300,
		ProbeNamePgStatStatements:        300,
		ProbeNamePgExtension:             3600,

		// System stats probes
		ProbeNamePgSysOsInfo:             3600,
		ProbeNamePgSysCPUInfo:            3600,
		ProbeNamePgSysCPUUsageInfo:       60,
		ProbeNamePgSysMemoryInfo:         300,
		ProbeNamePgSysIoAnalysisInfo:     300,
		ProbeNamePgSysDiskInfo:           300,
		ProbeNamePgSysLoadAvgInfo:        60,
		ProbeNamePgSysProcessInfo:        300,
		ProbeNamePgSysNetworkInfo:        300,
		ProbeNamePgSysCPUMemoryByProcess: 300,
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
