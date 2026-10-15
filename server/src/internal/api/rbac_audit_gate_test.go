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
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// createTestRBACHandlerWithDir is createTestRBACHandler with the data
// directory exposed, so that a test can reach into the auth database
// and break one table on purpose.
func createTestRBACHandlerWithDir(t *testing.T) (*RBACHandler, *auth.AuthStore,
	string, func()) {

	t.Helper()

	dir, err := os.MkdirTemp("", "rbac-audit-gate-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store, err := auth.NewAuthStore(dir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("Failed to create auth store: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	handler := NewRBACHandler(store, auth.NewRBACChecker(store))
	cleanup := func() {
		store.Close()
		os.RemoveAll(dir)
	}

	return handler, store, dir, cleanup
}

// dropAuthTable drops one table from the auth database behind the
// running store, so that exactly one lookup fails whilst every other
// query keeps working. Closing the whole store would deny for any
// reason at all, which would not test the branch under examination.
func dropAuthTable(t *testing.T, dataDir, table string) {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "auth.db"))
	if err != nil {
		t.Fatalf("Failed to open the auth database: %v", err)
	}
	defer db.Close()

	// Each permitted name has its own arm holding a fully literal
	// statement, so that no table name is ever interpolated into SQL,
	// and a name that is not on the list fails the test where it was
	// asked for rather than as an opaque SQL error at the driver.
	var execErr error
	switch table {
	case "tokens":
		_, execErr = db.Exec("DROP TABLE tokens")
	case "token_connection_scope":
		_, execErr = db.Exec("DROP TABLE token_connection_scope")
	case "token_mcp_scope":
		_, execErr = db.Exec("DROP TABLE token_mcp_scope")
	case "token_admin_scope":
		_, execErr = db.Exec("DROP TABLE token_admin_scope")
	case "group_memberships":
		_, execErr = db.Exec("DROP TABLE group_memberships")
	case "connection_privileges":
		_, execErr = db.Exec("DROP TABLE connection_privileges")
	case "group_admin_permissions":
		_, execErr = db.Exec("DROP TABLE group_admin_permissions")
	case "user_groups":
		_, execErr = db.Exec("DROP TABLE user_groups")
	default:
		t.Fatalf("dropAuthTable: %q is not a droppable table", table)
	}
	if execErr != nil {
		t.Fatalf("Failed to drop %s: %v", table, execErr)
	}
}

// singleDenialEvent returns the one recorded audit.read denial,
// failing the test when there is not exactly one.
func singleDenialEvent(t *testing.T, store *auth.AuthStore) auth.AuditEvent {
	t.Helper()

	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action:  "audit.read",
		Outcome: string(auth.OutcomeDenied),
	})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected one denial event, got %d", len(events))
	}
	return events[0]
}

// withSuperuserToken presents the request as a superuser authenticated
// by the given API token.
func withSuperuserToken(req *http.Request, tokenID int64) *http.Request {
	ctx := context.WithValue(req.Context(), auth.IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, auth.IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, auth.TokenIDContextKey, tokenID)
	return req.WithContext(ctx)
}

// auditTokenRequest issues an audit GET as a superuser API token.
func auditTokenRequest(h *RBACHandler, tokenID int64) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit", nil)
	req = withSuperuserToken(req, tokenID)
	rec := httptest.NewRecorder()
	h.handleAudit(rec, req)
	return rec
}

// mustCreateScopedToken creates a service account, mints a token for it
// and sets the token's admin scope to the given permissions.
func mustCreateScopedToken(t *testing.T, store *auth.AuthStore,
	username string, scope []string) int64 {

	t.Helper()

	if err := store.CreateServiceAccount(username, "", "", ""); err != nil {
		t.Fatalf("Failed to create service account %s: %v", username, err)
	}
	_, token, err := store.AsActor(auth.SystemActor()).CreateToken(username, "audit gate test", nil, true)
	if err != nil {
		t.Fatalf("Failed to create token for %s: %v", username, err)
	}
	if len(scope) > 0 {
		if err := store.AsActor(auth.SystemActor()).SetTokenAdminScope(token.ID, scope, true); err != nil {
			t.Fatalf("Failed to set admin scope: %v", err)
		}
	}

	return token.ID
}

