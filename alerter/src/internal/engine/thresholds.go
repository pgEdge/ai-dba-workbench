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
	"fmt"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// evaluateThresholds checks all threshold rules against current metrics
func (e *Engine) evaluateThresholds(ctx context.Context) {
	e.debugLog("Evaluating threshold rules...")

	// Get all enabled rules
	rules, err := e.datastore.GetEnabledAlertRules(ctx)
	if err != nil {
		e.log("ERROR: Failed to get alert rules: %v", err)
		return
	}

	e.debugLog("Found %d enabled rules", len(rules))

	for _, rule := range rules {
		if ctx.Err() != nil {
			return
		}
		e.evaluateRuleForAllConnections(ctx, rule)
	}

	// Then the two probe-scoped passes, which read the same view.
	e.evaluateProbeScopedRules(ctx)
}

// evaluateProbeScopedRules runs the two passes keyed on a probe rather
// than a metric: staleness, for a probe that is merely late, and
// availability, for one that was collecting and has stopped. Both judge
// exactly the same rows, so the view is read once here and shared; a
// failed read says nothing about either condition, so it is logged once
// and both passes are skipped rather than each reporting the same error.
func (e *Engine) evaluateProbeScopedRules(ctx context.Context) {
	entries, err := e.datastore.GetProbeStalenessByConnection(ctx)
	if err != nil {
		e.log("ERROR: Failed to get probe staleness: %v", err)
		return
	}

	e.evaluateMetricStaleness(ctx, entries)

	// The availability pass follows staleness, and reports the entries
	// that the staleness evaluator deliberately says nothing about.
	e.evaluateProbeUnavailable(ctx, entries)
}

// Rule and metric names for the two probe-scoped rules. Neither metric
// has a metricRegistry entry, because both rules are evaluated by the
// bespoke passes in this file rather than through the registry, so these
// constants are the only place the names are written.
const (
	// metricStalenessRuleName is the rule reporting a probe that is
	// merely late.
	metricStalenessRuleName = "metric_staleness"

	// probeUnavailableRuleName is the rule reporting a probe that was
	// collecting and has stopped being available.
	probeUnavailableRuleName = "probe_unavailable"

	// probeUnavailableMetricName is that rule's metric_name, and is how
	// the alert cleaner tells a probe_unavailable alert apart from a
	// metric_staleness one: both carry a probe name, and the two resolve
	// on entirely different conditions.
	probeUnavailableMetricName = "probe_available"
)

// The two values the probe_available metric takes. The rule is seeded as
// "< 1", so an unavailable probe reports 0 and a probe that is collecting
// reports 1; the alert cleaner clears on the latter.
const (
	probeUnavailableValue = 0.0
	probeAvailableValue   = 1.0
)

// evaluateRuleForAllConnections evaluates a rule across all connections with data
func (e *Engine) evaluateRuleForAllConnections(ctx context.Context, rule *database.AlertRule) {
	// Get all metric values for this rule's metric
	values, err := e.datastore.GetLatestMetricValues(ctx, rule.MetricName)
	if err != nil {
		e.debugLog("No data for metric %s: %v", rule.MetricName, err)
		return
	}

	// Rules with a required_extension only apply to connections whose
	// newest pg_extension snapshot lists it. Resolve the set once per rule
	// rather than once per value; a nil set means "no gate".
	withExtension := e.connectionsWithRequiredExtension(ctx, rule)

	for _, mv := range values {
		if ctx.Err() != nil {
			return
		}

		connID := mv.ConnectionID
		if withExtension != nil && !withExtension[connID] {
			e.debugLog("Skipping rule %s for connection %d: extension %s not installed",
				rule.Name, connID, *rule.RequiredExtension)
			continue
		}

		// Check if there's a blackout active for this connection
		active, err := e.datastore.IsBlackoutActive(ctx, &connID, mv.DatabaseName)
		if err != nil {
			e.debugLog("Error checking blackout for connection %d: %v", connID, err)
		}
		if active {
			e.debugLog("Skipping rule %s for connection %d: blackout active", rule.Name, connID)
			continue
		}

		// Get the effective threshold (global or per-connection override)
		threshold, operator, severity, enabled := e.datastore.GetEffectiveThreshold(
			ctx, rule.ID, mv.ConnectionID, mv.DatabaseName)
		if !enabled {
			e.debugLog("Rule %s disabled for connection %d", rule.Name, mv.ConnectionID)
			continue
		}

		// Check if threshold is violated
		violated := e.checkThreshold(mv.Value, operator, threshold)

		if violated {
			e.triggerThresholdAlert(ctx, rule, mv.Value, threshold, operator,
				severity, mv.ConnectionID, mv.DatabaseName, mv.ObjectName)
		}
	}
}

