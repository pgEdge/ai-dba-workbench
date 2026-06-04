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

import "testing"

// TestValidateEmail exercises the shared email validator used by the RBAC
// user handlers. It confirms that well-formed addresses pass and that the two
// failure modes called out in the issue ("notanemail" with no "@" and "test@"
// with no domain) are both rejected, along with a few related edge cases. It
// also confirms parity with the client regex by rejecting the RFC 5322
// name-addr form and addresses with surrounding whitespace, which the handlers
// would otherwise store verbatim.
func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{name: "simple valid address", email: "user@example.com", wantErr: false},
		{name: "subdomain valid address", email: "user@mail.example.co.uk", wantErr: false},
		{name: "plus tag valid address", email: "user+tag@example.com", wantErr: false},
		{name: "display name form rejected", email: "User <user@example.com>", wantErr: true},
		{name: "leading and trailing whitespace rejected", email: " user@example.com ", wantErr: true},
		{name: "leading whitespace rejected", email: " user@example.com", wantErr: true},
		{name: "trailing whitespace rejected", email: "user@example.com ", wantErr: true},
		{name: "no at sign", email: "notanemail", wantErr: true},
		{name: "empty local and domain", email: "test@", wantErr: true},
		{name: "domain without dot", email: "user@localhost", wantErr: true},
		{name: "empty string", email: "", wantErr: true},
		{name: "whitespace only", email: "   ", wantErr: true},
		{name: "missing local part", email: "@example.com", wantErr: true},
		{name: "trailing dot only domain part is fine", email: "a@b.c", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmail(tt.email)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for email %q, got nil", tt.email)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error for email %q, got %v", tt.email, err)
			}
		})
	}
}
