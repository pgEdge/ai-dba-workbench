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
	"os"
	"slices"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	collectordb "github.com/pgedge/ai-workbench/collector/src/database"
)

// These tests check the query_datastore catalog against the schema the
// collector really creates (issue #645). The fixtures in
// query_datastore_integration_test.go build their tables from the
// catalog itself, so they cannot notice a column the collector renamed,
// dropped, retyped or added; this file brings up the real schema with the
// collector's SchemaManager in a database of its own and compares.

// catalogSchemaDatabase is the scratch database the collector schema is
// migrated into. It is separate from the shared test database because
// other tests in this package and module create tables with the same
// names but different shapes there. The name is a constant so the
// statements below are constant SQL; two concurrent runs of this test
// against the same PostgreSQL cluster would therefore collide.
const catalogSchemaDatabase = "ai_workbench_catalog_schema_check"

const (
	catalogSchemaDrop   = `DROP DATABASE IF EXISTS ai_workbench_catalog_schema_check WITH (FORCE)`
	catalogSchemaCreate = `CREATE DATABASE ai_workbench_catalog_schema_check`
)

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
	// password_encrypted, the certificate paths and owner_token are
	// secrets; see TestConfigurationSourcesExposeNoSecrets. username and
	// hostaddr are not secrets (the REST API returns both) but the catalog
	// has never offered them; adding them is a separate decision.
	"connections": {
		"username", "hostaddr", "password_encrypted", "sslcert", "sslkey",
		"sslrootcert", "owner_token",
	},
	// embedding_id refers to anomaly_embeddings, which query_datastore
	// never exposes.
	"anomaly_candidates": {"embedding_id"},
	// A secret; see TestConfigurationSourcesExposeNoSecrets.
	"cluster_groups": {"owner_token"},
}

// newCollectorSchemaPool creates the scratch database, migrates it to the
// collector's latest schema and returns a pool on it. The database is
// dropped when the test ends.
func newCollectorSchemaPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping catalog schema test")
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(admin.Close)

	if _, err := admin.Exec(ctx, catalogSchemaDrop); err != nil {
		t.Fatalf("drop stale %s: %v", catalogSchemaDatabase, err)
	}
	if _, err := admin.Exec(ctx, catalogSchemaCreate); err != nil {
		t.Fatalf("create %s: %v", catalogSchemaDatabase, err)
	}

	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	cfg.ConnConfig.Database = catalogSchemaDatabase
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to %s: %v", catalogSchemaDatabase, err)
	}
	// Cleanups run last-in first-out: the pool closes before the drop,
	// which runs before the admin pool closes.
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), catalogSchemaDrop); err != nil {
			t.Logf("drop %s: %v", catalogSchemaDatabase, err)
		}
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection to %s: %v", catalogSchemaDatabase, err)
	}
	defer conn.Release()
	if err := collectordb.NewSchemaManager().Migrate(conn); err != nil {
		t.Fatalf("migrate collector schema: %v", err)
	}
	return pool
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
	schema := collectorSchemaColumns(t, newCollectorSchemaPool(t))

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
	pool := newCollectorSchemaPool(t)
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
