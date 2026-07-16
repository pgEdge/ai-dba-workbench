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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeRow implements pgx.Row to return a canned error, letting the test
// drive embeddingColumnTypeName's non-ErrNoRows error branch without a
// live database fault.
type fakeRow struct{ err error }

func (r fakeRow) Scan(dest ...any) error { return r.err }

// fakeQuerier returns a fakeRow carrying a fixed error from QueryRow.
type fakeQuerier struct{ err error }

func (q fakeQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return fakeRow(q)
}

// TestEmbeddingColumnTypeName_ErrorPaths exercises both Scan outcomes of
// embeddingColumnTypeName via a fake querier: pgx.ErrNoRows maps to an
// empty type name with no error, and any other error is wrapped.
func TestEmbeddingColumnTypeName_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	got, err := embeddingColumnTypeName(ctx,
		fakeQuerier{err: pgx.ErrNoRows}, "t", "c")
	if err != nil {
		t.Errorf("ErrNoRows should map to (\"\", nil); got err=%v", err)
	}
	if got != "" {
		t.Errorf("ErrNoRows should yield empty type, got %q", got)
	}

	sentinel := errors.New("boom")
	if _, err := embeddingColumnTypeName(ctx,
		fakeQuerier{err: sentinel}, "t", "c"); err == nil {
		t.Errorf("expected wrapped error for non-ErrNoRows Scan failure")
	} else if !errors.Is(err, sentinel) {
		t.Errorf("error should wrap the underlying cause, got %v", err)
	}
}

// columnTypeName returns format_type() for a public-schema column, the
// same probe migration #6 uses to decide whether a column still needs
// converting. Tests use it to assert the resulting column types.
func columnTypeName(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table, column string) string {
	t.Helper()
	var typeName string
	err := pool.QueryRow(ctx, `
		SELECT format_type(a.atttypid, a.atttypmod)
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relname = $1
		  AND a.attname = $2
		  AND a.attnum > 0
		  AND NOT a.attisdropped
	`, table, column).Scan(&typeName)
	if err != nil {
		t.Fatalf("read type of %s.%s: %v", table, column, err)
	}
	return typeName
}

// indexDef returns the indexdef for the named index, or an empty
// string when the index is absent.
func indexDef(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	var def string
	err := pool.QueryRow(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = $1
	`, name).Scan(&def)
	if err != nil {
		return ""
	}
	return def
}

// pgvectorHasHalfvec reports whether the installed pgvector build knows
// the halfvec type. Older pgvector releases lack it; the test degrades
// to a skip in that case, matching the migration's graceful behavior.
func pgvectorHasHalfvec(ctx context.Context, t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM pg_type WHERE typname = 'halfvec')
	`).Scan(&exists); err != nil {
		return false
	}
	return exists
}

// TestMigrationV6_FreshSchemaUsesHalfvec verifies that a brand-new
// install creates both embedding columns as halfvec(4000) with HNSW
// halfvec_cosine_ops indexes, with no manual upgrade step required.
func TestMigrationV6_FreshSchemaUsesHalfvec(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	if !pgvectorHasHalfvec(ctx, t, pool) {
		// halfvec presence is only knowable after the extension exists;
		// create it first so the probe is meaningful.
		if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
			t.Skipf("pgvector not installable: %v", err)
		}
		if !pgvectorHasHalfvec(ctx, t, pool) {
			t.Skip("pgvector build lacks halfvec; skipping")
		}
	}

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, tc := range []struct {
		table, index string
	}{
		{"chat_memories", "idx_chat_memories_embedding"},
		{"anomaly_embeddings", "idx_anomaly_embeddings_vector"},
	} {
		typeName := columnTypeName(ctx, t, pool, tc.table, "embedding")
		if !strings.HasPrefix(typeName, "halfvec") {
			t.Errorf("%s.embedding type = %q, want halfvec(...)", tc.table, typeName)
		}
		if !strings.Contains(typeName, "4000") {
			t.Errorf("%s.embedding type = %q, want width 4000", tc.table, typeName)
		}
		def := indexDef(ctx, t, pool, tc.index)
		if def == "" {
			t.Errorf("index %s missing on fresh schema", tc.index)
			continue
		}
		if !strings.Contains(def, "hnsw") || !strings.Contains(def, "halfvec_cosine_ops") {
			t.Errorf("index %s not HNSW halfvec_cosine_ops: %s", tc.index, def)
		}
	}
}

