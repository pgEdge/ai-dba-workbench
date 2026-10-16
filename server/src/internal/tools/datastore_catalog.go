/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/pgsettings"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// datastoreScope says how the rows of a datastore source are limited to
// the connections a caller can read. Every rule mirrors the visibility
// the REST API already applies to the same table, so query_datastore
// can never show a caller more than the rest of the product does.
type datastoreScope int

const (
	// scopeConnection admits a row whose connection column holds a
	// visible connection ID.
	scopeConnection datastoreScope = iota
	// scopeViaAlert admits a row whose alert_id names an alert the
	// caller may see under the scopeAlert rule.
	scopeViaAlert
	// scopeHierarchy admits a row by its connection_id, cluster_id and
	// group_id columns, as blackoutVisible in api/blackout_handlers.go
	// and scopeVisibleToCaller in api/rbac_visibility.go do: a
	// connection row by that connection, a cluster row when the cluster
	// holds a visible connection, a group row when one of the group's
	// clusters does, and an estate-wide row (scope 'estate' or 'global'
	// with no reference) for everyone.
	scopeHierarchy
	// scopeCluster admits a cluster that contains a visible connection,
	// as clusterHasVisibleConnectionFn does.
	scopeCluster
	// scopeGroup admits a cluster group one of whose clusters contains
	// a visible connection, as groupHasVisibleConnectionFn does.
	scopeGroup
	// scopeGlobal admits every row to every caller holding the tool
	// privilege. It is used only for tables that hold no data about any
	// connection.
	scopeGlobal
	// scopeAlert admits a connection alert whose connection is visible,
	// and a system alert (alert_type 'system', no connection) when the
	// caller may see system alerts and has not asked for particular
	// connections, as handleAlerts and get_alert_history do (issue #582).
	scopeAlert
)

// String returns the name describe_datastore reports for the scope.
func (s datastoreScope) String() string {
	switch s {
	case scopeConnection:
		return "connection"
	case scopeViaAlert:
		return "via_alert"
	case scopeHierarchy:
		return "hierarchy"
	case scopeCluster:
		return "cluster"
	case scopeGroup:
		return "group"
	case scopeGlobal:
		return "global"
	case scopeAlert:
		return "alert"
	default:
		return "unknown"
	}
}

// Description explains, in a sentence, which rows of a source with this
// scope a caller sees.
func (s datastoreScope) Description() string {
	switch s {
	case scopeConnection:
		return "Rows for connections you can access."
	case scopeViaAlert:
		return "Rows belonging to alerts you can see."
	case scopeHierarchy:
		return "Estate-wide rows, rows for connections you can access, and rows for clusters or groups that contain one."
	case scopeCluster:
		return "Clusters that contain a connection you can access."
	case scopeGroup:
		return "Cluster groups that contain a connection you can access."
	case scopeGlobal:
		return "All rows; the source holds no data about any connection."
	case scopeAlert:
		return "Alerts for connections you can access, plus system alerts when no connection_ids are given."
	default:
		return "No rows."
	}
}

// Source kinds.
const (
	datastoreKindMetric        = "metric"
	datastoreKindConfiguration = "configuration"
)

// Metric source bookkeeping columns. Every probe table carries both.
const (
	metricConnectionColumn = "connection_id"
	metricTimeColumn       = "collected_at"
)

// datastoreColumn is one column a source permits callers to read.
type datastoreColumn struct {
	Name     string
	DataType string
	// IsMetric marks a numeric value column, as opposed to a dimension
	// or identifier; avg and sum aggregate only these.
	IsMetric bool
	// RedactBy, when set, names the column holding a setting's name, and
	// the column's value is masked on every row whose setting name looks
	// like it carries a credential (credentialSettingCondition).
	RedactBy string
	// MaskOptions marks a text[] column of name=value options, such as
	// pg_hba_file_rules.options; the value of each option whose name
	// looks like it carries a credential is masked (maskedOptions).
	MaskOptions bool
	// MaskWhole marks a column, such as pg_hba_file_rules.error, that
	// can quote a credential in a form no pattern catches; a
	// non-empty value is replaced by redactedValue.
	MaskWhole bool
	// RedactText marks a free-text column, such as query text or a
	// replicated row image, whose text has the passwords that
	// textRedactions match masked (redactedText).
	RedactText bool
	// StoredType is the column's type in its table when DataType differs
	// from it, as for a row image returned as text. Whether the column
	// is an entity key follows the stored type, so masking a column
	// does not change which rows shape latest returns.
	StoredType string
}

// keyType returns the type that decides whether c is an entity key.
func (c datastoreColumn) keyType() string {
	if c.StoredType != "" {
		return c.StoredType
	}
	return c.DataType
}

// datastoreSource is one source query_datastore accepts. Anything not
// described by a datastoreSource is rejected.
type datastoreSource struct {
	// Name is the source name callers pass.
	Name string
	// Table is the table the source reads: in the metrics schema for a
	// metric source, and in the default schema otherwise.
	Table       string
	Description string
	// TimeColumn is the column time_start and time_end apply to; empty
	// when the source takes no time range.
	TimeColumn string
	// ConnectionColumn holds the connection ID the scoping rule tests;
	// empty when the rule does not use one directly.
	ConnectionColumn string
	// Kind is datastoreKindMetric or datastoreKindConfiguration.
	Kind  string
	Scope datastoreScope
	// Columns is the explicit allow-list of readable columns. Nothing
	// outside it can be selected, filtered, grouped or ordered on.
	Columns []datastoreColumn
}

// column returns the permitted column with the given name.
func (s datastoreSource) column(name string) (datastoreColumn, bool) {
	for _, c := range s.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return datastoreColumn{}, false
}

// columnNames returns the names of every permitted column, in order.
func (s datastoreSource) columnNames() []string {
	names := make([]string, len(s.Columns))
	for i, c := range s.Columns {
		names[i] = c.Name
	}
	return names
}

// Column type names, as information_schema.columns reports them.
const (
	typeInteger     = "integer"
	typeBigint      = "bigint"
	typeText        = "text"
	typeVarchar     = "character varying"
	typeBoolean     = "boolean"
	typeTimestamptz = "timestamp with time zone"
	typeReal        = "real"
	typeJSONB       = "jsonb"
)