// TestRBACHandlerAuditRejectsScopedToken checks that a superuser's
// token whose admin scope was narrowed cannot read the audit log, and
// that the refusal is itself audited. The refusal now comes from
// requireSuperuser, because a narrowed admin scope withdraws the
// token's superuser status outright; the audit endpoint no longer
// carries a gate of its own.
func TestRBACHandlerAuditRejectsScopedToken(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	tokenID := mustCreateScopedToken(t, store, "svc-narrow",
		[]string{auth.PermManageUsers})

	rec := auditTokenRequest(handler, tokenID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "superuser privileges") {
		t.Errorf("Expected a superuser refusal, got %q", body)
	}

	// requireSuperuser emits the same sentence for every refusal, so
	// the branch is identified by what was recorded and by what the
	// same token can still do: the audit event names the acting token,
	// meaning it was identified and its scope was read, and the
	// permission its scope names is still allowed, meaning this is the
	// blanket gate refusing rather than the token being cut off.
	event := singleDenialEvent(t, store)
	if event.ActorType != auth.ActorToken || event.ActorID == nil ||
		*event.ActorID != tokenID {
		t.Errorf("Expected the denial to name token %d, got %s/%v", tokenID,
			event.ActorType, event.ActorID)
	}

	req := withSuperuserToken(
		httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil), tokenID)
	if !handler.rbacChecker.HasAdminPermission(req.Context(), auth.PermManageUsers) {
		t.Error("Expected the scoped permission to remain allowed")
	}
}

// TestRBACHandlerAuditAllowsUnscopedToken checks the two passing
// branches: a token with no admin scope at all, and one holding the
// wildcard.
func TestRBACHandlerAuditAllowsUnscopedToken(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	unscoped := mustCreateScopedToken(t, store, "svc-open", nil)
	wildcard := mustCreateScopedToken(t, store, "svc-wild",
		[]string{auth.AdminPermissionWildcard})

	for name, tokenID := range map[string]int64{
		"no scope": unscoped,
		"wildcard": wildcard,
	} {
		rec := auditTokenRequest(handler, tokenID)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d: %s", name, rec.Code,
				rec.Body.String())
		}
	}
}

// TestRBACHandlerAuditAllowsSessionSuperuser checks that a superuser
// authenticated by session, which carries no token, is unaffected by
// the token gate.
func TestRBACHandlerAuditAllowsSessionSuperuser(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	if rec := auditRequest(handler, ""); rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestRBACHandlerAuditTokenWithoutIDIsRefused covers an API-token
// request that carries no acting token id. Its admin scope cannot be
// checked, so its superuser status cannot be confirmed and the request
// is refused: a route mounted without createAuthWrapper must fail
// here, not be waved on.
func TestRBACHandlerAuditTokenWithoutIDIsRefused(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	rec := auditTokenRequest(handler, 0)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "superuser privileges") {
		t.Errorf("Expected the refusal to say why, got %s", rec.Body.String())
	}

	// This branch is told apart from a scope refusal by the actor: no
	// token id survived into the context, so nothing could be
	// identified and the denial is recorded against no token at all.
	event := singleDenialEvent(t, store)
	if event.ActorID != nil {
		t.Errorf("Expected the denial to name no acting token, got %v",
			*event.ActorID)
	}
	if event.ActorType == auth.ActorToken {
		t.Error("Expected an unidentified caller not to be recorded as a token")
	}
}