// connectionsWithRequiredExtension returns the connections on which rule
// may be evaluated, or nil when the rule has no required_extension and
// every connection qualifies.
//
// The set comes from the newest metrics.pg_extension snapshot per
// connection, so a connection whose probe has never run is treated as
// lacking the extension. If the lookup itself fails the error is logged
// and nil is returned, so the rule is evaluated without the gate: a
// transient datastore problem must not silence a whole class of rules.
// See GitHub issue #409.
func (e *Engine) connectionsWithRequiredExtension(ctx context.Context, rule *database.AlertRule) map[int]bool {
	if rule.RequiredExtension == nil || *rule.RequiredExtension == "" {
		return nil
	}
	withExtension, err := e.datastore.GetConnectionsWithExtension(ctx, *rule.RequiredExtension)
	if err != nil {
		e.log("ERROR: Failed to look up connections with extension %s for rule %s, evaluating without the gate: %v",
			*rule.RequiredExtension, rule.Name, err)
		return nil
	}
	return withExtension
}

// formatMetricValue renders an alert's metric_value safely for logging.
// MetricValue is a *float64 because the database column is nullable, so a
// raw dereference would panic on rows where the value is NULL. Returning
// a placeholder for nil preserves the diagnostic message without aborting
// triggerThresholdAlert and skipping its auto-reactivation logic.
func formatMetricValue(v *float64) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%.2f", *v)
}

// checkThreshold checks if a value violates a threshold
func (e *Engine) checkThreshold(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return false
	}
}

// triggerThresholdAlert creates or updates an alert for a threshold violation
func (e *Engine) triggerThresholdAlert(ctx context.Context, rule *database.AlertRule, value, threshold float64, operator, severity string, connectionID int, dbName *string, objectName *string) {
	e.log("Threshold violated: %s (%.2f %s %.2f) on connection %d", rule.Name, value, operator, threshold, connectionID)

	// Check if there's already an active or acknowledged alert for this rule/connection
	existing, err := e.datastore.GetActiveThresholdAlert(ctx, rule.ID, connectionID, dbName)
	if err == nil && existing != nil {
		// Capture the previous state BEFORE any database write. The
		// auto-reactivation check below must compare the new severity
		// against the severity that was visible to the user (i.e. the
		// value stored when the alert was acknowledged), so reading the
		// in-memory copy before UpdateAlertValues runs decouples the
		// check from the order of the writes. UpdateAlertValues
		// overwrites the severity column unconditionally, so without
		// this capture a future refactor that re-reads `existing` after
		// the write could silently break the reactivation path.
		previousStatus := existing.Status
		previousSeverity := existing.Severity
		needsReactivation := previousStatus == "acknowledged" && previousSeverity != severity

		// Alert already exists - update metric_value, threshold, operator,
		// severity, and last_updated timestamp. Track whether the write
		// succeeded so that the reactivation step below can be skipped if
		// the persisted severity is still the old value. Reactivating an
		// alert whose UPDATE failed would leave the database holding the
		// previous severity while the queued notification advertised the
		// new one, drifting state away from what users see.
		updated := true
		if err := e.datastore.UpdateAlertValues(ctx, existing.ID, value, threshold, operator, severity); err != nil {
			updated = false
			e.log("ERROR: Failed to update alert values for alert %d: %v", existing.ID, err)
		} else {
			e.debugLog("Updated metric value for existing alert %s: %s -> %.2f",
				rule.Name, formatMetricValue(existing.MetricValue), value)
		}

		// If the alert was acknowledged but the severity has changed,
		// reactivate it so the user sees the severity change. Skip this
		// when the UpdateAlertValues call above failed: the database
		// still has the previous severity, so reactivating would queue a
		// notification with a severity that does not match the persisted
		// row.
		if needsReactivation && updated {
			if err := e.datastore.ReactivateAlert(ctx, existing.ID); err != nil {
				e.log("ERROR: Failed to reactivate alert %d after severity change: %v", existing.ID, err)
			} else {
				e.log("Reactivated acknowledged alert %d: severity changed from %s to %s",
					existing.ID, previousSeverity, severity)
				// Mirror the database mutation onto the in-memory
				// struct so the queued notification reflects the new
				// severity and active status. Re-reading the alert via
				// GetAlert here would add a database round-trip whose
				// only failure mode is rare pool errors and whose
				// success case returns the values we already know;
				// trusting the in-memory state keeps the path simple
				// and fully covered by unit tests.
				existing.Severity = severity
				existing.Status = "active"
				e.queueNotification(existing, database.NotificationTypeAlertFire)
			}
		}
		return
	}

	// Check cooldown - don't re-fire if recently cleared (prevents flapping)
	recentlyClosed, err := e.datastore.GetRecentlyClearedAlert(ctx, rule.ID, connectionID, dbName, AlertCooldownPeriod)
	if err != nil {
		e.debugLog("Error checking cooldown for %s on connection %d: %v", rule.Name, connectionID, err)
	} else if recentlyClosed {
		e.debugLog("Skipping alert %s on connection %d: cooldown active (cleared within %v)", rule.Name, connectionID, AlertCooldownPeriod)
		return
	}

	// Create new alert
	alert := &database.Alert{
		AlertType:      "threshold",
		RuleID:         &rule.ID,
		ConnectionID:   connectionID,
		DatabaseName:   dbName,
		ObjectName:     objectName,
		MetricName:     &rule.MetricName,
		MetricValue:    &value,
		ThresholdValue: &threshold,
		Operator:       &operator,
		Severity:       severity,
		Title:          rule.Name,
		Description:    rule.Description,
		Status:         "active",
		TriggeredAt:    time.Now(),
	}

	if err := e.datastore.CreateAlert(ctx, alert); err != nil {
		e.log("ERROR: Failed to create alert: %v", err)
		return
	}

	// Queue alert notification for async processing by worker pool
	e.queueNotification(alert, database.NotificationTypeAlertFire)
}

