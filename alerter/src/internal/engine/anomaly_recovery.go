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

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// Anomaly alert recovery (GitHub issue #611).
//
// The threshold cleaner re-evaluates a rule's condition and clears the
// alert once it no longer holds; an anomaly alert has no rule or threshold,
// so cleanResolvedAlerts never touches it. Instead, each Tier 1 pass in
// detectAnomalies re-scores the metric behind every active anomaly alert
// against the same baseline Tier 1 would select, and:
//
//   - while the value stays outside the sensitivity band, refreshes the
//     alert's metric_value, anomaly_score and last_updated, escalating but
//     never lowering its severity;
//   - once anomaly.tier1.clear_count consecutive samples of the metric
//     have scored inside the band, clears the alert and queues the usual
//     AlertClear notification.
//
// The count is of distinct samples, not of passes. Tier 1 runs every
// evaluation_interval_seconds (60 by default), whereas most probes
// collect only every 300 to 600 seconds, so several passes in a row
// usually re-score the same sample; counting passes would clear an alert
// on a single reading. A streak therefore records the collected_at of the
// last sample it counted and advances only on a newer one. Re-scoring the
// same sample neither advances nor resets it.
//
// The alert is held open, never cleared, whenever it cannot be judged:
//
//   - a pass in which the metric or connection did not report keeps the
//     count as it was, since no sample was missed, only none was new;
//   - an out-of-band sample, a cold or unusable baseline (no sample can be
//     judged against it) and a blackout, connection-wide or on the alert's
//     database, reset the count, so that clearing needs clear_count fresh
//     in-band samples taken after the condition lifts.
//
// Only 'active' alerts take part. An acknowledged alert belongs to the
// user and to re-evaluation, whose fingerprint covers its value, z-score
// and severity; refreshing it would invalidate a stored "keep" verdict
// and buy a fresh Tier 3 call for no change in the anomaly. Recovery is
// Tier 1 only and never calls an LLM provider.
//
// The consecutive counts live in memory, so they restart from zero when
// the alerter restarts; an alert then needs a further clear_count in-band
// samples before it clears.

// anomalyAlertKey identifies the metric series an anomaly alert was raised
// on: the metric, connection and, for per-database metrics, the database.
type anomalyAlertKey struct {
	metric string
	connID int
	db     string
	hasDB  bool
}

// newAnomalyAlertKey builds the key for a metric series, distinguishing a
// connection-wide series (nil database) from a per-database one.
func newAnomalyAlertKey(metric string, connID int, db *string) anomalyAlertKey {
	key := anomalyAlertKey{metric: metric, connID: connID}
	if db != nil {
		key.db = *db
		key.hasDB = true
	}
	return key
}

// anomalyStreak is an alert's run of consecutive in-band samples: how
// many there have been, and the collected_at of the newest one counted.
type anomalyStreak struct {
	count      int
	lastSample time.Time
}

// anomalyRecoveryPass is the recovery state for one detectAnomalies run.
// next starts as a copy of the engine's streaks for the alerts still
// active, so an alert the run does not reach keeps its count; the run then
// advances or resets entries, and the result replaces the engine's
// streaks only when the run completes.
type anomalyRecoveryPass struct {
	alerts  map[anomalyAlertKey][]*database.Alert
	metrics []string
	next    map[int64]anomalyStreak
	visited map[int64]bool
}

// empty reports whether the pass has no active anomaly alert to recover.
func (p *anomalyRecoveryPass) empty() bool {
	return p == nil || len(p.alerts) == 0
}

// covers reports whether an active anomaly alert exists on the series.
func (p *anomalyRecoveryPass) covers(key anomalyAlertKey) bool {
	return p != nil && len(p.alerts[key]) > 0
}

// resetSeries drops the counts of the alerts on one series to zero. The
// newest sample already counted is remembered, so that re-scoring it
// after the reset cannot count it a second time.
func (p *anomalyRecoveryPass) resetSeries(key anomalyAlertKey) {
	for _, alert := range p.alerts[key] {
		p.resetAlert(alert.ID, time.Time{})
	}
}

// resetAlert drops one alert's count to zero, remembering whichever is
// newer of the sample it last counted and the given one.
func (p *anomalyRecoveryPass) resetAlert(alertID int64, sample time.Time) {
	last := p.next[alertID].lastSample
	if sample.After(last) {
		last = sample
	}
	if last.IsZero() {
		delete(p.next, alertID)
		return
	}
	p.next[alertID] = anomalyStreak{lastSample: last}
}

