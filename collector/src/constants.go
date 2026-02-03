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

import "time"

// Database connection defaults
const (
	DefaultPostgresPort = 5432
	DefaultSSLMode      = "prefer"
)

// Connection pool defaults
const (
	DefaultPoolMaxConnections          = 25  // For datastore connection pool
	DefaultPoolMaxIdleSeconds          = 300 // For datastore connection pool (5 minutes)
	DefaultMonitoredPoolMaxConnections = 5   // Max concurrent connections PER monitored database server
)

// Timeouts and intervals
const (
	ConnectionTimeout        = 10 * time.Second
	ContextTimeout           = 30 * time.Second
	ProbeExecutionTimeout    = 60 * time.Second
	DatastoreWaitTimeout     = 5 * time.Second
	IdleConnectionCheck      = 1 * time.Second
	GarbageCollectorInterval = 5 * time.Minute
	DayInHours               = 24 * time.Hour
)

// Batch and limit sizes
const (
	MetricsBatchSize = 100
)

// Collection intervals (in seconds) - customized per probe type
const (
	// Fast-changing data - collect more frequently
	IntervalReplication = 30 // Replication lag changes rapidly
	IntervalWALReceiver = 30 // WAL receiver status changes rapidly
	IntervalActivity    = 60 // Activity changes frequently

	// Normal-changing data - default interval
	IntervalDefault   = 300 // 5 minutes
	IntervalDatabase  = 300
	IntervalTables    = 300
	IntervalIndexes   = 300
	IntervalFunctions = 300

	// Slow-changing data - collect less frequently
	IntervalArchiver     = 600 // 10 minutes - archiver stats change slowly
	IntervalBgwriter     = 600 // 10 minutes - bgwriter stats change slowly
	IntervalCheckpointer = 600 // 10 minutes - checkpointer stats change slowly
	IntervalWAL          = 600 // 10 minutes - WAL stats accumulate slowly
	IntervalSLRU         = 600 // 10 minutes - SLRU stats change slowly

	// Very slow-changing or sparse data
	IntervalIO               = 900 // 15 minutes - I/O stats accumulate slowly
	IntervalSubscription     = 300 // 5 minutes - subscription state can change
	IntervalReplicationSlots = 300 // 5 minutes - slot state changes moderately
	IntervalRecoveryPrefetch = 600 // 10 minutes - prefetch stats change slowly
)
