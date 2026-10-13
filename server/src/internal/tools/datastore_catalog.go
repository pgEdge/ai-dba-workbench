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
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
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
		{
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
				col("port", typeInteger),
				col("database_name", typeVarchar),
				col("sslmode", typeVarchar),
				col("is_monitored", typeBoolean),
				col("is_shared", typeBoolean),
				col("enabled", typeBoolean),
				col("owner_username", typeVarchar),
				col("cluster_id", typeInteger),
				col("role", typeVarchar),
				col("membership_source", typeVarchar),
				col("connection_error", typeText),
				col("created_at", typeTimestamptz),
				col("updated_at", typeTimestamptz),
			},
		},
		{
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
				col("description", typeText),
				col("object_name", typeText),
				col("correlation_id", typeText),
				col("status", typeText),
				col("triggered_at", typeTimestamptz),
				col("cleared_at", typeTimestamptz),
				col("last_updated", typeTimestamptz),
				metricCol("anomaly_score", typeReal),
				col("anomaly_details", typeJSONB),
				col("ai_analysis", typeText),
				metricCol("ai_analysis_metric_value", typeReal),
				col("last_reevaluated_at", typeTimestamptz),
				metricCol("reevaluation_count", typeInteger),
				col("reevaluation_fingerprint", typeText),
			},
		},
		{
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
		},
		{
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
		},
		{
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
		},
		{
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
		},
		{
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
		},
		{
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
		},
		{
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
		},
		{
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
		},
		{
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
				col("tier3_error", typeText),
				col("final_decision", typeText),
				col("alert_id", typeBigint),
				col("processed_at", typeTimestamptz),
			},
		},
		{
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
		},
		{
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
		},
		{
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
		},
	}
}

// deniedMetricColumns lists probe columns never offered from a metric
// source. pg_stat_replication.conninfo copies pg_stat_wal_receiver's
// connection string; PostgreSQL obscures the password in it, but the
// rest of a connection string is still credential material.
var deniedMetricColumns = map[string]bool{
	"conninfo":    true,
	"sslcert":     true,
	"sslkey":      true,
	"sslrootcert": true,
}

// secretColumnFragments are name fragments that mark a column as
// holding a credential. A metric column whose name contains one is never
// offered, so a future probe cannot expose a secret by accident.
var secretColumnFragments = []string{"password", "passwd", "secret", "token", "credential", "private_key"}

// isSecretColumnName reports whether a column name looks like it holds
// a credential or other secret.
func isSecretColumnName(name string) bool {
	lower := strings.ToLower(name)
	if deniedMetricColumns[lower] {
		return true
	}
	for _, fragment := range secretColumnFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
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

	rows, err := pool.Query(ctx, datastoreMetricTableQuery, name)
	if err != nil {
		return datastoreSource{}, fmt.Errorf("failed to look up source: %w", err)
	}
	var description string
	found := false
	for rows.Next() {
		if err := rows.Scan(&description); err != nil {
			rows.Close()
			return datastoreSource{}, fmt.Errorf("failed to look up source: %w", err)
		}
		found = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return datastoreSource{}, fmt.Errorf("failed to look up source: %w", err)
	}
	if !found {
		return datastoreSource{}, errUnknownDatastoreSource
	}

	colRows, err := pool.Query(ctx, datastoreMetricColumnsQuery, name)
	if err != nil {
		return datastoreSource{}, fmt.Errorf("failed to read source columns: %w", err)
	}
	defer colRows.Close()

	var columns []datastoreColumn
	hasConnection, hasTime := false, false
	for colRows.Next() {
		var colName, dataType string
		if err := colRows.Scan(&colName, &dataType); err != nil {
			return datastoreSource{}, fmt.Errorf("failed to read source columns: %w", err)
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
		return datastoreSource{}, fmt.Errorf("failed to read source columns: %w", err)
	}

	// A table without the probe bookkeeping columns cannot be scoped to
	// connections, so it is not a source at all.
	if !hasConnection || !hasTime {
		return datastoreSource{}, errUnknownDatastoreSource
	}

	if description == "" {
		description = fmt.Sprintf("Metrics collected by the %s probe.", name)
	}
	return datastoreSource{
		Name:             name,
		Table:            name,
		Description:      strings.TrimSpace(description),
		TimeColumn:       metricTimeColumn,
		ConnectionColumn: metricConnectionColumn,
		Kind:             datastoreKindMetric,
		Scope:            scopeConnection,
		Columns:          columns,
	}, nil
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
// live metrics schema, each group in name order.
func listDatastoreSources(ctx context.Context, pool *pgxpool.Pool) ([]datastoreSourceSummary, error) {
	config := configurationSources()
	slices.SortFunc(config, func(a, b datastoreSource) int { return strings.Compare(a.Name, b.Name) })
	summaries := make([]datastoreSourceSummary, 0, len(config))
	for i := range config {
		src := &config[i]
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
		if !metrics.IsValidIdentifier(name) {
			continue
		}
		description = strings.TrimSpace(description)
		if description == "" {
			description = fmt.Sprintf("Metrics collected by the %s probe.", name)
		}
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
