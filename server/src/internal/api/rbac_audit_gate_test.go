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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// withSuperuserToken presents the request as a superuser authenticated
// by the given API token.
func withSuperuserToken(req *http.Request, tokenID int64) *http.Request {
	ctx := context.WithValue(req.Context(), auth.IsSuperuserContextKey, true)
	ctx = context.WithValue(ctx, auth.IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, auth.AuditTokenIDContextKey, tokenID)
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
	_, token, err := store.CreateToken(username, "audit gate test", nil)
	if err != nil {
		t.Fatalf("Failed to create token for %s: %v", username, err)
	}
	if len(scope) > 0 {
		if err := store.SetTokenAdminScope(token.ID, scope); err != nil {
			t.Fatalf("Failed to set admin scope: %v", err)
		}
	}

	return token.ID
}

// TestRBACHandlerAuditRejectsScopedToken checks that a superuser's
// token whose admin scope was narrowed cannot read the audit log, and
// that the refusal is itself audited.
func TestRBACHandlerAuditRejectsScopedToken(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	tokenID := mustCreateScopedToken(t, store, "svc-narrow",
		[]string{auth.PermManageUsers})

	rec := auditTokenRequest(handler, tokenID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "token admin scope") {
		t.Errorf("Expected a scope message, got %q", body)
	}

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

// TestRBACHandlerAuditTokenWithoutAuditID covers an API-token request
// that carries no acting token id, which cannot be scope-checked and so
// falls through to the superuser decision already made.
func TestRBACHandlerAuditTokenWithoutAuditID(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	if rec := auditTokenRequest(handler, 0); rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestRBACHandlerAuditScopeLookupFailureDenies checks that a scope
// lookup which errors refuses the request rather than letting it
// through.
func TestRBACHandlerAuditScopeLookupFailureDenies(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	tokenID := mustCreateScopedToken(t, store, "svc-broken",
		[]string{auth.PermManageUsers})
	store.Close()

	rec := auditTokenRequest(handler, tokenID)
	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 when the scope cannot be read, got %d", rec.Code)
	}
}

// =============================================================================
// Denial coalescing
// =============================================================================

// denialKeyFor builds the map key the handler uses for a given actor
// and reason.
func denialKeyFor(name, action, reason string) denialKey {
	return denialKey{
		actorType: string(auth.ActorUser),
		actorName: name,
		action:    action,
		reason:    reason,
	}
}

// TestAdmitDenialCoalescesWithinWindow checks that identical denials
// inside the window are counted rather than recorded, and that the
// first denial after the window reports how many it stands for.
func TestAdmitDenialCoalescesWithinWindow(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	key := denialKeyFor("mallory", "user.create", "Permission denied")
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)

	if record, repeats, _ := handler.admitDenial(key, start); !record || repeats != 0 {
		t.Fatalf("First denial: expected (true, 0), got (%v, %d)", record, repeats)
	}
	for i := 1; i <= 4; i++ {
		at := start.Add(time.Duration(i) * time.Second)
		if record, repeats, _ := handler.admitDenial(key, at); record || repeats != 0 {
			t.Errorf("Repeat %d: expected (false, 0), got (%v, %d)", i, record,
				repeats)
		}
	}

	after := start.Add(denialCoalesceWindow + time.Second)
	record, repeats, _ := handler.admitDenial(key, after)
	if !record {
		t.Fatal("Expected the denial after the window to be recorded")
	}
	if repeats != 5 {
		t.Errorf("Expected repeat_count 5 (4 suppressed plus this one), got %d",
			repeats)
	}

	// The window has been reopened, so the next repeat is suppressed
	// again and carries no stale count.
	if record, repeats, _ := handler.admitDenial(key,
		after.Add(time.Second)); record || repeats != 0 {
		t.Errorf("Expected the reopened window to suppress, got (%v, %d)",
			record, repeats)
	}
}

// TestAdmitDenialWithoutRepeatsCarriesNoCount checks that a denial
// following a quiet window records no repeat_count at all.
func TestAdmitDenialWithoutRepeatsCarriesNoCount(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	key := denialKeyFor("mallory", "user.create", "Permission denied")
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)

	handler.admitDenial(key, start)
	// The entry is evicted as expired and then reinserted, so either
	// path must report no repeats.
	if record, repeats, _ := handler.admitDenial(key,
		start.Add(2*denialCoalesceWindow)); !record || repeats != 0 {
		t.Errorf("Expected (true, 0), got (%v, %d)", record, repeats)
	}
}