// col and metricCol keep the static source list below readable.
func col(name, dataType string) datastoreColumn {
	return datastoreColumn{Name: name, DataType: dataType}
}

// redactedTextCol is a column whose free text is passed through
// redactedText.
func redactedTextCol(name, dataType string) datastoreColumn {
	return datastoreColumn{Name: name, DataType: dataType, RedactText: true}
}

func metricCol(name, dataType string) datastoreColumn {
	return datastoreColumn{Name: name, DataType: dataType, IsMetric: true}
}

// configurationSources returns the static list of configuration
// tables. Each entry lists its readable columns explicitly; stored
// credentials (connections.password_encrypted, sslcert, sslkey,
// sslrootcert), owner tokens, and every notification channel secret are
// deliberately absent, and TestConfigurationSourcesExposeNoSecrets
// enforces that.
//
// notification_channels is left out: its REST endpoints require the
// manage_notification_channels admin permission, which no connection
// scoping rule can express.
func configurationSources() []datastoreSource {
	return []datastoreSource{
		connectionsSource(),
		alertsSource(),
		alertAcknowledgmentsSource(),
		alertRulesSource(),
		alertThresholdsSource(),
		blackoutsSource(),
		blackoutSchedulesSource(),
		probeConfigsSource(),
		alerterSettingsSource(),
		metricBaselinesSource(),
		anomalyCandidatesSource(),
		correlationGroupsSource(),
		clustersSource(),
		clusterGroupsSource(),
	}
}

// connectionsSource is the connections configuration source.
func connectionsSource() datastoreSource {
	return datastoreSource{
		Name:             "connections",
		Table:            "connections",
		Description:      "Registered monitored connections. Stored passwords and certificate paths are not available.",
		ConnectionColumn: "id",
		Kind:             datastoreKindConfiguration,
		Scope:            scopeConnection,
		Columns: []datastoreColumn{
			col("id", typeInteger),
			col("name", typeVarchar),
			col("description", typeText),
			col("host", typeVarchar),
			col("hostaddr", typeVarchar),
			col("port", typeInteger),
			col("database_name", typeVarchar),
			col("username", typeVarchar),
			col("sslmode", typeVarchar),
			col("is_monitored", typeBoolean),
			col("is_shared", typeBoolean),
			col("enabled", typeBoolean),
			col("owner_username", typeVarchar),
			col("cluster_id", typeInteger),
			col("role", typeVarchar),
			col("membership_source", typeVarchar),
			redactedTextCol("connection_error", typeText),
			col("created_at", typeTimestamptz),
			col("updated_at", typeTimestamptz),
		},
	}
}

// alertsSource is the alerts configuration source.
func alertsSource() datastoreSource {
	return datastoreSource{
		Name:             "alerts",
		Table:            "alerts",
		Description:      "Threshold, anomaly, connection and system alerts raised by the alerter. For a formatted view use get_alert_history.",
		TimeColumn:       "triggered_at",
		ConnectionColumn: "connection_id",
		Kind:             datastoreKindConfiguration,
		Scope:            scopeAlert,
		Columns: []datastoreColumn{
			col("id", typeBigint),
			col("alert_type", typeText),
			col("rule_id", typeBigint),
			col("connection_id", typeInteger),
			col("database_name", typeText),
			col("probe_name", typeText),
			col("metric_name", typeText),
			metricCol("metric_value", typeReal),
			metricCol("threshold_value", typeReal),
			col("operator", typeText),
			col("severity", typeText),
			col("title", typeText),
			redactedTextCol("description", typeText),
			col("object_name", typeText),
			col("correlation_id", typeText),
			col("status", typeText),
			col("triggered_at", typeTimestamptz),
			col("cleared_at", typeTimestamptz),
			col("last_updated", typeTimestamptz),
			metricCol("anomaly_score", typeReal),
			col("anomaly_details", typeJSONB),
			redactedTextCol("ai_analysis", typeText),
			metricCol("ai_analysis_metric_value", typeReal),
			col("last_reevaluated_at", typeTimestamptz),
			metricCol("reevaluation_count", typeInteger),
			col("reevaluation_fingerprint", typeText),
		},
	}
}

// alertAcknowledgmentsSource is the alert_acknowledgments configuration source.
func alertAcknowledgmentsSource() datastoreSource {
	return datastoreSource{
		Name:        "alert_acknowledgments",
		Table:       "alert_acknowledgments",
		Description: "Acknowledgements recorded against alerts.",
		TimeColumn:  "acknowledged_at",
		Kind:        datastoreKindConfiguration,
		Scope:       scopeViaAlert,
		Columns: []datastoreColumn{
			col("id", typeBigint),
			col("alert_id", typeBigint),
			col("acknowledged_by", typeText),
			col("acknowledged_at", typeTimestamptz),
			col("acknowledge_type", typeText),
			col("message", typeText),
			col("false_positive", typeBoolean),
		},
	}
}

// alertRulesSource is the alert_rules configuration source.
func alertRulesSource() datastoreSource {
	return datastoreSource{
		Name:        "alert_rules",
		Table:       "alert_rules",
		Description: "Alert rule definitions and their default thresholds.",
		Kind:        datastoreKindConfiguration,
		Scope:       scopeGlobal,
		Columns: []datastoreColumn{
			col("id", typeBigint),
			col("name", typeText),
			col("description", typeText),
			col("category", typeText),
			col("metric_name", typeText),
			col("metric_unit", typeText),
			col("default_operator", typeText),
			metricCol("default_threshold", typeReal),
			col("default_severity", typeText),
			col("default_enabled", typeBoolean),
			col("required_extension", typeText),
			col("is_built_in", typeBoolean),
			col("created_at", typeTimestamptz),
		},
	}
}

