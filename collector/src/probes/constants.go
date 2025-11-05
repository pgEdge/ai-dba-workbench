/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package probes

// Probe names - Server-wide probes
const (
    ProbeNamePgStatActivity         = "pg_stat_activity"
    ProbeNamePgStatReplication      = "pg_stat_replication"
    ProbeNamePgStatReplicationSlots = "pg_stat_replication_slots"
    ProbeNamePgStatWALReceiver      = "pg_stat_wal_receiver"
    ProbeNamePgStatRecoveryPrefetch = "pg_stat_recovery_prefetch"
    ProbeNamePgStatSubscription     = "pg_stat_subscription"
    ProbeNamePgStatSubscriptionStats= "pg_stat_subscription_stats"
    ProbeNamePgStatSSL              = "pg_stat_ssl"
    ProbeNamePgStatGSSAPI           = "pg_stat_gssapi"
    ProbeNamePgStatArchiver         = "pg_stat_archiver"
    ProbeNamePgStatIO               = "pg_stat_io"
    ProbeNamePgStatBgwriter         = "pg_stat_bgwriter"
    ProbeNamePgStatCheckpointer     = "pg_stat_checkpointer"
    ProbeNamePgStatWAL              = "pg_stat_wal"
    ProbeNamePgStatSLRU             = "pg_stat_slru"
)

// Probe names - Database-scoped probes
const (
    ProbeNamePgStatDatabase          = "pg_stat_database"
    ProbeNamePgStatDatabaseConflicts = "pg_stat_database_conflicts"
    ProbeNamePgStatAllTables         = "pg_stat_all_tables"
    ProbeNamePgStatAllIndexes        = "pg_stat_all_indexes"
    ProbeNamePgStatioAllTables       = "pg_statio_all_tables"
    ProbeNamePgStatioAllIndexes      = "pg_statio_all_indexes"
    ProbeNamePgStatioAllSequences    = "pg_statio_all_sequences"
    ProbeNamePgStatUserFunctions     = "pg_stat_user_functions"
    ProbeNamePgStatStatements        = "pg_stat_statements"
)

// Probe-specific constants
const (
    PgStatStatementsQueryLimit = 1000 // Maximum number of queries to fetch from pg_stat_statements
)
