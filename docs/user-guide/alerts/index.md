# Alerts

The alert system monitors PostgreSQL metrics and notifies
users when thresholds are exceeded or anomalies are
detected. This guide explains how alerts appear in the
web interface and how to manage the alert lifecycle.

## Viewing Alerts

The status panel displays active alerts for the scope
selected in the cluster navigator. Alerts appear grouped
by severity and category. Each alert shows the rule
name, current metric value, threshold, and the server
where the alert originated.

Selecting a different node in the cluster navigator
updates the alert list to reflect that scope. Estate-wide
alerts appear when no specific node is selected.

## Severity Levels

Alerts use three severity levels to indicate urgency:

- A critical alert indicates a severe issue that requires
  immediate attention.
- A warning alert indicates a potential problem that
  should be investigated soon.
- An info alert indicates a noteworthy condition for
  general awareness.

The severity level determines the visual styling of each
alert in the status panel. Critical alerts appear with
red indicators; warning alerts appear with amber
indicators; info alerts appear with blue indicators.

## Alert Lifecycle

Each alert progresses through a defined set of states
from creation to resolution.

### Active

The alerter creates an alert with active status when a
metric violates a threshold. The alert remains active
until the condition resolves or an operator takes action.
The system updates the metric value on each evaluation
cycle while the alert stays active.

### Acknowledged

An operator can acknowledge an active alert to indicate
that the issue is under investigation. Acknowledged
alerts remain visible in the status panel but move to a
separate section. Acknowledging an alert does not resolve
the underlying condition.

### Cleared

The alerter automatically clears an alert when the
triggering condition returns to normal. The alert cleaner
runs every 30 seconds and re-evaluates active alerts.
When a metric value no longer violates the threshold, the
system marks the alert as cleared and records the
timestamp.

### False Positive

An operator can mark an alert as a false positive to
indicate that the alert does not represent a real issue.
The false positive designation helps refine alert
accuracy over time.

## Acknowledging Alerts

Click the acknowledge button on an active alert to mark
the alert as acknowledged. The alert moves to the
acknowledged section of the status panel.

Acknowledging an alert signals to other operators that
someone is investigating the issue. The system records
the operator who acknowledged the alert and the
timestamp.

## Alert History

The alert history provides a record of all past alerts
for a given scope. Use the alert history to review
patterns, identify recurring issues, and verify that
resolved conditions remain stable.

Historical alerts include the trigger time, resolution
time, peak metric value, and the action taken by the
operator.

## AI-Powered Analysis

Each alert in the status panel displays a brain icon
that triggers an AI-powered analysis. The analysis
examines the alert context, historical patterns, and
server configuration to produce actionable remediation
guidance. See [AI Alert Analysis](ai-analysis.md) for
details on this feature.

## Editing Alert Thresholds

Users can edit alert thresholds directly from an alert
instance. The edit button on an alert opens the Edit
Override dialog for the associated rule and scope. The
dialog allows adjustments to the threshold, operator,
severity, and enabled state.

The scope dropdown displays the available override
levels: server, cluster, and group. The dialog
pre-selects the scope that matches the originating
context of the alert.

## Blackout Interaction

During an active blackout period, the alerter suppresses
new alerts for the affected connection or database.
Existing active alerts remain visible during a blackout;
the blackout only prevents new alerts from being created.
See [Blackouts](../blackouts.md) for details on
maintenance windows.

## Related Documentation

- [Alert Rule Reference](rule-reference.md) lists all
  built-in alert rules and their default thresholds.
- [AI Alert Analysis](ai-analysis.md) describes the
  AI-powered analysis feature for alerts.
