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
	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// loadProbeConfigsQuery is the SELECT used by LoadProbeConfigs.
//
// The WHERE clause restricts the result set to scope IN ('global',
// 'server'). Cluster- and group-scoped rows also have connection_id
// IS NULL and would otherwise collapse into the global-default bucket
// (key 0) when grouped by COALESCE(connection_id, 0). Cluster and
// group inheritance is resolved separately by EnsureProbeConfig when
// a server-level row is materialized for each connection, and by
// SyncInheritedProbeConfigs on every reload.
//
// Disabled rows are deliberately included: the scheduler has to see a
// disabled server-level row to stop the probe it is running, and a
// disabled global row still names a probe that a server-level
// override may enable (issue #811).
const loadProbeConfigsQuery = `
        SELECT id, name, description, collection_interval_seconds, retention_days, is_enabled, connection_id
        FROM probe_configs
        WHERE scope IN ('global', 'server')
        ORDER BY COALESCE(connection_id, 0), name
    `

// syncInheritedProbeConfigsQuery copies the effective parent settings
// onto every server-level row that no user has overridden.
//
// A server-level row is materialized by EnsureProbeConfig with
// user_modified = FALSE, as a copy of the first parent that resolves
// (cluster, then group, then global). Without this statement the copy
// never changes again, so later edits to Probe Defaults or to a
// cluster or group override would never reach a connection that is
// already monitored (issue #810).
//
// The joins follow the same resolution order as EnsureProbeConfig.
// The partial unique indexes on probe_configs allow at most one row
// per join, and the copied columns are NOT NULL, so COALESCE over the
// three parents picks every value from the first parent that exists.
// Rows with no parent at all keep the hardcoded defaults they were
// created with, and rows already in step are left untouched.
var syncInheritedProbeConfigsQuery = sqlmarker.Tag(`
        UPDATE probe_configs AS t
        SET is_enabled = r.is_enabled,
            collection_interval_seconds = r.collection_interval_seconds,
            retention_days = r.retention_days,
            updated_at = NOW()
        FROM (
            SELECT s.id,
                   COALESCE(cl.is_enabled, gr.is_enabled, gl.is_enabled) AS is_enabled,
                   COALESCE(cl.collection_interval_seconds, gr.collection_interval_seconds,
                            gl.collection_interval_seconds) AS collection_interval_seconds,
                   COALESCE(cl.retention_days, gr.retention_days, gl.retention_days) AS retention_days
            FROM probe_configs s
            JOIN connections c ON c.id = s.connection_id
            LEFT JOIN clusters k ON k.id = c.cluster_id
            LEFT JOIN probe_configs cl
                   ON cl.scope = 'cluster' AND cl.cluster_id = c.cluster_id AND cl.name = s.name
            LEFT JOIN probe_configs gr
                   ON gr.scope = 'group' AND gr.group_id = k.group_id AND gr.name = s.name
            LEFT JOIN probe_configs gl
                   ON gl.scope = 'global' AND gl.connection_id IS NULL AND gl.name = s.name
            WHERE s.scope = 'server'
              AND NOT s.user_modified
        ) AS r
        WHERE t.id = r.id
          AND r.is_enabled IS NOT NULL
          AND (t.is_enabled, t.collection_interval_seconds, t.retention_days)
              IS DISTINCT FROM (r.is_enabled, r.collection_interval_seconds, r.retention_days)
    `)

// SyncInheritedProbeConfigs brings every server-level probe_configs row
// that no user has overridden (user_modified = FALSE) back in step
// with the cluster, group or global row it inherits from, and returns
// the number of rows it changed. Rows a user has overridden are never
// touched.
func SyncInheritedProbeConfigs(ctx context.Context, conn *pgxpool.Conn) (int64, error) {
	tag, err := conn.Exec(ctx, syncInheritedProbeConfigsQuery)
	if err != nil {
		return 0, fmt.Errorf("failed to sync inherited probe configs: %w", err)
	}
	return tag.RowsAffected(), nil
}

// LoadProbeConfigs loads global and server-scoped probe configurations
// from the database, enabled or not; callers check IsEnabled. The
// returned map keys server-scoped rows by their connection_id and
// global rows by 0.
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

// lastCollectionTimeQuery builds the query used by
// GetLastCollectionTime. The statement runs against the Workbench's own
// datastore on every scheduling decision, so it is tagged as
// Workbench-internal to keep it out of the server's Top Queries panel
// when monitoring queries are hidden.
//
// The probe name names the metrics table, so it is quoted with
// pgx.Identifier rather than bound: a relation cannot be supplied as a
// placeholder. The connection ID is bound as $1.
//
// The G201 suppression below sits on the formatting call itself rather
// than on this comment, because a doc-comment annotation would silence
// the rule for the whole function body.
func lastCollectionTimeQuery(probeName string) string {
	// #nosec G201 -- the probe name is not user-provided; it comes from
	// the probe definitions compiled into the collector, and it is
	// quoted by pgx.Identifier.
	return sqlmarker.Tag(fmt.Sprintf(`
        SELECT MAX(collected_at)
        FROM %s
        WHERE connection_id = $1
    `, pgx.Identifier{"metrics", probeName}.Sanitize()))
}

// GetLastCollectionTime queries the last collection timestamp for a probe/connection pair
// Returns the timestamp of the most recent metrics collection, or zero time if no data exists
func GetLastCollectionTime(ctx context.Context, conn *pgxpool.Conn, probeName string, connectionID int) (time.Time, error) {
	// Query the maximum collected_at timestamp for this probe and connection
	var lastCollected *time.Time
	query := lastCollectionTimeQuery(probeName)

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
