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

// IsKnownProbe reports whether name is the name of a probe the collector
// writes (knownProbes).
func IsKnownProbe(name string) bool {
	_, ok := knownProbes[name]
	return ok
}

// AdmitsProbeRelation reports whether the relation called name in the
// metrics schema may be served as a probe. A table is admitted whatever
// its name, as it always has been. A view is admitted only when it
// carries a known probe's name, which every view on a pgEdge ColdFront
// datastore does (issue #755).
//
// This fails closed for a view under any other name, such as
// CREATE VIEW metrics.activity_report AS SELECT * FROM
// metrics.pg_stat_activity: the read-side masking (ProbeRedactions,
// RedactProbeRows, statementTextColumns) is keyed by probe name, so a
// renamed view of a masked probe would otherwise return its credentials
// and statement text in clear. Every route that resolves a caller's
// source or probe name must decide through this function:
// ProbeExists (QueryTimeSeries, QueryLatestRows, /api/v1/metrics/latest,
// describe_probe and query_metrics), list_probes, and the datastore
// catalog behind describe_datastore and query_datastore.
func AdmitsProbeRelation(name string, isView bool) bool {
	return !isView || IsKnownProbe(name)
}
