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
	"slices"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
	"github.com/pgedge/ai-workbench/server/internal/testutil/scratchdb"
)

// These tests check the query_datastore catalog against the schema the
// collector really creates (issue #645). The fixtures in
// query_datastore_integration_test.go build their tables from the
// catalog itself, so they cannot notice a column the collector renamed,
// dropped, retyped or added; this file brings up the real schema with the
// collector's SchemaManager in a database of its own and compares.

// The collector schema is migrated into a scratch database of its own,
// named after the test database plus catalogSchemaSuffix, separate from
// the shared test database because other tests in this package and
// module create tables with the same names but different shapes there.
const catalogSchemaSuffix = "_catalog_schema"

// catalogSchemaColumnsQuery lists every column of every ordinary or
// partitioned table in the public schema.
const catalogSchemaColumnsQuery = `
        SELECT c.table_name, c.column_name, c.data_type
        FROM information_schema.columns c
        JOIN information_schema.tables t
            ON t.table_schema = c.table_schema AND t.table_name = c.table_name
        WHERE c.table_schema = 'public'
            AND t.table_type = 'BASE TABLE'
        ORDER BY c.table_name, c.ordinal_position
    `

// catalogOmittedColumns lists, per catalog table, the columns the
// collector creates that the catalog deliberately does not expose. A
// column the collector adds must be either cataloged or listed here, so
// a new column is a decision rather than something silently never
// offered.
var catalogOmittedColumns = map[string][]string{
	// Secrets; see TestConfigurationSourcesExposeNoSecrets.
	"connections": {
		"password_encrypted", "sslcert", "sslkey", "sslrootcert", "owner_token",
		// The source's LiveColumn: every row it returns has it NULL.
		"deleted_at",
	},
	// embedding_id refers to anomaly_embeddings, which query_datastore
	// never exposes.
	"anomaly_candidates": {"embedding_id"},
	// A secret; see TestConfigurationSourcesExposeNoSecrets.
	"cluster_groups": {"owner_token"},
	// tier1_scoring is the alerter's published scoring settings as one
	// JSONB document for the anomaly comparison endpoint (#348), not a
	// value query_datastore can filter or sort on.
	"alerter_settings": {"tier1_scoring"},
}

// collectorSchemaColumns returns the data type of every column of every
// public table, keyed by table then column.
func collectorSchemaColumns(t *testing.T, pool *pgxpool.Pool) map[string]map[string]string {
	t.Helper()
	rows, err := pool.Query(context.Background(), catalogSchemaColumnsQuery)
	if err != nil {
		t.Fatalf("read collector schema columns: %v", err)
	}
	defer rows.Close()

	tables := map[string]map[string]string{}
	for rows.Next() {
		var table, column, dataType string
		if err := rows.Scan(&table, &column, &dataType); err != nil {
			t.Fatalf("read collector schema columns: %v", err)
		}
		if tables[table] == nil {
			tables[table] = map[string]string{}
		}
		tables[table][column] = dataType
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read collector schema columns: %v", err)
	}
	return tables
}

// TestConfigurationSourcesMatchCollectorSchema checks every catalog
// table and column against the collector's schema: each must exist with
// exactly the type the catalog declares, since the declared type drives
// filter coercion, aggregation and what describe_datastore reports. It
// also checks that every collector column of a catalog table is either
// cataloged or deliberately omitted.
func TestConfigurationSourcesMatchCollectorSchema(t *testing.T) {
	schema := collectorSchemaColumns(t, scratchdb.NewCollectorSchema(t, catalogSchemaSuffix, 2))

	for _, src := range configurationSources() {
		actual, ok := schema[src.Table]
		if !ok {
			t.Errorf("source %q: table %q does not exist in the collector schema", src.Name, src.Table)
			continue
		}

		cataloged := map[string]bool{}
		for _, c := range src.Columns {
			cataloged[c.Name] = true
			dataType, ok := actual[c.Name]
			if !ok {
				t.Errorf("source %q: column %s.%s does not exist in the collector schema", src.Name, src.Table, c.Name)
				continue
			}
			if dataType != c.DataType {
				t.Errorf("source %q: column %s.%s is %q in the collector schema, but the catalog declares %q",
					src.Name, src.Table, c.Name, dataType, c.DataType)
			}
		}

		if src.LiveColumn != "" {
			if dataType, ok := actual[src.LiveColumn]; !ok {
				t.Errorf("source %q: LiveColumn %s.%s does not exist in the collector schema",
					src.Name, src.Table, src.LiveColumn)
			} else if dataType != typeTimestamptz {
				t.Errorf("source %q: LiveColumn %s.%s is %q, want a timestamp",
					src.Name, src.Table, src.LiveColumn, dataType)
			}
		}

		omitted := catalogOmittedColumns[src.Table]
		for _, name := range omitted {
			if cataloged[name] {
				t.Errorf("source %q: column %s.%s is both cataloged and listed in catalogOmittedColumns",
					src.Name, src.Table, name)
			}
			if _, ok := actual[name]; !ok {
				t.Errorf("source %q: catalogOmittedColumns lists %s.%s, which the collector schema does not have",
					src.Name, src.Table, name)
			}
		}

		var uncovered []string
		for name := range actual {
			if !cataloged[name] && !slices.Contains(omitted, name) {
				uncovered = append(uncovered, name)
			}
		}
		sort.Strings(uncovered)
		for _, name := range uncovered {
			t.Errorf("source %q: collector column %s.%s (%s) is neither cataloged nor listed in catalogOmittedColumns",
				src.Name, src.Table, name, actual[name])
		}
	}

	for table := range catalogOmittedColumns {
		if !slices.ContainsFunc(configurationSources(), func(s datastoreSource) bool { return s.Table == table }) {
			t.Errorf("catalogOmittedColumns lists table %q, which no configuration source reads", table)
		}
	}
}

