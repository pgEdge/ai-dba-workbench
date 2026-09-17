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

import "time"

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

	// historicalScanWithDBAndSamples is historicalScanWithDB plus a
	// trailing sample_count column: (connection_id, database_name, value,
	// collected_at, sample_count). It is for queries whose rows aggregate
	// several raw samples into one bucket, so the count of underlying
	// samples can no longer be inferred from the number of rows. See
	// GitHub issue #409.
	historicalScanWithDBAndSamples
)

// metricQueryConfig holds SQL and scan type for a metric
type metricQueryConfig struct {
	latestSQL      string
	historicalSQL  string
	scan           scanType
	historicalScan historicalScanType

	// clearWhenAbsent tells the alert cleaner what a missing row means.
	//
	// Some latest queries emit a row for every healthy connection (a CPU
	// percentage, a per-hour checkpoint count, a cache hit ratio) so a
	// connection that vanishes from the result set has stopped reporting,
	// not recovered; the cleaner must leave its alert active and let the
	// metric_staleness rule say why. Other queries emit a row only while
	// the condition holds (a disconnected standby, a blocked backend, an
	// inactive slot), or describe an object that can legitimately be
	// dropped (a replication slot, a standby); for those a missing row is
	// the recovery signal and the cleaner must clear the alert. Set this
	// to true only for the second kind, and say why on the entry. See
	// GitHub issue #407.
	clearWhenAbsent bool

	// probeName is the collector probe that fills the metrics table the
	// latest query reads (pg_stat_activity, pg_replication_slots,
	// pg_stat_database and so on). Every entry names one, and it must
	// match a table the query selects from, which the registry audit
	// test checks.
	//
	// The alert cleaner uses it to tell "the condition ended" from "the
	// data stopped arriving" for the clearWhenAbsent entries: those
	// queries all bound collected_at, so once collection stops they go
	// empty and a genuine condition would otherwise be reported as
	// resolved. The cleaner therefore only believes an absent row when
	// this probe is currently reporting for the alert's connection. See
	// GitHub issue #407.
	probeName string

	// absenceWindow is how far back the latest query looks: the interval
	// in its collected_at cutoff, and for a query with more than one
	// cutoff the shortest of them, because that is the bound that
	// decides whether the result is empty.
	//
	// Every clearWhenAbsent entry sets it, and the registry audit test
	// checks the value against the SQL literal so the two cannot drift.
	// The alert cleaner compares it against how long ago the probe last
	// collected: an absent row only means recovery when the probe stored
	// something inside the window the query reads, because outside it
	// the query returns nothing whatever the server is doing.
	//
	// Gating on the window rather than on a multiple of the configured
	// collection interval is what makes the check hold at any interval.
	// An operator may raise probe_configs.collection_interval_seconds,
	// globally or for one connection, and a gate counted in intervals
	// then widens whilst the window stays where it is, leaving the
	// cleaner quiet exactly where ordinary collection starts emptying
	// the query. See GitHub issue #407.
	absenceWindow time.Duration
}