// evaluateMetricStaleness checks for probes whose data exceeds the configured
// staleness threshold (expressed as a multiple of the collection interval).
// The entries are the pass's shared read of the probe staleness view, made
// once by evaluateProbeScopedRules.
func (e *Engine) evaluateMetricStaleness(ctx context.Context,
	entries []database.ProbeStaleness) {
	e.debugLog("Evaluating metric staleness...")

	rule, err := e.datastore.GetAlertRuleByName(ctx, metricStalenessRuleName)
	if err != nil {
		e.debugLog("No metric_staleness rule found: %v", err)
		return
	}
	if !rule.DefaultEnabled {
		e.debugLog("metric_staleness rule is disabled")
		return
	}

	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}

		// Unavailable probes are in the snapshot for the alert cleaner's
		// benefit, but they must not raise staleness alerts here. A probe
		// whose extension is not installed, or that the server will never
		// support, sits at is_available = FALSE indefinitely, and it is a
		// perfectly normal steady state rather than a fault to report; if
		// this loop evaluated those entries it would raise a permanent
		// staleness alert for every such probe on every connection. Do
		// not "fix" this skip: the alert cleaner, not the evaluator, is
		// what issue #465 changed, and a probe that was collecting and
		// has stopped being available is reported by
		// evaluateProbeUnavailable below under its own rule, which is
		// what issue #512 added.
		if !entry.IsAvailable {
			e.debugLog("Skipping staleness check for probe %s on connection %d: probe is unavailable",
				entry.ProbeName, entry.ConnectionID)
			continue
		}

		// Check if there's a blackout active for this connection
		connID := entry.ConnectionID
		active, err := e.datastore.IsBlackoutActive(ctx, &connID, nil)
		if err != nil {
			e.debugLog("Error checking blackout for connection %d: %v", connID, err)
		}
		if active {
			e.debugLog("Skipping staleness check for connection %d: blackout active", connID)
			continue
		}

		threshold, operator, severity, enabled := e.datastore.GetEffectiveThreshold(
			ctx, rule.ID, entry.ConnectionID, nil)
		if !enabled {
			e.debugLog("metric_staleness disabled for connection %d", entry.ConnectionID)
			continue
		}

		violated := e.checkThreshold(entry.StalenessRatio, operator, threshold)
		if violated {
			elapsedMinutes := stalenessMinutes(entry.CollectionInterval, entry.StalenessRatio)
			thresholdMinutes := stalenessMinutes(entry.CollectionInterval, threshold)

			title := fmt.Sprintf("Stale metrics: %s on %s", entry.ProbeName, entry.ConnectionName)
			description := stalenessAlertDescription(entry.ProbeName, entry.ConnectionName,
				elapsedMinutes, thresholdMinutes)

			metricName := rule.MetricName
			probeName := entry.ProbeName
			alert := &database.Alert{
				AlertType:      "threshold",
				RuleID:         &rule.ID,
				ConnectionID:   entry.ConnectionID,
				ProbeName:      &probeName,
				MetricName:     &metricName,
				MetricValue:    &entry.StalenessRatio,
				ThresholdValue: &threshold,
				Operator:       &operator,
				Severity:       severity,
				Title:          title,
				Description:    description,
				Status:         "active",
				TriggeredAt:    time.Now(),
			}

			// Check if there's already an active alert for this
			// rule/connection/probe. The lookup is keyed by probe so that
			// several stale probes on one connection raise one alert each
			// rather than fighting over a single row.
			existing, err := e.datastore.GetActiveThresholdAlertForProbe(ctx, rule.ID, entry.ConnectionID, entry.ProbeName)
			if err != nil {
				// Creating an alert now could duplicate one that already
				// exists, so wait for the next pass instead.
				e.log("ERROR: Failed to look up staleness alert for %s on connection %d: %v",
					entry.ProbeName, entry.ConnectionID, err)
				continue
			}
			if existing != nil {
				if err := e.datastore.UpdateAlertValues(ctx, existing.ID, entry.StalenessRatio, threshold, operator, severity); err != nil {
					e.log("ERROR: Failed to update staleness alert values: %v", err)
				} else {
					e.debugLog("Updated staleness alert for %s on connection %d", entry.ProbeName, entry.ConnectionID)
				}
				continue
			}

			// Check cooldown - don't re-fire if recently cleared. The
			// registry-backed path applies the same guard in
			// triggerThresholdAlert.
			recentlyCleared, err := e.datastore.GetRecentlyClearedAlertForProbe(
				ctx, rule.ID, entry.ConnectionID, entry.ProbeName, AlertCooldownPeriod)
			if err != nil {
				e.debugLog("Error checking staleness cooldown for %s on connection %d: %v",
					entry.ProbeName, entry.ConnectionID, err)
			} else if recentlyCleared {
				e.debugLog("Skipping staleness alert for %s on connection %d: cooldown active (cleared within %v)",
					entry.ProbeName, entry.ConnectionID, AlertCooldownPeriod)
				continue
			}

			if err := e.datastore.CreateAlert(ctx, alert); err != nil {
				e.log("ERROR: Failed to create staleness alert: %v", err)
				continue
			}

			e.log("Staleness alert created: %s", title)
			e.queueNotification(alert, database.NotificationTypeAlertFire)
		}
	}
}

