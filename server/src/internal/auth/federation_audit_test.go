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
	"testing"
)

// =============================================================================
// Federated Reconciliation Audit Tests
// =============================================================================

const testFederatedIssuer = "https://idp.example.com"

// federatedEvents returns the events recorded for action, newest first.
func federatedEvents(t *testing.T, store *AuthStore, action string) []AuditEvent {
	t.Helper()

	events, _, err := store.ListAuditEvents(AuditFilter{Action: action})
	if err != nil {
		t.Fatalf("ListAuditEvents(%s): %v", action, err)
	}
	return events
}

// assertFederatedEvent checks the attribution, target and reason every event
// recorded by reconciliation must carry, and returns its decoded details.
func assertFederatedEvent(t *testing.T, ev AuditEvent, action, targetType,
	targetName string) map[string]any {

	t.Helper()

	if ev.Action != action {
		t.Errorf("action = %q, want %q", ev.Action, action)
	}
	if ev.ActorType != ActorSystem || ev.ActorName != "system" {
		t.Errorf("actor = %q/%q, want the system actor", ev.ActorType, ev.ActorName)
	}
	if ev.TargetType != targetType || ev.TargetName != targetName {
		t.Errorf("target = %q/%q, want %q/%q", ev.TargetType, ev.TargetName,
			targetType, targetName)
	}
	if ev.Outcome != OutcomeSuccess {
		t.Errorf("outcome = %q, want success", ev.Outcome)
	}
	details := auditDetails(t, ev)
	if details["reason"] != federatedAuditReason {
		t.Errorf("details.reason = %v, want %q", details["reason"], federatedAuditReason)
	}
	if details["issuer"] != testFederatedIssuer {
		t.Errorf("details.issuer = %v, want %q", details["issuer"], testFederatedIssuer)
	}
	assertNoSecrets(t, ev)
	return details
}

// dropAuditLog makes every subsequent audit write fail.
func dropAuditLog(t *testing.T, store *AuthStore) {
	t.Helper()

	mustExec(t, store, "DROP TRIGGER audit_events_no_update")
	mustExec(t, store, "DROP TABLE audit_events")
}

