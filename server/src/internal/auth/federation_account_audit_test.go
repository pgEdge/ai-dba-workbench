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
	"database/sql"
	"strings"
	"testing"
)

// =============================================================================
// Federated Account Audit Tests: provisioning, linking and unlinking
// =============================================================================

// testCLIActor stands in for the operator the CLI attributes changes to.
func testCLIActor() Actor {
	return Actor{Type: ActorCLI, Name: "dba-ops"}
}

// assertCLIEvent checks an event is a success attributed to the CLI operator
// on the named user, and returns its decoded details.
func assertCLIEvent(t *testing.T, ev AuditEvent, action, username string) map[string]any {
	t.Helper()

	if ev.Action != action {
		t.Errorf("action = %q, want %q", ev.Action, action)
	}
	if ev.ActorType != ActorCLI || ev.ActorName != "dba-ops" {
		t.Errorf("actor = %q/%q, want the CLI operator", ev.ActorType, ev.ActorName)
	}
	if ev.TargetType != "user" || ev.TargetName != username || ev.TargetID == nil {
		t.Errorf("target = %q/%q/%v, want user %q with an id", ev.TargetType,
			ev.TargetName, ev.TargetID, username)
	}
	if ev.Outcome != OutcomeSuccess {
		t.Errorf("outcome = %q, want success", ev.Outcome)
	}
	assertNoSecrets(t, ev)
	return auditDetails(t, ev)
}

// assertLinkState checks one before or after object of a link event.
func assertLinkState(t *testing.T, details map[string]any, which, authSource string,
	externalSubject any) {

	t.Helper()

	if got := snapshotField(t, details, which, "auth_source"); got != authSource {
		t.Errorf("%s.auth_source = %v, want %q", which, got, authSource)
	}
	if got := snapshotField(t, details, which, "external_subject"); got != externalSubject {
		t.Errorf("%s.external_subject = %v, want %v", which, got, externalSubject)
	}
}

// assertFailureEvent checks the newest event is a failure of action on the
// named user, with or without a target id as wantID says.
func assertFailureEvent(t *testing.T, store *AuthStore, action, username, errText string,
	wantID bool) {

	t.Helper()

	ev := lastAuditEvent(t, store)
	if ev.Action != action || ev.Outcome != OutcomeFailure {
		t.Fatalf("newest event = %s/%s, want a %s failure", ev.Action, ev.Outcome, action)
	}
	if ev.ActorType != ActorCLI || ev.TargetName != username {
		t.Errorf("failure attributed to %q on %q, want the CLI operator on %q",
			ev.ActorType, ev.TargetName, username)
	}
	if (ev.TargetID != nil) != wantID {
		t.Errorf("failure target id = %v, want present = %v", ev.TargetID, wantID)
	}
	if !strings.Contains(ev.Error, errText) {
		t.Errorf("failure error = %q, want it to contain %q", ev.Error, errText)
	}
}

