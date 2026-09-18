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
	"fmt"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// cleanResolvedAlerts clears alerts where the condition has resolved
func (e *Engine) cleanResolvedAlerts(ctx context.Context) {
	e.debugLog("Checking for resolved alerts...")

	// Get all active threshold alerts
	alerts, err := e.datastore.GetActiveAlerts(ctx)
	if err != nil {
		e.log("ERROR: Failed to get active alerts: %v", err)
		return
	}

	// Rules with a required_extension are gated on the connections whose
	// newest pg_extension snapshot lists it. Resolve that set once per
	// rule for the whole pass, as the evaluator does, rather than once
	// per alert.
	gates := e.resolveExtensionGates(ctx, alerts)

	// Probe staleness is a single snapshot of every reporting probe, and
	// both the staleness alerts and the absent-metric gate read the same
	// one. Resolve it at most once for the pass rather than once per
	// alert, lazily so that a pass which needs it never reads it.
	staleness := &probeStalenessSnapshot{}

	for _, alert := range alerts {
		if ctx.Err() != nil {
			return
		}

		if alert.AlertType == "threshold" && alert.RuleID != nil {
			e.checkAlertResolved(ctx, alert, gates[*alert.RuleID], staleness)
		}
	}
}

// probeStalenessSnapshot holds one cleanup pass's view of which probes
// are reporting, read on first use and reused for every alert after
// that. A failed read is remembered too: retrying it once per alert
// would hammer a datastore that is already unwell, and every caller
// treats the failure the same way, by leaving the alert active.
//
// The entries are measured against the datastore's NOW() at the moment
// they were read, and a pass can take long enough for that to matter: a
// probe 4m58s late when the snapshot loads is 5m08s late for an alert
// judged ten seconds on, which is outside a five minute window rather
// than inside it. Every judgement therefore adds the snapshot's age, so
// the effective window stays the metric's own rather than the window
// plus however long the pass has been running. See GitHub issue #407.
type probeStalenessSnapshot struct {
	entries []database.ProbeStaleness
	err     error
	loaded  bool

	// readAt is taken before the datastore read so that the age errs
	// towards treating a probe as later than it is, never earlier.
	readAt time.Time

	// now is the clock the snapshot ages by; nil means time.Now. Tests
	// set it to move the judgement away from the read.
	now func() time.Time
}

// get returns the pass's probe staleness entries, reading them through
// the engine's datastore the first time it is called.
func (s *probeStalenessSnapshot) get(ctx context.Context, e *Engine) ([]database.ProbeStaleness, error) {
	if !s.loaded {
		s.readAt = s.clock()
		s.entries, s.err = e.datastore.GetProbeStalenessByConnection(ctx)
		s.loaded = true
	}
	return s.entries, s.err
}

// age is how long ago the entries were read, and so how much later than
// their SinceCollected every probe now is. An unloaded snapshot has no
// age.
func (s *probeStalenessSnapshot) age() time.Duration {
	if !s.loaded {
		return 0
	}
	return s.clock().Sub(s.readAt)
}

func (s *probeStalenessSnapshot) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// currentStalenessRatio brings a snapshot entry's ratio forward by the
// snapshot's age, so that a staleness alert is judged against how late
// the probe is now rather than how late it was when the pass began. A
// non-positive interval cannot occur, because the staleness query
// divides by it, but the frozen ratio is returned rather than dividing
// by zero here.
func currentStalenessRatio(entry database.ProbeStaleness, age time.Duration) float64 {
	if entry.CollectionInterval <= 0 {
		return entry.StalenessRatio
	}
	return (entry.SinceCollected + age).Seconds() / float64(entry.CollectionInterval)
}

