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
	"errors"
	"strings"
	"testing"
	"time"
)

// thirdAuditKey is the key a store opens with once its server secret
// has been changed a second time.
var thirdAuditKey = DeriveAuditKey("a third server secret")

// expectTampering checks that the log fails verification as tampered
// with, not as a key mismatch.
func expectTampering(t *testing.T, s *AuthStore) {
	t.Helper()

	_, err := s.VerifyAuditLog()
	if !errors.Is(err, ErrAuditChainBroken) ||
		errors.Is(err, ErrAuditKeyMismatch) {
		t.Fatalf("Expected tampering, got %v", err)
	}
}

// expectKeyMismatch checks that the log fails verification as written
// under another secret.
func expectKeyMismatch(t *testing.T, s *AuthStore) {
	t.Helper()

	if _, err := s.VerifyAuditLog(); !errors.Is(err, ErrAuditKeyMismatch) {
		t.Fatalf("Expected a key mismatch, got %v", err)
	}
}

// expectBoundAnchorRefused checks that a rotated log whose last row
// under the old secret is last reads as tampering, because its
// current-key anchor no longer verifies beside the primary anchor.
func expectBoundAnchorRefused(t *testing.T, s *AuthStore,
	last auditRowState) {
	t.Helper()

	expectTampering(t, s)
	err := s.verifyAuditTailAfterKeyChange(s.db, AuditEvent{ID: last.ID,
		Hash: last.Hash})
	if !errors.Is(err, ErrAuditChainBroken) || !strings.Contains(err.Error(),
		"together with the anchor beside it") {
		t.Errorf("Expected the current-key anchor to be refused, got %v",
			err)
	}
}

// unattendedReanchorAccepts reports whether -confirm-rechain would
// re-anchor a log shown plan, as confirmAuditReanchor decides.
func unattendedReanchorAccepts(plan AuditRechainPlan) bool {
	return plan.KeyMismatch && plan.PreviousKeyGiven && plan.HistoryProven &&
		plan.LaterEventsVerify
}

// writeUnderKeys writes n events under each key in turn, reopening the
// store at dir under the next, and returns the store left open under
// the last key.
func writeUnderKeys(t *testing.T, n int, keys ...[]byte) (*AuthStore,
	string) {
	t.Helper()

	store, dir := newReopenableStore(t)
	recordN(t, store, n)
	for _, key := range keys {
		store.Close()
		store = openWithKey(t, dir, key)
		recordN(t, store, n)
	}

	return store, dir
}

// TestNoRotationLeavesNoCurrentKeyAnchor checks the ordinary case: a log
// written under one secret has the primary anchor only.
func TestNoRotationLeavesNoCurrentKeyAnchor(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 5)
	st := readTail(t, store)
	if st.hasCurrent || !st.namesNewest() || !store.auditTailVerifies(st) {
		t.Fatalf("Expected the primary anchor alone, at the newest row, "+
			"got %+v", st)
	}
	expectVerifies(t, store)
}

// TestCurrentKeyAnchorFollowsARotation checks that the first event under
// a new secret starts the current-key anchor, which then follows each
// event, whilst the primary anchor stays at the last row written under
// the old secret.
func TestCurrentKeyAnchorFollowsARotation(t *testing.T) {
	store, _ := writeUnderKeys(t, 4, rotatedAuditKey)

	st := readTail(t, store)
	if st.anchorID != 4 || !st.hasCurrent || !st.currentNamesNewest() ||
		st.currentID != 8 || !store.currentTailVerifies(st) {
		t.Fatalf("Expected the primary anchor at 4 and the current-key "+
			"anchor at 8, got %+v", st)
	}
	expectKeyMismatch(t, store)
}

