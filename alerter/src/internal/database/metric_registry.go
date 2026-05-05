/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

// scanType identifies how to scan metric query results
type scanType int

const (
	scanBasic        scanType = iota // (connection_id, value, collected_at)
	scanWithDB                       // (connection_id, db_name, value, collected_at)
	scanWithDBObject                 // (connection_id, db_name, object_name, value, collected_at)
)

// historicalScanType identifies how to scan historical metric query results
type historicalScanType int

const (
	historicalScanBasic  historicalScanType = iota // (connection_id, database_name, value, collected_at) where database_name is NULL
	historicalScanWithDB                           // (connection_id, database_name, value, collected_at) where database_name is a string
)

// metricQueryConfig holds SQL and scan type for a metric
type metricQueryConfig struct {
	latestSQL      string
	historicalSQL  string
	scan           scanType
	historicalScan historicalScanType
}

// metricRegistry maps metric names to their query configurations.
// Each entry contains the SQL for latest and historical queries, along with
// the scan type that determines how to parse the result rows.
var metricRegistry = map[string]metricQueryConfig{
	"pg_settings.max_connections": {
		latestSQL: `
			SELECT DISTINCT ON (connection_id)
			       connection_id, setting::float as value, collected_at
			FROM metrics.pg_settings
			WHERE name = 'max_connections'
			  AND collected_at > NOW() - INTERVAL '1 hour'
			ORDER BY connection_id, collected_at DESC
		`,
		historicalSQL: `
			SELECT DISTINCT ON (ps.connection_id)
			       ps.connection_id, NULL::text as database_name,
			       ps.setting::float as value, ps.collected_at
			FROM metrics.pg_settings ps
			JOIN connections c ON c.id = ps.connection_id
			WHERE ps.name = 'max_connections'
			  AND ps.collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY ps.connection_id, ps.collected_at DESC
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"connection_utilization_percent": {
		latestSQL: `
			WITH active_counts AS (
				SELECT connection_id, COUNT(*) as active
				FROM metrics.pg_stat_activity
				WHERE backend_type = 'client backend'
				  AND (connection_id, collected_at) IN (
				      SELECT connection_id, MAX(collected_at)
				      FROM metrics.pg_stat_activity
				      WHERE collected_at > NOW() - INTERVAL '5 minutes'
				      GROUP BY connection_id
				  )
				GROUP BY connection_id
			),
			max_conns AS (
				SELECT connection_id, setting::float as max_connections
				FROM metrics.pg_settings
				WHERE name = 'max_connections'
				  AND collected_at > NOW() - INTERVAL '1 hour'
			)
			SELECT a.connection_id,
			       (a.active / NULLIF(m.max_connections, 0)) * 100 as value,
			       NOW() as collected_at
			FROM active_counts a
			JOIN max_conns m ON a.connection_id = m.connection_id
		`,
		historicalSQL: `
			WITH activity_counts AS (
				SELECT psa.connection_id, psa.collected_at, COUNT(*) as active
				FROM metrics.pg_stat_activity psa
				JOIN connections c ON c.id = psa.connection_id
				WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
				GROUP BY psa.connection_id, psa.collected_at
			),
			max_conns AS (
				SELECT DISTINCT ON (ps.connection_id) ps.connection_id, ps.setting::float as max_connections
				FROM metrics.pg_settings ps
				JOIN connections c ON c.id = ps.connection_id
				WHERE ps.name = 'max_connections'
				ORDER BY ps.connection_id, ps.collected_at DESC
			)
			SELECT a.connection_id, NULL::text as database_name,
			       (a.active / NULLIF(m.max_connections, 0)) * 100 as value,
			       a.collected_at
			FROM activity_counts a
			JOIN max_conns m ON a.connection_id = m.connection_id
			ORDER BY a.connection_id, a.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_activity.count": {
		latestSQL: `
			SELECT connection_id,
			       COUNT(*)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`,
		historicalSQL: `
			SELECT psa.connection_id, NULL::text as database_name,
			       COUNT(*)::float as value, psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.backend_type = 'client backend'
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_replication.replay_lag_seconds": {
		latestSQL: `
			SELECT connection_id,
			       EXTRACT(EPOCH FROM (NOW() - replay_lsn_timestamp))::float as value,
			       collected_at
			FROM metrics.pg_stat_replication
			WHERE collected_at > NOW() - INTERVAL '5 minutes'
			  AND replay_lsn_timestamp IS NOT NULL
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_replication.lag_bytes": {
		latestSQL: `
			WITH recent_replication AS (
				SELECT connection_id,
				       sent_lsn,
				       replay_lsn,
				       collected_at,
				       ROW_NUMBER() OVER (PARTITION BY connection_id, pid ORDER BY collected_at DESC) as rn
				FROM metrics.pg_stat_replication
				WHERE collected_at > NOW() - INTERVAL '5 minutes'
				  AND sent_lsn IS NOT NULL
				  AND replay_lsn IS NOT NULL
			)
			SELECT connection_id,
			       COALESCE(MAX((sent_lsn::pg_lsn - replay_lsn::pg_lsn)::float), 0) as value,
			       MAX(collected_at) as collected_at
			FROM recent_replication
			WHERE rn = 1
			GROUP BY connection_id
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_replication_slots.retained_bytes": {
		latestSQL: `
			WITH recent_slots AS (
				SELECT connection_id,
				       slot_name,
				       retained_bytes,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, slot_name
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_replication_slots
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND retained_bytes IS NOT NULL
			)
			SELECT connection_id,
			       COALESCE(MAX(retained_bytes), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM recent_slots
			WHERE rn = 1
			GROUP BY connection_id
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_replication_slots.inactive": {
		latestSQL: `
			WITH slot_status AS (
				SELECT DISTINCT connection_id, 1 as has_inactive
				FROM metrics.pg_stat_replication_slots s
				WHERE s.collected_at > NOW() - INTERVAL '5 minutes'
				  AND NOT EXISTS (
				      SELECT 1 FROM metrics.pg_stat_replication r
				      WHERE r.connection_id = s.connection_id
				        AND r.collected_at > NOW() - INTERVAL '5 minutes'
				        AND r.application_name = s.slot_name
				  )
			)
			SELECT connection_id,
			       has_inactive::float as value,
			       NOW() as collected_at
			FROM slot_status
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	// pg_replication_slots.inactive_count counts replication slots that
	// have active=false in the latest metrics.pg_replication_slots sample
	// for each connection. The COUNT(*) FILTER aggregate yields zero when
	// every slot is active (the alert clears) and the inactive count when
	// any slot has dropped its WAL receiver.
	"pg_replication_slots.inactive_count": {
		latestSQL: `
			WITH latest AS (
				SELECT connection_id, MAX(collected_at) AS collected_at
				  FROM metrics.pg_replication_slots
				 GROUP BY connection_id
			)
			SELECT s.connection_id,
			       COUNT(*) FILTER (WHERE NOT s.active)::float AS value,
			       l.collected_at
			  FROM metrics.pg_replication_slots s
			  JOIN latest l
			    ON s.connection_id = l.connection_id
			   AND s.collected_at  = l.collected_at
			 GROUP BY s.connection_id, l.collected_at
		`,
		historicalSQL: `
			SELECT s.connection_id,
			       NULL::text AS database_name,
			       COUNT(*) FILTER (WHERE NOT s.active)::float AS value,
			       s.collected_at
			  FROM metrics.pg_replication_slots s
			  JOIN connections c ON c.id = s.connection_id
			 WHERE s.collected_at > NOW() - INTERVAL '1 day' * $1
			 GROUP BY s.connection_id, s.collected_at
			 ORDER BY s.connection_id, s.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	// pg_replication_slots.max_retained_bytes returns the maximum
	// retained_bytes across all replication slots in the latest sample
	// for each connection. retained_bytes is NUMERIC in the source table
	// to handle WAL retention values larger than int64; the cast to float
	// is safe for the threshold ranges the operator-facing alert rules
	// configure (1 GiB warning, 10 GiB critical).
	"pg_replication_slots.max_retained_bytes": {
		latestSQL: `
			WITH latest AS (
				SELECT connection_id, MAX(collected_at) AS collected_at
				  FROM metrics.pg_replication_slots
				 GROUP BY connection_id
			)
			SELECT s.connection_id,
			       COALESCE(MAX(s.retained_bytes), 0)::float AS value,
			       l.collected_at
			  FROM metrics.pg_replication_slots s
			  JOIN latest l
			    ON s.connection_id = l.connection_id
			   AND s.collected_at  = l.collected_at
			 GROUP BY s.connection_id, l.collected_at
		`,
		historicalSQL: `
			SELECT s.connection_id,
			       NULL::text AS database_name,
			       COALESCE(MAX(s.retained_bytes), 0)::float AS value,
			       s.collected_at
			  FROM metrics.pg_replication_slots s
			  JOIN connections c ON c.id = s.connection_id
			 WHERE s.collected_at > NOW() - INTERVAL '1 day' * $1
			 GROUP BY s.connection_id, s.collected_at
			 ORDER BY s.connection_id, s.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	// spock_exception_log.recent_count counts rows in the latest
	// metrics.spock_exception_log sample for each connection. The collector
	// re-evaluates a rolling 15-minute source-side window every probe cycle,
	// so the latest-sample count equals the live rolling-window count.
	// Counting only the latest sample (rather than across multiple samples)
	// avoids double-counting rows captured by overlapping cycles.
	//
	// The latest CTE constrains MAX(collected_at) to samples within the past
	// 5 minutes. Without the cutoff a stale-but-still-present sample (for
	// example, the source-side rolling window has drained but the probe
	// short-circuits Store on the empty result) keeps the alert active long
	// after the underlying condition resolves. Five minutes spans five
	// default 60-second probe cycles, which is enough headroom to absorb a
	// missed cycle without flapping but short enough that the alert clears
	// promptly when source rows age out of the rolling window.
	"spock_exception_log.recent_count": {
		latestSQL: `
			WITH latest AS (
				SELECT connection_id, MAX(collected_at) AS collected_at
				  FROM metrics.spock_exception_log
				 WHERE collected_at > NOW() - INTERVAL '5 minutes'
				 GROUP BY connection_id
			)
			SELECT s.connection_id,
			       COUNT(*)::float AS value,
			       l.collected_at
			  FROM metrics.spock_exception_log s
			  JOIN latest l
			    ON s.connection_id = l.connection_id
			   AND s.collected_at  = l.collected_at
			 GROUP BY s.connection_id, l.collected_at
		`,
		historicalSQL: `
			SELECT s.connection_id,
			       NULL::text AS database_name,
			       COUNT(*)::float AS value,
			       s.collected_at
			  FROM metrics.spock_exception_log s
			  JOIN connections c ON c.id = s.connection_id
			 WHERE s.collected_at > NOW() - INTERVAL '1 day' * $1
			 GROUP BY s.connection_id, s.collected_at
			 ORDER BY s.connection_id, s.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	// spock_resolutions.recent_count mirrors spock_exception_log.recent_count
	// against the metrics.spock_resolutions table. The collector applies the
	// same rolling 15-minute source-side window to spock.resolutions, so the
	// latest-sample count equals the live rolling-window count.
	//
	// As with spock_exception_log.recent_count the latest CTE constrains
	// MAX(collected_at) to samples within the past 5 minutes so a stale
	// non-empty sample cannot keep an otherwise-resolved alert active after
	// the source-side rolling window has drained.
	"spock_resolutions.recent_count": {
		latestSQL: `
			WITH latest AS (
				SELECT connection_id, MAX(collected_at) AS collected_at
				  FROM metrics.spock_resolutions
				 WHERE collected_at > NOW() - INTERVAL '5 minutes'
				 GROUP BY connection_id
			)
			SELECT s.connection_id,
			       COUNT(*)::float AS value,
			       l.collected_at
			  FROM metrics.spock_resolutions s
			  JOIN latest l
			    ON s.connection_id = l.connection_id
			   AND s.collected_at  = l.collected_at
			 GROUP BY s.connection_id, l.collected_at
		`,
		historicalSQL: `
			SELECT s.connection_id,
			       NULL::text AS database_name,
			       COUNT(*)::float AS value,
			       s.collected_at
			  FROM metrics.spock_resolutions s
			  JOIN connections c ON c.id = s.connection_id
			 WHERE s.collected_at > NOW() - INTERVAL '1 day' * $1
			 GROUP BY s.connection_id, s.collected_at
			 ORDER BY s.connection_id, s.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_replication.standby_disconnected": {
		latestSQL: `
			WITH recent_standby AS (
				SELECT connection_id,
				       receiver_status,
				       collected_at,
				       ROW_NUMBER() OVER (PARTITION BY connection_id ORDER BY collected_at DESC) as rn
				FROM metrics.pg_stat_replication
				WHERE collected_at > NOW() - INTERVAL '5 minutes'
				  AND role = 'standby'
			)
			SELECT connection_id,
			       CASE WHEN receiver_status IS NULL THEN 1 ELSE 0 END::float as value,
			       collected_at
			FROM recent_standby
			WHERE rn = 1
			  AND receiver_status IS NULL
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_node_role.subscription_worker_down": {
		latestSQL: `
			WITH recent_node_role AS (
				SELECT connection_id,
				       subscription_count,
				       active_subscription_count,
				       collected_at,
				       ROW_NUMBER() OVER (PARTITION BY connection_id ORDER BY collected_at DESC) as rn
				FROM metrics.pg_node_role
				WHERE collected_at > NOW() - INTERVAL '5 minutes'
				  AND subscription_count > 0
			)
			SELECT connection_id,
			       1::float as value,
			       collected_at
			FROM recent_node_role
			WHERE rn = 1
			  AND active_subscription_count < subscription_count
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_activity.blocked_count": {
		latestSQL: `
			SELECT connection_id,
			       COUNT(*)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE wait_event_type = 'Lock'
			  AND backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`,
		historicalSQL: `
			SELECT psa.connection_id, NULL::text as database_name,
			       COUNT(*)::float as value, psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.wait_event_type = 'Lock'
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_activity.idle_in_transaction_seconds": {
		latestSQL: `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - xact_start))), 0)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE state = 'idle in transaction'
			  AND xact_start IS NOT NULL
			  AND backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`,
		historicalSQL: `
			SELECT psa.connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (psa.collected_at - psa.xact_start))), 0)::float as value,
			       psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.state = 'idle in transaction'
			  AND psa.xact_start IS NOT NULL
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_activity.max_lock_wait_seconds": {
		latestSQL: `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - query_start))), 0)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE wait_event_type = 'Lock'
			  AND query_start IS NOT NULL
			  AND backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_activity.max_query_duration_seconds": {
		latestSQL: `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - query_start))), 0)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE state = 'active'
			  AND query_start IS NOT NULL
			  AND backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`,
		historicalSQL: `
			SELECT psa.connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (psa.collected_at - psa.query_start))), 0)::float as value,
			       psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.state = 'active'
			  AND psa.query_start IS NOT NULL
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_activity.max_xact_duration_seconds": {
		latestSQL: `
			SELECT connection_id,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (collected_at - xact_start))), 0)::float as value,
			       collected_at
			FROM metrics.pg_stat_activity
			WHERE xact_start IS NOT NULL
			  AND backend_type = 'client backend'
			  AND (connection_id, collected_at) IN (
			      SELECT connection_id, MAX(collected_at)
			      FROM metrics.pg_stat_activity
			      WHERE collected_at > NOW() - INTERVAL '5 minutes'
			      GROUP BY connection_id
			  )
			GROUP BY connection_id, collected_at
		`,
		historicalSQL: `
			SELECT psa.connection_id, NULL::text as database_name,
			       COALESCE(MAX(EXTRACT(EPOCH FROM (psa.collected_at - psa.xact_start))), 0)::float as value,
			       psa.collected_at
			FROM metrics.pg_stat_activity psa
			JOIN connections c ON c.id = psa.connection_id
			WHERE psa.collected_at > NOW() - INTERVAL '1 day' * $1
			  AND psa.xact_start IS NOT NULL
			GROUP BY psa.connection_id, psa.collected_at
			ORDER BY psa.connection_id, psa.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_all_tables.dead_tuple_percent": {
		latestSQL: `
			WITH recent_tables AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       n_live_tup,
				       n_dead_tup,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name, schemaname, relname
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_stat_all_tables
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND (n_live_tup + n_dead_tup) >= 1000
			),
			calculated AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       (n_dead_tup::float / NULLIF(n_live_tup + n_dead_tup, 0)) * 100 as dead_pct,
				       collected_at
				FROM recent_tables
				WHERE rn = 1
			),
			ranked AS (
				SELECT *,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY dead_pct DESC
				       ) as rank
				FROM calculated
			)
			SELECT connection_id,
			       database_name,
			       schemaname || '.' || relname as object_name,
			       dead_pct::float as value,
			       collected_at
			FROM ranked
			WHERE rank = 1
		`,
		historicalSQL:  "",
		scan:           scanWithDBObject,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_archiver.failed_count_delta": {
		latestSQL: `
			WITH archiver_data AS (
				SELECT connection_id,
				       failed_count,
				       collected_at,
				       LAG(failed_count) OVER (PARTITION BY connection_id ORDER BY collected_at) as prev_failed_count
				FROM metrics.pg_stat_archiver
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
			)
			SELECT connection_id,
			       COALESCE(MAX(failed_count - COALESCE(prev_failed_count, failed_count)), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM archiver_data
			WHERE prev_failed_count IS NOT NULL
			GROUP BY connection_id
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_checkpointer.checkpoints_req_delta": {
		latestSQL: `
			WITH checkpointer_data AS (
				SELECT connection_id,
				       num_requested,
				       collected_at,
				       LAG(num_requested) OVER (PARTITION BY connection_id ORDER BY collected_at) as prev_num_requested
				FROM metrics.pg_stat_checkpointer
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
			)
			SELECT connection_id,
			       COALESCE(MAX(num_requested - COALESCE(prev_num_requested, num_requested)), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM checkpointer_data
			WHERE prev_num_requested IS NOT NULL
			GROUP BY connection_id
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_stat_database.cache_hit_ratio": {
		latestSQL: `
			WITH db_blocks AS (
				SELECT connection_id,
				       database_name,
				       blks_hit,
				       blks_read,
				       collected_at,
				       LAG(blks_hit) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_blks_hit,
				       LAG(blks_read) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_blks_read
				FROM metrics.pg_stat_database
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND datname IS NOT NULL
				  AND datname NOT LIKE 'template%'
			),
			deltas AS (
				SELECT connection_id,
				       database_name,
				       (blks_hit - prev_blks_hit) as delta_hit,
				       (blks_read - prev_blks_read) as delta_read,
				       collected_at
				FROM db_blocks
				WHERE prev_blks_hit IS NOT NULL
				  AND (blks_hit - prev_blks_hit + blks_read - prev_blks_read) >= 10000
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
		`,
		historicalSQL: `
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
				       ) as prev_blks_read
				FROM metrics.pg_stat_database m
				JOIN connections c ON c.id = m.connection_id
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
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
				  AND (blks_hit - prev_blks_hit + blks_read - prev_blks_read) >= 10000
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
		scan:           scanWithDB,
		historicalScan: historicalScanWithDB,
	},

	"pg_stat_database.deadlocks_delta": {
		latestSQL: `
			WITH db_deadlocks AS (
				SELECT connection_id,
				       database_name,
				       deadlocks,
				       collected_at,
				       LAG(deadlocks) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_deadlocks
				FROM metrics.pg_stat_database
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND datname IS NOT NULL
				  AND datname NOT LIKE 'template%'
			)
			SELECT connection_id,
			       database_name,
			       COALESCE(MAX(deadlocks - COALESCE(prev_deadlocks, deadlocks)), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM db_deadlocks
			WHERE prev_deadlocks IS NOT NULL
			GROUP BY connection_id, database_name
		`,
		historicalSQL: `
			WITH db_deadlocks AS (
				SELECT m.connection_id, m.database_name, m.deadlocks, m.collected_at,
				       LAG(m.deadlocks) OVER (
				           PARTITION BY m.connection_id, m.database_name
				           ORDER BY m.collected_at
				       ) as prev_deadlocks
				FROM metrics.pg_stat_database m
				JOIN connections c ON c.id = m.connection_id
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.datname IS NOT NULL
				  AND m.datname NOT LIKE 'template%'
			)
			SELECT connection_id, database_name,
			       (deadlocks - COALESCE(prev_deadlocks, deadlocks))::float as value,
			       collected_at
			FROM db_deadlocks
			WHERE prev_deadlocks IS NOT NULL
			ORDER BY connection_id, database_name, collected_at
		`,
		scan:           scanWithDB,
		historicalScan: historicalScanWithDB,
	},

	"pg_stat_database.temp_files_delta": {
		latestSQL: `
			WITH db_temp_files AS (
				SELECT connection_id,
				       database_name,
				       temp_files,
				       collected_at,
				       LAG(temp_files) OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY collected_at
				       ) as prev_temp_files
				FROM metrics.pg_stat_database
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND datname IS NOT NULL
				  AND datname NOT LIKE 'template%'
			)
			SELECT connection_id,
			       database_name,
			       COALESCE(MAX(temp_files - COALESCE(prev_temp_files, temp_files)), 0)::float as value,
			       MAX(collected_at) as collected_at
			FROM db_temp_files
			WHERE prev_temp_files IS NOT NULL
			GROUP BY connection_id, database_name
		`,
		historicalSQL: `
			WITH db_temp_files AS (
				SELECT m.connection_id, m.database_name, m.temp_files, m.collected_at,
				       LAG(m.temp_files) OVER (
				           PARTITION BY m.connection_id, m.database_name
				           ORDER BY m.collected_at
				       ) as prev_temp_files
				FROM metrics.pg_stat_database m
				JOIN connections c ON c.id = m.connection_id
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.datname IS NOT NULL
				  AND m.datname NOT LIKE 'template%'
			)
			SELECT connection_id, database_name,
			       (temp_files - COALESCE(prev_temp_files, temp_files))::float as value,
			       collected_at
			FROM db_temp_files
			WHERE prev_temp_files IS NOT NULL
			ORDER BY connection_id, database_name, collected_at
		`,
		scan:           scanWithDB,
		historicalScan: historicalScanWithDB,
	},

	"pg_stat_statements.slow_query_count": {
		latestSQL: `
			WITH recent_statements AS (
				SELECT connection_id,
				       database_name,
				       queryid,
				       mean_exec_time,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name, queryid
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_stat_statements
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
			)
			SELECT connection_id,
			       database_name,
			       COUNT(*)::float as value,
			       MAX(collected_at) as collected_at
			FROM recent_statements
			WHERE rn = 1
			  AND mean_exec_time > 1000
			GROUP BY connection_id, database_name
		`,
		historicalSQL:  "",
		scan:           scanWithDB,
		historicalScan: historicalScanBasic,
	},

	"pg_sys_cpu_usage_info.processor_time_percent": {
		latestSQL: `
			SELECT connection_id,
			       COALESCE(processor_time_percent, 0)::float as value,
			       collected_at
			FROM (
			    SELECT DISTINCT ON (connection_id)
			           connection_id, processor_time_percent, collected_at
			    FROM metrics.pg_sys_cpu_usage_info
			    WHERE collected_at > NOW() - INTERVAL '15 minutes'
			    ORDER BY connection_id, collected_at DESC
			) latest
		`,
		historicalSQL: `
			SELECT m.connection_id, NULL::text as database_name,
			       COALESCE(m.processor_time_percent, 0)::float as value,
			       m.collected_at
			FROM metrics.pg_sys_cpu_usage_info m
			JOIN connections c ON c.id = m.connection_id
			WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY m.connection_id, m.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_sys_disk_info.used_percent": {
		latestSQL: `
			WITH recent_disk AS (
				SELECT connection_id,
				       mount_point,
				       total_space,
				       used_space,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, mount_point
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_sys_disk_info
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND total_space > 0
			)
			SELECT connection_id,
			       MAX((used_space::float / total_space) * 100)::float as value,
			       MAX(collected_at) as collected_at
			FROM recent_disk
			WHERE rn = 1
			GROUP BY connection_id
		`,
		historicalSQL: `
			WITH disk_data AS (
				SELECT m.connection_id, m.collected_at,
				       MAX((m.used_space::float / NULLIF(m.total_space, 0)) * 100) as value
				FROM metrics.pg_sys_disk_info m
				JOIN connections c ON c.id = m.connection_id
				WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
				  AND m.total_space > 0
				GROUP BY m.connection_id, m.collected_at
			)
			SELECT connection_id, NULL::text as database_name,
			       value::float, collected_at
			FROM disk_data
			ORDER BY connection_id, collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_sys_load_avg_info.load_avg_fifteen_minutes": {
		latestSQL: `
			SELECT connection_id,
			       COALESCE(load_avg_fifteen_minutes, 0)::float as value,
			       collected_at
			FROM (
			    SELECT DISTINCT ON (connection_id)
			           connection_id, load_avg_fifteen_minutes, collected_at
			    FROM metrics.pg_sys_load_avg_info
			    WHERE collected_at > NOW() - INTERVAL '15 minutes'
			    ORDER BY connection_id, collected_at DESC
			) latest
		`,
		historicalSQL: `
			SELECT m.connection_id, NULL::text as database_name,
			       COALESCE(m.load_avg_fifteen_minutes, 0)::float as value,
			       m.collected_at
			FROM metrics.pg_sys_load_avg_info m
			JOIN connections c ON c.id = m.connection_id
			WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY m.connection_id, m.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"pg_sys_memory_info.used_percent": {
		latestSQL: `
			SELECT connection_id,
			       CASE
			           WHEN total_memory > 0
			           THEN (used_memory::float / total_memory) * 100
			           ELSE 0
			       END as value,
			       collected_at
			FROM (
			    SELECT DISTINCT ON (connection_id)
			           connection_id, total_memory, used_memory, collected_at
			    FROM metrics.pg_sys_memory_info
			    WHERE collected_at > NOW() - INTERVAL '15 minutes'
			    ORDER BY connection_id, collected_at DESC
			) latest
		`,
		historicalSQL: `
			SELECT m.connection_id, NULL::text as database_name,
			       CASE
			           WHEN m.total_memory > 0
			           THEN (m.used_memory::float / m.total_memory) * 100
			           ELSE 0
			       END as value,
			       m.collected_at
			FROM metrics.pg_sys_memory_info m
			JOIN connections c ON c.id = m.connection_id
			WHERE m.collected_at > NOW() - INTERVAL '1 day' * $1
			ORDER BY m.connection_id, m.collected_at
		`,
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"age_percent": {
		latestSQL: `
			WITH freeze_settings AS (
				SELECT DISTINCT ON (connection_id)
				       connection_id,
				       setting::bigint as freeze_max_age
				FROM metrics.pg_settings
				WHERE name = 'autovacuum_freeze_max_age'
				  AND collected_at > NOW() - INTERVAL '1 hour'
				ORDER BY connection_id, collected_at DESC
			),
			table_ages AS (
				SELECT t.connection_id,
				       t.database_name,
				       t.relname,
				       COALESCE(t.n_live_tup, 0) as n_live_tup,
				       t.collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY t.connection_id, t.database_name, t.schemaname, t.relname
				           ORDER BY t.collected_at DESC
				       ) as rn
				FROM metrics.pg_stat_all_tables t
				WHERE t.collected_at > NOW() - INTERVAL '15 minutes'
			)
			SELECT ta.connection_id,
			       50.0::float as value,
			       MAX(ta.collected_at) as collected_at
			FROM table_ages ta
			JOIN freeze_settings fs ON ta.connection_id = fs.connection_id
			WHERE ta.rn = 1
			GROUP BY ta.connection_id
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	"table_bloat_ratio": {
		latestSQL: `
			WITH recent_tables AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       n_live_tup,
				       n_dead_tup,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name, schemaname, relname
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_stat_all_tables
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND n_live_tup >= 1000
				  AND schemaname NOT IN ('pg_catalog', 'pg_toast', 'information_schema')
			),
			calculated AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       (n_dead_tup::float / NULLIF(n_live_tup, 0)) * 100 as bloat_ratio,
				       collected_at
				FROM recent_tables
				WHERE rn = 1
			),
			ranked AS (
				SELECT *,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY bloat_ratio DESC
				       ) as rank
				FROM calculated
			)
			SELECT connection_id,
			       database_name,
			       schemaname || '.' || relname as object_name,
			       bloat_ratio::float as value,
			       collected_at
			FROM ranked
			WHERE rank = 1
		`,
		historicalSQL:  "",
		scan:           scanWithDBObject,
		historicalScan: historicalScanBasic,
	},

	"table_last_autovacuum_hours": {
		latestSQL: `
			WITH recent_tables AS (
				SELECT connection_id,
				       database_name,
				       schemaname,
				       relname,
				       n_live_tup,
				       n_dead_tup,
				       last_autovacuum,
				       collected_at,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name, schemaname, relname
				           ORDER BY collected_at DESC
				       ) as rn
				FROM metrics.pg_stat_all_tables
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				  AND schemaname NOT IN ('pg_catalog', 'pg_toast', 'information_schema')
			),
			av_settings AS (
				SELECT DISTINCT ON (connection_id)
				       connection_id,
				       MAX(CASE WHEN name = 'autovacuum_vacuum_threshold'
				           THEN setting::float ELSE NULL END) as av_threshold,
				       MAX(CASE WHEN name = 'autovacuum_vacuum_scale_factor'
				           THEN setting::float ELSE NULL END) as av_scale_factor
				FROM metrics.pg_settings
				WHERE name IN ('autovacuum_vacuum_threshold', 'autovacuum_vacuum_scale_factor')
				  AND collected_at > NOW() - INTERVAL '1 hour'
				GROUP BY connection_id
			),
			exceeding AS (
				SELECT t.connection_id,
				       t.database_name,
				       t.schemaname,
				       t.relname,
				       t.n_dead_tup,
				       COALESCE(s.av_threshold, 50) + COALESCE(s.av_scale_factor, 0.2) * t.n_live_tup as calc_threshold,
				       EXTRACT(EPOCH FROM (NOW() - COALESCE(t.last_autovacuum, '1970-01-01'::timestamptz))) / 3600 as hours_since_vacuum,
				       t.collected_at
				FROM recent_tables t
				LEFT JOIN av_settings s ON t.connection_id = s.connection_id
				WHERE t.rn = 1
				  AND t.n_dead_tup > (COALESCE(s.av_threshold, 50) + COALESCE(s.av_scale_factor, 0.2) * t.n_live_tup)
			),
			ranked AS (
				SELECT *,
				       ROW_NUMBER() OVER (
				           PARTITION BY connection_id, database_name
				           ORDER BY hours_since_vacuum DESC
				       ) as rank
				FROM exceeding
			)
			SELECT connection_id,
			       database_name,
			       schemaname || '.' || relname as object_name,
			       hours_since_vacuum::float as value,
			       collected_at
			FROM ranked
			WHERE rank = 1
		`,
		historicalSQL:  "",
		scan:           scanWithDBObject,
		historicalScan: historicalScanBasic,
	},
}