// TestMetricSourcesResolveAgainstCollectorSchema checks that the metric
// sources describe_datastore lists from the collector's probe tables can
// each be looked up, so the two discovery queries agree on the real
// schema and no probe table lacks the bookkeeping columns.
func TestMetricSourcesResolveAgainstCollectorSchema(t *testing.T) {
	pool := scratchdb.NewCollectorSchema(t, catalogSchemaSuffix, 2)
	ctx := context.Background()

	summaries, err := listDatastoreSources(ctx, pool)
	if err != nil {
		t.Fatalf("listDatastoreSources: %v", err)
	}
	metricSources := 0
	for _, s := range summaries {
		if s.Kind != datastoreKindMetric {
			continue
		}
		metricSources++
		src, err := lookupDatastoreSource(ctx, pool, s.Name)
		if err != nil {
			t.Errorf("metric source %q is listed but cannot be looked up: %v", s.Name, err)
			continue
		}
		for _, required := range []string{metricConnectionColumn, metricTimeColumn} {
			if _, ok := src.column(required); !ok {
				t.Errorf("metric source %q has no %s column", s.Name, required)
			}
		}
	}
	if metricSources == 0 {
		t.Error("the collector schema yields no metric sources")
	}
}

// TestMetricRedactionsMatchCollectorSchema checks that every probe in
// metrics.ProbeRedactions still has the setting-name column and every column
// it masks, and that each is masked the way the rule says, so a
// collector rename cannot leave a credential unmasked or quietly drop a
// column.
func TestMetricRedactionsMatchCollectorSchema(t *testing.T) {
	pool := scratchdb.NewCollectorSchema(t, catalogSchemaSuffix, 2)
	ctx := context.Background()

	for name, rule := range metrics.ProbeRedactions {
		src, err := lookupDatastoreSource(ctx, pool, name)
		if err != nil {
			t.Errorf("redacted probe %q cannot be looked up: %v", name, err)
			continue
		}
		if rule.KeyColumn != "" {
			if _, ok := src.column(rule.KeyColumn); !ok {
				t.Errorf("redacted probe %q has no %s column", name, rule.KeyColumn)
			}
		}
		for _, c := range rule.Columns {
			got, ok := src.column(c)
			if !ok || got.RedactBy != rule.KeyColumn {
				t.Errorf("probe %q column %s should be masked by %s, got %+v (present=%v)",
					name, c, rule.KeyColumn, got, ok)
			}
		}
		for _, c := range rule.OptionColumns {
			if got, ok := src.column(c); !ok || !got.MaskOptions {
				t.Errorf("probe %q options column %s should be masked, got %+v (present=%v)", name, c, got, ok)
			}
		}
		for _, c := range rule.WholeColumns {
			if got, ok := src.column(c); !ok || !got.MaskWhole {
				t.Errorf("probe %q column %s should be masked whole, got %+v (present=%v)", name, c, got, ok)
			}
		}
		for _, c := range rule.TextColumns {
			if got, ok := src.column(c); !ok || !got.RedactText || got.DataType != typeText {
				t.Errorf("probe %q text column %s should be redacted text, got %+v (present=%v)", name, c, got, ok)
			}
		}
	}

	// connections.connection_error is free text from libpq, which can
	// quote a connection string; the catalog redacts it.
	for _, src := range configurationSources() {
		if src.Name != "connections" {
			continue
		}
		if got, ok := src.column("connection_error"); !ok || !got.RedactText {
			t.Errorf("connections.connection_error should be redacted text, got %+v (present=%v)", got, ok)
		}
	}
}