// TestMigrationV6_UpgradesLegacyVectorColumns proves the in-place
// upgrade path. It migrates a fresh database, rewrites both embedding
// columns back to the legacy vector(1536) shape with the old
// vector_cosine_ops indexes, seeds a populated row and a NULL row, then
// runs the migration #6 conversion logic. It asserts the columns become
// halfvec(4000), the populated row is zero-padded, the NULL row stays
// NULL, the index is rebuilt with halfvec_cosine_ops, and a second run
// is a no-op.
func TestMigrationV6_UpgradesLegacyVectorColumns(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		t.Skipf("pgvector not installable: %v", err)
	}
	if !pgvectorHasHalfvec(ctx, t, pool) {
		t.Skip("pgvector build lacks halfvec; skipping")
	}

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("baseline migrate: %v", err)
	}

	// Synthesize the pre-#6 (legacy) shape: revert both columns to
	// vector(1536) with the original vector_cosine_ops HNSW indexes,
	// exactly what a database originally provisioned under OpenAI dims
	// carries on disk.
	legacy := []struct {
		table, index string
	}{
		{"chat_memories", "idx_chat_memories_embedding"},
		{"anomaly_embeddings", "idx_anomaly_embeddings_vector"},
	}
	for _, l := range legacy {
		if _, err := pool.Exec(ctx,
			"DROP INDEX IF EXISTS "+l.index); err != nil {
			t.Fatalf("drop %s: %v", l.index, err)
		}
		if _, err := pool.Exec(ctx,
			"ALTER TABLE "+l.table+
				" ALTER COLUMN embedding TYPE vector(1536) USING NULL"); err != nil {
			t.Fatalf("revert %s.embedding to vector(1536): %v", l.table, err)
		}
		if _, err := pool.Exec(ctx,
			"CREATE INDEX "+l.index+" ON "+l.table+
				" USING hnsw (embedding vector_cosine_ops)"); err != nil {
			t.Fatalf("recreate legacy index %s: %v", l.index, err)
		}
		// Confirm the legacy shape is in place before the upgrade.
		if got := columnTypeName(ctx, t, pool, l.table, "embedding"); !strings.HasPrefix(got, "vector") {
			t.Fatalf("setup failure: %s.embedding type = %q, want vector(...)", l.table, got)
		}
	}

	// Seed chat_memories: one populated 1536-dim row and one NULL row.
	vec := "[" + strings.Repeat("0.5,", 1535) + "0.5]"
	var populatedID, nullID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_memories (username, scope, category, content, embedding, model_name)
		VALUES ('alice', 'user', 'c', 'populated', $1::vector, 'm')
		RETURNING id
	`, vec).Scan(&populatedID); err != nil {
		t.Fatalf("seed populated chat_memories row: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_memories (username, scope, category, content, embedding, model_name)
		VALUES ('alice', 'user', 'c', 'null-embed', NULL, 'm')
		RETURNING id
	`).Scan(&nullID); err != nil {
		t.Fatalf("seed null chat_memories row: %v", err)
	}

	// Run migration #6's conversion logic exactly as the migration does.
	runV6 := func() {
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		if err := upgradeEmbeddingColumnToHalfvec(ctx, tx,
			"chat_memories", "embedding", "idx_chat_memories_embedding",
			"CREATE INDEX IF NOT EXISTS idx_chat_memories_embedding "+
				"ON chat_memories USING hnsw (embedding halfvec_cosine_ops)",
		); err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				t.Logf("rollback chat_memories upgrade: %v", rbErr)
			}
			t.Fatalf("upgrade chat_memories: %v", err)
		}
		if err := upgradeEmbeddingColumnToHalfvec(ctx, tx,
			"anomaly_embeddings", "embedding", "idx_anomaly_embeddings_vector",
			"CREATE INDEX IF NOT EXISTS idx_anomaly_embeddings_vector "+
				"ON anomaly_embeddings USING hnsw (embedding halfvec_cosine_ops)",
		); err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				t.Logf("rollback anomaly_embeddings upgrade: %v", rbErr)
			}
			t.Fatalf("upgrade anomaly_embeddings: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit upgrade: %v", err)
		}
	}

	runV6()

	// Columns must now be halfvec(4000) with rebuilt halfvec indexes.
	for _, l := range legacy {
		typeName := columnTypeName(ctx, t, pool, l.table, "embedding")
		if !strings.HasPrefix(typeName, "halfvec") || !strings.Contains(typeName, "4000") {
			t.Errorf("%s.embedding type = %q, want halfvec(4000)", l.table, typeName)
		}
		def := indexDef(ctx, t, pool, l.index)
		if !strings.Contains(def, "halfvec_cosine_ops") {
			t.Errorf("index %s not rebuilt with halfvec_cosine_ops: %s", l.index, def)
		}
	}

	// The populated row must be widened to 4000 dims and its leading
	// values preserved; the NULL row must stay NULL.
	var dims int
	if err := pool.QueryRow(ctx,
		`SELECT vector_dims(embedding) FROM chat_memories WHERE id = $1`,
		populatedID).Scan(&dims); err != nil {
		t.Fatalf("read padded dims: %v", err)
	}
	if dims != 4000 {
		t.Errorf("populated row dims = %d, want 4000", dims)
	}
	var first float64
	if err := pool.QueryRow(ctx,
		`SELECT (embedding::real[])[1] FROM chat_memories WHERE id = $1`,
		populatedID).Scan(&first); err != nil {
		t.Fatalf("read first element: %v", err)
	}
	if first < 0.49 || first > 0.51 {
		t.Errorf("padded leading value = %v, want ~0.5", first)
	}
	var trailing float64
	if err := pool.QueryRow(ctx,
		`SELECT (embedding::real[])[4000] FROM chat_memories WHERE id = $1`,
		populatedID).Scan(&trailing); err != nil {
		t.Fatalf("read trailing element: %v", err)
	}
	if trailing != 0 {
		t.Errorf("trailing pad value = %v, want 0", trailing)
	}
	var isNull bool
	if err := pool.QueryRow(ctx,
		`SELECT embedding IS NULL FROM chat_memories WHERE id = $1`,
		nullID).Scan(&isNull); err != nil {
		t.Fatalf("read null row: %v", err)
	}
	if !isNull {
		t.Errorf("NULL embedding row was not preserved as NULL")
	}

	// Re-running migration #6 must be a clean no-op: the columns are
	// already halfvec, so the type probe short-circuits without touching
	// the data or index.
	runV6()
	for _, l := range legacy {
		typeName := columnTypeName(ctx, t, pool, l.table, "embedding")
		if !strings.HasPrefix(typeName, "halfvec") {
			t.Errorf("after re-run %s.embedding type = %q, want halfvec", l.table, typeName)
		}
	}
	// The padded row must be untouched by the no-op re-run.
	if err := pool.QueryRow(ctx,
		`SELECT vector_dims(embedding) FROM chat_memories WHERE id = $1`,
		populatedID).Scan(&dims); err != nil {
		t.Fatalf("re-read padded dims: %v", err)
	}
	if dims != 4000 {
		t.Errorf("after re-run populated row dims = %d, want 4000", dims)
	}
}

