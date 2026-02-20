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
			name:     "valid password with mixed case and digit",
			password: "Secure1pass",
			wantErr:  false,
		},
		{
			name:     "valid password exactly 8 characters",
			password: "Abcdef1x",
			wantErr:  false,
		},
		{
			name:     "valid password at max length",
			password: strings.Repeat("a", 69) + "A1x",
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
			password:  "Abcde1x",
			wantErr:   true,
			errSubstr: "at least 8 characters",
		},
		{
			name:      "exceeds max length",
			password:  strings.Repeat("a", 70) + "A1x",
			wantErr:   true,
			errSubstr: "at most 72 characters",
		},
		{
			name:      "no uppercase letter",
			password:  "abcdefg1",
			wantErr:   true,
			errSubstr: "uppercase letter",
		},
		{
			name:      "no lowercase letter",
			password:  "ABCDEFG1",
			wantErr:   true,
			errSubstr: "lowercase letter",
		},
		{
			name:      "no digit",
			password:  "Abcdefgh",
			wantErr:   true,
			errSubstr: "digit",
		},
		{
			name:      "only lowercase",
			password:  "abcdefgh",
			wantErr:   true,
			errSubstr: "complexity requirements",
		},
		{
			name:      "only uppercase",
			password:  "ABCDEFGH",
			wantErr:   true,
			errSubstr: "complexity requirements",
		},
		{
			name:      "only digits",
			password:  "12345678",
			wantErr:   true,
			errSubstr: "complexity requirements",
		},
		{
			name:     "password with special characters",
			password: "P@ssw0rd!",
			wantErr:  false,
		},
		{
			name:     "password with unicode letters",
			password: "Passwort1",
			wantErr:  false,
		},
		{
			name:      "short password reports multiple failures",
			password:  "ab",
			wantErr:   true,
			errSubstr: "at least 8 characters",
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
