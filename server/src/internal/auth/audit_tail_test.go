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
	"errors"
	"strings"
	"testing"
	"time"
)

// recordN records n events through the store, as the server would.
func recordN(t *testing.T, s *AuthStore, n int) {
	t.Helper()

	for i := 0; i < n; i++ {
		mustRecord(t, s, newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil))
	}
}

// readTail returns the tail state as the verifier sees it.
func readTail(t *testing.T, s *AuthStore) auditTailState {
	t.Helper()

	st, err := readAuditTailState(s.db)
	if err != nil {
		t.Fatalf("Failed to read the tail anchor: %v", err)
	}

	return st
}

// tamperDB runs a statement that stands in for tampering with auth.db.
func tamperDB(t *testing.T, db *sql.DB, stmt string, args ...any) {
	t.Helper()

	if _, err := db.Exec(stmt, args...); err != nil {
		t.Fatalf("Failed to run %q: %v", stmt, err)
	}
}

// expectTailError checks that the chain fails verification with an
// error wrapping ErrAuditChainBroken and containing want.
func expectTailError(t *testing.T, s *AuthStore, want string) {
	t.Helper()

	_, _, err := s.VerifyAuditChain()
	if !errors.Is(err, ErrAuditChainBroken) {
		t.Fatalf("Expected a broken chain, got %v", err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Expected the error to say %q, got %v", want, err)
	}
}

// expectVerifies checks that the chain verifies.
func expectVerifies(t *testing.T, s *AuthStore) {
	t.Helper()

	if _, firstBad, err := s.VerifyAuditChain(); err != nil || firstBad != 0 {
		t.Fatalf("Expected the chain to verify, got firstBad=%d err=%v",
			firstBad, err)
	}
}

// insertVersion2Log appends n rows hashed under version 2, as a build
// from before the tail anchor wrote them, with no anchor.
func insertVersion2Log(t *testing.T, s *AuthStore, n int) {
	t.Helper()

	at := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < n; i++ {
		ev := newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "old", nil)
		ev.OccurredAt = at.Add(time.Duration(i) * time.Second)
		insertVersion2Event(t, s, ev)
	}
}

