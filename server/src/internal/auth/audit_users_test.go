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
	"encoding/json"
	"strings"
	"testing"
)

// testActor is the acting principal used by the audited-mutation tests.
// Later tasks reuse it, so keep its fields stable.
func testActor() Actor {
	return Actor{
		Type: ActorUser,
		ID:   int64Ptr(1),
		Name: "alice",
		IP:   "192.0.2.1",
	}
}

// lastAuditEvent returns the most recently recorded audit event, failing
// the test when the log is empty.
func lastAuditEvent(t *testing.T, s *AuthStore) AuditEvent {
	t.Helper()

	events, _, err := s.ListAuditEvents(AuditFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("No audit events recorded")
	}

	return events[0]
}

// auditDetails decodes an event's details column into a generic map.
func auditDetails(t *testing.T, ev AuditEvent) map[string]any {
	t.Helper()

	if len(ev.Details) == 0 {
		t.Fatalf("Audit event %d has no details", ev.ID)
	}
	var out map[string]any
	if err := json.Unmarshal(ev.Details, &out); err != nil {
		t.Fatalf("Failed to decode audit details %q: %v", string(ev.Details), err)
	}

	return out
}

// assertNoSecrets fails when an event's details mention anything that
// looks like a password or a hash.
func assertNoSecrets(t *testing.T, ev AuditEvent) {
	t.Helper()

	raw := strings.ToLower(string(ev.Details))
	for _, needle := range []string{"password_hash", "password\"", "$2a$", "$2b$", "hash\""} {
		if strings.Contains(raw, needle) {
			t.Errorf("Audit details leak %q: %s", needle, string(ev.Details))
		}
	}
}

// assertUserActor checks the actor columns against testActor.
func assertUserActor(t *testing.T, ev AuditEvent) {
	t.Helper()

	if ev.ActorType != ActorUser {
		t.Errorf("Expected actor type %q, got %q", ActorUser, ev.ActorType)
	}
	if ev.ActorID == nil || *ev.ActorID != 1 {
		t.Errorf("Expected actor id 1, got %v", ev.ActorID)
	}
	if ev.ActorName != "alice" {
		t.Errorf("Expected actor name alice, got %q", ev.ActorName)
	}
	if ev.ActorIP != "192.0.2.1" {
		t.Errorf("Expected actor IP 192.0.2.1, got %q", ev.ActorIP)
	}
}

// assertSuccessOn checks the common success-event columns.
func assertSuccessOn(t *testing.T, ev AuditEvent, action, targetName string) {
	t.Helper()

	assertUserActor(t, ev)
	if ev.Action != action {
		t.Errorf("Expected action %q, got %q", action, ev.Action)
	}
	if ev.TargetType != "user" {
		t.Errorf("Expected target type user, got %q", ev.TargetType)
	}
	if ev.TargetName != targetName {
		t.Errorf("Expected target name %q, got %q", targetName, ev.TargetName)
	}
	if ev.TargetID == nil {
		t.Errorf("Expected a target id, got nil")
	}
	if ev.Outcome != OutcomeSuccess {
		t.Errorf("Expected outcome success, got %q", ev.Outcome)
	}
	if ev.Error != "" {
		t.Errorf("Expected no error text, got %q", ev.Error)
	}
	assertNoSecrets(t, ev)
}

// snapshotField reads a string field out of a before/after snapshot.
func snapshotField(t *testing.T, details map[string]any, key, field string) any {
	t.Helper()

	section, ok := details[key].(map[string]any)
	if !ok {
		t.Fatalf("Expected %q object in details, got %v", key, details[key])
	}
	value, ok := section[field]
	if !ok {
		t.Fatalf("Expected field %q in %q, got %v", field, key, section)
	}

	return value
}

// =============================================================================
// Creation
// =============================================================================

func TestAuditUsersCreateUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "note", "Bob Example",
		"bob@example.com"); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertSuccessOn(t, ev, "user.create", "bob")

	details := auditDetails(t, ev)
	if got := snapshotField(t, details, "after", "username"); got != "bob" {
		t.Errorf("Expected after.username bob, got %v", got)
	}
	if got := snapshotField(t, details, "after", "email"); got != "bob@example.com" {
		t.Errorf("Expected after.email bob@example.com, got %v", got)
	}
	if _, ok := details["before"]; ok {
		t.Errorf("Expected no before snapshot on create, got %v", details["before"])
	}
}

func TestAuditUsersCreateServiceAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateServiceAccount("svc", "note", "Service", "svc@example.com"); err != nil {
		t.Fatalf("CreateServiceAccount failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertSuccessOn(t, ev, "service_account.create", "svc")

	details := auditDetails(t, ev)
	if got := snapshotField(t, details, "after", "is_service_account"); got != true {
		t.Errorf("Expected after.is_service_account true, got %v", got)
	}
}

func TestAuditUsersCreateUserDuplicateRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "", "", ""); err == nil {
		t.Fatal("Expected duplicate CreateUser to fail")
	}

	ev := lastAuditEvent(t, store)
	if ev.Action != "user.create" {
		t.Errorf("Expected action user.create, got %q", ev.Action)
	}
	if ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
	if ev.TargetName != "bob" {
		t.Errorf("Expected target name bob, got %q", ev.TargetName)
	}
	if ev.Error == "" {
		t.Error("Expected error text on a failure event")
	}
}

// =============================================================================
// Update
// =============================================================================

func TestAuditUsersUpdateUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "old", "Old Name",
		"old@example.com"); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if err := as.UpdateUser("bob", "An0therPassphrase!", "new", "New Name",
		"new@example.com"); err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertSuccessOn(t, ev, "user.update", "bob")

	details := auditDetails(t, ev)
	if got := snapshotField(t, details, "before", "email"); got != "old@example.com" {
		t.Errorf("Expected before.email old@example.com, got %v", got)
	}
	if got := snapshotField(t, details, "after", "email"); got != "new@example.com" {
		t.Errorf("Expected after.email new@example.com, got %v", got)
	}
	if got := snapshotField(t, details, "after", "display_name"); got != "New Name" {
		t.Errorf("Expected after.display_name New Name, got %v", got)
	}
	if details["password_changed"] != true {
		t.Errorf("Expected password_changed true, got %v", details["password_changed"])
	}
}

func TestAuditUsersUpdateUserWithoutPassword(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "old", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := as.UpdateUser("bob", "", "new", "", ""); err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}

	details := auditDetails(t, lastAuditEvent(t, store))
	if details["password_changed"] != false {
		t.Errorf("Expected password_changed false, got %v", details["password_changed"])
	}
}

func TestAuditUsersUpdateUserNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.UpdateUser("nobody", "", "new", "", ""); err == nil {
		t.Fatal("Expected UpdateUser on a missing user to fail")
	}

	ev := lastAuditEvent(t, store)
	if ev.Action != "user.update" || ev.Outcome != OutcomeFailure {
		t.Errorf("Expected a failed user.update event, got %q/%q", ev.Action, ev.Outcome)
	}
	if ev.TargetID != nil {
		t.Errorf("Expected no target id, got %v", *ev.TargetID)
	}
	if ev.TargetName != "nobody" {
		t.Errorf("Expected target name nobody, got %q", ev.TargetName)
	}
}