func TestReconcileFederatedGroupsAuditsMembershipChanges(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	groupID, err := store.CreateGroup("workbench-admins", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	user := resolveForTest(t, store)
	opts := FederationOptions{GroupMap: map[string]string{"idp-admins": "workbench-admins"}}
	present := FederatedIdentity{Issuer: testFederatedIssuer, Groups: []string{"idp-admins"}}
	absent := FederatedIdentity{Issuer: testFederatedIssuer}

	if err := store.ReconcileFederatedGroups(user.ID, present, opts); err != nil {
		t.Fatalf("reconcile (present): %v", err)
	}
	added := federatedEvents(t, store, "group.member.add")
	if len(added) != 1 {
		t.Fatalf("group.member.add events = %d, want 1", len(added))
	}
	details := assertFederatedEvent(t, added[0], "group.member.add", "group", "workbench-admins")
	if added[0].TargetID == nil || *added[0].TargetID != groupID {
		t.Errorf("target_id = %v, want %d", added[0].TargetID, groupID)
	}
	if details["member_type"] != memberTypeUser || details["member_name"] != user.Username {
		t.Errorf("member = %v/%v, want %s/%s", details["member_type"],
			details["member_name"], memberTypeUser, user.Username)
	}

	// A login that changes nothing records nothing.
	before := auditEventCount(t, store)
	if err := store.ReconcileFederatedGroups(user.ID, present, opts); err != nil {
		t.Fatalf("reconcile (repeat): %v", err)
	}
	if got := auditEventCount(t, store); got != before {
		t.Errorf("repeat reconcile recorded %d event(s), want 0", got-before)
	}

	if err := store.ReconcileFederatedGroups(user.ID, absent, opts); err != nil {
		t.Fatalf("reconcile (absent): %v", err)
	}
	removed := federatedEvents(t, store, "group.member.remove")
	if len(removed) != 1 {
		t.Fatalf("group.member.remove events = %d, want 1", len(removed))
	}
	details = assertFederatedEvent(t, removed[0], "group.member.remove", "group", "workbench-admins")
	if details["member_name"] != user.Username {
		t.Errorf("member_name = %v, want %s", details["member_name"], user.Username)
	}

	before = auditEventCount(t, store)
	if err := store.ReconcileFederatedGroups(user.ID, absent, opts); err != nil {
		t.Fatalf("reconcile (absent, repeat): %v", err)
	}
	if got := auditEventCount(t, store); got != before {
		t.Errorf("repeat removal recorded %d event(s), want 0", got-before)
	}
}

func TestReconcileFederatedGroupsAuditsSuperuserChanges(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	user := resolveForTest(t, store)
	opts := FederationOptions{SuperuserGroup: "idp-superusers"}
	granted := FederatedIdentity{Issuer: testFederatedIssuer, Groups: []string{"idp-superusers"}}
	revoked := FederatedIdentity{Issuer: testFederatedIssuer}

	if err := store.ReconcileFederatedGroups(user.ID, granted, opts); err != nil {
		t.Fatalf("reconcile (grant): %v", err)
	}
	updates := federatedEvents(t, store, "user.update")
	if len(updates) != 1 {
		t.Fatalf("user.update events after grant = %d, want 1", len(updates))
	}
	details := assertFederatedEvent(t, updates[0], "user.update", "user", user.Username)
	if got := snapshotField(t, details, "before", "is_superuser"); got != false {
		t.Errorf("before.is_superuser = %v, want false", got)
	}
	if got := snapshotField(t, details, "after", "is_superuser"); got != true {
		t.Errorf("after.is_superuser = %v, want true", got)
	}
	if details["password_changed"] != false {
		t.Errorf("password_changed = %v, want false", details["password_changed"])
	}

	before := auditEventCount(t, store)
	if err := store.ReconcileFederatedGroups(user.ID, granted, opts); err != nil {
		t.Fatalf("reconcile (grant, repeat): %v", err)
	}
	if got := auditEventCount(t, store); got != before {
		t.Errorf("repeat grant recorded %d event(s), want 0", got-before)
	}

	if err := store.ReconcileFederatedGroups(user.ID, revoked, opts); err != nil {
		t.Fatalf("reconcile (revoke): %v", err)
	}
	updates = federatedEvents(t, store, "user.update")
	if len(updates) != 2 {
		t.Fatalf("user.update events after revoke = %d, want 2", len(updates))
	}
	details = assertFederatedEvent(t, updates[0], "user.update", "user", user.Username)
	if got := snapshotField(t, details, "before", "is_superuser"); got != true {
		t.Errorf("before.is_superuser = %v, want true", got)
	}
	if got := snapshotField(t, details, "after", "is_superuser"); got != false {
		t.Errorf("after.is_superuser = %v, want false", got)
	}

	before = auditEventCount(t, store)
	if err := store.ReconcileFederatedGroups(user.ID, revoked, opts); err != nil {
		t.Fatalf("reconcile (revoke, repeat): %v", err)
	}
	if got := auditEventCount(t, store); got != before {
		t.Errorf("repeat revocation recorded %d event(s), want 0", got-before)
	}
}

// TestReconcileFederatedGroupsRollsBackAnUnauditedGrant proves a grant whose
// event cannot be written is not applied, and that the login is refused.
func TestReconcileFederatedGroupsRollsBackAnUnauditedGrant(t *testing.T) {
	tests := []struct {
		name string
		opts FederationOptions
	}{
		{"Membership", FederationOptions{GroupMap: map[string]string{"idp-admins": "workbench-admins"}}},
		{"Superuser", FederationOptions{SuperuserGroup: "idp-admins"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForStore(t)
			defer cleanup()

			groupID, err := store.CreateGroup("workbench-admins", "")
			if err != nil {
				t.Fatalf("CreateGroup: %v", err)
			}
			user := resolveForTest(t, store)
			dropAuditLog(t, store)

			if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
				Issuer: testFederatedIssuer, Groups: []string{"idp-admins"}}, tc.opts); err == nil {
				t.Fatal("expected a grant whose event cannot be written to fail")
			}
			assertInGroup(t, store, user.ID, groupID, false)
			assertSuperuser(t, store, user.Username, false)
		})
	}
}

// TestReconcileFederatedGroupsKeepsAnUnauditedRevocation proves the opposite
// direction: a revocation stands even when its event cannot be written,
// because rolling it back would restore the privilege being removed.
func TestReconcileFederatedGroupsKeepsAnUnauditedRevocation(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	groupID, err := store.CreateGroup("workbench-admins", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	user := resolveForTest(t, store)
	opts := FederationOptions{
		GroupMap:       map[string]string{"idp-admins": "workbench-admins"},
		SuperuserGroup: "idp-superusers",
	}
	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Issuer: testFederatedIssuer,
		Groups: []string{"idp-admins", "idp-superusers"}}, opts); err != nil {
		t.Fatalf("reconcile (grant): %v", err)
	}
	assertInGroup(t, store, user.ID, groupID, true)
	assertSuperuser(t, store, user.Username, true)

	dropAuditLog(t, store)

	if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
		Issuer: testFederatedIssuer}, opts); err != nil {
		t.Fatalf("reconcile (revoke): %v", err)
	}
	assertInGroup(t, store, user.ID, groupID, false)
	assertSuperuser(t, store, user.Username, false)
}

