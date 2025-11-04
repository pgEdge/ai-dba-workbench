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
	"sync"
	"time"
)

// ProbeScheduler manages the execution of monitoring probes
type ProbeScheduler struct {
	datastore    *Datastore
	poolManager  *MonitoredConnectionPoolManager
	serverSecret string
	probes       map[string]MetricsProbe
	shutdownChan chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// NewProbeScheduler creates a new probe scheduler
func NewProbeScheduler(datastore *Datastore, poolManager *MonitoredConnectionPoolManager, serverSecret string) *ProbeScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProbeScheduler{
		datastore:    datastore,
		poolManager:  poolManager,
		serverSecret: serverSecret,
		probes:       make(map[string]MetricsProbe),
		shutdownChan: make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start begins the probe scheduling loop
func (ps *ProbeScheduler) Start(ctx context.Context) error {
	// Load probe configurations from database
	conn, err := ps.datastore.GetConnection()
	if err != nil {
		return err
	}

	configs, err := LoadProbeConfigs(ctx, conn)
	if err != nil {
		if rerr := ps.datastore.ReturnConnection(conn); rerr != nil {
			log.Printf("Error returning connection: %v", rerr)
		}
		return err
	}

	if rerr := ps.datastore.ReturnConnection(conn); rerr != nil {
		log.Printf("Error returning connection: %v", rerr)
	}

	// Initialize probes
	for _, config := range configs {
		probe := ps.createProbe(&config)
		if probe != nil {
			ps.probes[config.Name] = probe
			log.Printf("Initialized probe: %s (interval: %ds, retention: %dd)",
				config.Name, config.CollectionIntervalSeconds, config.RetentionDays)
		}
	}

	// Start scheduling goroutines for each probe
	for _, probe := range ps.probes {
		ps.wg.Add(1)
		go ps.scheduleProbe(probe)
	}

	log.Printf("Probe scheduler started with %d probe(s)", len(ps.probes))
	return nil
}

// scheduleProbe runs a probe at its configured interval
func (ps *ProbeScheduler) scheduleProbe(probe MetricsProbe) {
	defer ps.wg.Done()

	config := probe.GetConfig()
	ticker := time.NewTicker(time.Duration(config.CollectionIntervalSeconds) * time.Second)
	defer ticker.Stop()

	// Run immediately on startup
	ps.executeProbe(ps.ctx, probe)

	for {
		select {
		case <-ps.shutdownChan:
			log.Printf("Stopping probe scheduler for %s", config.Name)
			return
		case <-ps.ctx.Done():
			log.Printf("Context cancelled, stopping probe scheduler for %s", config.Name)
			return
		case <-ticker.C:
			ps.executeProbe(ps.ctx, probe)
		}
	}
}

// executeProbe executes a probe against all monitored connections
func (ps *ProbeScheduler) executeProbe(ctx context.Context, probe MetricsProbe) {
	config := probe.GetConfig()

	// Get all monitored connections
	connections, err := ps.datastore.GetMonitoredConnections()
	if err != nil {
		log.Printf("Error getting monitored connections for probe %s: %v", config.Name, err)
		return
	}

	if len(connections) == 0 {
		return // No connections to monitor
	}

	// Execute probe against each connection in parallel
	var wg sync.WaitGroup
	for _, conn := range connections {
		wg.Add(1)
		go func(connection MonitoredConnection) {
			defer wg.Done()
			ps.executeProbeForConnection(ctx, probe, connection)
		}(conn)
	}

	wg.Wait()
}

// executeProbeForConnection executes a probe against a single monitored connection
func (ps *ProbeScheduler) executeProbeForConnection(ctx context.Context, probe MetricsProbe, conn MonitoredConnection) {
	config := probe.GetConfig()

	// Create a timeout context for this probe execution (30 seconds)
	// This ensures that if a connection hangs, we don't wait forever
	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if probe.IsDatabaseScoped() {
		// Execute probe for each database
		ps.executeProbeForAllDatabases(execCtx, probe, conn)
	} else {
		// Execute probe once for the connection
		ps.executeProbeForSingleDatabase(execCtx, probe, conn, "")
	}

	// Check if we hit the timeout
	if execCtx.Err() == context.DeadlineExceeded {
		log.Printf("Probe %s execution timed out for connection %s", config.Name, conn.Name)
	}
}

// executeProbeForAllDatabases executes a database-scoped probe for all databases
func (ps *ProbeScheduler) executeProbeForAllDatabases(ctx context.Context, probe MetricsProbe, conn MonitoredConnection) {
	config := probe.GetConfig()

	// Check if context is already cancelled
	if ctx.Err() != nil {
		log.Printf("Context cancelled before getting connection for probe %s on %s: %v",
			config.Name, conn.Name, ctx.Err())
		return
	}

	// Get connection to query pg_database
	monitoredDB, err := ps.poolManager.GetConnection(ctx, conn, ps.serverSecret)
	if err != nil {
		// Check if this was a timeout/cancellation
		if ctx.Err() != nil {
			log.Printf("Context error while getting connection to %s for probe %s: %v (original error: %v)",
				conn.Name, config.Name, ctx.Err(), err)
		} else {
			log.Printf("Error getting connection to monitored database %s for probe %s: %v",
				conn.Name, config.Name, err)
		}
		return
	}
	defer func() {
		if rerr := ps.poolManager.ReturnConnection(conn.ID, monitoredDB); rerr != nil {
			log.Printf("Error returning monitored connection: %v", rerr)
		}
	}()

	// Query pg_database to get list of databases
	databases, err := ps.getDatabaseList(ctx, monitoredDB)
	if err != nil {
		// Check if this was a timeout/cancellation
		if ctx.Err() != nil {
			log.Printf("Context error while getting database list for probe %s on %s: %v (original error: %v)",
				config.Name, conn.Name, ctx.Err(), err)
		} else {
			log.Printf("Error getting database list for probe %s on connection %s: %v",
				config.Name, conn.Name, err)
		}
		return
	}

	log.Printf("Discovered %d database(s) for probe %s on connection %s: %v",
		len(databases), config.Name, conn.Name, databases)

	// Execute probe for each database
	for _, dbName := range databases {
		ps.executeProbeForSingleDatabase(ctx, probe, conn, dbName)
	}
}

// getDatabaseList queries pg_database to get list of accessible databases
func (ps *ProbeScheduler) getDatabaseList(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT datname
		FROM pg_database
		WHERE datallowconn = true
		  AND NOT datistemplate
		ORDER BY datname
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query pg_database: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			log.Printf("Error closing rows: %v", cerr)
		}
	}()

	var databases []string
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			return nil, fmt.Errorf("failed to scan database name: %w", err)
		}
		databases = append(databases, dbName)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating database list: %w", err)
	}

	return databases, nil
}