// resolveExtensionGates maps the rule id of each active threshold alert
// to the set of connections on which that rule may be evaluated, or to
// nil when the rule has no required_extension and no gate applies.
//
// Each rule is loaded once per cleanup pass and each extension resolved
// once, so a pass costs one GetAlertRuleByID per distinct rule and one
// GetConnectionsWithExtension per distinct extension however many alerts
// are active. A rule that cannot be loaded, or an extension that cannot
// be resolved, is logged and left ungated: a datastore fault must not
// pin a whole class of alerts open. See GitHub issue #409.
func (e *Engine) resolveExtensionGates(ctx context.Context, alerts []*database.Alert) map[int64]map[int]bool {
	gates := make(map[int64]map[int]bool)
	byExtension := make(map[string]map[int]bool)

	for _, alert := range alerts {
		if alert.AlertType != "threshold" || alert.RuleID == nil {
			continue
		}
		if _, seen := gates[*alert.RuleID]; seen {
			continue
		}
		gates[*alert.RuleID] = nil

		rule, err := e.datastore.GetAlertRuleByID(ctx, *alert.RuleID)
		if err != nil {
			e.log("ERROR: Failed to load rule %d for alert %d, checking resolution without the extension gate: %v",
				*alert.RuleID, alert.ID, err)
			continue
		}
		if rule.RequiredExtension == nil || *rule.RequiredExtension == "" {
			continue
		}

		ext := *rule.RequiredExtension
		if set, resolved := byExtension[ext]; resolved {
			gates[*alert.RuleID] = set
			continue
		}
		set := e.connectionsWithRequiredExtension(ctx, rule)
		byExtension[ext] = set
		gates[*alert.RuleID] = set
	}

	return gates
}

// checkAlertResolved checks if a threshold alert's condition has
// resolved. withExtension is the set of connections on which the alert's
// rule may be evaluated, as resolved by resolveExtensionGates, or nil
// when no extension gate applies. staleness carries the cleanup pass's
// probe staleness snapshot, and may be nil for a caller checking a
// single alert outside a pass.
func (e *Engine) checkAlertResolved(ctx context.Context, alert *database.Alert,
	withExtension map[int]bool, staleness *probeStalenessSnapshot) {
	if staleness == nil {
		staleness = &probeStalenessSnapshot{}
	}
	if alert.MetricName == nil || alert.ThresholdValue == nil || alert.Operator == nil {
		return
	}

	// Probe-scoped alerts (metric_staleness) are raised by a bespoke
	// evaluator and their metric has no registry entry, so they need a
	// bespoke resolution check too.
	if alert.ProbeName != nil {
		e.checkStalenessAlertResolved(ctx, alert, staleness)
		return
	}

	// An alert whose rule needs an extension the connection no longer
	// reports is not resolved: the condition may well persist, the
	// alerter simply cannot see it any more. Leave it active rather than
	// clearing it. The trade-off is that an operator who uninstalls the
	// extension keeps the alert until they acknowledge or clear it, which
	// is preferable to silently reporting the condition resolved. See
	// GitHub issue #409.
	if withExtension != nil && !withExtension[alert.ConnectionID] {
		e.debugLog("Leaving alert %d active: connection %d does not report the extension its rule requires",
			alert.ID, alert.ConnectionID)
		return
	}

	// Get current metric values for all connections/databases
	values, err := e.datastore.GetLatestMetricValues(ctx, *alert.MetricName)
	if err != nil {
		if errors.Is(err, database.ErrNoMetricData) {
			// The query ran and reported nothing for any connection.
			e.resolveAbsentMetric(ctx, alert, "no connection reports it", staleness)
			return
		}
		// The metric could not be evaluated at all: either it has no
		// registry entry or the query failed. Neither says anything
		// about whether the condition still holds, so leave the alert
		// alone rather than clearing it and letting the evaluator
		// re-raise it on its next pass.
		e.log("ERROR: Cannot evaluate metric %s for alert %d, leaving it active: %v",
			*alert.MetricName, alert.ID, err)
		return
	}

	// Find the metric value matching this alert's connection and database
	var found bool
	var value float64
	for _, mv := range values {
		if mv.ConnectionID != alert.ConnectionID {
			continue
		}
		// Match database name if the alert has one
		if alert.DatabaseName != nil {
			if mv.DatabaseName == nil || *mv.DatabaseName != *alert.DatabaseName {
				continue
			}
		}
		found = true
		value = mv.Value
		break
	}

	if !found {
		e.resolveAbsentMetric(ctx, alert,
			"no row for this connection and database", staleness)
		return
	}

	// Check if threshold is still violated
	stillViolated := e.checkThreshold(value, *alert.Operator, *alert.ThresholdValue)
	if !stillViolated {
		e.clearResolvedAlert(ctx, alert, value)
	}
}

