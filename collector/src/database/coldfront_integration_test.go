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
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// coldFrontStub stands in for the coldfront extension and pg_duckdb's
// raw_query, closely enough to catch what failed against live ColdFront
// datastores:
//
//   - duckdb.raw_query records each statement with the transaction that
//     ran it, and create_iceberg_table refuses a table whose Iceberg
//     namespace was not created by an earlier, committed statement, as a
//     Lakekeeper catalog does with "Namespace with name 'metrics' does
//     not exist".
//   - create_iceberg_table creates metrics.<table> as a view (relkind
//     'v'), so a statement that only works on a table, such as COMMENT
//     ON TABLE, fails as it does live. The view selects from a backing
//     table in coldfront_stub, which keeps it automatically updatable so
//     that StoreMetrics can insert through it as it does through
//     ColdFront's insert hook.
//   - It rejects the types ColdFront rejects and any partition spec but
//     day(collected_at), and records each call and registers the view in
//     coldfront.tiered_views, which the insert helper requires.
//   - Its ADD COLUMN emulation drops and recreates the view, as
//     coldfront._rebuild_iceberg_view does, so the view and column
//     comments are lost as they are live and must be restored.
//
// ensure_attached and _partition_clause are no-ops that exist so the
// startup check finds them.
const coldFrontStub = `
    CREATE SCHEMA coldfront_stub;
    CREATE TABLE coldfront_stub.raw_queries (query text NOT NULL, xid bigint NOT NULL);
    CREATE TABLE coldfront_stub.calls (
        p_schema text, p_table text, p_columns jsonb, p_partition_cols text[]);

    CREATE SCHEMA duckdb;
    CREATE FUNCTION duckdb.raw_query(query text) RETURNS void
    LANGUAGE sql AS $f$
        INSERT INTO coldfront_stub.raw_queries VALUES (query, txid_current())
    $f$;

    CREATE SCHEMA coldfront;
    CREATE TABLE coldfront.tiered_views (schema_name text, relname text);
    CREATE FUNCTION coldfront.ensure_attached() RETURNS void
    LANGUAGE plpgsql AS $f$ BEGIN NULL; END $f$;
    CREATE FUNCTION coldfront._partition_clause(p_columns jsonb, p_partition_cols text[])
    RETURNS text LANGUAGE sql AS $f$ SELECT ''::text $f$;

    CREATE FUNCTION coldfront.create_iceberg_table(
        p_schema text, p_table text, p_columns jsonb,
        p_partition_cols text[] DEFAULT NULL)
    RETURNS void LANGUAGE plpgsql AS $f$
    DECLARE
        col  jsonb;
        cols text := '';
        typ  text;
    BEGIN
        IF NOT EXISTS (
            SELECT 1 FROM coldfront_stub.raw_queries
            WHERE query = format('CREATE SCHEMA IF NOT EXISTS ice.%s', p_schema)
              AND xid <> txid_current()) THEN
            RAISE EXCEPTION 'Namespace with name ''%'' does not exist in warehouse', p_schema;
        END IF;
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
        INSERT INTO coldfront_stub.calls
            VALUES (p_schema, p_table, p_columns, p_partition_cols);
        INSERT INTO coldfront.tiered_views VALUES (p_schema, p_table);
        EXECUTE format('CREATE TABLE coldfront_stub.%I (%s)', p_table, cols);
        EXECUTE format('CREATE VIEW %I.%I AS SELECT * FROM coldfront_stub.%I',
                       p_schema, p_table, p_table);
    END
    $f$;

    -- Emulates ColdFront's handling of ALTER TABLE ... ADD COLUMN on one
    -- of its views; see coldFrontAlterTx.
    CREATE FUNCTION coldfront_stub.add_column(p_table text, p_column text, p_type text)
    RETURNS void LANGUAGE plpgsql AS $f$
    BEGIN
        EXECUTE format('ALTER TABLE coldfront_stub.%I ADD COLUMN %I %s',
                       p_table, p_column, p_type);
        EXECUTE format('DROP VIEW metrics.%I', p_table);
        EXECUTE format('CREATE VIEW metrics.%I AS SELECT * FROM coldfront_stub.%I',
                       p_table, p_table);
    END
    $f$;
`