func TestAuditUsersUpdateUserAtomic(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "old", "Old", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	password := "An0therPassphrase!"
	annotation := "new"
	enabled := false
	superuser := true
	if err := as.UpdateUserAtomic("bob", UserUpdate{
		Password:    &password,
		Annotation:  &annotation,
		Enabled:     &enabled,
		IsSuperuser: &superuser,
	}); err != nil {
		t.Fatalf("UpdateUserAtomic failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertSuccessOn(t, ev, "user.update", "bob")

	details := auditDetails(t, ev)
	if details["password_changed"] != true {
		t.Errorf("Expected password_changed true, got %v", details["password_changed"])
	}
	if got := snapshotField(t, details, "before", "is_superuser"); got != false {
		t.Errorf("Expected before.is_superuser false, got %v", got)
	}
	if got := snapshotField(t, details, "after", "is_superuser"); got != true {
		t.Errorf("Expected after.is_superuser true, got %v", got)
	}
	if got := snapshotField(t, details, "after", "enabled"); got != false {
		t.Errorf("Expected after.enabled false, got %v", got)
	}
	if got := snapshotField(t, details, "after", "annotation"); got != "new" {
		t.Errorf("Expected after.annotation new, got %v", got)
	}
}

func TestAuditUsersUpdateUserAtomicNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	annotation := "new"
	as := store.AsActor(testActor())
	if err := as.UpdateUserAtomic("nobody", UserUpdate{Annotation: &annotation}); err == nil {
		t.Fatal("Expected UpdateUserAtomic on a missing user to fail")
	}

	ev := lastAuditEvent(t, store)
	if ev.Outcome != OutcomeFailure || ev.Action != "user.update" {
		t.Errorf("Expected a failed user.update event, got %q/%q", ev.Action, ev.Outcome)
	}
}

func TestAuditUsersUpdateUserAtomicRejectsWeakPassword(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	weak := "short"
	if err := as.UpdateUserAtomic("bob", UserUpdate{Password: &weak}); err == nil {
		t.Fatal("Expected a weak password to be rejected")
	}

	ev := lastAuditEvent(t, store)
	if ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
}

func TestAuditUsersUpdateUserDisplayName(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "", "Old", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := as.UpdateUserDisplayName("bob", "New"); err != nil {
		t.Fatalf("UpdateUserDisplayName failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertSuccessOn(t, ev, "user.update", "bob")

	details := auditDetails(t, ev)
	if got := snapshotField(t, details, "before", "display_name"); got != "Old" {
		t.Errorf("Expected before.display_name Old, got %v", got)
	}
	if got := snapshotField(t, details, "after", "display_name"); got != "New" {
		t.Errorf("Expected after.display_name New, got %v", got)
	}
	if details["password_changed"] != false {
		t.Errorf("Expected password_changed false, got %v", details["password_changed"])
	}
}

func TestAuditUsersUpdateUserDisplayNameNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.UpdateUserDisplayName("nobody", "New"); err == nil {
		t.Fatal("Expected UpdateUserDisplayName on a missing user to fail")
	}
	if ev := lastAuditEvent(t, store); ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
}

func TestAuditUsersUpdateUserEmail(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "", "", "old@example.com"); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := as.UpdateUserEmail("bob", "new@example.com"); err != nil {
		t.Fatalf("UpdateUserEmail failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertSuccessOn(t, ev, "user.update", "bob")

	details := auditDetails(t, ev)
	if got := snapshotField(t, details, "before", "email"); got != "old@example.com" {
		t.Errorf("Expected before.email old@example.com, got %v", got)
	}
	if got := snapshotField(t, details, "after", "email"); got != "new@example.com" {
		t.Errorf("Expected after.email new@example.com, got %v", got)
	}
}

func TestAuditUsersUpdateUserEmailNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.UpdateUserEmail("nobody", "new@example.com"); err == nil {
		t.Fatal("Expected UpdateUserEmail on a missing user to fail")
	}
	if ev := lastAuditEvent(t, store); ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
}

// =============================================================================
// Enable, disable and superuser
// =============================================================================

func TestAuditUsersEnableUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := as.DisableUser("bob"); err != nil {
		t.Fatalf("DisableUser failed: %v", err)
	}
	if err := as.EnableUser("bob"); err != nil {
		t.Fatalf("EnableUser failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertSuccessOn(t, ev, "user.enable", "bob")

	details := auditDetails(t, ev)
	if got := snapshotField(t, details, "before", "enabled"); got != false {
		t.Errorf("Expected before.enabled false, got %v", got)
	}
	if got := snapshotField(t, details, "after", "enabled"); got != true {
		t.Errorf("Expected after.enabled true, got %v", got)
	}
}

func TestAuditUsersDisableUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := as.DisableUser("bob"); err != nil {
		t.Fatalf("DisableUser failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertSuccessOn(t, ev, "user.disable", "bob")

	details := auditDetails(t, ev)
	if got := snapshotField(t, details, "before", "enabled"); got != true {
		t.Errorf("Expected before.enabled true, got %v", got)
	}
	if got := snapshotField(t, details, "after", "enabled"); got != false {
		t.Errorf("Expected after.enabled false, got %v", got)
	}
}

func TestAuditUsersEnableDisableNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.EnableUser("nobody"); err == nil {
		t.Fatal("Expected EnableUser on a missing user to fail")
	}
	if ev := lastAuditEvent(t, store); ev.Action != "user.enable" ||
		ev.Outcome != OutcomeFailure {
		t.Errorf("Expected a failed user.enable event, got %q/%q", ev.Action, ev.Outcome)
	}

	if err := as.DisableUser("nobody"); err == nil {
		t.Fatal("Expected DisableUser on a missing user to fail")
	}
	if ev := lastAuditEvent(t, store); ev.Action != "user.disable" ||
		ev.Outcome != OutcomeFailure {
		t.Errorf("Expected a failed user.disable event, got %q/%q", ev.Action, ev.Outcome)
	}
}

func TestAuditUsersSetUserSuperuser(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if err := as.SetUserSuperuser("bob", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	ev := lastAuditEvent(t, store)
	assertSuccessOn(t, ev, "user.set_superuser", "bob")
	details := auditDetails(t, ev)
	if got := snapshotField(t, details, "before", "is_superuser"); got != false {
		t.Errorf("Expected before.is_superuser false, got %v", got)
	}
	if got := snapshotField(t, details, "after", "is_superuser"); got != true {
		t.Errorf("Expected after.is_superuser true, got %v", got)
	}

	if err := as.SetUserSuperuser("bob", false); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	ev = lastAuditEvent(t, store)
	assertSuccessOn(t, ev, "user.unset_superuser", "bob")
	details = auditDetails(t, ev)
	if got := snapshotField(t, details, "after", "is_superuser"); got != false {
		t.Errorf("Expected after.is_superuser false, got %v", got)
	}
}

func TestAuditUsersSetUserSuperuserNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.SetUserSuperuser("nobody", true); err == nil {
		t.Fatal("Expected SetUserSuperuser on a missing user to fail")
	}
	if ev := lastAuditEvent(t, store); ev.Action != "user.set_superuser" ||
		ev.Outcome != OutcomeFailure {
		t.Errorf("Expected a failed user.set_superuser event, got %q/%q",
			ev.Action, ev.Outcome)
	}
}

// =============================================================================
// Deletion
// =============================================================================

func TestAuditUsersDeleteUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "note", "Bob", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	groupID, err := store.CreateGroup("editors", "Editors")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	user, err := store.GetUser("bob")
	if err != nil || user == nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if err := store.AddUserToGroup(groupID, user.ID); err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}
	if _, _, err := store.CreateToken("bob", "token", nil); err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	if err := as.DeleteUser("bob"); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	assertSuccessOn(t, ev, "user.delete", "bob")

	details := auditDetails(t, ev)
	if got := snapshotField(t, details, "before", "username"); got != "bob" {
		t.Errorf("Expected before.username bob, got %v", got)
	}
	groups, ok := details["groups"].([]any)
	if !ok || len(groups) != 1 || groups[0] != "editors" {
		t.Errorf("Expected groups [editors], got %v", details["groups"])
	}
	if got, ok := details["tokens_deleted"].(float64); !ok || got != 1 {
		t.Errorf("Expected tokens_deleted 1, got %v", details["tokens_deleted"])
	}
	if _, ok := details["after"]; ok {
		t.Errorf("Expected no after snapshot on delete, got %v", details["after"])
	}
}

func TestAuditUsersDeleteUserNotFound(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	err := as.DeleteUser("nobody")
	if err == nil {
		t.Fatal("Expected DeleteUser on a missing user to fail")
	}

	ev := lastAuditEvent(t, store)
	assertUserActor(t, ev)
	if ev.Action != "user.delete" {
		t.Errorf("Expected action user.delete, got %q", ev.Action)
	}
	if ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
	if ev.TargetName != "nobody" {
		t.Errorf("Expected target name nobody, got %q", ev.TargetName)
	}
	if ev.TargetID != nil {
		t.Errorf("Expected no target id, got %v", *ev.TargetID)
	}
	if ev.Error != err.Error() {
		t.Errorf("Expected error text %q, got %q", err.Error(), ev.Error)
	}
}

// =============================================================================
// Actor attribution
// =============================================================================

func TestAuditUsersSystemActor(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	if err := store.CreateUser("bob", "Str0ngPassphrase!", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := store.DisableUser("bob"); err != nil {
		t.Fatalf("DisableUser failed: %v", err)
	}

	ev := lastAuditEvent(t, store)
	if ev.ActorType != ActorSystem {
		t.Errorf("Expected actor type system, got %q", ev.ActorType)
	}
	if ev.ActorName != "system" {
		t.Errorf("Expected actor name system, got %q", ev.ActorName)
	}
	if ev.ActorID != nil {
		t.Errorf("Expected no actor id, got %v", *ev.ActorID)
	}
	if ev.Action != "user.disable" {
		t.Errorf("Expected action user.disable, got %q", ev.Action)
	}
}

func TestAuditUsersChainStaysIntact(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "", "", ""); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := as.SetUserSuperuser("bob", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	if err := as.DeleteUser("bob"); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	count, firstBad, err := store.VerifyAuditChain()
	if err != nil {
		t.Fatalf("VerifyAuditChain failed at row %d: %v", firstBad, err)
	}
	if count < 3 {
		t.Errorf("Expected at least 3 audit rows, got %d", count)
	}
}

// =============================================================================
// Error paths
// =============================================================================

// mustExec runs a statement directly against the store's database,
// failing the test if it errors. The audited-mutation error paths are
// otherwise unreachable, so the tests below break the schema on purpose
// to reach them.
func mustExec(t *testing.T, s *AuthStore, stmt string) {
	t.Helper()

	if _, err := s.db.Exec(stmt); err != nil {
		t.Fatalf("Failed to run %q: %v", stmt, err)
	}
}

// mustCreateUser creates a user through the system actor.
func mustCreateUser(t *testing.T, s *AuthStore, username string) {
	t.Helper()

	if err := s.CreateUser(username, "Str0ngPassphrase!", "note", "Bob",
		"bob@example.com"); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
}

func TestAuditUsersValidationFailures(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateUser("", "Str0ngPassphrase!", "", "", ""); err == nil {
		t.Error("Expected an empty username to be rejected")
	}
	if err := as.CreateUser("bob", "short", "", "", ""); err == nil {
		t.Error("Expected a weak password to be rejected")
	}
	if err := as.CreateServiceAccount("", "", "", ""); err == nil {
		t.Error("Expected an empty service account name to be rejected")
	}
	if err := as.UpdateUser("bob", "short", "", "", ""); err == nil {
		t.Error("Expected a weak replacement password to be rejected")
	}

	// A validation error happens before any database access, so no
	// event is recorded for it.
	events, _, err := store.ListAuditEvents(AuditFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Expected no audit events, got %d", len(events))
	}
}

func TestAuditUsersDuplicateServiceAccountRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	as := store.AsActor(testActor())
	if err := as.CreateServiceAccount("svc", "", "", ""); err != nil {
		t.Fatalf("CreateServiceAccount failed: %v", err)
	}
	if err := as.CreateServiceAccount("svc", "", "", ""); err == nil {
		t.Fatal("Expected a duplicate service account to fail")
	}

	ev := lastAuditEvent(t, store)
	if ev.Action != "service_account.create" || ev.Outcome != OutcomeFailure {
		t.Errorf("Expected a failed service_account.create event, got %q/%q",
			ev.Action, ev.Outcome)
	}
}

func TestAuditUsersHashFailureRecordsFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateUser(t, store, "bob")

	// A cost above bcrypt.MaxCost makes every hashing call fail.
	store.bcryptCost = 99

	as := store.AsActor(testActor())
	if err := as.CreateUser("carol", "Str0ngPassphrase!", "", "", ""); err == nil {
		t.Error("Expected CreateUser to fail with an invalid bcrypt cost")
	}
	if err := as.UpdateUser("bob", "An0therPassphrase!", "", "", ""); err == nil {
		t.Error("Expected UpdateUser to fail with an invalid bcrypt cost")
	}
	password := "An0therPassphrase!"
	if err := as.UpdateUserAtomic("bob", UserUpdate{Password: &password}); err == nil {
		t.Error("Expected UpdateUserAtomic to fail with an invalid bcrypt cost")
	}
}

