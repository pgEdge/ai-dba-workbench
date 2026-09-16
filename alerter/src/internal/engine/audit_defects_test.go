/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/pkg/worker"
)

// This file holds verification tests for an audit of the alerter
// subsystem. Each test documents an audit claim and pins the behavior
// the engine actually exhibits today. Where the claim describes a
// defect, the test asserts the CURRENT (defective) behavior and the
// comment states what the behavior SHOULD be, so the suite stays green
// until the defect is fixed.
//
// Claim C1 (the metric_staleness fire/clear loop and its missing
// cooldown guard) was fixed in #405, and its pinned tests were removed
// with that fix as the header above prescribes. The behavior is now
// covered against the corrected code in
// staleness_alerts_integration_test.go and
// staleness_alerts_errors_integration_test.go. Claim C8 (cache_hit_ratio
// flapping) was fixed in #407; its test below now asserts the corrected
// behavior under its original name, and the cleaner's treatment of
// missing data is covered in missing_data_integration_test.go.
//
// Claims C6, C7 and C10 (baseline selection, metrics without a
// historical query, and database scoping in detection) were fixed in
// #408. Their tests below now assert the corrected behavior, and the
// ALERTER_DEFECT_DEMO gate that used to hide the failing-by-design
// "Demo" variants is gone from this package with them.

// Seed and read-back statements used by the audit tests. They are
// named constants so the Codacy/Semgrep go_sql_rule-concat-sqli rule
// does not flag inline multi-line SQL passed to Exec/QueryRow; every
// value is still bound via $N.
const (
	insertArchiverRuleSQL = `
        INSERT INTO alert_rules
            (name, description, category, metric_name, default_operator,
             default_threshold, default_severity, default_enabled, is_built_in)
        VALUES ('wal_archive_failed', 'WAL archiving failures detected',
                'wal', 'pg_stat_archiver.failed_count_delta', '>', 0,
                'critical', TRUE, TRUE)
        RETURNING id
    `

	insertCacheHitRuleSQL = `
        INSERT INTO alert_rules
            (name, description, category, metric_name, default_operator,
             default_threshold, default_severity, default_enabled, is_built_in)
        VALUES ('cache_hit_ratio_low', 'Cache hit ratio below threshold',
                'performance', 'pg_stat_database.cache_hit_ratio', '<', 80,
                'warning', TRUE, TRUE)
        RETURNING id
    `

	selectAlertStatusSQL = `
        SELECT status FROM alerts WHERE id = $1
    `

	// createStatDatabaseTableSQL carries every metrics.pg_stat_database
	// column the audit tests read, so a single definition serves both
	// the cache_hit_ratio tests (blks_hit, blks_read) and the
	// deadlocks_delta tests (deadlocks). Keeping one fixture avoids the
	// two definitions drifting apart, which would make a test fail with
	// an undefined-column error that has nothing to do with the defect
	// under examination.
	createStatDatabaseTableSQL = `
        CREATE TABLE metrics.pg_stat_database (
            connection_id INTEGER NOT NULL,
            database_name VARCHAR(255) NOT NULL,
            datname TEXT,
            blks_hit BIGINT,
            blks_read BIGINT,
            deadlocks BIGINT,
            collected_at TIMESTAMPTZ NOT NULL
        )
    `
)

// notificationCapture records the notification jobs the engine submits
// to its worker pool. The pool handler forwards each job onto a
// buffered channel so tests can await an exact number of jobs without
// polling.
type notificationCapture struct {
	jobs chan notificationJob
}

// installNotificationCapture replaces the engine's notification worker
// pool with one whose handler records every submitted job. The pool is
// stopped via t.Cleanup so no goroutine outlives the test.
//
// The engine's own pool is only created when a notification manager is
// configured; the integration environments in this package build the
// engine without one, so this helper is the only way to observe
// queueNotification.
func installNotificationCapture(t *testing.T, e *Engine) *notificationCapture {
	t.Helper()

	capture := &notificationCapture{jobs: make(chan notificationJob, 256)}
	pool := worker.NewWorkerPool(1, 256, func(job notificationJob) {
		capture.jobs <- job
	})
	pool.Start()

	previous := e.notificationPool
	e.notificationPool = pool
	t.Cleanup(func() {
		e.notificationPool = previous
		pool.Stop()
	})
	return capture
}

