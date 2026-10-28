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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/pkg/anomaly"
)

// scoringRecorder records each publication of the scoring settings.
type scoringRecorder struct {
	mu    sync.Mutex
	calls []anomaly.ScoringSettings
	err   error
}

func (r *scoringRecorder) publish(_ context.Context, s anomaly.ScoringSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
	return r.err
}

func (r *scoringRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// TestReloadConfigPublishesScoringSettings verifies a reload publishes
// the settings of the new configuration, overrides included.
func TestReloadConfigPublishesScoringSettings(t *testing.T) {
	rec := &scoringRecorder{}
	e := &Engine{config: config.NewConfig(), publishScoring: rec.publish}

	cfg := config.NewConfig()
	cfg.Anomaly.Tier1.MaxZScore = 7.5
	e.ReloadConfig(cfg)

	if rec.count() != 1 {
		t.Fatalf("published %d times, want 1", rec.count())
	}
	if got := rec.calls[0].MaxZScore; got != 7.5 {
		t.Errorf("published MaxZScore = %v, want 7.5", got)
	}
}

// TestReloadConfigNilPublishesNothing verifies a nil configuration is
// not published.
func TestReloadConfigNilPublishesNothing(t *testing.T) {
	rec := &scoringRecorder{}
	e := &Engine{config: config.NewConfig(), publishScoring: rec.publish}
	e.ReloadConfig(nil)
	if rec.count() != 0 {
		t.Errorf("published %d times for a nil config, want 0", rec.count())
	}
}

// TestPublishScoringSettingsFailureIsNotFatal verifies a failed
// publication is only logged.
func TestPublishScoringSettingsFailureIsNotFatal(t *testing.T) {
	rec := &scoringRecorder{err: errors.New("datastore down")}
	e := &Engine{config: config.NewConfig(), publishScoring: rec.publish}
	e.ReloadConfig(config.NewConfig())
	if rec.count() != 1 {
		t.Errorf("published %d times, want 1", rec.count())
	}
}

// TestPublishScoringSettingsWithoutDatastore verifies nothing happens,
// and nothing panics, when there is neither a datastore nor a publisher.
func TestPublishScoringSettingsWithoutDatastore(t *testing.T) {
	e := &Engine{config: config.NewConfig()}
	e.ReloadConfig(config.NewConfig())
}

// TestRunPublishesScoringSettingsAtStart_Integration runs the engine
// against the test database and verifies the settings land in
// alerter_settings.tier1_scoring before the workers start.
func TestRunPublishesScoringSettingsAtStart_Integration(t *testing.T) {
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	cfg := config.NewConfig()
	cfg.Anomaly.Enabled = false
	cfg.Anomaly.Tier1.MaxZScore = 7.5
	e := &Engine{config: cfg, datastore: ds}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := e.Run(ctx); err != nil {
			t.Errorf("Run returned %v", err)
		}
	}()
	time.Sleep(500 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after its context was canceled")
	}

	var raw []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT tier1_scoring FROM alerter_settings WHERE id = 1`).Scan(&raw); err != nil {
		t.Fatalf("failed to read tier1_scoring: %v", err)
	}
	var got anomaly.ScoringSettings
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("stored JSON does not decode: %v", err)
	}
	if got.MaxZScore != 7.5 {
		t.Errorf("published MaxZScore = %v, want 7.5", got.MaxZScore)
	}
}