// TestRotationCutFromTheNewEventsIsCaught is the case from the review of
// #565: five events under one secret, four under the next, and the
// newest three of those deleted with the sequence written down to match.
// It must read as tampering, and the unattended re-anchor must refuse
// it.
func TestRotationCutFromTheNewEventsIsCaught(t *testing.T) {
	store, dir := newReopenableStore(t)
	recordN(t, store, 5)
	store.Close()
	store = openWithKey(t, dir, rotatedAuditKey)
	recordN(t, store, 4)

	deleteAuditRows(t, store, 7, 8, 9)
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 6
        WHERE name = 'audit_events'`)
	expectTampering(t, store)
	store.Close()

	plan := planWithPreviousKey(t, dir, rotatedAuditKey,
		AuditKeyForTesting())
	if unattendedReanchorAccepts(plan) {
		t.Errorf("Expected the unattended re-anchor to refuse, got %+v",
			plan)
	}
}

// TestRotationLosingEveryNewEventIsCaught checks the other case from the
// review: every event written under the new secret deleted, and the
// server then writing one more. The current-key anchor still names a
// row that is gone, and is not moved on from there.
func TestRotationLosingEveryNewEventIsCaught(t *testing.T) {
	store, _ := writeUnderKeys(t, 4, rotatedAuditKey)
	deleteAuditRows(t, store, 5, 6, 7, 8)
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 4
        WHERE name = 'audit_events'`)

	recordN(t, store, 1)
	if st := readTail(t, store); st.currentID != 8 {
		t.Fatalf("Expected the current-key anchor to stay at 8, got %+v", st)
	}
	expectTampering(t, store)
}

// TestRotationDecoyCannotRestartTheCurrentKeyAnchor checks that the
// decoy cannot be replayed against the current-key anchor: a writer who
// cuts the newest rows, deletes the current-key anchor, points the
// primary at a decoy so that the server starts a new one, and then
// deletes the decoy and puts the saved primary anchor back, is caught,
// because the new current-key anchor is bound to the decoy's.
func TestRotationDecoyCannotRestartTheCurrentKeyAnchor(t *testing.T) {
	store, dir := writeUnderKeys(t, 4, rotatedAuditKey)
	rows := auditRowStates(t, store)
	saved := readTail(t, store)

	deleteAuditRows(t, store, 7, 8)
	tamperDB(t, store.db, "DELETE FROM audit_tail WHERE id = 2")
	tamperDB(t, store.db, `INSERT INTO audit_events (id, occurred_at,
            actor_type, actor_name, action, outcome, prev_hash, hash,
            hash_version)
        VALUES (7, ?, 'system', 'system', 'user.create', 'success',
            'decoy', ?, 3)`,
		time.Now().UTC().Format(auditTimeLayout), rows[5].Hash)
	tamperDB(t, store.db, `UPDATE audit_tail SET event_id = 7,
        event_hash = ?, mac = 'forged' WHERE id = 1`, rows[5].Hash)
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 7
        WHERE name = 'audit_events'`)

	recordN(t, store, 1)
	if st := readTail(t, store); !st.hasCurrent || st.currentID != 8 {
		t.Fatalf("Expected the server to start the current-key anchor at "+
			"8, got %+v", st)
	}

	deleteAuditRows(t, store, 7)
	tamperDB(t, store.db, `UPDATE audit_tail SET event_id = ?,
        event_hash = ?, mac = ? WHERE id = 1`, saved.anchorID,
		saved.anchorHash, saved.anchorMAC)
	expectBoundAnchorRefused(t, store, rows[3])
	store.Close()

	plan := planWithPreviousKey(t, dir, rotatedAuditKey,
		AuditKeyForTesting())
	if unattendedReanchorAccepts(plan) {
		t.Errorf("Expected the unattended re-anchor to refuse, got %+v",
			plan)
	}
}

// TestPromotionDecoyIsCaught checks the same decoy against promotion: a
// current-key anchor pointed at a decoy is promoted when the server
// writes its next event, and neither leaving the promoted value in
// place nor putting the saved primary anchor back gets past the
// verifier once the decoy is deleted.
func TestPromotionDecoyIsCaught(t *testing.T) {
	store, _ := writeUnderKeys(t, 4, rotatedAuditKey)
	rows := auditRowStates(t, store)
	saved := readTail(t, store)

	deleteAuditRows(t, store, 7, 8)
	tamperDB(t, store.db, `INSERT INTO audit_events (id, occurred_at,
            actor_type, actor_name, action, outcome, prev_hash, hash,
            hash_version)
        VALUES (7, ?, 'system', 'system', 'user.create', 'success',
            'decoy', ?, 3)`,
		time.Now().UTC().Format(auditTimeLayout), rows[5].Hash)
	tamperDB(t, store.db, `UPDATE audit_tail SET event_id = 7,
        event_hash = ?, mac = 'forged' WHERE id = 2`, rows[5].Hash)
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 7
        WHERE name = 'audit_events'`)

	recordN(t, store, 1)
	st := readTail(t, store)
	if st.anchorID != 7 || st.anchorMAC != "forged" || st.currentID != 8 {
		t.Fatalf("Expected the decoy anchor to be promoted, got %+v", st)
	}

	deleteAuditRows(t, store, 7)
	expectTampering(t, store)

	tamperDB(t, store.db, `UPDATE audit_tail SET event_id = ?,
        event_hash = ?, mac = ? WHERE id = 1`, saved.anchorID,
		saved.anchorHash, saved.anchorMAC)
	expectBoundAnchorRefused(t, store, rows[3])
}

