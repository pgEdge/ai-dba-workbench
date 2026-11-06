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
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// scriptedRow scans fixed values, or fails with err.
type scriptedRow struct {
	values []any
	err    error
}

func (r scriptedRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan into %d destinations, row has %d values", len(dest), len(r.values))
	}
	for i, d := range dest {
		ok := false
		switch p := d.(type) {
		case *bool:
			*p, ok = r.values[i].(bool)
		case *[]string:
			*p, ok = r.values[i].([]string)
		case *string:
			*p, ok = r.values[i].(string)
		}
		if !ok {
			return fmt.Errorf("cannot scan %T into %T", r.values[i], d)
		}
	}
	return nil
}

// scriptedQuerier answers each QueryRow with the next scripted row.
type scriptedQuerier struct {
	rows []scriptedRow
	sql  []string
}

func (q *scriptedQuerier) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	q.sql = append(q.sql, sql)
	if len(q.rows) == 0 {
		return scriptedRow{err: errors.New("unexpected query")}
	}
	r := q.rows[0]
	q.rows = q.rows[1:]
	return r
}

func extensionRow(installed, recentEnough, canAttach bool) scriptedRow {
	return scriptedRow{values: []any{installed, recentEnough, canAttach}}
}

func relationsRow(tables, views []string) scriptedRow {
	return scriptedRow{values: []any{tables, views}}
}

func TestCheckMetricsStorage(t *testing.T) {
	errBoom := errors.New("boom")
	many := []string{"pg_database", "pg_settings", "pg_stat_activity", "pg_stat_wal", "pg_stat_io"}

	tests := []struct {
		name      string
		coldFront bool
		rows      []scriptedRow
		wantErr   []string
		wantSQL   int
	}{
		{
			name: "default mode on a fresh datastore",
			rows: []scriptedRow{relationsRow(nil, nil)}, wantSQL: 1,
		},
		{
			name: "default mode on partitioned tables",
			rows: []scriptedRow{relationsRow(many, nil)}, wantSQL: 1,
		},
		{
			name:    "default mode on ColdFront views",
			rows:    []scriptedRow{relationsRow(nil, []string{"pg_stat_wal"})},
			wantErr: []string{"use_coldfront_for_metrics is disabled", "metrics.pg_stat_wal", "not supported"},
			wantSQL: 1,
		},
		{
			name:    "default mode relation query fails",
			rows:    []scriptedRow{{err: errBoom}},
			wantErr: []string{"failed to inspect existing metrics tables", "boom"},
			wantSQL: 1,
		},
		{
			name: "ColdFront on a fresh datastore", coldFront: true,
			rows: []scriptedRow{extensionRow(true, true, true), relationsRow(nil, nil)}, wantSQL: 2,
		},
		{
			name: "ColdFront on ColdFront views", coldFront: true,
			rows: []scriptedRow{extensionRow(true, true, true), relationsRow(nil, many)}, wantSQL: 2,
		},
		{
			name: "ColdFront without the extension", coldFront: true,
			rows:    []scriptedRow{extensionRow(false, false, false)},
			wantErr: []string{"coldfront extension is not installed", "pgEdge/coldfront#92", "CREATE EXTENSION coldfront"},
			wantSQL: 1,
		},
		{
			name: "ColdFront with a build that is too old", coldFront: true,
			rows:    []scriptedRow{extensionRow(true, false, true)},
			wantErr: []string{"too old", "coldfront._partition_clause(jsonb,text[])", "pgEdge/coldfront#92"},
			wantSQL: 1,
		},
		{
			name: "ColdFront build without ensure_attached", coldFront: true,
			rows:    []scriptedRow{extensionRow(true, true, false)},
			wantErr: []string{"too old", "coldfront.ensure_attached()", "pgEdge/coldfront#92"},
			wantSQL: 1,
		},
		{
			name: "ColdFront extension query fails", coldFront: true,
			rows:    []scriptedRow{{err: errBoom}},
			wantErr: []string{"failed to check for the coldfront extension", "boom"},
			wantSQL: 1,
		},
		{
			name: "ColdFront on partitioned tables", coldFront: true,
			rows: []scriptedRow{extensionRow(true, true, true), relationsRow(many, nil)},
			wantErr: []string{
				"use_coldfront_for_metrics is enabled", "PostgreSQL tables",
				"metrics.pg_database, metrics.pg_settings, metrics.pg_stat_activity and 2 more",
			},
			wantSQL: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &scriptedQuerier{rows: tt.rows}
			err := CheckMetricsStorage(context.Background(), q, tt.coldFront)
			if len(tt.wantErr) == 0 && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatal("expected an error")
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not mention %q", err, want)
					}
				}
			}
			if len(q.sql) != tt.wantSQL {
				t.Errorf("issued %d queries, want %d", len(q.sql), tt.wantSQL)
			}
		})
	}
}

// scriptedConn is a datastore connection outside a transaction: it
// answers QueryRow from a scriptedQuerier and records each Exec, failing
// the one whose 1-based position is failExec.
type scriptedConn struct {
	scriptedQuerier
	execs    []string
	failExec int
	execErr  error

	// tx is what Begin returns; a fresh helperTx owned by the
	// collector when nil. beginErr fails Begin.
	tx       *helperTx
	beginErr error
}

