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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// Seed statements for the server-wide metric tests: the built-in
// slow_query_count rule and an active threshold alert raised by it,
// optionally naming a database. They are named constants for the reason
// given beside the unmonitored connection test statements.
const (
	serverWideRuleInsertSQL = `
        INSERT INTO alert_rules (name, description, category, metric_name,
            default_operator, default_threshold, default_severity,
            default_enabled, required_extension, is_built_in)
        VALUES ('slow_query_count', 'High number of slow queries', 'queries',
                $1, '>', 10, 'warning', TRUE, 'pg_stat_statements', TRUE)
        RETURNING id
    `

	serverWideAlertInsertSQL = `
        INSERT INTO alerts
            (alert_type, rule_id, connection_id, database_name, metric_name,
             metric_value, threshold_value, operator, severity, title,
             description, status, triggered_at)
        VALUES ('threshold', $1, $2, $3, $4, 42, 10, '>', 'warning',
                'High number of slow queries', $5, 'active', NOW())
        RETURNING id
    `
)

// slowQueryMetric is the metric GitHub issue #640 made server-wide.
const slowQueryMetric = "pg_stat_statements.slow_query_count"

// insertServerWideTestAlert inserts one active slow_query_count threshold
// alert, seeding its rule on first use, and returns the alert's id.
func insertServerWideTestAlert(t *testing.T, pool *pgxpool.Pool, connID int,
	db *string, description string) int64 {
	t.Helper()

	ctx := context.Background()
	var ruleID int64
	err := pool.QueryRow(ctx, `SELECT id FROM alert_rules WHERE name = 'slow_query_count'`).
		Scan(&ruleID)
	if err != nil {
		if err := pool.QueryRow(ctx, serverWideRuleInsertSQL, slowQueryMetric).
			Scan(&ruleID); err != nil {
			t.Fatalf("failed to seed rule: %v", err)
		}
	}

	var id int64
	if err := pool.QueryRow(ctx, serverWideAlertInsertSQL,
		ruleID, connID, db, slowQueryMetric, description).Scan(&id); err != nil {
		t.Fatalf("failed to insert alert: %v", err)
	}
	return id
}

// TestThresholdAlertNamingDatabaseOnServerWideMetricRetired is the
// cleaner's half of GitHub issue #640. Alerts raised whilst
// slow_query_count was reported per database name a database that no
// server-wide value can match, so the cleaner closes them, saying why and
// without a clear notification, whilst a server-wide alert on the same
// metric is left to the ordinary resolution path.
func TestThresholdAlertNamingDatabaseOnServerWideMetricRetired(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	capture := installStalenessNotificationCapture(t, engine)
	connID := insertTestConnection(t, pool, "server-wide-640")
	db := "appdb"

	retired := insertServerWideTestAlert(t, pool, connID, &db, "12 slow queries")
	already := databaseScopedClearDescriptionPrefix + "earlier"
	retiredBefore := insertServerWideTestAlert(t, pool, connID, &db, already)
	serverWide := insertServerWideTestAlert(t, pool, connID, nil, "12 slow queries")

	engine.cleanResolvedAlerts(ctx)

	got := readUnmonitoredAlert(t, pool, retired)
	if got.status != "cleared" || got.clearedAt == nil {
		t.Errorf("alert naming a database: status %q, cleared_at %v; want cleared",
			got.status, got.clearedAt)
	}
	if !strings.HasPrefix(got.description, databaseScopedClearDescriptionPrefix) ||
		!strings.Contains(got.description, slowQueryMetric) ||
		!strings.HasSuffix(got.description, "The alert said: 12 slow queries") {
		t.Errorf("description = %q, want the reason followed by the original", got.description)
	}

	before := readUnmonitoredAlert(t, pool, retiredBefore)
	if before.status != "cleared" {
		t.Errorf("previously explained alert status = %q, want cleared", before.status)
	}
	if before.description != already {
		t.Errorf("description rewritten twice: %q", before.description)
	}

	if kept := readUnmonitoredAlert(t, pool, serverWide); kept.status != "active" ||
		kept.description != "12 slow queries" {
		t.Errorf("server-wide alert = %+v, want it active and unchanged", kept)
	}

	if counts := capture.drain(t); counts[database.NotificationTypeAlertClear] != 0 {
		t.Errorf("clear notifications = %d, want 0; nothing was seen to recover",
			counts[database.NotificationTypeAlertClear])
	}
}

// TestRetireDatabaseScopedAlertFailures covers the write failures: a
// failed rewrite and a failed clear leave the alert active and unchanged
// for the next pass.
func TestRetireDatabaseScopedAlertFailures(t *testing.T) {
	engine, _, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "server-wide-640-fail")
	db := "appdb"
	id := insertServerWideTestAlert(t, pool, connID, &db, "seeded")

	if _, err := pool.Exec(ctx, failRecoveryUpdatesSQL); err != nil {
		t.Fatalf("failed to install trigger: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), dropFailRecoveryUpdatesSQL); err != nil {
			t.Errorf("failed to drop trigger: %v", err)
		}
	}()

	metric := slowQueryMetric
	alert := &database.Alert{ID: id, MetricName: &metric, DatabaseName: &db,
		Description: "seeded"}
	engine.retireDatabaseScopedAlert(ctx, alert)

	got := readUnmonitoredAlert(t, pool, id)
	if got.status != "active" || got.description != "seeded" {
		t.Errorf("alert after failed writes = %+v, want it active and unchanged", got)
	}
	if alert.Description != "seeded" {
		t.Errorf("in-memory description = %q, want it unchanged after a failed rewrite",
			alert.Description)
	}
}