// TestUpgradeEmbeddingColumn_AbsentAndUnexpectedType covers the two
// non-conversion branches of upgradeEmbeddingColumnToHalfvec: a missing
// table/column is a clean no-op, and a column of an unexpected type is
// refused rather than cast destructively.
func TestUpgradeEmbeddingColumn_AbsentAndUnexpectedType(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	// Absent table: embeddingColumnTypeName returns "" so the upgrade is
	// a no-op and commits cleanly.
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := upgradeEmbeddingColumnToHalfvec(ctx, tx,
		"no_such_table", "embedding", "no_such_index",
		"CREATE INDEX IF NOT EXISTS no_such_index ON no_such_table USING hnsw (embedding halfvec_cosine_ops)",
	); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			t.Logf("rollback absent-table tx: %v", rbErr)
		}
		t.Fatalf("absent table should be a no-op, got: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit absent-table no-op: %v", err)
	}

	// Type probe must also report "" for a wholly absent table.
	tx2, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin probe: %v", err)
	}
	got, err := embeddingColumnTypeName(ctx, tx2, "still_missing", "embedding")
	if err != nil {
		if rbErr := tx2.Rollback(ctx); rbErr != nil {
			t.Logf("rollback probe tx: %v", rbErr)
		}
		t.Fatalf("type probe of missing table: %v", err)
	}
	if got != "" {
		t.Errorf("missing table type = %q, want empty", got)
	}
	if rbErr := tx2.Rollback(ctx); rbErr != nil {
		t.Logf("rollback probe tx: %v", rbErr)
	}

	// Unexpected type: a non-vector embedding column must be refused.
	if _, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS weird_embed CASCADE;
		CREATE TABLE weird_embed (id BIGSERIAL PRIMARY KEY, embedding INTEGER)
	`); err != nil {
		t.Fatalf("create weird_embed: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS weird_embed CASCADE`); err != nil {
			t.Logf("drop weird_embed: %v", err)
		}
	}()

	tx3, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin unexpected-type: %v", err)
	}
	err = upgradeEmbeddingColumnToHalfvec(ctx, tx3,
		"weird_embed", "embedding", "weird_embed_idx",
		"CREATE INDEX IF NOT EXISTS weird_embed_idx ON weird_embed USING hnsw (embedding halfvec_cosine_ops)",
	)
	if err == nil {
		t.Errorf("expected refusal for unexpected column type")
	}
	if rbErr := tx3.Rollback(ctx); rbErr != nil {
		t.Logf("rollback unexpected-type tx: %v", rbErr)
	}
}

