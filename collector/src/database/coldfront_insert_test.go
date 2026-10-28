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
	"math"
	"math/big"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
	"github.com/pgedge/ai-workbench/collector/src/probes"
)

// awkwardText exercises the quoting the insert helper relies on: a
// quote, a statement terminator and a comment, backslashes and
// characters outside ASCII.
const awkwardText = `it's'; DROP TABLE connections; -- \ back\\slash ünïcødé 🚀`

// typedSample is a value of one PostgreSQL column type, as a probe would
// hand it to StoreMetrics, with the text PostgreSQL shows for it once
// stored.
type typedSample struct {
	value any
	text  string
}

// samples gives a value for every column type in the specification.
var samples = map[string]typedSample{
	"integer":          {int32(42), "42"},
	"bigint":           {int64(9007199254740993), "9007199254740993"}, // not a float64
	"real":             {float32(1.5), "1.5"},
	"double precision": {0.1, "0.1"},
	"boolean":          {true, "true"},
	"timestamp with time zone": {
		time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.FixedZone("x", -5*3600)),
		"2026-01-02 08:04:05.123456+00",
	},
	"text":                   {awkwardText, awkwardText},
	"character varying(255)": {awkwardText, awkwardText},
	"text[]":                 {[]string{"a'b", "c,d", `e\f`, "ü"}, `{a'b,"c,d","e\\f",ü}`},
	"jsonb":                  {`{"k": "v'; DROP", "n": 1}`, `{"k": "v'; DROP", "n": 1}`},
	"interval": {
		pgtype.Interval{Months: 1, Days: 2, Microseconds: 3_500_000, Valid: true},
		"1 mon 2 days 00:00:03.5",
	},
	"numeric":  {pgtype.Numeric{Int: bigInt("123456789012345678901"), Valid: true}, "123456789012345678901"},
	"oid":      {uint32(4000000001), "4000000001"},
	"xid":      {uint32(4000000000), "4000000000"},
	"inet":     {netip.MustParsePrefix("10.1.2.3/32"), "10.1.2.3"},
	`"char"`:   {'c', "c"},
	"name":     {"node one", "node one"},
	"smallint": {int16(7), "7"},
}

// bigInt parses a decimal integer for a pgtype.Numeric sample.
func bigInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("bad integer " + s)
	}
	return n
}

// exerciseInsertHelper drives public.workbench_coldfront_insert, through
// StoreMetrics and directly, against the stub views: every column type
// of every metrics table, a row of NULLs, awkward text, a NaN, and the
// relations the helper must refuse.
func exerciseInsertHelper(t *testing.T, conn *pgxpool.Conn) {
	t.Helper()
	ctx := context.Background()

	probes.SetColdFrontMetrics(true)
	t.Cleanup(func() { probes.SetColdFrontMetrics(false) })
	if _, err := conn.Exec(ctx, "SET TimeZone = 'UTC'"); err != nil {
		t.Fatal(err)
	}

	for _, spec := range coldfront.Tables() {
		columns := make([]string, len(spec.Columns))
		full := make([]any, len(spec.Columns))
		nulls := make([]any, len(spec.Columns))
		for i, c := range spec.Columns {
			s, ok := samples[c.PGType]
			if !ok {
				t.Fatalf("no sample for type %s of metrics.%s.%s", c.PGType, spec.Name, c.Name)
			}
			columns[i] = c.Name
			full[i] = s.value
		}
		if err := probes.StoreMetrics(ctx, conn, spec.Name, columns, [][]any{full, nulls}); err != nil {
			t.Fatalf("StoreMetrics into metrics.%s: %v", spec.Name, err)
		}

		for _, c := range spec.Columns {
			col := pgx.Identifier{c.Name}.Sanitize()
			rel := pgx.Identifier{"metrics", spec.Name}.Sanitize()
			var got *string
			var nullRows int
			if err := conn.QueryRow(ctx,
				"SELECT (SELECT "+col+"::text FROM "+rel+" WHERE connection_id IS NOT NULL LIMIT 1), "+
					"(SELECT count(*) FROM "+rel+" WHERE connection_id IS NULL AND "+col+" IS NULL)").
				Scan(&got, &nullRows); err != nil {
				t.Fatalf("metrics.%s.%s: %v", spec.Name, c.Name, err)
			}
			if want := samples[c.PGType].text; got == nil || *got != want {
				t.Errorf("metrics.%s.%s (%s) stored %v, want %q", spec.Name, c.Name, c.PGType, got, want)
			}
			if nullRows != 1 {
				t.Errorf("metrics.%s.%s: %d NULL rows, want 1", spec.Name, c.Name, nullRows)
			}
		}
	}

	// A NaN cannot travel as JSON and is stored as NULL.
	if err := probes.StoreMetrics(ctx, conn, "pg_connectivity",
		[]string{"connection_id", "collected_at", "response_time_ms"},
		[][]any{{int32(77), time.Now(), math.NaN()}}); err != nil {
		t.Fatalf("StoreMetrics with a NaN: %v", err)
	}
	var nanStored *float64
	if err := conn.QueryRow(ctx,
		"SELECT response_time_ms FROM metrics.pg_connectivity WHERE connection_id = 77").Scan(&nanStored); err != nil {
		t.Fatal(err)
	}
	if nanStored != nil {
		t.Errorf("NaN stored as %v, want NULL", *nanStored)
	}

	// The helper refuses anything but a registered metrics view, before
	// it looks at the relation, so a hostile name is never interpolated.
	for _, table := range []string{"connections", "pg_stat_activity; DROP TABLE connections", "no_such_table"} {
		_, err := conn.Exec(ctx, "SELECT public.workbench_coldfront_insert($1, $2::jsonb)",
			table, `[{"id": 1}]`)
		if err == nil || !strings.Contains(err.Error(), "is not a ColdFront metrics table") {
			t.Errorf("helper accepted %q: %v", table, err)
		}
	}
	_, err := conn.Exec(ctx, "SELECT public.workbench_coldfront_insert('pg_connectivity', '{}'::jsonb)")
	if err == nil || !strings.Contains(err.Error(), "must be a JSON array") {
		t.Errorf("helper accepted a non-array: %v", err)
	}
	if _, err := conn.Exec(ctx, "SELECT public.workbench_coldfront_insert('pg_connectivity', '[]'::jsonb)"); err != nil {
		t.Errorf("helper refused an empty batch: %v", err)
	}

	var connections int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_class WHERE oid = 'public.connections'::regclass").Scan(&connections); err != nil || connections != 1 {
		t.Errorf("connections table missing after the quoting tests: %v", err)
	}
}
