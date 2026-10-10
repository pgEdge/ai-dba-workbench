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
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// effectiveStdDev applies the hybrid variance floor described in
// the anomaly detector design. Returns max(raw_stddev,
// max(|mean| * RelativePct, AbsoluteFloor)). Callers retain the
// existing stddev == 0 guard for the degenerate case where both
// floor knobs are zero.
func effectiveStdDev(
	b database.MetricBaseline,
	cfg config.VarianceFloorConfig,
) float64 {
	relFloor := math.Abs(b.Mean) * cfg.RelativePct
	floor := math.Max(relFloor, cfg.AbsoluteFloor)
	return math.Max(b.StdDev, floor)
}

// isBaselineWarm reports whether the baseline has accumulated
// enough samples and enough wall-clock span to be trustworthy
// for anomaly detection. A zero EarliestSampleAt (e.g. a row
// written before the column was populated, or a fallback
// baseline that lacks raw sample timestamps) is treated as not
// warm.
//
// The span check is skipped when the configured MinSpanHours is
// zero, which is the documented escape hatch to disable the
// time-span half of the gate.
//
// Unknown period_types fall back to the daily thresholds, which
// are the strictest of the three configured pairs by default.
// This is defensive; period_type is enum-constrained at write
// time.
func isBaselineWarm(
	b database.MetricBaseline,
	cfg config.WarmupConfig,
	now time.Time,
) bool {
	thresh := warmupThresholdFor(b.PeriodType, cfg)
	if b.SampleCount < int64(thresh.MinSamples) {
		return false
	}
	if thresh.MinSpanHours > 0 {
		if b.EarliestSampleAt.IsZero() {
			return false
		}
		minSpan := time.Duration(thresh.MinSpanHours) * time.Hour
		if now.Sub(b.EarliestSampleAt) < minSpan {
			return false
		}
	}
	return true
}

// warmupThresholdFor selects the per-period_type warmup
// thresholds, falling back to the daily thresholds for any
// unrecognized period_type.
func warmupThresholdFor(
	periodType string,
	cfg config.WarmupConfig,
) config.PerPeriodWarmupConfig {
	switch periodType {
	case "all":
		return cfg.All
	case "hourly":
		return cfg.Hourly
	case "daily":
		return cfg.Daily
	default:
		return cfg.Daily
	}
}

// baselineCandidates returns the rows selectBaseline considers, in
// preference order: the hourly row for now's UTC hour, the daily row for
// now's UTC weekday, then the 'all' row. Missing rows are nil entries so
// the caller can keep the order without re-deriving it.
func baselineCandidates(
	baselines []*database.MetricBaseline,
	now time.Time,
) []*database.MetricBaseline {
	hour, weekday := baselinePeriodKeys(now)

	var hourly, daily, all *database.MetricBaseline
	for _, b := range baselines {
		if b == nil {
			continue
		}
		switch b.PeriodType {
		case "hourly":
			if b.HourOfDay != nil && *b.HourOfDay == hour {
				hourly = b
			}
		case "daily":
			if b.DayOfWeek != nil && *b.DayOfWeek == weekday {
				daily = b
			}
		case "all":
			all = b
		}
	}
	return []*database.MetricBaseline{hourly, daily, all}
}

// selectBaseline picks the baseline row to score a value against. The
// preference order is the hourly row for the current UTC hour, then the
// daily row for the current UTC weekday, then the 'all' row; the first
// candidate in that order that passes isBaselineWarm is returned as
// chosen. skippedCold is the most preferred candidate that exists but
// failed the gate, whether or not a later candidate was chosen, so the
// caller can log both a suppressed detection and a fall-through to a
// less specific baseline. It is nil when nothing was skipped.
//
// Hourly and daily rows are preferred over 'all' because a metric with a
// diurnal or weekly cycle has a much tighter spread within one period
// than across the whole window; scoring against the grand mean inflates
// the divisor and hides genuine within-cycle deviation. Warmth is
// checked per candidate so a cold hourly row does not block a mature
// 'all' row, and a cold 'all' row does not block a mature hourly one.
// See GitHub issue #408.
func selectBaseline(
	baselines []*database.MetricBaseline,
	now time.Time,
	cfg config.WarmupConfig,
) (chosen, skippedCold *database.MetricBaseline) {
	for _, candidate := range baselineCandidates(baselines, now) {
		if candidate == nil {
			continue
		}
		if isBaselineWarm(*candidate, cfg, now) {
			return candidate, skippedCold
		}
		if skippedCold == nil {
			skippedCold = candidate
		}
	}
	return nil, skippedCold
}

