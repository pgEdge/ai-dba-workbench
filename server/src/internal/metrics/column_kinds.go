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

// ColumnKind classifies how a stored metric column behaves over time, which
// decides which derived metrics (_per_sec, _delta, _pct, _sessions) may be
// computed from it. Anything not registered is a gauge.
type ColumnKind int

const (
	// KindGauge is an instantaneous reading such as n_dead_tup or
	// numbackends. Differencing it is meaningless, so no counter-derived
	// metric is offered for it.
	KindGauge ColumnKind = iota
	// KindCounter is a monotonic event or byte counter such as xact_commit
	// or tx_bytes; it supports _per_sec and _delta.
	KindCounter
	// KindTimeCounter is cumulative milliseconds of elapsed time such as
	// blk_read_time; it supports _delta (ms) and _pct (share of wall-clock
	// time), but not _per_sec, which would be a dimensionless ms/s.
	KindTimeCounter
	// KindSessionTimeCounter is cumulative milliseconds summed across
	// sessions, such as active_time in pg_stat_database; it supports
	// _delta (ms) and _sessions (average sessions in that state).
	KindSessionTimeCounter
	// KindWatermark is a lifetime min/max/mean/stddev such as
	// mean_exec_time. It is never differenced.
	KindWatermark
	// KindRatio is a percentage or similar already-normalised value.
	KindRatio
)

// String returns the kind's name as used in client-facing error messages.
func (k ColumnKind) String() string {
	switch k {
	case KindGauge:
		return "gauge"
	case KindCounter:
		return "cumulative counter"
	case KindTimeCounter:
		return "cumulative time counter"
	case KindSessionTimeCounter:
		return "session time counter"
	case KindWatermark:
		return "lifetime watermark"
	case KindRatio:
		return "ratio"
	}
	return "unknown"
}

// probeRegistryEntry carries the per-probe metadata the derived-metric
// path needs beyond what information_schema can tell it.
type probeRegistryEntry struct {
	// kinds maps a column name to its kind; a column absent from the map
	// is a gauge.
	kinds map[string]ColumnKind
	// resetColumn returns the stats_reset marker column guarding a given
	// counter column, or "" when the probe has none.
	resetColumn func(col string) string
	// excludeEntities is an extra WHERE clause fragment applied to every
	// query on the probe (raw and derived), or "".
	excludeEntities string
}

// kindGroup pairs a kind with the columns that carry it; it exists only to
// keep the registry literal below readable.
type kindGroup struct {
	kind ColumnKind
	cols []string
}

func counters(cols ...string) kindGroup     { return kindGroup{KindCounter, cols} }
func timeCounters(cols ...string) kindGroup { return kindGroup{KindTimeCounter, cols} }
func sessionTimeCounters(cols ...string) kindGroup {
	return kindGroup{KindSessionTimeCounter, cols}
}
func watermarks(cols ...string) kindGroup { return kindGroup{KindWatermark, cols} }
func ratios(cols ...string) kindGroup     { return kindGroup{KindRatio, cols} }

// columnKinds flattens the groups into the column-to-kind map an entry holds.
func columnKinds(groups ...kindGroup) map[string]ColumnKind {
	m := make(map[string]ColumnKind)
	for _, g := range groups {
		for _, c := range g.cols {
			m[c] = g.kind
		}
	}
	return m
}

// fixedReset returns a resetColumn func naming the same marker for every
// column of the probe.
func fixedReset(col string) func(string) string {
	return func(string) string { return col }
}

// splitReset returns a resetColumn func that names alt for the listed
// columns and def for every other column, for probes consolidated from two
// PostgreSQL views with independent reset times.
func splitReset(def, alt string, altCols ...string) func(string) string {
	alts := make(map[string]bool, len(altCols))
	for _, c := range altCols {
		alts[c] = true
	}
	return func(col string) string {
		if alts[col] {
			return alt
		}
		return def
	}
}

