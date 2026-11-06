/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import "sync"

// knownProbes holds the name of every probe the collector writes into
// the metrics schema. The collector's probe constants
// (collector/src/probes/constants.go) and migrations
// (collector/src/database/schema.go) are the source of truth;
// TestKnownProbesMatchCollectorSchema migrates a scratch database and
// fails when this set and the collector's metrics tables differ, so a new
// probe must be added here in the same change.
var knownProbes = map[string]struct{}{
	"pg_connectivity":              {},
	"pg_database":                  {},
	"pg_extension":                 {},
	"pg_hba_file_rules":            {},
	"pg_ident_file_mappings":       {},
	"pg_node_role":                 {},
	"pg_replication_slots":         {},
	"pg_server_info":               {},
	"pg_settings":                  {},
	"pg_stat_activity":             {},
	"pg_stat_all_indexes":          {},
	"pg_stat_all_tables":           {},
	"pg_stat_checkpointer":         {},
	"pg_stat_connection_security":  {},
	"pg_stat_database":             {},
	"pg_stat_database_conflicts":   {},
	"pg_stat_io":                   {},
	"pg_stat_recovery_prefetch":    {},
	"pg_stat_replication":          {},
	"pg_stat_statements":           {},
	"pg_stat_subscription":         {},
	"pg_stat_user_functions":       {},
	"pg_stat_wal":                  {},
	"pg_statio_all_sequences":      {},
	"pg_sys_cpu_info":              {},
	"pg_sys_cpu_memory_by_process": {},
	"pg_sys_cpu_usage_info":        {},
	"pg_sys_disk_info":             {},
	"pg_sys_io_analysis_info":      {},
	"pg_sys_load_avg_info":         {},
	"pg_sys_memory_info":           {},
	"pg_sys_network_info":          {},
	"pg_sys_os_info":               {},
	"pg_sys_process_info":          {},
	"spock_exception_log":          {},
	"spock_resolutions":            {},
}

// knownProbesMu guards knownProbes. Production code only reads it; the
// package's tests add fixture probes for the length of a test.
var knownProbesMu sync.RWMutex

// IsKnownProbe reports whether name is the name of a probe the collector
// writes (knownProbes). It is the allowlist LookupProbeRelation applies,
// so it decides which metrics relations the server will serve at all.
func IsKnownProbe(name string) bool {
	knownProbesMu.RLock()
	defer knownProbesMu.RUnlock()
	_, ok := knownProbes[name]
	return ok
}