// alertThresholdsSource is the alert_thresholds configuration source.
func alertThresholdsSource() datastoreSource {
	return datastoreSource{
		Name:             "alert_thresholds",
		Table:            "alert_thresholds",
		Description:      "Per-server, per-cluster and per-group overrides of alert rule thresholds.",
		ConnectionColumn: "connection_id",
		Kind:             datastoreKindConfiguration,
		Scope:            scopeHierarchy,
		Columns: []datastoreColumn{
			col("id", typeBigint),
			col("rule_id", typeBigint),
			col("scope", typeText),
			col("connection_id", typeInteger),
			col("cluster_id", typeInteger),
			col("group_id", typeInteger),
			col("database_name", typeText),
			col("operator", typeText),
			metricCol("threshold", typeReal),
			col("severity", typeText),
			col("enabled", typeBoolean),
			col("created_at", typeTimestamptz),
			col("updated_at", typeTimestamptz),
		},
	}
}

// blackoutsSource is the blackouts configuration source.
func blackoutsSource() datastoreSource {
	return datastoreSource{
		Name:             "blackouts",
		Table:            "blackouts",
		Description:      "One-off blackout (maintenance) windows at estate, group, cluster or server scope. Query blackout_schedules too, or use get_blackouts.",
		TimeColumn:       "start_time",
		ConnectionColumn: "connection_id",
		Kind:             datastoreKindConfiguration,
		Scope:            scopeHierarchy,
		Columns: []datastoreColumn{
			col("id", typeBigint),
			col("scope", typeText),
			col("connection_id", typeInteger),
			col("cluster_id", typeInteger),
			col("group_id", typeInteger),
			col("database_name", typeText),
			col("reason", typeText),
			col("start_time", typeTimestamptz),
			col("end_time", typeTimestamptz),
			col("created_by", typeText),
			col("created_at", typeTimestamptz),
		},
	}
}

// blackoutSchedulesSource is the blackout_schedules configuration source.
func blackoutSchedulesSource() datastoreSource {
	return datastoreSource{
		Name:             "blackout_schedules",
		Table:            "blackout_schedules",
		Description:      "Recurring blackout schedules (cron expressions) at estate, group, cluster or server scope.",
		ConnectionColumn: "connection_id",
		Kind:             datastoreKindConfiguration,
		Scope:            scopeHierarchy,
		Columns: []datastoreColumn{
			col("id", typeBigint),
			col("scope", typeText),
			col("connection_id", typeInteger),
			col("cluster_id", typeInteger),
			col("group_id", typeInteger),
			col("database_name", typeText),
			col("name", typeText),
			col("cron_expression", typeText),
			metricCol("duration_minutes", typeInteger),
			col("timezone", typeText),
			col("reason", typeText),
			col("enabled", typeBoolean),
			col("created_by", typeText),
			col("created_at", typeTimestamptz),
			col("updated_at", typeTimestamptz),
		},
	}
}

// probeConfigsSource is the probe_configs configuration source.
func probeConfigsSource() datastoreSource {
	return datastoreSource{
		Name:             "probe_configs",
		Table:            "probe_configs",
		Description:      "Probe collection settings at global, group, cluster or server scope.",
		ConnectionColumn: "connection_id",
		Kind:             datastoreKindConfiguration,
		Scope:            scopeHierarchy,
		Columns: []datastoreColumn{
			col("id", typeInteger),
			col("name", typeText),
			col("description", typeText),
			col("scope", typeText),
			col("connection_id", typeInteger),
			col("cluster_id", typeInteger),
			col("group_id", typeInteger),
			col("is_enabled", typeBoolean),
			metricCol("collection_interval_seconds", typeInteger),
			metricCol("retention_days", typeInteger),
			col("user_modified", typeBoolean),
			col("created_at", typeTimestamptz),
			col("updated_at", typeTimestamptz),
		},
	}
}

// alerterSettingsSource is the alerter_settings configuration source.
func alerterSettingsSource() datastoreSource {
	return datastoreSource{
		Name:        "alerter_settings",
		Table:       "alerter_settings",
		Description: "Estate-wide alerter settings (a single row).",
		Kind:        datastoreKindConfiguration,
		Scope:       scopeGlobal,
		Columns: []datastoreColumn{
			col("id", typeInteger),
			metricCol("retention_days", typeInteger),
			col("default_anomaly_enabled", typeBoolean),
			metricCol("default_anomaly_sensitivity", typeReal),
			metricCol("baseline_refresh_interval_mins", typeInteger),
			metricCol("correlation_window_seconds", typeInteger),
			col("updated_at", typeTimestamptz),
		},
	}
}

// metricBaselinesSource is the metric_baselines configuration source.
func metricBaselinesSource() datastoreSource {
	return datastoreSource{
		Name:             "metric_baselines",
		Table:            "metric_baselines",
		Description:      "Statistical baselines the alerter's anomaly detection compares metrics against. For a formatted view use get_metric_baselines.",
		ConnectionColumn: "connection_id",
		Kind:             datastoreKindConfiguration,
		Scope:            scopeConnection,
		Columns: []datastoreColumn{
			col("id", typeBigint),
			col("connection_id", typeInteger),
			col("database_name", typeText),
			col("metric_name", typeText),
			col("period_type", typeText),
			col("day_of_week", typeInteger),
			col("hour_of_day", typeInteger),
			metricCol("mean", typeReal),
			metricCol("stddev", typeReal),
			metricCol("min", typeReal),
			metricCol("max", typeReal),
			metricCol("sample_count", typeBigint),
			col("last_calculated", typeTimestamptz),
			col("earliest_sample_at", typeTimestamptz),
		},
	}
}

// anomalyCandidatesSource is the anomaly_candidates configuration source.
func anomalyCandidatesSource() datastoreSource {
	return datastoreSource{
		Name:             "anomaly_candidates",
		Table:            "anomaly_candidates",
		Description:      "Metric values the anomaly detector flagged, with each tier's verdict.",
		TimeColumn:       "detected_at",
		ConnectionColumn: "connection_id",
		Kind:             datastoreKindConfiguration,
		Scope:            scopeConnection,
		Columns: []datastoreColumn{
			col("id", typeBigint),
			col("connection_id", typeInteger),
			col("database_name", typeText),
			col("metric_name", typeText),
			metricCol("metric_value", typeReal),
			metricCol("z_score", typeReal),
			col("detected_at", typeTimestamptz),
			col("context", typeJSONB),
			col("tier1_pass", typeBoolean),
			metricCol("tier2_score", typeReal),
			col("tier2_pass", typeBoolean),
			col("tier3_result", typeText),
			col("tier3_pass", typeBoolean),
			col("tier3_source", typeText),
			col("tier3_model", typeText),
			col("tier3_error", typeText),
			col("final_decision", typeText),
			col("alert_id", typeBigint),
			col("processed_at", typeTimestamptz),
		},
	}
}

