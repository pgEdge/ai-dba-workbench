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

// setErr changes the error later publications return.
func (r *scoringRecorder) setErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

// last returns the most recent publication.
func (r *scoringRecorder) last() anomaly.ScoringSettings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[len(r.calls)-1]
}

// all returns a copy of every publication, oldest first.
func (r *scoringRecorder) all() []anomaly.ScoringSettings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]anomaly.ScoringSettings(nil), r.calls...)
}

// sendTick delivers one retry tick to a waiting retry loop. The channel
// is unbuffered, so the send completes only when the loop has taken the
// tick, and a second send cannot complete until the first has been
// processed.
func sendTick(t *testing.T, tick chan<- time.Time) {
	t.Helper()
	select {
	case tick <- time.Time{}:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out sending a retry tick: no retry loop is waiting")
	}
}

// assertNoRetryLoop fails if a retry loop is still receiving ticks.
func assertNoRetryLoop(t *testing.T, tick chan<- time.Time) {
	t.Helper()
	select {
	case tick <- time.Time{}:
		t.Error("a retry loop took a tick it should have stopped before")
	default:
	}
}

// waitFor polls cond until it holds, failing the test after five seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// retryLoops returns the number of scoring retry loops running on e.
func (e *Engine) retryLoops() int {
	e.scoringMu.Lock()
	defer e.scoringMu.Unlock()
	return e.scoringRetryLoops
}

// configWithMaxZ returns a default configuration with the given z cap,
// which identifies it in a publication.
func configWithMaxZ(z float64) *config.Config {
	cfg := config.NewConfig()
	cfg.Anomaly.Tier1.MaxZScore = z
	return cfg
}

// TestPublishScoringSettingsRetriesUntilSuccess verifies a failed
// publication is retried until one succeeds, and that the retry
// publishes the configuration current at the time, not the one that
// first failed.
func TestPublishScoringSettingsRetriesUntilSuccess(t *testing.T) {
	rec := &scoringRecorder{err: errors.New("column tier1_scoring does not exist")}
	tick := make(chan time.Time)
	e := &Engine{config: configWithMaxZ(5), publishScoring: rec.publish,
		scoringRetryTick: tick}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e.publishScoringSettings(ctx, true)
	sendTick(t, tick)
	sendTick(t, tick)
	waitFor(t, "two failed retries", func() bool { return rec.count() == 3 })

	e.mu.Lock()
	e.config = configWithMaxZ(9)
	e.mu.Unlock()
	rec.setErr(nil)
	sendTick(t, tick)

	waitFor(t, "the retry loop to stop", func() bool { return e.retryLoops() == 0 })
	if n := rec.count(); n != 4 {
		t.Errorf("published %d times, want 4 (start, two failures, success)", n)
	}
	if got := rec.last().MaxZScore; got != 9 {
		t.Errorf("published MaxZScore = %v, want the current configuration's 9", got)
	}
	assertNoRetryLoop(t, tick)
}

// TestPublishScoringSettingsRetryStopsWithContext verifies the retry
// loop ends when its context is canceled, even if every attempt fails.
func TestPublishScoringSettingsRetryStopsWithContext(t *testing.T) {
	rec := &scoringRecorder{err: errors.New("datastore down")}
	tick := make(chan time.Time)
	e := &Engine{config: config.NewConfig(), publishScoring: rec.publish,
		scoringRetryTick: tick}
	ctx, cancel := context.WithCancel(context.Background())

	e.publishScoringSettings(ctx, true)
	sendTick(t, tick)
	waitFor(t, "a retry", func() bool { return rec.count() == 2 })
	cancel()

	waitFor(t, "the retry loop to stop", func() bool { return e.retryLoops() == 0 })
	assertNoRetryLoop(t, tick)
	if n := rec.count(); n != 2 {
		t.Errorf("published %d times, want 2 (none after cancellation)", n)
	}
}