// await reads exactly n jobs from the capture channel, failing the
// test if they do not arrive within the timeout. It then waits a short
// grace period and fails if any extra job arrives, so the caller's
// count assertion is exact rather than a lower bound.
func (c *notificationCapture) await(t *testing.T, n int) []notificationJob {
	t.Helper()

	jobs := make([]notificationJob, 0, n)
	deadline := time.After(10 * time.Second)
	for len(jobs) < n {
		select {
		case job := <-c.jobs:
			jobs = append(jobs, job)
		case <-deadline:
			t.Fatalf("timed out waiting for %d notifications; got %d",
				n, len(jobs))
		}
	}

	select {
	case extra := <-c.jobs:
		t.Fatalf("received an unexpected extra notification: type=%s alert=%q",
			extra.notifTyp, extra.alert.Title)
	case <-time.After(200 * time.Millisecond):
	}
	return jobs
}

// countTypes summarizes the captured jobs by notification type.
func countTypes(jobs []notificationJob) map[database.NotificationType]int {
	counts := make(map[database.NotificationType]int)
	for _, job := range jobs {
		counts[job.notifTyp]++
	}
	return counts
}

// TestAuditC2ArchiverRuleErrorIsSwallowed verifies the engine half of
// audit claim C2: when the metric query fails because
// metrics.pg_stat_archiver does not exist,
// evaluateRuleForAllConnections logs at debug level and returns. With
// debug disabled - the production default - nothing reaches the log
// and the wal_archive_failed rule is silently inert.
//
// The evaluator SHOULD distinguish "no data" from a query error and
// surface the latter.
func TestAuditC2ArchiverRuleErrorIsSwallowed(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	installNotificationCapture(t, engine)

	var ruleID int64
	if err := pool.QueryRow(ctx, insertArchiverRuleSQL).Scan(&ruleID); err != nil {
		t.Fatalf("failed to insert wal_archive_failed rule: %v", err)
	}
	insertTestConnection(t, pool, "audit-c2-engine")

	output := captureStderr(t, func() {
		engine.evaluateThresholds(ctx)
	})

	if strings.Contains(output, "pg_stat_archiver") {
		t.Errorf("expected the archiver query failure to be swallowed, "+
			"but stderr mentioned it: %s", output)
	}
	if strings.Contains(output, "ERROR") {
		t.Errorf("expected no ERROR log for the failing rule, got: %s", output)
	}

	var alertCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE rule_id = $1`,
		ruleID).Scan(&alertCount); err != nil {
		t.Fatalf("failed to count alerts: %v", err)
	}
	if alertCount != 0 {
		t.Errorf("alerts for wal_archive_failed = %d, want 0", alertCount)
	}
}

// insertStatDatabaseSampleSQL seeds one metrics.pg_stat_database row at
// NOW() minus the given interval.
const insertStatDatabaseSampleSQL = `
        INSERT INTO metrics.pg_stat_database
            (connection_id, database_name, datname, blks_hit, blks_read,
             collected_at)
        VALUES ($1, $2::text, $2::text, $3, $4, NOW() - $5::interval)
    `

// TestAuditC8CacheHitRatioFiresAndClearsOnSameData covers the engine half
// of audit claim C8, fixed in #407. cache_hit_ratio used to return one
// row per delta interval with no ordering, so the evaluator (any row
// violating) and the cleaner (first matching row) disagreed on identical
// data and the alert flapped once per cycle: two cycles produced two fire
// and two clear notifications.
//
// The metric now reduces to the newest interval, so a stable cold cache
// produces one alert that stays active across cycles and exactly one fire
// notification. The alert then clears once a newer, healthy interval
// arrives.
func TestAuditC8CacheHitRatioFiresAndClearsOnSameData(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installNotificationCapture(t, engine)

	if _, err := pool.Exec(ctx, createStatDatabaseTableSQL); err != nil {
		t.Fatalf("failed to create metrics.pg_stat_database: %v", err)
	}

	var ruleID int64
	if err := pool.QueryRow(ctx, insertCacheHitRuleSQL).Scan(&ruleID); err != nil {
		t.Fatalf("failed to insert cache_hit_ratio_low rule: %v", err)
	}
	connID := insertTestConnection(t, pool, "audit-c8-engine")

	// Two healthy intervals followed by a cold one. Before the fix the
	// healthy rows sorted first, so the cleaner saw 100% while the
	// evaluator saw the 0% row.
	samples := []struct {
		offset string
		hits   int64
		reads  int64
	}{
		{"12 minutes", 0, 0},
		{"8 minutes", 30_000, 0},
		{"4 minutes", 60_000, 0},
		{"1 minute", 60_000, 30_000},
	}
	for _, s := range samples {
		if _, err := pool.Exec(ctx, insertStatDatabaseSampleSQL,
			connID, "appdb", s.hits, s.reads, s.offset); err != nil {
			t.Fatalf("failed to seed pg_stat_database: %v", err)
		}
	}

	values, err := ds.GetLatestMetricValues(ctx,
		"pg_stat_database.cache_hit_ratio")
	if err != nil {
		t.Fatalf("GetLatestMetricValues failed: %v", err)
	}
	if len(values) != 1 || values[0].Value != 0 {
		t.Fatalf("expected one reduced row carrying the cold newest "+
			"interval (0%%), got %+v", values)
	}

	// Cycle the evaluator and the cleaner over unchanged data: the
	// alert must be raised once and stay active.
	const cycles = 2
	var alertID int64
	for i := 0; i < cycles; i++ {
		engine.evaluateThresholds(ctx)

		alert, err := ds.GetActiveThresholdAlert(ctx, ruleID, connID, strPtr("appdb"))
		if err != nil {
			t.Fatalf("GetActiveThresholdAlert failed: %v", err)
		}
		if alert == nil {
			t.Fatalf("cycle %d: expected an active alert on the cold interval", i)
		}
		if i == 0 {
			alertID = alert.ID
		} else if alert.ID != alertID {
			t.Fatalf("cycle %d: alert id changed from %d to %d; the alert was re-raised",
				i, alertID, alert.ID)
		}

		engine.cleanResolvedAlerts(ctx)

		var status string
		if err := pool.QueryRow(ctx, selectAlertStatusSQL, alertID).Scan(&status); err != nil {
			t.Fatalf("failed to read alert status: %v", err)
		}
		if status != "active" {
			t.Fatalf("cycle %d: alert status = %q, want \"active\" on unchanged cold data",
				i, status)
		}
	}

	// A newer, healthy interval (30000 hits, no reads) resolves it.
	if _, err := pool.Exec(ctx, insertStatDatabaseSampleSQL,
		connID, "appdb", int64(90_000), int64(30_000), "10 seconds"); err != nil {
		t.Fatalf("failed to seed the recovery sample: %v", err)
	}
	engine.cleanResolvedAlerts(ctx)

	var status string
	if err := pool.QueryRow(ctx, selectAlertStatusSQL, alertID).Scan(&status); err != nil {
		t.Fatalf("failed to read alert status: %v", err)
	}
	if status != "cleared" {
		t.Fatalf("alert status after a healthy interval = %q, want \"cleared\"", status)
	}

	jobs := capture.await(t, 2)
	counts := countTypes(jobs)
	if counts[database.NotificationTypeAlertFire] != 1 ||
		counts[database.NotificationTypeAlertClear] != 1 {
		t.Errorf("notifications = %v, want exactly 1 fire and 1 clear", counts)
	}
}

// Additional seed statements for the anomaly-side audit tests.
const (
	insertAnomalyRuleForMetricSQL = `
        INSERT INTO alert_rules
            (name, description, category, metric_name, default_operator,
             default_threshold, default_severity, default_enabled, is_built_in)
        VALUES ($1, 'Audit rule', 'audit', $2, '>', 0, 'warning', TRUE, FALSE)
    `

	createSlotsTableSQL = `
        CREATE TABLE metrics.pg_replication_slots (
            connection_id INTEGER NOT NULL,
            slot_name TEXT NOT NULL,
            active BOOLEAN,
            retained_bytes NUMERIC,
            collected_at TIMESTAMPTZ NOT NULL
        )
    `

	selectCandidateDatabaseSQL = `
        SELECT database_name, metric_value, context
        FROM anomaly_candidates
        WHERE connection_id = $1 AND metric_name = $2
        ORDER BY id
    `
)

// seedAuditBaseline upserts a baseline row through the datastore.
func seedAuditBaseline(t *testing.T, ds *database.Datastore, b *database.MetricBaseline) {
	t.Helper()
	if err := ds.UpsertMetricBaseline(context.Background(), b); err != nil {
		t.Fatalf("UpsertMetricBaseline(%s) failed: %v", b.PeriodType, err)
	}
}

// candidateSummary is one anomaly_candidates row as the audit tests
// read it back.
type candidateSummary struct {
	dbName     *string
	value      float64
	periodType string
}

// readCandidates returns the candidates written for a connection and
// metric, in insertion order, with the baseline period_type parsed out
// of the JSON context so tests can assert which baseline was scored.
func readCandidates(t *testing.T, pool *pgxpool.Pool, connID int, metric string) []candidateSummary {
	t.Helper()
	rows, err := pool.Query(context.Background(), selectCandidateDatabaseSQL,
		connID, metric)
	if err != nil {
		t.Fatalf("failed to read candidates: %v", err)
	}
	defer rows.Close()

	var out []candidateSummary
	for rows.Next() {
		var c candidateSummary
		var rawContext []byte
		if err := rows.Scan(&c.dbName, &c.value, &rawContext); err != nil {
			t.Fatalf("failed to scan candidate: %v", err)
		}
		var parsed struct {
			PeriodType string `json:"period_type"`
		}
		if err := json.Unmarshal(rawContext, &parsed); err != nil {
			t.Fatalf("failed to parse candidate context %q: %v", rawContext, err)
		}
		c.periodType = parsed.PeriodType
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}
	return out
}

// TestAuditC6DetectAnomaliesPrefersTimeAwareBaselines covers the engine
// half of audit claim C6, fixed in #408. detectAnomalies used to read
// baselines[0], which the alphabetical ORDER BY made the 'all' row, so
// the hourly and daily baselines were written and never consulted. It
// now scores against the hourly row for the current UTC hour, then the
// daily row for the current UTC weekday, then 'all', taking the first
// that is warm; a cold candidate is skipped rather than blocking a
// warmer, less specific one.
func TestAuditC6DetectAnomaliesPrefersTimeAwareBaselines(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	const metric = "pg_settings.max_connections"

	if _, err := pool.Exec(ctx, insertAnomalyAlertRuleSQL,
		"audit_c6_rule", metric); err != nil {
		t.Fatalf("failed to insert alert rule: %v", err)
	}

	// The current value is 500. Baselines with mean 100 and stddev 1
	// flag it instantly; mean 100 and stddev 1000 do not. Warm rows
	// are aged well past every default min_span_hours; cold rows have
	// too few samples.
	hour, weekday := baselinePeriodKeys(now)
	otherHour := (hour + 12) % 24
	warm := now.Add(-20 * 24 * time.Hour)
	cold := now.Add(-1 * time.Hour)

	type row struct {
		periodType string
		hour       *int
		weekday    *int
		stddev     float64
		samples    int64
		earliest   time.Time
	}
	cases := []struct {
		name       string
		rows       []row
		wantPeriod string // "" means no candidate expected
	}{
		{
			name: "warm hourly beats warm wide all",
			rows: []row{
				{"all", nil, nil, 1000, 500, warm},
				{"hourly", &hour, nil, 1, 500, warm},
			},
			wantPeriod: "hourly",
		},
		{
			name: "cold all does not block warm hourly",
			rows: []row{
				{"all", nil, nil, 1, 5, cold},
				{"hourly", &hour, nil, 1, 500, warm},
			},
			wantPeriod: "hourly",
		},
		{
			name: "cold hourly falls back to warm all",
			rows: []row{
				{"all", nil, nil, 1, 500, warm},
				{"hourly", &hour, nil, 1, 2, cold},
			},
			wantPeriod: "all",
		},
		{
			name: "hourly row for another hour is ignored",
			rows: []row{
				{"all", nil, nil, 1000, 500, warm},
				{"hourly", &otherHour, nil, 1, 500, warm},
			},
			wantPeriod: "",
		},
		{
			name: "warm daily beats warm wide all when no hourly row matches",
			rows: []row{
				{"all", nil, nil, 1000, 500, warm},
				{"daily", nil, &weekday, 1, 500, warm},
			},
			wantPeriod: "daily",
		},
		{
			name: "only a cold all row suppresses detection",
			rows: []row{
				{"all", nil, nil, 1, 5, cold},
			},
			wantPeriod: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx,
				`TRUNCATE anomaly_candidates RESTART IDENTITY`); err != nil {
				t.Fatalf("truncate anomaly_candidates failed: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM metric_baselines`); err != nil {
				t.Fatalf("delete metric_baselines failed: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM metrics.pg_settings`); err != nil {
				t.Fatalf("delete metrics.pg_settings failed: %v", err)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM connections`); err != nil {
				t.Fatalf("delete connections failed: %v", err)
			}

			var connID int
			if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
				"audit-c6").Scan(&connID); err != nil {
				t.Fatalf("failed to insert connection: %v", err)
			}
			if _, err := pool.Exec(ctx, insertAnomalyPgSettingsSQL,
				connID, "500"); err != nil {
				t.Fatalf("failed to insert pg_settings sample: %v", err)
			}

			for _, r := range tc.rows {
				seedAuditBaseline(t, ds, &database.MetricBaseline{
					ConnectionID:     connID,
					MetricName:       metric,
					PeriodType:       r.periodType,
					HourOfDay:        r.hour,
					DayOfWeek:        r.weekday,
					Mean:             100,
					StdDev:           r.stddev,
					Min:              0,
					Max:              200,
					SampleCount:      r.samples,
					LastCalculated:   now,
					EarliestSampleAt: r.earliest,
				})
			}

			engine.detectAnomalies(ctx)

			candidates := readCandidates(t, pool, connID, metric)
			if tc.wantPeriod == "" {
				if len(candidates) != 0 {
					t.Fatalf("expected no candidates, got %d (period %q)",
						len(candidates), candidates[0].periodType)
				}
				return
			}
			if len(candidates) != 1 {
				t.Fatalf("expected one candidate, got %d", len(candidates))
			}
			if candidates[0].periodType != tc.wantPeriod {
				t.Errorf("candidate scored against %q baseline, want %q",
					candidates[0].periodType, tc.wantPeriod)
			}
			if candidates[0].dbName != nil {
				t.Errorf("candidate database_name = %q, want NULL for a "+
					"connection-wide metric", *candidates[0].dbName)
			}
		})
	}
}

// TestAuditC7UnsupportedMetricsAreExcluded covers audit claim C7, fixed
// in #408. A metric whose registry entry has no historicalSQL used to be
// routed through a fallback that built a baseline from the single
// latest sample; that row had sample_count 1, stddev 0 and a NULL
// earliest_sample_at, so isBaselineWarm rejected it forever while it
// was rewritten every cycle. Such metrics are now skipped by both
// calculateBaselines and detectAnomalies, and any rows the old fallback
// left behind are deleted on the next baseline run.
func TestAuditC7UnsupportedMetricsAreExcluded(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	const unsupported = "pg_replication_slots.inactive"
	const supported = "pg_settings.max_connections"

	if _, err := pool.Exec(ctx, createSlotsTableSQL); err != nil {
		t.Fatalf("failed to create metrics.pg_replication_slots: %v", err)
	}
	for _, r := range []struct{ name, metric string }{
		{"audit_c7_rule", unsupported},
		// metric_staleness is not in the registry at all and is
		// evaluated by its own code path; it must be skipped too.
		{"audit_c7_staleness", "metric_staleness"},
	} {
		if _, err := pool.Exec(ctx, insertAnomalyRuleForMetricSQL,
			r.name, r.metric); err != nil {
			t.Fatalf("failed to insert alert rule %s: %v", r.name, err)
		}
	}
	if _, err := pool.Exec(ctx, insertAnomalyAlertRuleSQL,
		"audit_c7_supported", supported); err != nil {
		t.Fatalf("failed to insert supported alert rule: %v", err)
	}

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
		"audit-c7").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}
	if _, err := pool.Exec(ctx, `
        INSERT INTO metrics.pg_replication_slots
            (connection_id, slot_name, active, retained_bytes, collected_at)
        VALUES ($1, 'slot_a', FALSE, 0, NOW())
    `, connID); err != nil {
		t.Fatalf("failed to seed replication slot: %v", err)
	}
	if _, err := pool.Exec(ctx, insertAnomalyPgSettingsSQL,
		connID, "500"); err != nil {
		t.Fatalf("failed to insert pg_settings sample: %v", err)
	}

	if database.SupportsBaselines(unsupported) {
		t.Fatalf("%s unexpectedly supports baselines", unsupported)
	}

	// Leftovers from the old fallback: a cold row for the unsupported
	// metric and one for the unregistered metric_staleness. The
	// supported metric's row is written by calculateBaselines itself
	// from the pg_settings sample and must be the only one to survive.
	for _, metric := range []string{unsupported, "metric_staleness"} {
		seedAuditBaseline(t, ds, &database.MetricBaseline{
			ConnectionID:   connID,
			MetricName:     metric,
			PeriodType:     "all",
			Mean:           1,
			SampleCount:    1,
			LastCalculated: now,
		})
	}

	engine.calculateBaselines(ctx)

	var remaining []string
	rows, err := pool.Query(ctx, `
        SELECT DISTINCT metric_name FROM metric_baselines
        WHERE connection_id = $1 ORDER BY metric_name
    `, connID)
	if err != nil {
		t.Fatalf("failed to read baselines: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		remaining = append(remaining, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}
	if len(remaining) != 1 || remaining[0] != supported {
		t.Fatalf("baseline metrics after calculateBaselines = %v, want [%s]",
			remaining, supported)
	}

	// End to end: replace the single-sample row calculateBaselines just
	// wrote with a warm one so the supported metric is scored (500
	// against mean 100, stddev 1), and confirm the unsupported ones
	// emit nothing.
	seedAuditBaseline(t, ds, &database.MetricBaseline{
		ConnectionID:     connID,
		MetricName:       supported,
		PeriodType:       "all",
		Mean:             100,
		StdDev:           1,
		Min:              99,
		Max:              101,
		SampleCount:      500,
		LastCalculated:   now,
		EarliestSampleAt: now.Add(-10 * 24 * time.Hour),
	})
	engine.detectAnomalies(ctx)
	if got := readCandidates(t, pool, connID, unsupported); len(got) != 0 {
		t.Errorf("candidates for %s = %d, want 0", unsupported, len(got))
	}
	if got := readCandidates(t, pool, connID, "metric_staleness"); len(got) != 0 {
		t.Errorf("candidates for metric_staleness = %d, want 0", len(got))
	}
	if got := readCandidates(t, pool, connID, supported); len(got) != 1 {
		t.Errorf("candidates for %s = %d, want 1", supported, len(got))
	}
}

// TestAuditC10AnomalyCandidateCarriesDatabaseName covers audit claim
// C10, fixed in #408. For per-database metrics the baseline calculator
// writes one baseline row per (connection, database); detectAnomalies
// now scores every latest value for the connection against the
// baseline for that value's database and records the database on the
// candidate, so deduplication in createAnomalyAlert is per database
// too.
func TestAuditC10AnomalyCandidateCarriesDatabaseName(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	const metric = "pg_stat_database.deadlocks_delta"

	if _, err := pool.Exec(ctx, createStatDatabaseTableSQL); err != nil {
		t.Fatalf("failed to create metrics.pg_stat_database: %v", err)
	}
	if _, err := pool.Exec(ctx, insertAnomalyRuleForMetricSQL,
		"audit_c10_rule", metric); err != nil {
		t.Fatalf("failed to insert alert rule: %v", err)
	}

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
		"audit-c10").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	// Two databases on one connection: alpha's deadlocks jump by 900,
	// which is normal for alpha, and beta's by 101, which is wildly
	// abnormal for beta.
	samples := []struct {
		dbName    string
		deadlocks []int64
	}{
		{"alpha", []int64{100, 1000}},
		{"beta", []int64{5, 106}},
	}
	offsets := []string{"10 minutes", "1 minute"}
	for _, s := range samples {
		for i, offset := range offsets {
			if _, err := pool.Exec(ctx, `
                INSERT INTO metrics.pg_stat_database
                    (connection_id, database_name, datname, deadlocks,
                     collected_at)
                VALUES ($1, $2, $3, $4, NOW() - $5::interval)
            `, connID, s.dbName, s.dbName, s.deadlocks[i], offset); err != nil {
				t.Fatalf("failed to seed pg_stat_database: %v", err)
			}
		}
	}

	values, err := ds.GetLatestMetricValues(ctx, metric)
	if err != nil {
		t.Fatalf("GetLatestMetricValues failed: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("expected one row per database, got %d", len(values))
	}

	// The baseline calculator keys its output on the database: running
	// it over this data writes one 'all' baseline per database.
	engine.calculateBaselines(ctx)
	var perDatabaseBaselines int
	if err := pool.QueryRow(ctx, `
        SELECT COUNT(DISTINCT database_name)
        FROM metric_baselines
        WHERE connection_id = $1 AND metric_name = $2
          AND database_name IS NOT NULL
    `, connID, metric).Scan(&perDatabaseBaselines); err != nil {
		t.Fatalf("failed to count per-database baselines: %v", err)
	}
	if perDatabaseBaselines != 2 {
		t.Errorf("distinct baseline database names = %d, want 2",
			perDatabaseBaselines)
	}

	// Replace them with warm baselines that make alpha's 900 ordinary
	// and beta's 101 anomalous.
	if _, err := pool.Exec(ctx, `DELETE FROM metric_baselines`); err != nil {
		t.Fatalf("failed to reset baselines: %v", err)
	}
	for _, b := range []struct {
		dbName string
		mean   float64
	}{{"alpha", 900}, {"beta", 1}} {
		dbName := b.dbName
		seedAuditBaseline(t, ds, &database.MetricBaseline{
			ConnectionID:     connID,
			DatabaseName:     &dbName,
			MetricName:       metric,
			PeriodType:       "all",
			Mean:             b.mean,
			StdDev:           0.5,
			Min:              b.mean - 1,
			Max:              b.mean + 1,
			SampleCount:      500,
			LastCalculated:   now,
			EarliestSampleAt: now.Add(-10 * 24 * time.Hour),
		})
	}

	engine.detectAnomalies(ctx)

	candidates := readCandidates(t, pool, connID, metric)
	if len(candidates) != 1 {
		t.Fatalf("expected exactly one candidate, got %d", len(candidates))
	}
	if candidates[0].dbName == nil || *candidates[0].dbName != "beta" {
		t.Errorf("candidate database_name = %v, want \"beta\"", candidates[0].dbName)
	}
	if candidates[0].value != float32ish(101) {
		t.Errorf("candidate metric_value = %v, want 101 (beta's delta)",
			candidates[0].value)
	}
}

// float32ish rounds a float64 through float32, matching the REAL
// column type used by anomaly_candidates.metric_value.
func float32ish(v float64) float64 {
	return float64(float32(v))
}