// correlationGroupsSource is the correlation_groups configuration source.
func correlationGroupsSource() datastoreSource {
	return datastoreSource{
		Name:             "correlation_groups",
		Table:            "correlation_groups",
		Description:      "Groups of anomalies the alerter correlated in time on one connection.",
		TimeColumn:       "start_time",
		ConnectionColumn: "connection_id",
		Kind:             datastoreKindConfiguration,
		Scope:            scopeConnection,
		Columns: []datastoreColumn{
			col("id", typeBigint),
			col("connection_id", typeInteger),
			col("database_name", typeText),
			col("start_time", typeTimestamptz),
			col("end_time", typeTimestamptz),
			metricCol("anomaly_count", typeInteger),
			col("root_cause_guess", typeText),
		},
	}
}

// clustersSource is the clusters configuration source.
func clustersSource() datastoreSource {
	return datastoreSource{
		Name:        "clusters",
		Table:       "clusters",
		Description: "Database clusters; a connection's cluster is connections.cluster_id.",
		Kind:        datastoreKindConfiguration,
		Scope:       scopeCluster,
		Columns: []datastoreColumn{
			col("id", typeInteger),
			col("group_id", typeInteger),
			col("name", typeVarchar),
			col("description", typeText),
			col("replication_type", typeVarchar),
			col("auto_cluster_key", typeVarchar),
			col("dismissed", typeBoolean),
			col("created_at", typeTimestamptz),
			col("updated_at", typeTimestamptz),
		},
	}
}

// clusterGroupsSource is the cluster_groups configuration source.
func clusterGroupsSource() datastoreSource {
	return datastoreSource{
		Name:        "cluster_groups",
		Table:       "cluster_groups",
		Description: "Groups of clusters; a cluster's group is clusters.group_id.",
		Kind:        datastoreKindConfiguration,
		Scope:       scopeGroup,
		Columns: []datastoreColumn{
			col("id", typeInteger),
			col("name", typeVarchar),
			col("description", typeText),
			col("owner_username", typeVarchar),
			col("is_shared", typeBoolean),
			col("is_default", typeBoolean),
			col("auto_group_key", typeVarchar),
			col("created_at", typeTimestamptz),
			col("updated_at", typeTimestamptz),
		},
	}
}

// secretColumnFragments are name fragments that mark a column as
// holding a credential. A metric column whose name contains one is never
// offered, so a future probe cannot expose a secret by accident. The
// connection string fragments cover pg_stat_wal_receiver.conninfo and
// the like: PostgreSQL obscures the password in some of them, but the
// rest of a connection string is still credential material.
var secretColumnFragments = []string{
	"password", "passwd", "passphrase", "secret", "token", "credential", "private_key",
	"conninfo", "connstr", "connection_string", "dsn",
	"sslcert", "sslkey", "sslrootcert",
}

// secretNamePatterns are the regular expressions, matched against a
// lower-cased setting or option name, that mark its value as possibly
// holding a credential: every secretColumnFragments entry, plus
// patterns too broad for column names. Masking a harmless value is
// acceptable; exposing a credential is not.
var secretNamePatterns = append(slices.Clone(secretColumnFragments),
	"pass$", "key$", "api_?key", "access_key", "salt")

// textSecretNamePatterns are the names whose value is masked in free
// text: the secretColumnFragments, which are specific enough not to
// catch ordinary identifiers such as sort_key or bypass, plus the API
// and access key names.
var textSecretNamePatterns = append(slices.Clone(secretColumnFragments), "api_?key", "access_key")

// nonCredentialSettings match secretNamePatterns but hold no secret, so
// pg_settings shows their values. The list is pkg/pgsettings', which the
// collector also uses.
var nonCredentialSettings = pgsettings.NonCredentialSettings()

