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
	"sync"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// Threshold hysteresis (GitHub issue #614).
//
// A registry-backed threshold rule raises an alert only once its
// condition has held for threshold.trigger_count consecutive metric
// samples, and an active alert clears only once the condition has been
// false for threshold.clear_count consecutive samples. Before this, both
// happened on a single sample, so a metric hovering near its threshold
// raised and cleared an alert every few minutes.
//
// A sample is a distinct collected_at value, not an evaluation cycle.
// The threshold evaluator runs every minute and the cleaner every thirty
// seconds, whilst most probes collect every five to ten minutes, so the
// same sample is read many times over; reading it again must neither
// advance nor reset a count.
//
// The counts live in memory only. A restart starts every count from
// zero, which at worst delays a raise or a clear by trigger_count or
// clear_count samples; persisting them would buy nothing an operator
// would notice.
//
// The probe-scoped rules (metric_staleness and probe_unavailable) and
// connection error alerts are not subject to these counts: they judge
// time since collection and connection state rather than metric
// samples, so there is no sample to count.

// sampleStreaks counts, per key, how many consecutive distinct samples a
// condition has held for. Its zero value is ready to use, and it is safe
// for concurrent use, although in practice each instance belongs to a
// single engine goroutine.
//
// An entry exists only whilst its condition holds: a judgement that the
// condition does not hold deletes it, so the map stays as small as the
// set of conditions currently holding.
//
// The two users prune differently, and in each case a pass that cannot
// judge a key errs towards leaving the alert state as it is:
//
//   - The evaluator's trigger counts are pass-based. Each evaluation
//     starts with startPass and ends with sweep, which drops every entry
//     the pass did not observe, so a key it could not judge (no row for
//     it, a blackout, a disabled override, a missing extension) has
//     broken its run and starts again from zero; a rule whose values
//     could not be read at all is kept through sweep's keep function,
//     because a failed query says nothing about the condition.
//   - The cleaner's clear counts are reset only by a breaching sample.
//     Missing data, a probe that is not reporting or a missing extension
//     neither advances nor resets them, so a row that briefly drops out
//     between collections cannot stop an alert from ever clearing; retain
//     drops the counts of alerts that are no longer active.
type sampleStreaks[K comparable] struct {
	mu      sync.Mutex
	entries map[K]*sampleStreak
	pass    uint64
}

// sampleStreak is one key's run: the newest sample counted, how many
// consecutive samples the condition has held for, and the pass that last
// observed it.
type sampleStreak struct {
	sample time.Time
	count  int
	pass   uint64
}

// startPass begins a pass; entries not observed before the next sweep
// are dropped by it.
func (s *sampleStreaks[K]) startPass() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pass++
}

// observe records a judgement of key's condition against the sample
// collected at sample, and returns how many consecutive samples the
// condition has now held for (0 when it does not hold).
//
// A sample no newer than the last one counted leaves the count as it
// is, because it is the same sample read again. A judgement that the
// condition does not hold always resets the count, even on a sample
// already counted, since the only way the verdict can change on the same
// sample is an edited threshold, and the conservative reading of that is
// to start again.
func (s *sampleStreaks[K]) observe(key K, sample time.Time, holds bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !holds {
		delete(s.entries, key)
		return 0
	}
	if s.entries == nil {
		s.entries = make(map[K]*sampleStreak)
	}

	entry, ok := s.entries[key]
	if !ok {
		s.entries[key] = &sampleStreak{sample: sample, count: 1, pass: s.pass}
		return 1
	}
	entry.pass = s.pass
	if sample.After(entry.sample) {
		entry.sample = sample
		entry.count++
	}
	return entry.count
}

// sweep drops every entry the current pass did not observe, unless keep
// (which may be nil) asks for it to be kept.
func (s *sampleStreaks[K]) sweep(keep func(K) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, entry := range s.entries {
		if entry.pass == s.pass {
			continue
		}
		if keep != nil && keep(key) {
			continue
		}
		delete(s.entries, key)
	}
}

// retain drops every entry for which keep returns false, whichever pass
// last observed it.
func (s *sampleStreaks[K]) retain(keep func(K) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.entries {
		if !keep(key) {
			delete(s.entries, key)
		}
	}
}

// count returns key's current count without recording anything.
func (s *sampleStreaks[K]) count(key K) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.entries[key]; ok {
		return entry.count
	}
	return 0
}

// size returns the number of entries, for tests.
func (s *sampleStreaks[K]) size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// thresholdSampleKey identifies one stream of samples a threshold rule is
// judged against: one rule on one connection, database and object, which
// is the grain at which the metric queries report values. A nil database
// or object name is kept distinct from an empty one.
type thresholdSampleKey struct {
	ruleID       int64
	connectionID int
	databaseName string
	hasDatabase  bool
	objectName   string
	hasObject    bool
}

// newThresholdSampleKey builds the key for one metric value of a rule.
func newThresholdSampleKey(ruleID int64, mv database.MetricValue) thresholdSampleKey {
	key := thresholdSampleKey{ruleID: ruleID, connectionID: mv.ConnectionID}
	if mv.DatabaseName != nil {
		key.databaseName = *mv.DatabaseName
		key.hasDatabase = true
	}
	if mv.ObjectName != nil {
		key.objectName = *mv.ObjectName
		key.hasObject = true
	}
	return key
}

// thresholdTriggerCount returns threshold.trigger_count. With no
// configuration, or a value below 1 (which Validate rejects), it returns
// 1, which raises on the first breaching sample as the alerter did
// before the setting existed.
func (e *Engine) thresholdTriggerCount() int {
	cfg := e.getConfig()
	if cfg == nil || cfg.Threshold.TriggerCount < 1 {
		return 1
	}
	return cfg.Threshold.TriggerCount
}

// thresholdClearCount returns threshold.clear_count, falling back to 1 in
// the same way as thresholdTriggerCount.
func (e *Engine) thresholdClearCount() int {
	cfg := e.getConfig()
	if cfg == nil || cfg.Threshold.ClearCount < 1 {
		return 1
	}
	return cfg.Threshold.ClearCount
}

// recordViolatingSample tells the clear count that alert's condition
// still holds on the sample collected at sample, which resets it.
func (e *Engine) recordViolatingSample(alert *database.Alert, sample time.Time) {
	e.clearStreaks.observe(alert.ID, sample, false)
}

// recordResolvedSample tells the clear count that alert's condition did
// not hold on the sample collected at sample, and clears the alert once
// that has been true of threshold.clear_count consecutive samples. A
// clear that fails leaves the count where it is, so the next pass
// retries it on the same sample rather than starting again.
func (e *Engine) recordResolvedSample(ctx context.Context, alert *database.Alert,
	sample time.Time, value float64) {
	required := e.thresholdClearCount()
	resolved := e.clearStreaks.observe(alert.ID, sample, true)
	if resolved < required {
		e.debugLog("Alert %d condition not met on %d of %d consecutive samples; leaving it active",
			alert.ID, resolved, required)
		return
	}
	e.clearResolvedAlert(ctx, alert, value)
}
