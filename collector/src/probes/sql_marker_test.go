/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Tests covering the Workbench-internal SQL marker applied to the
// statements the collector runs against its own datastore, and the
// partition maintenance helpers that carry it. See GitHub issue #364.
package probes

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// assertTagged fails the test unless sql carries the internal marker in
// a position that PostgreSQL preserves in pg_stat_statements, which is
// to say after the leading keyword rather than before it.
func assertTagged(t *testing.T, what, sql string) {
	t.Helper()
	if !strings.Contains(sql, sqlmarker.Marker) {
		t.Errorf("%s is missing the internal marker: %s", what, sql)
		return
	}
	trimmed := strings.TrimLeft(sql, " \t\r\n")
	if strings.HasPrefix(trimmed, sqlmarker.Comment) {
		t.Errorf("%s carries the marker before the leading keyword, "+
			"where PostgreSQL strips it: %s", what, sql)
	}
}

// fakeRow implements pgx.Row for the partition existence check.
type fakeRow struct {
	exists  bool
	scanErr error
}

func (f *fakeRow) Scan(dest ...any) error {
	if f.scanErr != nil {
		return f.scanErr
	}
	if len(dest) == 1 {
		if p, ok := dest[0].(*bool); ok {
			*p = f.exists
		}
	}
	return nil
}

// fakeRows implements pgx.Rows over a fixed set of string rows, with
// optional Scan and iteration errors so every error branch in the
// partition helpers can be exercised.
type fakeRows struct {
	data    [][]string
	index   int
	scanErr error
	iterErr error
	closed  bool
}

func (f *fakeRows) Close()                                       { f.closed = true }
func (f *fakeRows) Err() error                                   { return f.iterErr }
func (f *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeRows) RawValues() [][]byte                          { return nil }
func (f *fakeRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeRows) Values() ([]any, error)                       { return nil, nil }

func (f *fakeRows) Next() bool {
	if f.index >= len(f.data) {
		return false
	}
	f.index++
	return true
}

func (f *fakeRows) Scan(dest ...any) error {
	if f.scanErr != nil {
		return f.scanErr
	}
	row := f.data[f.index-1]
	for i := range dest {
		if i >= len(row) {
			break
		}
		p, ok := dest[i].(*string)
		if !ok {
			return errors.New("fakeRows: destination is not *string")
		}
		*p = row[i]
	}
	return nil
}

// fakeQuerier implements DatastoreQuerier, recording every statement it
// is handed so tests can assert on the SQL actually issued.
type fakeQuerier struct {
	queries   []string
	queryArgs [][]any
	execs     []string
	rows      *fakeRows
	row       *fakeRow
	queryErr  error
	execErr   error
}

func (f *fakeQuerier) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.queries = append(f.queries, sql)
	f.queryArgs = append(f.queryArgs, args)
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.rows == nil {
		return &fakeRows{}, nil
	}
	return f.rows, nil
}

func (f *fakeQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.queries = append(f.queries, sql)
	f.queryArgs = append(f.queryArgs, args)
	if f.row == nil {
		return &fakeRow{}
	}
	return f.row
}

func (f *fakeQuerier) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.execs = append(f.execs, sql)
	return pgconn.CommandTag{}, f.execErr
}

// TestBuildMetricsInsert_Tagged covers the chokepoint from issue #364:
// every probe's metric writes are built here, and the resulting INSERT
// must carry the internal marker.
func TestBuildMetricsInsert_Tagged(t *testing.T) {
	fullTableName := pgx.Identifier{"metrics", "pg_stat_statements"}.Sanitize()
	columns := []string{"connection_id", "queryid"}
	batch := [][]any{{1, "abc"}, {2, "def"}}

	query, args := buildMetricsInsert(fullTableName, columns, batch)

	assertTagged(t, "metrics INSERT", query)
	if !strings.HasPrefix(query, "INSERT "+sqlmarker.Comment+" INTO ") {
		t.Errorf("marker is not immediately after the keyword: %s", query)
	}
	want := `INSERT ` + sqlmarker.Comment +
		` INTO "metrics"."pg_stat_statements" ` +
		`("connection_id", "queryid") VALUES ($1, $2), ($3, $4)`
	if query != want {
		t.Errorf("query = %q, want %q", query, want)
	}
	if len(args) != 4 {
		t.Fatalf("len(args) = %d, want 4", len(args))
	}
	if args[0] != 1 || args[1] != "abc" || args[2] != 2 || args[3] != "def" {
		t.Errorf("args = %v, want [1 abc 2 def]", args)
	}
}