// isSecretColumnName reports whether a column name looks like it holds
// a credential or other secret.
func isSecretColumnName(name string) bool {
	lower := strings.ToLower(name)
	for _, fragment := range secretColumnFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

// metricRedactions lists, per probe, the values that can carry a
// credential no column-name check can catch, so they are masked:
// Columns hold a setting's value and KeyColumn names the setting (a
// credential-bearing setting such as primary_conninfo can hold a
// password), OptionColumns are text[] columns of name=value options
// (pg_hba.conf's ldapbindpasswd and radiussecrets hold one in clear
// text), and TextColumns hold free text, such as SQL statements and
// replicated row images, that can quote a password. WholeColumns can
// quote a credential in a form no pattern catches, as
// pg_hba_file_rules.error quotes an option it cannot parse, so a
// non-empty value is masked entirely. Note is added to the probe's
// description.
var metricRedactions = map[string]struct {
	KeyColumn     string
	Columns       []string
	OptionColumns []string
	TextColumns   []string
	WholeColumns  []string
	Note          string
}{
	"pg_settings": {
		KeyColumn: "name", Columns: []string{"setting", "reset_val", "boot_val"},
		Note: redactionNote,
	},
	"pg_hba_file_rules": {
		OptionColumns: []string{"options"}, WholeColumns: []string{"error"},
		Note: optionsRedactionNote,
	},
	"pg_stat_activity":   {TextColumns: []string{"query"}, Note: textRedactionNote},
	"pg_stat_statements": {TextColumns: []string{"query"}, Note: textRedactionNote},
	"spock_exception_log": {
		TextColumns: []string{"ddl_statement", "error_message", "local_tup", "remote_old_tup", "remote_new_tup"},
		Note:        textRedactionNote + " Row images are returned as text.",
	},
	"spock_resolutions": {TextColumns: []string{"local_tuple", "remote_tuple"}, Note: textRedactionNote},
}

// credentialSettings are setting names whose value can carry a
// credential: primary_conninfo is a libpq connection string that may
// hold password=, and the rest are shell commands that may carry a
// passphrase or an inline secret such as PGPASSWORD=... or
// AWS_SECRET_ACCESS_KEY=.... Any setting whose name contains a
// secretNamePatterns entry is masked as well, which covers an extension
// setting such as myext.password. The list is pkg/pgsettings', from
// which the collector redacts the same settings before storing them.
var credentialSettings = pgsettings.CredentialSettings()

// redactedValue replaces a masked value, as PostgreSQL masks a password
// in pg_stat_wal_receiver.conninfo.
const redactedValue = "********"

// redactionNote and optionsRedactionNote are appended to the
// description of a probe in metricRedactions, so describe_datastore says
// which values are masked.
const (
	redactionNote = "Values of settings that can hold a credential, such as primary_conninfo, are shown as " +
		redactedValue + "."
	optionsRedactionNote = "Values of options that can hold a credential, such as ldapbindpasswd and " +
		"radiussecrets, are shown as " + redactedValue + ", and so is a non-empty error, which can quote an " +
		"option."
	textRedactionNote = "Passwords in known forms in SQL text and row data, such as PASSWORD '...', " +
		"password=... in a connection string and the value of a JSON key naming a password or secret, " +
		"are shown as " + redactedValue + "; other sensitive text, including passwords in other forms, " +
		"is returned as stored."
)

// textRedaction is one password form redactedText masks: Pattern is a
// case-insensitive regular expression in the subset PostgreSQL's
// advanced regular expressions share with Go's RE2, so the unit tests
// can check it in Go, and Replacement keeps capture group 1 (\1), the
// only capturing group, and puts redactedValue in place of the secret.
// Every quantifier is greedy, so PostgreSQL takes the longest match, as
// Go does in the tests.
type textRedaction struct {
	Name        string
	Pattern     string
	Replacement string
}

// secretKeyPattern returns a regular expression with no capturing
// group that matches a name holding a textSecretNamePatterns entry,
// such as password, AWS_SECRET_ACCESS_KEY or app.api_key: lead and
// trail are what may come before and after the entry.
func secretKeyPattern(lead, trail string) string {
	return lead + "(?:" + strings.Join(textSecretNamePatterns, "|") + ")" + trail
}

// Pieces of the textRedactions patterns. A quoted value may lack its
// closing quote, since pg_stat_activity truncates long statements, and
// a quoted value accepts both a doubled quote and a backslash escape,
// so an escaped quote never ends it early. Row images are masked as
// JSON text, where every backslash is doubled, so an escape backslash
// (escapeBackslash) may be one backslash or two: in SQL text that can
// mask more than the value, but never less. White space is the ASCII
// white space SQL itself accepts, spelt out so that PostgreSQL, whose
// [[:space:]] follows the database locale, and Go agree.
const (
	// ws is one ASCII white space character, and notWS starts a
	// bracket expression that excludes them.
	ws    = `[ \t\n\r\f\v]`
	notWS = `[^ \t\n\r\f\v`
	// escapeBackslash is the backslash that starts an escape, doubled
	// in JSON text.
	escapeBackslash = `\\\\?`
	// sqlQuoted is a single-quoted SQL string.
	sqlQuoted = `'(?:''|` + escapeBackslash + `.|[^'\\])*'?`
	// sqlDollarQuoted is a dollar-quoted SQL string, $$...$$ or
	// $tag$...$tag$. Without back-references the closing tag cannot be
	// matched to the opening one, so a tagged string ends at the first
	// $name$; a parameter such as $1 is not a dollar quote.
	sqlDollarQuoted = `\$\$(?:[^$]|\$[^$])*(?:\$\$)?` +
		`|\$[a-z_][a-z0-9_]*\$(?:[^$]|\$\$|\$[^a-z_$]|\$[a-z_][a-z0-9_]*(?:[^a-z0-9_$]|$))*(?:\$[a-z_][a-z0-9_]*\$)?`
	// sqlConstant is any SQL string constant: '...', E'...', N'...',
	// U&'...' or dollar-quoted.
	sqlConstant = `(?:(?:e|n|u&)?` + sqlQuoted + `|` + sqlDollarQuoted + `)`
	// sqlNestedQuoted is a quoted value inside a SQL literal, as in
	// CONNECTION 'password=''secret''', where its quotes are doubled
	// and an escaped quote is '''' or \''.
	sqlNestedQuoted = `''(?:''''|` + escapeBackslash + `''|[^'])*(?:'')?`
	// sqlBackslashQuoted is a quoted value inside an escape string, as
	// in CONNECTION E'password=\'secret\'', where its quotes are \', an
	// escaped quote is \\\', an escaped backslash is \\\\ (the
	// conninfo escape \\, escaped again for the escape string) and a
	// bare quote can only be doubled. A backslash followed by a quote
	// always ends the value or escapes the quote, so in JSON text, where
	// the closing \' is \\', the value cannot run on past it.
	sqlBackslashQuoted = escapeBackslash + `'(?:` +
		escapeBackslash + escapeBackslash + escapeBackslash + escapeBackslash + `|` +
		escapeBackslash + escapeBackslash + escapeBackslash + `'|` +
		escapeBackslash + escapeBackslash + `[^'\\]|` + escapeBackslash + `[^'\\]|''|[^'\\])*(?:` +
		escapeBackslash + `')?`
	// nameQuote is the double quote that may close a quoted name, as in
	// SET "app.password", escaped as \" in JSON text and as \\\" in
	// JSON text inside JSON text.
	nameQuote = `(?:(?:\\|\\\\\\)?")?`
	// doubleQuoted is a double-quoted value, as in PGPASSWORD="secret",
	// and jsonDoubleQuoted is one in JSON text, as in
	// PGPASSWORD=\"secret\" inside a row image.
	doubleQuoted     = `"(?:[^"\\]|\\.)*"?`
	jsonDoubleQuoted = `\\"(?:[^\\]|\\[^"])*(?:\\")?`
	// secretValue is a value that follows a secret name: any SQL string
	// constant or one nested in another literal.
	secretValue = `(?:` + sqlConstant + `|` + sqlNestedQuoted + `|` + sqlBackslashQuoted + `)`
	// blank is one white space character. In JSON text a line break
	// is \n, so an escaped \n, \r or \t, with one backslash or two,
	// counts as white space there.
	blank = `(?:` + ws + `|` + escapeBackslash + `[nrt])`
	// blockComment is a closed /* */ comment whose body is at most 32
	// characters, or runs of stars each followed by one character, and
	// lineComment a -- comment, which runs to the end of the line (\n
	// in JSON text). A longer or nested comment is not matched, so a
	// value behind one is not masked.
	blockComment = `/\*(?:[^*]|\*+[^*/]){0,32}\*+/`
	lineComment  = `--(?:[^\n\\]|\\[^n])*`
	// sep is white space with up to two comments, and sep1 is the
	// same but not empty. Requiring a block comment to be closed and
	// short, and bounding the number of comments, keeps a run of
	// comments from making a match scan to the end of the text at every
	// secret name, which took minutes on a megabyte of password=/* .
	sep  = blank + `*(?:(?:` + blockComment + `|` + lineComment + `)` + blank + `*){0,2}`
	sep1 = `(?:` + blank + `|` + blockComment + `|` + lineComment + `)` + sep
	// identChars may surround a secret name, as in app.password or
	// AWS_SECRET_ACCESS_KEY.
	identChars = `[a-z0-9_.]*`
)

// maxTextLength is the length, in characters, to which redactedText
// cuts a value before masking it, so that the time a crafted value can
// take to mask is bounded (about a second at most). Text longer than
// this is returned cut short.
const maxTextLength = 65536

// textRedactions are the usual places a password appears in SQL text,
// connection strings, shell commands and row data. They are applied in
// order, as regexp_replace calls inside the scoped subquery, so a
// caller's filters and aggregates see only the masked text. A secret name is any name holding a
// textSecretNamePatterns entry. Masking is best effort: a password in
// any other form is returned as stored.
var textRedactions = []textRedaction{
	{
		// scheme://user:secret@host, including a truncated one that ends
		// before the @. A user name may hold a doubled quote, as in
		// 'postgresql://o''brien:secret@host', or a double quote.
		Name:        "URI userinfo",
		Pattern:     `([a-z][a-z0-9+.-]*://(?:` + notWS + `:/@']|'')*:)` + notWS + `@]+(?:@|$)`,
		Replacement: `\1` + redactedValue + `@`,
	},
	{
		// "password": value in a JSON row image, whatever the value's
		// type, and the bare keys "pass" and "pwd"; a nested value is
		// masked to its first closing bracket.
		Name: "JSON key",
		Pattern: `((?:` + secretKeyPattern(`"[^"]*`, `[^"]*`) + `|"(?:pass|pwd)\\?)"` + ws + `*:` + ws + `*)` +
			`(?:"(?:[^"\\]|\\.)*"?|\{[^}]*\}?|\[[^\]]*\]?|` + notWS + `,}\]]+)`,
		Replacement: `\1"` + redactedValue + `"`,
	},
	{
		// ALTER SYSTEM SET archive_command = '...' and the other
		// credentialSettings.
		Name: "SET credential setting",
		Pattern: `(set` + sep1 + nameQuote + `(?:` + strings.Join(credentialSettings, "|") + `)` + nameQuote +
			`(?:` + sep + `=|` + sep1 + `to)` + sep + `)` + sqlConstant,
		Replacement: `\1'` + redactedValue + `'`,
	},
	{
		// set_config('app.password', 'secret', false), with or without a
		// cast on the name or the new_value => argument name, and
		// json_build_object('password', 'secret').
		Name: "quoted name, value",
		Pattern: `(` + secretKeyPattern(`'[^']*`, `[^']*`) + `'(?:::[a-z_ ]+)?` + sep + `,` + sep +
			`(?:new_value` + sep + `=>` + sep + `)?)` + sqlConstant,
		Replacement: `\1'` + redactedValue + `'`,
	},
	{
		// password=secret in a libpq connection string, ldapbindpasswd=
		// and radiussecrets= in pg_hba.conf, PGPASSWORD=... and
		// AWS_SECRET_ACCESS_KEY=... in a shell command, and
		// SET app.password = '...'. A quoted value may follow white
		// space or up to two /* */ comments, which are masked with it.
		// An unquoted value is masked to the next white space that no
		// backslash escapes, as in password=correct\ horse, whatever it
		// starts with, so a value such as --secret is not taken for a
		// comment; a parameter such as $1 is kept when white space, a
		// comma, a bracket, a semicolon, a double quote or a backslash
		// ends it.
		Name: "keyword=value",
		Pattern: `(` + secretKeyPattern(identChars, identChars) + nameQuote + sep + `(?:=>?|:=)` + ws + `*)(?:` +
			`(?:(?:` + blockComment + `|` + escapeBackslash + `[nrt])` + blank + `*){0,2}` +
			`(?:` + secretValue + `|` + doubleQuoted + `|` + jsonDoubleQuoted + `)|` +
			`(?:` + blockComment + `)*(?:` + notWS + `$'"/]|/` + notWS + `*]|\$` + notWS + `0-9]|\$[0-9]+` + notWS + `0-9,);"\\])` +
			`(?:` + escapeBackslash + `[^\n\r]|` + notWS + `])*)`,
		Replacement: `\1` + redactedValue,
	},
	{
		// EXECUTE 'ALTER ROLE app PASSWORD ' || '''secret''', where the
		// value is a quoted value in the next literal.
		Name: "concatenated PASSWORD",
		Pattern: `(` + secretKeyPattern(identChars, identChars) + nameQuote + sep + `'` + sep + `\|\|` + sep + `'` +
			ws + `*)` + sqlNestedQuoted,
		Replacement: `\1''` + redactedValue + `''`,
	},
	{
		// [ENCRYPTED] PASSWORD '...' in CREATE or ALTER ROLE and USER,
		// password '...' in a user mapping's OPTIONS, and
		// SET app.password TO '...'. White space or a comment may
		// separate the name from the value, or nothing at all, as in
		// PASSWORD'...'. A name that starts a quoted string, as in
		// EXECUTE 'ALTER ROLE app ' || 'PASSWORD ''...''', needs a
		// separator after it, so a quoted name followed by a comma, as in
		// set_config('app.password', ...), is left to the quoted name
		// pattern. PASSWORD NULL and a $1 parameter are kept.
		Name: "PASSWORD 'value'",
		Pattern: `((?:(?:^|[^'a-z0-9_.])` + secretKeyPattern(identChars, identChars) + nameQuote + sep + `|` +
			secretKeyPattern(`'`+identChars, identChars) + nameQuote + sep1 + `)` +
			`(?:to` + sep + `)?)` + secretValue,
		Replacement: `\1'` + redactedValue + `'`,
	},
}

// sqlStringLiteral quotes a constant as an escape string literal,
// doubling quotes and backslashes, so it means the same whatever
// standard_conforming_strings is set to. It is used only for the
// constants in textRedactions, never caller input.
func sqlStringLiteral(v string) string {
	return "E'" + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), "'", "''") + "'"
}