// evaluateProbeUnavailable reports probes that were collecting and have
// stopped being available, which is the fault evaluateMetricStaleness
// cannot report and, before issue #512, nothing else did either: the
// collector flips is_available false on the first probe run after the
// extension, the privilege or the connection goes, whilst the staleness
// ratio is still about 1, so no staleness alert has fired for the alert
// cleaner to hold open and the staleness evaluator will not raise one
// afterwards because it skips unavailable entries.
//
// No stored record of the previous availability is needed to tell that
// transition from a probe that has never been available, and none should
// be added. probe_availability.last_collected is sticky, because the
// collector's UpsertProbeAvailability coalesces it, so it never returns
// to NULL once a probe has collected at all, and
// GetProbeStalenessByConnection filters on pa.last_collected IS NOT NULL.
// An entry in that view whose IsAvailable is false is therefore, by
// construction, a probe that collected before and has stopped being
// available; a probe whose extension was never installed has a NULL
// last_collected and never appears in the view at all. Adding a column or
// a previous-state table to re-derive this would duplicate a fact the
// view already carries.
//
// The entries are the same shared read the staleness pass judged, so this
// pass adds no query of its own.
func (e *Engine) evaluateProbeUnavailable(ctx context.Context,
	entries []database.ProbeStaleness) {
	e.debugLog("Evaluating probe availability...")

	rule, err := e.datastore.GetAlertRuleByName(ctx, probeUnavailableRuleName)
	if err != nil {
		e.debugLog("No %s rule found: %v", probeUnavailableRuleName, err)
		return
	}
	if !rule.DefaultEnabled {
		e.debugLog("%s rule is disabled", probeUnavailableRuleName)
		return
	}

	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if entry.IsAvailable {
			continue
		}
		e.evaluateProbeUnavailableEntry(ctx, rule, entry)
	}
}