// checkStalenessAlertResolved checks whether a probe-scoped staleness alert
// has resolved. The metric_staleness rule is evaluated by
// evaluateMetricStaleness rather than through metricRegistry, so the
// resolution check reads the same probe_availability source the evaluator
// does. The alert clears when the probe's staleness ratio no longer violates
// the threshold stored on the alert, or when the probe stops being reported
// at all because it was disabled or its connection is no longer monitored.
//
// A probe that has gone unavailable is the case the check must not treat
// as a resolution. The probe is still configured and its connection is
// still monitored, but collection has stopped, so the metric queries
// behind the condition alert go quiet at the same moment; clearing the
// staleness alert then would retire the alert meant to report that the
// alerter has gone blind, exactly as the condition alert it was watching
// went quiet. Such an alert is held open and its description rewritten to
// say why, whilst its title stays as it was so that it reads as the same
// alert across notification history; the description is put back once the
// probe collects again, so that the alert does not go on to clear in the
// words of the hold. See GitHub issue #465.
func (e *Engine) checkStalenessAlertResolved(ctx context.Context, alert *database.Alert,
	staleness *probeStalenessSnapshot) {
	entries, err := staleness.get(ctx, e)
	if err != nil {
		e.log("ERROR: Failed to get probe staleness for alert %d: %v", alert.ID, err)
		return
	}

	for _, entry := range entries {
		if entry.ConnectionID != alert.ConnectionID || entry.ProbeName != *alert.ProbeName {
			continue
		}
		if !entry.IsAvailable {
			e.holdStalenessAlertForUnavailableProbe(ctx, alert, entry)
			return
		}
		ratio := currentStalenessRatio(entry, staleness.age())

		// The probe is collecting again, so a description written whilst
		// it was unavailable is now wrong, and wrong in the text the
		// clear notification and the stored alert history would carry.
		// Put the staleness wording back before deciding whether the
		// alert clears.
		e.restoreStalenessAlertDescription(ctx, alert, entry, ratio)

		if !e.checkThreshold(ratio, *alert.Operator, *alert.ThresholdValue) {
			e.clearResolvedAlert(ctx, alert, ratio)
		}
		return
	}

	// The probe no longer appears in the staleness view at all, which now
	// means only deliberate operator action: the probe was disabled, the
	// connection is no longer monitored, or the availability row itself
	// has gone. There is nothing left to be stale about, so the alert
	// clears, but at normal log level rather than debug so that an
	// operator can see which of their changes retired the alert.
	e.log("Probe %s on connection %d is no longer reported; clearing alert %d",
		*alert.ProbeName, alert.ConnectionID, alert.ID)
	e.clearResolvedAlert(ctx, alert, 0)
}

// holdStalenessAlertForUnavailableProbe leaves a staleness alert active
// for a probe whose availability has gone false, and records why in the
// alert's description so an operator reading the alert learns that
// collection has stopped rather than that the probe is merely late.
//
// Both the rewrite and the line announcing it are conditional on the
// text actually changing, because a cleanup pass runs on every cycle:
// writing the same text again would bump last_updated for as long as the
// probe stayed unavailable, and logging it again would put a line in the
// operator's log every thirty seconds, in both cases for no new
// information. The operator therefore gets one line when collection
// stops and another only if the reason changes, in the same spirit as
// resolveAbsentMetric, which reports each verdict once per pass rather
// than once per probe. A failure to write the description is logged and
// otherwise ignored, since the alert staying active matters more than
// its wording; the next pass retries it, because alert.Description is
// only advanced once the write has succeeded.
func (e *Engine) holdStalenessAlertForUnavailableProbe(ctx context.Context,
	alert *database.Alert, entry database.ProbeStaleness) {
	reason := unavailableProbeReason(entry)

	description := unavailableProbeDescription(entry, reason)
	if description == alert.Description {
		return
	}

	e.log("Probe %s on connection %d is unavailable (%s); leaving staleness alert %d active",
		entry.ProbeName, alert.ConnectionID, reason, alert.ID)

	if err := e.datastore.UpdateAlertDescription(ctx, alert.ID, description); err != nil {
		e.log("ERROR: Failed to update description of alert %d: %v", alert.ID, err)
		return
	}
	alert.Description = description
}

