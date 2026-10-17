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
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestIsTableNotExistError checks which errors the migration bootstrap
// treats as "schema_version does not exist yet".
func TestIsTableNotExistError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"undefined table", &pgconn.PgError{Code: "42P01"}, true},
		{"wrapped undefined table",
			fmt.Errorf("query: %w", &pgconn.PgError{Code: "42P01"}), true},
		{"other postgres error", &pgconn.PgError{Code: "42703"}, false},
		{"no rows", pgx.ErrNoRows, true},
		{"unrelated error", errors.New("connection reset"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTableNotExistError(tc.err); got != tc.want {
				t.Errorf("isTableNotExistError(%v) = %v, want %v",
					tc.err, got, tc.want)
			}
		})
	}
}
