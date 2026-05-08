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

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestFloat32SliceToVectorString(t *testing.T) {
	tests := []struct {
		name string
		in   []float32
		want string
	}{
		{"empty", nil, "[]"},
		{"empty slice", []float32{}, "[]"},
		{"single", []float32{1.5}, "[1.5]"},
		{"multi", []float32{1.5, 2.0, 3.25}, "[1.5,2,3.25]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := float32SliceToVectorString(tt.in)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCreateAndGetAnomalyCandidate(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "ac-conn")

	c := &AnomalyCandidate{
		ConnectionID: connID,
		MetricName:   "anomaly_metric",
		MetricValue:  4.2,
		ZScore:       3.5,
		DetectedAt:   time.Now(),
		Context:      `{"foo":"bar"}`,
		Tier1Pass:    true,
	}
	if err := ds.CreateAnomalyCandidate(ctx, c); err != nil {
		t.Fatalf("CreateAnomalyCandidate: %v", err)
	}
	if c.ID == 0 {
		t.Fatal("expected ID to be set")
	}

	got, err := ds.GetAnomalyCandidateByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetAnomalyCandidateByID: %v", err)
	}
	if got.MetricName != "anomaly_metric" || got.ZScore != 3.5 {
		t.Errorf("got %+v", got)
	}

	// Missing.
	if _, err := ds.GetAnomalyCandidateByID(ctx, 99999); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestGetUnprocessedAndUpdateAnomalyCandidate(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "uac-conn")

	// Insert one unprocessed candidate (tier1_pass=TRUE) and one
	// already-processed candidate.
	c1 := &AnomalyCandidate{
		ConnectionID: connID, MetricName: "m1", MetricValue: 1, ZScore: 4,
		DetectedAt: time.Now(), Context: "{}", Tier1Pass: true,
	}
	if err := ds.CreateAnomalyCandidate(ctx, c1); err != nil {
		t.Fatal(err)
	}
	c2 := &AnomalyCandidate{
		ConnectionID: connID, MetricName: "m2", MetricValue: 2, ZScore: 5,
		DetectedAt: time.Now(), Context: "{}", Tier1Pass: true,
	}
	if err := ds.CreateAnomalyCandidate(ctx, c2); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	c2.ProcessedAt = &now
	dec := "alert"
	c2.FinalDecision = &dec
	if err := ds.UpdateAnomalyCandidate(ctx, c2); err != nil {
		t.Fatalf("UpdateAnomalyCandidate: %v", err)
	}

	results, err := ds.GetUnprocessedAnomalyCandidates(ctx, 10)
	if err != nil {
		t.Fatalf("GetUnprocessedAnomalyCandidates: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 unprocessed, got %d", len(results))
	}
	if results[0].ID != c1.ID {
		t.Errorf("expected c1.ID=%d, got %d", c1.ID, results[0].ID)
	}

	// Canceled context.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetUnprocessedAnomalyCandidates(canceled, 10); err == nil {
		t.Errorf("expected cancel error")
	}
}

