/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package scheduler manages the execution of monitoring probes
package scheduler

import (
	"github.com/pgedge/ai-workbench/collector/src/database"
	"github.com/pgedge/ai-workbench/collector/src/probes"
	"github.com/pgedge/ai-workbench/pkg/logger"

	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// availabilityKey identifies a probe for a specific connection
type availabilityKey struct {
	connectionID int
	probeName    string
}

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
	availCache     map[availabilityKey]bool      // tracks last known is_available per probe/connection
	availLastWrite map[availabilityKey]time.Time  // tracks when we last wrote to DB
	availMutex     sync.Mutex
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
		probesByConn:   make(map[int]map[string]probes.MetricsProbe),
		shutdownChan:   make(chan struct{}),
		ctx:            ctx,
		cancel:         cancel,
		availCache:     make(map[availabilityKey]bool),
		availLastWrite: make(map[availabilityKey]time.Time),
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

	logger.Startup("Probe scheduler started")
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

	// Build list of active connection IDs
	activeConnectionIDs := make([]int, len(connections))
	activeConnectionMap := make(map[int]bool)
	for i, conn := range connections {
		activeConnectionIDs[i] = conn.ID
		activeConnectionMap[conn.ID] = true
	}

	// Sync connection pools with active connections
	// This closes pools for connections that are no longer monitored
	ps.poolManager.SyncPools(activeConnectionIDs)

	// Clean up probes for connections that are no longer monitored
	var connectionsToRemove []int
	for connID := range ps.probesByConn {
		if !activeConnectionMap[connID] {
			connectionsToRemove = append(connectionsToRemove, connID)
		}
	}
	for _, connID := range connectionsToRemove {
		delete(ps.probesByConn, connID)
		logger.Infof("Removed probes for connection %d (no longer monitored)", connID)
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
					logger.Errorf("Error getting datastore connection for probe config creation: %v", err)
					continue
				}

				config, err = probes.EnsureProbeConfig(ctx, datastoreConn, conn.ID, globalConfig.Name)
				ps.datastore.ReturnConnection(datastoreConn)

				if err != nil {
					logger.Errorf("Error ensuring probe config for %s on connection %d: %v",
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
					logger.Infof("Initialized probe %s for connection %d (interval: %ds, retention: %dd)",
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
			logger.Info("Reloading probe configurations...")
			if err := ps.loadConfigs(ps.ctx); err != nil {
				logger.Errorf("Error reloading probe configurations: %v", err)
			}
		}
	}
}

// calculateInitialDelay calculates how long to wait before the first probe execution
// based on the last collection time. Returns 0 or negative if probe should run immediately.
func (ps *ProbeScheduler) calculateInitialDelay(probe probes.MetricsProbe, connectionID int, connectionName string, config *probes.ProbeConfig) time.Duration {
	// Get a datastore connection to query last collection time
	conn, err := ps.datastore.GetConnection()
	if err != nil {
		logger.Errorf("Warning: failed to get datastore connection for initial delay calculation: %v", err)
		return 0 // Run immediately if we can't determine last collection time
	}
	defer ps.datastore.ReturnConnection(conn)

	// Query last collection time for this probe/connection pair
	lastCollected, err := probes.GetLastCollectionTime(ps.ctx, conn, probe.GetName(), connectionID)
	if err != nil {
		logger.Errorf("Warning: failed to query last collection time for probe %s on %s: %v",
			probe.GetName(), connectionName, err)
		return 0 // Run immediately if we can't determine last collection time
	}

	// If no previous collection (zero time), run immediately
	if lastCollected.IsZero() {
		logger.Infof("No previous collection found for probe %s on %s, running immediately",
			probe.GetName(), connectionName)
		return 0
	}

	// Calculate when the next collection should happen
	interval := time.Duration(config.CollectionIntervalSeconds) * time.Second
	nextCollection := lastCollected.Add(interval)
	delay := time.Until(nextCollection)

	return delay
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
		logger.Errorf("Error getting monitored connections for probe %s: %v", config.Name, err)
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
		logger.Errorf("Connection %d not found for probe %s", connectionID, config.Name)
		return
	}

	// Calculate initial delay based on last collection time
	initialDelay := ps.calculateInitialDelay(probe, connectionID, conn.Name, config)

	if initialDelay > 0 {
		logger.Infof("Delaying first execution of probe %s on %s by %v (last collected recently)",
			config.Name, conn.Name, initialDelay)

		// Wait for the initial delay before first execution
		select {
		case <-ps.shutdownChan:
			logger.Infof("Stopping probe scheduler for %s on %s during initial delay", config.Name, conn.Name)
			return
		case <-ps.ctx.Done():
			logger.Infof("Context canceled, stopping probe scheduler for %s on %s during initial delay", config.Name, conn.Name)
			return
		case <-time.After(initialDelay):
			// Initial delay elapsed, execute probe now
			ps.executeProbeForConnection(ps.ctx, probe, conn)
		}
	} else {
		// No delay needed, run immediately
		if initialDelay < 0 {
			logger.Infof("Probe %s on %s is past due by %v, executing immediately",
				config.Name, conn.Name, -initialDelay)
		}
		ps.executeProbeForConnection(ps.ctx, probe, conn)
	}

	for {
		select {
		case <-ps.shutdownChan:
			logger.Infof("Stopping probe scheduler for %s on %s", config.Name, conn.Name)
			return
		case <-ps.ctx.Done():
			logger.Infof("Context canceled, stopping probe scheduler for %s on %s", config.Name, conn.Name)
			return
		case <-ticker.C:
			// Check if probe still exists and config hasn't changed
			ps.probesMutex.RLock()
			currentProbe, exists := ps.probesByConn[connectionID][config.Name]
			ps.probesMutex.RUnlock()

			if !exists || currentProbe.GetConfig().CollectionIntervalSeconds != config.CollectionIntervalSeconds {
				// Probe was removed or interval changed, stop this goroutine
				logger.Infof("Probe %s for %s has changed, stopping scheduler", config.Name, conn.Name)
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

	// Create a timeout context for this probe execution using configured timeout
	// This ensures that if a connection hangs, we don't wait forever
	monitoredTimeout := time.Duration(ps.config.GetMonitoredPoolMaxWaitSeconds()) * time.Second
	execCtx, cancel := context.WithTimeout(ctx, monitoredTimeout)
	defer cancel()

	// Collect all metrics before storing
	var allMetrics []map[string]interface{}
	var databases []string
	timestamp := time.Now()

	if probe.IsDatabaseScoped() {
		// Execute probe for each database and collect metrics
		allMetrics, databases = ps.executeProbeForAllDatabases(execCtx, probe, conn)
	} else {
		// Execute probe once for the connection
		allMetrics = ps.executeProbeForServerWide(execCtx, probe, conn)
	}

	// Check if we hit the timeout
	if execCtx.Err() == context.DeadlineExceeded {
		logger.Errorf("Probe %s execution timed out for connection %s (timeout: %d seconds)",
			config.Name, conn.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
		// Record timeout as unavailable
		var extName *string
		if ep, ok := probe.(probes.ExtensionProbe); ok {
			name := ep.GetExtensionName()
			extName = &name
		}
		reason := "probe execution timed out"
		ps.recordAvailability(conn.ID, config.Name, extName, false, &reason)
		return
	}

	// If we have metrics, store them all at once
	metricsStored := 0
	if len(allMetrics) > 0 {
		metricsStored = ps.storeMetrics(ctx, probe, conn.ID, timestamp, allMetrics)
	}

	// Log a single comprehensive message
	duration := time.Since(startTime)
	if metricsStored > 0 {
		if probe.IsDatabaseScoped() && len(databases) > 0 {
			logger.Infof("Probe %s on %s in databases %v stored %d metrics in %.2fms",
				config.Name, conn.Name, databases, metricsStored, float64(duration.Microseconds())/1000.0)
		} else {
			logger.Infof("Probe %s on %s stored %d metrics in %.2fms",
				config.Name, conn.Name, metricsStored, float64(duration.Microseconds())/1000.0)
		}
	} else {
		logger.Infof("Probe %s on %s completed in %.2fms (no metrics collected)",
			config.Name, conn.Name, float64(duration.Microseconds())/1000.0)
	}

	// Record probe availability
	var extName *string
	if ep, ok := probe.(probes.ExtensionProbe); ok {
		name := ep.GetExtensionName()
		extName = &name
	}

	if metricsStored > 0 {
		ps.recordAvailability(conn.ID, config.Name, extName, true, nil)
	} else if extName != nil && allMetrics == nil {
		reason := fmt.Sprintf("extension '%s' not installed", *extName)
		ps.recordAvailability(conn.ID, config.Name, extName, false, &reason)
	} else {
		// Non-extension probe with no metrics is normal (e.g., no replication)
		ps.recordAvailability(conn.ID, config.Name, extName, true, nil)
	}
}

// recordAvailability records probe availability, writing to the DB only
// when the status changes or every 10 minutes to keep last_checked fresh.
func (ps *ProbeScheduler) recordAvailability(connectionID int, probeName string, extensionName *string, isAvailable bool, unavailableReason *string) {
	key := availabilityKey{connectionID: connectionID, probeName: probeName}

	ps.availMutex.Lock()
	prev, known := ps.availCache[key]
	lastWrite := ps.availLastWrite[key]
	ps.availMutex.Unlock()

	changed := !known || prev != isAvailable
	stale := time.Since(lastWrite) > 10*time.Minute

	if !changed && !stale {
		return
	}

	// Get a datastore connection to write availability
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := ps.datastore.GetConnectionWithContext(ctx)
	if err != nil {
		logger.Errorf("Failed to get datastore connection for probe availability: %v", err)
		return
	}
	defer ps.datastore.ReturnConnection(conn)

	err = database.UpsertProbeAvailability(ctx, conn, connectionID, nil, probeName, extensionName, isAvailable, unavailableReason)
	if err != nil {
		logger.Errorf("Failed to upsert probe availability for %s on connection %d: %v", probeName, connectionID, err)
		return
	}

	ps.availMutex.Lock()
	ps.availCache[key] = isAvailable
	ps.availLastWrite[key] = time.Now()
	ps.availMutex.Unlock()
}

// executeProbeForAllDatabases executes a database-scoped probe for all databases and returns all collected metrics and database list
func (ps *ProbeScheduler) executeProbeForAllDatabases(ctx context.Context, probe probes.MetricsProbe, conn database.MonitoredConnection) ([]map[string]interface{}, []string) {
	config := probe.GetConfig()
	var allMetrics []map[string]interface{}
	var databases []string

	// Check if context is already canceled
	if ctx.Err() != nil {
		logger.Errorf("Error getting connection for probe %s on %s: context already canceled",
			config.Name, conn.Name)
		return allMetrics, databases
	}

	// Get connection to query pg_database (connects to default database)
	monitoredDB, err := ps.poolManager.GetConnection(ctx, conn, ps.serverSecret)
	if err != nil {
		// Check if this was a timeout/cancellation
		if ctx.Err() != nil {
			logger.Errorf("Error getting connection to %s for probe %s: timed out after %d seconds while waiting for a connection from the monitored connection pool",
				conn.Name, config.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
		} else {
			logger.Errorf("Error getting connection to monitored database %s for probe %s: %v",
				conn.Name, config.Name, err)
		}
		return allMetrics, databases
	}

	// Detect and cache PostgreSQL version
	pgVersion, err := ps.poolManager.DetectAndCacheVersion(ctx, conn.ID, monitoredDB)
	if err != nil {
		logger.Debugf("Warning: failed to detect PostgreSQL version for %s: %v", conn.Name, err)
		pgVersion = 0 // Use 0 to indicate unknown version
	}

	// Query pg_database to get list of databases
	databases, err = ps.getDatabaseList(ctx, monitoredDB)
	if err != nil {
		// Return connection before returning
		ps.poolManager.ReturnConnection(conn.ID, monitoredDB)

		// Check if this was a timeout/cancellation
		if ctx.Err() != nil {
			logger.Errorf("Error getting database list for probe %s on %s: timed out after %d seconds while waiting for a connection from the monitored connection pool",
				config.Name, conn.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
		} else {
			logger.Errorf("Error getting database list for probe %s on connection %s: %v",
				config.Name, conn.Name, err)
		}
		return allMetrics, databases
	}

	// Execute probe on the default/first database using the connection we already have
	if len(databases) > 0 {
		defaultDB := databases[0]
		metrics, err := probe.Execute(ctx, conn.Name, monitoredDB, pgVersion)
		if err != nil {
			logger.Debugf("Error executing probe %s on default database %s/%s: %v",
				config.Name, conn.Name, defaultDB, err)
		} else if len(metrics) > 0 {
			// Add database name to metrics
			for i := range metrics {
				metrics[i]["_database_name"] = defaultDB
			}
			allMetrics = append(allMetrics, metrics...)
		}
	}

	// Return the connection now that we're done with the default database
	ps.poolManager.ReturnConnection(conn.ID, monitoredDB)

	// Execute probe for remaining databases (skip the first one we already did)
	for i := 1; i < len(databases); i++ {
		dbName := databases[i]

		// Check if context is canceled (e.g., during shutdown) before processing next database
		if ctx.Err() != nil {
			logger.Infof("Stopping probe %s execution on %s due to context cancellation", config.Name, conn.Name)
			break
		}

		// Get connection for this specific database
		db, err := ps.poolManager.GetConnectionForDatabase(ctx, conn, dbName, ps.serverSecret)
		if err != nil {
			if ctx.Err() != nil {
				logger.Errorf("Error getting connection to %s/%s for probe %s: timed out after %d seconds while waiting for a connection from the monitored connection pool",
					conn.Name, dbName, config.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
			} else {
				logger.Errorf("Error getting connection to %s/%s for probe %s: %v",
					conn.Name, dbName, config.Name, err)
			}
			continue // Skip this database but continue with others
		}

		// Execute probe
		metrics, err := probe.Execute(ctx, conn.Name, db, pgVersion)

		// Return the connection immediately
		ps.poolManager.ReturnConnection(conn.ID, db)

		if err != nil {
			logger.Debugf("Error executing probe %s on database %s/%s: %v",
				config.Name, conn.Name, dbName, err)
			continue // Skip this database but continue with others
		}

		if len(metrics) > 0 {
			// Add database name to metrics
			for j := range metrics {
				metrics[j]["_database_name"] = dbName
			}
			allMetrics = append(allMetrics, metrics...)
		}
	}

	return allMetrics, databases
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

	// Check if context is already canceled
	if ctx.Err() != nil {
		logger.Errorf("Error getting connection for probe %s on %s: context already canceled",
			config.Name, conn.Name)
		return metrics
	}

	// Get connection to the monitored server
	monitoredDB, err := ps.poolManager.GetConnection(ctx, conn, ps.serverSecret)
	if err != nil {
		// Check if this was a timeout/cancellation
		if ctx.Err() != nil {
			logger.Errorf("Error getting connection to %s for probe %s: timed out after %d seconds while waiting for a connection from the monitored connection pool",
				conn.Name, config.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
		} else {
			logger.Errorf("Error getting connection to monitored database %s for probe %s: %v",
				conn.Name, config.Name, err)
		}
		return metrics
	}

	// Detect and cache PostgreSQL version
	pgVersion, err := ps.poolManager.DetectAndCacheVersion(ctx, conn.ID, monitoredDB)
	if err != nil {
		logger.Debugf("Warning: failed to detect PostgreSQL version for %s: %v", conn.Name, err)
		pgVersion = 0 // Use 0 to indicate unknown version
	}

	// Execute probe
	metrics, err = probe.Execute(ctx, conn.Name, monitoredDB, pgVersion)

	// Return the connection immediately
	ps.poolManager.ReturnConnection(conn.ID, monitoredDB)

	if err != nil {
		// Check if this was a timeout during query execution
		if ctx.Err() != nil {
			logger.Errorf("Error executing probe %s on connection %s: query execution timed out after %d seconds",
				config.Name, conn.Name, ps.config.GetMonitoredPoolMaxWaitSeconds())
		} else {
			logger.Debugf("Error executing probe %s on connection %s: %v",
				config.Name, conn.Name, err)
		}
		return nil
	}

	return metrics
}

// storeMetrics stores collected metrics to the datastore and returns the number of metrics stored
func (ps *ProbeScheduler) storeMetrics(ctx context.Context, probe probes.MetricsProbe, connectionID int, timestamp time.Time, metrics []map[string]interface{}) int {
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
			logger.Errorf("Error storing metrics for probe %s: timed out after %d seconds while waiting for a connection from the datastore pool",
				config.Name, ps.config.GetDatastorePoolMaxWaitSeconds())
		} else {
			logger.Errorf("Error getting datastore connection for probe %s: %v", config.Name, err)
		}
		return 0
	}
	defer ps.datastore.ReturnConnection(datastoreDB)

	// Store metrics
	err = probe.Store(ctx, datastoreDB, connectionID, timestamp, metrics)
	if err != nil {
		logger.Errorf("Error storing metrics for probe %s: %v", config.Name, err)
		return 0
	}

	return len(metrics)
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
	case probes.ProbeNamePgSettings:
		return probes.NewPgSettingsProbe(config)
	case probes.ProbeNamePgHbaFileRules:
		return probes.NewPgHbaFileRulesProbe(config)
	case probes.ProbeNamePgIdentFileMappings:
		return probes.NewPgIdentFileMappingsProbe(config)
	case probes.ProbeNamePgServerInfo:
		return probes.NewPgServerInfoProbe(config)
	case probes.ProbeNamePgNodeRole:
		return probes.NewPgNodeRoleProbe(config)
	case probes.ProbeNamePgDatabase:
		return probes.NewPgDatabaseProbe(config)
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
	case probes.ProbeNamePgExtension:
		return probes.NewPgExtensionProbe(config)
	// System Stats Extension probes (server-scoped)
	case probes.ProbeNamePgSysOsInfo:
		return probes.NewPgSysOsInfoProbe(config)
	case probes.ProbeNamePgSysCPUInfo:
		return probes.NewPgSysCPUInfoProbe(config)
	case probes.ProbeNamePgSysCPUUsageInfo:
		return probes.NewPgSysCPUUsageInfoProbe(config)
	case probes.ProbeNamePgSysMemoryInfo:
		return probes.NewPgSysMemoryInfoProbe(config)
	case probes.ProbeNamePgSysIoAnalysisInfo:
		return probes.NewPgSysIoAnalysisInfoProbe(config)
	case probes.ProbeNamePgSysDiskInfo:
		return probes.NewPgSysDiskInfoProbe(config)
	case probes.ProbeNamePgSysLoadAvgInfo:
		return probes.NewPgSysLoadAvgInfoProbe(config)
	case probes.ProbeNamePgSysProcessInfo:
		return probes.NewPgSysProcessInfoProbe(config)
	case probes.ProbeNamePgSysNetworkInfo:
		return probes.NewPgSysNetworkInfoProbe(config)
	case probes.ProbeNamePgSysCPUMemoryByProcess:
		return probes.NewPgSysCPUMemoryByProcessProbe(config)
	default:
		logger.Errorf("Warning: unknown probe type: %s", config.Name)
		return nil
	}
}

// Stop stops the probe scheduler
func (ps *ProbeScheduler) Stop() {
	logger.Startup("Stopping probe scheduler...")
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
	logger.Startup("Probe scheduler stopped")
}