// probeRegistry is keyed by probe (table) name. Every column name here must
// exist in the collector DDL in collector/src/database/schema.go;
// TestProbeRegistryMatchesCollectorDDL checks that. Probes that store no
// counters are simply absent, and every column of theirs is a gauge.
var probeRegistry = map[string]probeRegistryEntry{
	"pg_stat_all_tables": {
		kinds: columnKinds(counters(
			"seq_scan", "seq_tup_read", "idx_scan", "idx_tup_fetch",
			"n_tup_ins", "n_tup_upd", "n_tup_del", "n_tup_hot_upd",
			"vacuum_count", "autovacuum_count", "analyze_count",
			"autoanalyze_count",
			"heap_blks_read", "heap_blks_hit", "idx_blks_read", "idx_blks_hit",
			"toast_blks_read", "toast_blks_hit", "tidx_blks_read",
			"tidx_blks_hit",
		)),
	},
	"pg_stat_all_indexes": {
		kinds: columnKinds(counters(
			"idx_scan", "idx_tup_read", "idx_tup_fetch",
			"idx_blks_read", "idx_blks_hit",
		)),
	},
	"pg_stat_statements": {
		kinds: columnKinds(
			counters(
				"calls", "rows",
				"shared_blks_hit", "shared_blks_read", "shared_blks_dirtied",
				"shared_blks_written",
				"local_blks_hit", "local_blks_read", "local_blks_dirtied",
				"local_blks_written",
				"temp_blks_read", "temp_blks_written",
			),
			timeCounters(
				"total_exec_time",
				"shared_blk_read_time", "shared_blk_write_time",
				"local_blk_read_time", "local_blk_write_time",
			),
			watermarks(
				"mean_exec_time", "min_exec_time", "max_exec_time",
				"stddev_exec_time",
			),
		),
		// Added by collector migration 10 (issue #402); the builder only
		// references it when the table actually has the column.
		resetColumn: fixedReset("stats_reset"),
	},
	"pg_stat_database": {
		kinds: columnKinds(
			counters(
				"xact_commit", "xact_rollback", "blks_read", "blks_hit",
				"tup_returned", "tup_fetched", "tup_inserted", "tup_updated",
				"tup_deleted", "conflicts", "temp_files", "temp_bytes",
				"deadlocks", "checksum_failures",
				"sessions", "sessions_abandoned", "sessions_fatal",
				"sessions_killed",
			),
			timeCounters("blk_read_time", "blk_write_time"),
			sessionTimeCounters(
				"session_time", "active_time", "idle_in_transaction_time",
			),
		),
		resetColumn: fixedReset("stats_reset"),
	},
	"pg_stat_database_conflicts": {
		kinds: columnKinds(counters(
			"confl_tablespace", "confl_lock", "confl_snapshot",
			"confl_bufferpin", "confl_deadlock", "confl_active_logicalslot",
		)),
	},
	"pg_stat_checkpointer": {
		kinds: columnKinds(
			counters(
				"num_timed", "num_requested",
				"restartpoints_timed", "restartpoints_req", "restartpoints_done",
				"buffers_written", "buffers_clean", "maxwritten_clean",
				"buffers_alloc",
			),
			timeCounters("write_time", "sync_time"),
		),
		// The bgwriter columns were consolidated from pg_stat_bgwriter and
		// keep their own reset time.
		resetColumn: splitReset("stats_reset", "bgwriter_stats_reset",
			"buffers_clean", "maxwritten_clean", "buffers_alloc"),
	},
	"pg_stat_wal": {
		kinds: columnKinds(
			counters(
				"wal_records", "wal_fpi", "wal_bytes", "wal_buffers_full",
				"wal_write", "wal_sync", "archived_count", "failed_count",
			),
			timeCounters("wal_write_time", "wal_sync_time"),
		),
		// The archiver pair was consolidated from pg_stat_archiver and
		// keeps its own reset time.
		resetColumn: splitReset("stats_reset", "archiver_stats_reset",
			"archived_count", "failed_count"),
	},
	"pg_replication_slots": {
		kinds: columnKinds(counters(
			"spill_txns", "spill_count", "spill_bytes",
			"stream_txns", "stream_count", "stream_bytes",
			"total_txns", "total_count", "total_bytes",
		)),
		resetColumn: fixedReset("stats_reset"),
	},
	"pg_stat_subscription": {
		kinds:       columnKinds(counters("apply_error_count", "sync_error_count")),
		resetColumn: fixedReset("stats_reset"),
	},
	"pg_stat_recovery_prefetch": {
		kinds: columnKinds(counters(
			"prefetch", "hit", "skip_init", "skip_new", "skip_fpw", "skip_rep",
		)),
		resetColumn: fixedReset("stats_reset"),
	},
	"pg_stat_io": {
		kinds: columnKinds(
			counters(
				"reads", "writes", "writebacks", "extends", "hits",
				"evictions", "reuses", "fsyncs",
				"blks_zeroed", "blks_exists", "flushes", "truncates",
			),
			timeCounters(
				"read_time", "write_time", "writeback_time", "extend_time",
				"fsync_time",
			),
		),
		resetColumn: fixedReset("stats_reset"),
	},
	"pg_stat_user_functions": {
		kinds: columnKinds(
			counters("calls"),
			timeCounters("total_time", "self_time"),
		),
	},
	"pg_statio_all_sequences": {
		kinds: columnKinds(counters("blks_read", "blks_hit")),
	},
	"pg_sys_cpu_usage_info": {
		kinds: columnKinds(ratios(
			"usermode_normal_process_percent", "usermode_niced_process_percent",
			"kernelmode_process_percent", "io_completion_percent",
			"servicing_irq_percent", "servicing_softirq_percent",
			"idle_mode_percent", "user_time_percent", "processor_time_percent",
			"privileged_time_percent", "interrupt_time_percent",
		)),
	},
	"pg_sys_io_analysis_info": {
		kinds: columnKinds(
			counters("total_reads", "total_writes", "read_bytes", "write_bytes"),
			timeCounters("read_time_ms", "write_time_ms"),
		),
	},
	"pg_sys_network_info": {
		kinds: columnKinds(counters(
			"tx_bytes", "tx_packets", "tx_errors", "tx_dropped",
			"rx_bytes", "rx_packets", "rx_errors", "rx_dropped",
		)),
		// The loopback interface's traffic is not network traffic, and
		// its presence varies between hosts.
		excludeEntities: "interface_name NOT IN ('lo', 'lo0')",
	},
}