// TestThreeSecretsReadAsAKeyMismatch checks the design decision recorded
// in audit_tail.go: a log written under three secrets with no re-anchor
// between them promotes the current-key anchor at the first event under
// the third, and reads as a key mismatch, as it did before the tail
// anchor existed, both before and after that event.
func TestThreeSecretsReadAsAKeyMismatch(t *testing.T) {
	store, dir := writeUnderKeys(t, 3, rotatedAuditKey)
	store.Close()

	store = openWithKey(t, dir, thirdAuditKey)
	expectKeyMismatch(t, store)

	recordN(t, store, 3)
	st := readTail(t, store)
	if st.anchorID != 6 || st.currentID != 9 || !st.currentNamesNewest() ||
		!store.currentTailVerifies(st) {
		t.Fatalf("Expected the primary anchor at 6 and the current-key "+
			"anchor at 9, got %+v", st)
	}
	expectKeyMismatch(t, store)

	// A cut from the events under the third secret is still caught.
	deleteAuditRows(t, store, 9)
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 8
        WHERE name = 'audit_events'`)
	expectTampering(t, store)
}

// TestReplayedLegacyPurgeIsCaughtByTheTail checks the case the admin
// guide describes: a purge event written before purges recorded the
// head of the log, saved and put back into an emptied log, after which
// the server writes one more event. The chain alone accepts that log;
// the tail anchor does not, whether or not the secret has changed.
func TestReplayedLegacyPurgeIsCaughtByTheTail(t *testing.T) {
	build := func(t *testing.T) (*AuthStore, string) {
		t.Helper()

		store, dir := newReopenableStore(t)
		recordN(t, store, 3)
		legacy := newEvent(systemActor, auditActionPurge, "", nil, "",
			map[string]any{"older_than": time.Now().UTC().
				Add(-time.Hour).Format(time.RFC3339), "removed": 1})
		if err := store.recordAuditInOwnTx(legacy); err != nil {
			t.Fatalf("Failed to record a legacy purge event: %v", err)
		}
		saveAuditRow(t, store, legacy.ID)
		recordN(t, store, 2)

		return store, dir
	}
	replay := func(t *testing.T, store *AuthStore) {
		t.Helper()

		tamperDB(t, store.db, "DELETE FROM audit_events")
		replaySavedAuditRow(t, store, nil)
		recordN(t, store, 1)
	}

	t.Run("one secret", func(t *testing.T) {
		store, _ := build(t)
		replay(t, store)
		expectTailError(t, store, "tail missing")
	})

	t.Run("after a rotation", func(t *testing.T) {
		store, dir := build(t)
		store.Close()
		store = openWithKey(t, dir, rotatedAuditKey)
		recordN(t, store, 2)
		replay(t, store)
		expectTampering(t, store)
	})
}

// TestReanchorClearsTheCurrentKeyAnchor checks that a re-anchor of a
// rotated log leaves the primary anchor alone, at its own event.
func TestReanchorClearsTheCurrentKeyAnchor(t *testing.T) {
	dir, store, _ := rotatedLog(t, 3, 2)
	if st := readTail(t, store); !st.hasCurrent {
		t.Fatalf("Expected a current-key anchor before the re-anchor, got "+
			"%+v", st)
	}
	store.Close()

	reanchor(t, dir, rotatedAuditKey)
	store = openWithKey(t, dir, rotatedAuditKey)
	st := readTail(t, store)
	if st.hasCurrent || !st.namesNewest() || !store.auditTailVerifies(st) {
		t.Errorf("Expected the primary anchor alone, at the re-anchor "+
			"event, got %+v", st)
	}
	expectVerifies(t, store)
}

// TestResetAuditTailReportsAFailedDelete checks that a current-key
// anchor that cannot be removed fails the re-chain.
func TestResetAuditTailReportsAFailedDelete(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 1)
	tamperDB(t, store.db, `INSERT INTO audit_tail (id, event_id, event_hash,
        mac) VALUES (2, 1, 'h', 'm')`)
	tamperDB(t, store.db, `CREATE TRIGGER audit_tail_keep BEFORE DELETE
        ON audit_tail BEGIN SELECT RAISE(ABORT, 'kept'); END`)
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin: %v", err)
	}
	defer tx.Rollback()
	err = store.resetAuditTail(tx, &AuditEvent{ID: 1, Hash: "h"})
	if err == nil || !strings.Contains(err.Error(), "current-key") {
		t.Errorf("Expected the failed delete to be reported, got %v", err)
	}
}

// TestApplyAuditTailMoveReportsFailedWrites checks each move's error
// path, and that a key-less store refuses to sign a current-key anchor.
func TestApplyAuditTailMoveReportsFailedWrites(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	tamperDB(t, store.db, "DROP TABLE audit_tail")
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin: %v", err)
	}
	defer tx.Rollback()

	ev := &AuditEvent{ID: 1, Hash: "h"}
	for _, move := range []auditTailMove{auditTailAdvance,
		auditTailAdvanceCurrent, auditTailPromote} {
		if err := store.applyAuditTailMove(tx, move, auditTailState{},
			ev); err == nil {
			t.Errorf("Expected move %d to report the failed write", move)
		}
	}
	if err := store.applyAuditTailMove(tx, auditTailStay, auditTailState{},
		ev); err != nil {
		t.Errorf("Expected staying put to write nothing, got %v", err)
	}

	keyless := &AuthStore{}
	if err := keyless.writeAuditTailCurrent(nil, ev, 0, "", ""); !errors.Is(
		err, errNoAuditKey) {
		t.Errorf("Expected an empty key to be refused, got %v", err)
	}
	if keyless.currentTailVerifies(auditTailState{hasAnchor: true}) {
		t.Error("Expected nothing to verify under an empty key")
	}
}

// TestCurrentKeyAnchorMACBindsThePrimary checks that the current-key
// anchor's HMAC changes with each field of the primary anchor.
func TestCurrentKeyAnchorMACBindsThePrimary(t *testing.T) {
	key := AuditKeyForTesting()
	base, err := auditTailCurrentMAC(key, 2, "h2", 1, "h1", "m1")
	if err != nil {
		t.Fatalf("auditTailCurrentMAC failed: %v", err)
	}
	plain, _ := auditTailMAC(key, 2, "h2")
	if base == plain {
		t.Error("Expected the current-key rendering to differ from the " +
			"primary's")
	}
	for _, other := range []struct {
		id        int64
		hash, mac string
		field     string
	}{
		{3, "h1", "m1", "id"}, {1, "hx", "m1", "hash"},
		{1, "h1", "mx", "mac"},
	} {
		got, _ := auditTailCurrentMAC(key, 2, "h2", other.id, other.hash,
			other.mac)
		if got == base {
			t.Errorf("Expected the primary anchor's %s to be covered",
				other.field)
		}
	}
}

// TestVerifyAuditTailCurrentStates checks each state of the current-key
// anchor the rotation-aware tail check meets, on states built directly.
func TestVerifyAuditTailCurrentStates(t *testing.T) {
	store := &AuthStore{auditKey: AuditKeyForTesting()}
	primaryMAC, _ := auditTailMAC(rotatedAuditKey, 5, "h5")
	signed, _ := auditTailCurrentMAC(store.auditKey, 9, "h9", 5, "h5",
		primaryMAC)
	lastFailing := AuditEvent{ID: 5, Hash: "h5"}
	base := auditTailState{
		hasNewest: true, newestID: 9, newestHash: "h9",
		newestVersion: auditTailHashVersion,
		hasAnchor:     true, anchorID: 5, anchorHash: "h5",
		anchorMAC:  primaryMAC,
		hasCurrent: true, currentID: 9, currentHash: "h9",
		currentMAC: signed,
	}

	tests := []struct {
		name  string
		alter func(st *auditTailState)
		want  string
	}{
		{"current-key anchor at the newest row", func(*auditTailState) {},
			""},
		{"no events under the key in use", func(st *auditTailState) {
			st.newestID, st.newestHash = 5, "h5"
			st.hasCurrent = false
		}, ""},
		{"current-key anchor left after its events went",
			func(st *auditTailState) {
				st.newestID, st.newestHash = 5, "h5"
			}, "no event after 5"},
		{"current-key anchor deleted", func(st *auditTailState) {
			st.hasCurrent = false
		}, "holds no anchor for them"},
		{"current-key anchor behind the newest row",
			func(st *auditTailState) {
				st.newestID, st.newestHash = 10, "h10"
			}, "records event 9 as the newest"},
		{"current-key anchor forged", func(st *auditTailState) {
			st.currentMAC = "forged"
		}, "together with the anchor beside it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := base
			tt.alter(&st)
			err := store.verifyAuditTailCurrent(st, lastFailing)
			if tt.want == "" {
				if err != nil {
					t.Errorf("Expected the state to pass, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrAuditChainBroken) ||
				!strings.Contains(err.Error(), tt.want) {
				t.Errorf("Expected %q, got %v", tt.want, err)
			}
		})
	}
}

// singleSlotTailDDL is audit_tail as pre-release builds of the tail
// anchor created it, with room for the primary anchor only.
const singleSlotTailDDL = `CREATE TABLE audit_tail (
        id INTEGER PRIMARY KEY CHECK (id = 1),
        event_id INTEGER NOT NULL,
        event_hash TEXT NOT NULL,
        mac TEXT NOT NULL
    )`

// auditTailDefinition reads the CREATE TABLE statement for audit_tail.
func auditTailDefinition(t *testing.T, s *AuthStore) string {
	t.Helper()

	var definition string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master
         WHERE type = 'table' AND name = 'audit_tail'`).
		Scan(&definition); err != nil {
		t.Fatalf("Failed to read the audit_tail definition: %v", err)
	}

	return definition
}