// redactedText returns value, cast to text and cut to maxTextLength
// characters, with every textRedactions pattern masked. A NULL value
// stays NULL.
func redactedText(value string) string {
	out := "left(" + value + "::text, " + strconv.Itoa(maxTextLength) + ")"
	for _, r := range textRedactions {
		out = "regexp_replace(" + out + ", " + sqlStringLiteral(r.Pattern) + ", " +
			sqlStringLiteral(r.Replacement) + ", 'gi')"
	}
	return out
}

// redactMetricColumns marks the value columns of probe name for masking.
// If the probe has no column naming the setting, the value columns
// cannot be masked row by row, so they are dropped instead.
func redactMetricColumns(name string, columns []datastoreColumn) []datastoreColumn {
	rule, ok := metricRedactions[name]
	if !ok {
		return columns
	}
	hasKey := slices.ContainsFunc(columns, func(c datastoreColumn) bool { return c.Name == rule.KeyColumn })
	out := make([]datastoreColumn, 0, len(columns))
	for _, c := range columns {
		c.MaskOptions = slices.Contains(rule.OptionColumns, c.Name)
		c.MaskWhole = slices.Contains(rule.WholeColumns, c.Name)
		if slices.Contains(rule.TextColumns, c.Name) {
			// A row image is masked as text and returned as text, since
			// masking can leave it invalid as JSON.
			c.RedactText = true
			if c.DataType != typeText {
				c.StoredType = c.DataType
				c.DataType = typeText
			}
		}
		if slices.Contains(rule.Columns, c.Name) {
			if !hasKey {
				continue
			}
			c.RedactBy = rule.KeyColumn
		}
		out = append(out, c)
	}
	return out
}