// cpuBusyPercentExpr is the SQL expression that derives a portable CPU
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
const cpuBusyPercentExpr = `
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

// pseudoFilesystemExclusion is the SQL predicate that keeps a
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
const pseudoFilesystemExclusion = `COALESCE(file_system_type, '') NOT IN (
			      'tmpfs', 'devtmpfs', 'devfs', 'proc', 'sysfs', 'cgroup',
			      'cgroup2', 'overlay', 'squashfs', 'ramfs', 'debugfs',
			      'tracefs', 'securityfs', 'pstore', 'autofs', 'mqueue',
			      'hugetlbfs', 'configfs', 'fusectl', 'binfmt_misc',
			      'efivarfs', 'nsfs', 'bpf'
			  )`

// metricRegistry maps metric names to their query configurations.
// Each entry contains the SQL for latest and historical queries, along with
// the scan type that determines how to parse the result rows.
var metricRegistry = map[string]metricQueryConfig{
	// metrics.pg_settings is written by a change-tracked probe that skips
	// the write whenever the settings hash is unchanged, so a stable server
	// receives one snapshot at onboarding and nothing afterwards. Any
	// max-age predicate on collected_at therefore kills the metric within
	// the hour; the latest query takes the newest row per connection with
	// no time filter, matching what the historical variant already did.
	// See GitHub issue #406.
	"pg_settings.max_connections": {
		probeName: "pg_settings",
		latestSQL: `
			SELECT DISTINCT ON (connection_id)
			       connection_id, setting::float as value, collected_at
			FROM metrics.pg_settings
			WHERE name = 'max_connections'
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

	// The max_conns CTE reads the newest pg_settings row per connection
	// rather than requiring one written within the last hour, for the same
	// reason as pg_settings.max_connections above. Freshness of the metric
	// still comes from metrics.pg_stat_activity, which the 5 minute window
	// on active_counts bounds. See GitHub issue #406.
	"connection_utilization_percent": {
		probeName: "pg_stat_activity",
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
				SELECT DISTINCT ON (connection_id)
				       connection_id, setting::float as max_connections
				FROM metrics.pg_settings
				WHERE name = 'max_connections'
				ORDER BY connection_id, collected_at DESC
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
		probeName: "pg_stat_activity",
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

	// A primary with no connected standby has no pg_stat_replication row,
	// so a missing row means the standby has gone (decommissioned or
	// disconnected, which pg_stat_replication.standby_disconnected reports
	// separately) rather than that lag data has stopped arriving; the lag
	// alert clears. See GitHub issue #407.
	"pg_stat_replication.replay_lag_seconds": {
		probeName: "pg_stat_replication",
		latestSQL: `
			SELECT connection_id,
			       EXTRACT(EPOCH FROM (NOW() - replay_lsn_timestamp))::float as value,
			       collected_at
			FROM metrics.pg_stat_replication
			WHERE collected_at > NOW() - INTERVAL '5 minutes'
			  AND replay_lsn_timestamp IS NOT NULL
		`,
		historicalSQL:   "",
		scan:            scanBasic,
		historicalScan:  historicalScanBasic,
		clearWhenAbsent: true,
		absenceWindow:   5 * time.Minute,
	},

	"pg_stat_replication.lag_bytes": {
		probeName: "pg_stat_replication",
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
		// Same reasoning as replay_lag_seconds: no row means no standby,
		// so the lag alert clears. See GitHub issue #407.
		clearWhenAbsent: true,
		absenceWindow:   5 * time.Minute,
	},

	"pg_replication_slots.retained_bytes": {
		probeName: "pg_replication_slots",
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
		// Dropping every replication slot is the operator's usual remedy
		// for runaway WAL retention, and it leaves the probe with nothing
		// to store, so a missing row is the recovery signal. See GitHub
		// issue #407.
		clearWhenAbsent: true,
		absenceWindow:   15 * time.Minute,
	},

	// pg_replication_slots.inactive emits a single row per connection with
	// value=1 whenever the most recent sample for at least one replication
	// slot recorded in the last fifteen minutes has active=false. No row is
	// emitted for connections whose slots are all currently active, which
	// clears the matching `==` alert as soon as every slot recovers; that
	// is why the entry is clearWhenAbsent.
	//
	// The window is three times the probe's 300 second interval. It was
	// five minutes, exactly one interval, so a single late collection
	// emptied it, the cleaner read the empty result as recovery, and the
	// critical replication_slot_inactive alert cleared and re-fired on the
	// next sample. See GitHub issue #407.
	//
	// The latest-sample-per-slot reduction is essential: without it, a slot
	// that was inactive at t-4m but active at t-1m would still match the
	// active=false predicate against its older row and keep the alert
	// firing after recovery. DISTINCT ON (connection_id, slot_name) ordered
	// by collected_at DESC inside the freshness window selects exactly the
	// most recent sample for every slot, and a single connection with N
	// slots is reduced to one row regardless of how many of those slots
	// are inactive (the alert engine consumes one row per connection_id).
	//
	// The earlier implementation read from metrics.pg_stat_replication_slots
	// (a table that no migration creates) and inferred inactivity by an
	// application_name join against metrics.pg_stat_replication. That join
	// is unnecessary because metrics.pg_replication_slots.active mirrors
	// the source pg_replication_slots.active column directly, so the query
	// can decide inactivity from a single table.
	"pg_replication_slots.inactive": {
		probeName: "pg_replication_slots",
		latestSQL: `
			WITH latest_per_slot AS (
				SELECT DISTINCT ON (connection_id, slot_name)
				       connection_id, slot_name, active, collected_at
				FROM metrics.pg_replication_slots
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				ORDER BY connection_id, slot_name, collected_at DESC
			)
			SELECT connection_id,
			       1::float AS value,
			       MAX(collected_at) AS collected_at
			FROM latest_per_slot
			WHERE active = false
			GROUP BY connection_id
		`,
		historicalSQL:   "",
		scan:            scanBasic,
		historicalScan:  historicalScanBasic,
		clearWhenAbsent: true,
		absenceWindow:   15 * time.Minute,
	},

	// pg_replication_slots.inactive_count counts replication slots that
	// have active=false in the latest metrics.pg_replication_slots sample
	// for each connection. The COUNT(*) FILTER aggregate yields zero when
	// every slot is active (the alert clears) and the inactive count when
	// any slot has dropped its WAL receiver.
	//
	// The latest CTE only considers samples from the last fifteen minutes
	// (three 300 second probe intervals). Without the cutoff the newest
	// sample was whatever the table still held, so once the collector
	// stopped, or the connection stopped being monitored, the alert kept
	// firing on days-old data until retention purged the partition. A
	// connection whose slots have all been dropped stores no rows, so the
	// entry is clearWhenAbsent. See GitHub issue #407.
	"pg_replication_slots.inactive_count": {
		probeName: "pg_replication_slots",
		latestSQL: `
			WITH latest AS (
				SELECT connection_id, MAX(collected_at) AS collected_at
				  FROM metrics.pg_replication_slots
				 WHERE collected_at > NOW() - INTERVAL '15 minutes'
				 GROUP BY connection_id
			)
			SELECT s.connection_id,
			       COUNT(*) FILTER (WHERE NOT s.active)::float AS value,
			       l.collected_at
			  FROM metrics.pg_replication_slots s
			  JOIN latest l
			    ON s.connection_id = l.connection_id
			   AND s.collected_at  = l.collected_at
			 WHERE s.collected_at > NOW() - INTERVAL '15 minutes'
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
		scan:            scanBasic,
		historicalScan:  historicalScanBasic,
		clearWhenAbsent: true,
		absenceWindow:   15 * time.Minute,
	},

	// pg_replication_slots.max_retained_bytes returns the maximum
	// retained_bytes across all replication slots in the latest sample
	// for each connection. retained_bytes is NUMERIC in the source table
	// to handle WAL retention values larger than int64; the cast to float
	// is safe for the threshold ranges the operator-facing alert rules
	// configure (1 GiB warning, 10 GiB critical).
	//
	// The fifteen minute cutoff on the latest CTE and the clearWhenAbsent
	// flag are there for the reasons given on inactive_count above. See
	// GitHub issue #407.
	"pg_replication_slots.max_retained_bytes": {
		probeName: "pg_replication_slots",
		latestSQL: `
			WITH latest AS (
				SELECT connection_id, MAX(collected_at) AS collected_at
				  FROM metrics.pg_replication_slots
				 WHERE collected_at > NOW() - INTERVAL '15 minutes'
				 GROUP BY connection_id
			)
			SELECT s.connection_id,
			       COALESCE(MAX(s.retained_bytes), 0)::float AS value,
			       l.collected_at
			  FROM metrics.pg_replication_slots s
			  JOIN latest l
			    ON s.connection_id = l.connection_id
			   AND s.collected_at  = l.collected_at
			 WHERE s.collected_at > NOW() - INTERVAL '15 minutes'
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
		scan:            scanBasic,
		historicalScan:  historicalScanBasic,
		clearWhenAbsent: true,
		absenceWindow:   15 * time.Minute,
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
		probeName: "spock_exception_log",
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
		// The probe short-circuits Store on an empty result, so a connection
		// with no recent exceptions has no fresh sample and the query emits no
		// row: absence is how the alert clears. See GitHub issue #407.
		clearWhenAbsent: true,
		absenceWindow:   5 * time.Minute,
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
		probeName: "spock_resolutions",
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
		// As for spock_exception_log.recent_count: no fresh sample means no
		// recent resolutions, so absence clears the alert. See GitHub issue
		// #407.
		clearWhenAbsent: true,
		absenceWindow:   5 * time.Minute,
	},

	"pg_stat_replication.standby_disconnected": {
		probeName: "pg_stat_replication",
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
		// The query emits a row only while receiver_status is NULL, so a
		// reconnected standby produces no row and the alert must clear on
		// absence. See GitHub issue #407.
		clearWhenAbsent: true,
		absenceWindow:   5 * time.Minute,
	},

	"pg_node_role.subscription_worker_down": {
		probeName: "pg_node_role",
		latestSQL: `
			WITH recent_node_role AS (
				SELECT connection_id,
				       subscription_count,
				       active_subscription_count,
				       collected_at,
				       ROW_NUMBER() OVER (PARTITION BY connection_id ORDER BY collected_at DESC) as rn
				FROM metrics.pg_node_role
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
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
		// The query emits a row only while a subscription worker is down, so
		// absence is the recovery signal. The window is three times the
		// probe's 300 second interval; at the five minutes it used to be
		// it held a single sample, so one late collection emptied it and
		// the cleaner read that as recovery. See GitHub issue #407.
		clearWhenAbsent: true,
		absenceWindow:   15 * time.Minute,
	},

	"pg_stat_activity.blocked_count": {
		probeName: "pg_stat_activity",
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
		// The COUNT runs over lock waiters only, so a connection with no
		// blocked backends emits no row; absence means nothing is blocked. See
		// GitHub issue #407.
		clearWhenAbsent: true,
		absenceWindow:   5 * time.Minute,
	},

	"pg_stat_activity.idle_in_transaction_seconds": {
		probeName: "pg_stat_activity",
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
		// Rows exist only while some backend is idle in transaction, so a
		// connection with none emits no row and the alert clears on absence.
		// See GitHub issue #407.
		clearWhenAbsent: true,
		absenceWindow:   5 * time.Minute,
	},

	"pg_stat_activity.max_lock_wait_seconds": {
		probeName: "pg_stat_activity",
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
		// Rows exist only while some backend waits on a lock, so absence means
		// nothing is waiting and the alert clears. See GitHub issue #407.
		clearWhenAbsent: true,
		absenceWindow:   5 * time.Minute,
	},

	"pg_stat_activity.max_query_duration_seconds": {
		probeName: "pg_stat_activity",
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
		// Rows exist only while some client backend is running a query, so an
		// idle server emits no row; the long-running query has finished and
		// the alert clears on absence. See GitHub issue #407.
		clearWhenAbsent: true,
		absenceWindow:   5 * time.Minute,
	},

	"pg_stat_activity.max_xact_duration_seconds": {
		probeName: "pg_stat_activity",
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
		// Rows exist only while some client backend has an open transaction,
		// so absence means the long transaction has ended and the alert clears.
		// See GitHub issue #407.
		clearWhenAbsent: true,
		absenceWindow:   5 * time.Minute,
	},

	"pg_stat_all_tables.dead_tuple_percent": {
		probeName: "pg_stat_all_tables",
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

	// The archiver counters live on metrics.pg_stat_wal, which the
	// collector populates from pg_stat_archiver and pg_stat_wal together;
	// there is no metrics.pg_stat_archiver table, so this query used to
	// fail with SQLSTATE 42P01 on every evaluation.
	//
	// The value is the number of archive failures observed in the last
	// hour, which is six samples at the probe's 600 second interval. Only
	// positive per-sample deltas are summed so a stats reset (failed_count
	// dropping back to zero) does not report a spurious burst of failures,
	// and connections with a single sample in the window report 0 rather
	// than dropping out of the result set entirely. See GitHub issue #406.
	"pg_stat_archiver.failed_count_delta": {
		probeName: "pg_stat_wal",
		latestSQL: `
			WITH archiver_data AS (
				SELECT connection_id,
				       failed_count,
				       collected_at,
				       LAG(failed_count) OVER (PARTITION BY connection_id ORDER BY collected_at) as prev_failed_count
				FROM metrics.pg_stat_wal
				WHERE collected_at > NOW() - INTERVAL '1 hour'
			)
			SELECT connection_id,
			       COALESCE(
			           SUM(GREATEST(failed_count - prev_failed_count, 0))
			               FILTER (WHERE prev_failed_count IS NOT NULL),
			           0)::float as value,
			       MAX(collected_at) as collected_at
			FROM archiver_data
			GROUP BY connection_id
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	// The value is the number of requested (as opposed to timed)
	// checkpoints observed in the last hour. The previous query took the
	// largest increase between two consecutive samples inside a 15 minute
	// window, which at the probe's 600 second interval meant "requested
	// checkpoints in a single 10 minute sample" and additionally returned
	// no row at all whenever only one sample fell inside the window.
	//
	// Summing positive per-sample deltas over an hour gives a stable,
	// absolute count; negative deltas from a stats reset contribute zero,
	// and a connection with a single sample reports 0 instead of vanishing
	// from the result set. See GitHub issue #406.
	"pg_stat_checkpointer.checkpoints_req_delta": {
		probeName: "pg_stat_checkpointer",
		latestSQL: `
			WITH checkpointer_data AS (
				SELECT connection_id,
				       num_requested,
				       collected_at,
				       LAG(num_requested) OVER (PARTITION BY connection_id ORDER BY collected_at) as prev_num_requested
				FROM metrics.pg_stat_checkpointer
				WHERE collected_at > NOW() - INTERVAL '1 hour'
			)
			SELECT connection_id,
			       COALESCE(
			           SUM(GREATEST(num_requested - prev_num_requested, 0))
			               FILTER (WHERE prev_num_requested IS NOT NULL),
			           0)::float as value,
			       MAX(collected_at) as collected_at
			FROM checkpointer_data
			GROUP BY connection_id
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	// The latest query reduces to the newest qualifying delta (at least
	// 10000 blocks moved) per connection and database. It used to return
	// every delta row in the fifteen minute window, about three per
	// database at the 300 second probe interval, with no ORDER BY. The
	// evaluator fires when any row violates whilst the cleaner stops at
	// the first row for the alert, so the same data could fire and clear
	// the alert in one cycle (violation in the newest interval) or latch
	// it (violation in the oldest). DISTINCT ON ... ORDER BY collected_at
	// DESC makes both sides read one value, the most recent. A database
	// that moved fewer than 10000 blocks in every interval emits no row:
	// its ratio is unmeasurable rather than healthy, so the entry is not
	// clearWhenAbsent and an alert waits for the next busy interval. See
	// GitHub issue #407.
	"pg_stat_database.cache_hit_ratio": {
		probeName: "pg_stat_database",
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
			SELECT DISTINCT ON (connection_id, database_name)
			       connection_id,
			       database_name,
			       CASE
			           WHEN (delta_hit + delta_read) > 0
			           THEN (delta_hit::float / (delta_hit + delta_read)) * 100
			           ELSE 100
			       END as value,
			       collected_at
			FROM deltas
			ORDER BY connection_id, database_name, collected_at DESC
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

	// The value is the number of deadlocks detected in the last hour, per
	// connection and database. The previous query took the largest
	// increase between two consecutive samples inside a 15 minute window,
	// so the figure depended on the probe interval and a connection with a
	// single sample in the window returned no row at all.
	//
	// Summing positive per-sample deltas over an hour gives an absolute
	// count that does not change when the probe interval does; negative
	// deltas from a stats reset contribute zero, and a connection with a
	// single sample reports 0 instead of vanishing from the result set.
	// The historical query feeds baselines with the same per-hour figure,
	// one row per connection, database and hour bucket. Both queries
	// filter inside the aggregate rather than in a WHERE clause, so a
	// bucket whose only sample has no predecessor reports 0 instead of
	// disappearing, which matters for a newly onboarded connection. The
	// buckets are cut with date_trunc on the UTC value of collected_at
	// because the collector stores UTC and the alerter's session TimeZone
	// is whatever the server GUC says. Each row also carries the number of
	// raw samples it aggregates, so baseline warmup still counts samples
	// rather than buckets. See GitHub issue #409.
	"pg_stat_database.deadlocks_delta": {
		probeName: "pg_stat_database",
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
				WHERE collected_at > NOW() - INTERVAL '1 hour'
				  AND datname IS NOT NULL
				  AND datname NOT LIKE 'template%'
			)
			SELECT connection_id,
			       database_name,
			       COALESCE(
			           SUM(GREATEST(deadlocks - prev_deadlocks, 0))
			               FILTER (WHERE prev_deadlocks IS NOT NULL),
			           0)::float as value,
			       MAX(collected_at) as collected_at
			FROM db_deadlocks
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
			SELECT connection_id,
			       database_name,
			       COALESCE(
			           SUM(GREATEST(deadlocks - prev_deadlocks, 0))
			               FILTER (WHERE prev_deadlocks IS NOT NULL),
			           0)::float as value,
			       date_trunc('hour', collected_at AT TIME ZONE 'UTC')
			           AT TIME ZONE 'UTC' as collected_at,
			       COUNT(*) as sample_count
			FROM db_deadlocks
			GROUP BY connection_id, database_name,
			         date_trunc('hour', collected_at AT TIME ZONE 'UTC')
			ORDER BY connection_id, database_name, collected_at
		`,
		scan:           scanWithDB,
		historicalScan: historicalScanWithDBAndSamples,
	},

	// The value is the number of temporary files created in the last
	// hour, per connection and database. Same shape and reasoning as the
	// deadlocks entry above, including the hourly historical buckets, the
	// UTC truncation and the per-bucket sample count: the per-sample MAX()
	// over 15 minutes depended on the probe interval and dropped
	// single-sample connections. See GitHub issue #409.
	"pg_stat_database.temp_files_delta": {
		probeName: "pg_stat_database",
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
				WHERE collected_at > NOW() - INTERVAL '1 hour'
				  AND datname IS NOT NULL
				  AND datname NOT LIKE 'template%'
			)
			SELECT connection_id,
			       database_name,
			       COALESCE(
			           SUM(GREATEST(temp_files - prev_temp_files, 0))
			               FILTER (WHERE prev_temp_files IS NOT NULL),
			           0)::float as value,
			       MAX(collected_at) as collected_at
			FROM db_temp_files
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
			SELECT connection_id,
			       database_name,
			       COALESCE(
			           SUM(GREATEST(temp_files - prev_temp_files, 0))
			               FILTER (WHERE prev_temp_files IS NOT NULL),
			           0)::float as value,
			       date_trunc('hour', collected_at AT TIME ZONE 'UTC')
			           AT TIME ZONE 'UTC' as collected_at,
			       COUNT(*) as sample_count
			FROM db_temp_files
			GROUP BY connection_id, database_name,
			         date_trunc('hour', collected_at AT TIME ZONE 'UTC')
			ORDER BY connection_id, database_name, collected_at
		`,
		scan:           scanWithDB,
		historicalScan: historicalScanWithDBAndSamples,
	},

	// The value is the number of queryids whose mean execution time over
	// the most recent probe interval exceeded 1000 ms. The query used to
	// test pg_stat_statements.mean_exec_time, which is a lifetime average
	// since the last pg_stat_statements_reset(): a query that ran slowly
	// once kept the count elevated for ever, whether or not it ran again,
	// and the alert cleared only on a manual stats reset or eviction.
	//
	// The interval mean is delta(total_exec_time) / delta(calls) between
	// the newest sample in the fifteen minute window (three samples at the
	// 300 second probe interval) and the sample before it. The LEAD runs
	// within each statement identity (queryid plus userid, dbid and
	// toplevel, the table's key), because each identity is its own
	// counter that a reset can zero independently and differencing across
	// them subtracts one identity's counter from another's. The ROW_NUMBER
	// shares the identity window, so the whole window is sorted once:
	// ordering descending and reading the predecessor with LEAD rather
	// than ordering ascending and reading it with LAG costs one WindowAgg
	// and one sort instead of two, which measured at roughly a third off
	// the execution time over 24000 rows in the window. An identity
	// whose calls did not increase contributes nothing, which skips
	// statements that never ran in the interval and statements whose
	// counters were reset (calls fell), and a queryid with no executing
	// identity has a NULL mean that the FILTER does not count. COUNT(*)
	// FILTER over every queryid seen in the newest samples means a
	// database with statements but no slow ones reports 0 rather than
	// disappearing from the result set. See GitHub issue #407.
	"pg_stat_statements.slow_query_count": {
		probeName: "pg_stat_statements",
		latestSQL: `
			WITH ranked AS (
				SELECT connection_id,
				       database_name,
				       queryid,
				       calls,
				       total_exec_time,
				       collected_at,
				       ROW_NUMBER() OVER identity as rn,
				       LEAD(calls) OVER identity as prev_calls,
				       LEAD(total_exec_time) OVER identity as prev_total_exec_time
				FROM metrics.pg_stat_statements
				WHERE collected_at > NOW() - INTERVAL '15 minutes'
				WINDOW identity AS (
				    PARTITION BY connection_id, database_name, queryid,
				                 userid, dbid, toplevel
				    ORDER BY collected_at DESC
				)
			),
			newest AS (
				SELECT connection_id,
				       database_name,
				       queryid,
				       collected_at,
				       CASE WHEN calls > prev_calls
				            THEN calls - prev_calls END as delta_calls,
				       CASE WHEN calls > prev_calls
				            THEN total_exec_time - prev_total_exec_time END as delta_exec_time
				FROM ranked
				WHERE rn = 1
			),
			per_query AS (
				SELECT connection_id,
				       database_name,
				       queryid,
				       SUM(delta_exec_time) / NULLIF(SUM(delta_calls), 0) as interval_mean_ms,
				       MAX(collected_at) as collected_at
				FROM newest
				GROUP BY connection_id, database_name, queryid
			)
			SELECT connection_id,
			       database_name,
			       COUNT(*) FILTER (WHERE interval_mean_ms > 1000)::float as value,
			       MAX(collected_at) as collected_at
			FROM per_query
			GROUP BY connection_id, database_name
		`,
		historicalSQL:  "",
		scan:           scanWithDB,
		historicalScan: historicalScanBasic,
	},

	// The system_stats extension only populates processor_time_percent on
	// Windows; on Linux the column is NULL, so coalescing it to zero made
	// the cpu_usage_high rule read 0% on a fully saturated host and never
	// fire. The value is now the first of three expressions that yields a
	// number: the Windows processor time, the Linux busy percentage
	// derived from idle_mode_percent, or the sum of the individual Linux
	// busy buckets. The result is clamped to 0-100 so a partial row cannot
	// produce a nonsensical percentage. See GitHub issue #406.
	"pg_sys_cpu_usage_info.processor_time_percent": {
		probeName: "pg_sys_cpu_usage_info",
		latestSQL: `
			SELECT connection_id,
			       ` + cpuBusyPercentExpr + ` as value,
			       collected_at
			FROM (
			    SELECT DISTINCT ON (connection_id) *
			    FROM metrics.pg_sys_cpu_usage_info
			    WHERE collected_at > NOW() - INTERVAL '15 minutes'
			    ORDER BY connection_id, collected_at DESC
			) latest
		`,
		historicalSQL: `
			SELECT m.connection_id, NULL::text as database_name,
			       ` + cpuBusyPercentExpr + ` as value,
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
		probeName: "pg_sys_disk_info",
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
				  AND ` + pseudoFilesystemExclusion + `
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
				  AND ` + pseudoFilesystemExclusion + `
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
		probeName: "pg_sys_load_avg_info",
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
		probeName: "pg_sys_memory_info",
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

	// The value is the transaction ID age of the oldest database on the
	// connection, template databases included, expressed as a percentage
	// of the 2^31-1 wraparound limit.
	//
	// Templates must be counted. Wraparound is decided by the oldest
	// datfrozenxid in the cluster, and template0 has datallowconn = false
	// so autovacuum only reaches it on the anti-wraparound path; with
	// autovacuum disabled it is precisely the database that ages while
	// every user database stays fresh. Filtering templates out therefore
	// let the rule stay silent until the server began refusing writes.
	// The dashboard's XID Age tile counts templates for the same reason.
	//
	// The query previously joined metrics.pg_stat_all_tables against
	// pg_settings, discarded both, and returned the literal 50.0, so the
	// transaction_wraparound rule reported a constant regardless of the
	// real age. metrics.pg_stat_all_tables carries no frozen-xid column,
	// so the value is read from metrics.pg_database.age_datfrozenxid,
	// which the pg_database probe collects every 300 seconds. See GitHub
	// issue #406.
	"age_percent": {
		probeName: "pg_database",
		latestSQL: `
			WITH latest_ages AS (
				SELECT DISTINCT ON (connection_id, datname)
				       connection_id,
				       datname,
				       age_datfrozenxid,
				       collected_at
				FROM metrics.pg_database
				WHERE collected_at > NOW() - INTERVAL '1 hour'
				  AND age_datfrozenxid IS NOT NULL
				ORDER BY connection_id, datname, collected_at DESC
			)
			SELECT connection_id,
			       MAX(age_datfrozenxid::float / 2147483647.0 * 100)::float as value,
			       MAX(collected_at) as collected_at
			FROM latest_ages
			GROUP BY connection_id
		`,
		historicalSQL:  "",
		scan:           scanBasic,
		historicalScan: historicalScanBasic,
	},

	// The autovacuum settings are read from the newest pg_settings row per
	// connection and setting name rather than from a one-hour window. The
	// window meant the LEFT JOIN found nothing on any server whose
	// settings had been stable for an hour, so the COALESCE defaults of 50
	// and 0.2 applied and tuned autovacuum_vacuum_threshold and
	// autovacuum_vacuum_scale_factor values were ignored. See GitHub issue
	// #406.
	"table_last_autovacuum_hours": {
		probeName: "pg_stat_all_tables",
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
			latest_av_settings AS (
				SELECT DISTINCT ON (connection_id, name)
				       connection_id, name, setting
				FROM metrics.pg_settings
				WHERE name IN ('autovacuum_vacuum_threshold', 'autovacuum_vacuum_scale_factor')
				ORDER BY connection_id, name, collected_at DESC
			),
			av_settings AS (
				SELECT connection_id,
				       MAX(setting::float) FILTER (
				           WHERE name = 'autovacuum_vacuum_threshold') as av_threshold,
				       MAX(setting::float) FILTER (
				           WHERE name = 'autovacuum_vacuum_scale_factor') as av_scale_factor
				FROM latest_av_settings
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
		// The exceeding CTE keeps only tables whose dead tuples are over the
		// autovacuum threshold, so once autovacuum has caught up the database
		// emits no row; absence is the recovery signal. See GitHub issue #407.
		clearWhenAbsent: true,
		absenceWindow:   15 * time.Minute,
	},
}
