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
	"errors"

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

	// Cadence, seeded from the constants above. These are fields rather
	// than direct uses of the constants so that tests can drive the
	// scheduling loop without waiting out real delays.
	interval     time.Duration
	startupGrace time.Duration
	retryDelay   time.Duration
}

// NewGarbageCollector creates a new garbage collector
func NewGarbageCollector(datastore *database.Datastore) *GarbageCollector {
	return &GarbageCollector{
		datastore:    datastore,
		shutdownChan: make(chan struct{}),
		interval:     gcInterval,
		startupGrace: gcStartupGrace,
		retryDelay:   gcRetryDelay,
	}
}

// Start begins the garbage collection loop
func (gc *GarbageCollector) Start(ctx context.Context) error {
	gc.wg.Add(1)
	go gc.run(ctx)

	logger.Info("Garbage collector started")
	return nil
}

// Garbage collection cadence. The interval is the target period between
// collections; the grace period is how long after startup, or after a
// failure, the collector waits before trying.
//
// The grace period is deliberately short. Retention used to be scheduled
// from process start, which coupled it to uptime: a collector that
// restarted more often than the delay never enforced retention even
// once, and a restart loop could therefore fill the datastore's disk
// (issue #437). It is not zero only so that collection does not compete
// with the burst of probes the scheduler runs at startup.
const (
	gcInterval     = 24 * time.Hour
	gcStartupGrace = 30 * time.Second
	gcRetryDelay   = 15 * time.Minute
)

// errNoDatastore reports a garbage collector constructed without a
// datastore. Production always supplies one, so this exists to keep the
// collection goroutine from panicking rather than to be handled.
var errNoDatastore = errors.New("garbage collector has no datastore")

// run executes the garbage collection loop. The next collection is timed
// from the last recorded completion in the datastore rather than from
// process start, so restarts resume the existing cycle instead of
// beginning a fresh one.
func (gc *GarbageCollector) run(ctx context.Context) {
	defer gc.wg.Done()

	var lastAttemptFailed bool

	for {
		// Check for shutdown before touching the datastore, so a
		// collector told to stop during startup exits promptly rather
		// than waiting on a query.
		select {
		case <-gc.shutdownChan:
			logger.Info("Stopping garbage collector")
			return
		case <-ctx.Done():
			logger.Info("Context canceled, stopping garbage collector")
			return
		default:
		}

		delay := gc.nextRunDelay(ctx, lastAttemptFailed)
		logger.Infof("Garbage collector will run next collection in %v", delay)

		select {
		case <-gc.shutdownChan:
			logger.Info("Stopping garbage collector")
			return
		case <-ctx.Done():
			logger.Info("Context canceled, stopping garbage collector")
			return
		case <-time.After(delay):
		}

		lastAttemptFailed = gc.collectGarbage(ctx) != nil
	}
}

// nextRunDelay returns how long to wait before the next collection.
//
// A task that has never run, or whose recorded completion is already
// older than the interval, is due now and waits only the startup grace
// period. Otherwise the delay carries the remainder of the current
// cycle. A failed attempt backs off instead of retrying immediately, so
// that an unreachable datastore does not produce a tight error loop.
func (gc *GarbageCollector) nextRunDelay(ctx context.Context, lastAttemptFailed bool) time.Duration {
	if lastAttemptFailed {
		return gc.retryDelay
	}

	lastRun, found, err := gc.lastRun(ctx)
	switch {
	case err != nil:
		// Treat an unreadable timestamp as due: dropping partitions a
		// little early is harmless, whereas not dropping them at all is
		// what filled a disk in issue #437.
		logger.Errorf("Error reading last garbage collection time, treating collection as due: %v", err)
	case !found:
		logger.Info("No previous garbage collection recorded, collection is due")
	default:
		if time.Until(lastRun.Add(gc.interval)) <= gc.startupGrace {
			logger.Infof("Last garbage collection was %v ago, collection is due",
				time.Since(lastRun).Truncate(time.Second))
		}
	}

	return gc.computeNextRunDelay(lastRun, found, err, lastAttemptFailed, time.Now())
}

// computeNextRunDelay holds the scheduling decision, separated from the
// datastore read so it can be exercised directly.
//
// A task that has never run, or whose recorded completion is already
// older than the interval, is due now and waits only the startup grace
// period. Anything else carries the remainder of the current cycle.
func (gc *GarbageCollector) computeNextRunDelay(lastRun time.Time, found bool, readErr error, lastAttemptFailed bool, now time.Time) time.Duration {
	if lastAttemptFailed {
		return gc.retryDelay
	}
	if readErr != nil || !found {
		return gc.startupGrace
	}

	remaining := lastRun.Add(gc.interval).Sub(now)
	if remaining <= gc.startupGrace {
		return gc.startupGrace
	}

	return remaining
}

// lastRun reads the recorded completion time of the partition retention
// task from the datastore.
func (gc *GarbageCollector) lastRun(ctx context.Context) (time.Time, bool, error) {
	if gc.datastore == nil {
		return time.Time{}, false, errNoDatastore
	}

	conn, err := gc.datastore.GetConnection()
	if err != nil {
		return time.Time{}, false, err
	}
	defer gc.datastore.ReturnConnection(conn)

	return database.GetMaintenanceRun(ctx, conn, database.PartitionRetentionTask)
}

// collectGarbage performs garbage collection for all probes. It returns
// an error only when the pass could not be attempted at all; a failure
// to drop partitions for one probe is logged and does not abandon the
// rest, nor prevent the pass being recorded as done.
func (gc *GarbageCollector) collectGarbage(ctx context.Context) error {
	logger.Info("Starting garbage collection...")

	if gc.datastore == nil {
		logger.Errorf("Error getting database connection for garbage collection: %v", errNoDatastore)
		return errNoDatastore
	}

	// Get database connection
	conn, err := gc.datastore.GetConnection()
	if err != nil {
		logger.Errorf("Error getting database connection for garbage collection: %v", err)
		return err
	}
	defer gc.datastore.ReturnConnection(conn)

	// Load probe configurations
	configsByConnection, err := probes.LoadProbeConfigs(ctx, conn)
	if err != nil {
		logger.Errorf("Error loading probe configs for garbage collection: %v", err)
		return err
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

	// Record the completion so the next run is timed from here rather
	// than from the next process start.
	if err := database.RecordMaintenanceRun(ctx, conn,
		database.PartitionRetentionTask, time.Now()); err != nil {
		logger.Errorf("Error recording garbage collection completion: %v", err)
		return err
	}

	return nil
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