// unavailableProbeReason is whatever the collector recorded against the
// probe, or a stand-in when it recorded nothing, so that neither the log
// line nor the description has a hole in it.
func unavailableProbeReason(entry database.ProbeStaleness) string {
	if entry.UnavailableReason != nil && *entry.UnavailableReason != "" {
		return *entry.UnavailableReason
	}
	return "no reason recorded"
}

// unavailableProbeDescriptionPrefix opens every description the cleaner
// writes for a held alert, and is how the cleaner later tells its own
// wording apart: the alert row carries no record of what it said when it
// was raised, so the prefix is what distinguishes a description this code
// wrote from the evaluator's own.
const unavailableProbeDescriptionPrefix = "Collection has stopped: "

// unavailableProbeDescription is the text a held staleness alert carries
// whilst its probe is unavailable. It is deterministic, with nothing in it
// that moves between passes, so that the idempotence check above compares
// like with like.
func unavailableProbeDescription(entry database.ProbeStaleness, reason string) string {
	return fmt.Sprintf(
		unavailableProbeDescriptionPrefix+
			"the %s probe on %s is no longer available (%s). "+
			"Metrics from this probe are neither arriving nor being evaluated, so "+
			"conditions it reports on cannot be seen. This alert stays active until "+
			"the probe collects again, or until the probe or connection is disabled.",
		entry.ProbeName, entry.ConnectionName, reason)
}

// restoreStalenessAlertDescription undoes the hold once the probe is
// collecting again. Without it the alert would keep the wording of the
// hold for the rest of its life, so the clear notification and the stored
// history would both announce the resolution in the words "Collection has
// stopped ... This alert stays active until the probe collects again",
// which is precisely untrue at that point.
//
// Only a description the cleaner itself wrote is replaced, matched on
// its prefix, so an alert the evaluator worded is left alone and the write
// happens once on recovery rather than on every pass. The replacement is
// the evaluator's own wording, rebuilt from the ratio recorded on the
// alert, which is the measurement the alert was raised or last updated on
// and so reproduces the text the evaluator would have written; the current
// ratio stands in if the alert carries no value. A failed write leaves the
// held text in place and is retried next pass, as the hold's own write is.
func (e *Engine) restoreStalenessAlertDescription(ctx context.Context,
	alert *database.Alert, entry database.ProbeStaleness, ratio float64) {
	if !strings.HasPrefix(alert.Description, unavailableProbeDescriptionPrefix) {
		return
	}
	if alert.MetricValue != nil {
		ratio = *alert.MetricValue
	}

	description := stalenessAlertDescription(entry.ProbeName, entry.ConnectionName,
		stalenessMinutes(entry.CollectionInterval, ratio),
		stalenessMinutes(entry.CollectionInterval, *alert.ThresholdValue))
	if description == alert.Description {
		return
	}

	e.log("Probe %s on connection %d is collecting again; restoring the description of staleness alert %d",
		entry.ProbeName, alert.ConnectionID, alert.ID)

	if err := e.datastore.UpdateAlertDescription(ctx, alert.ID, description); err != nil {
		e.log("ERROR: Failed to restore description of alert %d: %v", alert.ID, err)
		return
	}
	alert.Description = description
}