// linkedLocalUser creates a local account and links it through the CLI actor.
func linkedLocalUser(t *testing.T, store *AuthStore, username string) {
	t.Helper()

	if err := store.CreateUser(username, linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.AsActor(testCLIActor()).LinkFederatedIdentity(username,
		linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
}

// assertLinkColumns reads auth_source and external_subject back directly.
func assertLinkColumns(t *testing.T, store *AuthStore, username, authSource string,
	externalSubject sql.NullString) {

	t.Helper()

	var gotSource string
	var gotSubject sql.NullString
	if err := store.db.QueryRow(
		"SELECT auth_source, external_subject FROM users WHERE username = ?",
		username).Scan(&gotSource, &gotSubject); err != nil {
		t.Fatalf("reading %s: %v", username, err)
	}
	if gotSource != authSource || gotSubject != externalSubject {
		t.Fatalf("%s has auth_source=%q external_subject=%v, want %q/%v",
			username, gotSource, gotSubject, authSource, externalSubject)
	}
}

func TestProvisioningRecordsUserCreate(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	user := resolveForTest(t, store)

	events := federatedEvents(t, store, "user.create")
	if len(events) != 1 {
		t.Fatalf("recorded %d user.create event(s), want 1", len(events))
	}
	details := assertFederatedEvent(t, events[0], "user.create", "user", user.Username)
	if events[0].TargetID == nil || *events[0].TargetID != user.ID {
		t.Errorf("target id = %v, want %d", events[0].TargetID, user.ID)
	}
	if details["subject"] != testIdentity().Subject {
		t.Errorf("details.subject = %v, want %q", details["subject"], testIdentity().Subject)
	}
	if got := snapshotField(t, details, "after", "username"); got != user.Username {
		t.Errorf("after.username = %v, want %q", got, user.Username)
	}

	// A second login resolves the same account and creates nothing.
	resolveForTest(t, store)
	if got := len(federatedEvents(t, store, "user.create")); got != 1 {
		t.Errorf("a returning login recorded %d user.create event(s), want 1", got)
	}
}

// Provisioning is fail-closed: an account whose event cannot be written is
// not created, and the login fails.
func TestProvisioningRollsBackAnUnauditedAccount(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	dropAuditLog(t, store)

	if _, err := store.ResolveFederatedUser(testIdentity(),
		FederationOptions{ProvisionUsers: true}); err == nil {
		t.Fatal("expected provisioning to fail when its event cannot be written")
	}
	user, err := store.GetUser(testIdentity().Username)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user != nil {
		t.Fatal("the unaudited account was created")
	}
}

// The loser of a provisioning race adopts the winner's row and records no
// user.create of its own: the winner already did.
func TestProvisioningAdoptionRecordsNoSecondEvent(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	identity := testIdentity()
	if _, err := store.ResolveFederatedUser(FederatedIdentity{
		Issuer: identity.Issuer, Subject: identity.Subject,
		Username: "winner@example.com"}, FederationOptions{ProvisionUsers: true}); err != nil {
		t.Fatalf("seed the winning account: %v", err)
	}

	store.mu.Lock()
	_, err := store.provisionFederatedUserLocked(identity,
		ExternalSubjectKey(identity.Issuer, identity.Subject))
	store.mu.Unlock()
	if err != nil {
		t.Fatalf("provisionFederatedUserLocked: %v", err)
	}
	if got := len(federatedEvents(t, store, "user.create")); got != 1 {
		t.Errorf("recorded %d user.create event(s), want only the winner's", got)
	}
}

func TestLinkFederatedIdentityRecordsTheOperator(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	linkedLocalUser(t, store, "alice")
	key := ExternalSubjectKey(linkTestIssuer, linkTestSubject)

	events := federatedEvents(t, store, "user.link_identity")
	if len(events) != 1 {
		t.Fatalf("recorded %d user.link_identity event(s), want 1", len(events))
	}
	details := assertCLIEvent(t, events[0], "user.link_identity", "alice")
	if details["issuer"] != linkTestIssuer || details["subject"] != linkTestSubject {
		t.Errorf("details name %v/%v, want %q/%q", details["issuer"], details["subject"],
			linkTestIssuer, linkTestSubject)
	}
	if details["relink"] != false {
		t.Errorf("details.relink = %v, want false", details["relink"])
	}
	assertLinkState(t, details, "before", AuthSourceLocal, nil)
	assertLinkState(t, details, "after", AuthSourceOIDC, key)

	// The idempotent re-link changes nothing and records nothing.
	if _, err := store.AsActor(testCLIActor()).LinkFederatedIdentity("alice",
		linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("re-link: %v", err)
	}
	if got := len(federatedEvents(t, store, "user.link_identity")); got != 1 {
		t.Errorf("an idempotent re-link recorded %d event(s) in all, want 1", got)
	}
}

func TestLinkFederatedIdentityRecordsTheMovedSubject(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	linkedLocalUser(t, store, "alice")
	oldKey := ExternalSubjectKey(linkTestIssuer, linkTestSubject)
	newKey, err := store.AsActor(testCLIActor()).LinkFederatedIdentity("alice",
		linkTestIssuer, "subject-0002", true)
	if err != nil {
		t.Fatalf("relink: %v", err)
	}

	details := assertCLIEvent(t, lastAuditEvent(t, store), "user.link_identity", "alice")
	if details["relink"] != true {
		t.Errorf("details.relink = %v, want true", details["relink"])
	}
	assertLinkState(t, details, "before", AuthSourceOIDC, oldKey)
	assertLinkState(t, details, "after", AuthSourceOIDC, newKey)
}

// Linking is fail-closed: a link whose event cannot be written is not
// applied.
func TestLinkFederatedIdentityRollsBackAnUnauditedLink(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.CreateUser("alice", linkTestPassword, "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	dropAuditLog(t, store)

	if _, err := store.AsActor(testCLIActor()).LinkFederatedIdentity("alice",
		linkTestIssuer, linkTestSubject, false); err == nil {
		t.Fatal("expected a link whose event cannot be written to fail")
	}
	assertLinkColumns(t, store, "alice", AuthSourceLocal, sql.NullString{})
}

func TestLinkFederatedIdentityRecordsRefusals(t *testing.T) {
	t.Run("UnknownUser", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		if _, err := store.AsActor(testCLIActor()).LinkFederatedIdentity("ghost",
			linkTestIssuer, linkTestSubject, false); err == nil {
			t.Fatal("linking an unknown user succeeded")
		}
		assertFailureEvent(t, store, "user.link_identity", "ghost", "user not found", false)
	})

	t.Run("SecondIdentityWithoutRelink", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		linkedLocalUser(t, store, "alice")
		if _, err := store.AsActor(testCLIActor()).LinkFederatedIdentity("alice",
			linkTestIssuer, "subject-0002", false); err == nil {
			t.Fatal("a second identity was linked without -relink")
		}
		assertFailureEvent(t, store, "user.link_identity", "alice",
			"already linked to a different identity", true)
	})

	t.Run("SubjectHeldElsewhere", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		linkedLocalUser(t, store, "alice")
		if err := store.CreateUser("bob", linkTestPassword, "", "", ""); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if _, err := store.AsActor(testCLIActor()).LinkFederatedIdentity("bob",
			linkTestIssuer, linkTestSubject, false); err == nil {
			t.Fatal("a subject was linked to a second account")
		}
		assertFailureEvent(t, store, "user.link_identity", "bob",
			"already linked to account alice", true)
	})
}