// insertVersion2Event appends ev hashed under version 2 and linked to
// the newest row, as a build from before the tail anchor wrote it,
// without touching audit_tail.
func insertVersion2Event(t *testing.T, s *AuthStore, ev *AuditEvent) {
	t.Helper()

	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	ev.Outcome = OutcomeSuccess

	var prevHash string
	err := s.db.QueryRow(
		"SELECT hash FROM audit_events ORDER BY id DESC LIMIT 1").
		Scan(&prevHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Failed to read the previous hash: %v", err)
	}
	ev.PrevHash = prevHash
	ev.HashVersion = 2
	ev.Hash, err = auditHashV2(ev, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("auditHashV2 failed: %v", err)
	}

	tamperDB(t, s.db, `
        INSERT INTO audit_events (
            occurred_at, actor_type, actor_id, actor_name, action,
            target_type, target_id, target_name, outcome, details,
            prev_hash, hash, hash_version
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.OccurredAt.UTC().Format(auditTimeLayout), string(ev.ActorType),
		ev.ActorID, ev.ActorName, ev.Action, nullableText(ev.TargetType),
		ev.TargetID, nullableText(ev.TargetName), string(ev.Outcome),
		nullableText(string(ev.Details)), ev.PrevHash, ev.Hash,
		ev.HashVersion)
}

// TestTailAnchorFollowsEachEvent checks that every event moves the
// anchor on to itself, in the same transaction.
func TestTailAnchorFollowsEachEvent(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	if st := readTail(t, store); st.hasAnchor || st.hasNewest {
		t.Fatalf("Expected a fresh log with no anchor, got %+v", st)
	}

	for i := 1; i <= 3; i++ {
		ev := newEvent(systemActor, "user.create", "user", nil, "alice", nil)
		mustRecord(t, store, ev)

		st := readTail(t, store)
		if !st.namesNewest() || st.anchorID != ev.ID || st.anchorHash != ev.Hash {
			t.Fatalf("Expected the anchor to name event %d, got %+v", ev.ID, st)
		}
		if !store.auditTailVerifies(st) {
			t.Fatalf("Expected the anchor for event %d to verify", ev.ID)
		}
	}
	expectVerifies(t, store)
}

// TestVerifyDetectsTruncationWithSequenceRewritten is the attack of
// issue #544: delete the newest rows and write sqlite_sequence down to
// the surviving maximum, which the sequence comparison alone accepts.
func TestVerifyDetectsTruncationWithSequenceRewritten(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 5)
	expectVerifies(t, store)

	tamperDB(t, store.db, "DELETE FROM audit_events WHERE id > 3")
	tamperDB(t, store.db,
		"UPDATE sqlite_sequence SET seq = 3 WHERE name = 'audit_events'")

	if err := checkAuditTail(3, sql.NullInt64{Int64: 3, Valid: true}); err != nil {
		t.Fatalf("Expected the sequence comparison alone to agree, got %v", err)
	}
	expectTailError(t, store,
		"tail anchor records event 5 as the newest, but the newest event is now 3")
}

// TestTruncationEvidenceSurvivesLaterEvents checks that events written
// after a truncation do not move a stranded anchor on, so the deletion
// is still reported however much the server writes afterwards.
func TestTruncationEvidenceSurvivesLaterEvents(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 4)
	tamperDB(t, store.db, "DELETE FROM audit_events WHERE id > 2")
	tamperDB(t, store.db,
		"UPDATE sqlite_sequence SET seq = 2 WHERE name = 'audit_events'")

	recordN(t, store, 3)

	if st := readTail(t, store); st.anchorID != 4 {
		t.Errorf("Expected the anchor to stay on event 4, got %d", st.anchorID)
	}
	expectTailError(t, store, "tail anchor records event 4 as the newest")
}

// TestVerifyDetectsADeletedAnchor checks that deleting the anchor along
// with the tail does not pass, because the newest surviving row is
// version 3 and was written by a build that keeps the anchor.
func TestVerifyDetectsADeletedAnchor(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 4)
	tamperDB(t, store.db, "DELETE FROM audit_events WHERE id > 2")
	tamperDB(t, store.db,
		"UPDATE sqlite_sequence SET seq = 2 WHERE name = 'audit_events'")
	tamperDB(t, store.db, "DELETE FROM audit_tail")

	expectTailError(t, store, "tail anchor missing")

	// A later event must not start a new anchor over the gap.
	recordN(t, store, 1)
	if st := readTail(t, store); st.hasAnchor {
		t.Errorf("Expected no anchor to be written, got %+v", st)
	}
	expectTailError(t, store, "tail anchor missing")

	// Nor must a restart.
	if err := store.seedAuditTail(); err != nil {
		t.Fatalf("seedAuditTail failed: %v", err)
	}
	expectTailError(t, store, "tail anchor missing")
}

// TestVerifyDetectsAnAlteredAnchor checks that an anchor rewritten to
// name the new newest row, without the secret, is reported, and that
// recordAudit leaves it alone rather than signing over it.
func TestVerifyDetectsAnAlteredAnchor(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 4)
	tamperDB(t, store.db, "DELETE FROM audit_events WHERE id > 2")
	tamperDB(t, store.db,
		"UPDATE sqlite_sequence SET seq = 2 WHERE name = 'audit_events'")
	tamperDB(t, store.db, `UPDATE audit_tail SET event_id = 2,
        event_hash = (SELECT hash FROM audit_events WHERE id = 2)`)

	expectTailError(t, store, "tail anchor altered")

	recordN(t, store, 1)
	if st := readTail(t, store); st.anchorID != 2 {
		t.Errorf("Expected the altered anchor to be left alone, got %+v", st)
	}
	expectTailError(t, store, "tail anchor records event 2 as the newest")
}

// TestVerifyDetectsAnAnchorOverAnEmptyLog checks that deleting every
// event, and the sequence row with them, is reported while the anchor
// survives.
func TestVerifyDetectsAnAnchorOverAnEmptyLog(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 3)
	tamperDB(t, store.db, "DELETE FROM audit_events")
	tamperDB(t, store.db,
		"DELETE FROM sqlite_sequence WHERE name = 'audit_events'")

	expectTailError(t, store, "the log is empty, but the tail anchor records event 3")
}

// TestEmptiedLogDoesNotStartAnAnchor checks that the first event
// written to a log that once held events, anchor and all deleted, does
// not start a new anchor: sqlite_sequence remembers the ids issued.
func TestEmptiedLogDoesNotStartAnAnchor(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 3)
	tamperDB(t, store.db, "DELETE FROM audit_events")
	tamperDB(t, store.db, "DELETE FROM audit_tail")

	recordN(t, store, 1)
	if st := readTail(t, store); st.hasAnchor {
		t.Errorf("Expected no anchor over an emptied log, got %+v", st)
	}
	// The head check reports the moved genesis first; the tail check
	// has its own verdict on the same log.
	if _, _, err := store.VerifyAuditChain(); !errors.Is(err,
		ErrAuditChainBroken) {
		t.Errorf("Expected the emptied log to fail, got %v", err)
	}
	if err := store.verifyAuditTailAnchor(store.db); err == nil ||
		!strings.Contains(err.Error(), "tail anchor missing") {
		t.Errorf("Expected the missing anchor to be reported, got %v", err)
	}
}

// TestSeedAnchorsAVersion2Log checks the migration: a log written
// before the anchor existed is anchored when the store is opened, and
// then verifies and moves on as a new one would.
func TestSeedAnchorsAVersion2Log(t *testing.T) {
	store, dir := newReopenableStore(t)
	insertVersion2Log(t, store, 3)

	if st := readTail(t, store); st.hasAnchor {
		t.Fatalf("Expected no anchor before the reopen, got %+v", st)
	}
	expectVerifies(t, store)

	reopened, err := reopenStore(t, dir)
	if err != nil {
		t.Fatalf("Failed to reopen the store: %v", err)
	}
	st := readTail(t, reopened)
	if !st.namesNewest() || st.anchorID != 3 || !reopened.auditTailVerifies(st) {
		t.Fatalf("Expected the reopen to anchor event 3, got %+v", st)
	}
	expectVerifies(t, reopened)

	recordN(t, reopened, 1)
	if st := readTail(t, reopened); st.anchorID != 4 {
		t.Errorf("Expected the anchor to follow the next event, got %+v", st)
	}
	expectVerifies(t, reopened)
}

// TestSeedRefusesATruncatedVersion2Log checks that a version 2 log
// whose sequence disagrees with its newest row is not anchored, since
// an anchor would then vouch for the truncation.
func TestSeedRefusesATruncatedVersion2Log(t *testing.T) {
	store, dir := newReopenableStore(t)
	insertVersion2Log(t, store, 4)
	tamperDB(t, store.db, "DELETE FROM audit_events WHERE id > 2")

	reopened, err := reopenStore(t, dir)
	if err != nil {
		t.Fatalf("Failed to reopen the store: %v", err)
	}
	if st := readTail(t, reopened); st.hasAnchor {
		t.Fatalf("Expected no anchor over a truncated log, got %+v", st)
	}
	expectTailError(t, reopened, "audit chain tail missing")

	// The next event is version 3, so the missing anchor is now
	// reported in its own words rather than by the sequence alone.
	recordN(t, reopened, 1)
	if st := readTail(t, reopened); st.hasAnchor {
		t.Errorf("Expected no anchor after the next event, got %+v", st)
	}
}

// TestSeedRefusesARowTheKeyDoesNotVerify checks that a version 2 log
// first opened under the wrong secret is not anchored under that
// secret, so that reopening it under the right one verifies rather
// than reporting an altered anchor.
func TestSeedRefusesARowTheKeyDoesNotVerify(t *testing.T) {
	store, dir := newReopenableStore(t)
	insertVersion2Log(t, store, 3)

	wrong, err := NewAuthStore(dir, 0, 0, rotatedAuditKey)
	if err != nil {
		t.Fatalf("Failed to open under the wrong secret: %v", err)
	}
	if st := readTail(t, wrong); st.hasAnchor {
		t.Errorf("Expected no anchor under the wrong secret, got %+v", st)
	}
	if err := wrong.Close(); err != nil {
		t.Fatalf("Failed to close the store: %v", err)
	}

	reopened, err := reopenStore(t, dir)
	if err != nil {
		t.Fatalf("Failed to reopen the store: %v", err)
	}
	st := readTail(t, reopened)
	if st.anchorID != 3 || !reopened.auditTailVerifies(st) {
		t.Fatalf("Expected the right secret to anchor event 3, got %+v", st)
	}
	expectVerifies(t, reopened)
}

// TestSeedRefusesWithoutASequenceTable checks that a version 2 log in a
// database with no sqlite_sequence table is not anchored.
func TestSeedRefusesWithoutASequenceTable(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open in-memory database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tamperDB(t, db, `CREATE TABLE audit_events (id INTEGER PRIMARY KEY,
        hash TEXT, hash_version INTEGER)`)
	tamperDB(t, db, auditTailDDL)
	tamperDB(t, db, "INSERT INTO audit_events VALUES (7, 'h', 2)")

	store := &AuthStore{db: db, auditKey: AuditKeyForTesting()}
	if err := store.seedAuditTail(); err != nil {
		t.Fatalf("seedAuditTail failed: %v", err)
	}
	if st := readTail(t, store); st.hasAnchor {
		t.Errorf("Expected no anchor, got %+v", st)
	}

	// The sequence table being absent also reads as never having
	// issued an id.
	unused, err := auditSequenceUnused(db)
	if err != nil || !unused {
		t.Errorf("Expected an unused sequence, got %v and %v", unused, err)
	}
}

// TestSeedLeavesOtherLogsAlone checks that the open-time seed writes
// nothing for an empty log, one already anchored, or one whose newest
// row is not version 2.
func TestSeedLeavesOtherLogsAlone(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	if err := store.seedAuditTail(); err != nil {
		t.Fatalf("seedAuditTail failed on an empty log: %v", err)
	}
	if st := readTail(t, store); st.hasAnchor {
		t.Fatalf("Expected no anchor on an empty log, got %+v", st)
	}

	recordN(t, store, 2)
	before := readTail(t, store)
	if err := store.seedAuditTail(); err != nil {
		t.Fatalf("seedAuditTail failed on an anchored log: %v", err)
	}
	if after := readTail(t, store); after != before {
		t.Errorf("Expected the anchor to be left alone, got %+v", after)
	}
}

// TestSeedReportsAClosedDatabase checks that the seed returns a
// database failure rather than skipping the anchor quietly.
func TestSeedReportsAClosedDatabase(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	store.db.Close()
	if err := store.seedAuditTail(); err == nil {
		t.Error("Expected an error from a closed database")
	}
	if err := store.verifyAuditTailAnchor(store.db); err == nil {
		t.Error("Expected verifyAuditTailAnchor to report a closed database")
	}
}

// TestSeedReportsAMissingTailTable checks the read failures inside the
// seed transaction.
func TestSeedReportsAMissingTailTable(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	tamperDB(t, store.db, "DROP TABLE audit_tail")
	if err := store.seedAuditTail(); err == nil {
		t.Error("Expected an error with audit_tail missing")
	}
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin: %v", err)
	}
	defer tx.Rollback()
	if err := store.recordAudit(tx, newEvent(systemActor, "user.create",
		"user", nil, "alice", nil)); err == nil {
		t.Error("Expected recordAudit to fail with audit_tail missing")
	}
}

// TestTailAnchorStaysWithARotatedSecret checks the key-change case: an
// anchor written under the old secret is not moved on under the new one,
// and the tail check for a log with the shape of a rotation accepts it
// only where it names the last row written under the old secret.
func TestTailAnchorStaysWithARotatedSecret(t *testing.T) {
	store, dir := newReopenableStore(t)
	recordN(t, store, 2)

	rotated, err := NewAuthStore(dir, 0, 0, rotatedAuditKey)
	if err != nil {
		t.Fatalf("Failed to open under a rotated secret: %v", err)
	}
	defer rotated.Close()

	st := readTail(t, rotated)
	if rotated.auditTailVerifies(st) {
		t.Fatal("Expected the old anchor not to verify under the new secret")
	}
	if err := rotated.verifyAuditTailAnchor(rotated.db); err == nil {
		t.Error("Expected an anchor that does not verify to be refused")
	}
	last := AuditEvent{ID: st.anchorID, Hash: st.anchorHash}
	if err := rotated.verifyAuditTailAfterKeyChange(rotated.db,
		last); err != nil {
		t.Errorf("Expected an anchor naming the last failing row to be "+
			"accepted before any new event, got %v", err)
	}

	recordN(t, rotated, 1)
	after := readTail(t, rotated)
	if after.anchorID != st.anchorID || after.anchorHash != st.anchorHash ||
		after.anchorMAC != st.anchorMAC {
		t.Errorf("Expected the anchor to stay where the old secret left "+
			"it, got %+v", after)
	}
	if err := rotated.verifyAuditTailAfterKeyChange(rotated.db,
		last); err != nil {
		t.Errorf("Expected a stranded anchor naming the last failing row "+
			"to be accepted, got %v", err)
	}
	newest := AuditEvent{ID: after.newestID, Hash: after.newestHash}
	if err := rotated.verifyAuditTailAfterKeyChange(rotated.db,
		newest); !errors.Is(err, ErrAuditChainBroken) {
		t.Errorf("Expected an anchor naming another row to be refused, "+
			"got %v", err)
	}

	if _, err := rotated.VerifyAuditLog(); !errors.Is(err,
		ErrAuditKeyMismatch) {
		t.Errorf("Expected a rotation to read as a key mismatch, got %v", err)
	}
}

// TestRotationDoesNotExcuseAnAlteredAnchor checks the other half of the
// rotation rule: when the newest row verifies but the anchor does not,
// the anchor was altered and is not moved on.
func TestRotationDoesNotExcuseAnAlteredAnchor(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 2)
	tamperDB(t, store.db, "UPDATE audit_tail SET mac = 'forged'")

	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin: %v", err)
	}
	defer tx.Rollback()
	st, err := readAuditTailState(tx)
	if err != nil {
		t.Fatalf("readAuditTailState failed: %v", err)
	}
	move, err := store.planAuditTailMove(tx, st)
	if err != nil || move != auditTailStay {
		t.Errorf("Expected an altered anchor not to move, got %v and %v",
			move, err)
	}
}

// TestNewestRowCheckTreatsAMissingRowAsFailing checks the helper's
// answer for a row that has gone between the reads.
func TestNewestRowCheckTreatsAMissingRowAsFailing(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	ok, err := store.newestAuditRowVerifies(store.db,
		auditTailState{hasNewest: true, newestID: 99})
	if err != nil || ok {
		t.Errorf("Expected a missing row to read as not verifying, got %v "+
			"and %v", ok, err)
	}

	store.db.Close()
	if _, err := store.newestAuditRowVerifies(store.db,
		auditTailState{newestID: 1}); err == nil {
		t.Error("Expected a closed database to be reported")
	}
}

// TestRechainResetsTheTailAnchor checks that the operator's re-chain
// accepts a truncated log and leaves an anchor that verifies, which is
// the documented way out once the deletion has been investigated.
func TestRechainResetsTheTailAnchor(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 4)
	tamperDB(t, store.db, "DELETE FROM audit_events WHERE id > 2")
	tamperDB(t, store.db,
		"UPDATE sqlite_sequence SET seq = 2 WHERE name = 'audit_events'")
	expectTailError(t, store, "tail anchor records event 4")

	result, err := store.rechainAuditLog(systemActor, nil, alwaysConfirmRechain)
	if err != nil {
		t.Fatalf("The re-chain failed: %v", err)
	}
	if !result.Confirmed {
		t.Fatalf("Expected the re-chain to be recorded, got %+v", result)
	}
	expectVerifies(t, store)

	st := readTail(t, store)
	if !st.namesNewest() || !store.auditTailVerifies(st) {
		t.Errorf("Expected the re-chain to anchor its own event, got %+v", st)
	}
}

// TestPurgeKeepsTheTailAnchor checks that the retention purge, which
// deletes from the oldest end, leaves the anchor naming its own event.
func TestPurgeKeepsTheTailAnchor(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	old := time.Now().UTC().Add(-48 * time.Hour)
	for i := 0; i < 3; i++ {
		ev := newEvent(systemActor, "user.create", "user", nil, "alice", nil)
		ev.OccurredAt = old
		mustRecord(t, store, ev)
	}
	recordN(t, store, 1)

	removed, err := store.PurgeAuditEvents(time.Now().UTC().Add(-time.Hour))
	if err != nil || removed != 3 {
		t.Fatalf("Expected the purge to remove 3 rows, got %d and %v",
			removed, err)
	}

	st := readTail(t, store)
	if !st.namesNewest() || st.anchorID != 5 {
		t.Errorf("Expected the anchor to name the purge event, got %+v", st)
	}
	expectVerifies(t, store)
}

// TestAuditTailMACRequiresAKey checks that the anchor is never signed
// under an empty key, and that the signature binds both the id and the
// hash.
func TestAuditTailMACRequiresAKey(t *testing.T) {
	if _, err := auditTailMAC(nil, 1, "h"); !errors.Is(err, errNoAuditKey) {
		t.Errorf("Expected errNoAuditKey, got %v", err)
	}

	key := AuditKeyForTesting()
	base, err := auditTailMAC(key, 1, "h")
	if err != nil {
		t.Fatalf("auditTailMAC failed: %v", err)
	}
	for name, got := range map[string]func() (string, error){
		"id":   func() (string, error) { return auditTailMAC(key, 2, "h") },
		"hash": func() (string, error) { return auditTailMAC(key, 1, "g") },
		"key": func() (string, error) {
			return auditTailMAC(rotatedAuditKey, 1, "h")
		},
	} {
		mac, err := got()
		if err != nil || mac == base {
			t.Errorf("Expected a different %s to change the MAC, got %v", name,
				err)
		}
	}

	store := &AuthStore{}
	if err := store.writeAuditTail(nil, &AuditEvent{ID: 1}); !errors.Is(err,
		errNoAuditKey) {
		t.Errorf("Expected writeAuditTail to refuse an empty key, got %v", err)
	}
}

// TestWriteAuditTailReportsAFailedWrite checks that a failed upsert is
// returned.
func TestWriteAuditTailReportsAFailedWrite(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	tamperDB(t, store.db, "DROP TABLE audit_tail")
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin: %v", err)
	}
	defer tx.Rollback()
	if err := store.writeAuditTail(tx, &AuditEvent{ID: 1, Hash: "h"}); err == nil {
		t.Error("Expected a failed write to be reported")
	}
}

// TestOpenReportsAnUncreatableTailTable checks that a store whose
// audit_tail table cannot be created refuses to open, rather than
// running with no anchor. An index already holding the name is enough
// to make the CREATE TABLE fail, since IF NOT EXISTS excuses only a
// table or view.
func TestOpenReportsAnUncreatableTailTable(t *testing.T) {
	store, dir := newReopenableStore(t)
	tamperDB(t, store.db, "DROP TABLE audit_tail")
	tamperDB(t, store.db, "CREATE INDEX audit_tail ON users (id)")

	if _, err := reopenStore(t, dir); err == nil ||
		!strings.Contains(err.Error(), "failed to create audit_tail table") {
		t.Errorf("Expected the open to fail on audit_tail, got %v", err)
	}
}

// TestReadAuditSequenceReportsAFailedRead checks both queries' error
// paths.
func TestReadAuditSequenceReportsAFailedRead(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	store.db.Close()
	if _, _, err := readAuditSequence(store.db); err == nil {
		t.Error("Expected a closed database to be reported")
	}
	if _, err := auditSequenceUnused(store.db); err == nil {
		t.Error("Expected auditSequenceUnused to report a closed database")
	}
}

// TestTailAnchorIsNotResignedOverADecoy checks that a writer without the
// secret cannot have the server re-sign the anchor over a truncated
// tail: a decoy row that copies the hash of the new newest row, with the
// anchor pointed at it, leaves the anchor where it was when the server
// writes its next event, and deleting the decoy afterwards is reported.
func TestTailAnchorIsNotResignedOverADecoy(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 10)
	rows := auditRowStates(t, store)
	keptHash := rows[5].Hash

	tamperDB(t, store.db, "DELETE FROM audit_events WHERE id > 6")
	tamperDB(t, store.db, `INSERT INTO audit_events (id, occurred_at,
            actor_type, actor_name, action, outcome, prev_hash, hash,
            hash_version)
        VALUES (7, ?, 'system', 'system', 'user.create', 'success',
            'decoy', ?, 3)`,
		time.Now().UTC().Format(auditTimeLayout), keptHash)
	tamperDB(t, store.db, `UPDATE audit_tail SET event_id = 7,
        event_hash = ?, mac = 'forged'`, keptHash)
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 7
        WHERE name = 'audit_events'`)

	recordN(t, store, 1)
	if st := readTail(t, store); st.anchorID != 7 || st.anchorMAC != "forged" {
		t.Fatalf("Expected the decoy anchor to be left alone, got %+v", st)
	}

	tamperDB(t, store.db, "DELETE FROM audit_events WHERE id = 7")
	expectTailError(t, store, "tail missing")
	if _, err := store.VerifyAuditLog(); errors.Is(err, ErrAuditKeyMismatch) {
		t.Errorf("Expected tampering, not a key mismatch, got %v", err)
	}
}

