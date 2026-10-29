/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package anomaly

import (
	"sort"
	"time"
)

// HistoricalScan identifies the column layout of a historical query's
// rows.
type HistoricalScan int

const (
	// HistoricalScanBasic rows are (connection_id, database_name, value,
	// collected_at) where database_name is NULL.
	HistoricalScanBasic HistoricalScan = iota

	// HistoricalScanWithDB rows are (connection_id, database_name, value,
	// collected_at) where database_name is a string.
	HistoricalScanWithDB

	// HistoricalScanWithDBAndSamples is HistoricalScanWithDB plus a
	// trailing sample_count column: (connection_id, database_name, value,
	// collected_at, sample_count). It is for queries whose rows aggregate
	// several raw samples into one bucket, so the count of underlying
	// samples can no longer be inferred from the number of rows. See
	// GitHub issue #409.
	HistoricalScanWithDBAndSamples

	// HistoricalScanBasicWithSamples is the per-connection counterpart
	// of HistoricalScanWithDBAndSamples: (connection_id, database_name,
	// value, collected_at, sample_count) where database_name is NULL.
	// It is for bucketed queries over metrics that have no database
	// dimension. See GitHub issue #490.
	HistoricalScanBasicWithSamples
)

// PerDatabase reports whether rows of this layout carry a non-NULL
// database_name.
func (s HistoricalScan) PerDatabase() bool {
	return s == HistoricalScanWithDB || s == HistoricalScanWithDBAndSamples
}

// MetricDef is the shared definition of one baselined metric.
type MetricDef struct {
	// Name is the metric name, as used by alert rules and
	// metric_baselines.metric_name.
	Name string

	// ProbeName is the collector probe whose metrics table the queries
	// read.
	ProbeName string

	// HistoricalSQL reads the samples a baseline is computed from. $1 is
	// the lookback in days and $2 an inclusive upper bound on
	// collected_at, applied in the same predicate as the lookback so a
	// caller reading a past window does not scan up to the present (the
	// alerter passes +infinity); rows are (connection_id, database_name,
	// value, collected_at), plus a trailing sample_count for the
	// *WithSamples scan kinds, ordered by connection and time. Every
	// query joins live connections only.
	HistoricalSQL string

	// HistoricalScan is the layout of HistoricalSQL's rows.
	HistoricalScan HistoricalScan

	// PerDatabase is true when rows carry a non-NULL database_name; it
	// is derived from HistoricalScan.
	PerDatabase bool

	// BucketWidth is the width of the buckets HistoricalSQL aggregates
	// samples into, each row's collected_at being its bucket's start
	// truncated in UTC; zero means each row is a raw sample. Every query
	// that buckets by date_trunc('hour', ...) sets it to time.Hour.
	BucketWidth time.Duration
}

// BucketStart returns the start of the bucket holding t, in UTC: t
// truncated to BucketWidth, or t itself for raw samples. A row whose
// collected_at is BucketStart(t) covers t, so a reader selecting rows
// from t onwards must start there or lose the bucket straddling t.
func (d MetricDef) BucketStart(t time.Time) time.Time {
	t = t.UTC()
	if d.BucketWidth <= 0 {
		return t
	}
	// Truncate measures from the zero time, which is midnight UTC, so
	// any width dividing a day aligns with the UTC buckets date_trunc
	// produces.
	return t.Truncate(d.BucketWidth)
}

// Metric returns the definition of a baselined metric. ok is false, and
// the definition zero, for any other name.
func Metric(name string) (MetricDef, bool) {
	def, ok := metricDefs[name]
	if !ok {
		return MetricDef{}, false
	}
	def.Name = name
	def.PerDatabase = def.HistoricalScan.PerDatabase()
	return def, true
}

// BaselineMetrics returns the definition of every baselined metric,
// sorted by name.
func BaselineMetrics() []MetricDef {
	names := make([]string, 0, len(metricDefs))
	for name := range metricDefs {
		names = append(names, name)
	}
	sort.Strings(names)
	defs := make([]MetricDef, 0, len(names))
	for _, name := range names {
		def, _ := Metric(name)
		defs = append(defs, def)
	}
	return defs
}

