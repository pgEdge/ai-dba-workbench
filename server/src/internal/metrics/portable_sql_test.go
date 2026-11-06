/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestIntListArg(t *testing.T) {
	tests := []struct {
		ids  []int
		want string
	}{
		{nil, ""},
		{[]int{}, ""},
		{[]int{7}, "7"},
		{[]int{2, 3, -1}, "2,3,-1"},
	}
	for _, tt := range tests {
		if got := IntListArg(tt.ids); got != tt.want {
			t.Errorf("IntListArg(%v) = %q, want %q", tt.ids, got, tt.want)
		}
	}
	if got := IntArrayExpr("$4"); got != "string_to_array($4, ',')::integer[]" {
		t.Errorf("IntArrayExpr = %q", got)
	}
	if got := EpochSeconds("a - b"); got != "EXTRACT(EPOCH FROM a - b)::float8" {
		t.Errorf("EpochSeconds = %q", got)
	}
}

func TestAggregateExpr(t *testing.T) {
	tests := []struct {
		agg, dataType, want string
	}{
		{"avg", "bigint", "avg(x)::float8"},
		{"avg", "numeric", "avg(x)::float8"},
		{"avg", "double precision", "avg(x)::float8"},
		{"avg", "interval", "avg(x)"},
		{"sum", "integer", "sum(x::bigint)"},
		{"sum", "smallint", "sum(x::bigint)"},
		{"sum", "bigint", "sum(x)"},
		{"sum", "double precision", "sum(x)"},
		{"sum", "interval", "(sum(EXTRACT(EPOCH FROM x)::float8) * INTERVAL '1 second')"},
		{"min", "integer", "min(x)"},
		{"max", "interval", "max(x)"},
		{"count", "text", "count(x)"},
	}
	for _, tt := range tests {
		if got := AggregateExpr(tt.agg, "x", tt.dataType); got != tt.want {
			t.Errorf("AggregateExpr(%s, %s) = %q, want %q", tt.agg, tt.dataType, got, tt.want)
		}
	}
}

// TestPortableSQLMatchesPlainPostgreSQL checks, on plain PostgreSQL, that
// each portable shape gives the value the shape it replaces gave there,
// so the ColdFront fixes (issue #755) change no result.
func TestPortableSQLMatchesPlainPostgreSQL(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	ctx := context.Background()

	const values = `(VALUES (1, 10, 100::bigint, 1.5::float8, interval '1 day 2 hours'),
                           (2, 20, 300::bigint, 2.5::float8, interval '30 minutes'),
                           (3, 33, 500::bigint, 4.0::float8, interval '45 seconds'))
                    AS v(id, i, b, d, iv)`

	for _, col := range []struct{ name, dataType string }{
		{"i", "integer"}, {"b", "bigint"}, {"d", "double precision"},
	} {
		for _, agg := range []string{"avg", "sum", "min", "max"} {
			var bare, portable float64
			q := "SELECT " + agg + "(" + col.name + ")::float8, " +
				AggregateExpr(agg, col.name, col.dataType) + "::float8 FROM " + values
			if err := pool.QueryRow(ctx, q).Scan(&bare, &portable); err != nil {
				t.Fatalf("%s(%s): %v", agg, col.name, err)
			}
			if bare != portable {
				t.Errorf("%s(%s): portable %v, bare %v", agg, col.name, portable, bare)
			}
		}
	}

	// An interval sum keeps its length, now carried in the time field.
	var bareSec, portableSec float64
	q := "SELECT EXTRACT(EPOCH FROM sum(iv))::float8, EXTRACT(EPOCH FROM " +
		AggregateExpr("sum", "iv", "interval") + ")::float8 FROM " + values
	if err := pool.QueryRow(ctx, q).Scan(&bareSec, &portableSec); err != nil {
		t.Fatalf("interval sum: %v", err)
	}
	if bareSec != portableSec || bareSec != 26*3600+30*60+45 {
		t.Errorf("interval sum: portable %v s, bare %v s", portableSec, bareSec)
	}
	var avgIv pgtype.Interval
	if err := pool.QueryRow(ctx, "SELECT "+AggregateExpr("avg", "iv", "interval")+
		" FROM "+values).Scan(&avgIv); err != nil || !avgIv.Valid {
		t.Fatalf("interval avg: %v %+v", err, avgIv)
	}

	// The integer list matches exactly the listed rows, and an empty
	// list matches none.
	for _, tt := range []struct {
		ids  []int
		want int
	}{{[]int{1, 3}, 2}, {[]int{2}, 1}, {nil, 0}} {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+values+
			" WHERE id = ANY("+IntArrayExpr("$1")+")", IntListArg(tt.ids)).Scan(&n); err != nil {
			t.Fatalf("IntArrayExpr %v: %v", tt.ids, err)
		}
		if n != tt.want {
			t.Errorf("IntArrayExpr %v matched %d rows, want %d", tt.ids, n, tt.want)
		}
	}

	var epoch float64
	if err := pool.QueryRow(ctx, "SELECT "+EpochSeconds("$1::timestamptz - $2::timestamptz"),
		time.Unix(90, 0), time.Unix(0, 0)).Scan(&epoch); err != nil || epoch != 90 {
		t.Errorf("EpochSeconds = %v, %v; want 90", epoch, err)
	}
}