func TestLinkFederatedIdentityReportsAClosedStore(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	if _, err := store.AsActor(testCLIActor()).LinkFederatedIdentity("alice",
		linkTestIssuer, linkTestSubject, false); err == nil {
		t.Fatal("linking through a closed store succeeded")
	}
}

func TestUnlinkFederatedIdentityRecordsTheOperator(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	linkedLocalUser(t, store, "alice")
	mustCreateToken(t, store, "alice", "to be revoked")
	key := ExternalSubjectKey(linkTestIssuer, linkTestSubject)

	if _, revoked, err := store.AsActor(testCLIActor()).UnlinkFederatedIdentity("alice",
		false); err != nil || revoked != 1 {
		t.Fatalf("UnlinkFederatedIdentity = %d, %v; want 1 token revoked", revoked, err)
	}

	events := federatedEvents(t, store, "user.unlink_identity")
	if len(events) != 1 {
		t.Fatalf("recorded %d user.unlink_identity event(s), want 1", len(events))
	}
	details := assertCLIEvent(t, events[0], "user.unlink_identity", "alice")
	if details["password_changed"] != true {
		t.Errorf("details.password_changed = %v, want true", details["password_changed"])
	}
	if details["issuer"] != linkTestIssuer || details["subject"] != linkTestSubject {
		t.Errorf("details name %v/%v, want %q/%q", details["issuer"], details["subject"],
			linkTestIssuer, linkTestSubject)
	}
	assertLinkState(t, details, "before", AuthSourceOIDC, key)
	assertLinkState(t, details, "after", AuthSourceLocal, nil)

	// The revocation the unlink makes names the same operator.
	deletions := federatedEvents(t, store, "token.delete")
	if len(deletions) != 1 {
		t.Fatalf("recorded %d token.delete event(s), want 1", len(deletions))
	}
	if deletions[0].ActorType != ActorCLI || deletions[0].ActorName != "dba-ops" {
		t.Errorf("token.delete attributed to %q/%q, want the CLI operator",
			deletions[0].ActorType, deletions[0].ActorName)
	}
}

