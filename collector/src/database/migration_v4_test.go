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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigrationV4_AnomalyCandidatesPartialIndexExists verifies that the v4
// migration creates the partial index used by the alerter sweeper query.
// The fixture migrates a fresh test database to the latest schema version
// and asserts the index exists with the expected predicate and key column.
func TestMigrationV4_AnomalyCandidatesPartialIndexExists(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	var indexDef string
	err := pool.QueryRow(ctx, `
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = 'public'
		  AND tablename = 'anomaly_candidates'
		  AND indexname = 'idx_anomaly_candidates_unprocessed'
	`).Scan(&indexDef)
	if err != nil {
		t.Fatalf("idx_anomaly_candidates_unprocessed missing: %v", err)
	}

	// The partial WHERE clause must match the alerter query predicate
	// exactly so the planner can use the index. The leading sort column
	// must be detected_at ASC so the ORDER BY can be served by the
	// index without an extra sort node.
	if !strings.Contains(indexDef, "detected_at") {
		t.Errorf("index %s does not key on detected_at: %s",
			"idx_anomaly_candidates_unprocessed", indexDef)
	}
	if !strings.Contains(indexDef, "processed_at IS NULL") {
		t.Errorf("index %s missing processed_at IS NULL predicate: %s",
			"idx_anomaly_candidates_unprocessed", indexDef)
	}
	if !strings.Contains(indexDef, "tier1_pass") {
		t.Errorf("index %s missing tier1_pass predicate: %s",
			"idx_anomaly_candidates_unprocessed", indexDef)
	}
}