// credentialSettingCondition is true for a row whose setting name, in
// column key of alias, is a credentialSettings entry or contains a
// secretNamePatterns entry. Every literal is a constant above, checked
// by TestCredentialSettingLiteralsAreSafe, so nothing from the caller
// reaches it.
func credentialSettingCondition(alias, key string) string {
	lhs := "lower(" + qualified(alias, key) + ")"
	quoted := make([]string, len(credentialSettings))
	for i, v := range credentialSettings {
		quoted[i] = "'" + v + "'"
	}
	exempt := make([]string, len(nonCredentialSettings))
	for i, v := range nonCredentialSettings {
		exempt[i] = "'" + v + "'"
	}
	return "(" + lhs + " IN (" + strings.Join(quoted, ", ") + ") OR (" +
		secretNameMatch(lhs) + " AND " + lhs + " NOT IN (" + strings.Join(exempt, ", ") + ")))"
}

// secretNameMatch is true when the lower-cased name lhs matches a
// secretNamePatterns entry.
func secretNameMatch(lhs string) string {
	return lhs + " ~ '(" + strings.Join(secretNamePatterns, "|") + ")'"
}

// maskedOptions returns value, a text[] of name=value options, with the
// value of every option whose name matches secretNamePatterns replaced
// by redactedValue, keeping the options' order. A NULL array stays NULL.
func maskedOptions(value string) string {
	const opt = "o.opt"
	name := "split_part(" + opt + ", '=', 1)"
	return "CASE WHEN " + value + " IS NULL THEN NULL ELSE ARRAY(SELECT CASE WHEN strpos(" + opt +
		", '=') > 0 AND " + secretNameMatch("lower("+name+")") + " THEN " + name + " || '=" +
		redactedValue + "' ELSE " + opt + " END FROM unnest(" + value +
		") WITH ORDINALITY AS o(opt, n) ORDER BY o.n) END"
}

// errUnknownDatastoreSource reports a source name the source list does not
// hold. Its text names describe_datastore so a caller can recover.
var errUnknownDatastoreSource = errors.New("unknown source: call describe_datastore to list the sources query_datastore accepts")

// datastoreMetricTableQuery finds a parent probe table in the metrics
// schema. Partitions are excluded by relispartition rather than by name,
// so a probe whose name happens to contain "_p" is still found.
const datastoreMetricTableQuery = `
        SELECT COALESCE(obj_description(c.oid, 'pg_class'), '')
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'metrics'
            AND c.relname = $1
            AND c.relkind IN ('r', 'p')
            AND NOT c.relispartition
    `

// datastoreMetricColumnsQuery lists a probe table's columns.
const datastoreMetricColumnsQuery = `
        SELECT column_name, data_type
        FROM information_schema.columns
        WHERE table_schema = 'metrics'
            AND table_name = $1
        ORDER BY ordinal_position
    `