// TestUpgradeEmbeddingColumn_WrongHalfvecWidth proves that a column
// already sitting at a halfvec width other than the 4000-dim target is
// rejected rather than silently treated as migrated. The application
// pads and casts every embedding to halfvec(4000), so a stray
// halfvec(1536) would only fail at runtime; the migration must catch it.
func TestUpgradeEmbeddingColumn_WrongHalfvecWidth(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		t.Skipf("pgvector not installable: %v", err)
	}
	if !pgvectorHasHalfvec(ctx, t, pool) {
		t.Skip("pgvector build lacks halfvec; skipping")
	}

	// A column already at the wrong halfvec width simulates a partial or
	// manual migration; the upgrade must refuse it rather than skip it.
	if _, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS wrong_width CASCADE;
		CREATE TABLE wrong_width (id BIGSERIAL PRIMARY KEY, embedding halfvec(1536))
	`); err != nil {
		t.Fatalf("create wrong_width: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS wrong_width CASCADE`); err != nil {
			t.Logf("drop wrong_width: %v", err)
		}
	}()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin wrong-width: %v", err)
	}
	err = upgradeEmbeddingColumnToHalfvec(ctx, tx,
		"wrong_width", "embedding", "wrong_width_idx",
		"CREATE INDEX IF NOT EXISTS wrong_width_idx ON wrong_width USING hnsw (embedding halfvec_cosine_ops)",
	)
	if err == nil {
		t.Errorf("expected error for halfvec width other than 4000")
	} else if !strings.Contains(err.Error(), "unexpected halfvec width") {
		t.Errorf("error should name the unexpected halfvec width, got: %v", err)
	}
	if rbErr := tx.Rollback(ctx); rbErr != nil {
		t.Logf("rollback wrong-width tx: %v", rbErr)
	}

	// A column already at the exact target type is a clean no-op, proving
	// the strict guard still lets a correctly migrated column pass.
	if _, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS right_width CASCADE;
		CREATE TABLE right_width (id BIGSERIAL PRIMARY KEY, embedding halfvec(4000))
	`); err != nil {
		t.Fatalf("create right_width: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS right_width CASCADE`); err != nil {
			t.Logf("drop right_width: %v", err)
		}
	}()

	tx2, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin right-width: %v", err)
	}
	if err := upgradeEmbeddingColumnToHalfvec(ctx, tx2,
		"right_width", "embedding", "right_width_idx",
		"CREATE INDEX IF NOT EXISTS right_width_idx ON right_width USING hnsw (embedding halfvec_cosine_ops)",
	); err != nil {
		if rbErr := tx2.Rollback(ctx); rbErr != nil {
			t.Logf("rollback right-width tx: %v", rbErr)
		}
		t.Fatalf("halfvec(4000) column should be a no-op, got: %v", err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit right-width no-op: %v", err)
	}
}

