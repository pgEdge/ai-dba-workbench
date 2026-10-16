/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"context"
	"testing"
)

// TestRedactStatementText checks that only the named text columns are
// redacted, and that NULL and non-text values are left alone.
func TestRedactStatementText(t *testing.T) {
	rows := []map[string]any{
		{
			"query":    "ALTER ROLE app PASSWORD 'hunter2'",
			"other":    "ALTER ROLE app PASSWORD 'hunter2'",
			"calls":    int64(3),
			"username": "app",
		},
		{"query": nil, "calls": int64(1)},
		{"query": "SELECT $1", "calls": int64(2)},
		{
			"ddl":   []byte("x"),
			"query": "CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=s3cret' PUBLICATION p",
		},
	}

	redactStatementText(rows, "query", "ddl")

	if got, want := rows[0]["query"], "ALTER ROLE app PASSWORD '********'"; got != want {
		t.Errorf("row 0 query = %v, want %q", got, want)
	}
	if got := rows[0]["other"]; got != "ALTER ROLE app PASSWORD 'hunter2'" {
		t.Errorf("row 0 other = %v, want it untouched", got)
	}
	if rows[0]["calls"] != int64(3) || rows[0]["username"] != "app" {
		t.Errorf("row 0 non-text columns changed: %v", rows[0])
	}
	if rows[1]["query"] != nil {
		t.Errorf("row 1 query = %v, want nil", rows[1]["query"])
	}
	if got := rows[2]["query"]; got != "SELECT $1" {
		t.Errorf("row 2 query = %v, want it untouched", got)
	}
	if got, want := rows[3]["query"],
		"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=********' PUBLICATION p"; got != want {
		t.Errorf("row 3 query = %v, want %q", got, want)
	}
	if b, ok := rows[3]["ddl"].([]byte); !ok || string(b) != "x" {
		t.Errorf("row 3 ddl = %v, want the []byte untouched", rows[3]["ddl"])
	}
}

// TestStatementProbes_ExecuteErrors checks the probes that redact
// statement text return the error, rather than unredacted rows, when the
// query fails before or after the first row. Temporary views shadow the
// system catalogs; see version_branch_test.go.
func TestStatementProbes_ExecuteErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("activity query fails", func(t *testing.T) {
		conn := shadowViews(t, `CREATE TEMP VIEW pg_stat_activity AS SELECT 1 AS pid`)
		p := NewPgStatActivityProbe(&ProbeConfig{Name: ProbeNamePgStatActivity})
		rows, err := p.Execute(ctx, "activity-query-fails", conn, detectPgVersion(t, conn))
		if err == nil || rows != nil {
			t.Fatalf("Execute() = %v, %v; want an error and no rows", rows, err)
		}
	})

	t.Run("activity scan fails", func(t *testing.T) {
		conn := shadowViews(t, `CREATE TEMP VIEW pg_stat_activity AS
            SELECT a.* FROM pg_catalog.pg_stat_activity a
            CROSS JOIN generate_series(0, 1) g WHERE 1 / g > 0`)
		p := NewPgStatActivityProbe(&ProbeConfig{Name: ProbeNamePgStatActivity})
		rows, err := p.Execute(ctx, "activity-scan-fails", conn, detectPgVersion(t, conn))
		if err == nil || rows != nil {
			t.Fatalf("Execute() = %v, %v; want an error and no rows", rows, err)
		}
	})

	t.Run("spock query fails", func(t *testing.T) {
		conn := shadowViews(t, `CREATE TEMP VIEW pg_extension AS
            SELECT 'spock'::name AS extname`)
		var present bool
		if err := conn.QueryRow(ctx,
			`SELECT to_regclass('spock.exception_log') IS NOT NULL`).Scan(&present); err != nil {
			t.Fatalf("check for spock.exception_log: %v", err)
		}
		if present {
			t.Skip("spock.exception_log exists, so the query would succeed")
		}
		p := NewSpockExceptionLogProbe(&ProbeConfig{Name: ProbeNameSpockExceptionLog})
		rows, err := p.Execute(ctx, "spock-query-fails", conn, 16)
		if err == nil || rows != nil {
			t.Fatalf("Execute() = %v, %v; want an error and no rows", rows, err)
		}
	})
}