// detectAnomalies runs the tiered anomaly detection
func (e *Engine) detectAnomalies(ctx context.Context) {
	e.debugLog("Running anomaly detection...")

	cfg := e.getConfig()

	if !cfg.Anomaly.Enabled || !cfg.Anomaly.Tier1.Enabled {
		e.resetAnomalyStreaks()
		return
	}

	// Active anomaly alerts are re-scored on every pass so they clear
	// once enough new samples of their metric have recovered (issue #611). Recovery is Tier 1 only,
	// so it runs even when no later tier could process a new candidate.
	recovery := e.loadAnomalyRecovery(ctx)

	// A candidate that no later tier can process would never raise an
	// alert or be cleaned up, so write none; startup applies the same
	// rule by disabling anomaly detection (issue #581).
	detect := e.anomalyProcessingAvailable(cfg)
	if !detect {
		e.debugLog("Skipping anomaly detection: no LLM provider available for the enabled tiers")
		if recovery.empty() {
			e.finishAnomalyRecovery(recovery)
			return
		}
	}

	// Get all active connections
	connections, err := e.datastore.GetActiveConnections(ctx)
	if err != nil {
		e.log("ERROR: Failed to get active connections: %v", err)
		return
	}

	// Get all enabled alert rules
	rules, err := e.datastore.GetEnabledAlertRules(ctx)
	if err != nil {
		e.log("ERROR: Failed to get alert rules: %v", err)
		return
	}

	// Blackout status is per connection and does not change during a
	// run, so resolve it once rather than once per rule. evaluable is the
	// complement: the active connections the run scores.
	blackedOut := make(map[int]bool, len(connections))
	evaluable := make(map[int]bool, len(connections))
	for _, connID := range connections {
		active, err := e.datastore.IsBlackoutActive(ctx, &connID, nil)
		if err != nil {
			e.debugLog("Error checking blackout for connection %d: %v", connID, err)
		}
		if active {
			e.debugLog("Skipping anomaly detection for connection %d: blackout active", connID)
			blackedOut[connID] = true
			recovery.resetConnection(connID)
			continue
		}
		evaluable[connID] = true
	}

	sensitivity := cfg.Anomaly.Tier1.DefaultSensitivity
	now := time.Now()

	// Metrics are the outer loop so each metric's latest values are
	// fetched once per run instead of once per connection.
	for _, metric := range anomalyMetrics(rules, recovery) {
		if ctx.Err() != nil {
			return
		}

		// An absence-driven metric reports nothing at all while no
		// connection has the condition, which is the very state its
		// alerts recover into, so an empty result is carried on into
		// recovery rather than skipped.
		values, err := e.datastore.GetLatestMetricValues(ctx, metric.name)
		if err != nil && !errors.Is(err, database.ErrNoMetricData) {
			continue
		}

		for _, connID := range connections {
			if ctx.Err() != nil {
				return
			}
			if blackedOut[connID] {
				continue
			}

			// Per-database metrics return one latest value per
			// database on the connection; each is scored against the
			// baseline written for that same database.
			for _, value := range baselineableValues(values, connID) {
				recordCandidate := detect && metric.fromRule
				key := newAnomalyAlertKey(metric.name, value.ConnectionID, value.DatabaseName)
				if !recordCandidate && !recovery.covers(key) {
					continue
				}

				score := e.scoreAnomalyValue(ctx, metric.name, value, cfg, now)
				if !recovery.empty() {
					e.recoverAnomalyAlerts(ctx, recovery, metric.name, value, score, cfg, sensitivity)
				}
				if recordCandidate && score.scored {
					e.recordAnomalyCandidate(ctx, metric.name, value, score, sensitivity)
				}
			}
		}

		e.recoverAbsentAnomalyAlerts(ctx, recovery, metric.name, values, evaluable, cfg)
	}

	e.finishAnomalyRecovery(recovery)

	if detect {
		e.processTier2And3(ctx)
	}
}

// anomalyMetric is a metric to score in a Tier 1 pass. fromRule is false
// for a metric scored only because an active anomaly alert was raised on
// it and no enabled rule still covers it; such a metric is re-scored for
// recovery but never produces a new candidate.
type anomalyMetric struct {
	name     string
	fromRule bool
}

// anomalyMetrics returns each baselineable metric once: those of the
// enabled rules first, in rule order, then those of active anomaly alerts
// that no rule covers. Several rules on one metric used to score it, and
// record a candidate for it, once per rule.
func anomalyMetrics(rules []*database.AlertRule, recovery *anomalyRecoveryPass) []anomalyMetric {
	seen := make(map[string]bool)
	var metrics []anomalyMetric
	add := func(name string, fromRule bool) {
		// Metrics without a historical query have no baselines to
		// score against (see calculateBaselines), so skip them
		// rather than querying for rows that cannot exist.
		if seen[name] || !database.SupportsBaselines(name) {
			return
		}
		seen[name] = true
		metrics = append(metrics, anomalyMetric{name: name, fromRule: fromRule})
	}
	for _, rule := range rules {
		add(rule.MetricName, true)
	}
	if recovery != nil {
		for _, name := range recovery.metrics {
			add(name, false)
		}
	}
	return metrics
}

