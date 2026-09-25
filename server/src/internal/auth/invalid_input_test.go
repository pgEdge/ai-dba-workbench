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
	"fmt"
	"testing"
)

// TestInvalidInput covers the marker itself: a nil error stays nil, the
// message is the reason alone, errors.Is finds both the sentinel and the
// wrapped reason, and errors.As reaches the unwrapped message through any
// context a caller adds on the way out.
func TestInvalidInput(t *testing.T) {
	if err := invalidInput(nil); err != nil {
		t.Fatalf("invalidInput(nil) = %v, want nil", err)
	}

	reason := errors.New("username must not be empty")
	err := invalidInput(reason)
	if err.Error() != reason.Error() {
		t.Errorf("Error() = %q, want %q", err.Error(), reason.Error())
	}

	wrapped := fmt.Errorf("creating user: %w", err)
	if !errors.Is(wrapped, ErrInvalidInput) {
		t.Error("errors.Is did not find ErrInvalidInput")
	}
	if !errors.Is(wrapped, reason) {
		t.Error("errors.Is did not find the wrapped reason")
	}

	var invalid *InvalidInputError
	if !errors.As(wrapped, &invalid) {
		t.Fatal("errors.As did not find *InvalidInputError")
	}
	if invalid.Error() != reason.Error() {
		t.Errorf("unwrapped message = %q, want %q", invalid.Error(), reason.Error())
	}

	if errors.Is(reason, ErrInvalidInput) {
		t.Error("an unmarked error matched ErrInvalidInput")
	}
}

// TestStoreValidationErrorsAreInvalidInput pins the store paths that refuse
// a request on its merits, so that the RBAC handlers can answer 400 for
// them rather than 500.
func TestStoreValidationErrorsAreInvalidInput(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("existing", "Sup3r-Str0ng-Pass!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	short := "short"

	tests := []struct {
		name string
		call func() error
	}{
		{"CreateUser bad username", func() error {
			return store.CreateUser("bad name!", "Sup3r-Str0ng-Pass!", "", "", "")
		}},
		{"CreateUser weak password", func() error {
			return store.CreateUser("newuser", short, "", "", "")
		}},
		{"CreateServiceAccount bad username", func() error {
			return store.CreateServiceAccount("bad name!", "", "", "")
		}},
		{"UpdateUser weak password", func() error {
			return store.UpdateUser("existing", short, "", "", "")
		}},
		{"UpdateUserAtomic weak password", func() error {
			return store.UpdateUserAtomic("existing", UserUpdate{Password: &short})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if err == nil {
				t.Fatal("expected the request to be refused")
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput, got: %v", err)
			}
		})
	}
}