func TestAuditUsersClosedStoreFailsToBegin(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateUser(t, store, "bob")
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	as := store.AsActor(testActor())
	password := "An0therPassphrase!"
	cases := []struct {
		name string
		call func() error
	}{
		{"CreateUser", func() error {
			return as.CreateUser("carol", "Str0ngPassphrase!", "", "", "")
		}},
		{"CreateServiceAccount", func() error {
			return as.CreateServiceAccount("svc", "", "", "")
		}},
		{"UpdateUser", func() error {
			return as.UpdateUser("bob", "", "note", "", "")
		}},
		{"UpdateUserAtomic", func() error {
			return as.UpdateUserAtomic("bob", UserUpdate{Password: &password})
		}},
		{"UpdateUserDisplayName", func() error {
			return as.UpdateUserDisplayName("bob", "New")
		}},
		{"UpdateUserEmail", func() error {
			return as.UpdateUserEmail("bob", "new@example.com")
		}},
		{"EnableUser", func() error { return as.EnableUser("bob") }},
		{"DisableUser", func() error { return as.DisableUser("bob") }},
		{"SetUserSuperuser", func() error { return as.SetUserSuperuser("bob", true) }},
		{"DeleteUser", func() error { return as.DeleteUser("bob") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Errorf("Expected %s to fail on a closed store", tc.name)
			}
		})
	}
}

