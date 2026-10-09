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

An anomaly alert has no threshold, so the alerter clears
an active anomaly alert once the metric returns to its
normal range instead. The anomaly detector checks the
metric on each evaluation, every minute by default, and
clears the alert once three newly collected samples in a
row have fallen within the normal range; the
`anomaly.tier1.clear_count` option in the alerter
configuration changes that number. Most probes collect
every five or ten minutes, so an alert usually clears
ten to twenty minutes after the first normal sample. Until then, the alert
shows the latest value and anomaly score, and its
severity can rise but does not fall. The alerter sends
the usual clear notification and raises no new anomaly
alert for the same metric on that server for five
minutes afterwards. An acknowledged anomaly alert is not
cleared this way. For a metric that reports only whilst its
condition holds, such as a blocked session, each collection
that no longer reports the condition counts as a normal
sample. An anomaly alert that the alerter can no longer
check, for example one raised by an earlier release without
the database it concerns, is closed without a notification
and its description explains why.

A gap in the collected data does not clear an alert. When
a metric that normally reports a value for every monitored
connection stops returning one, the alerter treats the gap
as missing data rather than recovery, so the alert stays
active and the metric staleness rule reports the probe
that stopped. Alerts on metrics that report only whilst
the condition holds, such as an inactive replication slot
or a blocked session, still clear as soon as the metric
goes quiet.

A metric staleness alert that has already fired follows
the same principle. When the probe behind such an alert
becomes unavailable, because a monitored server no longer
offers the extension the probe needs or because the probe
run timed out, the alert stays active and its description
changes to report that collection has stopped and why. The
alert title does not change, so the alert remains
recognisable in notification history, and the original
description returns once the probe collects again. When an
operator disables the probe, or stops monitoring the
connection, the alerter clears the staleness alert,
because both actions are deliberate, and it restores the
original description as the alert clears so that the
cleared alert does not report that collection has
stopped.

This behaviour holds an existing alert open; it does not
raise one. The probe that has stopped is reported by a
separate rule, `probe_unavailable`, which raises a warning
alert as soon as a probe that had been collecting stops
being available. That moment normally arrives long before
a staleness alert could fire, so the two rules cover
different halves of the same failure. A probe whose
extension was never installed does not raise the alert,
which keeps a permanent alert off every probe that has
simply never run.

The alerter clears a probe unavailable alert when the
probe collects again. The alerter also clears the alert
when an operator retires the probe by disabling it or by
no longer monitoring the connection, and when the probe
no longer reports its availability for that connection.

A held staleness alert and a probe unavailable alert can
be open on the same probe at the same time; the first
reports that the data went stale and the second reports
why, and each one clears on its own terms.

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

## System Alerts

A system alert reports a fault in the Workbench itself
rather than in a monitored server; the alert type is
`system` and the alert belongs to no connection. The
alerter currently raises one kind of system alert, for an
embedding or reasoning provider that keeps failing and so
degrades anomaly detection.

The alert title names the affected tier and provider, as
in `Anomaly detection degraded: Tier 2 embedding provider
ollama failing`. The description gives the model, the
number of consecutive failures so far
(or notes a failed startup check, or gives how many of the
provider's recent calls failed), what anomaly detection
does whilst the provider fails, and the last error the
provider returned, with any credentials removed. A network
failure appears only as its category, such as `connection
refused`, without the endpoint's address; the alerter log
records the endpoint for the administrator. The alerter
raises one alert for each tier and provider, at warning
severity, and clears the alert on the next successful call
to that provider unless a steady share of the provider's
recent calls are still failing; the alerter also raises the
alert for a provider that fails such a share of its calls
without failing consecutively. A successful tier 3 or
re-evaluation call counts as a success for both, since they
share the reasoning provider, and so can clear the alerts of
both. Once cleared, the alert is not raised again for
five minutes.

System alerts behave differently from connection alerts in
the following ways:

- every user and API token with access to alerts sees system
  alerts, whatever connections the user or token may access.
- a list of alerts filtered to particular connections leaves
  system alerts out.
- the alert counts report system alerts in a separate
  `system` total rather than under a server.
- blackouts, including estate blackouts, do not suppress
  system alerts.
- notifications go to the estate default channels, since
  channel overrides apply to a server, cluster or group.
- the event timeline leaves system alerts out.

Only a superuser, or a user whose group holds the
`manage_alert_rules` administrative permission, may
acknowledge a system alert or restore an acknowledged one;
an API token may do so only when its owner qualifies and
the token's administrative scope, if it has one, includes
`manage_alert_rules`. A system alert concerns no monitored
server and has no metric to explain, so the server refuses
to save an AI analysis on a system alert, answering the
request with a 400 status. See
[Alerter Configuration](../../getting-started/configuration/alerter.md#provider-health-provider_health)
for the failure threshold.

## Related Documentation

- [Alert Rule Reference](rule-reference.md) lists all
  built-in alert rules and their default thresholds.
- [AI Alert Analysis](ai-analysis.md) describes the
  AI-powered analysis feature for alerts.