// CPUBusyPercentExpr is the SQL expression that derives a portable CPU
// busy percentage from a metrics.pg_sys_cpu_usage_info row.
//
// The system_stats extension fills different columns on different
// platforms: processor_time_percent is Windows-only, while the Linux
// build reports the individual busy buckets and idle_mode_percent. The
// expression therefore prefers the Windows column, falls back to
// 100 minus the idle percentage, and finally sums the Linux busy
// buckets, clamping the result to the 0-100 range. Column names are
// left unqualified so the expression can be embedded in both the latest
// and historical queries.
const CPUBusyPercentExpr = `
	LEAST(GREATEST(COALESCE(
	    processor_time_percent,
	    100 - idle_mode_percent,
	    COALESCE(usermode_normal_process_percent, 0)
	        + COALESCE(usermode_niced_process_percent, 0)
	        + COALESCE(kernelmode_process_percent, 0)
	        + COALESCE(io_completion_percent, 0)
	        + COALESCE(servicing_irq_percent, 0)
	        + COALESCE(servicing_softirq_percent, 0)
	), 0), 100)::float`

// PseudoFilesystemExclusion is the SQL predicate that keeps a
// metrics.pg_sys_disk_info row only when the mount is a real volume.
// Kernel-backed and image-backed filesystems are never something a DBA
// provisions or can free space on, and a squashfs image is by
// construction 100% used, so on any host with a snap installed the MAX
// across mounts pinned pg_sys_disk_info.used_percent at 100% and the
// disk alert fired permanently.
//
// The COALESCE is load-bearing: file_system_type is nullable, and a
// bare NOT IN comparison against NULL yields NULL rather than true,
// which would silently drop every row whose type the probe did not
// record. The fragment is declared once and embedded in both the latest
// and the historical query so the two cannot drift apart; the column
// name is left unqualified so it can be embedded in either. See GitHub
// issue #428.
const PseudoFilesystemExclusion = `COALESCE(file_system_type, '') NOT IN (
			      'tmpfs', 'devtmpfs', 'devfs', 'proc', 'sysfs', 'cgroup',
			      'cgroup2', 'overlay', 'squashfs', 'ramfs', 'debugfs',
			      'tracefs', 'securityfs', 'pstore', 'autofs', 'mqueue',
			      'hugetlbfs', 'configfs', 'fusectl', 'binfmt_misc',
			      'efivarfs', 'nsfs', 'bpf'
			  )`