// executeProbeForSingleDatabase executes a probe against a single database
func (ps *ProbeScheduler) executeProbeForSingleDatabase(ctx context.Context, probe MetricsProbe, conn MonitoredConnection, databaseName string) {
	config := probe.GetConfig()

	// Check if context is already cancelled
	if ctx.Err() != nil {
		dbInfo := formatDatabaseInfo(conn.Name, databaseName)
		log.Printf("Context cancelled before getting connection for probe %s on %s: %v",
			config.Name, dbInfo, ctx.Err())
		return
	}

	// Get connection to the specific database
	monitoredDB, err := ps.poolManager.GetConnectionForDatabase(ctx, conn, databaseName, ps.serverSecret)
	if err != nil {
		dbInfo := formatDatabaseInfo(conn.Name, databaseName)
		// Check if this was a timeout/cancellation
		if ctx.Err() != nil {
			log.Printf("Context error while getting connection to %s for probe %s: %v (original error: %v)",
				dbInfo, config.Name, ctx.Err(), err)
		} else {
			log.Printf("Error getting connection to monitored database %s for probe %s: %v",
				dbInfo, config.Name, err)
		}
		return
	}
	defer func() {
		if rerr := ps.poolManager.ReturnConnection(conn.ID, monitoredDB); rerr != nil {
			log.Printf("Error returning monitored connection: %v", rerr)
		}
	}()

	// Execute probe
	timestamp := time.Now()
	metrics, err := probe.Execute(ctx, monitoredDB)
	if err != nil {
		dbInfo := formatDatabaseInfo(conn.Name, databaseName)
		log.Printf("Error executing probe %s on connection %s: %v",
			config.Name, dbInfo, err)
		return
	}

	if len(metrics) == 0 {
		return // No metrics to store
	}

	// Add database name to metrics for database-scoped probes
	if databaseName != "" {
		for i := range metrics {
			metrics[i]["_database_name"] = databaseName
		}
	}

	// Get datastore connection
	datastoreDB, err := ps.datastore.GetConnection()
	if err != nil {
		log.Printf("Error getting datastore connection for probe %s: %v", config.Name, err)
		return
	}
	defer func() {
		if rerr := ps.datastore.ReturnConnection(datastoreDB); rerr != nil {
			log.Printf("Error returning datastore connection: %v", rerr)
		}
	}()

	// Store metrics
	err = probe.Store(ctx, datastoreDB, conn.ID, timestamp, metrics)
	if err != nil {
		dbInfo := formatDatabaseInfo(conn.Name, databaseName)
		log.Printf("Error storing metrics for probe %s on connection %s: %v",
			config.Name, dbInfo, err)
		return
	}

	dbInfo := formatDatabaseInfo(conn.Name, databaseName)
	log.Printf("Probe %s collected %d metric(s) from connection %s",
		config.Name, len(metrics), dbInfo)
}

// createProbe creates a probe instance based on the configuration
func (ps *ProbeScheduler) createProbe(config *ProbeConfig) MetricsProbe {
	switch config.Name {
	case "pg_stat_activity":
		return NewPgStatActivityProbe(config)
	case "pg_stat_all_tables":
		return NewPgStatAllTablesProbe(config)
	case "pg_stat_statements":
		return NewPgStatStatementsProbe(config)
	default:
		log.Printf("Warning: unknown probe type: %s", config.Name)
		return nil
	}
}

// Stop stops the probe scheduler
func (ps *ProbeScheduler) Stop() {
	log.Println("Stopping probe scheduler...")
	// Cancel the context to interrupt any pending operations
	ps.cancel()
	// Close the shutdown channel to signal goroutines
	close(ps.shutdownChan)
	// Wait for all goroutines to finish
	ps.wg.Wait()
	log.Println("Probe scheduler stopped")
}
