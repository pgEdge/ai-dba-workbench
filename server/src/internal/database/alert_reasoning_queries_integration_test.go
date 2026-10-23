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
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// reasoningCandidatesSchema adds the anomaly_candidates columns that
// GetAlertTier3Info reads to the unacknowledge test schema, mirroring
// the production table in collector/src/database/schema.go.
const reasoningCandidatesSchema = `
DROP TABLE IF EXISTS anomaly_candidates CASCADE;
CREATE TABLE anomaly_candidates (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT REFERENCES alerts(id) ON DELETE SET NULL,
    metric_value REAL NOT NULL DEFAULT 0,
    z_score REAL NOT NULL DEFAULT 0,
    tier3_pass BOOLEAN,
    tier3_result TEXT,
    tier3_error TEXT,
    tier3_source TEXT,
    tier3_model TEXT,
    final_decision TEXT
);
`

func newReasoningTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool) {
	t.Helper()
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	if _, err := pool.Exec(context.Background(), reasoningCandidatesSchema); err != nil {
		cleanup()
		t.Fatalf("create anomaly_candidates: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS anomaly_candidates CASCADE"); err != nil {
			t.Logf("drop anomaly_candidates: %v", err)
		}
		cleanup()
	})
	return ds, pool
}

func insertReasoningTestAlert(t *testing.T, pool *pgxpool.Pool, alertType string, details *string) int64 {
	t.Helper()
	connID := insertUnackTestConnection(t, pool, "reasoning-test")
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO alerts (alert_type, connection_id, database_name, metric_name,
		                    metric_value, severity, title, description, status,
		                    anomaly_score, anomaly_details)
		VALUES ($1, $2, 'appdb', 'pg_stat_database.xact_rollback', 412, 'warning',
		        'Anomaly', 'Test alert', 'active', 5.2, $3::jsonb)
		RETURNING id`, alertType, connID, details).Scan(&id)
	if err != nil {
		t.Fatalf("insert alert: %v", err)
	}
	return id
}

// TestAlertTier3Info_RaisingCandidateAndConditionalSave links three
// candidates to one alert, as the alerter does: the one that raised it,
// a later duplicate it skipped because the alert was open (every Tier 3
// column NULL, final_decision 'alert'), and a still later one that
// passed Tier 3 before finding the alert open. The raising candidate,
// the oldest with a Tier 3 outcome, must be the one returned.
func TestAlertTier3Info_RaisingCandidateAndConditionalSave(t *testing.T) {
	ds, pool := newReasoningTestDatastore(t)
	ctx := context.Background()

	details := `{"z_score": 5.2, "tier3_result": "Decision model m: alert"}`
	alertID := insertReasoningTestAlert(t, pool, "anomaly", &details)
	_, err := pool.Exec(ctx, `
		INSERT INTO anomaly_candidates (alert_id, metric_value, z_score, tier3_pass,
		                                tier3_result, tier3_source, tier3_model, final_decision)
		VALUES ($1, 300, 4.5, true, 'Decision model m: alert', 'decision', 'm', 'alert'),
		       ($1, 350, 4.8, NULL, NULL, NULL, NULL, 'alert'),
		       ($1, 380, 5.0, true, 'later', 'reasoning', 'r', 'alert')`, alertID)
	if err != nil {
		t.Fatalf("insert candidates: %v", err)
	}

	info, err := ds.GetAlertTier3Info(ctx, alertID)
	if err != nil {
		t.Fatalf("GetAlertTier3Info: %v", err)
	}
	if info.AlertType != "anomaly" || info.MetricName == nil || *info.MetricName != "pg_stat_database.xact_rollback" {
		t.Errorf("alert fields wrong: %+v", info)
	}
	if info.Tier3Source == nil || *info.Tier3Source != "decision" ||
		info.Tier3Pass == nil || !*info.Tier3Pass || *info.FinalDecision != "alert" {
		t.Errorf("expected the raising candidate, got source=%v pass=%v", info.Tier3Source, info.Tier3Pass)
	}
	// The alert row holds refreshed values (412, 5.2); the prompt must
	// get the values Tier 3 judged.
	if info.CandidateMetricValue == nil || *info.CandidateMetricValue != 300 ||
		info.CandidateZScore == nil || *info.CandidateZScore != 4.5 {
		t.Errorf("candidate values = %v, %v, want 300, 4.5", info.CandidateMetricValue, info.CandidateZScore)
	}
	if info.StoredReasoning != nil {
		t.Errorf("no reasoning should be stored yet, got %q", *info.StoredReasoning)
	}

	got, err := ds.SaveAlertTier3Reasoning(ctx, alertID, "First explanation.")
	if err != nil || got != "First explanation." {
		t.Fatalf("first save = %q, %v", got, err)
	}
	got, err = ds.SaveAlertTier3Reasoning(ctx, alertID, "Second explanation.")
	if err != nil || got != "First explanation." {
		t.Fatalf("second save should return the first text, got %q, %v", got, err)
	}

	info, err = ds.GetAlertTier3Info(ctx, alertID)
	if err != nil {
		t.Fatalf("GetAlertTier3Info after save: %v", err)
	}
	if info.StoredReasoning == nil || *info.StoredReasoning != "First explanation." {
		t.Errorf("stored reasoning = %v", info.StoredReasoning)
	}
	// The alerter's fields must survive the merge.
	var zScore float64
	if err := pool.QueryRow(ctx,
		"SELECT (anomaly_details ->> 'z_score')::float8 FROM alerts WHERE id = $1",
		alertID).Scan(&zScore); err != nil || zScore != 5.2 {
		t.Errorf("z_score after save = %v, %v", zScore, err)
	}
}

// TestAlertTier3Info_DuplicateOnlyCandidates verifies that an alert
// whose newest linked candidate is a skipped duplicate still reports
// the older raising candidate, and that with no Tier 3 outcome at all
// the oldest candidate is returned.
func TestAlertTier3Info_DuplicateOnlyCandidates(t *testing.T) {
	ds, pool := newReasoningTestDatastore(t)
	ctx := context.Background()

	raised := insertReasoningTestAlert(t, pool, "anomaly", nil)
	noTier3 := insertReasoningTestAlert(t, pool, "anomaly", nil)
	_, err := pool.Exec(ctx, `
		INSERT INTO anomaly_candidates (alert_id, metric_value, z_score, tier3_pass,
		                                tier3_source, final_decision)
		VALUES ($1, 10, 3.1, true, 'decision', 'alert'),
		       ($1, 11, 3.2, NULL, NULL, 'alert'),
		       ($2, 20, 4.1, NULL, NULL, 'alert'),
		       ($2, 21, 4.2, NULL, NULL, 'alert')`, raised, noTier3)
	if err != nil {
		t.Fatalf("insert candidates: %v", err)
	}

	info, err := ds.GetAlertTier3Info(ctx, raised)
	if err != nil {
		t.Fatalf("GetAlertTier3Info: %v", err)
	}
	if info.Tier3Source == nil || *info.Tier3Source != "decision" || info.Tier3Pass == nil || !*info.Tier3Pass {
		t.Errorf("a skipped duplicate hid the raising candidate: source=%v pass=%v", info.Tier3Source, info.Tier3Pass)
	}

	info, err = ds.GetAlertTier3Info(ctx, noTier3)
	if err != nil {
		t.Fatalf("GetAlertTier3Info: %v", err)
	}
	if info.Tier3Pass != nil || info.CandidateMetricValue == nil || *info.CandidateMetricValue != 20 {
		t.Errorf("expected the oldest candidate, got pass=%v value=%v", info.Tier3Pass, info.CandidateMetricValue)
	}
}

func TestAlertTier3Info_NoCandidateAndNullDetails(t *testing.T) {
	ds, pool := newReasoningTestDatastore(t)
	ctx := context.Background()
	alertID := insertReasoningTestAlert(t, pool, "threshold", nil)

	info, err := ds.GetAlertTier3Info(ctx, alertID)
	if err != nil {
		t.Fatalf("GetAlertTier3Info: %v", err)
	}
	if info.Tier3Source != nil || info.AnomalyDetails != nil || info.StoredReasoning != nil {
		t.Errorf("expected no candidate or details, got %+v", info)
	}
	got, err := ds.SaveAlertTier3Reasoning(ctx, alertID, "Text.")
	if err != nil || got != "Text." {
		t.Errorf("save onto NULL details = %q, %v", got, err)
	}
}

func TestAlertTier3Info_ConcurrentSavesKeepOneText(t *testing.T) {
	ds, pool := newReasoningTestDatastore(t)
	ctx := context.Background()
	details := `{}`
	alertID := insertReasoningTestAlert(t, pool, "anomaly", &details)

	texts := []string{"A.", "B.", "C.", "D."}
	results := make([]string, len(texts))
	var wg sync.WaitGroup
	for i, text := range texts {
		wg.Add(1)
		go func(i int, text string) {
			defer wg.Done()
			got, err := ds.SaveAlertTier3Reasoning(ctx, alertID, text)
			if err != nil {
				t.Errorf("save %q: %v", text, err)
			}
			results[i] = got
		}(i, text)
	}
	wg.Wait()
	for _, r := range results {
		if r != results[0] {
			t.Fatalf("saves disagree on the stored text: %v", results)
		}
	}
}

func TestAlertTier3Info_MissingAlertAndErrors(t *testing.T) {
	ds, pool := newReasoningTestDatastore(t)
	ctx := context.Background()

	if _, err := ds.GetAlertTier3Info(ctx, 999999); !errors.Is(err, ErrAlertNotFound) {
		t.Errorf("GetAlertTier3Info missing alert: %v", err)
	}
	if _, err := ds.SaveAlertTier3Reasoning(ctx, 999999, "x"); !errors.Is(err, ErrAlertNotFound) {
		t.Errorf("SaveAlertTier3Reasoning missing alert: %v", err)
	}

	nullDetails := `{"tier3_reasoning": null}`
	alertID := insertReasoningTestAlert(t, pool, "anomaly", &nullDetails)
	if _, err := ds.SaveAlertTier3Reasoning(ctx, alertID, "x"); err == nil {
		t.Error("a JSON null reasoning should be reported as an error")
	}

	// With the candidates table gone, both queries fail outright.
	if _, err := pool.Exec(ctx, "DROP TABLE anomaly_candidates"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := ds.GetAlertTier3Info(ctx, alertID); err == nil || errors.Is(err, ErrAlertNotFound) {
		t.Errorf("expected a query error, got %v", err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE alerts ALTER COLUMN anomaly_details TYPE text"); err != nil {
		t.Fatalf("alter: %v", err)
	}
	if _, err := ds.SaveAlertTier3Reasoning(ctx, alertID, "x"); err == nil || errors.Is(err, ErrAlertNotFound) {
		t.Errorf("expected an update error, got %v", err)
	}
}