// resolveAbsentMetric decides what to do with an active alert whose metric
// returned no row for it. A missing row is only a recovery signal for
// metrics whose query goes quiet when the condition ends or whose subject
// can legitimately disappear (the registry marks those clearWhenAbsent);
// for every other metric it means the data has stopped arriving, and
// clearing on it made alerts flap or resolve falsely whenever a probe ran
// late or a collector stopped. Those alerts stay active until fresh data
// shows the condition has ended, and the metric_staleness rule reports
// the stalled probe.
//
// Even for a clearWhenAbsent metric an empty result only means recovery
// if the data behind it is current: every one of those queries bounds
// collected_at, so a stopped collector empties them just as effectively
// as a recovered condition does, and clearing then would report a
// genuinely inactive replication slot or runaway WAL retention as
// resolved. The clear is therefore gated on the registry's probe for the
// metric having collected inside the very window that query reads. See
// GitHub issue #407.
func (e *Engine) resolveAbsentMetric(ctx context.Context, alert *database.Alert,
	reason string, staleness *probeStalenessSnapshot) {
	metric := *alert.MetricName
	clears := e.datastore.MetricClearsWhenAbsent(metric)
	probe := e.datastore.MetricProbeName(metric)
	window := e.datastore.MetricAbsenceWindow(metric)

	// Only the metrics that can clear, and that name both a probe and a
	// window to check it against, need the staleness read at all.
	var entries []database.ProbeStaleness
	if clears && probe != "" && window > 0 {
		var err error
		entries, err = staleness.get(ctx, e)
		if err != nil {
			e.log("ERROR: Cannot check whether probe %s is current for alert %d, "+
				"leaving it active: %v", probe, alert.ID, err)
			return
		}
	}

	switch classifyAbsentMetric(clears, probe, window, alert.ConnectionID, entries,
		staleness.age()) {
	case absentMetricClear:
		e.clearResolvedAlert(ctx, alert, 0)
	case absentMetricNotAbsenceDriven:
		e.debugLog("Metric %s has no current value for alert %d (%s); leaving it active until data returns",
			metric, alert.ID, reason)
	case absentMetricNoProbe, absentMetricNoWindow:
		// The registry audits keep this unreachable: an entry that
		// clears when absent names a probe and declares the window its
		// query reads. Both verdicts report the same operator problem,
		// an entry the cleaner cannot judge, so they share a line.
		e.log("WARNING: Metric %s clears when absent but its registry entry is "+
			"incomplete (probe %q, window %s); leaving alert %d active",
			metric, probe, window, alert.ID)
	case absentMetricProbeNotReporting:
		e.log("Alert %d on metric %s has no current value (%s) and probe %s has not "+
			"collected for connection %d within the %s window its query reads; "+
			"leaving the alert active",
			alert.ID, metric, reason, probe, alert.ConnectionID, window)
	}
}

// absentMetricVerdict is what the cleaner should do with an alert whose
// metric returned no row for it.
type absentMetricVerdict int

const (
	// absentMetricClear means the absence is a real recovery signal: the
	// metric clears when absent and the probe behind it is current.
	absentMetricClear absentMetricVerdict = iota

	// absentMetricNotAbsenceDriven means the metric emits a row for every
	// healthy connection, so a missing row is missing data. Expected
	// whenever collection lags, hence logged at debug level.
	absentMetricNotAbsenceDriven

	// absentMetricNoProbe means the registry entry clears when absent but
	// names no probe, so its freshness cannot be established.
	absentMetricNoProbe

	// absentMetricNoWindow means the registry entry clears when absent
	// but declares no collection window, so there is nothing to judge
	// the probe's last collection against. The registry audit tests keep
	// this unreachable in practice.
	absentMetricNoWindow

	// absentMetricProbeNotReporting means the probe behind the metric has
	// stalled, been disabled, or belongs to a connection that is no
	// longer monitored, so the empty result proves nothing.
	absentMetricProbeNotReporting
)

