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

import "time"

// Probe names
const (
    ProbeNamePgStatActivity   = "pg_stat_activity"
    ProbeNamePgStatAllTables  = "pg_stat_all_tables"
    ProbeNamePgStatStatements = "pg_stat_statements"
)

// Database connection defaults
const (
    DefaultPostgresPort = 5432
    DefaultSSLMode      = "prefer"
)

// Connection pool defaults
const (
    DefaultPoolMaxConnections          = 25
    DefaultPoolMaxIdleSeconds          = 300
    DefaultMonitoredPoolMaxConnections = 5
)

// Timeouts and intervals
const (
    ConnectionTimeout        = 10 * time.Second
    ContextTimeout           = 30 * time.Second
    DatastoreWaitTimeout     = 5 * time.Second
    IdleConnectionCheck      = 1 * time.Second
    GarbageCollectorInterval = 5 * time.Minute
    DayInHours               = 24 * time.Hour
)

// Batch and limit sizes
const (
    MetricsBatchSize           = 100
    PgStatStatementsQueryLimit = 1000
)

// Application identifiers
const (
    ApplicationName = "pgEdge AI Workbench - Monitoring"
)
