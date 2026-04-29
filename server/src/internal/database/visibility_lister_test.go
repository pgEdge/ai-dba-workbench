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
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// ---------------------------------------------------------------------------
// Mock connectionLister for testing
// ---------------------------------------------------------------------------

type mockConnectionLister struct {
	connections []ConnectionListItem
	err         error
}

func (m *mockConnectionLister) GetAllConnections(_ context.Context) ([]ConnectionListItem, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.connections, nil
}

// ---------------------------------------------------------------------------
// NewVisibilityLister tests
// ---------------------------------------------------------------------------

func TestNewVisibilityListerNilDatastore(t *testing.T) {
	// A nil datastore should return a nil lister
	lister := NewVisibilityLister(nil)
	if lister != nil {
		t.Errorf("expected nil lister for nil datastore, got %v", lister)
	}
}

func TestNewVisibilityListerNonNilDatastore(t *testing.T) {
	// A non-nil datastore should return a non-nil lister. We cannot call
	// GetAllConnections without a real database, but we can verify that
	// the factory returns a concrete adapter.
	ds := &Datastore{} // minimal stub; no pool needed for this test
	lister := NewVisibilityLister(ds)
	if lister == nil {
		t.Error("expected non-nil lister for non-nil datastore")
	}
}

// ---------------------------------------------------------------------------
// GetAllConnections tests
// ---------------------------------------------------------------------------

