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
    "github.com/pgedge/ai-workbench/collector/src/probes"
    "github.com/pgedge/ai-workbench/collector/src/database"

	"context"
	"log"
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

	log.Println("Garbage collector started")
	return nil
}

// run executes the garbage collection loop
func (gc *GarbageCollector) run(ctx context.Context) {
	defer gc.wg.Done()

	// Wait a short time after startup before first collection
	startupDelay := 5 * time.Minute
	log.Printf("Garbage collector will run first collection in %v", startupDelay)

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
			log.Println("Stopping garbage collector")
			return
		case <-ticker.C:
			gc.collectGarbage(ctx)
		}
	}
}

// collectGarbage performs garbage collection for all probes
func (gc *GarbageCollector) collectGarbage(ctx context.Context) {
	log.Println("Starting garbage collection...")

	// Get database connection
	conn, err := gc.datastore.GetConnection()
	if err != nil {
		log.Printf("Error getting database connection for garbage collection: %v", err)
		return
	}
	defer gc.datastore.ReturnConnection(conn)

	// Load probe configurations
	configsByConnection, err := probes.LoadProbeConfigs(ctx, conn)
	if err != nil {
		log.Printf("Error loading probe configs for garbage collection: %v", err)
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
				log.Printf("Error collecting garbage for probe %s: %v", config.Name, err)
				continue
			}
			totalDropped += dropped
		}
	}

	if totalDropped > 0 {
		log.Printf("Garbage collection completed: dropped %d partition(s)", totalDropped)
	} else {
		log.Println("Garbage collection completed: no partitions to drop")
	}
}

// collectGarbageForProbe performs garbage collection for a single probe
func (gc *GarbageCollector) collectGarbageForProbe(ctx context.Context, conn *pgxpool.Conn, config *probes.ProbeConfig) (int, error) {
	// Get the table name for this probe
	tableName := getProbeTableName(config.Name)
	if tableName == "" {
		log.Printf("Warning: unknown table name for probe %s", config.Name)
		return 0, nil
	}

	// Drop expired partitions
	err := probes.DropExpiredPartitions(ctx, conn, tableName, config.RetentionDays)
	if err != nil {
		return 0, err
	}

	// Note: DropExpiredPartitions logs the count internally
	return 0, nil
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
	log.Println("Garbage collector stopped")
}
