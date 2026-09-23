/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// auditReadAsUser issues an audit GET as a named superuser, so that the
// log line under test has an actor to name.
func auditReadAsUser(h *RBACHandler, username, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit"+query, nil)
	ctx := context.WithValue(req.Context(), auth.IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, username)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.handleAudit(rec, req)
	return rec
}

// TestAuditReadIsLogged checks that a successful read of the audit log
// leaves a line in the server log naming who read it, what they asked
// for and how much they got. Successful reads are deliberately not
// written to audit_events itself, so this line is the only record that
// the read happened.
func TestAuditReadIsLogged(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	seedAuditEvents(t, store)

	logBuf := captureLog(t)
	rec := auditReadAsUser(handler, "carol", "?"+seedActorFilter+"&limit=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	line := logBuf.String()
	for _, want := range []string{
		"[AUDIT]", "carol", `actor="seeder"`, `actor_type="user"`,
		`limit="2"`, "2 row(s) returned",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("Expected the log line to contain %q, got %q", want, line)
		}
	}
	if strings.Count(strings.TrimSpace(line), "[AUDIT]") != 1 {
		t.Errorf("Expected exactly one [AUDIT] line, got %q", line)
	}
}

// TestAuditReadLogNamesAnUnfilteredRead checks that a read of everything
// says so, rather than printing a row of empty filter values that reads
// as though something had been narrowed.
func TestAuditReadLogNamesAnUnfilteredRead(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	seedAuditEvents(t, store)

	logBuf := captureLog(t)
	if rec := auditReadAsUser(handler, "carol", ""); rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	if !strings.Contains(logBuf.String(), "filter none") {
		t.Errorf("Expected an unfiltered read to say so, got %q", logBuf.String())
	}
}

// TestAuditReadLogIsNotWrittenForAFailedRead checks that a request
// refused before it reaches the store leaves no line claiming a read
// took place.
func TestAuditReadLogIsNotWrittenForAFailedRead(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	logBuf := captureLog(t)

	// A malformed filter is a 400, and a non-superuser is a 403.
	if rec := auditReadAsUser(handler, "carol", "?limit=banana"); rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit", nil)
	rec := httptest.NewRecorder()
	handler.handleAudit(rec, withUser(req, 1))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d", rec.Code)
	}

	if strings.Contains(logBuf.String(), "[AUDIT]") {
		t.Errorf("Expected no audit read line for refused requests, got %q",
			logBuf.String())
	}
}

// TestDescribeAuditFilter covers the rendering of each filter, since the
// log line is only useful if it says what was actually asked for.
func TestDescribeAuditFilter(t *testing.T) {
	targetID := int64(7)
	since := time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)
	until := time.Date(2026, 9, 2, 8, 30, 0, 0, time.UTC)

	tests := []struct {
		name   string
		filter auth.AuditFilter
		want   string
	}{
		{
			name:   "empty",
			filter: auth.AuditFilter{},
			want:   "none",
		},
		{
			name:   "single",
			filter: auth.AuditFilter{Action: "user.create"},
			want:   `action="user.create"`,
		},
		{
			// A value holding the separator would otherwise render as
			// two filters, and anything reading the line back would
			// believe the second one had been applied.
			name:   "a space in a value does not fabricate a filter",
			filter: auth.AuditFilter{ActorName: "alice action=user.delete"},
			want:   `actor="alice action=user.delete"`,
		},
		{
			// Nor can a value close its own quoting to the same end.
			name:   "a quote in a value is escaped",
			filter: auth.AuditFilter{ActorName: `alice" action="user.delete`},
			want:   `actor="alice\" action=\"user.delete"`,
		},
		{
			name: "every filter",
			filter: auth.AuditFilter{
				ActorName: "alice", ActorType: "user", Action: "user.create",
				TargetType: "user", TargetID: &targetID, Outcome: "success",
				Since: &since, Until: &until, Limit: 10, Offset: 20,
			},
			want: `actor="alice" actor_type="user" action="user.create" ` +
				`target_type="user" target_id="7" outcome="success" ` +
				`since="2026-09-01T08:30:00Z" until="2026-09-02T08:30:00Z" ` +
				`limit="10" offset="20"`,
		},
		{
			name:   "zero limit and offset are omitted",
			filter: auth.AuditFilter{Outcome: "denied", Limit: 0, Offset: 0},
			want:   `outcome="denied"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeAuditFilter(tc.filter); got != tc.want {
				t.Errorf("describeAuditFilter = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDescribeAuditFilterCapsLongValues checks that a filter value
// supplied in the query string cannot write a line of unbounded length
// to the server log, and that the cut is marked and never splits a
// multi-byte character.
func TestDescribeAuditFilterCapsLongValues(t *testing.T) {
	long := strings.Repeat("é", auditFilterValueMax*4)
	got := describeAuditFilter(auth.AuditFilter{ActorName: long})

	want := `actor="` + strings.Repeat("é", auditFilterValueMax-3) + `..."`
	if got != want {
		t.Errorf("describeAuditFilter = %q, want %q", got, want)
	}

	exact := strings.Repeat("a", auditFilterValueMax)
	if got := capAuditFilterValue(exact); got != exact {
		t.Errorf("a value at the cap must be kept whole, got %q", got)
	}
}