func TestAuditUsersBlockedUpdatesRecordFailure(t *testing.T) {
	blockedColumns := []struct {
		column string
		call   func(as *ActorStore) error
	}{
		{"password_hash", func(as *ActorStore) error {
			return as.UpdateUser("bob", "An0therPassphrase!", "note", "", "")
		}},
		{"annotation", func(as *ActorStore) error {
			return as.UpdateUser("bob", "", "note", "", "")
		}},
		{"display_name", func(as *ActorStore) error {
			return as.UpdateUserDisplayName("bob", "New")
		}},
		{"email", func(as *ActorStore) error {
			return as.UpdateUserEmail("bob", "new@example.com")
		}},
		{"enabled", func(as *ActorStore) error { return as.DisableUser("bob") }},
		{"is_superuser", func(as *ActorStore) error {
			return as.SetUserSuperuser("bob", true)
		}},
	}

	for _, tc := range blockedColumns {
		t.Run(tc.column, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			mustCreateUser(t, store, "bob")
			mustExec(t, store, "CREATE TRIGGER block_update BEFORE UPDATE OF "+
				tc.column+" ON users BEGIN SELECT RAISE(ABORT, 'blocked'); END")

			as := store.AsActor(testActor())
			if err := tc.call(as); err == nil {
				t.Fatalf("Expected the blocked %s update to fail", tc.column)
			}
			if ev := lastAuditEvent(t, store); ev.Outcome != OutcomeFailure {
				t.Errorf("Expected outcome failure, got %q", ev.Outcome)
			}
		})
	}
}