// dropColdFrontStub removes everything coldFrontStub created, including
// the metrics views that depend on its backing tables.
const dropColdFrontStub = `
    DROP SCHEMA IF EXISTS workbench_coldfront CASCADE;
    DROP SCHEMA IF EXISTS coldfront_stub CASCADE;
    DROP SCHEMA IF EXISTS coldfront CASCADE;
    DROP SCHEMA IF EXISTS duckdb CASCADE;
`

// addColumnStatement matches the ADD COLUMN form migrations 7, 10, 12
// and 13 send to a metrics relation.
var addColumnStatement = regexp.MustCompile(
	`(?is)^ALTER\s+TABLE\s+metrics\.(\w+)\s+ADD\s+COLUMN\s+IF\s+NOT\s+EXISTS\s+(\w+)\s+(.+)$`)

// coldFrontAlterTx runs a migration against the stub. A plain view
// rejects ALTER TABLE ... ADD COLUMN (SQLSTATE 42809, which
// TestColdFrontMigrationsAgainstStub demonstrates); ColdFront intercepts
// that statement on its own views, which no stub can do, so this
// wrapper replaces exactly that statement with the stub's emulation and
// records it. Every other statement, including any other ALTER TABLE
// form, runs unchanged and fails if a view cannot take it. Whether
// ColdFront really accepts these ADD COLUMN statements can only be
// confirmed against a live ColdFront datastore.
type coldFrontAlterTx struct {
	pgx.Tx
	emulated *[]string
}

func (w coldFrontAlterTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if len(args) > 0 || !strings.Contains(sql, "ALTER TABLE metrics.") {
		return w.Tx.Exec(ctx, sql, args...)
	}
	var tag pgconn.CommandTag
	for _, stmt := range splitSQLStatements(sql) {
		var err error
		if m := addColumnStatement.FindStringSubmatch(stmt); m != nil {
			*w.emulated = append(*w.emulated, m[1]+"."+m[2])
			tag, err = w.Tx.Exec(ctx,
				"SELECT coldfront_stub.add_column($1, $2, $3)", m[1], m[2], m[3])
		} else {
			tag, err = w.Tx.Exec(ctx, stmt)
		}
		if err != nil {
			return tag, err
		}
	}
	return tag, nil
}

// expectedViewComments returns the metrics relations whose ColdFront DDL
// carries a COMMENT ON VIEW, read from the statements the migrations
// issue under ColdFront.
func expectedViewComments(t *testing.T) []string {
	t.Helper()
	re := regexp.MustCompile(`^COMMENT ON VIEW metrics\.(\w+) IS`)
	var names []string
	for _, s := range statementsOf(runMigrationsRecorded(t, NewSchemaManager(WithColdFrontMetrics(true)))) {
		if m := re.FindStringSubmatch(squash(s.sql)); m != nil {
			names = append(names, m[1])
		}
	}
	return names
}

