/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package utils

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// mockRows implements pgx.Rows for testing ScanRowsToMaps. It supports
// overriding the Values() and Err() outcomes so all branches of the
// helper can be exercised.
type mockRows struct {
	columns     []string
	data        [][]any
	index       int
	valuesErr   error
	valuesErrOn int
	iterErr     error
}

func (m *mockRows) Close() {}

func (m *mockRows) Err() error { return m.iterErr }

func (m *mockRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }

func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription {
	fds := make([]pgconn.FieldDescription, len(m.columns))
	for i, c := range m.columns {
		fds[i] = pgconn.FieldDescription{Name: c}
	}
	return fds
}

func (m *mockRows) Next() bool {
	if m.index >= len(m.data) {
		return false
	}
	m.index++
	return true
}

func (m *mockRows) Scan(dest ...any) error { return nil }

func (m *mockRows) Values() ([]any, error) {
	if m.valuesErr != nil && m.index == m.valuesErrOn {
		return nil, m.valuesErr
	}
	return m.data[m.index-1], nil
}

func (m *mockRows) RawValues() [][]byte { return nil }

func (m *mockRows) Conn() *pgx.Conn { return nil }

func TestScanRowsToMaps_MultipleRows(t *testing.T) {
	rows := &mockRows{
		columns: []string{"id", "name", "active"},
		data: [][]any{
			{int64(1), "alice", true},
			{int64(2), "bob", false},
		},
	}

	got, err := ScanRowsToMaps(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}

	if got[0]["id"] != int64(1) {
		t.Errorf("row 0 id: got %v, want 1", got[0]["id"])
	}
	if got[0]["name"] != "alice" {
		t.Errorf("row 0 name: got %v, want alice", got[0]["name"])
	}
	if got[0]["active"] != true {
		t.Errorf("row 0 active: got %v, want true", got[0]["active"])
	}
	if got[1]["id"] != int64(2) {
		t.Errorf("row 1 id: got %v, want 2", got[1]["id"])
	}
	if got[1]["name"] != "bob" {
		t.Errorf("row 1 name: got %v, want bob", got[1]["name"])
	}
	if got[1]["active"] != false {
		t.Errorf("row 1 active: got %v, want false", got[1]["active"])
	}
}

func TestScanRowsToMaps_EmptyResultSet(t *testing.T) {
	rows := &mockRows{
		columns: []string{"id"},
		data:    nil,
	}

	got, err := ScanRowsToMaps(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty/nil result, got %v", got)
	}
}

func TestScanRowsToMaps_ValuesError(t *testing.T) {
	rows := &mockRows{
		columns: []string{"id"},
		data: [][]any{
			{int64(1)},
			{int64(2)},
		},
		valuesErr:   errors.New("scan boom"),
		valuesErrOn: 1,
	}

	_, err := ScanRowsToMaps(rows)
	if err == nil {
		t.Fatal("expected error from Values(), got nil")
	}
	if got := err.Error(); got != "failed to scan row: scan boom" {
		t.Errorf("unexpected error text: %q", got)
	}
}

func TestScanRowsToMaps_IterationError(t *testing.T) {
	rows := &mockRows{
		columns: []string{"id"},
		data: [][]any{
			{int64(1)},
		},
		iterErr: errors.New("iter boom"),
	}

	_, err := ScanRowsToMaps(rows)
	if err == nil {
		t.Fatal("expected error from Err(), got nil")
	}
	if got := err.Error(); got != "error iterating rows: iter boom" {
		t.Errorf("unexpected error text: %q", got)
	}
}

func TestScanRowsToMaps_NoColumns(t *testing.T) {
	rows := &mockRows{
		columns: nil,
		data: [][]any{
			{},
		},
	}

	got, err := ScanRowsToMaps(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one row, got %d", len(got))
	}
	if len(got[0]) != 0 {
		t.Errorf("expected empty row map, got %v", got[0])
	}
}
