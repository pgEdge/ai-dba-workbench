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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
	embeddingpkg "github.com/pgedge/ai-workbench/pkg/embedding"
)

// wideEmbeddingProvider stands in for a model whose vectors are wider
// than the halfvec(4000) column, the case the removed model allow-list
// used to catch at configuration time.
type wideEmbeddingProvider struct {
	width int
}

func (p *wideEmbeddingProvider) GenerateEmbedding(context.Context, string) ([]float32, error) {
	vec := make([]float32, p.width)
	vec[0] = 1
	return vec, nil
}

func (p *wideEmbeddingProvider) ModelName() string { return "too-wide-model" }

// TestTier2WideEmbeddingLogsErrorAtDefaultVerbosity guards the only
// remaining check on embedding width. The engine runs with debug off,
// as it does in production, and the similarity search and the store
// must both report the width error through the ordinary log rather
// than the debug log; the candidate still passes through to Tier 3
// and is marked processed.
func TestTier2WideEmbeddingLogsErrorAtDefaultVerbosity(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	if engine.debug {
		t.Fatal("test requires the engine to run at default verbosity")
	}

	cfg := engine.getConfig()
	cfg.Anomaly.Tier2.Enabled = true
	engine.embeddingProvider = &wideEmbeddingProvider{width: embeddingpkg.MaxDimensions + 1}

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
		"tier2-wide").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}
	candidate := &database.AnomalyCandidate{
		ConnectionID: connID,
		MetricName:   "pg_settings.max_connections",
		MetricValue:  999,
		ZScore:       10,
		Context:      "{}",
		Tier1Pass:    true,
	}
	if err := ds.CreateAnomalyCandidate(ctx, candidate); err != nil {
		t.Fatalf("CreateAnomalyCandidate: %v", err)
	}

	output := captureStderr(t, func() {
		engine.processTier2And3(ctx)
	})

	for _, want := range []string{
		"ERROR: Failed to find similar anomalies for candidate",
		"ERROR: Failed to store embedding for candidate",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("log output missing %q:\n%s", want, output)
		}
	}
	if got := strings.Count(output, "embedding has 4001 dimensions, exceeds maximum supported 4000"); got != 2 {
		t.Errorf("expected the width error twice (search and store), got %d:\n%s", got, output)
	}

	stored, err := ds.GetAnomalyCandidateByID(ctx, candidate.ID)
	if err != nil {
		t.Fatalf("GetAnomalyCandidateByID: %v", err)
	}
	if stored.Tier2Pass == nil || !*stored.Tier2Pass {
		t.Errorf("expected Tier2Pass = true so the candidate reaches Tier 3, got %v", stored.Tier2Pass)
	}
	if stored.ProcessedAt == nil {
		t.Error("expected the candidate to be marked processed")
	}
}

// fixedEmbeddingProvider returns the same vector for every input so the
// similarity search sees exact matches against seeded history.
type fixedEmbeddingProvider struct {
	vec []float32
}

func (p *fixedEmbeddingProvider) GenerateEmbedding(context.Context, string) ([]float32, error) {
	out := make([]float32, len(p.vec))
	copy(out, p.vec)
	return out, nil
}

func (p *fixedEmbeddingProvider) ModelName() string { return "fixed-model" }

// failingEmbeddingProvider models an embedding API that is down.
type failingEmbeddingProvider struct{}

func (failingEmbeddingProvider) GenerateEmbedding(context.Context, string) ([]float32, error) {
	return nil, errors.New("embedding API unavailable")
}

func (failingEmbeddingProvider) ModelName() string { return "failing-model" }

const anomalyEmbeddingsIntegrationSchema = `
CREATE EXTENSION IF NOT EXISTS vector;
DROP TABLE IF EXISTS anomaly_embeddings CASCADE;
CREATE TABLE anomaly_embeddings (
    id BIGSERIAL PRIMARY KEY,
    candidate_id BIGINT REFERENCES anomaly_candidates(id) ON DELETE CASCADE,
    embedding halfvec(4000),
    model_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(candidate_id)
);
`