// classifyAbsentMetric decides, from the registry classification for a
// metric and the current probe staleness entries, whether an absent row
// may clear the alert. It is pure so that every branch, including the
// entry that names no probe, is exercised without a database.
//
// window is how far back the metric's latest query looks. Outside it the
// query reports nothing whatever the server is doing, so an absent row
// is only evidence of recovery when the probe stored something inside
// it; a probe whose last collection is older than the window tells us
// only that the data stopped arriving. Measuring the gate against the
// query's own window rather than against a multiple of the configured
// collection interval keeps it correct at any interval, global or
// per-connection: an operator who raises probe_configs.collection_
// interval_seconds past the window would otherwise widen the gate whilst
// the window stayed put, leaving the cleaner quiet exactly where
// ordinary collection starts emptying the query.
//
// age is how long ago the entries were read. Their SinceCollected is
// fixed at that moment, so the probe is age later than it says by the
// time the alert is judged, and the comparison brings it forward before
// deciding; otherwise a pass that ran on for ten seconds would clear on
// a probe ten seconds outside its window.
//
// Failing safe in both directions is the point: a clearWhenAbsent metric
// whose probe is demonstrably current clears as it always did, whilst an
// unknown, stalled or disabled probe leaves the alert active. That is a
// deliberate trade-off, because a probe the operator turns off, or a
// connection they stop monitoring, now keeps the alert until they clear
// or acknowledge it; the alternative is announcing a resolution nobody
// observed, which for a critical rule such as replication_slot_inactive
// means a genuinely inactive slot reported as fixed. See GitHub issue
// #407.
func classifyAbsentMetric(clearsWhenAbsent bool, probe string, window time.Duration,
	connectionID int, entries []database.ProbeStaleness, age time.Duration) absentMetricVerdict {
	if !clearsWhenAbsent {
		return absentMetricNotAbsenceDriven
	}
	if probe == "" {
		return absentMetricNoProbe
	}
	if window <= 0 {
		return absentMetricNoWindow
	}
	for _, entry := range entries {
		if entry.ConnectionID != connectionID || entry.ProbeName != probe {
			continue
		}
		// An unavailable probe has stopped collecting whatever its last
		// collection says, and a last collection that still happens to
		// sit inside the window would otherwise clear the alert on data
		// that has stopped arriving. Before issue #465 the staleness
		// query filtered these rows out and they reached the verdict
		// below; keeping the verdict identical preserves the protection
		// issue #407 added.
		if !entry.IsAvailable {
			return absentMetricProbeNotReporting
		}
		if entry.SinceCollected+age <= window {
			return absentMetricClear
		}
		return absentMetricProbeNotReporting
	}

	// The staleness view still filters out probes that are disabled,
	// never collected, or whose connection is not monitored, so a probe
	// missing from it is not reporting. Unavailable probes are no longer
	// among them: they are present in the view and handled above.
	return absentMetricProbeNotReporting
}

// clearResolvedAlert clears an alert and queues a notification
func (e *Engine) clearResolvedAlert(ctx context.Context, alert *database.Alert, value float64) {
	e.log("Alert resolved: %s (%.2f no longer %s %.2f)", alert.Title, value, *alert.Operator, *alert.ThresholdValue)
	if err := e.datastore.ClearAlert(ctx, alert.ID); err != nil {
		e.log("ERROR: Failed to clear alert: %v", err)
		return
	}

	// Queue clear notification for async processing by worker pool
	e.queueNotification(alert, database.NotificationTypeAlertClear)
}

// cleanupOldData removes data older than retention period
func (e *Engine) cleanupOldData(ctx context.Context) {
	e.debugLog("Running retention cleanup...")

	// Get retention settings
	settings, err := e.datastore.GetAlerterSettings(ctx)
	if err != nil {
		e.log("ERROR: Failed to get settings: %v", err)
		return
	}

	retentionDays := settings.RetentionDays
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	// Delete old cleared/acknowledged alerts
	deleted, err := e.datastore.DeleteOldAlerts(ctx, cutoff)
	if err != nil {
		e.log("ERROR: Failed to delete old alerts: %v", err)
	} else if deleted > 0 {
		e.log("Deleted %d old alerts", deleted)
	}

	// Delete old anomaly candidates
	deleted, err = e.datastore.DeleteOldAnomalyCandidates(ctx, cutoff)
	if err != nil {
		e.log("ERROR: Failed to delete old anomaly candidates: %v", err)
	} else if deleted > 0 {
		e.log("Deleted %d old anomaly candidates", deleted)
	}
}