func TestGetAllConnectionsSuccess(t *testing.T) {
	// Test successful retrieval and mapping of connections
	mockData := []ConnectionListItem{
		{
			ID:            1,
			Name:          "conn1",
			Description:   "First connection",
			Host:          "localhost",
			Port:          5432,
			DatabaseName:  "db1",
			IsMonitored:   true,
			IsShared:      true,
			OwnerUsername: "alice",
		},
		{
			ID:            2,
			Name:          "conn2",
			Description:   "Second connection",
			Host:          "localhost",
			Port:          5433,
			DatabaseName:  "db2",
			IsMonitored:   false,
			IsShared:      false,
			OwnerUsername: "bob",
		},
		{
			ID:            3,
			Name:          "conn3",
			Description:   "Third connection",
			Host:          "remotehost",
			Port:          5432,
			DatabaseName:  "db3",
			IsMonitored:   true,
			IsShared:      true,
			OwnerUsername: "charlie",
		},
	}

	mock := &mockConnectionLister{connections: mockData}
	lister := newVisibilityListerWithSource(mock)

	result, err := lister.GetAllConnections(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != len(mockData) {
		t.Fatalf("expected %d connections, got %d", len(mockData), len(result))
	}

	// Verify each connection is mapped correctly
	expected := []auth.ConnectionVisibilityInfo{
		{ID: 1, IsShared: true, OwnerUsername: "alice"},
		{ID: 2, IsShared: false, OwnerUsername: "bob"},
		{ID: 3, IsShared: true, OwnerUsername: "charlie"},
	}

	for i, exp := range expected {
		if result[i].ID != exp.ID {
			t.Errorf("connection %d: expected ID %d, got %d", i, exp.ID, result[i].ID)
		}
		if result[i].IsShared != exp.IsShared {
			t.Errorf("connection %d: expected IsShared %v, got %v", i, exp.IsShared, result[i].IsShared)
		}
		if result[i].OwnerUsername != exp.OwnerUsername {
			t.Errorf("connection %d: expected OwnerUsername %q, got %q", i, exp.OwnerUsername, result[i].OwnerUsername)
		}
	}
}

func TestGetAllConnectionsEmptyResult(t *testing.T) {
	// Test behavior when no connections exist
	mock := &mockConnectionLister{connections: []ConnectionListItem{}}
	lister := newVisibilityListerWithSource(mock)

	result, err := lister.GetAllConnections(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Error("expected non-nil slice for empty result")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 connections, got %d", len(result))
	}
}

func TestGetAllConnectionsError(t *testing.T) {
	// Test error propagation from the underlying data source
	expectedErr := errors.New("database connection failed")
	mock := &mockConnectionLister{err: expectedErr}
	lister := newVisibilityListerWithSource(mock)

	result, err := lister.GetAllConnections(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if result != nil {
		t.Errorf("expected nil result on error, got %v", result)
	}
}

func TestGetAllConnectionsSingleConnection(t *testing.T) {
	// Test with exactly one connection
	mockData := []ConnectionListItem{
		{
			ID:            42,
			Name:          "single",
			Description:   "Only connection",
			Host:          "solo.example.com",
			Port:          5432,
			DatabaseName:  "solo_db",
			IsMonitored:   true,
			IsShared:      false,
			OwnerUsername: "admin",
		},
	}

	mock := &mockConnectionLister{connections: mockData}
	lister := newVisibilityListerWithSource(mock)

	result, err := lister.GetAllConnections(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(result))
	}

	if result[0].ID != 42 {
		t.Errorf("expected ID 42, got %d", result[0].ID)
	}
	if result[0].IsShared != false {
		t.Errorf("expected IsShared false, got %v", result[0].IsShared)
	}
	if result[0].OwnerUsername != "admin" {
		t.Errorf("expected OwnerUsername 'admin', got %q", result[0].OwnerUsername)
	}
}

func TestGetAllConnectionsPreservesOrder(t *testing.T) {
	// Verify that the order of connections is preserved
	mockData := []ConnectionListItem{
		{ID: 10, IsShared: true, OwnerUsername: "user1"},
		{ID: 5, IsShared: false, OwnerUsername: "user2"},
		{ID: 20, IsShared: true, OwnerUsername: "user3"},
		{ID: 1, IsShared: false, OwnerUsername: "user4"},
	}

	mock := &mockConnectionLister{connections: mockData}
	lister := newVisibilityListerWithSource(mock)

	result, err := lister.GetAllConnections(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedIDs := []int{10, 5, 20, 1}
	for i, expectedID := range expectedIDs {
		if result[i].ID != expectedID {
			t.Errorf("position %d: expected ID %d, got %d", i, expectedID, result[i].ID)
		}
	}
}

// ---------------------------------------------------------------------------
// ConnectionsToVisibilityInfo tests
// ---------------------------------------------------------------------------

func TestConnectionsToVisibilityInfo(t *testing.T) {
	cases := []struct {
		name string
		in   []ConnectionListItem
		want []auth.ConnectionVisibilityInfo
	}{
		{
			name: "preserves all sharing fields",
			in: []ConnectionListItem{
				{ID: 1, Name: "owned", IsShared: false, OwnerUsername: "alice"},
				{ID: 2, Name: "shared", IsShared: true, OwnerUsername: "bob"},
			},
			want: []auth.ConnectionVisibilityInfo{
				{ID: 1, IsShared: false, OwnerUsername: "alice"},
				{ID: 2, IsShared: true, OwnerUsername: "bob"},
			},
		},
		{
			name: "empty slice",
			in:   []ConnectionListItem{},
			want: []auth.ConnectionVisibilityInfo{},
		},
		{
			name: "nil slice yields zero-length non-nil slice",
			in:   nil,
			want: []auth.ConnectionVisibilityInfo{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConnectionsToVisibilityInfo(tc.in)
			if got == nil {
				t.Fatal("expected non-nil slice")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d entries, got %d", len(tc.want), len(got))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("index %d: got %+v want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NewSliceVisibilityLister tests
// ---------------------------------------------------------------------------

func TestNewSliceVisibilityLister_ProjectsAllFields(t *testing.T) {
	input := []ConnectionListItem{
		{ID: 1, Name: "owned", IsShared: false, OwnerUsername: "alice"},
		{ID: 2, Name: "shared", IsShared: true, OwnerUsername: "bob"},
		{ID: 3, Name: "other", IsShared: false, OwnerUsername: "carol"},
	}

	lister := NewSliceVisibilityLister(input)
	if lister == nil {
		t.Fatal("expected non-nil lister")
	}
	got, err := lister.GetAllConnections(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(input) {
		t.Fatalf("expected %d entries, got %d", len(input), len(got))
	}
	for i := range input {
		if got[i].ID != input[i].ID || got[i].IsShared != input[i].IsShared || got[i].OwnerUsername != input[i].OwnerUsername {
			t.Errorf("index %d: projection mismatch: got %+v from %+v", i, got[i], input[i])
		}
	}
}

func TestNewSliceVisibilityLister_EmptyAndNil(t *testing.T) {
	cases := []struct {
		name string
		in   []ConnectionListItem
	}{
		{name: "empty", in: []ConnectionListItem{}},
		{name: "nil", in: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lister := NewSliceVisibilityLister(tc.in)
			got, err := lister.GetAllConnections(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("expected non-nil slice, got nil")
			}
			if len(got) != 0 {
				t.Fatalf("expected zero-length slice, got %d", len(got))
			}
		})
	}
}