// TestTier2SimilarityDecisions drives processTier2 against seeded
// history so the suppress, pass and needs-review branches all run
// with a model of ordinary width. The test skips when pgvector cannot
// be installed in the test database.
func TestTier2SimilarityDecisions(t *testing.T) {
	engine, ds, pool, cleanup := newDetectAnomaliesEnv(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, anomalyEmbeddingsIntegrationSchema); err != nil {
		t.Skipf("anomaly_embeddings could not be created (pgvector missing?): %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS anomaly_embeddings CASCADE`); err != nil {
			t.Logf("anomaly_embeddings teardown failed: %v", err)
		}
	}()

	cfg := engine.getConfig()
	cfg.Anomaly.Tier2.Enabled = true
	cfg.Anomaly.Tier2.SimilarityThreshold = 0.3
	cfg.Anomaly.Tier2.SuppressionThreshold = 0.85
	vec := []float32{1, 0, 0}
	engine.embeddingProvider = &fixedEmbeddingProvider{vec: vec}

	var connID int
	if err := pool.QueryRow(ctx, insertAnomalyConnectionSQL,
		"tier2-similarity").Scan(&connID); err != nil {
		t.Fatalf("failed to insert connection: %v", err)
	}

	newCandidate := func(t *testing.T) *database.AnomalyCandidate {
		t.Helper()
		c := &database.AnomalyCandidate{
			ConnectionID: connID,
			MetricName:   "pg_settings.max_connections",
			MetricValue:  999,
			ZScore:       10,
			Context:      "{}",
			Tier1Pass:    true,
		}
		if err := ds.CreateAnomalyCandidate(ctx, c); err != nil {
			t.Fatalf("CreateAnomalyCandidate: %v", err)
		}
		return c
	}

	// seedHistory stores a processed candidate with the given final
	// decision and an embedding identical to the stub's output.
	seedHistory := func(t *testing.T, decision string) {
		t.Helper()
		c := newCandidate(t)
		now := c.DetectedAt
		c.FinalDecision = &decision
		c.ProcessedAt = &now
		if err := ds.UpdateAnomalyCandidate(ctx, c); err != nil {
			t.Fatalf("UpdateAnomalyCandidate: %v", err)
		}
		if err := ds.StoreAnomalyEmbedding(ctx, c.ID, vec, "fixed-model"); err != nil {
			t.Fatalf("StoreAnomalyEmbedding: %v", err)
		}
	}

	tests := []struct {
		name     string
		history  []string
		wantPass bool
		// minScore is the lowest acceptable Tier2Score: zero when
		// there is no history, and near one for identical vectors.
		minScore float64
	}{
		{"no history passes to tier 3", nil, true, 0},
		{"similar suppressed history suppresses", []string{"suppress"}, false, 0.85},
		{"similar alerted history passes", []string{"alert"}, true, 0.85},
		{"mixed history passes for review", []string{"suppress", "alert"}, true, 0.85},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `DELETE FROM anomaly_candidates`); err != nil {
				t.Fatalf("failed to reset candidates: %v", err)
			}
			for _, decision := range tt.history {
				seedHistory(t, decision)
			}
			c := newCandidate(t)

			embedding, similar := engine.processTier2(ctx, c)

			if len(embedding) != len(vec) {
				t.Errorf("embedding width = %d, want %d", len(embedding), len(vec))
			}
			if len(similar) != len(tt.history) {
				t.Errorf("similar anomalies = %d, want %d", len(similar), len(tt.history))
			}
			if c.Tier2Pass == nil || *c.Tier2Pass != tt.wantPass {
				t.Errorf("Tier2Pass = %v, want %v", c.Tier2Pass, tt.wantPass)
			}
			if c.Tier2Score == nil {
				t.Fatal("Tier2Score should always be set after Tier 2")
			}
			if *c.Tier2Score < tt.minScore {
				t.Errorf("Tier2Score = %v, want >= %v", *c.Tier2Score, tt.minScore)
			}
			if tt.minScore == 0 && *c.Tier2Score != 0 {
				t.Errorf("Tier2Score = %v, want 0 with no history", *c.Tier2Score)
			}
		})
	}

	t.Run("embedding failure passes to tier 3 and logs", func(t *testing.T) {
		engine.embeddingProvider = failingEmbeddingProvider{}
		c := newCandidate(t)

		var embedding []float32
		var similar []*database.SimilarAnomaly
		output := captureStderr(t, func() {
			embedding, similar = engine.processTier2(ctx, c)
		})

		if embedding != nil || similar != nil {
			t.Errorf("expected nil results on embedding failure, got %v, %v", embedding, similar)
		}
		if c.Tier2Pass == nil || !*c.Tier2Pass {
			t.Errorf("Tier2Pass = %v, want true", c.Tier2Pass)
		}
		if !strings.Contains(output, "ERROR: Failed to generate embedding for candidate") {
			t.Errorf("expected an embedding error in the log, got:\n%s", output)
		}
	})
}
