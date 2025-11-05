/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package scheduler

import (
    "github.com/pgedge/ai-workbench/collector/src/probes"
    "github.com/pgedge/ai-workbench/collector/src/database"

	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ProbeScheduler manages the execution of monitoring probes
type ProbeScheduler struct {
	datastore    *database.Datastore
	poolManager  *database.MonitoredConnectionPoolManager
	serverSecret string
	config       Config
	probes       map[string]probes.MetricsProbe
	shutdownChan chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// Config interface defines the minimal configuration needed by ProbeScheduler
type Config interface {
	GetDatastorePoolMaxWaitSeconds() int
	GetMonitoredPoolMaxWaitSeconds() int
}

// NewProbeScheduler creates a new probe scheduler
func NewProbeScheduler(datastore *database.Datastore, poolManager *database.MonitoredConnectionPoolManager, config Config, serverSecret string) *ProbeScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProbeScheduler{
		datastore:    datastore,
		poolManager:  poolManager,
		serverSecret: serverSecret,
		config:       config,
		probes:       make(map[string]probes.MetricsProbe),
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

	configs, err := probes.LoadProbeConfigs(ctx, conn)
	if err != nil {
		ps.datastore.ReturnConnection(conn)
		return err
	}

	ps.datastore.ReturnConnection(conn)

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
func (ps *ProbeScheduler) scheduleProbe(probe probes.MetricsProbe) {
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
func (ps *ProbeScheduler) executeProbe(ctx context.Context, probe probes.MetricsProbe) {
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
		go func(connection database.MonitoredConnection) {
			defer wg.Done()
			ps.executeProbeForConnection(ctx, probe, connection)
		}(conn)
	}

	wg.Wait()
}

// executeProbeForConnection executes a probe against a single monitored connection
func (ps *ProbeScheduler) executeProbeForConnection(ctx context.Context, probe probes.MetricsProbe, conn database.MonitoredConnection) {
	config := probe.GetConfig()
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		log.Printf("Probe %s on %s completed in %.2fms", config.Name, conn.Name, float64(duration.Microseconds())/1000.0)
	}()

	// Create a timeout context for this probe execution using configured timeout
	// This ensures that if a connection hangs, we don't wait forever
	monitoredTimeout := time.Duration(ps.config.GetMonitoredPoolMaxWaitSeconds()) * time.Second
	execCtx, cancel := context.WithTimeout(ctx, monitoredTimeout)
	defer cancel()

	// Collect all metrics before storing
	var allMetrics []map[string]interface{}
	timestamp := time.Now()

	if probe.IsDatabaseScoped() {
		// Execute probe for each database and collect metrics
		allMetrics = ps.executeProbeForAllDatabases(execCtx, probe, conn)
	} else {
		// Execute probe once for the connection
		allMetrics = ps.executeProbeForServerWide(execCtx, probe, conn)
	}

	// Check if we hit the timeout
	if execCtx.Err() == context.DeadlineExceeded {
		log.Printf("Probe %s execution timed out for connection %s (timeout: %d seconds)",
			config.Name, conn.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
		return
	}

	// If we have metrics, store them all at once
	if len(allMetrics) > 0 {
		ps.storeMetrics(ctx, probe, conn.ID, timestamp, allMetrics)
	}
}

// executeProbeForAllDatabases executes a database-scoped probe for all databases and returns all collected metrics
func (ps *ProbeScheduler) executeProbeForAllDatabases(ctx context.Context, probe probes.MetricsProbe, conn database.MonitoredConnection) []map[string]interface{} {
	config := probe.GetConfig()
	var allMetrics []map[string]interface{}

	// Check if context is already cancelled
	if ctx.Err() != nil {
		log.Printf("Error getting connection for probe %s on %s: context already cancelled",
			config.Name, conn.Name)
		return allMetrics
	}

	// Get connection to query pg_database (connects to default database)
	monitoredDB, err := ps.poolManager.GetConnection(ctx, conn, ps.serverSecret)
	if err != nil {
		// Check if this was a timeout/cancellation
		if ctx.Err() != nil {
			log.Printf("Error getting connection to %s for probe %s: timed out after %d seconds while waiting for a connection from the monitored connection pool",
				conn.Name, config.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
		} else {
			log.Printf("Error getting connection to monitored database %s for probe %s: %v",
				conn.Name, config.Name, err)
		}
		return allMetrics
	}

	// Query pg_database to get list of databases
	databases, err := ps.getDatabaseList(ctx, monitoredDB)
	if err != nil {
		// Return connection before returning
		ps.poolManager.ReturnConnection(conn.ID, monitoredDB)

		// Check if this was a timeout/cancellation
		if ctx.Err() != nil {
			log.Printf("Error getting database list for probe %s on %s: timed out after %d seconds while waiting for a connection from the monitored connection pool",
				config.Name, conn.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
		} else {
			log.Printf("Error getting database list for probe %s on connection %s: %v",
				config.Name, conn.Name, err)
		}
		return allMetrics
	}

	log.Printf("Discovered %d database(s) for probe %s on connection %s: %v",
		len(databases), config.Name, conn.Name, databases)

	// Execute probe on the default/first database using the connection we already have
	if len(databases) > 0 {
		defaultDB := databases[0]
		metrics, err := probe.Execute(ctx, monitoredDB)
		if err != nil {
			log.Printf("Error executing probe %s on default database %s/%s: %v",
				config.Name, conn.Name, defaultDB, err)
		} else if len(metrics) > 0 {
			// Add database name to metrics
			for i := range metrics {
				metrics[i]["_database_name"] = defaultDB
			}
			allMetrics = append(allMetrics, metrics...)
			log.Printf("Probe %s collected %d metric(s) from %s/%s",
				config.Name, len(metrics), conn.Name, defaultDB)
		}
	}

	// Return the connection now that we're done with the default database
	ps.poolManager.ReturnConnection(conn.ID, monitoredDB)

	// Execute probe for remaining databases (skip the first one we already did)
	for i := 1; i < len(databases); i++ {
		dbName := databases[i]

		// Check if context is cancelled (e.g., during shutdown) before processing next database
		if ctx.Err() != nil {
			log.Printf("Stopping probe %s execution on %s due to context cancellation", config.Name, conn.Name)
			break
		}

		// Get connection for this specific database
		db, err := ps.poolManager.GetConnectionForDatabase(ctx, conn, dbName, ps.serverSecret)
		if err != nil {
			if ctx.Err() != nil {
				log.Printf("Error getting connection to %s/%s for probe %s: timed out after %d seconds while waiting for a connection from the monitored connection pool",
					conn.Name, dbName, config.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
			} else {
				log.Printf("Error getting connection to %s/%s for probe %s: %v",
					conn.Name, dbName, config.Name, err)
			}
			continue // Skip this database but continue with others
		}

		// Execute probe
		metrics, err := probe.Execute(ctx, db)

		// Return the connection immediately
		ps.poolManager.ReturnConnection(conn.ID, db)

		if err != nil {
			log.Printf("Error executing probe %s on database %s/%s: %v",
				config.Name, conn.Name, dbName, err)
			continue // Skip this database but continue with others
		}

		if len(metrics) > 0 {
			// Add database name to metrics
			for j := range metrics {
				metrics[j]["_database_name"] = dbName
			}
			allMetrics = append(allMetrics, metrics...)
			log.Printf("Probe %s collected %d metric(s) from %s/%s",
				config.Name, len(metrics), conn.Name, dbName)
		}
	}

	return allMetrics
}

// getDatabaseList queries pg_database to get list of accessible databases
func (ps *ProbeScheduler) getDatabaseList(ctx context.Context, conn *pgxpool.Conn) ([]string, error) {
	rows, err := conn.Query(ctx, `
		SELECT datname
		FROM pg_database
		WHERE datallowconn = true
		  AND NOT datistemplate
		ORDER BY datname
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query pg_database: %w", err)
	}
	defer rows.Close()

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

// executeProbeForServerWide executes a server-wide probe and returns collected metrics
func (ps *ProbeScheduler) executeProbeForServerWide(ctx context.Context, probe probes.MetricsProbe, conn database.MonitoredConnection) []map[string]interface{} {
	config := probe.GetConfig()
	var metrics []map[string]interface{}

	// Check if context is already cancelled
	if ctx.Err() != nil {
		log.Printf("Error getting connection for probe %s on %s: context already cancelled",
			config.Name, conn.Name)
		return metrics
	}

	// Get connection to the monitored server
	monitoredDB, err := ps.poolManager.GetConnection(ctx, conn, ps.serverSecret)
	if err != nil {
		// Check if this was a timeout/cancellation
		if ctx.Err() != nil {
			log.Printf("Error getting connection to %s for probe %s: timed out after %d seconds while waiting for a connection from the monitored connection pool",
				conn.Name, config.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
		} else {
			log.Printf("Error getting connection to monitored database %s for probe %s: %v",
				conn.Name, config.Name, err)
		}
		return metrics
	}

	// Execute probe
	metrics, err = probe.Execute(ctx, monitoredDB)

	// Return the connection immediately
	ps.poolManager.ReturnConnection(conn.ID, monitoredDB)

	if err != nil {
		// Check if this was a timeout during query execution
		if ctx.Err() != nil {
			log.Printf("Error executing probe %s on connection %s: query execution timed out after %d seconds",
				config.Name, conn.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
		} else {
			log.Printf("Error executing probe %s on connection %s: %v",
				config.Name, conn.Name, err)
		}
		return nil
	}

	if len(metrics) > 0 {
		log.Printf("Probe %s collected %d metric(s) from %s",
			config.Name, len(metrics), conn.Name)
	}

	return metrics
}

// storeMetrics stores collected metrics to the datastore
func (ps *ProbeScheduler) storeMetrics(ctx context.Context, probe probes.MetricsProbe, connectionID int, timestamp time.Time, metrics []map[string]interface{}) {
	config := probe.GetConfig()

	// Get datastore connection with configured timeout
	// Use a longer timeout here to allow probes to wait for a datastore connection
	// if the pool is temporarily exhausted during high load
	waitTimeout := time.Duration(ps.config.GetDatastorePoolMaxWaitSeconds()) * time.Second
	datastoreCtx, datastoreCancel := context.WithTimeout(context.Background(), waitTimeout)
	defer datastoreCancel()

	datastoreDB, err := ps.datastore.GetConnectionWithContext(datastoreCtx)
	if err != nil {
		// Check if this was a timeout
		if datastoreCtx.Err() != nil {
			log.Printf("Error storing metrics for probe %s: timed out after %d seconds while waiting for a connection from the datastore pool",
				config.Name, ps.config.GetDatastorePoolMaxWaitSeconds())
		} else {
			log.Printf("Error getting datastore connection for probe %s: %v", config.Name, err)
		}
		return
	}
	defer ps.datastore.ReturnConnection(datastoreDB)

	// Store metrics
	err = probe.Store(ctx, datastoreDB, connectionID, timestamp, metrics)
	if err != nil {
		log.Printf("Error storing metrics for probe %s: %v", config.Name, err)
		return
	}

	log.Printf("Stored %d metric(s) for probe %s", len(metrics), config.Name)
}

// createProbe creates a probe instance based on the configuration
func (ps *ProbeScheduler) createProbe(config *probes.ProbeConfig) probes.MetricsProbe {
	// Create probe instance based on probe name
	switch config.Name {
	// Server-wide probes
	case probes.ProbeNamePgStatActivity:
		return probes.NewPgStatActivityProbe(config)
	case probes.ProbeNamePgStatReplication:
		return probes.NewPgStatReplicationProbe(config)
	case probes.ProbeNamePgStatReplicationSlots:
		return probes.NewPgStatReplicationSlotsProbe(config)
	case probes.ProbeNamePgStatWALReceiver:
		return probes.NewPgStatWALReceiverProbe(config)
	case probes.ProbeNamePgStatRecoveryPrefetch:
		return probes.NewPgStatRecoveryPrefetchProbe(config)
	case probes.ProbeNamePgStatSubscription:
		return probes.NewPgStatSubscriptionProbe(config)
	case probes.ProbeNamePgStatSubscriptionStats:
		return probes.NewPgStatSubscriptionStatsProbe(config)
	case probes.ProbeNamePgStatSSL:
		return probes.NewPgStatSSLProbe(config)
	case probes.ProbeNamePgStatGSSAPI:
		return probes.NewPgStatGSSAPIProbe(config)
	case probes.ProbeNamePgStatArchiver:
		return probes.NewPgStatArchiverProbe(config)
	case probes.ProbeNamePgStatIO:
		return probes.NewPgStatIOProbe(config)
	case probes.ProbeNamePgStatBgwriter:
		return probes.NewPgStatBgwriterProbe(config)
	case probes.ProbeNamePgStatCheckpointer:
		return probes.NewPgStatCheckpointerProbe(config)
	case probes.ProbeNamePgStatWAL:
		return probes.NewPgStatWalProbe(config)
	case probes.ProbeNamePgStatSLRU:
		return probes.NewPgStatSLRUProbe(config)
	// Database-scoped probes
	case probes.ProbeNamePgStatDatabase:
		return probes.NewPgStatDatabaseProbe(config)
	case probes.ProbeNamePgStatDatabaseConflicts:
		return probes.NewPgStatDatabaseConflictsProbe(config)
	case probes.ProbeNamePgStatAllTables:
		return probes.NewPgStatAllTablesProbe(config)
	case probes.ProbeNamePgStatAllIndexes:
		return probes.NewPgStatAllIndexesProbe(config)
	case probes.ProbeNamePgStatioAllTables:
		return probes.NewPgStatioAllTablesProbe(config)
	case probes.ProbeNamePgStatioAllIndexes:
		return probes.NewPgStatioAllIndexesProbe(config)
	case probes.ProbeNamePgStatioAllSequences:
		return probes.NewPgStatioAllSequencesProbe(config)
	case probes.ProbeNamePgStatUserFunctions:
		return probes.NewPgStatUserFunctionsProbe(config)
	case probes.ProbeNamePgStatStatements:
		return probes.NewPgStatStatementsProbe(config)
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
