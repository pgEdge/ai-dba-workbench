/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
	"github.com/pgedge/ai-workbench/collector/src/probes"
)

// metricsColumn is one column of a metrics relation as the catalog
// reports it.
type metricsColumn struct {
	table, name, pgType string
}

// readMetricsColumns returns every column of the metrics relations of
// the given relkinds, in table and attribute order.
func readMetricsColumns(t *testing.T, conn *pgxpool.Conn, relkinds []string) map[string][]metricsColumn {
	t.Helper()
	rows, err := conn.Query(context.Background(), `
        SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod)
        FROM pg_attribute a
        JOIN pg_class c ON c.oid = a.attrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'metrics'
          AND c.relkind = ANY($1::"char"[])
          AND NOT c.relispartition
          AND a.attnum > 0 AND NOT a.attisdropped
        ORDER BY c.relname, a.attnum`, relkinds)
	if err != nil {
		t.Fatalf("failed to read metrics columns: %v", err)
	}
	defer rows.Close()
	out := map[string][]metricsColumn{}
	for rows.Next() {
		var c metricsColumn
		if err := rows.Scan(&c.table, &c.name, &c.pgType); err != nil {
			t.Fatalf("failed to scan metrics column: %v", err)
		}
		out[c.table] = append(out[c.table], c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// compareWithSpec reports every difference between the catalog columns
// and the specification, with each specification type passed through
// typeOf first.
func compareWithSpec(t *testing.T, actual map[string][]metricsColumn, typeOf func(string) string) {
	t.Helper()
	for _, spec := range coldfront.Tables() {
		got := actual[spec.Name]
		if got == nil {
			t.Errorf("metrics.%s is in the ColdFront specification but not in the datastore", spec.Name)
			continue
		}
		if len(got) != len(spec.Columns) {
			t.Errorf("metrics.%s has %d columns, the ColdFront specification %d", spec.Name, len(got), len(spec.Columns))
			continue
		}
		for i, c := range spec.Columns {
			if got[i].name != c.Name || got[i].pgType != typeOf(c.PGType) {
				t.Errorf("metrics.%s column %d is %s %s, the ColdFront specification has %s %s",
					spec.Name, i+1, got[i].name, got[i].pgType, c.Name, typeOf(c.PGType))
			}
		}
	}
	for table := range actual {
		if _, ok := coldfront.Lookup(table); !ok {
			t.Errorf("metrics.%s has no ColdFront column specification", table)
		}
	}
}

// TestColdFrontSpecMatchesMigratedSchema migrates a datastore in the
// default mode and compares every partitioned metrics table with the
// ColdFront column specification, so a table or column added to the
// plain-PostgreSQL migrations without a specification fails here.
func TestColdFrontSpecMatchesMigratedSchema(t *testing.T) {
	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()
	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	if err := NewSchemaManager().Migrate(conn); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}
	compareWithSpec(t, readMetricsColumns(t, conn, []string{"p", "r"}),
		func(pgType string) string { return pgType })
}

// coldFrontStub stands in for the coldfront extension: it validates its
// arguments the way the real function does for the types the metrics
// tables use, and creates an ordinary table, which accepts the same
// ALTER TABLE, COMMENT and INSERT statements the migrations and probes
// send to a ColdFront view.
const coldFrontStub = `
    CREATE SCHEMA coldfront;
    CREATE FUNCTION coldfront.create_iceberg_table(
        p_schema text, p_table text, p_columns jsonb,
        p_partition_cols text[] DEFAULT NULL)
    RETURNS void LANGUAGE plpgsql AS $f$
    DECLARE
        col  jsonb;
        cols text := '';
        typ  text;
    BEGIN
        IF p_partition_cols IS DISTINCT FROM ARRAY['day(collected_at)'] THEN
            RAISE EXCEPTION 'unexpected partition spec %', p_partition_cols;
        END IF;
        FOR col IN SELECT * FROM jsonb_array_elements(p_columns) LOOP
            typ := lower(col->>'type');
            IF typ IN ('oid', 'inet', 'cidr', 'xid', '"char"', 'name', 'numeric') THEN
                RAISE EXCEPTION 'coldfront stub: type % rejected', typ;
            END IF;
            IF cols <> '' THEN
                cols := cols || ', ';
            END IF;
            cols := cols || quote_ident(col->>'name') || ' ' || (col->>'type');
        END LOOP;
        EXECUTE format('CREATE TABLE %I.%I (%s)', p_schema, p_table, cols);
    END
    $f$;
`

// TestColdFrontMigrationsAgainstStub runs the whole migration chain
// under ColdFront against a stub of create_iceberg_table, then checks
// the resulting schema and writes probe-shaped rows through
// StoreMetrics, so the SQL the ColdFront path sends and the value
// conversions are exercised on a real server.
func TestColdFrontMigrationsAgainstStub(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()
	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	if _, err := conn.Exec(ctx, coldFrontStub); err != nil {
		t.Fatalf("failed to install the coldfront stub: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS coldfront CASCADE"); err != nil {
			t.Errorf("failed to drop the coldfront stub: %v", err)
		}
	}()

	sm := NewSchemaManager(WithColdFrontMetrics(true))
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Migrate under ColdFront failed: %v", err)
	}

	var version int
	if err := conn.QueryRow(ctx, "SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != sm.LatestVersion() {
		t.Errorf("schema_version = %d, want %d", version, sm.LatestVersion())
	}

	// Every metrics table carries the specification's columns with their
	// ColdFront types, including those later migrations add.
	compareWithSpec(t, readMetricsColumns(t, conn, []string{"p", "r", "v"}),
		func(pgType string) string {
			st, err := coldfront.StorageType(pgType)
			if err != nil {
				t.Fatal(err)
			}
			return st
		})

	var indexes, constraints, partitioned int
	if err := conn.QueryRow(ctx, `
        SELECT
            (SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid
             JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'metrics'),
            (SELECT count(*) FROM pg_constraint k JOIN pg_namespace n ON n.oid = k.connamespace
             WHERE n.nspname = 'metrics'),
            (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
             WHERE n.nspname = 'metrics' AND (c.relkind = 'p' OR c.relispartition))
    `).Scan(&indexes, &constraints, &partitioned); err != nil {
		t.Fatal(err)
	}
	if indexes != 0 || constraints != 0 || partitioned != 0 {
		t.Errorf("metrics schema has %d indexes, %d constraints and %d partitioned relations under ColdFront, want none",
			indexes, constraints, partitioned)
	}

	var tableComment, columnComment *string
	if err := conn.QueryRow(ctx, `
        SELECT obj_description('metrics.pg_stat_activity'::regclass, 'pg_class'),
               col_description('metrics.pg_stat_activity'::regclass,
                   (SELECT attnum FROM pg_attribute
                    WHERE attrelid = 'metrics.pg_stat_activity'::regclass AND attname = 'query_id'))
    `).Scan(&tableComment, &columnComment); err != nil {
		t.Fatal(err)
	}
	if tableComment == nil || columnComment == nil {
		t.Error("COMMENT ON TABLE or COMMENT ON COLUMN did not reach the ColdFront relation")
	}

	var embeddingColumn, anomalyEmbeddings bool
	if err := conn.QueryRow(ctx, `
        SELECT EXISTS (SELECT 1 FROM information_schema.columns
                       WHERE table_name = 'chat_memories' AND column_name = 'embedding'),
               to_regclass('public.anomaly_embeddings') IS NOT NULL
    `).Scan(&embeddingColumn, &anomalyEmbeddings); err != nil {
		t.Fatal(err)
	}
	if embeddingColumn || anomalyEmbeddings {
		t.Error("the pgvector setup ran under ColdFront")
	}

	storeConvertedValues(t, conn)
}

// storeConvertedValues writes values of every mapped type, decoded by
// pgx from a real server exactly as the probes receive them, through
// StoreMetrics under ColdFront, and reads them back.
func storeConvertedValues(t *testing.T, conn *pgxpool.Conn) {
	t.Helper()
	ctx := context.Background()

	probes.SetColdFrontMetrics(true)
	t.Cleanup(func() { probes.SetColdFrontMetrics(false) })

	rows, err := conn.Query(ctx, `
        SELECT '10.1.2.3'::inet, '10.0.0.0/8'::inet, 'i'::"char",
               '4000000000'::xid, 4000000001::oid, 'node one'::name,
               123456789012345678901::numeric`)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	v, err := rows.Values()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	host, network, provider, frozen, dba, node, walBytes := v[0], v[1], v[2], v[3], v[4], v[5], v[6]

	at := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	writes := []struct {
		table   string
		columns []string
		values  [][]any
	}{
		{"pg_stat_activity", []string{"connection_id", "pid", "client_addr", "datid", "collected_at"},
			[][]any{{1, 10, host, dba, at}, {1, 11, network, nil, at}, {1, 12, nil, nil, at}}},
		{"pg_database", []string{"connection_id", "datname", "datdba", "datlocprovider", "datfrozenxid", "collected_at"},
			[][]any{{1, "postgres", dba, provider, frozen, at}}},
		{"spock_resolutions", []string{"connection_id", "database_name", "collected_at", "id", "node_name", "log_time"},
			[][]any{{1, "postgres", at, 1, node, at}}},
		{"pg_stat_wal", []string{"connection_id", "wal_bytes", "collected_at"},
			[][]any{{1, walBytes, at}}},
	}
	for _, w := range writes {
		if err := probes.StoreMetrics(ctx, conn, w.table, w.columns, w.values); err != nil {
			t.Fatalf("StoreMetrics into metrics.%s failed: %v", w.table, err)
		}
	}

	var got [7]*string
	if err := conn.QueryRow(ctx, `
        SELECT (SELECT client_addr FROM metrics.pg_stat_activity WHERE pid = 10),
               (SELECT client_addr FROM metrics.pg_stat_activity WHERE pid = 11),
               (SELECT datlocprovider FROM metrics.pg_database),
               (SELECT datfrozenxid::text FROM metrics.pg_database),
               (SELECT datdba::text FROM metrics.pg_database),
               (SELECT node_name FROM metrics.spock_resolutions),
               (SELECT wal_bytes::text FROM metrics.pg_stat_wal)
    `).Scan(&got[0], &got[1], &got[2], &got[3], &got[4], &got[5], &got[6]); err != nil {
		t.Fatal(err)
	}
	want := []string{"10.1.2.3", "10.0.0.0/8", "i", "4000000000", "4000000001", "node one", "123456789012345678901"}
	for i, w := range want {
		if got[i] == nil || *got[i] != w {
			t.Errorf("stored value %d = %v, want %q", i, got[i], w)
		}
	}
}