// lookupDatastoreSource returns the source entry for name: a static
// configuration source, or a metric source discovered from the live
// metrics schema. An unknown name returns errUnknownDatastoreSource.
func lookupDatastoreSource(ctx context.Context, pool *pgxpool.Pool, name string) (datastoreSource, error) {
	sources := configurationSources()
	for i := range sources {
		if sources[i].Name == name {
			return sources[i], nil
		}
	}

	if !metrics.IsValidIdentifier(name) {
		return datastoreSource{}, errUnknownDatastoreSource
	}
	if pool == nil {
		return datastoreSource{}, errors.New("datastore not configured")
	}

	description, err := lookupMetricTable(ctx, pool, name)
	if err != nil {
		return datastoreSource{}, err
	}
	columns, err := lookupMetricColumns(ctx, pool, name)
	if err != nil {
		return datastoreSource{}, err
	}
	columns = redactMetricColumns(name, columns)

	return datastoreSource{
		Name:             name,
		Table:            name,
		Description:      metricDescription(name, description),
		TimeColumn:       metricTimeColumn,
		ConnectionColumn: metricConnectionColumn,
		Kind:             datastoreKindMetric,
		Scope:            scopeConnection,
		Columns:          columns,
	}, nil
}

// metricDescription returns the description of probe name, from the
// table's comment or a default, noting any values that are masked.
func metricDescription(name, comment string) string {
	description := strings.TrimSpace(comment)
	if description == "" {
		description = fmt.Sprintf("Metrics collected by the %s probe.", name)
	}
	if rule, ok := metricRedactions[name]; ok {
		description += " " + rule.Note
	}
	return description
}

// lookupMetricTable returns the comment on the parent probe table name
// in the metrics schema, or errUnknownDatastoreSource when there is no
// such table.
func lookupMetricTable(ctx context.Context, pool *pgxpool.Pool, name string) (string, error) {
	// A relation name is unique within a schema, so there is at most one
	// row.
	var description string
	err := pool.QueryRow(ctx, datastoreMetricTableQuery, name).Scan(&description)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errUnknownDatastoreSource
	}
	if err != nil {
		return "", fmt.Errorf("failed to look up source: %w", err)
	}
	return description, nil
}

// lookupMetricColumns returns the columns of probe table name that a
// caller may read, leaving out secrets and names that are not valid
// identifiers. A table without the probe bookkeeping columns cannot be
// scoped to connections, so it is not a source at all, and gives
// errUnknownDatastoreSource.
func lookupMetricColumns(ctx context.Context, pool *pgxpool.Pool, name string) ([]datastoreColumn, error) {
	colRows, err := pool.Query(ctx, datastoreMetricColumnsQuery, name)
	if err != nil {
		return nil, fmt.Errorf("failed to read source columns: %w", err)
	}
	defer colRows.Close()

	var columns []datastoreColumn
	hasConnection, hasTime := false, false
	for colRows.Next() {
		var colName, dataType string
		if err := colRows.Scan(&colName, &dataType); err != nil {
			return nil, fmt.Errorf("failed to read source columns: %w", err)
		}
		if isSecretColumnName(colName) || !metrics.IsValidIdentifier(colName) {
			continue
		}
		hasConnection = hasConnection || colName == metricConnectionColumn
		hasTime = hasTime || colName == metricTimeColumn
		columns = append(columns, datastoreColumn{
			Name:     colName,
			DataType: dataType,
			IsMetric: metrics.IsMetricColumn(colName, dataType),
		})
	}
	if err := colRows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read source columns: %w", err)
	}
	if !hasConnection || !hasTime {
		return nil, errUnknownDatastoreSource
	}
	return columns, nil
}

// datastoreMetricSourcesQuery lists every parent probe table in the
// metrics schema that carries the bookkeeping columns
// lookupDatastoreSource requires, so describe_datastore lists exactly the
// metric sources query_datastore accepts.
const datastoreMetricSourcesQuery = `
        SELECT c.relname, COALESCE(obj_description(c.oid, 'pg_class'), '')
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'metrics'
            AND c.relkind IN ('r', 'p')
            AND NOT c.relispartition
            AND EXISTS (SELECT 1 FROM pg_attribute a
                        WHERE a.attrelid = c.oid AND a.attname = 'connection_id'
                            AND a.attnum > 0 AND NOT a.attisdropped)
            AND EXISTS (SELECT 1 FROM pg_attribute a
                        WHERE a.attrelid = c.oid AND a.attname = 'collected_at'
                            AND a.attnum > 0 AND NOT a.attisdropped)
        ORDER BY c.relname
    `

// datastoreSourceSummary is one row of describe_datastore's source list.
type datastoreSourceSummary struct {
	Name        string
	Kind        string
	TimeColumn  string
	Scope       datastoreScope
	Description string
}

// listDatastoreSources returns every source query_datastore accepts:
// the configuration sources, then the metric sources discovered from the
// live metrics schema, each group in name order. A probe table named
// like a configuration source is left out, since lookupDatastoreSource
// always resolves that name to the configuration source.
func listDatastoreSources(ctx context.Context, pool *pgxpool.Pool) ([]datastoreSourceSummary, error) {
	config := configurationSources()
	slices.SortFunc(config, func(a, b datastoreSource) int { return strings.Compare(a.Name, b.Name) })
	summaries := make([]datastoreSourceSummary, 0, len(config))
	configNames := make(map[string]bool, len(config))
	for i := range config {
		src := &config[i]
		configNames[src.Name] = true
		summaries = append(summaries, datastoreSourceSummary{
			Name:        src.Name,
			Kind:        src.Kind,
			TimeColumn:  src.TimeColumn,
			Scope:       src.Scope,
			Description: src.Description,
		})
	}

	rows, err := pool.Query(ctx, datastoreMetricSourcesQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to list metric sources: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, description string
		if err := rows.Scan(&name, &description); err != nil {
			return nil, fmt.Errorf("failed to list metric sources: %w", err)
		}
		// lookupDatastoreSource resolves a configuration source first, so
		// a probe table sharing a configuration source's name could never
		// be read as a metric source; listing it would be misleading.
		if !metrics.IsValidIdentifier(name) || configNames[name] {
			continue
		}
		description = metricDescription(name, description)
		summaries = append(summaries, datastoreSourceSummary{
			Name:        name,
			Kind:        datastoreKindMetric,
			TimeColumn:  metricTimeColumn,
			Scope:       scopeConnection,
			Description: description,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list metric sources: %w", err)
	}
	return summaries, nil
}
