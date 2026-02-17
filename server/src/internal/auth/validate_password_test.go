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

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name      string
		password  string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "valid password 8+ chars",
			password: "securepassword",
			wantErr:  false,
		},
		{
			name:      "empty string",
			password:  "",
			wantErr:   true,
			errSubstr: "at least 8 characters",
		},
		{
			name:      "7 character password",
			password:  "abcdefg",
			wantErr:   true,
			errSubstr: "at least 8 characters",
		},
		{
			name:     "exactly 8 characters",
			password: "abcdefgh",
			wantErr:  false,
		},
		{
			name:     "very long password",
			password: strings.Repeat("a", 1000),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for password %q, got nil", tt.password)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("expected error to contain %q, got %q", tt.errSubstr, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error for password %q, got %v", tt.password, err)
				}
			}
		})
	}
}