// TestRBACHandlerAuditScopeLookupFailureDenies checks that a scope
// lookup which errors refuses the request rather than letting it
// through.
func TestRBACHandlerAuditScopeLookupFailureDenies(t *testing.T) {
	handler, store, dir, cleanup := createTestRBACHandlerWithDir(t)
	defer cleanup()

	tokenID := mustCreateScopedToken(t, store, "svc-broken",
		[]string{auth.PermManageUsers})

	// Only the admin scope lookup is broken; the rest of the store
	// keeps working, so a 403 here can only have come from the failed
	// lookup, and the denial is still recorded.
	dropAuthTable(t, dir, "token_admin_scope")

	rec := auditTokenRequest(handler, tokenID)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 when the scope cannot be read, got %d", rec.Code)
	}

	event := singleDenialEvent(t, store)
	if event.ActorID == nil || *event.ActorID != tokenID {
		t.Errorf("Expected the denial to name token %d, got %v", tokenID,
			event.ActorID)
	}

	// Unlike a narrowed scope, an unreadable one denies the named
	// permission too, which is what separates this branch from the
	// refusal in TestRBACHandlerAuditRejectsScopedToken.
	req := withSuperuserToken(
		httptest.NewRequest(http.MethodGet, "/api/v1/rbac/users", nil), tokenID)
	if handler.rbacChecker.HasAdminPermission(req.Context(), auth.PermManageUsers) {
		t.Error("Expected an unreadable scope to deny the permission as well")
	}
}

// =============================================================================
// Denial coalescing
// =============================================================================

// TestRecordDenialWritesRepeatCount checks the end-to-end path: a
// repeated denial writes one row, and the row that closes the window
// carries repeat_count in its details.
func TestRecordDenialWritesRepeatCount(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit", nil)
	req = withUser(req, 7)

	for i := 0; i < 3; i++ {
		handler.recordDenial(req, "Permission denied: requires superuser privileges")
	}

	events, _, err := store.ListAuditEvents(auth.AuditFilter{Action: "audit.read"})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected the repeats to coalesce into 1 row, got %d", len(events))
	}
	if string(events[0].Details) != `{"target":"audit"}` {
		t.Errorf("Expected only the target on the first denial, got %s",
			events[0].Details)
	}

	// Age the window so the next denial closes it.
	ageDenials(handler)

	handler.recordDenial(req, "Permission denied: requires superuser privileges")

	events, _, err = store.ListAuditEvents(auth.AuditFilter{Action: "audit.read"})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Expected 2 rows after the window closed, got %d", len(events))
	}

	var details struct {
		RepeatCount int `json:"repeat_count"`
	}
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", events[0].Details, err)
	}
	if details.RepeatCount != 3 {
		t.Errorf("Expected repeat_count 3, got %d", details.RepeatCount)
	}
}

// TestRecordDenialCoalescesAcrossTargets checks that a flood of
// refusals from one actor against many distinct objects writes one row
// while the window is open, and that the row closing the window lists
// the distinct targets, capped at maxDenialTargets, with the rest
// counted in targets_truncated, rather than one row per path.
// maxDenialTargets mirrors the auth store's cap of 20 targets a window.
func TestRecordDenialCoalescesAcrossTargets(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	const maxDenialTargets = 20

	const reason = "Permission denied: this token's scope does not cover the access being granted"
	const flood = 50
	deny := func(id int) {
		req := withUser(httptest.NewRequest(http.MethodDelete,
			fmt.Sprintf("/api/v1/rbac/users/%d", id), nil), 7)
		handler.recordDenial(req, reason)
	}
	for id := 1; id <= flood; id++ {
		deny(id)
	}
	// Repeating a listed target adds to the count but not to the list.
	deny(2)

	events, _, err := store.ListAuditEvents(auth.AuditFilter{Action: "user.delete"})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected the flood to coalesce into 1 row, got %d", len(events))
	}
	if string(events[0].Details) != `{"target":"users/1"}` {
		t.Errorf("Expected the first row to name users/1, got %s", events[0].Details)
	}
	ageDenials(handler)
	deny(flood + 1)

	events, _, err = store.ListAuditEvents(auth.AuditFilter{Action: "user.delete"})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Expected 2 rows after the window closed, got %d", len(events))
	}
	var details struct {
		Target           string   `json:"target"`
		RepeatCount      int      `json:"repeat_count"`
		Targets          []string `json:"targets"`
		TargetsTruncated int      `json:"targets_truncated"`
	}
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", events[0].Details, err)
	}
	// 49 distinct suppressed targets plus one repeat, plus the closing
	// denial itself.
	if details.RepeatCount != flood+1 {
		t.Errorf("Expected repeat_count %d, got %d", flood+1, details.RepeatCount)
	}
	if details.Target != fmt.Sprintf("users/%d", flood+1) {
		t.Errorf("Expected the closing row to name its own target, got %q",
			details.Target)
	}
	if len(details.Targets) != maxDenialTargets {
		t.Fatalf("Expected %d targets listed, got %v", maxDenialTargets,
			details.Targets)
	}
	if details.Targets[0] != "users/2" {
		t.Errorf("Expected the first suppressed target first, got %v",
			details.Targets)
	}
	// Targets users/22 to users/51 arrived after the list was full.
	if details.TargetsTruncated != flood+1-1-maxDenialTargets {
		t.Errorf("Expected targets_truncated %d, got %d",
			flood+1-1-maxDenialTargets, details.TargetsTruncated)
	}
}