// baselineableValues returns the latest values that belong to the
// connection and can be paired with a baseline row. metric_baselines has
// no object column, so a value scoped to a table or other object is left
// out; no such metric currently has a historical query, and this guard
// keeps that assumption explicit rather than scoring an object-scoped
// value against a connection- or database-wide baseline.
func baselineableValues(values []database.MetricValue, connID int) []*database.MetricValue {
	var out []*database.MetricValue
	for i := range values {
		v := &values[i]
		if v.ConnectionID != connID || v.ObjectName != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}

// detectAnomalyForValue scores one latest metric value against the
// baselines written for its connection and database and records a
// tier-1 candidate when the z-score exceeds the sensitivity.
func (e *Engine) detectAnomalyForValue(
	ctx context.Context,
	metricName string,
	value *database.MetricValue,
	cfg *config.Config,
	sensitivity float64,
	now time.Time,
) {
	score := e.scoreAnomalyValue(ctx, metricName, value, cfg, now)
	if !score.scored {
		return
	}
	e.recordAnomalyCandidate(ctx, metricName, value, score, sensitivity)
}

// anomalyScore is the Tier 1 evaluation of one latest metric value.
type anomalyScore struct {
	// zScore is the value's distance from the baseline mean in units of
	// the floored divisor, clamped to max_z_score. It is 0 when
	// belowFloor is set, since no baseline was consulted.
	zScore float64

	// baseline is the warm baseline the value was scored against; it is
	// nil when belowFloor is set.
	baseline *database.MetricBaseline

	// effectiveStdDev is the divisor zScore was computed with.
	effectiveStdDev float64

	// scored is false when the value could not be evaluated: no usable
	// baseline exists, none is warm, or the floored divisor is zero.
	scored bool

	// belowFloor is set when the value is under the metric's MinValue
	// floor, so it is normal whatever its baseline says.
	belowFloor bool

	// stddevFloored is set when the baseline has no spread of its own:
	// its standard deviation is at or below variance_floor.absolute_floor,
	// so a floor supplied the whole divisor. A baseline that merely varies
	// by less than variance_floor.relative_pct of its mean, or by less
	// than the metric's MinStdDev, has a real spread and is not flagged.
	stddevFloored bool
}

// inBand reports whether the score is normal: under the metric's value
// floor, or with |z| within the sensitivity. Detection raises no
// candidate for an in-band value, and recovery counts it towards
// clearing an open alert, so the two paths cannot disagree.
func (s anomalyScore) inBand(sensitivity float64) bool {
	return s.belowFloor || (s.zScore <= sensitivity && s.zScore >= -sensitivity)
}

// severity is the alert severity for the score; see anomalySeverity and
// cappedAnomalySeverity.
func (s anomalyScore) severity(sensitivity float64) string {
	return cappedAnomalySeverity(s.zScore, sensitivity, s.stddevFloored)
}

// cappedAnomalySeverity maps a z-score to a severity, but never to
// "critical" when the baseline had no spread of its own (GitHub issue
// #617; see anomalyScore.stddevFloored).
//
// Against a flat baseline, the z-score measures distance in units a
// floor chose rather than in the metric's observed spread. It still
// says the value is unusual, which is enough for an info or warning
// alert, but it is not the statistical evidence a critical alert
// implies. This is also what kept a flat baseline from turning any
// small change into a z-score clamped at max_z_score and therefore a
// critical alert. Threshold rules remain the path for a critical alert
// on an absolute value.
func cappedAnomalySeverity(zScore, sensitivity float64, stddevFloored bool) string {
	severity := anomalySeverity(zScore, sensitivity)
	if stddevFloored && severity == "critical" {
		return "warning"
	}
	return severity
}

// scoreAnomalyValue evaluates one latest metric value. A value under the
// metric's MinValue floor is normal without consulting a baseline;
// otherwise the value is scored against the warm baseline Tier 1 selects
// for its connection and database, using the floored divisor.
func (e *Engine) scoreAnomalyValue(
	ctx context.Context,
	metricName string,
	value *database.MetricValue,
	cfg *config.Config,
	now time.Time,
) anomalyScore {
	connID := value.ConnectionID

	// The value floor is checked first: it needs no baseline, so a
	// value under it also counts as normal for recovery when the
	// baseline is cold or missing.
	floor := cfg.Anomaly.Tier1.MetricFloor(metricName)
	if floor.MinValue > 0 && value.Value < floor.MinValue {
		return anomalyScore{scored: true, belowFloor: true}
	}

	baselines, err := e.datastore.GetMetricBaselines(ctx, connID, metricName, value.DatabaseName)
	if err != nil || len(baselines) == 0 {
		return anomalyScore{}
	}

	// Warmup gate: prefer the time-aware baselines, and skip
	// entirely when no candidate has accumulated enough samples or
	// wall-clock span to be trustworthy.
	baseline, cold := selectBaseline(baselines, now, cfg.Anomaly.Tier1.Warmup)
	if baseline == nil {
		if cold != nil {
			e.debugLog(
				"Anomaly suppressed: baseline not warm "+
					"(connection=%d metric=%s period=%s samples=%d earliest=%s)",
				connID, metricName, cold.PeriodType,
				cold.SampleCount, cold.EarliestSampleAt,
			)
		}
		return anomalyScore{}
	}
	if cold != nil {
		e.debugLog(
			"Using %s baseline: preferred %s baseline not warm "+
				"(connection=%d metric=%s samples=%d earliest=%s)",
			baseline.PeriodType, cold.PeriodType, connID, metricName,
			cold.SampleCount, cold.EarliestSampleAt,
		)
	}

	// Variance floor: never divide by a divisor smaller than the
	// global hybrid floor or the metric's own MinStdDev (issue #617).
	// The global absolute_floor only guards against division by zero,
	// so on a flat baseline it turned a change of one unit into a
	// z-score in the thousands; MinStdDev sizes the divisor to the
	// smallest change that matters in the metric's units, so a small
	// value scores low while a genuinely large jump still scores high.
	// If every floor is zero the divisor can still be zero, so the
	// degenerate-case skip is retained.
	stddev := math.Max(
		effectiveStdDev(*baseline, cfg.Anomaly.Tier1.VarianceFloor),
		floor.MinStdDev,
	)
	if stddev == 0 {
		return anomalyScore{}
	}

	// Calculate z-score using the floored divisor.
	zScore := (value.Value - baseline.Mean) / stddev

	// Symmetric z-score cap: clamp |zScore| to MaxZScore
	// when the cap is positive. A zero cap disables the
	// clamp entirely.
	if zCap := cfg.Anomaly.Tier1.MaxZScore; zCap > 0 {
		if zScore > zCap {
			zScore = zCap
		} else if zScore < -zCap {
			zScore = -zCap
		}
	}
	return anomalyScore{
		zScore:          zScore,
		baseline:        baseline,
		effectiveStdDev: stddev,
		scored:          true,
		stddevFloored:   baseline.StdDev <= cfg.Anomaly.Tier1.VarianceFloor.AbsoluteFloor,
	}
}

// recordAnomalyCandidate records a tier-1 candidate for a scored value
// that lies outside the sensitivity band and above the metric's value
// floor.
func (e *Engine) recordAnomalyCandidate(
	ctx context.Context,
	metricName string,
	value *database.MetricValue,
	score anomalyScore,
	sensitivity float64,
) {
	connID := value.ConnectionID

	// Check if z-score exceeds threshold. An in-band score includes
	// one under the value floor, which has no baseline.
	if !score.scored || score.inBand(sensitivity) {
		return
	}
	zScore := score.zScore
	baseline := score.baseline

	e.debugLog("Tier 1 anomaly detected: %s on connection %d (z-score: %.2f)",
		metricName, connID, zScore)

	// Create anomaly candidate for further processing
	candidate := &database.AnomalyCandidate{
		ConnectionID: connID,
		DatabaseName: value.DatabaseName,
		MetricName:   metricName,
		MetricValue:  value.Value,
		ZScore:       zScore,
		DetectedAt:   time.Now(),
		Context: fmt.Sprintf(
			`{"baseline_mean": %.2f, "baseline_stddev": %.2f, "effective_stddev": %.4f, "stddev_floored": %t, "period_type": "%s"}`,
			baseline.Mean, baseline.StdDev, score.effectiveStdDev, score.stddevFloored, baseline.PeriodType),
		Tier1Pass: true,
	}

	// A candidate that could never raise an alert would only be
	// written, marked processed by processTier2And3 without any tier
	// running, and later deleted, so a condition that persists under
	// an open alert, a blackout or a suppression would add a row per
	// value on every cycle (issue #577). processTier2And3 repeats the
	// checks for candidates created before one of them applied.
	if reason := e.anomalyAlertSkipReason(ctx, candidate); reason != "" {
		e.debugLog("Not recording anomaly candidate for %s on connection %d: %s",
			metricName, connID, reason)
		return
	}

	if err := e.datastore.CreateAnomalyCandidate(ctx, candidate); err != nil {
		e.log("ERROR: Failed to create anomaly candidate: %v", err)
	}
}

// processTier2And3 processes anomaly candidates through tier 2 and tier 3
func (e *Engine) processTier2And3(ctx context.Context) {
	// Candidates left behind while processing was unavailable, or by a
	// backlog, would otherwise raise alerts about values that may no
	// longer hold, so they are expired before the queue is read.
	e.expireStaleAnomalyCandidates(ctx)

	candidates, err := e.datastore.GetUnprocessedAnomalyCandidates(ctx, AnomalyCandidateBatchLimit)
	if err != nil {
		e.log("ERROR: Failed to get anomaly candidates: %v", err)
		return
	}

	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return
		}

		// A candidate whose metric no longer supports baselines was
		// written before the metric was excluded (GitHub issue #576).
		// Its z-score came from a baseline the sweep has since deleted,
		// so suppress it rather than raise an alert from stale scoring.
		if !database.SupportsBaselines(candidate.MetricName) {
			e.suppressUnsupportedCandidate(ctx, candidate)
			continue
		}

		// A reload during the pass can disable Tier 2 and Tier 3, and
		// the rest of the batch would then reach determineFinalDecision
		// with no tier result and default to an alert. Stop instead,
		// leaving the remaining candidates for a later re-enable or for
		// expiry (issue #581).
		cfg := e.getConfig()
		if !cfg.Anomaly.Enabled || !e.anomalyProcessingAvailable(cfg) {
			e.debugLog("Stopping Tier 2 and Tier 3 pass: anomaly processing is no longer available")
			return
		}

		// Checks that do not depend on the tier results run first, so
		// a condition that persists across cycles does not buy an
		// embedding and an LLM call on every cycle only for
		// createAnomalyAlert to discard the result (issue #568).
		if reason := e.anomalyAlertSkipReason(ctx, candidate); reason != "" {
			e.debugLog("Skipping Tier 2 and Tier 3 for candidate %d (%s on connection %d): %s",
				candidate.ID, candidate.MetricName, candidate.ConnectionID, reason)
			// With no tier after Tier 1 run, determineFinalDecision
			// records "alert", the same decision a candidate that
			// passes the tiers and is then discarded by
			// createAnomalyAlert is left with.
			e.determineFinalDecision(candidate)
			e.markCandidateProcessed(ctx, candidate)
			continue
		}

		var similarAnomalies []*database.SimilarAnomaly
		var embedding []float32

		sensitivity := cfg.Anomaly.Tier1.DefaultSensitivity

		// Tier 2: Embedding similarity
		if cfg.Anomaly.Tier2.Enabled && e.embeddingProvider != nil {
			embedding, similarAnomalies = e.processTier2(ctx, candidate)
		} else {
			// Skip Tier 2, pass through to Tier 3
			tier2Pass := true
			candidate.Tier2Pass = &tier2Pass
		}

		// Tier 3: LLM classification (only if Tier 2 passed or was skipped)
		if cfg.Anomaly.Tier3.Enabled && e.reasoningProvider != nil &&
			(candidate.Tier2Pass == nil || *candidate.Tier2Pass) {
			e.processTier3(ctx, candidate, similarAnomalies)
		}

		// Determine final decision
		e.determineFinalDecision(candidate)

		// If final decision is alert, create an alert record
		if candidate.FinalDecision != nil && *candidate.FinalDecision == "alert" {
			e.createAnomalyAlert(ctx, candidate, sensitivity)
		}

		// Store embedding if we have one
		if len(embedding) > 0 {
			if err := e.datastore.StoreAnomalyEmbedding(ctx, candidate.ID, embedding, e.embeddingProvider.ModelName()); err != nil {
				// Logged at default verbosity: with no model allow-list
				// this is where a model wider than the halfvec column
				// first shows up, and the operator needs to see it.
				e.log("ERROR: Failed to store embedding for candidate %d: %v", candidate.ID, err)
			}
		}

		e.markCandidateProcessed(ctx, candidate)
	}
}

