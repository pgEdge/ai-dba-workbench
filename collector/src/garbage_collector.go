/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"github.com/pgedge/ai-workbench/collector/src/database"
	"github.com/pgedge/ai-workbench/collector/src/probes"

	"context"
	"github.com/pgedge/ai-workbench/pkg/logger"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GarbageCollector manages cleanup of expired metrics data
type GarbageCollector struct {
	datastore    *database.Datastore
	shutdownChan chan struct{}
	wg           sync.WaitGroup
}

// NewGarbageCollector creates a new garbage collector
func NewGarbageCollector(datastore *database.Datastore) *GarbageCollector {
	return &GarbageCollector{
		datastore:    datastore,
		shutdownChan: make(chan struct{}),
	}
}

// Start begins the garbage collection loop
func (gc *GarbageCollector) Start(ctx context.Context) error {
	gc.wg.Add(1)
	go gc.run(ctx)

	logger.Info("Garbage collector started")
	return nil
}

// run executes the garbage collection loop
func (gc *GarbageCollector) run(ctx context.Context) {
	defer gc.wg.Done()

	// Wait a short time after startup before first collection
	startupDelay := 5 * time.Minute
	logger.Infof("Garbage collector will run first collection in %v", startupDelay)

	select {
	case <-gc.shutdownChan:
		return
	case <-time.After(startupDelay):
		// Run first collection
		gc.collectGarbage(ctx)
	}

	// Schedule regular collections every 24 hours
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-gc.shutdownChan:
			logger.Info("Stopping garbage collector")
			return
		case <-ticker.C:
			gc.collectGarbage(ctx)
		}
	}
}

// collectGarbage performs garbage collection for all probes
func (gc *GarbageCollector) collectGarbage(ctx context.Context) {
	logger.Info("Starting garbage collection...")

	// Get database connection
	conn, err := gc.datastore.GetConnection()
	if err != nil {
		logger.Errorf("Error getting database connection for garbage collection: %v", err)
		return
	}
	defer gc.datastore.ReturnConnection(conn)

	// Load probe configurations
	configsByConnection, err := probes.LoadProbeConfigs(ctx, conn)
	if err != nil {
		logger.Errorf("Error loading probe configs for garbage collection: %v", err)
		return
	}

	// Process each probe from all connections (including global defaults)
	var totalDropped int
	seenProbes := make(map[string]bool)
	for _, configs := range configsByConnection {
		for _, config := range configs {
			// Skip if we've already processed this probe (same probe may exist for multiple connections)
			// We only need to drop partitions once per probe, not per connection
			if seenProbes[config.Name] {
				continue
			}
			seenProbes[config.Name] = true

			dropped, err := gc.collectGarbageForProbe(ctx, conn, &config)
			if err != nil {
				logger.Errorf("Error collecting garbage for probe %s: %v", config.Name, err)
				continue
			}
			totalDropped += dropped
		}
	}

	if totalDropped > 0 {
		logger.Infof("Garbage collection completed: dropped %d partition(s)", totalDropped)
	} else {
		logger.Info("Garbage collection completed: no partitions to drop")
	}
}

// collectGarbageForProbe performs garbage collection for a single probe
func (gc *GarbageCollector) collectGarbageForProbe(ctx context.Context, conn *pgxpool.Conn, config *probes.ProbeConfig) (int, error) {
	// Get the table name for this probe
	tableName := getProbeTableName(config.Name)
	if tableName == "" {
		logger.Errorf("Warning: unknown table name for probe %s", config.Name)
		return 0, nil
	}

	// Drop expired partitions
	dropped, err := probes.DropExpiredPartitions(ctx, conn, tableName, config.RetentionDays)
	if err != nil {
		return 0, err
	}

	return dropped, nil
}

// getProbeTableName returns the table name for a probe
func getProbeTableName(probeName string) string {
	// Map probe names to table names
	// For most probes, the table name matches the probe name
	switch probeName {
	case "pg_stat_activity":
		return "pg_stat_activity"
	case "pg_stat_all_tables":
		return "pg_stat_all_tables"
	case "pg_stat_statements":
		return "pg_stat_statements"
	default:
		return probeName
	}
}

// Stop stops the garbage collector
func (gc *GarbageCollector) Stop() {
	close(gc.shutdownChan)
	gc.wg.Wait()
	logger.Startup("Garbage collector stopped")
}