// ageDenials moves every coalescing window the handler's store tracks
// back well past the 60 second window, keeping their order, so that the
// next denial closes or evicts them.
func ageDenials(handler *RBACHandler) {
	auth.AgeCoalescedAuditForTesting(handler.authStore, time.Hour)
}

// TestRecordDenialSummaryListsTargets checks that the summary written
// for an evicted entry lists the targets its suppressed repeats named.
func TestRecordDenialSummaryListsTargets(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	const reason = "Permission denied: requires manage_groups permission"
	for _, path := range []string{"/api/v1/rbac/groups/3", "/api/v1/rbac/groups/4",
		"/api/v1/rbac/groups/0004", "/api/v1/rbac/groups/5/"} {
		req := withUser(httptest.NewRequest(http.MethodDelete, path, nil), 7)
		handler.recordDenial(req, reason)
	}

	ageDenials(handler)
	handler.recordDenial(withUser(httptest.NewRequest(http.MethodPost,
		"/api/v1/rbac/groups", nil), 7), reason)

	events, _, err := store.ListAuditEvents(auth.AuditFilter{Action: "group.delete"})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Expected the first denial and its summary, got %d rows", len(events))
	}
	var details struct {
		RepeatCount      int      `json:"repeat_count"`
		Targets          []string `json:"targets"`
		TargetsTruncated int      `json:"targets_truncated"`
		WindowClosed     bool     `json:"window_closed"`
		FirstSeen        string   `json:"first_seen"`
		LastSeen         string   `json:"last_seen"`
	}
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", events[0].Details, err)
	}
	if details.RepeatCount != 3 || !details.WindowClosed ||
		details.TargetsTruncated != 0 ||
		strings.Join(details.Targets, ",") != "groups/4,groups/5" ||
		details.FirstSeen == "" || details.LastSeen == "" {
		t.Errorf("Unexpected summary %s", events[0].Details)
	}
}