// TestPartitionSQLBuilders_Tagged asserts every partition maintenance
// statement carries the marker.
func TestPartitionSQLBuilders_Tagged(t *testing.T) {
	weekStart := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.AddDate(0, 0, 7)

	cases := []struct {
		name string
		sql  string
	}{
		{"partitionExistsQuery", partitionExistsQuery},
		{"createPartitionSQL", createPartitionSQL(
			"pg_stat_activity_20260727",
			"pg_stat_activity", weekStart, weekEnd)},
		{"dropPartitionSQL", dropPartitionSQL("pg_settings_20260727")},
		{"protectedPartitionsQuery",
			protectedPartitionsQuery("pg_settings")},
		{"partitionCandidatesQuery", partitionCandidatesQuery},
		{"lastCollectionTimeQuery",
			lastCollectionTimeQuery("pg_stat_activity")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertTagged(t, tc.name, tc.sql)
		})
	}
}

// TestCreatePartitionSQL_Content confirms the tagging refactor did not
// disturb the DDL the collector has always issued, and that both
// relation names are quoted by pgx.Identifier.
func TestCreatePartitionSQL_Content(t *testing.T) {
	weekStart := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	weekEnd := weekStart.AddDate(0, 0, 7)

	got := createPartitionSQL("t_20260727", "t", weekStart, weekEnd)

	for _, want := range []string{
		"CREATE " + sqlmarker.Comment,
		`IF NOT EXISTS "metrics"."t_20260727"`,
		`PARTITION OF "metrics"."t"`,
		"FROM ('2026-07-27 00:00:00Z') TO ('2026-08-03 00:00:00Z')",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("createPartitionSQL missing %q: %s", want, got)
		}
	}
}

// TestDropPartitionSQL_QuotesIdentifier confirms the partition name is
// still quoted after the marker was introduced.
func TestDropPartitionSQL_QuotesIdentifier(t *testing.T) {
	got := dropPartitionSQL("pg_settings_20260727")
	want := `DROP ` + sqlmarker.Comment +
		` TABLE IF EXISTS "metrics"."pg_settings_20260727"`
	if got != want {
		t.Errorf("dropPartitionSQL = %q, want %q", got, want)
	}
}

// TestPartitionQueries_IdentifiersAreQuoted asserts the identifier
// handling the Semgrep suppressions in partition.go rely on: relation
// names that cannot be bound as placeholders are quoted with
// pgx.Identifier, and the one name that is a value binds as $1.
func TestPartitionQueries_IdentifiersAreQuoted(t *testing.T) {
	weekStart := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)

	// A name containing a quote proves the escaping is real rather than
	// incidental; production names never look like this.
	got := createPartitionSQL(`ev"il`, `ta"ble`, weekStart,
		weekStart.AddDate(0, 0, 7))
	for _, want := range []string{
		`"metrics"."ev""il"`,
		`"metrics"."ta""ble"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("createPartitionSQL missing %q: %s", want, got)
		}
	}

	prot := protectedPartitionsQuery(`ev"il`)
	if !strings.Contains(prot, `"metrics"."ev""il"`) {
		t.Errorf("protectedPartitionsQuery does not quote its table: %s",
			prot)
	}

	if strings.Contains(partitionCandidatesQuery, "%s") ||
		!strings.Contains(partitionCandidatesQuery, "p.relname = $1") {
		t.Errorf("partitionCandidatesQuery should bind the parent "+
			"table name: %s", partitionCandidatesQuery)
	}
}

// TestLoadPartitionCandidates_BindsTableName confirms the parent table
// name reaches PostgreSQL as a bind parameter rather than being
// interpolated into the catalog query.
func TestLoadPartitionCandidates_BindsTableName(t *testing.T) {
	q := &fakeQuerier{}
	if _, err := loadPartitionCandidates(
		context.Background(), q, "pg_stat_activity"); err != nil {
		t.Fatalf("loadPartitionCandidates() error = %v", err)
	}
	if len(q.queryArgs) != 1 || len(q.queryArgs[0]) != 1 ||
		q.queryArgs[0][0] != "pg_stat_activity" {
		t.Errorf("args = %v, want [pg_stat_activity]", q.queryArgs)
	}
	if strings.Contains(q.queries[0], "pg_stat_activity") {
		t.Errorf("table name was interpolated into the SQL: %s",
			q.queries[0])
	}
}