// evaluateProbeUnavailableEntry raises, or updates, the probe_unavailable
// alert for one unavailable probe, applying the same blackout,
// per-connection threshold and cooldown guards as the staleness
// evaluator.
func (e *Engine) evaluateProbeUnavailableEntry(ctx context.Context,
	rule *database.AlertRule, entry database.ProbeStaleness) {
	connID := entry.ConnectionID

	active, err := e.datastore.IsBlackoutActive(ctx, &connID, nil)
	if err != nil {
		e.debugLog("Error checking blackout for connection %d: %v", connID, err)
	}
	if active {
		e.debugLog("Skipping probe availability check for connection %d: blackout active", connID)
		return
	}

	threshold, operator, severity, enabled := e.datastore.GetEffectiveThreshold(
		ctx, rule.ID, connID, nil)
	if !enabled {
		e.debugLog("%s disabled for connection %d", probeUnavailableRuleName, connID)
		return
	}

	// The metric is the availability flag itself, 0 whilst the probe is
	// unavailable, so the seeded "< 1" is violated for as long as the
	// probe stays that way. An operator who has overridden the operator
	// or threshold gets whatever they configured.
	if !e.checkThreshold(probeUnavailableValue, operator, threshold) {
		return
	}

	// One alert per probe, as for staleness: several probes failing on
	// one connection raise one alert each rather than fighting over a
	// single row.
	existing, err := e.datastore.GetActiveThresholdAlertForProbe(ctx, rule.ID, connID, entry.ProbeName)
	if err != nil {
		// Creating an alert now could duplicate one that already
		// exists, so wait for the next pass instead.
		e.log("ERROR: Failed to look up probe availability alert for %s on connection %d: %v",
			entry.ProbeName, connID, err)
		return
	}
	if existing != nil {
		if err := e.datastore.UpdateAlertValues(ctx, existing.ID, probeUnavailableValue,
			threshold, operator, severity); err != nil {
			e.log("ERROR: Failed to update probe availability alert values: %v", err)
		} else {
			e.debugLog("Updated probe availability alert for %s on connection %d",
				entry.ProbeName, connID)
		}
		return
	}

	recentlyCleared, err := e.datastore.GetRecentlyClearedAlertForProbe(
		ctx, rule.ID, connID, entry.ProbeName, AlertCooldownPeriod)
	if err != nil {
		e.debugLog("Error checking probe availability cooldown for %s on connection %d: %v",
			entry.ProbeName, connID, err)
	} else if recentlyCleared {
		e.debugLog("Skipping probe availability alert for %s on connection %d: cooldown active (cleared within %v)",
			entry.ProbeName, connID, AlertCooldownPeriod)
		return
	}

	reason := unavailableProbeReason(entry)
	title := fmt.Sprintf("Probe unavailable: %s on %s", entry.ProbeName, entry.ConnectionName)
	description := probeUnavailableAlertDescription(entry.ProbeName, entry.ConnectionName, reason)

	metricName := rule.MetricName
	probeName := entry.ProbeName
	value := probeUnavailableValue
	alert := &database.Alert{
		AlertType:      "threshold",
		RuleID:         &rule.ID,
		ConnectionID:   connID,
		ProbeName:      &probeName,
		MetricName:     &metricName,
		MetricValue:    &value,
		ThresholdValue: &threshold,
		Operator:       &operator,
		Severity:       severity,
		Title:          title,
		Description:    description,
		Status:         "active",
		TriggeredAt:    time.Now(),
	}

	if err := e.datastore.CreateAlert(ctx, alert); err != nil {
		e.log("ERROR: Failed to create probe availability alert: %v", err)
		return
	}

	e.log("Probe availability alert created: %s (%s)", title, reason)
	e.queueNotification(alert, database.NotificationTypeAlertFire)
}