// TestRecordFederatedSuperuserRevocationErrorPaths covers the failures the
// helper logs rather than returns, which a reconcile cannot reach in order.
func TestRecordFederatedSuperuserRevocationErrorPaths(t *testing.T) {
	t.Run("UnknownUser", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		before := auditEventCount(t, store)
		store.mu.Lock()
		store.recordFederatedSuperuserRevocationLocked("no-such-user", testFederatedIssuer)
		store.mu.Unlock()
		if got := auditEventCount(t, store); got != before {
			t.Errorf("recorded %d event(s) for an unknown user, want 0", got-before)
		}
	})

	t.Run("ClosedStore", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		if err := store.db.Close(); err != nil {
			t.Fatalf("db.Close: %v", err)
		}
		// Nothing to assert beyond not panicking: the failure is logged.
		store.mu.Lock()
		store.recordFederatedSuperuserRevocationLocked("anyone", testFederatedIssuer)
		store.mu.Unlock()
	})
}

func TestGrantFederatedSuperuserRefusesAnUnknownUser(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	store.mu.Lock()
	defer store.mu.Unlock()

	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() {
		//nolint:errcheck // test cleanup; the transaction is discarded
		tx.Rollback()
	}()

	if err := store.grantFederatedSuperuserTx(tx, 4242, "no-such-user",
		testFederatedIssuer); err == nil {
		t.Fatal("expected an unknown user to be refused")
	}
}

// TestFederatedReconcileStepsSurfaceDatabaseErrors drives the failures inside
// the revocation and grant steps: a refused write through a trigger, and a
// transaction that cannot begin, which a reconcile cannot reach because its
// account lookup fails first on a closed store.
func TestFederatedReconcileStepsSurfaceDatabaseErrors(t *testing.T) {
	setup := func(t *testing.T) (*AuthStore, func(), *StoredUser, []mappedGroupTarget) {
		t.Helper()
		store, cleanup := createTestAuthStoreForStore(t)
		groupID, err := store.CreateGroup("workbench-admins", "")
		if err != nil {
			cleanup()
			t.Fatalf("CreateGroup: %v", err)
		}
		user := resolveForTest(t, store)
		return store, cleanup, user, []mappedGroupTarget{
			{name: "workbench-admins", id: groupID, desired: true},
		}
	}
	opts := FederationOptions{GroupMap: map[string]string{"idp-admins": "workbench-admins"}}

	t.Run("GrantInsertRefused", func(t *testing.T) {
		store, cleanup, user, _ := setup(t)
		defer cleanup()
		mustExec(t, store, `CREATE TRIGGER refuse_membership BEFORE INSERT ON group_memberships
            BEGIN SELECT RAISE(ABORT, 'refused'); END`)

		if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
			Issuer: testFederatedIssuer, Groups: []string{"idp-admins"}}, opts); err == nil {
			t.Fatal("expected the refused insert to surface")
		}
	})

	t.Run("RevokeDeleteRefused", func(t *testing.T) {
		store, cleanup, user, _ := setup(t)
		defer cleanup()
		if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
			Issuer: testFederatedIssuer, Groups: []string{"idp-admins"}}, opts); err != nil {
			t.Fatalf("reconcile (grant): %v", err)
		}
		mustExec(t, store, `CREATE TRIGGER refuse_removal BEFORE DELETE ON group_memberships
            BEGIN SELECT RAISE(ABORT, 'refused'); END`)

		before := auditEventCount(t, store)
		if err := store.ReconcileFederatedGroups(user.ID, FederatedIdentity{
			Issuer: testFederatedIssuer}, opts); err == nil {
			t.Fatal("expected the refused delete to surface")
		}
		if got := auditEventCount(t, store); got != before {
			t.Errorf("a refused removal recorded %d event(s), want 0", got-before)
		}
	})

	t.Run("ClosedStore", func(t *testing.T) {
		store, cleanup, user, targets := setup(t)
		defer cleanup()
		if err := store.db.Close(); err != nil {
			t.Fatalf("db.Close: %v", err)
		}

		store.mu.Lock()
		defer store.mu.Unlock()
		if err := store.revokeFederatedLocked(user.ID, user.Username,
			testFederatedIssuer, targets); err == nil {
			t.Error("expected revokeFederatedLocked to fail on a closed store")
		}
		if err := store.grantFederatedLocked(user.ID, user.Username,
			testFederatedIssuer, targets, opts, false); err == nil {
			t.Error("expected grantFederatedLocked to fail on a closed store")
		}
	})
}
