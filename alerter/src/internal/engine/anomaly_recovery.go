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
//   - once the value has been inside the band for
//     anomaly.tier1.clear_count consecutive passes, clears the alert and
//     queues the usual AlertClear notification.
//
// Any pass in which an alert is not scored in band resets its count: the
// metric or connection did not report, the baseline was cold or had a zero
// divisor, a blackout covered it, or the value was out of band. The alert
// is then held open rather than cleared on the absence of evidence.
//
// Only 'active' alerts take part. An acknowledged alert belongs to the
// user and to re-evaluation, whose fingerprint covers its value, z-score
// and severity; refreshing it would invalidate a stored "keep" verdict
// and buy a fresh Tier 3 call for no change in the anomaly. Recovery is
// Tier 1 only and never calls an LLM provider.
//
// The consecutive counts live in memory, so they restart from zero when
// the alerter restarts; an alert then needs a further clear_count in-band passes before it clears.

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

// anomalyRecoveryPass is the recovery state for one detectAnomalies run.
// The streak counts built here replace the engine's only when the run
// completes, so that an alert not scored in band during the run (and
// therefore absent from next) loses its count.
type anomalyRecoveryPass struct {
	alerts  map[anomalyAlertKey][]*database.Alert
	metrics []string
	next    map[int64]int
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
		next:    make(map[int64]int),
		visited: make(map[int64]bool),
	}
	seenMetric := make(map[string]bool)
	for _, alert := range alerts {
		// The query excludes NULLs; the guard keeps a malformed row from
		// panicking the loop if that ever changes.
		if alert.MetricName == nil {
			continue
		}
		key := newAnomalyAlertKey(*alert.MetricName, alert.ConnectionID, alert.DatabaseName)
		pass.alerts[key] = append(pass.alerts[key], alert)
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

// anomalyStreak returns the consecutive in-band count recorded for an
// alert by the previous completed pass.
func (e *Engine) anomalyStreak(alertID int64) int {
	e.anomalyStreakMu.Lock()
	defer e.anomalyStreakMu.Unlock()
	return e.anomalyStreaks[alertID]
}

// recoverAnomalyAlerts applies one Tier 1 evaluation to the active anomaly
// alerts on the value's series. scored is false when the value could not
// be scored (no usable baseline), which holds the alerts open and resets
// their counts by leaving them out of the pass.
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
	if !pass.covers(key) || !scored {
		return
	}

	// A connection-wide blackout skips the connection before scoring; a
	// database-scoped one only shows up with the database name, so it is
	// checked here. Either way the alert is held open. A failed check is
	// treated as a blackout: holding an alert open one pass longer is
	// cheap, whereas clearing it during a maintenance window is not.
	connID := value.ConnectionID
	blackedOut, err := e.datastore.IsBlackoutActive(ctx, &connID, value.DatabaseName)
	if err != nil {
		e.debugLog("Error checking blackout for anomaly recovery on connection %d: %v", connID, err)
		return
	}
	if blackedOut {
		e.debugLog("Holding anomaly alerts for %s on connection %d open: blackout active",
			metricName, connID)
		return
	}

	inBand := zScore <= sensitivity && zScore >= -sensitivity
	for _, alert := range pass.alerts[key] {
		if pass.visited[alert.ID] {
			continue
		}
		pass.visited[alert.ID] = true

		if !inBand {
			e.refreshAnomalyAlert(ctx, alert, value.Value, zScore, sensitivity)
			continue
		}

		streak := e.anomalyStreak(alert.ID) + 1
		if streak < cfg.Anomaly.Tier1.ClearCount {
			pass.next[alert.ID] = streak
			e.debugLog("Anomaly alert %d back within band (%d/%d)",
				alert.ID, streak, cfg.Anomaly.Tier1.ClearCount)
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

// clearRecoveredAnomalyAlert clears an alert whose metric has stayed in
// band for clear_count passes. A failed clear keeps the count so the next
// in-band pass retries it.
func (e *Engine) clearRecoveredAnomalyAlert(
	ctx context.Context,
	pass *anomalyRecoveryPass,
	alert *database.Alert,
	metricValue, zScore float64,
	streak int,
) {
	cleared, err := e.datastore.ClearActiveAnomalyAlert(ctx, alert.ID)
	if err != nil {
		e.log("ERROR: Failed to clear anomaly alert %d: %v", alert.ID, err)
		pass.next[alert.ID] = streak
		return
	}
	if !cleared {
		// Acknowledged or cleared since the pass read it; nothing to
		// notify about.
		return
	}

	e.log("Anomaly alert %d cleared: %s on connection %d back within band for %d consecutive evaluations (value: %.4f, z-score: %.2f)",
		alert.ID, alert.Title, alert.ConnectionID, streak, metricValue, zScore)
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
