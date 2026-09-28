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
	"errors"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// providerHealthSchemaSQL gives the fixture's alerts table the shape the
// collector's v18 migration produces, so that it can hold system alerts.
const providerHealthSchemaSQL = `
ALTER TABLE alerts DROP CONSTRAINT alerts_alert_type_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_alert_type_check
    CHECK (alert_type IN ('threshold', 'anomaly', 'connection', 'system'));
ALTER TABLE alerts ALTER COLUMN connection_id DROP NOT NULL;
ALTER TABLE alerts ADD CONSTRAINT alerts_system_connection_check
    CHECK ((alert_type = 'system') = (connection_id IS NULL));
CREATE UNIQUE INDEX idx_alerts_system_open ON alerts (metric_name)
    WHERE alert_type = 'system' AND status <> 'cleared';
`

// providerHealthRowSQL reads the provider health alert for a key.
const providerHealthRowSQL = `
SELECT status, connection_id IS NULL, count(*) OVER ()
FROM alerts WHERE alert_type = 'system' AND metric_name = $1
ORDER BY id DESC LIMIT 1`

// TestProviderHealth_EndToEnd drives the tracker against the real
// datastore: consecutive failures raise one system alert with no
// connection, the resolved-alert sweep leaves it alone, and the next
// success clears it with a clear notification.
func TestProviderHealth_EndToEnd(t *testing.T) {
	engine, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, providerHealthSchemaSQL); err != nil {
		t.Fatalf("apply system alert schema: %v", err)
	}
	capture := installStalenessNotificationCapture(t, engine)

	tracker := newProviderHealthTracker(ds, engine.queueNotification,
		func() int { return 2 }, func() []string { return nil }, engine.log)
	emb := &healthTrackingEmbedding{inner: &fakeEmbedder{err: errors.New("model retired")},
		provider: "openai", tracker: tracker}
	key := providerHealthKey(providerTierEmbedding, "openai")

	for range 4 {
		if _, err := emb.GenerateEmbedding(ctx, "x"); err == nil {
			t.Fatal("wrapped error swallowed")
		}
	}

	var status string
	var noConnection bool
	var rows int
	if err := pool.QueryRow(ctx, providerHealthRowSQL, key).Scan(&status, &noConnection, &rows); err != nil {
		t.Fatalf("read provider health alert: %v", err)
	}
	if status != "active" || !noConnection || rows != 1 {
		t.Fatalf("status %q, no connection %v, rows %d", status, noConnection, rows)
	}

	engine.cleanResolvedAlerts(ctx)
	if err := pool.QueryRow(ctx, providerHealthRowSQL, key).Scan(&status, &noConnection, &rows); err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Errorf("resolved-alert sweep changed the system alert to %q", status)
	}

	emb.inner = &fakeEmbedder{}
	if _, err := emb.GenerateEmbedding(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, providerHealthRowSQL, key).Scan(&status, &noConnection, &rows); err != nil {
		t.Fatal(err)
	}
	if status != "cleared" {
		t.Errorf("status after success = %q, want cleared", status)
	}

	counts := capture.drain(t)
	if counts[database.NotificationTypeAlertFire] != 1 || counts[database.NotificationTypeAlertClear] != 1 {
		t.Errorf("notifications = %v, want one fire and one clear", counts)
	}
}