// TestAdmitDenialSeparatesActors checks that one actor's repeats never
// suppress another's denial.
func TestAdmitDenialSeparatesActors(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	mallory := denialKeyFor("mallory", "user.create", "Permission denied")
	trudy := denialKeyFor("trudy", "user.create", "Permission denied")
	otherAction := denialKeyFor("mallory", "group.create", "Permission denied")
	otherReason := denialKeyFor("mallory", "user.create", "Something else")

	for _, key := range []denialKey{mallory, trudy, otherAction, otherReason} {
		if record, _, _ := handler.admitDenial(key, start); !record {
			t.Errorf("Expected the first denial for %+v to be recorded", key)
		}
	}
	for _, key := range []denialKey{mallory, trudy, otherAction, otherReason} {
		if record, _, _ := handler.admitDenial(key, start.Add(time.Second)); record {
			t.Errorf("Expected the repeat for %+v to be suppressed", key)
		}
	}
}

// TestAdmitDenialEvictsExpiredEntries checks that entries whose window
// has closed do not accumulate.
func TestAdmitDenialEvictsExpiredEntries(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		handler.admitDenial(denialKeyFor("mallory",
			"user.create", string(rune('a'+i))), start)
	}
	if got := len(handler.denials); got != 20 {
		t.Fatalf("Expected 20 tracked keys, got %d", got)
	}

	handler.admitDenial(denialKeyFor("trudy", "user.create", "late"),
		start.Add(2*denialCoalesceWindow))
	if got := len(handler.denials); got != 1 {
		t.Errorf("Expected the expired entries to be evicted, %d remain", got)
	}
}

// TestAdmitDenialCapsMapSize checks that a client varying the reason
// cannot grow the map without bound: the cap holds, and the oldest
// entry is the one dropped.
func TestAdmitDenialCapsMapSize(t *testing.T) {
	handler, _, cleanup := createTestRBACHandler(t)
	defer cleanup()

	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < maxDenialKeys+50; i++ {
		// Every key is inside the same window, so nothing expires and
		// only the cap can bound the map.
		handler.admitDenial(denialKeyFor("mallory", "user.create",
			"reason-"+string(rune(i))), start.Add(time.Duration(i)*time.Microsecond))
	}

	if got := len(handler.denials); got > maxDenialKeys {
		t.Errorf("Expected at most %d tracked keys, got %d", maxDenialKeys, got)
	}
	if _, ok := handler.denials[denialKeyFor("mallory", "user.create",
		"reason-"+string(rune(0)))]; ok {
		t.Error("Expected the oldest entry to have been evicted")
	}
}

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
	if len(events[0].Details) != 0 {
		t.Errorf("Expected no details on the first denial, got %s",
			events[0].Details)
	}

	// Age the window so the next denial closes it.
	handler.denialMu.Lock()
	for _, state := range handler.denials {
		state.firstSeen = state.firstSeen.Add(-2 * denialCoalesceWindow)
	}
	handler.denialMu.Unlock()

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

// TestRecordDenialToleratesNilStore keeps the nil-store behavior that
// a partially constructed handler relies on.
func TestRecordDenialToleratesNilStore(t *testing.T) {
	handler := &RBACHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit", nil)

	handler.recordDenial(req, "Permission denied")

	if len(handler.denials) != 0 {
		t.Error("Expected a nil store to record nothing")
	}
}

// TestAdmitDenialInitialisesMap checks the lazy initialisation used by
// a handler built as a struct literal rather than through
// NewRBACHandler.
func TestAdmitDenialInitialisesMap(t *testing.T) {
	handler := &RBACHandler{}

	if record, _, _ := handler.admitDenial(
		denialKeyFor("mallory", "user.create", "no"), time.Now()); !record {
		t.Error("Expected the first denial to be recorded")
	}
	if len(handler.denials) != 1 {
		t.Errorf("Expected 1 tracked key, got %d", len(handler.denials))
	}
}

// withTokenFrom presents the request as an API token identified by
// tokenID, arriving from the given client address.
func withTokenFrom(req *http.Request, tokenID int64, ip string) *http.Request {
	ctx := context.WithValue(req.Context(), auth.IsAPITokenContextKey, true)
	ctx = context.WithValue(ctx, auth.AuditTokenIDContextKey, tokenID)
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
	handler.denialMu.Lock()
	for _, state := range handler.denials {
		state.firstSeen = state.firstSeen.Add(-2 * denialCoalesceWindow)
	}
	handler.denialMu.Unlock()

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

// TestRecordDenialSummariesToleratesStoreFailure checks that a summary
// which cannot be written is logged and otherwise ignored, as every
// other failure on the denial path is.
func TestRecordDenialSummariesToleratesStoreFailure(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()

	store.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rbac/audit", nil)
	handler.recordDenialSummaries(req, []denialSummary{{
		key:        denialKeyFor("mallory", "audit.read", "Permission denied"),
		suppressed: 3,
	}})
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