// probeRegistryMu guards probeRegistry. Production code only reads it; the
// test hook in column_kinds_test.go writes to it so fixtures can register
// their own table names as probes.
var probeRegistryMu sync.RWMutex

func lookupProbe(probe string) (probeRegistryEntry, bool) {
	probeRegistryMu.RLock()
	defer probeRegistryMu.RUnlock()
	e, ok := probeRegistry[probe]
	return e, ok
}

// ColumnKindFor returns the registered kind of col in probe, or KindGauge
// when either is unregistered.
func ColumnKindFor(probe, col string) ColumnKind {
	e, ok := lookupProbe(probe)
	if !ok {
		return KindGauge
	}
	return e.kinds[col]
}

// ResetColumnFor returns the stats_reset marker column guarding col in
// probe, or "" when the probe has none.
func ResetColumnFor(probe, col string) string {
	e, ok := lookupProbe(probe)
	if !ok || e.resetColumn == nil {
		return ""
	}
	return e.resetColumn(col)
}

// ProbeEntityExclusion returns the WHERE clause fragment excluding entities
// that should never be reported for probe, or "" when there is none.
func ProbeEntityExclusion(probe string) string {
	e, ok := lookupProbe(probe)
	if !ok {
		return ""
	}
	return e.excludeEntities
}