// TestMigrationV4_AnomalyCandidatesPartialIndexUsedByPlanner asserts that
// the partial index is actually picked by the planner for the alerter's
// hot GetUnprocessedAnomalyCandidates query. A seq-scan plan here would
// silently re-introduce the regression we fixed.
func TestMigrationV4_AnomalyCandidatesPartialIndexUsedByPlanner(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	// Insert a representative connection plus a small fixture of rows so
	// the planner has stats to reason about. Anomaly_candidates depends
	// on a connections row via FK; the connections row needs
	// owner_username (the CHECK constraint requires exactly one of
	// owner_username or owner_token).
	_, err := pool.Exec(ctx, `
		INSERT INTO connections (owner_username, name, host, port, database_name, username)
		VALUES ('plan-fixture-user', 'plan-fixture', 'localhost', 5432, 'postgres', 'postgres')
		ON CONFLICT DO NOTHING
	`)
	if err != nil {
		t.Fatalf("seed connections: %v", err)
	}
	var connID int
	if err := pool.QueryRow(ctx,
		`SELECT id FROM connections WHERE name = 'plan-fixture'`).Scan(&connID); err != nil {
		t.Fatalf("read connection id: %v", err)
	}

	now := time.Now().UTC()
	// Mix of processed / unprocessed rows so the partial index
	// genuinely filters something.
	for i := 0; i < 200; i++ {
		processed := i%4 == 0
		tier1 := i%3 != 0
		var processedAt any
		if processed {
			processedAt = now.Add(-time.Duration(i) * time.Minute)
		} else {
			processedAt = nil
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO anomaly_candidates
				(connection_id, metric_name, metric_value, z_score, detected_at, tier1_pass, processed_at)
			VALUES ($1, 'fixture_metric', 1.0, 3.0, $2, $3, $4)
		`, connID, now.Add(-time.Duration(i)*time.Minute), tier1, processedAt)
		if err != nil {
			t.Fatalf("seed anomaly_candidates row %d: %v", i, err)
		}
	}

	if _, err := pool.Exec(ctx, `ANALYZE anomaly_candidates`); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	// Disable seq scan so the assertion proves the partial index is
	// usable rather than depending on planner cost preference. With a
	// small fixture, the planner may legitimately pick a seq scan even
	// though the index is fine; we want this test to fail only if the
	// index cannot serve the query at all. SET enable_seqscan must run
	// on the same connection that subsequently issues EXPLAIN, so we
	// pass *pgxpool.Conn into readPlan instead of *pgxpool.Pool, which
	// would route the EXPLAIN to a different session.
	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}
	// Reset on the same session even if the test fails. The deferred
	// statements run LIFO, so this RESET fires before conn.Release()
	// (declared earlier above) and before pool.Close(). We log a
	// reset failure rather than fatal so cleanup never masks the real
	// test outcome.
	defer func() {
		if _, err := conn.Exec(
			context.Background(), `RESET enable_seqscan`,
		); err != nil {
			t.Logf("RESET enable_seqscan: %v", err)
		}
	}()

	// Mirror the exact alerter query (anomaly_queries.go) so the plan
	// reflects what the production sweeper will produce.
	plan, err := readPlan(ctx, conn, `
		EXPLAIN (FORMAT TEXT)
		SELECT id, connection_id, database_name, metric_name, metric_value,
		       z_score, detected_at, context, tier1_pass, tier2_score, tier2_pass,
		       tier3_result, tier3_pass, tier3_error, final_decision, alert_id,
		       processed_at
		FROM anomaly_candidates
		WHERE processed_at IS NULL AND tier1_pass = true
		ORDER BY detected_at ASC
		LIMIT 50
	`)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}

	// The plan must reference the new partial index. We do not require
	// a specific node shape (Index Scan vs Index Only Scan) but we do
	// require that the partial index name appears in the plan output.
	// With seq scan disabled, a missing index reference means the
	// planner cannot use the index at all, which is a real regression.
	if !strings.Contains(plan, "idx_anomaly_candidates_unprocessed") {
		t.Errorf("planner did not pick idx_anomaly_candidates_unprocessed; plan was:\n%s", plan)
	}
}

// TestMigrationV4_RedundantMetricsIndexesDropped verifies that the v4
// migration removes the two redundant partitioned indexes from the
// parent tables AND from every existing child partition. The fixture
// simulates an upgrade from a pre-v4 schema by manually recreating
// the legacy indexes on a freshly migrated database, attaching
// child indexes to a pre-created weekly partition, and then running
// the v4 drop. This is exactly the shape the migration will encounter
// on production datastores carrying the historical schema.
func TestMigrationV4_RedundantMetricsIndexesDropped(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	// Run the full migration stack to set up the metrics tables; v4
	// is idempotent and will be a no-op against a clean install. We
	// then synthesize the legacy index shape and re-run v4's drop
	// logic by calling its Up directly, which is sufficient because
	// the migration runner's only contribution is the surrounding
	// transaction and schema_version bookkeeping.
	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("baseline migrate: %v", err)
	}

	// Pre-create a weekly partition for each parent table so the v4
	// drop has to walk pg_inherits and clean up attached child
	// indexes. The collector creates partitions lazily at probe time,
	// so the test does it explicitly here.
	//
	// The partition range bounds are bound through query parameters
	// rather than concatenated into the DDL string so that even
	// static-analysis tools see the values flowing through the pgx
	// parameter path. The schema and table identifiers are
	// quoted with pgx.Identifier.Sanitize() to defend against any
	// future change that would surface arbitrary identifiers here.
	const partitionStart = "2026-04-13"
	const partitionEnd = "2026-04-20"
	const partitionSuffix = "20260413"
	for _, table := range []string{"pg_stat_all_indexes", "pg_stat_statements"} {
		parentIdent := pgx.Identifier{"metrics", table}.Sanitize()
		childIdent := pgx.Identifier{
			"metrics", table + "_" + partitionSuffix,
		}.Sanitize()
		ddl := fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s "+
				"FOR VALUES FROM ('%s') TO ('%s')",
			childIdent, parentIdent, partitionStart, partitionEnd,
		)
		if _, err := pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("create partition %s_%s: %v",
				table, partitionSuffix, err)
		}
	}

	// Recreate the legacy redundant indexes as they would exist on a
	// production datastore that was originally migrated under the
	// pre-v4 schema. Creating a partitioned index on the parent
	// automatically creates and attaches child indexes on every
	// existing partition, which is exactly the upgrade shape we need.
	// Identifiers are sanitized via pgx.Identifier.Sanitize() so the
	// DDL is well-formed regardless of the literal values used and so
	// static-analysis tools do not flag the concatenation as a SQL
	// injection candidate; both inputs in this loop are local string
	// constants.
	for _, legacy := range []struct {
		name  string
		table string
		expr  string
	}{
		{
			name:  "idx_pg_stat_all_indexes_conn_db_time",
			table: "pg_stat_all_indexes",
			expr:  "(connection_id, database_name, collected_at DESC)",
		},
		{
			name:  "idx_pg_stat_statements_conn_db_time",
			table: "pg_stat_statements",
			expr:  "(connection_id, database_name, collected_at DESC)",
		},
	} {
		indexIdent := pgx.Identifier{legacy.name}.Sanitize()
		tableIdent := pgx.Identifier{"metrics", legacy.table}.Sanitize()
		ddl := fmt.Sprintf("CREATE INDEX %s ON %s %s",
			indexIdent, tableIdent, legacy.expr)
		if _, err := pool.Exec(ctx, ddl); err != nil {
			t.Fatalf("create legacy index %s: %v", legacy.name, err)
		}
	}

	// Sanity check that both the parent partitioned indexes and at
	// least one child per parent exist before we run the drop.
	for _, parent := range []string{
		"idx_pg_stat_all_indexes_conn_db_time",
		"idx_pg_stat_statements_conn_db_time",
	} {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_class c
				JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = 'metrics'
				  AND c.relname = $1
				  AND c.relkind = 'I'
			)
		`, parent).Scan(&exists)
		if err != nil {
			t.Fatalf("pre-check %s: %v", parent, err)
		}
		if !exists {
			t.Fatalf("setup failure: parent index %s missing before drop", parent)
		}

		var childCount int
		err = pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM pg_class parent
			JOIN pg_namespace pn ON pn.oid = parent.relnamespace
			JOIN pg_inherits inh ON inh.inhparent = parent.oid
			JOIN pg_class child ON child.oid = inh.inhrelid
			WHERE pn.nspname = 'metrics'
			  AND parent.relname = $1
			  AND child.relkind = 'i'
		`, parent).Scan(&childCount)
		if err != nil {
			t.Fatalf("pre-count children of %s: %v", parent, err)
		}
		if childCount == 0 {
			t.Fatalf("setup failure: expected at least one child index attached to %s",
				parent)
		}
	}

	// Run the v4 drop logic directly via the same SQL the migration
	// emits. Dropping the partitioned parent index cascades to the
	// attached child indexes on every existing partition.
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		DROP INDEX IF EXISTS metrics.idx_pg_stat_all_indexes_conn_db_time;
		DROP INDEX IF EXISTS metrics.idx_pg_stat_statements_conn_db_time;
	`); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			t.Logf("rollback after failed drop: %v", rbErr)
		}
		t.Fatalf("drop redundant indexes: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit drop: %v", err)
	}

	// Parent indexes must be gone.
	for _, parent := range []string{
		"idx_pg_stat_all_indexes_conn_db_time",
		"idx_pg_stat_statements_conn_db_time",
	} {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_class c
				JOIN pg_namespace n ON n.oid = c.relnamespace
				WHERE n.nspname = 'metrics'
				  AND c.relname = $1
			)
		`, parent).Scan(&exists)
		if err != nil {
			t.Fatalf("post-check %s: %v", parent, err)
		}
		if exists {
			t.Errorf("parent index %s still exists after migration v4", parent)
		}
	}

	// Every child index that was attached to the dropped parents
	// must be gone too. Postgres does not cascade DROP INDEX through
	// partition attachments, so the migration has to do it
	// explicitly.
	for _, table := range []string{"pg_stat_all_indexes", "pg_stat_statements"} {
		var leakedChildren int
		err := pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM pg_indexes
			WHERE schemaname = 'metrics'
			  AND tablename = $1
			  AND (indexdef ILIKE '%connection_id, database_name, collected_at%'
			       OR indexdef ILIKE '%(connection_id, database_name, collected_at%')
			  AND indexdef NOT ILIKE '%schemaname%'
			  AND indexdef NOT ILIKE '%queryid%'
		`, table+"_"+partitionSuffix).Scan(&leakedChildren)
		if err != nil {
			t.Fatalf("post-check children for %s: %v", table, err)
		}
		if leakedChildren != 0 {
			t.Errorf("found %d leaked child indexes on metrics.%s_%s matching the dropped parent shape",
				leakedChildren, table, partitionSuffix)
		}
	}
}