// TestColdFrontMigrationsAgainstStub runs the startup preparation and the
// whole migration chain under ColdFront against coldFrontStub, then
// checks the resulting views, runs everything again to show it is a
// no-op, and writes probe-shaped rows through StoreMetrics.
func TestColdFrontMigrationsAgainstStub(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()
	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	if _, err := conn.Exec(ctx, dropColdFrontStub+coldFrontStub); err != nil {
		t.Fatalf("failed to install the coldfront stub: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, dropColdFrontStub); err != nil {
			t.Errorf("failed to drop the coldfront stub: %v", err)
		}
	}()

	var emulated []string
	wrap := func(tx pgx.Tx) pgx.Tx { return coldFrontAlterTx{Tx: tx, emulated: &emulated} }
	sm := NewSchemaManager(WithColdFrontMetrics(true))

	// Without the namespace step the first table is refused, as Lakekeeper
	// refuses it, and migration 1 rolls back.
	err := sm.migrate(conn, wrap)
	if err == nil || !strings.Contains(err.Error(), "Namespace with name 'metrics' does not exist") {
		t.Fatalf("migrating before the namespace exists: error = %v", err)
	}

	if err := PrepareMetricsStorage(ctx, conn, true); err != nil {
		t.Fatalf("PrepareMetricsStorage failed: %v", err)
	}
	emulated = nil
	if err := sm.migrate(conn, wrap); err != nil {
		t.Fatalf("Migrate under ColdFront failed: %v", err)
	}

	wantEmulated := []string{
		"pg_stat_all_tables.table_size", "pg_stat_all_indexes.index_size",
		"pg_stat_statements.stats_reset", "pg_stat_activity.query_id",
		"pg_sys_memory_info.available_memory",
	}
	if !reflect.DeepEqual(emulated, wantEmulated) {
		t.Errorf("emulated ADD COLUMN statements = %v, want %v", emulated, wantEmulated)
	}

	// A plain view really does reject the ADD COLUMN form, which is why
	// the emulation above is needed and why it must be checked live.
	_, err = conn.Exec(ctx, "ALTER TABLE metrics.pg_connectivity ADD COLUMN IF NOT EXISTS probe_check BIGINT")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42809" {
		t.Errorf("ADD COLUMN on a plain view: error = %v, want SQLSTATE 42809", err)
	}

	var version, versions int
	if err := conn.QueryRow(ctx, "SELECT MAX(version), COUNT(*) FROM schema_version").Scan(&version, &versions); err != nil {
		t.Fatal(err)
	}
	if version != sm.LatestVersion() {
		t.Errorf("schema_version = %d, want %d", version, sm.LatestVersion())
	}

	// Every metrics relation is a view with the specification's columns
	// and ColdFront types, including those later migrations add.
	compareWithSpec(t, readMetricsColumns(t, conn, []string{"v"}),
		func(pgType string) string {
			st, err := coldfront.StorageType(pgType)
			if err != nil {
				t.Fatal(err)
			}
			return st
		})

	var nonViews, indexes, constraints, calls, badPartitions int
	if err := conn.QueryRow(ctx, `
        SELECT
            (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
             WHERE n.nspname = 'metrics' AND c.relkind <> 'v'),
            (SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid
             JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'metrics'),
            (SELECT count(*) FROM pg_constraint k JOIN pg_namespace n ON n.oid = k.connamespace
             WHERE n.nspname = 'metrics'),
            (SELECT count(*) FROM coldfront_stub.calls),
            (SELECT count(*) FROM coldfront_stub.calls
             WHERE p_schema <> 'metrics' OR p_partition_cols <> ARRAY['day(collected_at)'])
    `).Scan(&nonViews, &indexes, &constraints, &calls, &badPartitions); err != nil {
		t.Fatal(err)
	}
	if nonViews != 0 || indexes != 0 || constraints != 0 {
		t.Errorf("metrics schema has %d relations that are not views, %d indexes and %d constraints, want none",
			nonViews, indexes, constraints)
	}
	if calls != len(coldfront.Tables()) || badPartitions != 0 {
		t.Errorf("create_iceberg_table called %d times (%d with the wrong schema or partitions), want %d",
			calls, badPartitions, len(coldfront.Tables()))
	}

	// Every view comment the DDL carries, and the comments on columns
	// later migrations add, reached the views.
	commented := expectedViewComments(t)
	if len(commented) == 0 {
		t.Fatal("no COMMENT ON VIEW was issued under ColdFront")
	}
	var missing []string
	if err := conn.QueryRow(ctx, `
        SELECT COALESCE(array_agg(v), '{}') FROM unnest($1::text[]) v
        WHERE obj_description(('metrics.' || quote_ident(v))::regclass, 'pg_class') IS NULL
    `, commented).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Errorf("views without their comment: %v", missing)
	}
	var columnComments int
	if err := conn.QueryRow(ctx, `
        SELECT count(*) FROM (VALUES
            ('pg_stat_activity', 'query_id'), ('pg_stat_statements', 'stats_reset'),
            ('pg_settings', 'setting'), ('pg_node_role', 'postmaster_start_time')) AS c(rel, col)
        JOIN pg_attribute a ON a.attrelid = ('metrics.' || c.rel)::regclass AND a.attname = c.col
        WHERE col_description(a.attrelid, a.attnum) IS NOT NULL
    `).Scan(&columnComments); err != nil {
		t.Fatal(err)
	}
	if columnComments != 4 {
		t.Errorf("%d of 4 sampled column comments reached the views", columnComments)
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

	// The startup step after the migrations creates them instead, in
	// transactions of their own, and is a no-op the second time.
	buf := captureLog(t)
	EnsureColdFrontEmbeddings(ctx, conn)
	EnsureColdFrontEmbeddings(ctx, conn)
	checkColdFrontEmbeddings(t, conn)
	if strings.Contains(buf.String(), "skipped") {
		t.Errorf("EnsureColdFrontEmbeddings on a fully migrated datastore logged: %q", buf.String())
	}

	// A second start finds views, recreates nothing and applies nothing.
	if err := PrepareMetricsStorage(ctx, conn, true); err != nil {
		t.Fatalf("second PrepareMetricsStorage failed: %v", err)
	}
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}
	var calls2, versions2 int
	if err := conn.QueryRow(ctx, `
        SELECT (SELECT count(*) FROM coldfront_stub.calls),
               (SELECT count(*) FROM schema_version)
    `).Scan(&calls2, &versions2); err != nil {
		t.Fatal(err)
	}
	if calls2 != calls || versions2 != versions {
		t.Errorf("second run changed create calls %d -> %d and versions %d -> %d", calls, calls2, versions, versions2)
	}

	exerciseInsertHelper(t, conn)
	storeConvertedValues(t, conn)
}