func TestUnlinkFederatedIdentityRestorePasswordRecordsNoPasswordChange(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	linkedLocalUser(t, store, "alice")
	mustCreateToken(t, store, "alice", "kept")

	if _, _, err := store.AsActor(testCLIActor()).UnlinkFederatedIdentity("alice",
		true); err != nil {
		t.Fatalf("UnlinkFederatedIdentity: %v", err)
	}
	details := assertCLIEvent(t, lastAuditEvent(t, store), "user.unlink_identity", "alice")
	if details["password_changed"] != false {
		t.Errorf("details.password_changed = %v, want false", details["password_changed"])
	}
	if got := len(federatedEvents(t, store, "token.delete")); got != 0 {
		t.Errorf("recorded %d token.delete event(s), want none", got)
	}
}

// Unlinking is fail-closed: an unlink whose event cannot be written leaves
// the account linked, with its tokens.
func TestUnlinkFederatedIdentityRollsBackAnUnauditedUnlink(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	linkedLocalUser(t, store, "alice")
	dropAuditLog(t, store)

	if _, _, err := store.AsActor(testCLIActor()).UnlinkFederatedIdentity("alice",
		false); err == nil {
		t.Fatal("expected an unlink whose event cannot be written to fail")
	}
	assertLinkColumns(t, store, "alice", AuthSourceOIDC, sql.NullString{
		String: ExternalSubjectKey(linkTestIssuer, linkTestSubject), Valid: true})
}

func TestUnlinkFederatedIdentityRecordsRefusals(t *testing.T) {
	t.Run("UnknownUser", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		if _, _, err := store.AsActor(testCLIActor()).UnlinkFederatedIdentity("ghost",
			false); err == nil {
			t.Fatal("unlinking an unknown user succeeded")
		}
		assertFailureEvent(t, store, "user.unlink_identity", "ghost", "user not found", false)
	})

	t.Run("NotLinked", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		if err := store.CreateUser("alice", linkTestPassword, "", "", ""); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if _, _, err := store.AsActor(testCLIActor()).UnlinkFederatedIdentity("alice",
			false); err == nil {
			t.Fatal("unlinking an unlinked account succeeded")
		}
		assertFailureEvent(t, store, "user.unlink_identity", "alice", "not linked", true)
	})

	// The identity moving between the read and the UPDATE cannot be produced
	// from one process, so the stale read is handed to applyUnlinkLocked
	// directly.
	t.Run("IdentityMoved", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		linkedLocalUser(t, store, "alice")
		state, err := readFederatedLinkState(store.db, "alice")
		if err != nil {
			t.Fatalf("readFederatedLinkState: %v", err)
		}
		state.externalSubject = sql.NullString{String: "stale", Valid: true}

		store.mu.Lock()
		err = store.applyUnlinkLocked(testCLIActor(), "alice", state, true)
		store.mu.Unlock()
		if err == nil {
			t.Fatal("an unlink pinned to a stale subject succeeded")
		}
		assertFailureEvent(t, store, "user.unlink_identity", "alice",
			"changed while the unlink was being applied", true)
	})
}