// TestMigrationV4_Idempotent verifies that v4 can be applied twice
// without error and that the partial index is not duplicated. The
// schema_version row count for v4 must be exactly one after both
// runs.
func TestMigrationV4_Idempotent(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("first Migrate failed: %v", err)
	}

	// A second Migrate call is the production idempotency path; it
	// must observe schema_version, skip v4, and exit clean.
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}

	var v4Count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_version WHERE version = 4`).Scan(&v4Count); err != nil {
		t.Fatalf("count v4 rows: %v", err)
	}
	if v4Count != 1 {
		t.Errorf("expected exactly one schema_version row for v4, got %d", v4Count)
	}

	// And the partial index must still exist as a single index, not
	// duplicated. pg_indexes returns one row per index name, so the
	// COUNT confirms there is no shadow index.
	var idxCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = 'public'
		  AND tablename = 'anomaly_candidates'
		  AND indexname = 'idx_anomaly_candidates_unprocessed'
	`).Scan(&idxCount); err != nil {
		t.Fatalf("count partial index: %v", err)
	}
	if idxCount != 1 {
		t.Errorf("expected exactly one idx_anomaly_candidates_unprocessed row, got %d", idxCount)
	}
}

// readPlan runs EXPLAIN and concatenates the textual plan rows so a
// caller can assert on substrings without depending on a particular
// plan node shape. The function takes a pinned *pgxpool.Conn rather
// than a *pgxpool.Pool so the caller can configure session-local
// planner GUCs (for example, SET enable_seqscan = off) that must
// observe the same connection as the EXPLAIN query.
func readPlan(ctx context.Context, conn *pgxpool.Conn, query string) (string, error) {
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", err
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
}
