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

// probeEntityKeyFallback holds, for every metrics probe whose table has a
// primary key on plain PostgreSQL, the entity-key columns that key gives:
// its columns other than connection_id and collected_at, in key order,
// exactly as primaryKeyEntityColumns returns them. A probe whose key is
// only (connection_id, collected_at) maps to an empty slice.
//
// GetProbeEntityKeyColumns reads it only when a probe has no primary key,
// as on a pgEdge ColdFront datastore, where every probe is a view
// (issue #755); without it, counter rates over a multi-entity probe would
// difference one entity's readings against another's.
//
// The collector's migrations in collector/src/database/schema.go are the
// source of truth. TestProbeEntityKeyFallbackMatchesCollectorSchema
// migrates a scratch database and fails when any entry here differs from
// the primary key there, or a probe with a primary key is missing, so a
// change to a probe's key must be mirrored here in the same change.
var probeEntityKeyFallback = map[string][]string{
	"pg_connectivity":              {},
	"pg_database":                  {"datname"},
	"pg_extension":                 {"database_name", "extname"},
	"pg_hba_file_rules":            {"rule_number"},
	"pg_ident_file_mappings":       {"map_number"},
	"pg_node_role":                 {},
	"pg_replication_slots":         {"slot_name"},
	"pg_server_info":               {},
	"pg_settings":                  {"name"},
	"pg_stat_activity":             {"pid"},
	"pg_stat_all_indexes":          {"database_name", "indexrelid"},
	"pg_stat_all_tables":           {"database_name", "schemaname", "relname"},
	"pg_stat_checkpointer":         {},
	"pg_stat_connection_security":  {"pid"},
	"pg_stat_database":             {"database_name"},
	"pg_stat_database_conflicts":   {"database_name"},
	"pg_stat_io":                   {"backend_type", "object", "context"},
	"pg_stat_recovery_prefetch":    {},
	"pg_stat_replication":          {"pid"},
	"pg_stat_statements":           {"database_name", "queryid", "userid", "dbid", "toplevel"},
	"pg_stat_subscription":         {"subid"},
	"pg_stat_user_functions":       {"database_name", "funcid"},
	"pg_stat_wal":                  {},
	"pg_statio_all_sequences":      {"database_name", "relid"},
	"pg_sys_cpu_info":              {},
	"pg_sys_cpu_memory_by_process": {"pid"},
	"pg_sys_cpu_usage_info":        {},
	"pg_sys_disk_info":             {"mount_point"},
	"pg_sys_io_analysis_info":      {"device_name"},
	"pg_sys_load_avg_info":         {},
	"pg_sys_memory_info":           {},
	"pg_sys_network_info":          {"interface_name"},
	"pg_sys_os_info":               {},
	"pg_sys_process_info":          {},
	"spock_exception_log": {
		"database_name", "remote_origin", "remote_commit_ts", "command_counter", "retry_errored_at",
	},
	"spock_resolutions": {"database_name", "id", "node_name"},
}