// TestUpgradeEmbeddingColumn_StepFailures covers the conversion error
// branches: a malformed CREATE INDEX statement after a successful column
// conversion, and an ALTER that cannot complete because a stored vector
// already exceeds the 4000-dim target (array_fill would need a negative
// length). Both run inside their own SAVEPOINT, so the failure rolls
// back cleanly and the outer transaction stays usable.
func TestUpgradeEmbeddingColumn_StepFailures(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	if pool == nil {
		return
	}
	defer pool.Close()
	defer conn.Release()

	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		t.Skipf("pgvector not installable: %v", err)
	}
	if !pgvectorHasHalfvec(ctx, t, pool) {
		t.Skip("pgvector build lacks halfvec; skipping")
	}

	// Recreate-index failure: a small vector(3) table converts fine, but
	// a syntactically broken CREATE INDEX statement triggers the
	// recreate-index error return.
	if _, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS recreate_fail CASCADE;
		CREATE TABLE recreate_fail (id BIGSERIAL PRIMARY KEY, embedding vector(3))
	`); err != nil {
		t.Fatalf("create recreate_fail: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS recreate_fail CASCADE`); err != nil {
			t.Logf("drop recreate_fail: %v", err)
		}
	}()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	err = upgradeEmbeddingColumnToHalfvec(ctx, tx,
		"recreate_fail", "embedding", "recreate_fail_idx",
		"CREATE INDEX recreate_fail_idx ON recreate_fail USING hnsw "+
			"(embedding NOT_A_REAL_OPCLASS)",
	)
	if err == nil {
		t.Errorf("expected recreate-index failure")
	}
	if rbErr := tx.Rollback(ctx); rbErr != nil {
		t.Logf("rollback recreate-fail tx: %v", rbErr)
	}

	// Drop-index failure: a syntactically invalid index name makes the
	// DROP INDEX statement itself error, covering that guard. The column
	// is a valid vector(3) so the type probe passes and the DROP runs.
	if _, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS drop_fail CASCADE;
		CREATE TABLE drop_fail (id BIGSERIAL PRIMARY KEY, embedding vector(3))
	`); err != nil {
		t.Fatalf("create drop_fail: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS drop_fail CASCADE`); err != nil {
			t.Logf("drop drop_fail: %v", err)
		}
	}()
	txd, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin drop-fail: %v", err)
	}
	err = upgradeEmbeddingColumnToHalfvec(ctx, txd,
		"drop_fail", "embedding", "bad index name );DROP",
		"CREATE INDEX IF NOT EXISTS drop_fail_idx ON drop_fail USING hnsw "+
			"(embedding halfvec_cosine_ops)",
	)
	if err == nil {
		t.Errorf("expected drop-index failure for invalid index name")
	}
	if rbErr := txd.Rollback(ctx); rbErr != nil {
		t.Logf("rollback drop-fail tx: %v", rbErr)
	}

	// Alter failure: store a vector wider than the 4000-dim target so the
	// padding expression's array_fill receives a negative length and the
	// ALTER ... USING cast errors out.
	wide := "[" + strings.Repeat("0.1,", 4099) + "0.1]"
	if _, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS alter_fail CASCADE;
		CREATE TABLE alter_fail (id BIGSERIAL PRIMARY KEY, embedding vector(4100))
	`); err != nil {
		t.Fatalf("create alter_fail: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS alter_fail CASCADE`); err != nil {
			t.Logf("drop alter_fail: %v", err)
		}
	}()
	if _, err := pool.Exec(ctx,
		`INSERT INTO alter_fail (embedding) VALUES ($1::vector)`, wide); err != nil {
		t.Fatalf("seed wide vector: %v", err)
	}

	tx2, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin alter-fail: %v", err)
	}
	err = upgradeEmbeddingColumnToHalfvec(ctx, tx2,
		"alter_fail", "embedding", "alter_fail_idx",
		"CREATE INDEX IF NOT EXISTS alter_fail_idx ON alter_fail USING hnsw "+
			"(embedding halfvec_cosine_ops)",
	)
	if err == nil {
		t.Errorf("expected alter-column failure for over-width vector")
	}
	if rbErr := tx2.Rollback(ctx); rbErr != nil {
		t.Logf("rollback alter-fail tx: %v", rbErr)
	}
}

// TestMigrationV6_Idempotent verifies the full migration runner applies
// version 6 exactly once and that a second Migrate call skips it.
func TestMigrationV6_Idempotent(t *testing.T) {
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
		t.Fatalf("first Migrate: %v", err)
	}
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	var v6Count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_version WHERE version = 6`).Scan(&v6Count); err != nil {
		t.Fatalf("count v6 rows: %v", err)
	}
	if v6Count != 1 {
		t.Errorf("expected exactly one schema_version row for v6, got %d", v6Count)
	}
}