// expireStaleAnomalyCandidates marks candidates that have waited longer
// than staleCandidateAge as processed with no final decision, so they
// raise no alert and the retention cleanup ages them out (issue #581).
// The count is logged at default verbosity whenever it is non-zero, so
// an expiry on live data is visible.
func (e *Engine) expireStaleAnomalyCandidates(ctx context.Context) {
	cutoff := time.Now().Add(-staleCandidateAge(e.getConfig()))
	expired, err := e.datastore.ExpireUnprocessedAnomalyCandidates(ctx, cutoff)
	if err != nil {
		e.log("ERROR: Failed to expire stale anomaly candidates: %v", err)
		return
	}
	if expired > 0 {
		e.log("Expired %d anomaly candidates left unprocessed since before %s",
			expired, cutoff.UTC().Format(time.RFC3339))
	}
}

// staleCandidateAge is how long a candidate may wait unprocessed before
// expireStaleAnomalyCandidates expires it.
//
// Expiry exists for candidates that nothing will ever process, and those
// gain nothing from being expired promptly, whereas expiring a genuine
// candidate that is merely queued behind a backlog would drop a transient
// anomaly without assessing it. The cut-off must therefore sit well
// beyond the slowest a healthy queue can drain. A pass processes up to
// AnomalyCandidateBatchLimit candidates in turn, and the dominant cost of
// each is the Tier 3 call, bounded by the Tier 3 timeout, so the
// worst-case pass is the batch limit times that timeout. The
// StaleCandidateSafetyFactor covers the Tier 2 embedding and database
// work around each call and a candidate queued behind more than one full
// batch. The timeout is used whether or not Tier 3 is enabled, which only
// lengthens the cut-off. StaleCandidateMinAge keeps the cut-off at an
// hour or more when the timeout is short. With the default 30-second
// timeout the cut-off is 100 x 30s x 3, 2.5 hours.
//
// A nil config, as an engine built for the retention cleanup alone has,
// uses DefaultTier3Timeout.
func staleCandidateAge(cfg *config.Config) time.Duration {
	var timeout time.Duration
	if cfg != nil {
		timeout = time.Duration(cfg.Anomaly.Tier3.TimeoutSeconds) * time.Second
	}
	if timeout <= 0 {
		timeout = DefaultTier3Timeout
	}
	return max(StaleCandidateMinAge,
		AnomalyCandidateBatchLimit*StaleCandidateSafetyFactor*timeout)
}

