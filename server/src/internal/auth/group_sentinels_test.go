/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import (
	"errors"
	"testing"
)

func TestCreateGroup_DuplicateNameReturnsSentinel(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	if _, err := store.CreateGroup("dupe", "first"); err != nil {
		t.Fatalf("first CreateGroup failed: %v", err)
	}

	_, err := store.CreateGroup("dupe", "second")
	if err == nil {
		t.Fatal("expected error creating duplicate group, got nil")
	}
	if !errors.Is(err, ErrGroupNameExists) {
		t.Errorf("expected ErrGroupNameExists, got %v", err)
	}
}

func TestUpdateGroup_DuplicateNameReturnsSentinel(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	if _, err := store.CreateGroup("alpha", ""); err != nil {
		t.Fatalf("CreateGroup alpha failed: %v", err)
	}
	betaID, err := store.CreateGroup("beta", "")
	if err != nil {
		t.Fatalf("CreateGroup beta failed: %v", err)
	}

	// Renaming beta to the existing name "alpha" must collide.
	err = store.UpdateGroup(betaID, "alpha", "")
	if err == nil {
		t.Fatal("expected error renaming group to existing name, got nil")
	}
	if !errors.Is(err, ErrGroupNameExists) {
		t.Errorf("expected ErrGroupNameExists, got %v", err)
	}
}

func TestUpdateGroup_RenameToUniqueSucceeds(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	id, err := store.CreateGroup("gamma", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if err := store.UpdateGroup(id, "gamma-renamed", ""); err != nil {
		t.Fatalf("UpdateGroup to unique name failed: %v", err)
	}
}

func TestUpdateGroup_DescriptionOnlyKeepsName(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	id, err := store.CreateGroup("stable-name", "old desc")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	// An empty name must leave the existing name untouched while still
	// updating the description.
	if err := store.UpdateGroup(id, "", "new desc"); err != nil {
		t.Fatalf("UpdateGroup (description only) failed: %v", err)
	}

	g, err := store.GetGroup(id)
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}
	if g == nil || g.Name != "stable-name" {
		t.Errorf("expected name to remain 'stable-name', got %+v", g)
	}
	if g.Description != "new desc" {
		t.Errorf("expected description 'new desc', got %q", g.Description)
	}
}

func TestUpdateGroup_NotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Renaming a group that does not exist must surface ErrGroupNotFound via
	// the RowsAffected == 0 branch (so the handler can return a 404), and
	// must not be mistaken for the duplicate-name conflict ErrGroupNameExists.
	err := store.UpdateGroup(99999, "nope", "")
	if err == nil {
		t.Fatal("expected error updating non-existent group, got nil")
	}
	if !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("expected ErrGroupNotFound for missing group, got %v", err)
	}
	if errors.Is(err, ErrGroupNameExists) {
		t.Errorf("did not expect ErrGroupNameExists for missing group, got %v", err)
	}
}

func TestCreateGroup_GenericErrorOnClosedStore(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	// Closing the underlying DB makes Exec fail with a non-UNIQUE error,
	// exercising the generic error path that maps to a 500 in the handler.
	store.Close()

	_, err := store.CreateGroup("anything", "")
	if err == nil {
		t.Fatal("expected error creating group on closed store, got nil")
	}
	if errors.Is(err, ErrGroupNameExists) {
		t.Errorf("did not expect ErrGroupNameExists for closed store, got %v", err)
	}
}

func TestUpdateGroup_GenericErrorOnClosedStore(t *testing.T) {
	store, cleanup := createTestAuthStoreForGroups(t)
	defer cleanup()

	id, err := store.CreateGroup("present", "")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	store.Close()

	err = store.UpdateGroup(id, "renamed", "")
	if err == nil {
		t.Fatal("expected error updating group on closed store, got nil")
	}
	if errors.Is(err, ErrGroupNameExists) {
		t.Errorf("did not expect ErrGroupNameExists for closed store, got %v", err)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		column string
		want   bool
	}{
		{name: "nil error", err: nil, column: "user_groups.name", want: false},
		{
			name:   "matching unique violation",
			err:    errors.New("UNIQUE constraint failed: user_groups.name"),
			column: "user_groups.name",
			want:   true,
		},
		{
			name:   "different column",
			err:    errors.New("UNIQUE constraint failed: tokens.token_hash"),
			column: "user_groups.name",
			want:   false,
		},
		{
			name:   "unrelated error",
			err:    errors.New("some other failure"),
			column: "user_groups.name",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUniqueViolation(tt.err, tt.column); got != tt.want {
				t.Errorf("isUniqueViolation() = %v, want %v", got, tt.want)
			}
		})
	}
}