// TestReloadDuringPendingRetry verifies a reload while a retry is
// pending starts no second loop, and that the newest configuration is
// what ends up published, whether the reload's own attempt succeeds or
// fails.
func TestReloadDuringPendingRetry(t *testing.T) {
	for _, reloadSucceeds := range []bool{true, false} {
		name := "reload fails"
		if reloadSucceeds {
			name = "reload succeeds"
		}
		t.Run(name, func(t *testing.T) {
			rec := &scoringRecorder{err: errors.New("datastore down")}
			// No tick arrives until the test sends one, so the loop
			// waits while the reload runs.
			tick := make(chan time.Time)
			e := &Engine{config: configWithMaxZ(5), publishScoring: rec.publish,
				scoringRetryTick: tick}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e.ctx = ctx

			e.publishScoringSettings(ctx, true)
			if e.retryLoops() == 0 {
				t.Fatal("no retry loop after a failed publication")
			}
			if reloadSucceeds {
				rec.setErr(nil)
			}
			e.ReloadConfig(configWithMaxZ(9))
			if rec.count() != 2 {
				t.Fatalf("published %d times, want the start and the reload", rec.count())
			}
			if loops := e.retryLoops(); loops != 1 {
				t.Errorf("%d retry loops running, want 1", loops)
			}

			rec.setErr(nil)
			sendTick(t, tick)
			waitFor(t, "the retry loop to stop", func() bool { return e.retryLoops() == 0 })
			assertNoRetryLoop(t, tick)
			for i, c := range rec.all()[1:] {
				if c.MaxZScore != 9 {
					t.Errorf("publication %d after the start has MaxZScore %v, want the reloaded 9", i+1, c.MaxZScore)
				}
			}
			want := 3
			if reloadSucceeds {
				// Nothing was pending when the loop woke, so it
				// published nothing.
				want = 2
			}
			if rec.count() != want {
				t.Errorf("published %d times, want %d", rec.count(), want)
			}
		})
	}
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

// TestRunRetriesFailedScoringPublish_Integration verifies Run keeps
// retrying when its first publication fails, so settings are published
// once the datastore recovers.
func TestRunRetriesFailedScoringPublish_Integration(t *testing.T) {
	ds, _, cleanup := newEngineIntegrationTestEnv(t)
	defer cleanup()

	rec := &scoringRecorder{err: errors.New("column tier1_scoring does not exist")}
	tick := make(chan time.Time)
	cfg := config.NewConfig()
	cfg.Anomaly.Enabled = false
	e := &Engine{config: cfg, datastore: ds, publishScoring: rec.publish,
		scoringRetryTick: tick}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := e.Run(ctx); err != nil {
			t.Errorf("Run returned %v", err)
		}
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("Run did not return after its context was canceled")
		}
	}()

	waitFor(t, "the first publication", func() bool { return rec.count() == 1 })
	rec.setErr(nil)
	sendTick(t, tick)
	waitFor(t, "the retry loop to stop", func() bool { return e.retryLoops() == 0 })
	if n := rec.count(); n != 2 {
		t.Errorf("published %d times, want 2 (failed start, successful retry)", n)
	}
}

// TestReloadConfigRetryDependsOnRunContext verifies a failed reload is
// retried once Run has set the engine context, even after an earlier
// success, and that before Run no retry loop is started.
func TestReloadConfigRetryDependsOnRunContext(t *testing.T) {
	t.Run("after Run starts", func(t *testing.T) {
		rec := &scoringRecorder{}
		tick := make(chan time.Time)
		e := &Engine{config: configWithMaxZ(5), publishScoring: rec.publish,
			scoringRetryTick: tick}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.ctx = ctx

		e.publishScoringSettings(ctx, true)
		if loops := e.retryLoops(); loops != 0 {
			t.Fatalf("%d retry loops after a successful start, want 0", loops)
		}
		rec.setErr(errors.New("datastore down"))
		e.ReloadConfig(configWithMaxZ(9))
		if loops := e.retryLoops(); loops != 1 {
			t.Fatalf("%d retry loops after a failed reload, want 1", loops)
		}
		rec.setErr(nil)
		sendTick(t, tick)
		waitFor(t, "the retry loop to stop", func() bool { return e.retryLoops() == 0 })
		if got := rec.last().MaxZScore; got != 9 {
			t.Errorf("retry published MaxZScore = %v, want 9", got)
		}
	})

	t.Run("before Run", func(t *testing.T) {
		rec := &scoringRecorder{err: errors.New("datastore down")}
		tick := make(chan time.Time)
		e := &Engine{config: configWithMaxZ(5), publishScoring: rec.publish,
			scoringRetryTick: tick}
		e.ReloadConfig(configWithMaxZ(9))
		if loops := e.retryLoops(); loops != 0 {
			t.Errorf("%d retry loops before Run, want 0", loops)
		}
		assertNoRetryLoop(t, tick)
	})
}