// resetConnection discards the streaks of every alert on a connection,
// for a pass in which a connection-wide blackout skipped it.
func (p *anomalyRecoveryPass) resetConnection(connID int) {
	if p == nil {
		return
	}
	for key := range p.alerts {
		if key.connID == connID {
			p.resetSeries(key)
		}
	}
}

// loadAnomalyRecovery reads the active anomaly alerts for this run. It
// returns nil when they cannot be read, in which case detection carries on
// without recovery and the existing streak counts are kept untouched.
func (e *Engine) loadAnomalyRecovery(ctx context.Context) *anomalyRecoveryPass {
	alerts, err := e.datastore.ListActiveAnomalyAlerts(ctx)
	if err != nil {
		e.log("ERROR: Failed to list active anomaly alerts for recovery: %v", err)
		return nil
	}

	pass := &anomalyRecoveryPass{
		alerts:  make(map[anomalyAlertKey][]*database.Alert),
		next:    make(map[int64]anomalyStreak),
		visited: make(map[int64]bool),
	}
	seenMetric := make(map[string]bool)
	e.anomalyStreakMu.Lock()
	defer e.anomalyStreakMu.Unlock()
	for _, alert := range alerts {
		// The query excludes NULLs; the guard keeps a malformed row from
		// panicking the loop if that ever changes.
		if alert.MetricName == nil {
			continue
		}
		key := newAnomalyAlertKey(*alert.MetricName, alert.ConnectionID, alert.DatabaseName)
		pass.alerts[key] = append(pass.alerts[key], alert)
		if streak, ok := e.anomalyStreaks[alert.ID]; ok {
			pass.next[alert.ID] = streak
		}
		if !seenMetric[*alert.MetricName] {
			seenMetric[*alert.MetricName] = true
			pass.metrics = append(pass.metrics, *alert.MetricName)
		}
	}
	return pass
}

// finishAnomalyRecovery installs the streak counts built during a
// completed pass. A nil pass (the alerts could not be read) keeps the
// previous counts, since nothing was learned about the alerts.
func (e *Engine) finishAnomalyRecovery(pass *anomalyRecoveryPass) {
	if pass == nil {
		return
	}
	e.anomalyStreakMu.Lock()
	e.anomalyStreaks = pass.next
	e.anomalyStreakMu.Unlock()
}

// resetAnomalyStreaks discards every streak count.
func (e *Engine) resetAnomalyStreaks() {
	e.anomalyStreakMu.Lock()
	e.anomalyStreaks = nil
	e.anomalyStreakMu.Unlock()
}

// anomalyStreak returns the consecutive in-band sample count recorded
// for an alert by the previous completed pass.
func (e *Engine) anomalyStreak(alertID int64) int {
	e.anomalyStreakMu.Lock()
	defer e.anomalyStreakMu.Unlock()
	return e.anomalyStreaks[alertID].count
}

// recoverAnomalyAlerts applies one Tier 1 evaluation of the latest sample
// to the active anomaly alerts on the value's series. scored is false when
// the value could not be scored (no usable baseline), which holds the
// alerts open and resets their counts.
func (e *Engine) recoverAnomalyAlerts(
	ctx context.Context,
	pass *anomalyRecoveryPass,
	metricName string,
	value *database.MetricValue,
	zScore float64,
	scored bool,
	cfg *config.Config,
	sensitivity float64,
) {
	key := newAnomalyAlertKey(metricName, value.ConnectionID, value.DatabaseName)
	if !pass.covers(key) {
		return
	}
	if !scored {
		pass.resetSeries(key)
		return
	}

	// A connection-wide blackout skips the connection before scoring; a
	// database-scoped one only shows up with the database name, so it is
	// checked here. Either way the alert is held open and its count
	// reset. A failed check is treated as a blackout: holding an alert
	// open a little longer is cheap, whereas clearing it during a
	// maintenance window is not.
	connID := value.ConnectionID
	blackedOut, err := e.datastore.IsBlackoutActive(ctx, &connID, value.DatabaseName)
	if err != nil {
		e.debugLog("Error checking blackout for anomaly recovery on connection %d: %v", connID, err)
		pass.resetSeries(key)
		return
	}
	if blackedOut {
		e.debugLog("Holding anomaly alerts for %s on connection %d open: blackout active",
			metricName, connID)
		pass.resetSeries(key)
		return
	}

	inBand := zScore <= sensitivity && zScore >= -sensitivity
	for _, alert := range pass.alerts[key] {
		if pass.visited[alert.ID] {
			continue
		}
		pass.visited[alert.ID] = true

		if !inBand {
			pass.resetAlert(alert.ID, value.CollectedAt)
			e.refreshAnomalyAlert(ctx, alert, value.Value, zScore, sensitivity)
			continue
		}

		// Only a sample newer than the last one counted advances the
		// streak; re-scoring the same sample leaves it as it was.
		streak := pass.next[alert.ID]
		newSample := value.CollectedAt.After(streak.lastSample)
		if newSample {
			streak = anomalyStreak{count: streak.count + 1, lastSample: value.CollectedAt}
		}
		if streak.count < cfg.Anomaly.Tier1.ClearCount {
			pass.next[alert.ID] = streak
			if newSample {
				e.debugLog("Anomaly alert %d back within band (%d/%d samples)",
					alert.ID, streak.count, cfg.Anomaly.Tier1.ClearCount)
			}
			continue
		}
		e.clearRecoveredAnomalyAlert(ctx, pass, alert, value.Value, zScore, streak)
	}
}