// TestMigrateAuditTailSlots checks that a single-slot audit_tail is
// rebuilt with room for both anchors when the store is opened, keeping
// the primary anchor, and that a rotation then works on it.
func TestMigrateAuditTailSlots(t *testing.T) {
	store, dir := newReopenableStore(t)
	recordN(t, store, 3)
	before := readTail(t, store)
	tamperDB(t, store.db, "DROP TABLE audit_tail")
	tamperDB(t, store.db, singleSlotTailDDL)
	tamperDB(t, store.db, `INSERT INTO audit_tail VALUES (1, ?, ?, ?)`,
		before.anchorID, before.anchorHash, before.anchorMAC)
	if _, err := store.db.Exec(`INSERT INTO audit_tail
        VALUES (2, 1, 'h', 'm')`); err == nil {
		t.Fatal("Expected the single-slot table to refuse a second row")
	}
	store.Close()

	store = openWithKey(t, dir, rotatedAuditKey)
	if def := auditTailDefinition(t, store); !strings.Contains(
		normalizeSchemaSQL(def), "CHECK (id IN (1, 2))") {
		t.Fatalf("Expected the two-slot definition, got %s", def)
	}
	after := readTail(t, store)
	if after.anchorID != before.anchorID ||
		after.anchorHash != before.anchorHash ||
		after.anchorMAC != before.anchorMAC {
		t.Errorf("Expected the primary anchor to survive, got %+v", after)
	}

	recordN(t, store, 1)
	if st := readTail(t, store); !st.hasCurrent {
		t.Errorf("Expected the rotation to start a current-key anchor, got "+
			"%+v", st)
	}
	expectKeyMismatch(t, store)

	if err := store.migrateAuditTailSchema(); err != nil {
		t.Errorf("Expected a second migration to do nothing, got %v", err)
	}
}