func (c *scriptedConn) Begin(context.Context) (pgx.Tx, error) {
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	if c.tx == nil {
		c.tx = &helperTx{owned: true}
	}
	return c.tx, nil
}

func (c *scriptedConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	c.execs = append(c.execs, sql)
	if len(c.execs) == c.failExec {
		return pgconn.CommandTag{}, c.execErr
	}
	return pgconn.CommandTag{}, nil
}

func TestEnsureColdFrontNamespace(t *testing.T) {
	want := []string{
		"SELECT coldfront.ensure_attached()",
		"SELECT duckdb.raw_query('CREATE SCHEMA IF NOT EXISTS ice.metrics')",
	}

	c := &scriptedConn{}
	if err := EnsureColdFrontNamespace(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.execs, want) {
		t.Errorf("issued %q, want %q", c.execs, want)
	}

	for failAt := 1; failAt <= len(want); failAt++ {
		c := &scriptedConn{failExec: failAt, execErr: errors.New("NotFound_404")}
		err := EnsureColdFrontNamespace(context.Background(), c)
		if err == nil {
			t.Fatalf("failing statement %d: no error", failAt)
		}
		for _, s := range []string{`"metrics"`, "coldfront.warehouse", "reachable and writable", "NotFound_404"} {
			if !strings.Contains(err.Error(), s) {
				t.Errorf("failing statement %d: error %q does not mention %q", failAt, err, s)
			}
		}
		if len(c.execs) != failAt {
			t.Errorf("failing statement %d: issued %d statements", failAt, len(c.execs))
		}
	}
}

func TestPrepareMetricsStorage(t *testing.T) {
	ctx := context.Background()

	// Default mode: checks only, never touches Iceberg.
	c := &scriptedConn{scriptedQuerier: scriptedQuerier{rows: []scriptedRow{relationsRow(nil, nil)}}}
	if err := PrepareMetricsStorage(ctx, c, false); err != nil {
		t.Fatal(err)
	}
	if len(c.execs) != 0 {
		t.Errorf("default mode issued %q", c.execs)
	}

	// ColdFront: checks, then creates the namespace.
	c = &scriptedConn{scriptedQuerier: scriptedQuerier{rows: []scriptedRow{
		extensionRow(true, true, true), relationsRow(nil, nil)}}}
	if err := PrepareMetricsStorage(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	if len(c.execs) != 2 || c.tx == nil || !c.tx.committed {
		t.Errorf("ColdFront mode issued %q and committed the helper %v, want the two namespace statements and a committed helper", c.execs, c.tx != nil && c.tx.committed)
	}

	// A failed check stops before the namespace is created.
	c = &scriptedConn{scriptedQuerier: scriptedQuerier{rows: []scriptedRow{extensionRow(false, false, false)}}}
	if err := PrepareMetricsStorage(ctx, c, true); err == nil {
		t.Fatal("PrepareMetricsStorage ignored a missing extension")
	}
	if len(c.execs) != 0 {
		t.Errorf("namespace statements issued after a failed check: %q", c.execs)
	}
}

func TestDescribeMetricsRelations(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"a"}, "metrics.a"},
		{[]string{"a", "b", "c"}, "metrics.a, metrics.b, metrics.c"},
		{[]string{"a", "b", "c", "d"}, "metrics.a, metrics.b, metrics.c and 1 more"},
	}
	for _, tt := range tests {
		if got := describeMetricsRelations(tt.in); got != tt.want {
			t.Errorf("describeMetricsRelations(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestCheckMetricsStorageAgainstDatastore runs both catalog queries on
// a real server: a migrated default-mode datastore passes in the default
// mode, ColdFront mode is refused for the missing extension, and a
// metrics view in place of a table is refused in the default mode.
func TestCheckMetricsStorageAgainstDatastore(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()
	cleanupTestSchema(t, pool)
	defer cleanupTestSchema(t, pool)

	// A fresh datastore has no metrics schema at all.
	if err := CheckMetricsStorage(ctx, conn, false); err != nil {
		t.Fatalf("fresh datastore refused in the default mode: %v", err)
	}

	if err := NewSchemaManager().Migrate(conn); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}
	if err := CheckMetricsStorage(ctx, conn, false); err != nil {
		t.Errorf("migrated datastore refused in the default mode: %v", err)
	}
	err := CheckMetricsStorage(ctx, conn, true)
	if err == nil || !strings.Contains(err.Error(), "coldfront extension is not installed") {
		t.Errorf("ColdFront mode without the extension: error = %v", err)
	}

	if _, err := conn.Exec(ctx, `
        DROP TABLE metrics.pg_connectivity;
        CREATE VIEW metrics.pg_connectivity AS SELECT 1 AS connection_id;
    `); err != nil {
		t.Fatal(err)
	}
	err = CheckMetricsStorage(ctx, conn, false)
	if err == nil || !strings.Contains(err.Error(), "metrics.pg_connectivity") {
		t.Errorf("default mode over a metrics view: error = %v", err)
	}
}
