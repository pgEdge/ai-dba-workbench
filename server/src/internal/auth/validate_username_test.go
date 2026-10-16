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
	"strings"
	"testing"
)

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantErr  string
	}{
		{name: "simple", username: "alice"},
		{name: "every allowed punctuation", username: "a_b.c@d-e"},
		{name: "leading digit", username: "1alice"},
		{name: "non-ASCII letters", username: "zo\u00eb"},
		// The limit counts runes, not bytes: 128 two-byte runes pass.
		{name: "128 runes", username: strings.Repeat("\u00e9", 128)},
		{name: "empty", username: "", wantErr: "must not be empty"},
		{
			name:     "129 runes",
			username: strings.Repeat("a", 129),
			wantErr:  "at most 128 characters",
		},
		{
			name:     "leading underscore",
			username: "_alice",
			wantErr:  "must start with a letter or digit",
		},
		{
			name:     "leading dot",
			username: ".alice",
			wantErr:  "must start with a letter or digit",
		},
		{
			name:     "space",
			username: "alice smith",
			wantErr:  "invalid character:  ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUsername(tt.username)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateUsername(%q) = %v, want nil", tt.username, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateUsername(%q) = %v, want an error containing %q",
					tt.username, err, tt.wantErr)
			}
		})
	}
}