// TestEnsurePartition_FakeQuerier exercises EnsurePartition against a
// fake connection, covering the already-exists short circuit, the
// creation path, the duplicate-relation race, and the two error paths,
// whilst asserting that both statements carry the marker.
func TestEnsurePartition_FakeQuerier(t *testing.T) {
	ctx := context.Background()
	ts := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	t.Run("already exists", func(t *testing.T) {
		q := &fakeQuerier{row: &fakeRow{exists: true}}
		if err := EnsurePartition(ctx, q, "pg_stat_activity", ts); err != nil {
			t.Fatalf("EnsurePartition() error = %v", err)
		}
		if len(q.queries) != 1 {
			t.Fatalf("expected 1 query, got %d", len(q.queries))
		}
		assertTagged(t, "exists check", q.queries[0])
		if len(q.execs) != 0 {
			t.Errorf("expected no DDL, got %v", q.execs)
		}
	})

	t.Run("creates partition", func(t *testing.T) {
		q := &fakeQuerier{row: &fakeRow{exists: false}}
		if err := EnsurePartition(ctx, q, "pg_stat_activity", ts); err != nil {
			t.Fatalf("EnsurePartition() error = %v", err)
		}
		if len(q.execs) != 1 {
			t.Fatalf("expected 1 DDL statement, got %d", len(q.execs))
		}
		assertTagged(t, "create partition", q.execs[0])
		if !strings.Contains(q.execs[0],
			`"metrics"."pg_stat_activity_20260727"`) {
			t.Errorf("unexpected DDL: %s", q.execs[0])
		}
	})

	t.Run("exists check error", func(t *testing.T) {
		q := &fakeQuerier{row: &fakeRow{scanErr: errors.New("boom")}}
		err := EnsurePartition(ctx, q, "pg_stat_activity", ts)
		if err == nil ||
			!strings.Contains(err.Error(), "check if partition exists") {
			t.Fatalf("expected exists-check error, got %v", err)
		}
	})

	t.Run("duplicate relation is tolerated", func(t *testing.T) {
		q := &fakeQuerier{
			row:     &fakeRow{exists: false},
			execErr: &pgconn.PgError{Code: "42P07"},
		}
		if err := EnsurePartition(ctx, q, "pg_stat_activity", ts); err != nil {
			t.Fatalf("EnsurePartition() error = %v", err)
		}
	})

	t.Run("create error", func(t *testing.T) {
		q := &fakeQuerier{
			row:     &fakeRow{exists: false},
			execErr: errors.New("disk on fire"),
		}
		err := EnsurePartition(ctx, q, "pg_stat_activity", ts)
		if err == nil ||
			!strings.Contains(err.Error(), "failed to create partition") {
			t.Fatalf("expected create error, got %v", err)
		}
	})
}

// TestLoadProtectedPartitions_FakeQuerier covers the change-tracked and
// non-change-tracked branches plus every error path.
func TestLoadProtectedPartitions_FakeQuerier(t *testing.T) {
	ctx := context.Background()

	t.Run("not change tracked", func(t *testing.T) {
		q := &fakeQuerier{}
		got, err := loadProtectedPartitions(ctx, q, "pg_stat_activity")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty map, got %v", got)
		}
		if len(q.queries) != 0 {
			t.Errorf("expected no query, got %v", q.queries)
		}
	})

	t.Run("returns protected names", func(t *testing.T) {
		q := &fakeQuerier{rows: &fakeRows{
			data: [][]string{{"pg_settings_20260727"}},
		}}
		got, err := loadProtectedPartitions(ctx, q, "pg_settings")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if !got["pg_settings_20260727"] {
			t.Errorf("expected the partition to be protected, got %v", got)
		}
		assertTagged(t, "protected partitions query", q.queries[0])
	})

	t.Run("query error", func(t *testing.T) {
		q := &fakeQuerier{queryErr: errors.New("boom")}
		if _, err := loadProtectedPartitions(ctx, q, "pg_settings"); err == nil ||
			!strings.Contains(err.Error(), "query protected partitions") {
			t.Fatalf("expected query error, got %v", err)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		q := &fakeQuerier{rows: &fakeRows{
			data:    [][]string{{"x"}},
			scanErr: errors.New("bad scan"),
		}}
		if _, err := loadProtectedPartitions(ctx, q, "pg_settings"); err == nil ||
			!strings.Contains(err.Error(), "scan protected partition name") {
			t.Fatalf("expected scan error, got %v", err)
		}
	})

	t.Run("iteration error", func(t *testing.T) {
		q := &fakeQuerier{rows: &fakeRows{iterErr: errors.New("late")}}
		if _, err := loadProtectedPartitions(ctx, q, "pg_settings"); err == nil ||
			!strings.Contains(err.Error(), "iterating protected partitions") {
			t.Fatalf("expected iteration error, got %v", err)
		}
	})
}