// markCandidateProcessed stamps the candidate as processed and writes its
// tier results, decision and alert link back to anomaly_candidates.
func (e *Engine) markCandidateProcessed(ctx context.Context, candidate *database.AnomalyCandidate) {
	now := time.Now()
	candidate.ProcessedAt = &now

	if err := e.datastore.UpdateAnomalyCandidate(ctx, candidate); err != nil {
		e.log("ERROR: Failed to update anomaly candidate: %v", err)
	}
}

// suppressUnsupportedCandidate marks a candidate for a metric that no
// longer supports baselines as processed and suppressed, without running
// Tier 2 or Tier 3 or creating an alert.
func (e *Engine) suppressUnsupportedCandidate(ctx context.Context, candidate *database.AnomalyCandidate) {
	e.debugLog("Suppressing candidate %d: metric %s no longer supports baselines",
		candidate.ID, candidate.MetricName)
	decision := "suppress"
	candidate.FinalDecision = &decision
	e.markCandidateProcessed(ctx, candidate)
}

// processTier2 handles Tier 2 embedding similarity processing
func (e *Engine) processTier2(ctx context.Context, candidate *database.AnomalyCandidate) ([]float32, []*database.SimilarAnomaly) {
	e.debugLog("Tier 2: Processing candidate %d for metric %s", candidate.ID, candidate.MetricName)

	// Build the context text for embedding
	contextText := e.buildContextText(candidate)

	// Generate embedding
	embedding, err := e.embeddingProvider.GenerateEmbedding(ctx, contextText)
	if err != nil {
		e.log("ERROR: Failed to generate embedding for candidate %d: %v", candidate.ID, err)
		// On embedding failure, pass through to Tier 3
		tier2Pass := true
		candidate.Tier2Pass = &tier2Pass
		return nil, nil
	}

	// Search for similar past anomalies
	cfg := e.getConfig()
	threshold := cfg.Anomaly.Tier2.SimilarityThreshold
	if threshold <= 0 {
		threshold = 0.3 // Default minimum similarity
	}

	similarAnomalies, err := e.datastore.FindSimilarAnomalies(ctx, embedding, candidate.ID, threshold, 10)
	if err != nil {
		e.log("ERROR: Failed to find similar anomalies for candidate %d: %v", candidate.ID, err)
		// On search failure, pass through to Tier 3
		tier2Pass := true
		candidate.Tier2Pass = &tier2Pass
		return embedding, nil
	}

	// Analyze similar anomalies
	if len(similarAnomalies) > 0 {
		// Find the highest similarity score
		var maxSimilarity float64
		var suppressCount, alertCount int

		for _, sa := range similarAnomalies {
			if sa.Similarity > maxSimilarity {
				maxSimilarity = sa.Similarity
			}
			if sa.FinalDecision != nil {
				switch *sa.FinalDecision {
				case "suppress", "suppressed", "false_positive":
					suppressCount++
				case "alert", "anomaly":
					alertCount++
				}
			}
		}

		candidate.Tier2Score = &maxSimilarity

		// Apply suppression logic based on similar anomalies
		suppressionThreshold := cfg.Anomaly.Tier2.SuppressionThreshold
		if suppressionThreshold <= 0 {
			suppressionThreshold = 0.85 // Default high similarity threshold for suppression
		}

		if maxSimilarity >= suppressionThreshold && suppressCount > alertCount {
			// High similarity to suppressed anomalies -> suppress this one too
			tier2Pass := false
			candidate.Tier2Pass = &tier2Pass
			e.debugLog("Tier 2: Suppressing candidate %d (similarity %.2f to %d suppressed anomalies)",
				candidate.ID, maxSimilarity, suppressCount)
		} else if maxSimilarity >= suppressionThreshold && alertCount > suppressCount {
			// High similarity to real anomalies -> this is likely a real issue
			tier2Pass := true
			candidate.Tier2Pass = &tier2Pass
			e.debugLog("Tier 2: Passing candidate %d (similarity %.2f to %d alerted anomalies)",
				candidate.ID, maxSimilarity, alertCount)
		} else {
			// Low similarity or mixed results -> needs LLM review
			tier2Pass := true
			candidate.Tier2Pass = &tier2Pass
			e.debugLog("Tier 2: Passing candidate %d to Tier 3 for review (similarity %.2f)",
				candidate.ID, maxSimilarity)
		}
	} else {
		// No similar anomalies found -> needs LLM review
		tier2Pass := true
		candidate.Tier2Pass = &tier2Pass
		score := 0.0
		candidate.Tier2Score = &score
		e.debugLog("Tier 2: No similar anomalies found for candidate %d, passing to Tier 3",
			candidate.ID)
	}

	return embedding, similarAnomalies
}

