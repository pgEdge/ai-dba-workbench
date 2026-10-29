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
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
)

// testRows returns n encoded rows whose JSON is size bytes long and
// whose connection is the row's index.
func testRows(n, size int) []coldFrontRow {
	rows := make([]coldFrontRow, n)
	for i := range rows {
		rows[i] = coldFrontRow{json: []byte(strings.Repeat("x", size)), connection: i}
	}
	return rows
}

// batchSizes returns the length of each batch.
func batchSizes(batches [][]coldFrontRow) []int {
	sizes := make([]int, len(batches))
	for i, b := range batches {
		sizes[i] = len(b)
	}
	return sizes
}

func TestSplitColdFrontBatches(t *testing.T) {
	tests := []struct {
		name     string
		rows     []coldFrontRow
		maxRows  int
		maxBytes int
		want     []int
	}{
		{"empty", nil, 10, 100, []int{}},
		{"fits", testRows(3, 9), 10, 100, []int{3}},
		{"row cap", testRows(7, 1), 3, 1000, []int{3, 3, 1}},
		// Each row costs 10 bytes with its separator.
		{"byte cap", testRows(5, 9), 100, 25, []int{2, 2, 1}},
		{"exact byte cap", testRows(4, 9), 100, 20, []int{2, 2}},
		{"oversized row alone", append(testRows(1, 9), append(testRows(1, 500), testRows(1, 9)...)...), 100, 50, []int{1, 1, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := batchSizes(splitColdFrontBatches(tt.rows, tt.maxRows, tt.maxBytes))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("batch sizes = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJoinColdFrontRows(t *testing.T) {
	rows := []coldFrontRow{{json: []byte(`{"a":1}`)}, {json: []byte(`{"a":2}`)}}
	if got := string(joinColdFrontRows(rows)); got != `[{"a":1},{"a":2}]` {
		t.Errorf("joinColdFrontRows = %s", got)
	}
	if got := string(joinColdFrontRows(nil)); got != `[]` {
		t.Errorf("joinColdFrontRows(nil) = %s", got)
	}
}

func TestRowSpecificColdFrontError(t *testing.T) {
	tests := []struct {
		err      error
		code     string
		specific bool
	}{
		{&pgconn.PgError{Code: "22P02"}, "22P02", true},
		{&pgconn.PgError{Code: "23505"}, "23505", true},
		{&pgconn.PgError{Code: "53200"}, "53200", false},
		{&pgconn.PgError{Code: "54000"}, "54000", true},
		{&pgconn.PgError{Code: "XX000", Message: "(PGDuckDB/pgduckdb_raw_query_cpp) Conversion Error: Type VARCHAR with value 'x' can't be cast"}, "XX000", true},
		// The live failure that a subtransaction caused: systemic, not a row.
		{&pgconn.PgError{Code: "XX000", Message: "(PGDuckDB/pgduckdb_raw_query_cpp) Not implemented Error: SAVEPOINT and subtransactions are not supported in DuckDB"}, "XX000", false},
		{&pgconn.PgError{Code: "XX000", Message: "internal error"}, "XX000", false},
		{&pgconn.PgError{Code: "XX001", Message: "Conversion Error"}, "XX001", false},
		{&pgconn.PgError{Code: "22000", Message: "feature not supported here"}, "22000", false},
		{fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "22003"}), "22003", true},
		{&pgconn.PgError{Code: "42883"}, "42883", false},
		{&pgconn.PgError{Code: "08006"}, "08006", false},
		{&pgconn.PgError{Code: ""}, "", false},
		{errors.New("conn closed"), "", false},
	}
	for _, tt := range tests {
		code, specific := rowSpecificColdFrontError(tt.err)
		if code != tt.code || specific != tt.specific {
			t.Errorf("rowSpecificColdFrontError(%v) = %q, %v; want %q, %v", tt.err, code, specific, tt.code, tt.specific)
		}
	}
}

// failingWriter records the batches it is given and fails any batch
// containing a row whose connection is in bad, with err.
type failingWriter struct {
	bad     map[int]bool
	err     error
	batches [][]int
}

func (w *failingWriter) write(_ context.Context, rows []coldFrontRow) error {
	ids := make([]int, len(rows))
	failed := false
	for i, r := range rows {
		id, ok := r.connection.(int)
		if !ok {
			return errors.New("connection is not an int")
		}
		ids[i] = id
		failed = failed || w.bad[id]
	}
	w.batches = append(w.batches, ids)
	if failed {
		return w.err
	}
	return nil
}

func TestWriteColdFrontBatchesDropsABadRow(t *testing.T) {
	w := &failingWriter{bad: map[int]bool{5: true}, err: &pgconn.PgError{Code: "22P02"}}
	if err := writeColdFrontBatches(context.Background(), "pg_settings", testRows(8, 3), w.write); err != nil {
		t.Fatalf("writeColdFrontBatches = %v", err)
	}
	var stored []int
	for _, b := range w.batches {
		if !containsInt(b, 5) {
			stored = append(stored, b...)
		}
	}
	if want := []int{0, 1, 2, 3, 4, 6, 7}; !reflect.DeepEqual(stored, want) {
		t.Errorf("stored rows %v, want %v (batches %v)", stored, want, w.batches)
	}
}

func TestWriteColdFrontBatchesReturnsSystemicErrors(t *testing.T) {
	// An error no single row causes is returned at once, without
	// splitting the batch.
	w := &failingWriter{bad: map[int]bool{0: true, 1: true, 2: true}, err: &pgconn.PgError{Code: "42883"}}
	err := writeColdFrontBatches(context.Background(), "pg_settings", testRows(3, 3), w.write)
	if err == nil || !strings.Contains(err.Error(), "failed to execute INSERT") || len(w.batches) != 1 {
		t.Errorf("writeColdFrontBatches = %v after %d statements", err, len(w.batches))
	}

	// A row-specific error that every row hits stops after
	// maxColdFrontDroppedRows drops.
	bad := map[int]bool{}
	for i := 0; i < 64; i++ {
		bad[i] = true
	}
	w = &failingWriter{bad: bad, err: &pgconn.PgError{Code: "XX000", Message: "Conversion Error: bad value"}}
	err = writeColdFrontBatches(context.Background(), "pg_settings", testRows(64, 3), w.write)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("more than %d rows", maxColdFrontDroppedRows)) {
		t.Errorf("writeColdFrontBatches = %v", err)
	}
	if len(w.batches) > 2*64 {
		t.Errorf("issued %d statements for a systemic failure", len(w.batches))
	}

	// The failure of a later half is returned too.
	w = &failingWriter{bad: map[int]bool{3: true}, err: &pgconn.PgError{Code: "22P02"}}
	calls := 0
	write := func(ctx context.Context, rows []coldFrontRow) error {
		calls++
		if calls == 3 {
			return errors.New("connection lost")
		}
		return w.write(ctx, rows)
	}
	if err := writeColdFrontBatches(context.Background(), "pg_settings", testRows(4, 3), write); err == nil {
		t.Error("a failure while retrying a half was not returned")
	}
}

// TestWriteColdFrontBatchesSubtransactionErrorIsSystemic replays the
// live failure: pg_duckdb refused every write with XX000 "SAVEPOINT and
// subtransactions are not supported", and the collector dropped rows one
// by one. It must return the error after the first attempt instead.
func TestWriteColdFrontBatchesSubtransactionErrorIsSystemic(t *testing.T) {
	bad := map[int]bool{}
	for i := 0; i < 8; i++ {
		bad[i] = true
	}
	w := &failingWriter{bad: bad, err: &pgconn.PgError{
		Code:    "XX000",
		Message: "(PGDuckDB/pgduckdb_raw_query_cpp) Not implemented Error: SAVEPOINT and subtransactions are not supported in DuckDB",
	}}
	err := writeColdFrontBatches(context.Background(), "pg_settings", testRows(8, 3), w.write)
	if err == nil || !strings.Contains(err.Error(), "subtransactions are not supported") {
		t.Errorf("writeColdFrontBatches = %v", err)
	}
	if len(w.batches) != 1 {
		t.Errorf("issued %d statements, want the single failing batch", len(w.batches))
	}
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// mustLookup returns the specification of a metrics table.
func mustLookup(t *testing.T, name string) coldfront.Table {
	t.Helper()
	spec, ok := coldfront.Lookup(name)
	if !ok {
		t.Fatalf("no specification for %s", name)
	}
	return spec
}

func TestEncodeColdFrontRowsRecordsConnection(t *testing.T) {
	spec := mustLookup(t, "pg_connectivity")
	rows, err := encodeColdFrontRows(spec, []string{"response_time_ms", "connection_id"}, [][]any{{1.5, 7}, {2.5}})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].connection != 7 || rows[1].connection != nil {
		t.Errorf("connections = %v, %v", rows[0].connection, rows[1].connection)
	}
}