// TestDenialTargetNormalization covers the targets recorded for refused
// requests: a kind and parsed id where the route names one, the kind
// alone where it does not, and a constant for an unrecognized route, so
// that nothing the caller writes in the path reaches the row verbatim.
func TestDenialTargetNormalization(t *testing.T) {
	long := "/api/v1/rbac/users/" + strings.Repeat("9", 4096)
	tests := []struct {
		method, path, action, target string
	}{
		{http.MethodDelete, "/api/v1/rbac/users/3", "user.delete", "users/3"},
		{http.MethodDelete, "/api/v1/rbac/users/003", "user.delete", "users/3"},
		{http.MethodDelete, long, "user.delete", "users"},
		{http.MethodDelete, "/api/v1/rbac/users/-1", "user.delete", "users"},
		{http.MethodPost, "/api/v1/rbac/users", "user.create", "users"},
		{http.MethodDelete, "/api/v1/rbac/groups/5/members/user/3",
			"group.member.remove", "groups/5"},
		{http.MethodPost, "/api/v1/connections", "connection.create", "connections"},
		{http.MethodPost, "/api/v1/connections/", "connection.create", "connections"},
		{http.MethodPut, "/api/v1/connections/7", "connection.update", "connections/7"},
		{http.MethodPut, "/api/v1/connections/7/cluster", "connection.cluster.update",
			"connections/7"},
		{http.MethodPost, "/api/v1/clusters", "cluster.create", "clusters"},
		{http.MethodDelete, "/api/v1/clusters/9", "cluster.delete", "clusters/9"},
		{http.MethodPost, "/api/v1/clusters/9/servers", "cluster.update", "clusters/9"},
		{http.MethodDelete, "/api/v1/clusters/9/servers/4", "cluster.update", "clusters/9"},
		{http.MethodPut, "/api/v1/clusters/server-abc", "cluster.update", "clusters"},
		{http.MethodPatch, "/api/v1/cluster-groups/2", "cluster_group.update",
			"cluster-groups/2"},
		{http.MethodDelete, "/api/v1/notification-channels/6/recipients/1",
			"notification_channel.update", "notification-channels/6"},
		{http.MethodPut, "/api/v1/alert-rules/8", "alert_rule.update", "alert-rules/8"},
		{http.MethodGet, "/api/v1/alert-rules/8", "alert_rule.read", "alert-rules/8"},
		{http.MethodOptions, "/api/v1/alert-rules/8", "rbac.options", unmatchedDenialTarget},
		{http.MethodPut, "/api/v1/widgets/8", "rbac.put", unmatchedDenialTarget},
		{http.MethodPut, "/elsewhere", "rbac.put", unmatchedDenialTarget},
		{http.MethodGet, "/api/v1/rbac/users/" + strings.Repeat("x", 300), "rbac.get",
			unmatchedDenialTarget},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		action, target := deniedRoute(req)
		if action != tt.action || target != tt.target {
			t.Errorf("deniedRoute(%s %.60s) = (%q, %q), want (%q, %q)", tt.method,
				tt.path, action, target, tt.action, tt.target)
		}
	}
}

// TestDeniedActionBoundsMethod checks that a method outside the
// standard set is recorded as rbac.other.
func TestDeniedActionBoundsMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/widgets", nil)
	req.Method = strings.Repeat("X", 4096)
	if got := deniedAction(req); got != "rbac.other" {
		t.Errorf("Expected rbac.other for a nonstandard method, got %q", got)
	}
	req.Method = http.MethodOptions
	if got := deniedAction(req); got != "rbac.options" {
		t.Errorf("Expected rbac.options, got %q", got)
	}
}