// processTier3 handles Tier 3 LLM classification
func (e *Engine) processTier3(ctx context.Context, candidate *database.AnomalyCandidate, similarAnomalies []*database.SimilarAnomaly) {
	e.debugLog("Tier 3: Processing candidate %d with LLM", candidate.ID)

	// Fetch acknowledgement history for this metric+connection to inform the LLM
	var ackHistory []*database.AcknowledgedAnomalyAlert
	ackHistory, err := e.datastore.GetAcknowledgmentHistoryForMetric(ctx, candidate.MetricName, candidate.ConnectionID, 0, 10)
	if err != nil {
		e.debugLog("Tier 3: Failed to fetch ack history for candidate %d: %v", candidate.ID, err)
	}

	// Fetch cluster context for the LLM prompt
	clusterPeers, err := e.datastore.GetClusterPeers(ctx, candidate.ConnectionID)
	if err != nil {
		e.debugLog("Tier 3: Failed to fetch cluster peers for candidate %d: %v", candidate.ID, err)
		clusterPeers = nil
	}
	clusterAlerts, err := e.datastore.GetAlertsByCluster(ctx, candidate.ConnectionID)
	if err != nil {
		e.debugLog("Tier 3: Failed to fetch cluster alerts for candidate %d: %v", candidate.ID, err)
		clusterAlerts = nil
	}

	// Build the classification prompt
	prompt := e.buildClassificationPrompt(candidate, similarAnomalies, ackHistory, clusterPeers, clusterAlerts)

	// Create a timeout context for Tier 3
	cfg := e.getConfig()
	timeout := time.Duration(cfg.Anomaly.Tier3.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = DefaultTier3Timeout
	}
	tier3Ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Call the LLM for classification
	response, err := e.reasoningProvider.Classify(tier3Ctx, prompt)
	if err != nil {
		e.log("ERROR: Tier 3 LLM classification failed for candidate %d: %v", candidate.ID, err)
		errStr := err.Error()
		candidate.Tier3Error = &errStr
		// On LLM failure, default to alert (fail safe)
		tier3Pass := true
		candidate.Tier3Pass = &tier3Pass
		result := "LLM classification failed, defaulting to alert"
		candidate.Tier3Result = &result
		return
	}

	// Store the raw response
	candidate.Tier3Result = &response

	// Parse the LLM response
	decision, _ := e.parseLLMResponse(response)

	switch strings.ToLower(decision) {
	case "alert", "anomaly":
		tier3Pass := true
		candidate.Tier3Pass = &tier3Pass
		e.debugLog("Tier 3: LLM classified candidate %d as ALERT", candidate.ID)
	case "suppress", "suppressed", "false_positive":
		tier3Pass := false
		candidate.Tier3Pass = &tier3Pass
		e.debugLog("Tier 3: LLM classified candidate %d as SUPPRESS", candidate.ID)
	default:
		// Unknown response, default to alert
		tier3Pass := true
		candidate.Tier3Pass = &tier3Pass
		e.debugLog("Tier 3: Unknown LLM response for candidate %d, defaulting to alert", candidate.ID)
	}
}

// determineFinalDecision sets the final decision based on tier results
func (e *Engine) determineFinalDecision(candidate *database.AnomalyCandidate) {
	// If Tier 2 explicitly suppressed, suppress the anomaly
	if candidate.Tier2Pass != nil && !*candidate.Tier2Pass {
		decision := "suppress"
		candidate.FinalDecision = &decision
		return
	}

	// If Tier 3 was run, use its decision
	if candidate.Tier3Pass != nil {
		if *candidate.Tier3Pass {
			decision := "alert"
			candidate.FinalDecision = &decision
		} else {
			decision := "suppress"
			candidate.FinalDecision = &decision
		}
		return
	}

	// If only Tier 2 passed, treat as anomaly
	if candidate.Tier2Pass != nil && *candidate.Tier2Pass {
		decision := "alert"
		candidate.FinalDecision = &decision
		return
	}

	// Default to anomaly (Tier 1 already passed)
	decision := "alert"
	candidate.FinalDecision = &decision
}

// anomalyAlertSkipReason runs the checks that stop an alert being raised
// for the candidate and that do not depend on the Tier 2 or Tier 3
// results: an active blackout, an open (active or acknowledged) anomaly
// alert for the same metric, connection and database, a recent
// re-evaluation clear, and a recent false-positive acknowledgment. It
// returns a short reason when one applies and "" otherwise. When an open
// alert exists, candidate.AlertID is set to it so the candidate is
// recorded against that alert.
//
// It runs three times over a candidate's life: in detectAnomalyForValue,
// before the candidate is created (issue #577); in processTier2And3,
// before the paid tiers (issue #568); and in createAnomalyAlert, before
// the alert is written.
//
// The order is significant: an acknowledged alert marked as a false
// positive is also an open alert, so the duplicate check claims it first
// and links the candidate to it. A lookup error is logged and treated as
// "does not apply", so a failing query never hides a real anomaly.
func (e *Engine) anomalyAlertSkipReason(ctx context.Context, candidate *database.AnomalyCandidate) string {
	connID := candidate.ConnectionID
	active, err := e.datastore.IsBlackoutActive(ctx, &connID, candidate.DatabaseName)
	if err != nil {
		e.debugLog("Error checking blackout for connection %d: %v", connID, err)
	}
	if active {
		e.debugLog("Skipping anomaly alert for connection %d: blackout active", connID)
		return "blackout active"
	}

	existing, err := e.datastore.GetActiveAnomalyAlert(ctx, candidate.MetricName, candidate.ConnectionID, candidate.DatabaseName)
	if err == nil && existing != nil {
		e.debugLog("Active anomaly alert already exists for %s on connection %d (alert %d), skipping",
			candidate.MetricName, candidate.ConnectionID, existing.ID)
		candidate.AlertID = &existing.ID
		return fmt.Sprintf("open anomaly alert %d", existing.ID)
	}

	// Tier 1 recovery just cleared an alert on this series; give the
	// metric the same cooldown a cleared threshold alert gets, so a
	// value hovering at the edge of the band does not fire, clear and
	// re-fire (with a paid Tier 2/3 classification each time).
	recent, err := e.datastore.GetRecentlyClearedAnomalyAlert(ctx, candidate.MetricName, candidate.ConnectionID, candidate.DatabaseName, AlertCooldownPeriod)
	if err != nil {
		e.debugLog("Error checking recently cleared anomaly alert for %s on connection %d: %v", candidate.MetricName, candidate.ConnectionID, err)
	} else if recent {
		e.debugLog("Skipping anomaly alert %s on connection %d: recently cleared (cooldown)", candidate.MetricName, candidate.ConnectionID)
		return "recently cleared anomaly alert (cooldown)"
	}

	// Re-evaluation previously cleared this alert based on user
	// feedback; use the longer suppression window to respect the
	// user's assessment.
	suppressed, err := e.datastore.GetReevaluationSuppressedAlert(ctx, candidate.MetricName, candidate.ConnectionID, candidate.DatabaseName, ReevaluationSuppressionPeriod)
	if err != nil {
		e.debugLog("Error checking re-evaluation suppression for %s on connection %d: %v", candidate.MetricName, candidate.ConnectionID, err)
	} else if suppressed {
		e.debugLog("Skipping anomaly alert %s on connection %d: suppressed by re-evaluation feedback", candidate.MetricName, candidate.ConnectionID)
		return "suppressed by re-evaluation feedback"
	}

	// The user acknowledged a similar alert as a false positive;
	// respect that for the same suppression period.
	fpSuppressed, err := e.datastore.GetFalsePositiveSuppressedAlert(ctx, candidate.MetricName, candidate.ConnectionID, candidate.DatabaseName, ReevaluationSuppressionPeriod)
	if err != nil {
		e.debugLog("Error checking false positive suppression for %s on connection %d: %v", candidate.MetricName, candidate.ConnectionID, err)
	} else if fpSuppressed {
		e.debugLog("Skipping anomaly alert %s on connection %d: suppressed by user false positive acknowledgment", candidate.MetricName, candidate.ConnectionID)
		return "suppressed by false positive acknowledgment"
	}

	return ""
}