func TestStoreAnomalyEmbeddingAndFindSimilar(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if !pgvectorAvailable(ctx, pool) {
		t.Skip("pgvector not available; skipping embedding tests")
	}
	if err := createAnomalyEmbeddingsTable(ctx, pool); err != nil {
		t.Skipf("anomaly_embeddings could not be created: %v", err)
	}

	connID := insertTestConnection(t, pool, "emb-conn")

	c := &AnomalyCandidate{
		ConnectionID: connID, MetricName: "m_emb", MetricValue: 1, ZScore: 4,
		DetectedAt: time.Now(), Context: "{}", Tier1Pass: true,
	}
	if err := ds.CreateAnomalyCandidate(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := ds.StoreAnomalyEmbedding(ctx, c.ID, []float32{1.0, 0.0, 0.0}, "test-model"); err != nil {
		t.Fatalf("StoreAnomalyEmbedding: %v", err)
	}

	// Mark candidate as processed so it's eligible for similarity search.
	now := time.Now()
	dec := "alert"
	c.ProcessedAt = &now
	c.FinalDecision = &dec
	if err := ds.UpdateAnomalyCandidate(ctx, c); err != nil {
		t.Fatal(err)
	}

	// Insert a second candidate to use as the "current" candidate excluded
	// from similarity results.
	c2 := &AnomalyCandidate{
		ConnectionID: connID, MetricName: "m_emb2", MetricValue: 2, ZScore: 5,
		DetectedAt: time.Now(), Context: "{}", Tier1Pass: true,
	}
	if err := ds.CreateAnomalyCandidate(ctx, c2); err != nil {
		t.Fatal(err)
	}
	results, err := ds.FindSimilarAnomalies(ctx, []float32{0.99, 0.01, 0.0}, c2.ID, 0.5, 10)
	if err != nil {
		t.Fatalf("FindSimilarAnomalies: %v", err)
	}
	if len(results) == 0 {
		t.Errorf("expected at least 1 similar result")
	}

	// Threshold filters out everything when set very high.
	highResults, err := ds.FindSimilarAnomalies(ctx, []float32{0.0, 1.0, 0.0}, c2.ID, 0.99, 10)
	if err != nil {
		t.Fatalf("FindSimilarAnomalies high threshold: %v", err)
	}
	_ = highResults

	// Update existing embedding (ON CONFLICT path).
	if err := ds.StoreAnomalyEmbedding(ctx, c.ID, []float32{0.0, 1.0, 0.0}, "test-model-v2"); err != nil {
		t.Errorf("update path: %v", err)
	}

	// Force the second-Exec error path: drop the embedding_id column
	// after the first Exec succeeds. The follow-up UPDATE on
	// anomaly_candidates references the missing column and must error.
	if _, err := pool.Exec(ctx, `ALTER TABLE anomaly_candidates DROP COLUMN embedding_id`); err != nil {
		t.Fatalf("drop embedding_id: %v", err)
	}
	c3 := &AnomalyCandidate{
		ConnectionID: connID, MetricName: "m_emb3", MetricValue: 1, ZScore: 4,
		DetectedAt: time.Now(), Context: "{}", Tier1Pass: true,
	}
	if err := ds.CreateAnomalyCandidate(ctx, c3); err != nil {
		t.Fatal(err)
	}
	if err := ds.StoreAnomalyEmbedding(ctx, c3.ID, []float32{1.0, 0.0, 0.0}, "model-bad"); err == nil {
		t.Errorf("expected StoreAnomalyEmbedding to fail when embedding_id column is missing")
	}
}

func TestGetMetricBaselinesAndUpsert(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "mb-conn")

	// Empty initially.
	baselines, err := ds.GetMetricBaselines(ctx, connID, "m_b")
	if err != nil {
		t.Fatalf("GetMetricBaselines: %v", err)
	}
	if len(baselines) != 0 {
		t.Errorf("expected 0 baselines, got %d", len(baselines))
	}

	// Upsert (insert path).
	b := &MetricBaseline{
		ConnectionID:   connID,
		MetricName:     "m_b",
		PeriodType:     "all",
		Mean:           5.0,
		StdDev:         1.0,
		Min:            0.0,
		Max:            10.0,
		SampleCount:    100,
		LastCalculated: time.Now(),
	}
	if err := ds.UpsertMetricBaseline(ctx, b); err != nil {
		t.Fatalf("UpsertMetricBaseline insert: %v", err)
	}

	// Upsert (update path) - same conflict key.
	b.Mean = 6.5
	if err := ds.UpsertMetricBaseline(ctx, b); err != nil {
		t.Fatalf("UpsertMetricBaseline update: %v", err)
	}

	baselines, err = ds.GetMetricBaselines(ctx, connID, "m_b")
	if err != nil {
		t.Fatalf("GetMetricBaselines after upsert: %v", err)
	}
	if len(baselines) != 1 {
		t.Fatalf("expected 1 baseline, got %d", len(baselines))
	}
	if baselines[0].Mean != 6.5 {
		t.Errorf("mean = %v, want 6.5 (update path)", baselines[0].Mean)
	}

	// Canceled context.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetMetricBaselines(canceled, connID, "m_b"); err == nil {
		t.Errorf("expected cancel error")
	}

	// Force a Scan failure: relax the NOT NULL constraint on mean,
	// insert a row with mean=NULL, and verify GetMetricBaselines
	// surfaces a scan error. The Mean field is a non-pointer float64
	// and pgx cannot scan NULL into it.
	if _, err := pool.Exec(ctx,
		`ALTER TABLE metric_baselines ALTER COLUMN mean DROP NOT NULL`); err != nil {
		t.Fatalf("alter table: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO metric_baselines (
			connection_id, metric_name, period_type, mean, stddev, min, max,
			sample_count, last_calculated
		) VALUES ($1, 'm_scan_fail', 'all', NULL, 1.0, 0.0, 1.0, 1, NOW())
	`, connID); err != nil {
		t.Fatalf("setup scan-fail row: %v", err)
	}
	if _, err := ds.GetMetricBaselines(ctx, connID, "m_scan_fail"); err == nil {
		t.Errorf("expected scan failure for NULL mean column")
	}
}

func TestGetAcknowledgedAnomalyAlerts(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "ack-conn")

	// Empty.
	alerts, err := ds.GetAcknowledgedAnomalyAlerts(ctx, 60, 10)
	if err != nil {
		t.Fatalf("GetAcknowledgedAnomalyAlerts: %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("expected 0, got %d", len(alerts))
	}

	// Insert acknowledged anomaly alert that's never been re-evaluated.
	var alertID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description,
		    status, metric_name)
		VALUES ('anomaly', $1, 'warning', 't', 'd', 'acknowledged', 'm_ack')
		RETURNING id
	`, connID).Scan(&alertID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO alert_acknowledgments (alert_id, acknowledged_by, acknowledge_type)
		VALUES ($1, 'tester', 'acknowledge')
	`, alertID); err != nil {
		t.Fatal(err)
	}
	alerts, err = ds.GetAcknowledgedAnomalyAlerts(ctx, 60, 10)
	if err != nil {
		t.Fatalf("GetAcknowledgedAnomalyAlerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Errorf("expected 1, got %d", len(alerts))
	}

	// A second alert with very recent re-evaluation must be excluded
	// when the interval is large enough that it's not yet due.
	var recentID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description,
		    status, metric_name, last_reevaluated_at, reevaluation_count)
		VALUES ('anomaly', $1, 'warning', 't', 'd', 'acknowledged', 'm_recent', NOW(), 1)
		RETURNING id
	`, connID).Scan(&recentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO alert_acknowledgments (alert_id, acknowledged_by, acknowledge_type)
		VALUES ($1, 'tester', 'acknowledge')
	`, recentID); err != nil {
		t.Fatal(err)
	}
	alerts, err = ds.GetAcknowledgedAnomalyAlerts(ctx, 86400, 10)
	if err != nil {
		t.Fatalf("GetAcknowledgedAnomalyAlerts (long interval): %v", err)
	}
	if len(alerts) != 1 {
		t.Errorf("expected 1 (only old enough), got %d", len(alerts))
	}

	// Canceled context.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetAcknowledgedAnomalyAlerts(canceled, 60, 10); err == nil {
		t.Errorf("expected cancel error")
	}
}

func TestGetAcknowledgmentHistoryForMetric(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "ah-conn")

	// Insert two acknowledged anomalies for the same metric.
	for i := 0; i < 2; i++ {
		var alertID int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO alerts (alert_type, connection_id, severity, title, description,
			    status, metric_name)
			VALUES ('anomaly', $1, 'warning', 't', 'd', 'acknowledged', 'm_hist')
			RETURNING id
		`, connID).Scan(&alertID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO alert_acknowledgments (alert_id, acknowledged_by, acknowledge_type)
			VALUES ($1, 'tester', 'acknowledge')
		`, alertID); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ds.GetAcknowledgmentHistoryForMetric(ctx, "m_hist", connID, 0, 10)
	if err != nil {
		t.Fatalf("GetAcknowledgmentHistoryForMetric: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 history rows, got %d", len(got))
	}

	// Pass an exclude id of one of them; result should drop to 1.
	got, err = ds.GetAcknowledgmentHistoryForMetric(ctx, "m_hist", connID, got[0].ID, 10)
	if err != nil {
		t.Fatalf("GetAcknowledgmentHistoryForMetric (with exclude): %v", err)
	}
	if len(got) != 1 {
		t.Errorf("after exclude: expected 1, got %d", len(got))
	}

	// Canceled context.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ds.GetAcknowledgmentHistoryForMetric(canceled, "x", connID, 0, 10); err == nil {
		t.Errorf("expected cancel error")
	}
}
