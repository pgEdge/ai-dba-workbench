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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
)

// runSystemAlertCountSQL counts the open provider health alerts.
const runSystemAlertCountSQL = `
SELECT count(*) FROM alerts
WHERE alert_type = 'system' AND status = 'active' AND connection_id IS NULL`

// unreachableProviderURL refuses connections at once, so every provider
// call fails quickly without leaving the host.
const unreachableProviderURL = "http://127.0.0.1:1/v1"

// newRunTestConfig returns a configuration that enables every worker Run
// starts, with both providers pointed at an address that refuses
// connections and notifications backed by a throwaway secret.
func newRunTestConfig(t *testing.T) *config.Config {
	t.Helper()
	secretPath := filepath.Join(t.TempDir(), "alerter.secret")
	if err := os.WriteFile(secretPath, []byte("test-secret-value\n"), 0600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	cfg := config.NewConfig()
	cfg.Anomaly.Enabled = true
	cfg.Anomaly.Tier2.Enabled = true
	cfg.Anomaly.Tier3.Enabled = true
	cfg.Anomaly.Tier3.TimeoutSeconds = 5
	cfg.Anomaly.Reevaluation.Enabled = true
	cfg.Anomaly.Reevaluation.IntervalSeconds = 3600
	cfg.LLM.EmbeddingProvider = "openai"
	cfg.LLM.ReasoningProvider = "openai"
	cfg.LLM.OpenAI.BaseURL = unreachableProviderURL
	cfg.LLM.OpenAI.EmbeddingModel = "text-embedding-3-small"
	cfg.LLM.OpenAI.ReasoningModel = "gpt-4o-mini"
	cfg.Notifications.Enabled = true
	cfg.Notifications.SecretFile = secretPath
	return cfg
}

// TestEngineRun_StartupHealthCheck builds an engine through NewEngine
// with a datastore and runs it: the startup check finds both providers
// unreachable and raises a system alert for each at once, and Run stops
// every worker and returns once its context ends.
func TestEngineRun_StartupHealthCheck(t *testing.T) {
	_, ds, pool, cleanup := newEngineSpockTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, providerHealthSchemaSQL); err != nil {
		t.Fatalf("apply system alert schema: %v", err)
	}

	e := NewEngine(newRunTestConfig(t), ds, false)
	if e.providerHealth == nil || e.notificationMgr == nil || e.notificationPool == nil {
		t.Fatalf("NewEngine wiring: health %v, manager %v, pool %v",
			e.providerHealth != nil, e.notificationMgr != nil, e.notificationPool != nil)
	}
	if _, ok := e.embeddingProvider.(*healthTrackingEmbedding); !ok {
		t.Errorf("embedding provider %T is not wrapped", e.embeddingProvider)
	}
	if _, ok := e.reasoningProvider.(*healthTrackingReasoning); !ok {
		t.Errorf("reasoning provider %T is not wrapped", e.reasoningProvider)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.Run(runCtx) }()

	deadline := time.Now().Add(20 * time.Second)
	var open int
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(ctx, runSystemAlertCountSQL).Scan(&open); err != nil {
			t.Fatalf("count system alerts: %v", err)
		}
		if open == 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if open != 2 {
		t.Errorf("open system alerts = %d, want one per failing tier", open)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}
