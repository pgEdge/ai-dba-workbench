/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package mcp

import (
	"slices"
	"testing"
)

func TestNegotiateProtocolVersion(t *testing.T) {
	tests := []struct {
		requested string
		want      string
	}{
		{"2025-11-25", "2025-11-25"},
		{"2025-06-18", "2025-06-18"},
		{"2025-03-26", "2025-03-26"},
		{"2024-11-05", "2024-11-05"},
		{"", "2025-11-25"},
		{"2026-07-28", "2025-11-25"},
		{"2024-10-07", "2025-11-25"},
		{" 2025-06-18", "2025-11-25"},
	}

	for _, tt := range tests {
		t.Run(tt.requested, func(t *testing.T) {
			if got := NegotiateProtocolVersion(tt.requested); got != tt.want {
				t.Errorf("NegotiateProtocolVersion(%q) = %q, want %q",
					tt.requested, got, tt.want)
			}
		})
	}
}

func TestSupportedProtocolVersions(t *testing.T) {
	want := []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}
	got := SupportedProtocolVersions()
	if !slices.Equal(got, want) {
		t.Fatalf("SupportedProtocolVersions() = %v, want %v", got, want)
	}
	if got[0] != ProtocolVersion {
		t.Errorf("latest supported = %q, want ProtocolVersion %q", got[0], ProtocolVersion)
	}

	// The result is a copy, so a caller cannot change what is supported.
	got[0] = "1900-01-01"
	if !IsSupportedProtocolVersion("2025-11-25") || IsSupportedProtocolVersion("1900-01-01") {
		t.Error("mutating the returned slice changed the supported set")
	}
}