// truncateBeforeRotation writes four events under the test key, deletes
// the newest and writes the sequence down to match, optionally points
// the anchor at the new newest row without re-signing it, and then
// writes one event under the rotated key. It returns the directory.
func truncateBeforeRotation(t *testing.T, rename bool) string {
	t.Helper()

	store, dir := newReopenableStore(t)
	recordN(t, store, 4)
	rows := auditRowStates(t, store)
	tamperDB(t, store.db, "DELETE FROM audit_events WHERE id = 4")
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 3
        WHERE name = 'audit_events'`)
	if rename {
		tamperDB(t, store.db, `UPDATE audit_tail SET event_id = 3,
            event_hash = ?`, rows[2].Hash)
	}
	store.Close()

	rotated := openWithKey(t, dir, rotatedAuditKey)
	recordN(t, rotated, 1)
	rotated.Close()

	return dir
}

// TestRotationDoesNotHideATruncatedTail checks the two ways a tail cut
// from the rows written under the old secret can sit behind a rotation.
// An anchor left naming a deleted row is tampering outright. One renamed
// to the new last row under the old secret has the shape of a rotation,
// and is read as a key mismatch, but the previous secret does not prove
// it, so an unattended re-anchor is refused.
func TestRotationDoesNotHideATruncatedTail(t *testing.T) {
	t.Run("anchor left behind", func(t *testing.T) {
		dir := truncateBeforeRotation(t, false)
		store := openWithKey(t, dir, rotatedAuditKey)
		_, err := store.VerifyAuditLog()
		if !errors.Is(err, ErrAuditChainBroken) ||
			errors.Is(err, ErrAuditKeyMismatch) {
			t.Errorf("Expected tampering, got %v", err)
		}
	})

	t.Run("anchor renamed", func(t *testing.T) {
		dir := truncateBeforeRotation(t, true)
		store := openWithKey(t, dir, rotatedAuditKey)
		if _, err := store.VerifyAuditLog(); !errors.Is(err,
			ErrAuditKeyMismatch) {
			t.Fatalf("Expected a key mismatch, got %v", err)
		}
		store.Close()

		plan := planWithPreviousKey(t, dir, rotatedAuditKey,
			AuditKeyForTesting())
		if !plan.KeyMismatch || plan.HistoryProven ||
			!errors.Is(plan.HistoryProofErr, errAuditTailUnproven) {
			t.Errorf("Expected the previous secret not to prove the tail, "+
				"got %+v", plan)
		}
	})
}

// TestProveAuditReanchorTail checks each answer the previous secret can
// give about the anchor.
func TestProveAuditReanchorTail(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	if proof, err := store.proveAuditReanchorTail(store.db,
		rotatedAuditKey, 0, ""); proof != nil || err != nil {
		t.Errorf("Expected no anchor to need no proof, got %v, %v",
			proof, err)
	}

	recordN(t, store, 3)
	rows := auditRowStates(t, store)
	if proof, err := store.proveAuditReanchorTail(store.db,
		rotatedAuditKey, 0, ""); proof != nil || err != nil {
		t.Errorf("Expected an anchor that verifies to need no proof, got "+
			"%v, %v", proof, err)
	}

	// Seen from a store under another key, the anchor verifies only
	// under the previous one.
	other := &AuthStore{db: store.db, auditKey: rotatedAuditKey}
	if proof, err := other.proveAuditReanchorTail(store.db,
		AuditKeyForTesting(), rows[2].ID, rows[2].Hash); proof != nil ||
		err != nil {
		t.Errorf("Expected the anchor to be proven, got %v, %v", proof, err)
	}
	if proof, _ := other.proveAuditReanchorTail(store.db,
		AuditKeyForTesting(), rows[1].ID,
		rows[1].Hash); !errors.Is(proof, errAuditTailUnproven) ||
		!strings.Contains(proof.Error(), "deleted from the end") {
		t.Errorf("Expected an anchor past the history to be refused, got %v",
			proof)
	}
	if proof, _ := other.proveAuditReanchorTail(store.db,
		rotatedAuditKey, rows[2].ID,
		rows[2].Hash); !errors.Is(proof, errAuditTailUnproven) {
		t.Errorf("Expected an anchor under neither key to be refused, got %v",
			proof)
	}

	store.db.Close()
	if _, err := other.proveAuditReanchorTail(store.db,
		AuditKeyForTesting(), 0, ""); err == nil {
		t.Error("Expected a closed database to be reported")
	}
}

// TestVerifyAuditTailAfterKeyChangeRefusals checks the states the
// rotation-aware tail check refuses or cannot read.
func TestVerifyAuditTailAfterKeyChangeRefusals(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 2)
	st := readTail(t, store)
	last := AuditEvent{ID: st.newestID, Hash: st.newestHash}
	if err := store.verifyAuditTailAfterKeyChange(store.db,
		last); err != nil {
		t.Errorf("Expected an anchor that verifies to pass, got %v", err)
	}

	tamperDB(t, store.db, "DELETE FROM audit_tail")
	if err := store.verifyAuditTailAfterKeyChange(store.db,
		last); !errors.Is(err, ErrAuditChainBroken) {
		t.Errorf("Expected a missing anchor behind version 3 to be "+
			"refused, got %v", err)
	}

	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 9
        WHERE name = 'audit_events'`)
	if err := store.verifyAuditTailAfterKeyChange(store.db,
		last); !errors.Is(err, ErrAuditChainBroken) {
		t.Errorf("Expected a sequence ahead of the log to be refused, got %v",
			err)
	}
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 2
        WHERE name = 'audit_events'`)

	tamperDB(t, store.db, "DROP TABLE audit_tail")
	if err := store.verifyAuditTailAfterKeyChange(store.db,
		last); err == nil {
		t.Error("Expected a missing audit_tail table to be reported")
	}
}
