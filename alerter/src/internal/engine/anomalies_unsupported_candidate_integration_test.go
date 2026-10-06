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

// TestProcessTier2And3SuppressesUnsupportedMetricCandidates covers a
// candidate left over from before its metric was excluded from baselines
// (GitHub issue #576). It must be marked processed and suppressed
// without reaching Tier 2, whilst a candidate for a baselined metric in
// the same batch still goes through the tiers to an alert decision.
func TestProcessTier2And3SuppressesUnsupportedMetricCandidates(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, anomalyEmbeddingsIntegrationSchema); err != nil {
		t.Skipf("pgvector is not available: %v", err)
	}

	cfg := engine.getConfig()
	cfg.Anomaly.Tier2.Enabled = true
	provider := &countingEmbeddingProvider{}
	engine.embeddingProvider = provider

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
		"unsupported-candidate").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	newCandidate := func(metric string) *database.AnomalyCandidate {
		c := &database.AnomalyCandidate{
			ConnectionID: connID,
			MetricName:   metric,
			MetricValue:  999,
			ZScore:       10,
			Context:      "{}",
			Tier1Pass:    true,
		}
		if err := ds.CreateAnomalyCandidate(ctx, c); err != nil {
			t.Fatalf("CreateAnomalyCandidate(%s): %v", metric, err)
		}
		return c
	}
	stale := []*database.AnomalyCandidate{
		newCandidate("pg_settings.max_connections"),
		newCandidate("pg_replication_slots.inactive_count"),
	}
	supported := newCandidate("pg_sys_load_avg_info.load_avg_fifteen_minutes")

	// The alert lookups for the supported candidate log errors because
	// this schema has no alerts table; the decision is stored regardless.
	_ = captureStderr(t, func() {
		engine.processTier2And3(ctx)
	})

	if got := provider.calls.Load(); got != 1 {
		t.Errorf("embedding calls = %d, want 1 (the supported candidate only)", got)
	}

	for _, c := range stale {
		stored, err := ds.GetAnomalyCandidateByID(ctx, c.ID)
		if err != nil {
			t.Fatalf("GetAnomalyCandidateByID(%d): %v", c.ID, err)
		}
		if stored.ProcessedAt == nil {
			t.Errorf("%s candidate was not marked processed", c.MetricName)
		}
		if stored.FinalDecision == nil || *stored.FinalDecision != "suppress" {
			t.Errorf("%s final decision = %s, want suppress", c.MetricName, formatOptionalString(stored.FinalDecision))
		}
		if stored.Tier2Pass != nil {
			t.Errorf("%s reached Tier 2 (Tier2Pass = %v)", c.MetricName, *stored.Tier2Pass)
		}
	}

	stored, err := ds.GetAnomalyCandidateByID(ctx, supported.ID)
	if err != nil {
		t.Fatalf("GetAnomalyCandidateByID(%d): %v", supported.ID, err)
	}
	if stored.ProcessedAt == nil {
		t.Error("supported candidate was not marked processed")
	}
	if stored.FinalDecision == nil || *stored.FinalDecision != "alert" {
		t.Errorf("supported final decision = %s, want alert", formatOptionalString(stored.FinalDecision))
	}
}

// TestSuppressUnsupportedCandidateLogsUpdateFailure checks that a failed
// update of a suppressed candidate is logged at default verbosity, and
// that the candidate is still suppressed in memory.
func TestSuppressUnsupportedCandidateLogsUpdateFailure(t *testing.T) {
	engine, _, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	closed, err := pgxpool.New(context.Background(), pool.Config().ConnString())
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	closed.Close()
	engine.datastore = database.NewTestDatastore(closed)

	candidate := &database.AnomalyCandidate{
		ID:         1,
		MetricName: "pg_settings.max_connections",
	}
	output := captureStderr(t, func() {
		engine.suppressUnsupportedCandidate(context.Background(), candidate)
	})

	if !strings.Contains(output, "ERROR: Failed to update anomaly candidate") {
		t.Errorf("log output missing the update failure:\n%s", output)
	}
	if candidate.FinalDecision == nil || *candidate.FinalDecision != "suppress" {
		t.Errorf("final decision = %s, want suppress", formatOptionalString(candidate.FinalDecision))
	}
	if candidate.ProcessedAt == nil {
		t.Error("expected ProcessedAt to be set")
	}
}