// TestRecordDenialRecordsOddMethodAsOther checks the end-to-end path for a
// nonstandard method: the row is written under rbac.other.
func TestRecordDenialRecordsOddMethodAsOther(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	req := withUser(httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit", nil), 7)
	req.Method = "BREW"
	handler.recordDenial(req, "Permission denied")

	events, _, err := store.ListAuditEvents(auth.AuditFilter{Action: "rbac.other"})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 1 || string(events[0].Details) != `{"target":"unmatched"}` {
		t.Fatalf("Expected one rbac.other row naming no target, got %d", len(events))
	}
}

// TestDeniedConnectionAction covers the action names given to refused
// connection requests.
func TestDeniedConnectionAction(t *testing.T) {
	tests := []struct {
		method, path, want string
	}{
		{http.MethodPost, "/api/v1/connections", "connection.create"},
		{http.MethodGet, "/api/v1/connections", "rbac.get"},
		{http.MethodPut, "/api/v1/connections/5", "connection.update"},
		{http.MethodDelete, "/api/v1/connections/5", "connection.delete"},
		{http.MethodPut, "/api/v1/connections/5/cluster", "connection.cluster.update"},
		{http.MethodGet, "/api/v1/connections/5", "rbac.get"},
		{http.MethodPut, "/api/v1/connections/5/databases", "rbac.put"},
		{http.MethodPut, "/api/v1/clusters/5", "cluster.update"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		if got := deniedAction(req); got != tt.want {
			t.Errorf("deniedAction(%s %s) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

// TestRecordDenialToleratesNilStore keeps the nil-store behavior that
// a partially constructed handler relies on.
func TestRecordDenialToleratesNilStore(t *testing.T) {
	handler := &RBACHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit", nil)

	// Reaching the end without a panic is the assertion.
	handler.recordDenial(req, "Permission denied")
}

// withTokenFrom presents the request as an API token identified by
// tokenID, arriving from the given client address.
func withTokenFrom(req *http.Request, tokenID int64, ip string) *http.Request {
	ctx := context.WithValue(req.Context(), auth.IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, auth.TokenIDContextKey, tokenID)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "mallory")
	ctx = context.WithValue(ctx, auth.IPAddressContextKey, ip)
	return req.WithContext(ctx)
}

// TestRecordDenialSeparatesTokensAndAddresses checks that two tokens of
// the same user, and one token used from two addresses, never suppress
// each other's denials.
func TestRecordDenialSeparatesTokensAndAddresses(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	const reason = "Permission denied: requires superuser privileges"
	newReq := func(tokenID int64, ip string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit", nil)
		return withTokenFrom(req, tokenID, ip)
	}

	handler.recordDenial(newReq(1, "192.0.2.10"), reason)
	// The identical repeat is suppressed, so it adds no row.
	handler.recordDenial(newReq(1, "192.0.2.10"), reason)
	// A second token of the same user, and the first token seen from a
	// second address, are each distinct.
	handler.recordDenial(newReq(2, "192.0.2.10"), reason)
	handler.recordDenial(newReq(1, "192.0.2.11"), reason)

	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action:  "audit.read",
		Outcome: string(auth.OutcomeDenied),
	})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("Expected 3 denial rows (one per token and address), got %d",
			len(events))
	}
}

// TestRecordDenialSummarisesEvictedRepeats checks that repeats which
// were suppressed in a window nobody returned to are written as a
// summary row when the entry is evicted, rather than discarded.
func TestRecordDenialSummarisesEvictedRepeats(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	const reason = "Permission denied: requires superuser privileges"
	burst := withTokenFrom(httptest.NewRequest(http.MethodGet,
		"/api/v1/rbac/audit", nil), 11, "192.0.2.20")

	// One recorded denial followed by two suppressed repeats.
	for i := 0; i < 3; i++ {
		handler.recordDenial(burst, reason)
	}

	// Age the window so the burst's entry is evicted by the next,
	// unrelated denial rather than reported by a repeat of its own.
	ageDenials(handler)

	other := withTokenFrom(httptest.NewRequest(http.MethodPost,
		"/api/v1/rbac/users", nil), 11, "192.0.2.20")
	handler.recordDenial(other, reason)

	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action:  "audit.read",
		Outcome: string(auth.OutcomeDenied),
	})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Expected the first denial and its summary, got %d rows",
			len(events))
	}

	var details struct {
		RepeatCount  int  `json:"repeat_count"`
		WindowClosed bool `json:"window_closed"`
	}
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", events[0].Details, err)
	}
	if details.RepeatCount != 2 || !details.WindowClosed {
		t.Errorf("Expected {repeat_count: 2, window_closed: true}, got %s",
			events[0].Details)
	}
}

// TestDeniedGroupActionUnknownSubresource checks that a group route
// naming a sub-resource the handler does not serve falls through to the
// caller's fallback rather than inventing an action.
func TestDeniedGroupActionUnknownSubresource(t *testing.T) {
	parts := []string{"groups", "4", "widgets"}
	if got := deniedGroupAction(http.MethodPost, parts); got != "" {
		t.Errorf("Expected no action for an unknown sub-resource, got %q", got)
	}
}
