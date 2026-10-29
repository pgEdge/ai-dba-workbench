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
	"time"
)

// scoringPublishTimeout bounds one publication of the scoring settings,
// so a slow datastore cannot stall start-up or a configuration reload.
const scoringPublishTimeout = 10 * time.Second

// scoringPublishRetryInterval is how long a failed publication of the
// scoring settings waits before the next attempt. A failure is expected
// on a fresh installation, where the alerter can start before the
// collector has added the alerter_settings.tier1_scoring column.
const scoringPublishRetryInterval = 30 * time.Second

// publishScoringSettings writes the effective Tier 1 scoring settings of
// the current configuration to alerter_settings, where the server reads
// them to score the anomaly comparison view the same way (issue #348).
// Until a publication succeeds the server uses the last published
// settings, or the defaults when there are none. When this attempt fails
// and retry is set, a retry loop bound to ctx tries again every
// scoringPublishRetryInterval until one succeeds; at most one loop runs
// at a time, and since every attempt reads the configuration current at
// that moment, a reload while a retry is pending needs no loop of its
// own and cannot have its settings overwritten by older ones.
func (e *Engine) publishScoringSettings(ctx context.Context, retry bool) {
	e.scoringMu.Lock()
	defer e.scoringMu.Unlock()
	if e.publishScoringLocked(ctx) || !retry || e.scoringRetryLoops > 0 {
		return
	}
	e.scoringRetryLoops++
	go e.retryScoringPublish(ctx)
}

// publishScoringLocked makes one attempt to publish the current
// configuration's scoring settings, records whether a publication is
// still pending, and reports whether nothing is left to publish. The
// caller holds scoringMu, which keeps attempts in order, so that the
// last one to finish always carries the newest configuration. Only the
// first failure of a pending stretch, and the success that ends it, are
// logged at the default level, so a long wait for the collector's
// migration does not flood the log.
func (e *Engine) publishScoringLocked(ctx context.Context) bool {
	publish := e.publishScoring
	if publish == nil && e.datastore != nil {
		publish = e.datastore.PublishScoringSettings
	}
	cfg := e.getConfig()
	if publish == nil || cfg == nil {
		e.scoringPending = false
		return true
	}

	ctx, cancel := context.WithTimeout(ctx, scoringPublishTimeout)
	defer cancel()
	err := publish(ctx, cfg.Anomaly.Tier1.ScoringSettings())
	wasPending := e.scoringPending
	e.scoringPending = err != nil
	switch {
	case err != nil && !wasPending:
		e.log("WARNING: failed to publish Tier 1 scoring settings, will retry: %v", err)
	case err != nil:
		e.debugLog("Tier 1 scoring settings still not published: %v", err)
	case wasPending:
		e.log("Published Tier 1 scoring settings after an earlier failure")
	}
	return err == nil
}

// retryScoringPublish retries a failed publication of the scoring
// settings until one succeeds, a publication made elsewhere leaves
// nothing pending, or ctx is done.
func (e *Engine) retryScoringPublish(ctx context.Context) {
	tick := e.scoringRetryTick
	if tick == nil {
		ticker := time.NewTicker(scoringPublishRetryInterval)
		defer ticker.Stop()
		tick = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			e.scoringMu.Lock()
			e.scoringRetryLoops--
			e.scoringMu.Unlock()
			return
		case <-tick:
		}

		e.scoringMu.Lock()
		done := !e.scoringPending || e.publishScoringLocked(ctx)
		if done {
			e.scoringRetryLoops--
		}
		e.scoringMu.Unlock()
		if done {
			return
		}
	}
}