// refreshAnomalyAlert records an out-of-band evaluation on an alert that
// stays open. Severity only escalates: the alert was raised at its
// original severity and a temporarily smaller deviation should not quietly
// downgrade it. No notification is sent, matching the threshold path,
// which updates an open alert's value and severity without notifying.
func (e *Engine) refreshAnomalyAlert(
	ctx context.Context,
	alert *database.Alert,
	metricValue, zScore, sensitivity float64,
) {
	severity := escalatedSeverity(alert.Severity, anomalySeverity(zScore, sensitivity))
	updated, err := e.datastore.RefreshAnomalyAlert(ctx, alert.ID, metricValue, zScore, severity)
	if err != nil {
		e.log("ERROR: Failed to refresh anomaly alert %d: %v", alert.ID, err)
		return
	}
	if !updated {
		// Acknowledged or cleared since the pass read it.
		return
	}
	if severity != alert.Severity {
		e.log("Anomaly alert %d escalated from %s to %s (z-score: %.2f)",
			alert.ID, alert.Severity, severity, zScore)
	}
	alert.MetricValue = &metricValue
	alert.AnomalyScore = &zScore
	alert.Severity = severity
}

// clearRecoveredAnomalyAlert clears an alert whose metric has scored in
// band for clear_count consecutive samples. A failed clear keeps the
// streak, so the next pass that scores the metric in band retries it
// without waiting for a newer sample.
func (e *Engine) clearRecoveredAnomalyAlert(
	ctx context.Context,
	pass *anomalyRecoveryPass,
	alert *database.Alert,
	metricValue, zScore float64,
	streak anomalyStreak,
) {
	cleared, err := e.datastore.ClearActiveAnomalyAlert(ctx, alert.ID)
	if err != nil {
		e.log("ERROR: Failed to clear anomaly alert %d: %v", alert.ID, err)
		pass.next[alert.ID] = streak
		return
	}
	delete(pass.next, alert.ID)
	if !cleared {
		// Acknowledged or cleared since the pass read it; nothing to
		// notify about.
		return
	}

	e.log("Anomaly alert %d cleared: %s on connection %d back within band for %d consecutive samples (value: %.4f, z-score: %.2f)",
		alert.ID, alert.Title, alert.ConnectionID, streak.count, metricValue, zScore)
	alert.Status = "cleared"
	e.queueNotification(alert, database.NotificationTypeAlertClear)
}

// severityRank orders alert severities for escalation.
func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

// escalatedSeverity returns whichever of the two severities is higher.
func escalatedSeverity(current, candidate string) string {
	if severityRank(candidate) > severityRank(current) {
		return candidate
	}
	return current
}

// anomalySeverity maps a z-score to an alert severity using multiples of
// the detection sensitivity, so that alerts do not all cluster at
// "critical" when baselines have small standard deviations.
func anomalySeverity(zScore, sensitivity float64) string {
	absZScore := zScore
	if absZScore < 0 {
		absZScore = -absZScore
	}
	switch {
	case absZScore >= 4*sensitivity:
		return "critical"
	case absZScore >= 2*sensitivity:
		return "warning"
	default:
		return "info"
	}
}
