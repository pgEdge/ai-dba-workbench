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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// =============================================================================
// Helpers
// =============================================================================

// mustCreateTokenOwner creates the user that owns the tokens used by the
// tests in this file.
func mustCreateTokenOwner(t *testing.T, s *AuthStore, username string) {
	t.Helper()

	if err := s.CreateUser(username, "Str0ngPassphrase!", "note", "Bob",
		"bob@example.com"); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
}

// mustCreateToken creates a token for the named owner through the system
// actor and returns the raw token and its stored row.
func mustCreateToken(t *testing.T, s *AuthStore, owner,
	annotation string) (string, *StoredToken) {

	t.Helper()

	raw, token, err := s.CreateToken(owner, annotation, nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	return raw, token
}

// assertTokenSuccess checks the common success-event columns for a token
// event.
func assertTokenSuccess(t *testing.T, ev AuditEvent, action string,
	targetID int64, targetName string) {

	t.Helper()

	assertUserActor(t, ev)
	if ev.Action != action {
		t.Errorf("Expected action %q, got %q", action, ev.Action)
	}
	if ev.TargetType != "token" {
		t.Errorf("Expected target type token, got %q", ev.TargetType)
	}
	if ev.TargetID == nil || *ev.TargetID != targetID {
		t.Errorf("Expected target id %d, got %v", targetID, ev.TargetID)
	}
	if ev.TargetName != targetName {
		t.Errorf("Expected target name %q, got %q", targetName, ev.TargetName)
	}
	if ev.Outcome != OutcomeSuccess {
		t.Errorf("Expected outcome success, got %q", ev.Outcome)
	}
	if ev.Error != "" {
		t.Errorf("Expected no error text, got %q", ev.Error)
	}
	assertNoSecrets(t, ev)
}

// auditEventCount returns the total number of recorded audit events.
func auditEventCount(t *testing.T, s *AuthStore) int {
	t.Helper()

	_, total, err := s.ListAuditEvents(AuditFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}

	return total
}

// stringsOf converts a decoded JSON array into a slice of strings.
func stringsOf(t *testing.T, value any) []string {
	t.Helper()

	items, ok := value.([]any)
	if !ok {
		t.Fatalf("Expected a JSON array, got %T (%v)", value, value)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("Expected a string array element, got %T", item)
		}
		out = append(out, text)
	}

	return out
}

// beforeMap returns the "before" object of an event's details.
func beforeMap(t *testing.T, details map[string]any) map[string]any {
	t.Helper()

	before, ok := details["before"].(map[string]any)
	if !ok {
		t.Fatalf("Expected a before object, got %v", details["before"])
	}

	return before
}

// =============================================================================
// Creation
// =============================================================================

func TestAuditTokensCreate(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")

	expiry := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	as := store.AsActor(testActor())
	raw, token, err := as.CreateToken("bob", "deploy key", &expiry)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	if raw == "" || token == nil {
		t.Fatal("Expected a raw token and a stored token")
	}

	ev := lastAuditEvent(t, store)
	assertTokenSuccess(t, ev, "token.create", token.ID, "deploy key")

	details := auditDetails(t, ev)
	if details["owner"] != "bob" {
		t.Errorf("Expected owner bob, got %v", details["owner"])
	}
	if details["annotation"] != "deploy key" {
		t.Errorf("Expected annotation 'deploy key', got %v", details["annotation"])
	}
	expiresAt, ok := details["expires_at"].(string)
	if !ok {
		t.Fatalf("Expected an expires_at string, got %v", details["expires_at"])
	}
	parsed, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		t.Fatalf("Expected an RFC 3339 expires_at, got %q: %v", expiresAt, err)
	}
	if !parsed.Equal(expiry) {
		t.Errorf("Expected expires_at %v, got %v", expiry, parsed)
	}
}

// TestAuditTokensCreateNeverLeaksTheToken is the central security
// assertion of this file: neither the raw token nor its SHA-256 hash may
// appear anywhere in the recorded event.
func TestAuditTokensCreateNeverLeaksTheToken(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")

	as := store.AsActor(testActor())
	raw, token, err := as.CreateToken("bob", "deploy key", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])
	if hash != token.TokenHash {
		t.Fatalf("Expected the stored hash to match the raw token")
	}

	events, _, err := store.ListAuditEvents(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("Expected at least one audit event")
	}

	for _, ev := range events {
		row := strings.Join([]string{string(ev.Details), ev.TargetName,
			ev.Error, ev.ActorName}, "|")
		if strings.Contains(row, raw) {
			t.Errorf("Audit event %d leaks the raw token", ev.ID)
		}
		if strings.Contains(row, hash) {
			t.Errorf("Audit event %d leaks the token hash", ev.ID)
		}
		// Even a short prefix of the hash must not appear.
		if strings.Contains(row, hash[:8]) {
			t.Errorf("Audit event %d leaks a token hash prefix", ev.ID)
		}
		assertNoSecrets(t, ev)
	}
}

func TestAuditTokensCreateWithoutExpiry(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")

	as := store.AsActor(testActor())
	if _, _, err := as.CreateToken("bob", "", nil); err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	details := auditDetails(t, lastAuditEvent(t, store))
	if value, present := details["expires_at"]; !present || value != nil {
		t.Errorf("Expected a null expires_at, got %v (present %v)", value, present)
	}
	if details["annotation"] != "" {
		t.Errorf("Expected an empty annotation, got %v", details["annotation"])
	}
}

func TestAuditTokensCreateUnknownOwnerRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	_, _, err := as.CreateToken("nobody", "deploy key", nil)
	if err == nil {
		t.Fatal("Expected CreateToken for an unknown owner to fail")
	}
	if err.Error() != "user 'nobody' not found" {
		t.Errorf("Expected the existing not-found message, got %q", err)
	}

	ev := lastAuditEvent(t, store)
	assertUserActor(t, ev)
	if ev.Action != "token.create" {
		t.Errorf("Expected action token.create, got %q", ev.Action)
	}
	if ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
	if ev.Error != err.Error() {
		t.Errorf("Expected error text %q, got %q", err.Error(), ev.Error)
	}
	if ev.TargetID != nil {
		t.Errorf("Expected no target id, got %v", *ev.TargetID)
	}
}

// =============================================================================
// Deletion
// =============================================================================

func TestAuditTokensDeleteUserToken(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	_, token := mustCreateToken(t, store, "bob", "deploy key")

	if _, err := store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool,
		"Tool A", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
	if err := store.SetTokenConnectionScope(token.ID, []ScopedConnection{
		{ConnectionID: 7, AccessLevel: AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	if err := store.SetTokenMCPScopeByNames(token.ID, []string{"tool_a"}); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}
	if err := store.SetTokenAdminScope(token.ID, []string{"users.read"}); err != nil {
		t.Fatalf("SetTokenAdminScope failed: %v", err)
	}

	as := store.AsActor(testActor())
	if err := as.DeleteUserToken("bob", token.ID); err != nil {
		t.Fatalf("DeleteUserToken failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertTokenSuccess(t, ev, "token.delete", token.ID, "deploy key")

	before := beforeMap(t, auditDetails(t, ev))
	if before["owner"] != "bob" {
		t.Errorf("Expected before.owner bob, got %v", before["owner"])
	}
	if before["annotation"] != "deploy key" {
		t.Errorf("Expected before.annotation 'deploy key', got %v", before["annotation"])
	}
	if value, present := before["expires_at"]; !present || value != nil {
		t.Errorf("Expected a null before.expires_at, got %v", value)
	}
	connections, ok := before["connections"].([]any)
	if !ok || len(connections) != 1 {
		t.Fatalf("Expected one before connection scope, got %v", before["connections"])
	}
	conn, ok := connections[0].(map[string]any)
	if !ok || conn["connection_id"] != float64(7) ||
		conn["access_level"] != AccessLevelRead {
		t.Errorf("Expected connection 7/read, got %v", connections[0])
	}
	if tools := stringsOf(t, before["tools"]); len(tools) != 1 || tools[0] != "tool_a" {
		t.Errorf("Expected before.tools [tool_a], got %v", before["tools"])
	}
	if admin := stringsOf(t, before["admin"]); len(admin) != 1 ||
		admin[0] != "users.read" {
		t.Errorf("Expected before.admin [users.read], got %v", before["admin"])
	}
}

func TestAuditTokensDeleteUserTokenNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")

	as := store.AsActor(testActor())
	err := as.DeleteUserToken("bob", 9999)
	if err == nil {
		t.Fatal("Expected DeleteUserToken on a missing token to fail")
	}
	if err.Error() != "token not found or not owned by user" {
		t.Errorf("Expected the existing not-found message, got %q", err)
	}

	// The caller named a token, so the failed attempt is recorded
	// against that id even though no row matched it.
	ev := lastAuditEvent(t, store)
	assertUserActor(t, ev)
	if ev.Action != "token.delete" {
		t.Errorf("Expected action token.delete, got %q", ev.Action)
	}
	if ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
	if ev.TargetType != "token" {
		t.Errorf("Expected target type token, got %q", ev.TargetType)
	}
	if ev.TargetID == nil || *ev.TargetID != 9999 {
		t.Errorf("Expected target id 9999, got %v", ev.TargetID)
	}
	if ev.TargetName != "" {
		t.Errorf("Expected an empty target name, got %q", ev.TargetName)
	}
	if ev.Error != err.Error() {
		t.Errorf("Expected error text %q, got %q", err.Error(), ev.Error)
	}
}

func TestAuditTokensDeleteTokenByID(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	_, token := mustCreateToken(t, store, "bob", "by id")

	as := store.AsActor(testActor())
	if err := as.DeleteToken(strconv.FormatInt(token.ID, 10)); err != nil {
		t.Fatalf("DeleteToken failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertTokenSuccess(t, ev, "token.delete", token.ID, "by id")
}

func TestAuditTokensDeleteTokenByHashPrefix(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	_, token := mustCreateToken(t, store, "bob", "by prefix")

	as := store.AsActor(testActor())
	if err := as.DeleteToken(token.TokenHash[:12]); err != nil {
		t.Fatalf("DeleteToken failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertTokenSuccess(t, ev, "token.delete", token.ID, "by prefix")
	before := beforeMap(t, auditDetails(t, ev))
	if before["owner"] != "bob" {
		t.Errorf("Expected before.owner bob, got %v", before["owner"])
	}
}

func TestAuditTokensDeleteTokenNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	countBefore := auditEventCount(t, store)

	as := store.AsActor(testActor())
	err := as.DeleteToken("deadbeefdeadbeef")
	if err == nil {
		t.Fatal("Expected DeleteToken on a missing token to fail")
	}
	if err.Error() != "token not found" {
		t.Errorf("Expected the existing not-found message, got %q", err)
	}

	if got := auditEventCount(t, store); got != countBefore {
		t.Errorf("Expected no new audit event for an unmatched filter, got %d new",
			got-countBefore)
	}
}

// TestAuditTokensDeleteTokenByIDNotFound checks that a delete naming a
// token id that does not exist leaves a failure event behind. A
// hash-prefix identifier names no id and so stays silent, which
// TestAuditTokensDeleteTokenNotFound covers.
func TestAuditTokensDeleteTokenByIDNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")

	as := store.AsActor(testActor())
	err := as.DeleteToken("9999")
	if err == nil {
		t.Fatal("Expected DeleteToken on a missing token to fail")
	}
	if err.Error() != "token not found" {
		t.Errorf("Expected the existing not-found message, got %q", err)
	}

	ev := lastAuditEvent(t, store)
	assertUserActor(t, ev)
	if ev.Action != "token.delete" {
		t.Errorf("Expected action token.delete, got %q", ev.Action)
	}
	if ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
	if ev.TargetType != "token" {
		t.Errorf("Expected target type token, got %q", ev.TargetType)
	}
	if ev.TargetID == nil || *ev.TargetID != 9999 {
		t.Errorf("Expected target id 9999, got %v", ev.TargetID)
	}
	if ev.Error != "token not found" {
		t.Errorf("Expected error text %q, got %q", "token not found", ev.Error)
	}

	if _, firstBad, err := store.VerifyAuditChain(); err != nil || firstBad != 0 {
		t.Errorf("Chain should verify: firstBad=%d err=%v", firstBad, err)
	}
}

// =============================================================================
// Scope changes
// =============================================================================

func TestAuditTokensSetConnectionScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	_, token := mustCreateToken(t, store, "bob", "scoped")

	as := store.AsActor(testActor())
	if err := as.SetTokenConnectionScope(token.ID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	if err := as.SetTokenConnectionScope(token.ID, []ScopedConnection{
		{ConnectionID: 2, AccessLevel: AccessLevelReadWrite},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertTokenSuccess(t, ev, "token.scope.set_connections", token.ID, "scoped")

	details := auditDetails(t, ev)
	before, ok := details["before"].([]any)
	if !ok || len(before) != 1 {
		t.Fatalf("Expected one before entry, got %v", details["before"])
	}
	if entry, ok := before[0].(map[string]any); !ok ||
		entry["connection_id"] != float64(1) {
		t.Errorf("Expected before connection 1, got %v", before[0])
	}
	after, ok := details["after"].([]any)
	if !ok || len(after) != 1 {
		t.Fatalf("Expected one after entry, got %v", details["after"])
	}
	if entry, ok := after[0].(map[string]any); !ok ||
		entry["connection_id"] != float64(2) ||
		entry["access_level"] != AccessLevelReadWrite {
		t.Errorf("Expected after connection 2/read_write, got %v", after[0])
	}
}

func TestAuditTokensSetConnectionScopeEmptyClears(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	_, token := mustCreateToken(t, store, "bob", "scoped")

	as := store.AsActor(testActor())
	if err := as.SetTokenConnectionScope(token.ID, nil); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}

	details := auditDetails(t, lastAuditEvent(t, store))
	for _, key := range []string{"before", "after"} {
		entries, ok := details[key].([]any)
		if !ok || len(entries) != 0 {
			t.Errorf("Expected an empty %s array, got %v", key, details[key])
		}
	}
}

func TestAuditTokensSetMCPScopeByIDs(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	_, token := mustCreateToken(t, store, "bob", "scoped")

	privID, err := store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool,
		"Tool A", false)
	if err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}

	as := store.AsActor(testActor())
	if err := as.SetTokenMCPScope(token.ID, []int64{privID}); err != nil {
		t.Fatalf("SetTokenMCPScope failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertTokenSuccess(t, ev, "token.scope.set_tools", token.ID, "scoped")

	details := auditDetails(t, ev)
	before, ok := details["before"].([]any)
	if !ok || len(before) != 0 {
		t.Errorf("Expected an empty before list, got %v", details["before"])
	}
	after, ok := details["after"].([]any)
	if !ok || len(after) != 1 || after[0] != float64(privID) {
		t.Errorf("Expected after [%d], got %v", privID, details["after"])
	}
}

func TestAuditTokensSetMCPScopeByNames(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	_, token := mustCreateToken(t, store, "bob", "scoped")

	if _, err := store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool,
		"Tool A", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}

	as := store.AsActor(testActor())
	if err := as.SetTokenMCPScopeByNames(token.ID, []string{"tool_a"}); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertTokenSuccess(t, ev, "token.scope.set_tools", token.ID, "scoped")

	details := auditDetails(t, ev)
	if before := stringsOf(t, details["before"]); len(before) != 0 {
		t.Errorf("Expected an empty before list, got %v", before)
	}
	if after := stringsOf(t, details["after"]); len(after) != 1 ||
		after[0] != "tool_a" {
		t.Errorf("Expected after [tool_a], got %v", after)
	}
}

func TestAuditTokensSetMCPScopeByNamesWildcard(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	_, token := mustCreateToken(t, store, "bob", "scoped")

	as := store.AsActor(testActor())
	if err := as.SetTokenMCPScopeByNames(token.ID, []string{"*", "tool_a"}); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}

	details := auditDetails(t, lastAuditEvent(t, store))
	if after := stringsOf(t, details["after"]); len(after) != 1 || after[0] != "*" {
		t.Errorf("Expected after [*], got %v", after)
	}
}

func TestAuditTokensSetAdminScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	_, token := mustCreateToken(t, store, "bob", "scoped")

	as := store.AsActor(testActor())
	if err := as.SetTokenAdminScope(token.ID, []string{"users.read"}); err != nil {
		t.Fatalf("SetTokenAdminScope failed: %v", err)
	}
	if err := as.SetTokenAdminScope(token.ID, []string{"*", "users.read"}); err != nil {
		t.Fatalf("SetTokenAdminScope failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertTokenSuccess(t, ev, "token.scope.set_admin", token.ID, "scoped")

	details := auditDetails(t, ev)
	if before := stringsOf(t, details["before"]); len(before) != 1 ||
		before[0] != "users.read" {
		t.Errorf("Expected before [users.read], got %v", before)
	}
	if after := stringsOf(t, details["after"]); len(after) != 1 || after[0] != "*" {
		t.Errorf("Expected after [*], got %v", after)
	}
}

func TestAuditTokensClearScope(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	_, token := mustCreateToken(t, store, "bob", "scoped")

	if _, err := store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool,
		"Tool A", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
	if err := store.SetTokenConnectionScope(token.ID, []ScopedConnection{
		{ConnectionID: 3, AccessLevel: AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	if err := store.SetTokenMCPScopeByNames(token.ID, []string{"tool_a"}); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}
	if err := store.SetTokenAdminScope(token.ID, []string{"users.read"}); err != nil {
		t.Fatalf("SetTokenAdminScope failed: %v", err)
	}

	as := store.AsActor(testActor())
	if err := as.ClearTokenScope(token.ID); err != nil {
		t.Fatalf("ClearTokenScope failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertTokenSuccess(t, ev, "token.scope.clear", token.ID, "scoped")

	before := beforeMap(t, auditDetails(t, ev))
	if connections, ok := before["connections"].([]any); !ok || len(connections) != 1 {
		t.Errorf("Expected one before connection, got %v", before["connections"])
	}
	if tools := stringsOf(t, before["tools"]); len(tools) != 1 || tools[0] != "tool_a" {
		t.Errorf("Expected before.tools [tool_a], got %v", tools)
	}
	if admin := stringsOf(t, before["admin"]); len(admin) != 1 ||
		admin[0] != "users.read" {
		t.Errorf("Expected before.admin [users.read], got %v", admin)
	}

	scope, err := store.GetTokenScope(token.ID)
	if err != nil {
		t.Fatalf("GetTokenScope failed: %v", err)
	}
	if scope != nil {
		t.Errorf("Expected the scope to be cleared, got %v", scope)
	}
}

// TestAuditTokensScopeOnMissingTokenStillRecords checks that setting an
// empty scope on a token that does not exist still succeeds, as it
// always has, and records an event with no target name.
func TestAuditTokensScopeOnMissingTokenStillRecords(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.ClearTokenScope(4242); err != nil {
		t.Fatalf("ClearTokenScope failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertTokenSuccess(t, ev, "token.scope.clear", 4242, "")
}

// =============================================================================
// Actor attribution and chain integrity
// =============================================================================

func TestAuditTokensSystemActor(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	if _, _, err := store.CreateToken("bob", "cli token", nil); err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	if ev.ActorType != ActorSystem {
		t.Errorf("Expected actor type system, got %q", ev.ActorType)
	}
	if ev.ActorName != "system" {
		t.Errorf("Expected actor name system, got %q", ev.ActorName)
	}
	if ev.Action != "token.create" {
		t.Errorf("Expected action token.create, got %q", ev.Action)
	}
}

func TestAuditTokensChainStaysIntact(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")

	as := store.AsActor(testActor())
	_, token, err := as.CreateToken("bob", "chained", nil)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	if err := as.SetTokenConnectionScope(token.ID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
	if err := as.SetTokenAdminScope(token.ID, []string{"users.read"}); err != nil {
		t.Fatalf("SetTokenAdminScope failed: %v", err)
	}
	if err := as.ClearTokenScope(token.ID); err != nil {
		t.Fatalf("ClearTokenScope failed: %v", err)
	}
	if err := as.DeleteUserToken("bob", token.ID); err != nil {
		t.Fatalf("DeleteUserToken failed: %v", err)
	}

	count, firstBad, err := store.VerifyAuditChain()
	if err != nil {
		t.Fatalf("VerifyAuditChain failed at row %d: %v", firstBad, err)
	}
	if count < 5 {
		t.Errorf("Expected at least 5 audit rows, got %d", count)
	}
}

// =============================================================================
// Lockout
// =============================================================================

// createLockoutStore builds a store that locks an account after a single
// failed authentication attempt.
func createLockoutStore(t *testing.T) (*AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "auth-audit-lockout-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store, err := NewAuthStore(tmpDir, 0, 1)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create auth store: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	return store, func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}
}

func TestAuditTokensLockoutRecordsDisable(t *testing.T) {
	store, cleanup := createLockoutStore(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")

	if _, _, err := store.AuthenticateUser("bob", "WrongPassphrase!"); err == nil {
		t.Fatal("Expected authentication with the wrong password to fail")
	}

	ev := lastAuditEvent(t, store)
	if ev.Action != "user.disable" {
		t.Fatalf("Expected action user.disable, got %q", ev.Action)
	}
	if ev.ActorType != ActorSystem || ev.ActorName != "system" {
		t.Errorf("Expected the system actor, got %q/%q", ev.ActorType, ev.ActorName)
	}
	if ev.TargetType != "user" || ev.TargetName != "bob" {
		t.Errorf("Expected target user/bob, got %q/%q", ev.TargetType, ev.TargetName)
	}
	if ev.Outcome != OutcomeSuccess {
		t.Errorf("Expected outcome success, got %q", ev.Outcome)
	}

	details := auditDetails(t, ev)
	if details["reason"] != "lockout" {
		t.Errorf("Expected reason lockout, got %v", details["reason"])
	}
	if got := snapshotField(t, details, "before", "enabled"); got != true {
		t.Errorf("Expected before.enabled true, got %v", got)
	}
	if got := snapshotField(t, details, "after", "enabled"); got != false {
		t.Errorf("Expected after.enabled false, got %v", got)
	}
	assertNoSecrets(t, ev)

	user, err := store.GetUser("bob")
	if err != nil || user == nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if user.Enabled {
		t.Error("Expected the locked-out user to be disabled")
	}
}

func TestAuditTokensFailedLoginWithoutLockoutRecordsNothing(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	countBefore := auditEventCount(t, store)

	if _, _, err := store.AuthenticateUser("bob", "WrongPassphrase!"); err == nil {
		t.Fatal("Expected authentication with the wrong password to fail")
	}

	if got := auditEventCount(t, store); got != countBefore {
		t.Errorf("Expected no audit event when lockout is disabled, got %d new",
			got-countBefore)
	}
}

// =============================================================================
// Error paths
// =============================================================================

func TestAuditTokensClosedStoreFailsToBegin(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	_, token := mustCreateToken(t, store, "bob", "closing")
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	as := store.AsActor(testActor())
	cases := []struct {
		name string
		call func() error
	}{
		{"CreateToken", func() error {
			_, _, err := as.CreateToken("bob", "x", nil)
			return err
		}},
		{"DeleteUserToken", func() error {
			return as.DeleteUserToken("bob", token.ID)
		}},
		{"DeleteToken", func() error { return as.DeleteToken("1") }},
		{"SetTokenConnectionScope", func() error {
			return as.SetTokenConnectionScope(token.ID, nil)
		}},
		{"SetTokenMCPScope", func() error {
			return as.SetTokenMCPScope(token.ID, nil)
		}},
		{"SetTokenMCPScopeByNames", func() error {
			return as.SetTokenMCPScopeByNames(token.ID, nil)
		}},
		{"SetTokenAdminScope", func() error {
			return as.SetTokenAdminScope(token.ID, nil)
		}},
		{"ClearTokenScope", func() error { return as.ClearTokenScope(token.ID) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Errorf("Expected %s to fail on a closed store", tc.name)
			}
		})
	}
}

// TestAuditTokensBrokenScopeTablesRecordFailure drops each scope table in
// turn so that the mutation fails after the target is known, and checks
// that the failure is recorded against that token.
func TestAuditTokensBrokenScopeTablesRecordFailure(t *testing.T) {
	cases := []struct {
		name   string
		table  string
		action string
		call   func(as *ActorStore, tokenID int64) error
	}{
		{"connections", "token_connection_scope", "token.scope.set_connections",
			func(as *ActorStore, id int64) error {
				return as.SetTokenConnectionScope(id, nil)
			}},
		{"tools", "token_mcp_scope", "token.scope.set_tools",
			func(as *ActorStore, id int64) error {
				return as.SetTokenMCPScope(id, nil)
			}},
		{"tools_by_name", "token_mcp_scope", "token.scope.set_tools",
			func(as *ActorStore, id int64) error {
				return as.SetTokenMCPScopeByNames(id, nil)
			}},
		{"admin", "token_admin_scope", "token.scope.set_admin",
			func(as *ActorStore, id int64) error {
				return as.SetTokenAdminScope(id, nil)
			}},
		{"clear", "token_connection_scope", "token.scope.clear",
			func(as *ActorStore, id int64) error {
				return as.ClearTokenScope(id)
			}},
		{"delete", "token_admin_scope", "token.delete",
			func(as *ActorStore, id int64) error {
				return as.DeleteUserToken("bob", id)
			}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			mustCreateTokenOwner(t, store, "bob")
			_, token := mustCreateToken(t, store, "bob", "broken")
			mustExec(t, store, "DROP TABLE "+tc.table)

			as := store.AsActor(testActor())
			if err := tc.call(as, token.ID); err == nil {
				t.Fatalf("Expected %s to fail with %s dropped", tc.name, tc.table)
			}

			ev := lastAuditEvent(t, store)
			if ev.Action != tc.action {
				t.Errorf("Expected action %q, got %q", tc.action, ev.Action)
			}
			if ev.Outcome != OutcomeFailure {
				t.Errorf("Expected outcome failure, got %q", ev.Outcome)
			}
			if ev.TargetID == nil || *ev.TargetID != token.ID {
				t.Errorf("Expected target id %d, got %v", token.ID, ev.TargetID)
			}
			if ev.Error == "" {
				t.Error("Expected error text on a failure event")
			}
		})
	}
}

// TestAuditTokensCreateInsertFailureRecordsFailure drops the tokens table
// so that the insert fails after the owner is known.
func TestAuditTokensCreateInsertFailureRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "bob")
	mustExec(t, store, "DROP TABLE tokens")

	as := store.AsActor(testActor())
	if _, _, err := as.CreateToken("bob", "doomed", nil); err == nil {
		t.Fatal("Expected CreateToken to fail with the tokens table dropped")
	}

	ev := lastAuditEvent(t, store)
	if ev.Action != "token.create" || ev.Outcome != OutcomeFailure {
		t.Errorf("Expected a failed token.create, got %q/%q", ev.Action, ev.Outcome)
	}
	if ev.TargetName != "doomed" {
		t.Errorf("Expected target name doomed, got %q", ev.TargetName)
	}
}

// TestAuditTokensCreateUserLookupFailure drops the users table so that
// the owner lookup fails for a reason other than "not found".
func TestAuditTokensCreateUserLookupFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustExec(t, store, "DROP TABLE users")

	if _, _, err := store.AsActor(testActor()).CreateToken("bob", "x",
		nil); err == nil {
		t.Fatal("Expected CreateToken to fail with the users table dropped")
	} else if !strings.Contains(err.Error(), "failed to get user") {
		t.Errorf("Expected a lookup failure, got %q", err)
	}
}

// TestAuditTokensDeleteFailurePaths exercises the failure branches of
// deleteTokensByFilter that sit after the matching tokens are known.
func TestAuditTokensDeleteFailurePaths(t *testing.T) {
	cases := []struct {
		name    string
		break_  string
		wantErr string
	}{
		{"query", "DROP TABLE tokens", "failed to query tokens"},
		{"snapshot", "UPDATE tokens SET expires_at = 'not-a-timestamp'",
			"failed to read token"},
		{"sessions", "DROP TABLE connection_sessions",
			"failed to delete connection session"},
		{"delete", `CREATE TRIGGER block_token_delete BEFORE DELETE ON tokens
             BEGIN SELECT RAISE(ABORT, 'blocked'); END`,
			"failed to delete tokens"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			mustCreateTokenOwner(t, store, "bob")
			_, token := mustCreateToken(t, store, "bob", "doomed")
			mustExec(t, store, tc.break_)

			err := store.AsActor(testActor()).DeleteUserToken("bob", token.ID)
			if err == nil {
				t.Fatalf("Expected DeleteUserToken to fail after %q", tc.break_)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Expected an error containing %q, got %q", tc.wantErr, err)
			}
		})
	}
}

// TestAuditTokensScopeScanFailures writes rows whose columns cannot be
// scanned into their Go types, so that the scope readers fail on scan.
func TestAuditTokensScopeScanFailures(t *testing.T) {
	cases := []struct {
		name    string
		insert  string
		call    func(as *ActorStore, tokenID int64) error
		wantErr string
	}{
		{"connections",
			`INSERT INTO token_connection_scope (token_id, connection_id, access_level)
             VALUES (%d, 'not-a-number', 'read')`,
			func(as *ActorStore, id int64) error {
				return as.SetTokenConnectionScope(id, nil)
			},
			"failed to scan connection scope"},
		{"tools",
			`INSERT INTO token_mcp_scope (token_id, privilege_identifier_id)
             VALUES (%d, 'not-a-number')`,
			func(as *ActorStore, id int64) error {
				return as.SetTokenMCPScope(id, nil)
			},
			"failed to scan privilege ID"},
		{"admin",
			`INSERT INTO token_admin_scope (token_id, permission)
             VALUES (%d, NULL)`,
			func(as *ActorStore, id int64) error {
				return as.SetTokenAdminScope(id, nil)
			},
			"failed to scan admin permission"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			mustCreateTokenOwner(t, store, "bob")
			_, token := mustCreateToken(t, store, "bob", "junk")
			if tc.name == "admin" {
				// token_admin_scope.permission is NOT NULL, so relax it
				// to reach the scan-failure branch of the reader.
				mustExec(t, store, "DROP TABLE token_admin_scope")
				mustExec(t, store, `CREATE TABLE token_admin_scope (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    token_id INTEGER NOT NULL,
                    permission TEXT,
                    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                    UNIQUE(token_id, permission)
                )`)
			}
			mustExec(t, store, fmt.Sprintf(tc.insert, token.ID))

			err := tc.call(store.AsActor(testActor()), token.ID)
			if err == nil {
				t.Fatal("Expected the scope read to fail on an unscannable row")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Expected an error containing %q, got %q", tc.wantErr, err)
			}

			ev := lastAuditEvent(t, store)
			if ev.Outcome != OutcomeFailure {
				t.Errorf("Expected outcome failure, got %q", ev.Outcome)
			}
		})
	}
}

// TestAuditTokensAuditTableMissing drops audit_events so that recording
// the event fails, and checks that every token mutation reports the
// failure rather than committing a change with no audit trail.
func TestAuditTokensAuditTableMissing(t *testing.T) {
	cases := []struct {
		name string
		call func(as *ActorStore, tokenID int64) error
	}{
		{"CreateToken", func(as *ActorStore, _ int64) error {
			_, _, err := as.CreateToken("bob", "second", nil)
			return err
		}},
		{"DeleteUserToken", func(as *ActorStore, id int64) error {
			return as.DeleteUserToken("bob", id)
		}},
		{"SetTokenConnectionScope", func(as *ActorStore, id int64) error {
			return as.SetTokenConnectionScope(id, nil)
		}},
		{"SetTokenMCPScope", func(as *ActorStore, id int64) error {
			return as.SetTokenMCPScope(id, nil)
		}},
		{"SetTokenMCPScopeByNames", func(as *ActorStore, id int64) error {
			return as.SetTokenMCPScopeByNames(id, nil)
		}},
		{"SetTokenAdminScope", func(as *ActorStore, id int64) error {
			return as.SetTokenAdminScope(id, nil)
		}},
		{"ClearTokenScope", func(as *ActorStore, id int64) error {
			return as.ClearTokenScope(id)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			mustCreateTokenOwner(t, store, "bob")
			_, token := mustCreateToken(t, store, "bob", "no audit")
			mustExec(t, store, "DROP TRIGGER audit_events_no_update")
			mustExec(t, store, "DROP TABLE audit_events")

			if err := tc.call(store.AsActor(testActor()), token.ID); err == nil {
				t.Errorf("Expected %s to fail with no audit table", tc.name)
			}
		})
	}
}

// TestAuditTokensLockoutHelperErrorPaths drives the lockout helper
// directly, because the failures it swallows cannot be reached through
// AuthenticateUser. The helper is best effort, so each case must return
// quietly rather than panic, and must leave no audit row behind.
func TestAuditTokensLockoutHelperErrorPaths(t *testing.T) {
	t.Run("MissingUser", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		before := auditEventCount(t, store)
		store.mu.Lock()
		store.disableForLockout(4242, "nobody")
		store.mu.Unlock()

		if got := auditEventCount(t, store); got != before {
			t.Errorf("Expected no audit event, got %d new", got-before)
		}
	})

	t.Run("BlockedUpdate", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		mustCreateTokenOwner(t, store, "bob")
		mustExec(t, store, `CREATE TRIGGER block_user_update BEFORE UPDATE ON users
            BEGIN SELECT RAISE(ABORT, 'blocked'); END`)

		user, err := store.GetUser("bob")
		if err != nil || user == nil {
			t.Fatalf("GetUser failed: %v", err)
		}
		before := auditEventCount(t, store)
		store.mu.Lock()
		store.disableForLockout(user.ID, "bob")
		store.mu.Unlock()

		if got := auditEventCount(t, store); got != before {
			t.Errorf("Expected no audit event, got %d new", got-before)
		}
	})

	// A failing audit write must not undo the lockout: this is the one
	// audited change the server makes to defend itself, so it fails
	// open on the audit and closed on the account.
	t.Run("NoAuditTable", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		mustCreateTokenOwner(t, store, "bob")
		user, err := store.GetUser("bob")
		if err != nil || user == nil {
			t.Fatalf("GetUser failed: %v", err)
		}
		mustExec(t, store, "DROP TRIGGER audit_events_no_update")
		mustExec(t, store, "DROP TABLE audit_events")

		store.mu.Lock()
		locked := store.disableForLockout(user.ID, "bob")
		store.mu.Unlock()

		if !locked {
			t.Error("Expected the lockout to be reported as applied")
		}

		user, err = store.GetUser("bob")
		if err != nil || user == nil {
			t.Fatalf("GetUser failed: %v", err)
		}
		if user.Enabled {
			t.Error("Expected the account to stay locked when the audit " +
				"write fails; a failed audit must not hand an attacker a " +
				"working account")
		}
	})

	t.Run("ClosedStore", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		mustCreateTokenOwner(t, store, "bob")
		if err := store.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		store.mu.Lock()
		store.disableForLockout(1, "bob")
		store.mu.Unlock()
	})
}

// TestAuditTokensScopeMutationFailures makes each statement of each
// scope mutation fail in turn, and checks that the error is unchanged
// and that a failure event names the token.
func TestAuditTokensScopeMutationFailures(t *testing.T) {
	blockDelete := func(table string) string {
		return `CREATE TRIGGER block_delete BEFORE DELETE ON ` + table +
			` BEGIN SELECT RAISE(ABORT, 'blocked'); END`
	}
	blockInsert := func(table string) string {
		return `CREATE TRIGGER block_insert BEFORE INSERT ON ` + table +
			` BEGIN SELECT RAISE(ABORT, 'blocked'); END`
	}

	cases := []struct {
		name    string
		seed    func(t *testing.T, s *AuthStore, tokenID int64)
		breaker string
		call    func(as *ActorStore, tokenID int64) error
		wantErr string
	}{
		{"ConnectionsClear", seedConnectionScope,
			blockDelete("token_connection_scope"),
			func(as *ActorStore, id int64) error {
				return as.SetTokenConnectionScope(id, nil)
			},
			"failed to clear token connection scope"},
		{"ConnectionsInsert", nil, "",
			func(as *ActorStore, id int64) error {
				return as.SetTokenConnectionScope(id, []ScopedConnection{
					{ConnectionID: 1, AccessLevel: AccessLevelRead},
					{ConnectionID: 1, AccessLevel: AccessLevelRead},
				})
			},
			"failed to add connection to token scope"},
		{"ToolsClear", seedMCPScope, blockDelete("token_mcp_scope"),
			func(as *ActorStore, id int64) error {
				return as.SetTokenMCPScope(id, nil)
			},
			"failed to clear token MCP scope"},
		{"ToolsInsert", nil, "",
			func(as *ActorStore, id int64) error {
				return as.SetTokenMCPScope(id, []int64{5, 5})
			},
			"failed to add privilege to token scope"},
		{"ToolsByNameClear", seedMCPScope, blockDelete("token_mcp_scope"),
			func(as *ActorStore, id int64) error {
				return as.SetTokenMCPScopeByNames(id, nil)
			},
			"failed to clear token MCP scope"},
		{"ToolsByNameInsert", nil, blockInsert("token_mcp_scope"),
			func(as *ActorStore, id int64) error {
				return as.SetTokenMCPScopeByNames(id, []string{"tool_a"})
			},
			"failed to add privilege to token scope"},
		{"ToolsByNameWildcardInsert", nil, blockInsert("token_mcp_scope"),
			func(as *ActorStore, id int64) error {
				return as.SetTokenMCPScopeByNames(id, []string{"*"})
			},
			"failed to add wildcard privilege to token scope"},
		{"AdminClear", seedAdminScope, blockDelete("token_admin_scope"),
			func(as *ActorStore, id int64) error {
				return as.SetTokenAdminScope(id, nil)
			},
			"failed to clear admin scope"},
		{"AdminInsert", nil, "",
			func(as *ActorStore, id int64) error {
				return as.SetTokenAdminScope(id, []string{"users.read", "users.read"})
			},
			"failed to add admin permission users.read to token scope"},
		{"AdminWildcardInsert", nil, blockInsert("token_admin_scope"),
			func(as *ActorStore, id int64) error {
				return as.SetTokenAdminScope(id, []string{"*"})
			},
			"failed to add wildcard admin permission to token scope"},
		{"ClearConnections", seedConnectionScope,
			blockDelete("token_connection_scope"),
			func(as *ActorStore, id int64) error { return as.ClearTokenScope(id) },
			"failed to clear token connection scope"},
		{"ClearTools", seedMCPScope, blockDelete("token_mcp_scope"),
			func(as *ActorStore, id int64) error { return as.ClearTokenScope(id) },
			"failed to clear token MCP scope"},
		{"ClearAdmin", seedAdminScope, blockDelete("token_admin_scope"),
			func(as *ActorStore, id int64) error { return as.ClearTokenScope(id) },
			"failed to clear token admin scope"},
		{"DeleteScopeRows", seedConnectionScope,
			blockDelete("token_connection_scope"),
			func(as *ActorStore, id int64) error {
				return as.DeleteUserToken("bob", id)
			},
			"failed to delete token_connection_scope rows"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			mustCreateTokenOwner(t, store, "bob")
			_, token := mustCreateToken(t, store, "bob", "failing")
			if _, err := store.RegisterMCPPrivilege("tool_a", MCPPrivilegeTypeTool,
				"Tool A", false); err != nil {
				t.Fatalf("RegisterMCPPrivilege failed: %v", err)
			}
			if tc.seed != nil {
				tc.seed(t, store, token.ID)
			}
			if tc.breaker != "" {
				mustExec(t, store, tc.breaker)
			}

			err := tc.call(store.AsActor(testActor()), token.ID)
			if err == nil {
				t.Fatalf("Expected %s to fail", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Expected an error containing %q, got %q", tc.wantErr, err)
			}

			ev := lastAuditEvent(t, store)
			if ev.Outcome != OutcomeFailure {
				t.Errorf("Expected outcome failure, got %q", ev.Outcome)
			}
			if ev.TargetID == nil || *ev.TargetID != token.ID {
				t.Errorf("Expected target id %d, got %v", token.ID, ev.TargetID)
			}
		})
	}
}

// seedConnectionScope gives a token one connection scope row.
func seedConnectionScope(t *testing.T, s *AuthStore, tokenID int64) {
	t.Helper()

	if err := s.SetTokenConnectionScope(tokenID, []ScopedConnection{
		{ConnectionID: 1, AccessLevel: AccessLevelRead},
	}); err != nil {
		t.Fatalf("SetTokenConnectionScope failed: %v", err)
	}
}

// seedMCPScope gives a token one MCP privilege scope row.
func seedMCPScope(t *testing.T, s *AuthStore, tokenID int64) {
	t.Helper()

	if err := s.SetTokenMCPScopeByNames(tokenID, []string{"tool_a"}); err != nil {
		t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
	}
}

// seedAdminScope gives a token one admin permission scope row.
func seedAdminScope(t *testing.T, s *AuthStore, tokenID int64) {
	t.Helper()

	if err := s.SetTokenAdminScope(tokenID, []string{"users.read"}); err != nil {
		t.Fatalf("SetTokenAdminScope failed: %v", err)
	}
}

// TestUnlinkFederatedIdentityFailsWhenRevocationCannotBeAudited checks
// that the token revocation an unlink performs is fail-closed like every
// other audited token deletion: when the token.delete events cannot be
// written, the unlink reports the failure rather than revoking silently.
func TestUnlinkFederatedIdentityFailsWhenRevocationCannotBeAudited(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateTokenOwner(t, store, "leaver")
	if _, err := store.LinkFederatedIdentity("leaver", "https://idp.example.com",
		"subject-1", false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	mustCreateToken(t, store, "leaver", "to be revoked")
	mustExec(t, store, "DROP TRIGGER audit_events_no_update")
	mustExec(t, store, "DROP TABLE audit_events")

	if _, _, err := store.UnlinkFederatedIdentity("leaver", false); err == nil {
		t.Fatal("Expected the unlink to fail when its token revocation cannot be audited")
	}
}