func TestAuditUsersBlockedAtomicUpdatesRecordFailure(t *testing.T) {
	password := "An0therPassphrase!"
	annotation := "note"
	enabled := false
	superuser := true

	cases := []struct {
		column string
		update UserUpdate
	}{
		{"password_hash", UserUpdate{Password: &password}},
		{"annotation", UserUpdate{Annotation: &annotation}},
		{"enabled", UserUpdate{Enabled: &enabled}},
		{"is_superuser", UserUpdate{IsSuperuser: &superuser}},
	}

	for _, tc := range cases {
		t.Run(tc.column, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			mustCreateUser(t, store, "bob")
			mustExec(t, store, "CREATE TRIGGER block_update BEFORE UPDATE OF "+
				tc.column+" ON users BEGIN SELECT RAISE(ABORT, 'blocked'); END")

			as := store.AsActor(testActor())
			if err := as.UpdateUserAtomic("bob", tc.update); err == nil {
				t.Fatalf("Expected the blocked %s update to fail", tc.column)
			}
			if ev := lastAuditEvent(t, store); ev.Outcome != OutcomeFailure {
				t.Errorf("Expected outcome failure, got %q", ev.Outcome)
			}
		})
	}
}

func TestAuditUsersBlockedDeletesRecordFailure(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(*testing.T, *AuthStore)
	}{
		{"connection_sessions", func(t *testing.T, s *AuthStore) {
			mustExec(t, s, "DROP TABLE connection_sessions")
		}},
		{"token_connection_scope", func(t *testing.T, s *AuthStore) {
			mustExec(t, s, "DROP TABLE token_connection_scope")
		}},
		{"tokens", func(t *testing.T, s *AuthStore) {
			mustExec(t, s, "CREATE TRIGGER block_delete BEFORE DELETE ON tokens "+
				"BEGIN SELECT RAISE(ABORT, 'blocked'); END")
		}},
		{"group_memberships", func(t *testing.T, s *AuthStore) {
			mustExec(t, s, "CREATE TRIGGER block_delete BEFORE DELETE ON "+
				"group_memberships BEGIN SELECT RAISE(ABORT, 'blocked'); END")
		}},
		{"users", func(t *testing.T, s *AuthStore) {
			mustExec(t, s, "CREATE TRIGGER block_delete BEFORE DELETE ON users "+
				"BEGIN SELECT RAISE(ABORT, 'blocked'); END")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			mustCreateUserWithDependents(t, store, "bob")
			tc.break_(t, store)

			as := store.AsActor(testActor())
			if err := as.DeleteUser("bob"); err == nil {
				t.Fatalf("Expected the blocked %s delete to fail", tc.name)
			}
			ev := lastAuditEvent(t, store)
			if ev.Action != "user.delete" || ev.Outcome != OutcomeFailure {
				t.Errorf("Expected a failed user.delete event, got %q/%q",
					ev.Action, ev.Outcome)
			}
		})
	}
}