// TestColdFrontEmbeddingsFollowSearchPath reproduces a live ColdFront
// datastore whose role's search_path starts with an existing schema
// other than public: the image's coldfront role and the coldfront
// schema make "$user", public resolve to coldfront first. Migration #1
// creates chat_memories, unqualified, in that schema, so the startup
// step must add the embedding column there; it used to ALTER
// public.chat_memories and log, at every startup, that chat memory
// embeddings were disabled because the relation did not exist.
func TestColdFrontEmbeddingsFollowSearchPath(t *testing.T) {
	const roleSchema = "wb_role_schema"
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()
	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	// pgvector, where available, stays in public as on the live
	// datastore, so dropping the role schema afterwards leaves it.
	if _, err := conn.Exec(ctx, `
        DO $$ BEGIN
            IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector') THEN
                CREATE EXTENSION IF NOT EXISTS vector SCHEMA public;
            END IF;
        END $$;
        DROP SCHEMA IF EXISTS `+roleSchema+` CASCADE;
        CREATE SCHEMA `+roleSchema+`;`+dropColdFrontStub+coldFrontStub); err != nil {
		t.Fatalf("failed to set up the role schema and the coldfront stub: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+roleSchema+" CASCADE;"+dropColdFrontStub); err != nil {
			t.Errorf("failed to drop the role schema and the coldfront stub: %v", err)
		}
	}()
	if _, err := conn.Exec(ctx, "SET search_path = "+roleSchema+", public"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := conn.Exec(ctx, "RESET search_path"); err != nil {
			t.Errorf("failed to reset search_path: %v", err)
		}
	}()

	if err := PrepareMetricsStorage(ctx, conn, true); err != nil {
		t.Fatalf("PrepareMetricsStorage failed: %v", err)
	}
	var emulated []string
	wrap := func(tx pgx.Tx) pgx.Tx { return coldFrontAlterTx{Tx: tx, emulated: &emulated} }
	if err := NewSchemaManager(WithColdFrontMetrics(true)).migrate(conn, wrap); err != nil {
		t.Fatalf("Migrate under ColdFront failed: %v", err)
	}

	buf := captureLog(t)
	EnsureColdFrontEmbeddings(ctx, conn)
	if out := buf.String(); strings.Contains(out, "skipped") || strings.Contains(out, "does not exist") {
		t.Errorf("EnsureColdFrontEmbeddings on a fully migrated datastore logged: %q", out)
	}

	var available, publicChat bool
	var chatType string
	if err := conn.QueryRow(ctx, `
        SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector'),
               to_regclass('public.chat_memories') IS NOT NULL,
               COALESCE((SELECT format_type(atttypid, atttypmod) FROM pg_attribute
                         WHERE attrelid = '`+roleSchema+`.chat_memories'::regclass
                           AND attname = 'embedding'), '')
    `).Scan(&available, &publicChat, &chatType); err != nil {
		t.Fatal(err)
	}
	if publicChat {
		t.Error("migration #1 created public.chat_memories despite the search_path")
	}
	want := ""
	if available {
		want = "halfvec(4000)"
	}
	if chatType != want {
		t.Errorf("%s.chat_memories.embedding is %q, want %q", roleSchema, chatType, want)
	}
}