// TestLoadPartitionCandidates_FakeQuerier covers the happy path and
// every error path of the catalog read.
func TestLoadPartitionCandidates_FakeQuerier(t *testing.T) {
	ctx := context.Background()

	t.Run("returns candidates", func(t *testing.T) {
		q := &fakeQuerier{rows: &fakeRows{data: [][]string{
			{"pg_stat_activity_20260720", "FOR VALUES FROM ('a') TO ('b')"},
		}}}
		got, err := loadPartitionCandidates(ctx, q, "pg_stat_activity")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(got) != 1 || got[0].name != "pg_stat_activity_20260720" {
			t.Fatalf("unexpected candidates: %v", got)
		}
		assertTagged(t, "partition candidates query", q.queries[0])
	})

	t.Run("query error", func(t *testing.T) {
		q := &fakeQuerier{queryErr: errors.New("boom")}
		if _, err := loadPartitionCandidates(ctx, q, "t"); err == nil ||
			!strings.Contains(err.Error(), "query partitions") {
			t.Fatalf("expected query error, got %v", err)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		q := &fakeQuerier{rows: &fakeRows{
			data:    [][]string{{"a", "b"}},
			scanErr: errors.New("bad scan"),
		}}
		if _, err := loadPartitionCandidates(ctx, q, "t"); err == nil ||
			!strings.Contains(err.Error(), "scan partition info") {
			t.Fatalf("expected scan error, got %v", err)
		}
	})

	t.Run("iteration error", func(t *testing.T) {
		q := &fakeQuerier{rows: &fakeRows{iterErr: errors.New("late")}}
		if _, err := loadPartitionCandidates(ctx, q, "t"); err == nil {
			t.Fatalf("expected iteration error, got nil")
		}
	})
}

// TestDropExpiredPartitions_FakeQuerier drives the retention sweep
// through a fake connection so the protected, unparseable, in-retention,
// expired, and failed-drop cases are all covered, and asserts the DROP
// statements carry the marker.
func TestDropExpiredPartitions_FakeQuerier(t *testing.T) {
	ctx := context.Background()

	// Bound expressions are parsed by parsePartitionEnd; use a bound
	// well in the past so the partition counts as expired.
	expiredBound := "FOR VALUES FROM ('2020-01-06 00:00:00+00') " +
		"TO ('2020-01-13 00:00:00+00')"
	futureBound := "FOR VALUES FROM ('2999-01-04 00:00:00+00') " +
		"TO ('2999-01-11 00:00:00+00')"

	t.Run("drops expired and keeps the rest", func(t *testing.T) {
		q := &fakeQuerier{rows: &fakeRows{data: [][]string{
			{"pg_stat_activity_20200106", expiredBound},
			{"pg_stat_activity_29990104", futureBound},
			{"pg_stat_activity_bogus", "nonsense"},
		}}}
		dropped, err := DropExpiredPartitions(
			ctx, q, "pg_stat_activity", 7)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if dropped != 1 {
			t.Fatalf("dropped = %d, want 1", dropped)
		}
		if len(q.execs) != 1 {
			t.Fatalf("expected 1 DROP, got %v", q.execs)
		}
		assertTagged(t, "drop partition", q.execs[0])
	})

	t.Run("skips protected partitions", func(t *testing.T) {
		// pg_settings is change-tracked, so the first query returns the
		// protected set and the second the candidate list. The fake
		// returns the same rows for both, which is enough to make the
		// candidate look protected.
		q := &fakeQuerier{rows: &fakeRows{data: [][]string{
			{"pg_settings_20200106", expiredBound},
		}}}
		dropped, err := DropExpiredPartitions(ctx, q, "pg_settings", 7)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if dropped != 0 {
			t.Errorf("dropped = %d, want 0", dropped)
		}
	})

	t.Run("protected query error", func(t *testing.T) {
		q := &fakeQuerier{queryErr: errors.New("boom")}
		if _, err := DropExpiredPartitions(
			ctx, q, "pg_settings", 7); err == nil {
			t.Fatal("expected an error from the protected query")
		}
	})

	t.Run("candidate query error", func(t *testing.T) {
		q := &fakeQuerier{queryErr: errors.New("boom")}
		if _, err := DropExpiredPartitions(
			ctx, q, "pg_stat_activity", 7); err == nil {
			t.Fatal("expected an error from the candidate query")
		}
	})

	t.Run("drop failure is logged and skipped", func(t *testing.T) {
		q := &fakeQuerier{
			rows: &fakeRows{data: [][]string{
				{"pg_stat_activity_20200106", expiredBound},
			}},
			execErr: errors.New("cannot drop"),
		}
		dropped, err := DropExpiredPartitions(
			ctx, q, "pg_stat_activity", 7)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if dropped != 0 {
			t.Errorf("dropped = %d, want 0", dropped)
		}
	})
}