// TestMigrateAuditTailSlotsReportsFailures checks that a migration that
// cannot finish leaves the single-slot table as it was, and that an
// unreadable schema is reported.
func TestMigrateAuditTailSlotsReportsFailures(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	tamperDB(t, store.db, "DROP TABLE audit_tail")
	tamperDB(t, store.db, singleSlotTailDDL)
	tamperDB(t, store.db, "CREATE TABLE audit_tail_legacy (x)")
	if err := store.migrateAuditTailSchema(); err == nil ||
		!strings.Contains(err.Error(), "the current definition") {
		t.Errorf("Expected the blocked rename to be reported, got %v", err)
	}
	if def := auditTailDefinition(t, store); !strings.Contains(
		normalizeSchemaSQL(def), "CHECK (id = 1)") {
		t.Errorf("Expected the failed migration to roll back, got %s", def)
	}

	tamperDB(t, store.db, "DROP TABLE audit_tail")
	if err := store.migrateAuditTailSchema(); err == nil {
		t.Error("Expected a missing audit_tail table to be reported")
	}

	store.db.Close()
	if err := store.migrateAuditTailSchema(); err == nil {
		t.Error("Expected a closed database to be reported")
	}
}

// TestOpenReportsAFailedTailMigration checks that a store whose
// single-slot audit_tail cannot be rebuilt refuses to open, rather than
// running with a table that would refuse the current-key anchor.
func TestOpenReportsAFailedTailMigration(t *testing.T) {
	store, dir := newReopenableStore(t)
	tamperDB(t, store.db, "DROP TABLE audit_tail")
	tamperDB(t, store.db, singleSlotTailDDL)
	tamperDB(t, store.db, "CREATE TABLE audit_tail_legacy (x)")
	store.Close()

	if _, err := reopenStore(t, dir); err == nil ||
		!strings.Contains(err.Error(), "the current definition") {
		t.Errorf("Expected the open to fail on the migration, got %v", err)
	}
}