// probeUnavailableAlertDescription is the wording a probe_unavailable
// alert carries. The reason is whatever the collector recorded, or the
// stand-in unavailableProbeReason supplies when it recorded nothing, so
// the sentence never has a hole in it.
func probeUnavailableAlertDescription(probeName, connectionName, reason string) string {
	return fmt.Sprintf(
		"The %s probe on %s was collecting and is no longer available (%s). "+
			"Metric collection for this probe has stopped, so dashboards and "+
			"alert rules that depend on it cannot be evaluated until it is "+
			"available again.",
		probeName, connectionName, reason)
}

// stalenessMinutes converts a staleness ratio into the number of minutes
// it stands for at a probe's collection interval, which is how both the
// evaluator and the alert cleaner phrase staleness to an operator.
func stalenessMinutes(collectionInterval int, ratio float64) float64 {
	return float64(collectionInterval) * ratio / 60.0
}

// stalenessAlertDescription is the wording a metric_staleness alert
// carries whilst its probe is merely late rather than gone. It is shared
// with the alert cleaner, which puts this text back when a probe that had
// gone unavailable starts collecting again, so that the alert does not
// resolve in the words of the hold. See GitHub issue #465.
func stalenessAlertDescription(probeName, connectionName string,
	elapsedMinutes, thresholdMinutes float64) string {
	return fmt.Sprintf(
		"The %s probe on %s has not collected data for %.0f minutes "+
			"(threshold: %.0f minutes). Dashboards may show outdated data.",
		probeName, connectionName, elapsedMinutes, thresholdMinutes)
}

// evaluateConnectionErrors checks monitored connections for error states
func (e *Engine) evaluateConnectionErrors(ctx context.Context) {
	e.debugLog("Evaluating connection errors...")

	connections, err := e.datastore.GetMonitoredConnectionErrors(ctx)
	if err != nil {
		e.log("ERROR: Failed to get monitored connection errors: %v", err)
		return
	}

	for _, conn := range connections {
		if ctx.Err() != nil {
			return
		}

		if conn.ConnectionError != nil {
			// Check if there's a blackout active for this connection
			connID := conn.ConnectionID
			active, err := e.datastore.IsBlackoutActive(ctx, &connID, nil)
			if err != nil {
				e.debugLog("Error checking blackout for connection %d: %v", connID, err)
			}
			if active {
				e.debugLog("Skipping connection error alert for connection %d: blackout active", connID)
				continue
			}

			// Connection has an error - create or update alert
			alertID, desc, found, err := e.datastore.GetActiveConnectionAlert(ctx, conn.ConnectionID)
			if err != nil {
				e.log("ERROR: Failed to check active connection alert for connection %d: %v", conn.ConnectionID, err)
				continue
			}

			if !found {
				alert, err := e.datastore.CreateConnectionAlert(ctx, conn.ConnectionID, conn.Name, *conn.ConnectionError)
				if err != nil {
					e.log("ERROR: Failed to create connection alert for connection %d: %v", conn.ConnectionID, err)
					continue
				}
				e.log("Connection error alert created for %s: %s", conn.Name, *conn.ConnectionError)

				// Queue notification for async processing
				e.queueNotification(alert, database.NotificationTypeAlertFire)
			} else if desc != *conn.ConnectionError {
				if err := e.datastore.UpdateConnectionAlertDescription(ctx, alertID, *conn.ConnectionError); err != nil {
					e.log("ERROR: Failed to update connection alert description for connection %d: %v", conn.ConnectionID, err)
				} else {
					e.debugLog("Updated connection error description for %s", conn.Name)
				}
			}
		} else {
			// No error - clear any active connection alert
			alertID, _, found, err := e.datastore.GetActiveConnectionAlert(ctx, conn.ConnectionID)
			if err != nil {
				e.log("ERROR: Failed to check active connection alert for connection %d: %v", conn.ConnectionID, err)
				continue
			}

			if found {
				if err := e.datastore.ClearAlert(ctx, alertID); err != nil {
					e.log("ERROR: Failed to clear connection alert for connection %d: %v", conn.ConnectionID, err)
					continue
				}
				e.log("Connection error alert cleared for %s", conn.Name)

				// Queue clear notification
				alert, err := e.datastore.GetAlert(ctx, alertID)
				if err == nil {
					e.queueNotification(alert, database.NotificationTypeAlertClear)
				}
			}
		}
	}
}