// mustCreateUserWithDependents creates a user that owns a token and
// belongs to a group, so that every dependent delete in DeleteUser has
// at least one row to remove.
func mustCreateUserWithDependents(t *testing.T, s *AuthStore, username string) {
	t.Helper()

	mustCreateUser(t, s, username)

	groupID, err := s.CreateGroup("editors", "Editors")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	user, err := s.GetUser(username)
	if err != nil || user == nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if err := s.AddUserToGroup(groupID, user.ID); err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}
	if _, _, err := s.CreateToken(username, "token", nil); err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
}

func TestAuditUsersLookupFailureIsWrapped(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateUser(t, store, "bob")
	mustExec(t, store, "ALTER TABLE users RENAME TO users_moved")

	as := store.AsActor(testActor())
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"DisableUser", func() error { return as.DisableUser("bob") }},
		{"SetUserSuperuser", func() error { return as.SetUserSuperuser("bob", true) }},
	} {
		err := tc.call()
		if err == nil {
			t.Fatalf("Expected %s to fail without a users table", tc.name)
		}
		if !strings.Contains(err.Error(), "failed to look up user") {
			t.Errorf("Expected a wrapped lookup error from %s, got %v", tc.name, err)
		}
	}
}

func TestAuditUsersGroupLookupFailureIsWrapped(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateUser(t, store, "bob")
	mustExec(t, store, "DROP TABLE user_groups")

	as := store.AsActor(testActor())
	err := as.DeleteUser("bob")
	if err == nil {
		t.Fatal("Expected DeleteUser to fail without a user_groups table")
	}
	if !strings.Contains(err.Error(), "failed to list user's groups") {
		t.Errorf("Expected a wrapped group-listing error, got %v", err)
	}
}

func TestAuditUsersRecordAuditFailurePropagates(t *testing.T) {
	as := func(s *AuthStore) *ActorStore { return s.AsActor(testActor()) }

	cases := []struct {
		name string
		call func(*AuthStore) error
	}{
		{"CreateUser", func(s *AuthStore) error {
			return as(s).CreateUser("carol", "Str0ngPassphrase!", "", "", "")
		}},
		{"UpdateUser", func(s *AuthStore) error {
			return as(s).UpdateUser("bob", "", "note", "", "")
		}},
		{"UpdateUserAtomic", func(s *AuthStore) error {
			annotation := "note"
			return as(s).UpdateUserAtomic("bob", UserUpdate{Annotation: &annotation})
		}},
		{"UpdateUserDisplayName", func(s *AuthStore) error {
			return as(s).UpdateUserDisplayName("bob", "New")
		}},
		{"DisableUser", func(s *AuthStore) error { return as(s).DisableUser("bob") }},
		{"SetUserSuperuser", func(s *AuthStore) error {
			return as(s).SetUserSuperuser("bob", true)
		}},
		{"DeleteUser", func(s *AuthStore) error { return as(s).DeleteUser("bob") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			mustCreateUser(t, store, "bob")
			mustExec(t, store, "DROP TRIGGER audit_events_no_update")
			mustExec(t, store, "DROP TABLE audit_events")

			if err := tc.call(store); err == nil {
				t.Errorf("Expected %s to fail without an audit_events table", tc.name)
			}
		})
	}
}

func TestAuditUsersUpdateFieldRejectsUnknownColumn(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	err := store.updateUserField(systemActor, "bob", "password_hash", "x")
	if err == nil {
		t.Fatal("Expected an unsupported column to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported user column") {
		t.Errorf("Expected an unsupported-column error, got %v", err)
	}
}