// TestResetAuditTailReportsAFailedWrite checks that a primary anchor
// that cannot be written fails the reset before the current-key anchor
// is touched.
func TestResetAuditTailReportsAFailedWrite(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	tamperDB(t, store.db, "DROP TABLE audit_tail")
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin: %v", err)
	}
	defer tx.Rollback()
	if err := store.resetAuditTail(tx, &AuditEvent{ID: 1,
		Hash: "h"}); err == nil {
		t.Error("Expected the failed write to be reported")
	}
}

// TestRotationMomentReplayIsTheDocumentedLimit pins the limit the
// documentation states: every event written since a rotation deleted,
// with the current-key anchor and the sequence to match, returns the log
// to the moment of the rotation. That reads as a key mismatch; the
// unattended re-anchor refuses it while no event verifies under the
// current secret, and accepts it once the server has written one.
func TestRotationMomentReplayIsTheDocumentedLimit(t *testing.T) {
	store, dir := writeUnderKeys(t, 4, rotatedAuditKey)
	deleteAuditRows(t, store, 5, 6, 7, 8)
	tamperDB(t, store.db, `DELETE FROM audit_tail WHERE id = 2`)
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 4
        WHERE name = 'audit_events'`)
	expectKeyMismatch(t, store)
	store.Close()

	plan := planWithPreviousKey(t, dir, rotatedAuditKey,
		AuditKeyForTesting())
	if plan.LaterEventsVerify || unattendedReanchorAccepts(plan) {
		t.Errorf("Expected the unattended re-anchor to refuse with no "+
			"later event, got %+v", plan)
	}

	store = openWithKey(t, dir, rotatedAuditKey)
	recordN(t, store, 1)
	expectKeyMismatch(t, store)
	store.Close()

	plan = planWithPreviousKey(t, dir, rotatedAuditKey,
		AuditKeyForTesting())
	if !unattendedReanchorAccepts(plan) {
		t.Errorf("Expected the unattended re-anchor to accept once a "+
			"later event verifies, got %+v", plan)
	}
}

// TestRotatedLogWithoutAnAnchorIsAccepted checks that a rotated log no
// release that keeps the tail anchor has written to, so with no anchor
// at all, has nothing for the rotated tail check to hold it to.
func TestRotatedLogWithoutAnAnchorIsAccepted(t *testing.T) {
	store, _ := newReopenableStore(t)
	st := auditTailState{hasNewest: true, newestID: 4,
		newestVersion: auditTailHashVersion - 1}
	if err := store.checkRotatedAuditTail(st,
		AuditEvent{ID: 2}); err != nil {
		t.Errorf("Expected a log with no anchor to be accepted, got %v",
			err)
	}
}
