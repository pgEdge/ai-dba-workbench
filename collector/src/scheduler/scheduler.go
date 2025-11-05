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
	datastore      *database.Datastore
	poolManager    *database.MonitoredConnectionPoolManager
	serverSecret   string
	config         Config
	probesByConn   map[int]map[string]probes.MetricsProbe // connection_id -> probe_name -> probe
	probesMutex    sync.RWMutex
	shutdownChan   chan struct{}
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	configReloader *time.Ticker
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
		probesByConn: make(map[int]map[string]probes.MetricsProbe),
		shutdownChan: make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start begins the probe scheduling loop
func (ps *ProbeScheduler) Start(ctx context.Context) error {
	// Load initial probe configurations
	if err := ps.loadConfigs(ctx); err != nil {
		return err
	}

	// Start config reloader (every 5 minutes)
	ps.configReloader = time.NewTicker(5 * time.Minute)
	ps.wg.Add(1)
	go ps.configReloadLoop()

	log.Printf("Probe scheduler started")
	return nil
}

// loadConfigs loads probe configurations and starts probe scheduling
func (ps *ProbeScheduler) loadConfigs(ctx context.Context) error {
	conn, err := ps.datastore.GetConnection()
	if err != nil {
		return err
	}
	defer ps.datastore.ReturnConnection(conn)

	configsByConnection, err := probes.LoadProbeConfigs(ctx, conn)
	if err != nil {
		return err
	}

	ps.probesMutex.Lock()
	defer ps.probesMutex.Unlock()

	// Get all monitored connections
	connections, err := ps.datastore.GetMonitoredConnections()
	if err != nil {
		return fmt.Errorf("failed to get monitored connections: %w", err)
	}

	// For each connection, ensure probe configs exist and initialize probes
	for _, conn := range connections {
		if _, exists := ps.probesByConn[conn.ID]; !exists {
			ps.probesByConn[conn.ID] = make(map[string]probes.MetricsProbe)
		}

		// Get global default probe configs (connection_id = 0)
		globalConfigs := configsByConnection[0]

		// For each global probe, ensure a connection-specific config exists
		for _, globalConfig := range globalConfigs {
			// Check if there's a connection-specific config
			var config *probes.ProbeConfig
			if connConfigs, exists := configsByConnection[conn.ID]; exists {
				for i := range connConfigs {
					if connConfigs[i].Name == globalConfig.Name {
						config = &connConfigs[i]
						break
					}
				}
			}

			// If no connection-specific config, ensure one is created
			if config == nil {
				datastoreConn, err := ps.datastore.GetConnection()
				if err != nil {
					log.Printf("Error getting datastore connection for probe config creation: %v", err)
					continue
				}

				config, err = probes.EnsureProbeConfig(ctx, datastoreConn, conn.ID, globalConfig.Name)
				ps.datastore.ReturnConnection(datastoreConn)

				if err != nil {
					log.Printf("Error ensuring probe config for %s on connection %d: %v",
						globalConfig.Name, conn.ID, err)
					continue
				}
			}

			// Create probe if it doesn't exist or if interval changed
			existingProbe := ps.probesByConn[conn.ID][config.Name]
			if existingProbe == nil || existingProbe.GetConfig().CollectionIntervalSeconds != config.CollectionIntervalSeconds {
				probe := ps.createProbe(config)
				if probe != nil {
					ps.probesByConn[conn.ID][config.Name] = probe
					log.Printf("Initialized probe %s for connection %d (interval: %ds, retention: %dd)",
						config.Name, conn.ID, config.CollectionIntervalSeconds, config.RetentionDays)

					// Start scheduling if this is a new probe
					if existingProbe == nil {
						ps.wg.Add(1)
						go ps.scheduleProbeForConnection(probe, conn.ID)
					}
				}
			}
		}
	}

	return nil
}

// configReloadLoop periodically reloads probe configurations
func (ps *ProbeScheduler) configReloadLoop() {
	defer ps.wg.Done()

	for {
		select {
		case <-ps.shutdownChan:
			return
		case <-ps.ctx.Done():
			return
		case <-ps.configReloader.C:
			log.Println("Reloading probe configurations...")
			if err := ps.loadConfigs(ps.ctx); err != nil {
				log.Printf("Error reloading probe configurations: %v", err)
			}
		}
	}
}

// scheduleProbeForConnection runs a probe at its configured interval for a specific connection
func (ps *ProbeScheduler) scheduleProbeForConnection(probe probes.MetricsProbe, connectionID int) {
	defer ps.wg.Done()

	config := probe.GetConfig()
	ticker := time.NewTicker(time.Duration(config.CollectionIntervalSeconds) * time.Second)
	defer ticker.Stop()

	// Get connection info
	connections, err := ps.datastore.GetMonitoredConnections()
	if err != nil {
		log.Printf("Error getting monitored connections for probe %s: %v", config.Name, err)
		return
	}

	var conn database.MonitoredConnection
	found := false
	for _, c := range connections {
		if c.ID == connectionID {
			conn = c
			found = true
			break
		}
	}

	if !found {
		log.Printf("Connection %d not found for probe %s", connectionID, config.Name)
		return
	}

	// Run immediately on startup
	ps.executeProbeForConnection(ps.ctx, probe, conn)

	for {
		select {
		case <-ps.shutdownChan:
			log.Printf("Stopping probe scheduler for %s on connection %d", config.Name, connectionID)
			return
		case <-ps.ctx.Done():
			log.Printf("Context cancelled, stopping probe scheduler for %s on connection %d", config.Name, connectionID)
			return
		case <-ticker.C:
			// Check if probe still exists and config hasn't changed
			ps.probesMutex.RLock()
			currentProbe, exists := ps.probesByConn[connectionID][config.Name]
			ps.probesMutex.RUnlock()

			if !exists || currentProbe.GetConfig().CollectionIntervalSeconds != config.CollectionIntervalSeconds {
				// Probe was removed or interval changed, stop this goroutine
				log.Printf("Probe %s for connection %d has changed, stopping scheduler", config.Name, connectionID)
				return
			}

			ps.executeProbeForConnection(ps.ctx, probe, conn)
		}
	}
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
	// Stop config reloader
	if ps.configReloader != nil {
		ps.configReloader.Stop()
	}
	// Cancel the context to interrupt any pending operations
	ps.cancel()
	// Close the shutdown channel to signal goroutines
	close(ps.shutdownChan)
	// Wait for all goroutines to finish
	ps.wg.Wait()
	log.Println("Probe scheduler stopped")
}
