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
	"strings"
	"testing"
)

// TestValidateDatabaseName covers the narrow checks applied to a
// caller-supplied database override: it turns away what cannot name a
// real database without rejecting the unusual names PostgreSQL allows.
func TestValidateDatabaseName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"plain name", "mydb", false},
		{"mixed case and digits", "MyDB_2", false},
		{"punctuation is allowed", "my-db.prod", false},
		{"spaces inside are allowed", "my db", false},
		{"unicode is allowed", "données", false},
		{"injection payload is still a valid name",
			"mydb?host=evil.example.com", false},
		{"63 bytes", strings.Repeat("a", 63), false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"tab only", "\t", true},
		{"64 bytes", strings.Repeat("a", 64), true},
		{"64 bytes of multibyte runes", strings.Repeat("é", 32), true},
		{"NUL byte", "my\x00db", true},
		{"newline", "my\ndb", true},
		{"carriage return", "my\rdb", true},
		{"escape", "my\x1bdb", true},
		{"delete", "my\x7fdb", true},
		{"C1 control", "my\u0085db", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDatabaseName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDatabaseName(%q) = %v, wantErr %v",
					tt.input, err, tt.wantErr)
			}
		})
	}
}