// checkColdFrontEmbeddings asserts that, where pgvector is available,
// EnsureColdFrontEmbeddings left the same pgvector objects migration 1
// creates in the default mode, at the halfvec(4000) width migration 6
// gives them, and none where it is not.
func checkColdFrontEmbeddings(t *testing.T, conn *pgxpool.Conn) {
	t.Helper()
	var available bool
	var chatType, anomalyType string
	var foreignKey, chatIndex, anomalyIndex bool
	if err := conn.QueryRow(context.Background(), `
        SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector'),
               COALESCE((SELECT format_type(atttypid, atttypmod) FROM pg_attribute
                         WHERE attrelid = 'public.chat_memories'::regclass
                           AND attname = 'embedding'), ''),
               COALESCE((SELECT format_type(atttypid, atttypmod) FROM pg_attribute
                         WHERE attrelid = to_regclass('public.anomaly_embeddings')
                           AND attname = 'embedding'), ''),
               EXISTS (SELECT 1 FROM pg_constraint
                       WHERE conname = 'fk_anomaly_candidates_embedding'),
               to_regclass('public.idx_chat_memories_embedding') IS NOT NULL,
               to_regclass('public.idx_anomaly_embeddings_vector') IS NOT NULL
    `).Scan(&available, &chatType, &anomalyType, &foreignKey, &chatIndex, &anomalyIndex); err != nil {
		t.Fatal(err)
	}
	if !available {
		if chatType != "" || anomalyType != "" {
			t.Errorf("without pgvector the embedding columns exist: %q, %q", chatType, anomalyType)
		}
		return
	}
	if chatType != "halfvec(4000)" || anomalyType != "halfvec(4000)" {
		t.Errorf("embedding columns are %q (chat_memories) and %q (anomaly_embeddings), want halfvec(4000)",
			chatType, anomalyType)
	}
	if !foreignKey || !chatIndex || !anomalyIndex {
		t.Errorf("foreign key %v, chat_memories index %v, anomaly_embeddings index %v; want all",
			foreignKey, chatIndex, anomalyIndex)
	}
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
               (SELECT datlocprovider FROM metrics.pg_database WHERE connection_id = 1),
               (SELECT datfrozenxid::text FROM metrics.pg_database WHERE connection_id = 1),
               (SELECT datdba::text FROM metrics.pg_database WHERE connection_id = 1),
               (SELECT node_name FROM metrics.spock_resolutions WHERE connection_id = 1),
               (SELECT wal_bytes::text FROM metrics.pg_stat_wal WHERE connection_id = 1)
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
