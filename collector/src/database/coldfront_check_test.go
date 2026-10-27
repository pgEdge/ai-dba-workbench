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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
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

func extensionRow(installed, recentEnough bool) scriptedRow {
	return scriptedRow{values: []any{installed, recentEnough}}
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
			rows: []scriptedRow{extensionRow(true, true), relationsRow(nil, nil)}, wantSQL: 2,
		},
		{
			name: "ColdFront on ColdFront views", coldFront: true,
			rows: []scriptedRow{extensionRow(true, true), relationsRow(nil, many)}, wantSQL: 2,
		},
		{
			name: "ColdFront without the extension", coldFront: true,
			rows:    []scriptedRow{extensionRow(false, false)},
			wantErr: []string{"coldfront extension is not installed", "pgEdge/coldfront#92", "CREATE EXTENSION coldfront"},
			wantSQL: 1,
		},
		{
			name: "ColdFront with a build that is too old", coldFront: true,
			rows:    []scriptedRow{extensionRow(true, false)},
			wantErr: []string{"too old", "coldfront._partition_clause(jsonb,text[])", "pgEdge/coldfront#92"},
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
			rows: []scriptedRow{extensionRow(true, true), relationsRow(many, nil)},
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
