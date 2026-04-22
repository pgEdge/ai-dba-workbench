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
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
