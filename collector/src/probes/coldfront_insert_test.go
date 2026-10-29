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
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
)

func TestJSONValue(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.FixedZone("x", 3600))
	tests := []struct {
		name    string
		pgType  string
		in      any
		want    string // JSON encoding of the result
		wantErr error
	}{
		{"nil", "text", nil, "null", nil},
		{"int64 exact", "bigint", int64(9007199254740993), "9007199254740993", nil},
		{"uint32", "oid", uint32(4000000000), "4000000000", nil},
		{"float", "double precision", 0.1, "0.1", nil},
		{"float32", "real", float32(1.5), "1.5", nil},
		{"time keeps nanoseconds and offset", "timestamp with time zone", at, `"2026-01-02T03:04:05.123456789+01:00"`, nil},
		{"interval", "interval", pgtype.Interval{Months: 1, Days: 2, Microseconds: 3, Valid: true}, `"1 months 2 days 3 microseconds"`, nil},
		{"null interval", "interval", pgtype.Interval{}, "null", nil},
		{"jsonb document string", "jsonb", `{"a": [1, 2]}`, `{"a":[1,2]}`, nil},
		{"json document bytes", "json", []byte(`[true]`), `[true]`, nil},
		{"text string stays a string", "text", `{"a": 1}`, `"{\"a\": 1}"`, nil},
		{"bytes for text", "text", []byte("x"), `"eA=="`, nil},
		{"array", "text[]", []string{"a", "b"}, `["a","b"]`, nil},
		{"map for jsonb", "jsonb", map[string]any{"k": "v"}, `{"k":"v"}`, nil},
		{"bad jsonb string", "jsonb", "{oops", "", errInvalidJSON},
		{"bad jsonb bytes", "jsonb", []byte("{oops"), "", errInvalidJSON},
		{"NaN", "double precision", math.NaN(), "", errNonFinite},
		{"+Inf", "double precision", math.Inf(1), "", errNonFinite},
		{"float32 -Inf", "real", float32(math.Inf(-1)), "", errNonFinite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jsonValue(tt.pgType, tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("jsonValue error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tt.want {
				t.Errorf("jsonValue(%#v) encodes as %s, want %s", tt.in, b, tt.want)
			}
		})
	}
}

// coldFrontRowsJSON encodes and joins a batch, as the write path does.
func coldFrontRowsJSON(spec coldfront.Table, columns []string, values [][]any) ([]byte, error) {
	rows, err := encodeColdFrontRows(spec, columns, values)
	if err != nil {
		return nil, err
	}
	return joinColdFrontRows(rows), nil
}

func TestColdFrontRowsJSON(t *testing.T) {
	spec, _ := coldfront.Lookup("pg_node_role")
	columns := []string{"connection_id", "role_details", "collected_at"}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	got, err := coldFrontRowsJSON(spec, columns, [][]any{
		{1, `{"role": "primary"}`, at},
		{2, nil, nil},
		{3}, // a short row leaves the rest out
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"collected_at":"2026-01-02T03:04:05Z","connection_id":1,"role_details":{"role":"primary"}},` +
		`{"collected_at":null,"connection_id":2,"role_details":null},` +
		`{"connection_id":3}]`
	if string(got) != want {
		t.Errorf("coldFrontRowsJSON = %s\nwant %s", got, want)
	}

	// NaN becomes NULL rather than failing the batch.
	conn, _ := coldfront.Lookup("pg_connectivity")
	got, err = coldFrontRowsJSON(conn, []string{"connection_id", "response_time_ms"}, [][]any{{1, math.NaN()}, {2, 1.5}})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `[{"connection_id":1,"response_time_ms":null},{"connection_id":2,"response_time_ms":1.5}]` {
		t.Errorf("NaN batch = %s", got)
	}
}

func TestColdFrontRowsJSONErrors(t *testing.T) {
	spec, _ := coldfront.Lookup("pg_node_role")
	tests := []struct {
		name    string
		columns []string
		rows    [][]any
		want    string
	}{
		{"unknown column", []string{"connection_id", "no_such_column"}, [][]any{{1, 2}}, "has no column no_such_column"},
		{"invalid json", []string{"role_details"}, [][]any{{"{oops"}}, "metrics.pg_node_role.role_details: value is not valid JSON"},
		{"unencodable value", []string{"connection_id"}, [][]any{{make(chan int)}}, "failed to encode rows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := coldFrontRowsJSON(spec, tt.columns, tt.rows)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("coldFrontRowsJSON error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

// TestStoreMetricsColdFrontWithoutHelper runs the ColdFront write path
// against a datastore that has no insert helper, so the statement fails
// and StoreMetrics reports it rather than committing anything.
func TestStoreMetricsColdFrontWithoutHelper(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	enableColdFront(t)

	err := StoreMetrics(context.Background(), conn, "pg_connectivity",
		[]string{"connection_id", "response_time_ms"}, [][]any{{1, 1.5}})
	if err == nil || !strings.Contains(err.Error(), "failed to execute INSERT") {
		t.Errorf("StoreMetrics error = %v, want a failed INSERT", err)
	}

	err = StoreMetrics(context.Background(), conn, "pg_connectivity",
		[]string{"connection_id", "no_such_column"}, [][]any{{1, 1.5}})
	if err == nil || !strings.Contains(err.Error(), "has no column no_such_column") {
		t.Errorf("StoreMetrics error = %v, want an unknown column", err)
	}
}