func TestApplyUnlinkLockedReportsAClosedStore(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	linkedLocalUser(t, store, "alice")
	state, err := readFederatedLinkState(store.db, "alice")
	if err != nil {
		t.Fatalf("readFederatedLinkState: %v", err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	store.mu.Lock()
	err = store.applyUnlinkLocked(testCLIActor(), "alice", state, true)
	store.mu.Unlock()
	if err == nil {
		t.Fatal("an unlink through a closed store succeeded")
	}
}

// A stored key that does not parse is reported in before.external_subject as
// it stands, with no guessed issuer or subject.
func TestUnlinkAuditDetailsOmitsAnUnparseableKey(t *testing.T) {
	details := unlinkAuditDetails(federatedLinkState{
		id: 1, authSource: AuthSourceOIDC,
		externalSubject: sql.NullString{String: "garbage", Valid: true},
	}, false)

	if _, ok := details["issuer"]; ok {
		t.Errorf("details.issuer = %v, want it omitted", details["issuer"])
	}
	if _, ok := details["subject"]; ok {
		t.Errorf("details.subject = %v, want it omitted", details["subject"])
	}
	assertLinkState(t, details, "before", AuthSourceOIDC, "garbage")
}

// failUserUpdates makes every UPDATE of users fail with an error that is not a
// unique violation, standing in for a full disk or a corrupt page.
func failUserUpdates(t *testing.T, store *AuthStore) {
	t.Helper()

	mustExec(t, store, `CREATE TRIGGER fail_user_update BEFORE UPDATE ON users
        BEGIN SELECT RAISE(ABORT, 'injected update failure'); END`)
}

// breakLinkStateRead renames a column the link state read selects, so the
// read fails with an error other than sql.ErrNoRows.
func breakLinkStateRead(t *testing.T, store *AuthStore) {
	t.Helper()

	mustExec(t, store, "ALTER TABLE users RENAME COLUMN external_subject TO external_subject_moved")
}

func TestLinkFederatedIdentityRecordsStoreFailures(t *testing.T) {
	t.Run("UpdateFails", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		if err := store.CreateUser("alice", linkTestPassword, "", "", ""); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		failUserUpdates(t, store)

		if _, err := store.AsActor(testCLIActor()).LinkFederatedIdentity("alice",
			linkTestIssuer, linkTestSubject, false); err == nil {
			t.Fatal("a link whose UPDATE failed was reported as applied")
		}
		assertFailureEvent(t, store, "user.link_identity", "alice",
			"failed to link alice", true)
	})

	t.Run("ReadFails", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		if err := store.CreateUser("alice", linkTestPassword, "", "", ""); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		breakLinkStateRead(t, store)

		if _, err := store.AsActor(testCLIActor()).LinkFederatedIdentity("alice",
			linkTestIssuer, linkTestSubject, false); err == nil {
			t.Fatal("a link whose account could not be read was reported as applied")
		}
		assertFailureEvent(t, store, "user.link_identity", "alice", "looking up user", false)
	})
}

func TestUnlinkFederatedIdentityRecordsStoreFailures(t *testing.T) {
	t.Run("UpdateFails", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		linkedLocalUser(t, store, "alice")
		failUserUpdates(t, store)

		if _, _, err := store.AsActor(testCLIActor()).UnlinkFederatedIdentity("alice",
			true); err == nil {
			t.Fatal("an unlink whose UPDATE failed was reported as applied")
		}
		assertFailureEvent(t, store, "user.unlink_identity", "alice",
			"failed to unlink alice", true)
	})

	t.Run("ReadFails", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForStore(t)
		defer cleanup()

		linkedLocalUser(t, store, "alice")
		breakLinkStateRead(t, store)

		if _, _, err := store.AsActor(testCLIActor()).UnlinkFederatedIdentity("alice",
			true); err == nil {
			t.Fatal("an unlink whose account could not be read was reported as applied")
		}
		assertFailureEvent(t, store, "user.unlink_identity", "alice", "looking up user", false)
	})
}

// An unlink that cannot hash its unusable password fails before it touches the
// row; bcrypt refuses a cost above its maximum, which stands in for the
// failure here.
func TestApplyUnlinkLockedReportsAHashFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	linkedLocalUser(t, store, "alice")
	state, err := readFederatedLinkState(store.db, "alice")
	if err != nil {
		t.Fatalf("readFederatedLinkState: %v", err)
	}

	store.mu.Lock()
	store.bcryptCost = 99
	err = store.applyUnlinkLocked(testCLIActor(), "alice", state, false)
	store.mu.Unlock()
	if err == nil {
		t.Fatal("an unlink that could not hash its password succeeded")
	}
	assertLinkColumns(t, store, "alice", AuthSourceOIDC, state.externalSubject)
}

func TestRevokeAccountTokensLockedReportsAClosedStore(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	store.mu.Lock()
	_, err := store.revokeAccountTokensLocked(testCLIActor(), "alice")
	store.mu.Unlock()
	if err == nil {
		t.Fatal("counting tokens through a closed store succeeded")
	}
}

func TestInsertFederatedUserLockedReportsAClosedStore(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if err := store.db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	identity := testIdentity()

	store.mu.Lock()
	_, err := store.insertFederatedUserLocked(identity,
		ExternalSubjectKey(identity.Issuer, identity.Subject), "unusable", "")
	store.mu.Unlock()
	if err == nil {
		t.Fatal("provisioning through a closed store succeeded")
	}
}