// metricDefs holds every baselined metric's historical query. Only
// metrics listed here are baselined and scored; the alerter's metric
// registry (alerter/src/internal/database/metric_registry.go) reads its
// historical queries from here and pairs each with the latest-value
// query, and the comment on each registry entry explains both. A
// historical query must compute the statistic its latest query computes,
// so change the two together.
//
// Name and PerDatabase are filled in by Metric.
var metricDefs = map[string]MetricDef{
	"connection_utilization_percent": {
		ProbeName:      "pg_stat_activity",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			WITH activity_counts AS (
				SELECT psa.connection_id, psa.collected_at, COUNT(*) as active
				FROM metrics.pg_stat_activity psa
				JOIN connections c ON c.id = psa.connection_id AND c.deleted_at IS NULL
				WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND psa.collected_at <= $2
				  AND psa.backend_type = 'client backend'
				GROUP BY psa.connection_id, psa.collected_at
			),
			max_conns AS (
				SELECT ps.connection_id,
				       ps.setting::float as max_connections,
				       CASE WHEN LAG(ps.collected_at) OVER w IS NULL
				            THEN '-infinity'::timestamptz
				            ELSE ps.collected_at END as valid_from,
				       COALESCE(LEAD(ps.collected_at) OVER w,
				                'infinity'::timestamptz) as valid_to
				FROM metrics.pg_settings ps
				JOIN connections c ON c.id = ps.connection_id AND c.deleted_at IS NULL
				WHERE ps.name = 'max_connections'
				WINDOW w AS (PARTITION BY ps.connection_id ORDER BY ps.collected_at)
			)
			SELECT a.connection_id, NULL::text as database_name,
			       (a.active / NULLIF(m.max_connections, 0)) * 100 as value,
			       a.collected_at
			FROM activity_counts a
			JOIN max_conns m
			  ON a.connection_id = m.connection_id
			 AND a.collected_at >= m.valid_from
			 AND a.collected_at < m.valid_to
			ORDER BY a.connection_id, a.collected_at
		`,
	},
	"pg_stat_activity.count": {
		ProbeName:      "pg_stat_activity",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT psa.connection_id, NULL::text as database_name,
			       COUNT(*)::float as value, psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id AND c.deleted_at IS NULL
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.collected_at <= $2
			  AND psa.backend_type = 'client backend'
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
		`,
	},
	"pg_stat_replication.lag_bytes": {
		ProbeName:      "pg_stat_replication",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT r.connection_id, NULL::text as database_name,
			       MAX((r.sent_lsn::pg_lsn - r.replay_lsn::pg_lsn)::float) as value,
			       r.collected_at
			FROM metrics.pg_stat_replication r
			JOIN connections c ON c.id = r.connection_id AND c.deleted_at IS NULL
			WHERE r.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND r.collected_at <= $2
			  AND r.sent_lsn IS NOT NULL
			  AND r.replay_lsn IS NOT NULL
			GROUP BY r.connection_id, r.collected_at
			ORDER BY r.connection_id, r.collected_at
		`,
	},
	"pg_replication_slots.max_retained_bytes": {
		ProbeName:      "pg_replication_slots",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT s.connection_id,
			       NULL::text AS database_name,
			       COALESCE(MAX(s.retained_bytes), 0)::float AS value,
			       s.collected_at
			  FROM metrics.pg_replication_slots s
			  JOIN connections c ON c.id = s.connection_id AND c.deleted_at IS NULL
			 WHERE s.collected_at > NOW() - INTERVAL '1 day' * $1
			   AND s.collected_at <= $2
			 GROUP BY s.connection_id, s.collected_at
			 ORDER BY s.connection_id, s.collected_at
		`,
	},
	"spock_exception_log.recent_count": {
		ProbeName:      "spock_exception_log",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT s.connection_id,
			       NULL::text AS database_name,
			       COUNT(*)::float AS value,
			       s.collected_at
			  FROM metrics.spock_exception_log s
			  JOIN connections c ON c.id = s.connection_id AND c.deleted_at IS NULL
			 WHERE s.collected_at > NOW() - INTERVAL '1 day' * $1
			   AND s.collected_at <= $2
			 GROUP BY s.connection_id, s.collected_at
			 ORDER BY s.connection_id, s.collected_at
		`,
	},
	"spock_resolutions.recent_count": {
		ProbeName:      "spock_resolutions",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT s.connection_id,
			       NULL::text AS database_name,
			       COUNT(*)::float AS value,
			       s.collected_at
			  FROM metrics.spock_resolutions s
			  JOIN connections c ON c.id = s.connection_id AND c.deleted_at IS NULL
			 WHERE s.collected_at > NOW() - INTERVAL '1 day' * $1
			   AND s.collected_at <= $2
			 GROUP BY s.connection_id, s.collected_at
			 ORDER BY s.connection_id, s.collected_at
		`,
	},
	"pg_stat_activity.blocked_count": {
		ProbeName:      "pg_stat_activity",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT psa.connection_id, NULL::text as database_name,
			       COUNT(*)::float as value, psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id AND c.deleted_at IS NULL
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.collected_at <= $2
			  AND psa.wait_event_type = 'Lock'
			  AND psa.backend_type = 'client backend'
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
		`,
	},
	"pg_stat_activity.idle_in_transaction_seconds": {
		ProbeName:      "pg_stat_activity",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT psa.connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (psa.collected_at - psa.xact_start))), 0)::float as value,
			       psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id AND c.deleted_at IS NULL
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.collected_at <= $2
			  AND psa.state = 'idle in transaction'
			  AND psa.xact_start IS NOT NULL
			  AND psa.backend_type = 'client backend'
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
		`,
	},
	"pg_stat_activity.max_lock_wait_seconds": {
		ProbeName:      "pg_stat_activity",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT psa.connection_id, NULL::text as database_name,
			       MAX(EXTRACT(EPOCH FROM (psa.collected_at - psa.query_start)))::float as value,
			       psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id AND c.deleted_at IS NULL
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.collected_at <= $2
			  AND psa.wait_event_type = 'Lock'
			  AND psa.query_start IS NOT NULL
			  AND psa.backend_type = 'client backend'
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
		`,
	},
	"pg_stat_activity.max_query_duration_seconds": {
		ProbeName:      "pg_stat_activity",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT psa.connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (psa.collected_at - psa.query_start))), 0)::float as value,
			       psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id AND c.deleted_at IS NULL
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.collected_at <= $2
			  AND psa.state = 'active'
			  AND psa.query_start IS NOT NULL
			  AND psa.backend_type = 'client backend'
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
		`,
	},
	"pg_stat_activity.max_xact_duration_seconds": {
		ProbeName:      "pg_stat_activity",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT psa.connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (psa.collected_at - psa.xact_start))), 0)::float as value,
			       psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id AND c.deleted_at IS NULL
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.collected_at <= $2
			  AND psa.xact_start IS NOT NULL
			  AND psa.backend_type = 'client backend'
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
		`,
	},
	"pg_stat_checkpointer.checkpoints_req_delta": {
		ProbeName:      "pg_stat_checkpointer",
		HistoricalScan: HistoricalScanBasicWithSamples,
		BucketWidth:    time.Hour,
		HistoricalSQL: `
			WITH checkpointer_data AS (
				SELECT m.connection_id,
				       m.num_requested,
				       m.collected_at,
				       date_trunc('hour', m.collected_at AT TIME ZONE 'UTC') as bucket,
				       LAG(m.num_requested) OVER (
				           PARTITION BY m.connection_id,
				                        date_trunc('hour', m.collected_at AT TIME ZONE 'UTC')
				           ORDER BY m.collected_at
				       ) as prev_num_requested
				FROM metrics.pg_stat_checkpointer m
				JOIN connections c ON c.id = m.connection_id AND c.deleted_at IS NULL
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.collected_at <= $2
			),
			bucket_role AS (
				SELECT DISTINCT ON (connection_id, date_trunc('hour', collected_at AT TIME ZONE 'UTC'))
				       connection_id,
				       date_trunc('hour', collected_at AT TIME ZONE 'UTC') as bucket,
				       is_in_recovery
				FROM metrics.pg_node_role
				WHERE collected_at > NOW() - INTERVAL '1 day' * $1
				  AND collected_at <= $2
				ORDER BY connection_id,
				         date_trunc('hour', collected_at AT TIME ZONE 'UTC'),
				         collected_at DESC
			)
			SELECT cd.connection_id,
			       NULL::text as database_name,
			       COALESCE(
			           SUM(GREATEST(cd.num_requested - cd.prev_num_requested, 0))
			               FILTER (WHERE cd.prev_num_requested IS NOT NULL),
			           0)::float as value,
			       cd.bucket AT TIME ZONE 'UTC' as collected_at,
			       COUNT(*) as sample_count
			FROM checkpointer_data cd
			WHERE NOT EXISTS (
			    SELECT 1
			    FROM bucket_role r
			    WHERE r.connection_id = cd.connection_id
			      AND r.bucket = cd.bucket
			      AND r.is_in_recovery
			)
			GROUP BY cd.connection_id, cd.bucket
			ORDER BY cd.connection_id, collected_at
		`,
	},
	"pg_stat_database.cache_hit_ratio": {
		ProbeName:      "pg_stat_database",
		HistoricalScan: HistoricalScanWithDB,
		HistoricalSQL: `
			WITH db_blocks AS (
				SELECT m.connection_id,
				       m.database_name,
				       m.blks_hit,
				       m.blks_read,
				       m.collected_at,
				       LAG(m.blks_hit) OVER (
				           PARTITION BY m.connection_id, m.database_name
				           ORDER BY m.collected_at
				       ) as prev_blks_hit,
				       LAG(m.blks_read) OVER (
				           PARTITION BY m.connection_id, m.database_name
				           ORDER BY m.collected_at
				       ) as prev_blks_read,
				       LAG(m.collected_at) OVER (
				           PARTITION BY m.connection_id, m.database_name
				           ORDER BY m.collected_at
				       ) as prev_collected_at
				FROM metrics.pg_stat_database m
				JOIN connections c ON c.id = m.connection_id AND c.deleted_at IS NULL
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.collected_at <= $2
				  AND m.datname IS NOT NULL
				  AND m.datname NOT LIKE 'template%'
			),
			deltas AS (
				SELECT connection_id,
				       database_name,
				       (blks_hit - prev_blks_hit) as delta_hit,
				       (blks_read - prev_blks_read) as delta_read,
				       collected_at
				FROM db_blocks
				WHERE prev_blks_hit IS NOT NULL
				  AND collected_at > prev_collected_at
				  AND (blks_hit - prev_blks_hit + blks_read - prev_blks_read) >= 10000
				  AND (blks_read - prev_blks_read) >=
				      100 * EXTRACT(EPOCH FROM (collected_at - prev_collected_at))
			)
			SELECT connection_id,
			       database_name,
			       CASE
			           WHEN (delta_hit + delta_read) > 0
			           THEN (delta_hit::float / (delta_hit + delta_read)) * 100
			           ELSE 100
			       END as value,
			       collected_at
			FROM deltas
			ORDER BY connection_id, database_name, collected_at
		`,
	},
	"pg_stat_database.deadlocks_delta": {
		ProbeName:      "pg_stat_database",
		HistoricalScan: HistoricalScanWithDBAndSamples,
		BucketWidth:    time.Hour,
		HistoricalSQL: `
			WITH db_deadlocks AS (
				SELECT m.connection_id, m.database_name, m.deadlocks,
				       date_trunc('hour', m.collected_at AT TIME ZONE 'UTC') as bucket,
				       LAG(m.deadlocks) OVER (
				           PARTITION BY m.connection_id, m.database_name,
				                        date_trunc('hour', m.collected_at AT TIME ZONE 'UTC')
				           ORDER BY m.collected_at
				       ) as prev_deadlocks
				FROM metrics.pg_stat_database m
				JOIN connections c ON c.id = m.connection_id AND c.deleted_at IS NULL
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.collected_at <= $2
				  AND m.datname IS NOT NULL
				  AND m.datname NOT LIKE 'template%'
			)
			SELECT connection_id,
			       database_name,
			       COALESCE(
			           SUM(GREATEST(deadlocks - prev_deadlocks, 0))
			               FILTER (WHERE prev_deadlocks IS NOT NULL),
			           0)::float as value,
			       bucket AT TIME ZONE 'UTC' as collected_at,
			       COUNT(*) as sample_count
			FROM db_deadlocks
			GROUP BY connection_id, database_name, bucket
			ORDER BY connection_id, database_name, collected_at
		`,
	},
	"pg_stat_database.temp_files_delta": {
		ProbeName:      "pg_stat_database",
		HistoricalScan: HistoricalScanWithDBAndSamples,
		BucketWidth:    time.Hour,
		HistoricalSQL: `
			WITH db_temp_files AS (
				SELECT m.connection_id, m.database_name, m.temp_files,
				       date_trunc('hour', m.collected_at AT TIME ZONE 'UTC') as bucket,
				       LAG(m.temp_files) OVER (
				           PARTITION BY m.connection_id, m.database_name,
				                        date_trunc('hour', m.collected_at AT TIME ZONE 'UTC')
				           ORDER BY m.collected_at
				       ) as prev_temp_files
				FROM metrics.pg_stat_database m
				JOIN connections c ON c.id = m.connection_id AND c.deleted_at IS NULL
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.collected_at <= $2
				  AND m.datname IS NOT NULL
				  AND m.datname NOT LIKE 'template%'
			)
			SELECT connection_id,
			       database_name,
			       COALESCE(
			           SUM(GREATEST(temp_files - prev_temp_files, 0))
			               FILTER (WHERE prev_temp_files IS NOT NULL),
			           0)::float as value,
			       bucket AT TIME ZONE 'UTC' as collected_at,
			       COUNT(*) as sample_count
			FROM db_temp_files
			GROUP BY connection_id, database_name, bucket
			ORDER BY connection_id, database_name, collected_at
		`,
	},
	"pg_stat_statements.slow_query_count": {
		ProbeName:      "pg_stat_statements",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			WITH samples AS (
				SELECT DISTINCT ON (s.connection_id, s.queryid, s.userid,
				                    s.dbid, s.toplevel, s.collected_at)
				       s.connection_id,
				       s.queryid,
				       s.userid,
				       s.dbid,
				       s.toplevel,
				       s.calls,
				       s.total_exec_time,
				       s.collected_at
				FROM metrics.pg_stat_statements s
				JOIN connections c ON c.id = s.connection_id AND c.deleted_at IS NULL
				WHERE s.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND s.collected_at <= $2
				ORDER BY s.connection_id, s.queryid, s.userid, s.dbid,
				         s.toplevel, s.collected_at, s.database_name
			),
			paired AS (
				SELECT connection_id,
				       queryid,
				       calls,
				       total_exec_time,
				       collected_at,
				       LAG(calls) OVER identity as prev_calls,
				       LAG(total_exec_time) OVER identity as prev_total_exec_time,
				       LAG(collected_at) OVER identity as prev_collected_at
				FROM samples
				WINDOW identity AS (
				    PARTITION BY connection_id, queryid, userid, dbid, toplevel
				    ORDER BY collected_at
				)
			),
			deltas AS (
				SELECT connection_id,
				       queryid,
				       collected_at,
				       CASE WHEN calls > prev_calls
				             AND prev_collected_at > collected_at - INTERVAL '15 minutes'
				            THEN calls - prev_calls END as delta_calls,
				       CASE WHEN calls > prev_calls
				             AND prev_collected_at > collected_at - INTERVAL '15 minutes'
				            THEN total_exec_time - prev_total_exec_time END as delta_exec_time
				FROM paired
			),
			per_query AS (
				SELECT connection_id,
				       queryid,
				       collected_at,
				       SUM(delta_exec_time) / NULLIF(SUM(delta_calls), 0) as interval_mean_ms
				FROM deltas
				GROUP BY connection_id, queryid, collected_at
			)
			SELECT connection_id,
			       NULL::text as database_name,
			       COUNT(*) FILTER (WHERE interval_mean_ms > 1000)::float as value,
			       collected_at
			FROM per_query
			GROUP BY connection_id, collected_at
			ORDER BY connection_id, collected_at
		`,
	},
	"pg_sys_cpu_usage_info.processor_time_percent": {
		ProbeName:      "pg_sys_cpu_usage_info",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT m.connection_id, NULL::text as database_name,
			       ` + CPUBusyPercentExpr + ` as value,
			       m.collected_at
			FROM metrics.pg_sys_cpu_usage_info m
			JOIN connections c ON c.id = m.connection_id AND c.deleted_at IS NULL
			WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND m.collected_at <= $2
			ORDER BY m.connection_id, m.collected_at
		`,
	},
	"pg_sys_disk_info.used_percent": {
		ProbeName:      "pg_sys_disk_info",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			WITH disk_data AS (
				SELECT m.connection_id, m.collected_at,
				       MAX((m.used_space::float / NULLIF(m.total_space, 0)) * 100) as value
				FROM metrics.pg_sys_disk_info m
				JOIN connections c ON c.id = m.connection_id AND c.deleted_at IS NULL
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.collected_at <= $2
				  AND m.total_space > 0
				  AND ` + PseudoFilesystemExclusion + `
				GROUP BY m.connection_id, m.collected_at
			)
			SELECT connection_id, NULL::text as database_name,
			       value::float, collected_at
			FROM disk_data
			ORDER BY connection_id, collected_at
		`,
	},
	"pg_sys_load_avg_info.load_avg_fifteen_minutes": {
		ProbeName:      "pg_sys_load_avg_info",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT m.connection_id, NULL::text as database_name,
			       COALESCE(m.load_avg_fifteen_minutes, 0)::float as value,
			       m.collected_at
			FROM metrics.pg_sys_load_avg_info m
			JOIN connections c ON c.id = m.connection_id AND c.deleted_at IS NULL
			WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND m.collected_at <= $2
			ORDER BY m.connection_id, m.collected_at
		`,
	},
	"pg_sys_memory_info.used_percent": {
		ProbeName:      "pg_sys_memory_info",
		HistoricalScan: HistoricalScanBasic,
		HistoricalSQL: `
			SELECT m.connection_id, NULL::text as database_name,
			       CASE
			           WHEN m.total_memory > 0
			           THEN (m.used_memory::float / m.total_memory) * 100
			           ELSE 0
			       END as value,
			       m.collected_at
			FROM metrics.pg_sys_memory_info m
			JOIN connections c ON c.id = m.connection_id AND c.deleted_at IS NULL
			WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND m.collected_at <= $2
			ORDER BY m.connection_id, m.collected_at
		`,
	},
}