// createAnomalyAlert creates an alert record for a confirmed anomaly candidate.
// processTier2And3 has already run anomalyAlertSkipReason before the paid
// tiers; it runs again here because Tier 3 can take up to its timeout, in
// which time a blackout may start or a user may acknowledge an alert, and
// an alert must not be raised against either.
func (e *Engine) createAnomalyAlert(ctx context.Context, candidate *database.AnomalyCandidate, sensitivity float64) {
	if e.anomalyAlertSkipReason(ctx, candidate) != "" {
		return
	}

	severity := cappedAnomalySeverity(candidate.ZScore, sensitivity,
		candidateStdDevFloored(candidate.Context))

	// Build anomaly details from tier results
	anomalyDetails := fmt.Sprintf(
		`{"z_score": %.2f, "baseline_context": %s, "tier2_score": %s, "tier3_result": %s}`,
		candidate.ZScore,
		candidate.Context,
		formatOptionalFloat(candidate.Tier2Score),
		formatOptionalString(candidate.Tier3Result),
	)

	title := candidate.MetricName
	description := fmt.Sprintf(
		"Statistical anomaly detected for metric %s (value: %.4f, z-score: %.2f).",
		candidate.MetricName, candidate.MetricValue, candidate.ZScore,
	)

	alert := &database.Alert{
		AlertType:      "anomaly",
		ConnectionID:   candidate.ConnectionID,
		DatabaseName:   candidate.DatabaseName,
		MetricName:     &candidate.MetricName,
		MetricValue:    &candidate.MetricValue,
		AnomalyScore:   &candidate.ZScore,
		AnomalyDetails: &anomalyDetails,
		Severity:       severity,
		Title:          title,
		Description:    description,
		Status:         "active",
		TriggeredAt:    time.Now(),
	}

	if err := e.datastore.CreateAlert(ctx, alert); err != nil {
		e.log("ERROR: Failed to create anomaly alert for candidate %d: %v", candidate.ID, err)
		return
	}

	candidate.AlertID = &alert.ID
	e.log("Anomaly alert created: %s (z-score: %.2f, severity: %s)", title, candidate.ZScore, severity)

	// Queue alert notification for async processing
	e.queueNotification(alert, database.NotificationTypeAlertFire)
}

// candidateStdDevFloored reports whether a candidate's z-score was
// computed with a floored divisor, from the stddev_floored key that
// recordAnomalyCandidate writes into its context. A candidate written
// before that key existed, or with an unreadable context, reports false,
// which keeps the severity it would have had before issue #617.
func candidateStdDevFloored(contextJSON string) bool {
	var ctxData struct {
		StdDevFloored bool `json:"stddev_floored"`
	}
	if err := json.Unmarshal([]byte(contextJSON), &ctxData); err != nil {
		return false
	}
	return ctxData.StdDevFloored
}

// formatOptionalFloat formats a *float64 as a JSON value string.
func formatOptionalFloat(v *float64) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%.4f", *v)
}

// formatOptionalString formats a *string as a JSON-quoted value string.
func formatOptionalString(v *string) string {
	if v == nil {
		return "null"
	}
	// Use JSON marshaling to safely escape the string
	b, err := json.Marshal(*v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// buildContextText builds a text representation of the anomaly for embedding
func (e *Engine) buildContextText(candidate *database.AnomalyCandidate) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Metric: %s\n", candidate.MetricName)
	fmt.Fprintf(&sb, "Value: %.4f\n", candidate.MetricValue)
	fmt.Fprintf(&sb, "Z-Score: %.2f\n", candidate.ZScore)
	fmt.Fprintf(&sb, "Connection ID: %d\n", candidate.ConnectionID)

	if candidate.DatabaseName != nil {
		fmt.Fprintf(&sb, "Database: %s\n", *candidate.DatabaseName)
	}

	fmt.Fprintf(&sb, "Detected at: %s\n", candidate.DetectedAt.Format(time.RFC3339))
	fmt.Fprintf(&sb, "Context: %s\n", candidate.Context)

	return sb.String()
}

// buildClassificationPrompt builds the prompt for LLM classification
func (e *Engine) buildClassificationPrompt(
	candidate *database.AnomalyCandidate,
	similarAnomalies []*database.SimilarAnomaly,
	ackHistory []*database.AcknowledgedAnomalyAlert,
	clusterPeers []*database.ClusterPeerInfo,
	clusterAlerts []*database.Alert,
) string {
	var sb strings.Builder

	sb.WriteString("Analyze the following anomaly candidate and determine if it is a real issue that requires attention (alert) or a false positive that should be suppressed.\n\n")

	sb.WriteString("## Current Anomaly\n")
	fmt.Fprintf(&sb, "- Metric: %s\n", candidate.MetricName)
	fmt.Fprintf(&sb, "- Value: %.4f\n", candidate.MetricValue)
	fmt.Fprintf(&sb, "- Z-Score: %.2f (standard deviations from mean)\n", candidate.ZScore)
	fmt.Fprintf(&sb, "- Connection ID: %d\n", candidate.ConnectionID)

	if candidate.DatabaseName != nil {
		fmt.Fprintf(&sb, "- Database: %s\n", *candidate.DatabaseName)
	}

	fmt.Fprintf(&sb, "- Detected at: %s\n", candidate.DetectedAt.Format(time.RFC3339))

	// Parse and include baseline info from context
	var contextData map[string]any
	if err := json.Unmarshal([]byte(candidate.Context), &contextData); err == nil {
		if mean, ok := contextData["baseline_mean"].(float64); ok {
			fmt.Fprintf(&sb, "- Baseline mean: %.4f\n", mean)
		}
		if stddev, ok := contextData["baseline_stddev"].(float64); ok {
			fmt.Fprintf(&sb, "- Baseline stddev: %.4f\n", stddev)
		}
		if effective, ok := contextData["effective_stddev"].(float64); ok {
			floored := contextData["stddev_floored"] == true
			fmt.Fprintf(&sb, "- Z-score divisor: %.4f (raised to a minimum floor: %t)\n", effective, floored)
		}
		if periodType, ok := contextData["period_type"].(string); ok {
			fmt.Fprintf(&sb, "- Baseline period: %s\n", periodType)
		}
	}

	// Include similar past anomalies if available
	if len(similarAnomalies) > 0 {
		sb.WriteString("\n## Similar Past Anomalies\n")
		for i, sa := range similarAnomalies {
			if i >= 5 {
				fmt.Fprintf(&sb, "... and %d more similar anomalies\n", len(similarAnomalies)-5)
				break
			}
			decision := "unknown"
			if sa.FinalDecision != nil {
				decision = *sa.FinalDecision
			}
			fmt.Fprintf(&sb, "- Similarity: %.2f%%, Decision: %s, Metric: %s\n",
				sa.Similarity*100, decision, sa.MetricName)
		}
	} else {
		sb.WriteString("\n## Similar Past Anomalies\nNo similar past anomalies found.\n")
	}

	// Include past user feedback if available
	if len(ackHistory) > 0 {
		sb.WriteString("\n## Past User Feedback\n")
		sb.WriteString("Users have previously acknowledged alerts for this same metric and server:\n")
		for _, ack := range ackHistory {
			if ack.AckMessage != nil && *ack.AckMessage != "" {
				fmt.Fprintf(&sb, "- Note: %s", *ack.AckMessage)
			} else {
				sb.WriteString("- (no message)")
			}
			if ack.FalsePositive {
				sb.WriteString(" [MARKED AS FALSE POSITIVE]")
			}
			if ack.AcknowledgedBy != nil {
				fmt.Fprintf(&sb, " (by %s", *ack.AcknowledgedBy)
				if ack.AcknowledgedAt != nil {
					fmt.Fprintf(&sb, " at %s", ack.AcknowledgedAt.Format(time.RFC3339))
				}
				sb.WriteString(")")
			}
			sb.WriteString("\n")
			if ack.MetricValue != nil {
				fmt.Fprintf(&sb, "  Original alert: value=%.4f", *ack.MetricValue)
			}
			if ack.ZScore != nil {
				fmt.Fprintf(&sb, ", z-score=%.2f", *ack.ZScore)
			}
			fmt.Fprintf(&sb, ", severity=%s\n", ack.Severity)
		}
	}

	writeClusterContext(&sb, candidate.ConnectionID, clusterPeers, clusterAlerts)

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Consider any past user feedback, but evaluate whether it still applies. If the current anomaly is significantly more severe than when the user dismissed it (e.g., much higher z-score or value), the alert should still fire. Past feedback suggests context, not a blanket rule.\n")
	sb.WriteString("Based on the above information, respond with a JSON object containing:\n")
	sb.WriteString("- \"decision\": either \"alert\" (real issue) or \"suppress\" (false positive)\n")
	sb.WriteString("- \"confidence\": a number from 0 to 1\n")
	sb.WriteString("- \"reasoning\": a brief explanation\n")

	return sb.String()
}

// writeClusterContext appends a "Cluster Context" section to the prompt
// if the connection belongs to a replication cluster with peers.
func writeClusterContext(
	sb *strings.Builder,
	connectionID int,
	clusterPeers []*database.ClusterPeerInfo,
	clusterAlerts []*database.Alert,
) {
	if len(clusterPeers) == 0 {
		return
	}

	fmt.Fprintf(sb, "\n## Cluster Context\n")
	fmt.Fprintf(sb, "This server belongs to a cluster with %d other node(s):\n", len(clusterPeers))

	// Build a lookup map from connection ID to peer name
	peerNames := make(map[int]string, len(clusterPeers))
	for _, peer := range clusterPeers {
		fmt.Fprintf(sb, "- %s (%s)\n", peer.ConnectionName, peer.NodeRole)
		peerNames[peer.ConnectionID] = peer.ConnectionName
	}

	if len(clusterAlerts) > 0 {
		sb.WriteString("\nActive alerts on cluster peers:\n")
		for _, alert := range clusterAlerts {
			name := peerNames[alert.ConnectionID]
			if name == "" {
				name = fmt.Sprintf("connection %d", alert.ConnectionID)
			}
			fmt.Fprintf(sb, "- %s (severity: %s, server: %s, triggered: %s)\n",
				alert.Title, alert.Severity, name,
				alert.TriggeredAt.Format(time.RFC3339))
		}
	} else {
		sb.WriteString("\nNo active alerts on cluster peers.\n")
	}
}

// parseLLMResponse parses the LLM response to extract the decision
func (e *Engine) parseLLMResponse(response string) (string, float64) {
	return parseLLMDecision(response, anomalyDecisionConfig)
}
