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
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// recordAt appends a validly keyed event dated at, and returns it.
func recordAt(t *testing.T, s *AuthStore, name string,
	at time.Time) *AuditEvent {
	t.Helper()

	ev := newEvent(systemActor, "user.create", "user", nil, name, nil)
	ev.OccurredAt = at
	if err := s.recordAuditInOwnTx(ev); err != nil {
		t.Fatalf("Failed to record event %q: %v", name, err)
	}

	return ev
}

// deleteAuditRows removes rows by id, the one-statement deletion an
// attacker with write access to auth.db needs no key for.
func deleteAuditRows(t *testing.T, s *AuthStore, ids ...int64) {
	t.Helper()

	for _, id := range ids {
		if _, err := s.db.Exec("DELETE FROM audit_events WHERE id = ?",
			id); err != nil {
			t.Fatalf("Failed to delete audit row %d: %v", id, err)
		}
	}
}

// auditRowCount returns the number of rows in audit_events.
func auditRowCount(t *testing.T, s *AuthStore) int {
	t.Helper()

	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM audit_events").
		Scan(&count); err != nil {
		t.Fatalf("Failed to count audit rows: %v", err)
	}

	return count
}

// newestPurgeHead reads the head record of the newest audit.purge event.
func newestPurgeHead(t *testing.T, s *AuthStore) auditPurgeHead {
	t.Helper()

	events, _, err := s.ListAuditEvents(AuditFilter{Action: auditActionPurge,
		Limit: 1})
	if err != nil || len(events) == 0 {
		t.Fatalf("Failed to read the newest purge event: %v (%d found)",
			err, len(events))
	}
	head, err := parseAuditPurgeHead(&events[0])
	if err != nil {
		t.Fatalf("Failed to parse the purge event: %v", err)
	}

	return head
}

// purgedLog builds a log whose three oldest events have been purged,
// leaving three events from a day and a half ago and one from now, and
// returns the store and the time the next purge should use to remove
// the day-and-a-half-old events too.
func purgedLog(t *testing.T) (*AuthStore, time.Time) {
	t.Helper()

	store, _ := newReopenableStore(t)
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		recordAt(t, store, "oldest", now.Add(-72*time.Hour))
	}
	for i := 0; i < 3; i++ {
		recordAt(t, store, "older", now.Add(-36*time.Hour))
	}
	recordAt(t, store, "recent", now)

	removed, err := store.PurgeAuditEvents(now.Add(-48 * time.Hour))
	if err != nil {
		t.Fatalf("Failed to purge: %v", err)
	}
	if removed != 3 {
		t.Fatalf("Expected 3 rows purged, got %d", removed)
	}

	return store, now.Add(-24 * time.Hour)
}

// TestPurgeRecordsTheHeadItLeaves checks the purge event names the row
// it left as the oldest, by id and by that row's own hash.
func TestPurgeRecordsTheHeadItLeaves(t *testing.T) {
	store, _ := purgedLog(t)

	oldest := auditRowStates(t, store)[0]
	head := newestPurgeHead(t, store)
	if head.OldestRetainedID == nil || *head.OldestRetainedID != oldest.ID {
		t.Errorf("Expected the purge to record row %d as the head, got %v",
			oldest.ID, head.OldestRetainedID)
	}
	if head.OldestRetainedHash != oldest.Hash {
		t.Errorf("Expected the purge to record the head's hash %q, got %q",
			oldest.Hash, head.OldestRetainedHash)
	}

	if _, firstBad, err := store.VerifyAuditChain(); err != nil {
		t.Fatalf("Expected the purged log to verify, got %v (row %d)",
			err, firstBad)
	}
}

// TestSuccessivePurgesVerify checks that a second purge starts where
// the first left the head, and that the log verifies after each.
func TestSuccessivePurgesVerify(t *testing.T) {
	store, next := purgedLog(t)

	removed, err := store.PurgeAuditEvents(next)
	if err != nil {
		t.Fatalf("Failed to run the second purge: %v", err)
	}
	// The three older events, and the first purge's own event, which
	// was written after them but is dated now and so is kept.
	if removed != 3 {
		t.Errorf("Expected 3 rows purged by the second purge, got %d",
			removed)
	}
	if _, firstBad, err := store.VerifyAuditChain(); err != nil {
		t.Fatalf("Expected the log to verify after two purges, got %v "+
			"(row %d)", err, firstBad)
	}
}

// TestVerifyReportsHeadDeletedWithoutAPurge checks the case the issue
// named: the oldest event has a predecessor, and nothing in the log
// accounts for its removal.
func TestVerifyReportsHeadDeletedWithoutAPurge(t *testing.T) {
	store, _ := newReopenableStore(t)
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		recordAt(t, store, "alice", now)
	}
	rows := auditRowStates(t, store)
	deleteAuditRows(t, store, rows[0].ID)

	count, firstBad, err := store.VerifyAuditChain()
	if !errors.Is(err, ErrAuditChainBroken) {
		t.Fatalf("Expected a head deletion to read as tampering, got %v",
			err)
	}
	if !strings.Contains(err.Error(), "no audit.purge event accounts") {
		t.Errorf("Expected the missing purge event to be named, got %v", err)
	}
	if firstBad != rows[1].ID || count != 2 {
		t.Errorf("Expected row %d after 2 events, got row %d after %d",
			rows[1].ID, firstBad, count)
	}
}

// TestVerifyReportsHeadDeletedSinceTheLastPurge checks that a deletion
// from the start of a purged log is caught, even though a purge event
// survives to explain a missing predecessor in general.
func TestVerifyReportsHeadDeletedSinceTheLastPurge(t *testing.T) {
	store, _ := purgedLog(t)
	rows := auditRowStates(t, store)
	deleteAuditRows(t, store, rows[0].ID)

	_, firstBad, err := store.VerifyAuditChain()
	if !errors.Is(err, ErrAuditChainBroken) {
		t.Fatalf("Expected a head deletion to read as tampering, got %v",
			err)
	}
	if !strings.Contains(err.Error(), "deleted from the start of the log") {
		t.Errorf("Expected the head deletion to be described, got %v", err)
	}
	if firstBad != rows[1].ID {
		t.Errorf("Expected row %d to be reported, got %d", rows[1].ID,
			firstBad)
	}
}

// TestVerifyAcceptsAHeadALegacyPurgeExplains pins the residual limit
// for a log whose purge events all predate the head record: a
// predecessor is then accounted for by the existence of a purge event,
// and nothing more precise is possible.
func TestVerifyAcceptsAHeadALegacyPurgeExplains(t *testing.T) {
	store, _ := newReopenableStore(t)
	now := time.Now().UTC()
	recordAt(t, store, "alice", now)
	recordAt(t, store, "bob", now)
	legacy := newEvent(systemActor, auditActionPurge, "", nil, "",
		map[string]any{"older_than": now.Format(time.RFC3339),
			"removed": 1})
	if err := store.recordAuditInOwnTx(legacy); err != nil {
		t.Fatalf("Failed to record a legacy purge event: %v", err)
	}
	deleteAuditRows(t, store, auditRowStates(t, store)[0].ID)

	if _, firstBad, err := store.VerifyAuditChain(); err != nil {
		t.Errorf("Expected a legacy purge event to account for the head, "+
			"got %v (row %d)", err, firstBad)
	}
}

// TestPurgeRefusesAHeadDeletedSinceTheLastPurge checks the purge will
// not start from a head other than the one the previous purge left,
// since the record it writes would otherwise launder the deletion.
func TestPurgeRefusesAHeadDeletedSinceTheLastPurge(t *testing.T) {
	store, next := purgedLog(t)
	deleteAuditRows(t, store, auditRowStates(t, store)[0].ID)
	before := auditRowCount(t, store)

	removed, err := store.PurgeAuditEvents(next)
	if !errors.Is(err, ErrAuditChainBroken) {
		t.Fatalf("Expected the purge to refuse, got %v", err)
	}
	if !strings.Contains(err.Error(), errAuditPurgeRefused) ||
		!strings.Contains(err.Error(), "not the one the previous purge left") {
		t.Errorf("Expected the refusal to name the head, got %v", err)
	}
	if removed != 0 || auditRowCount(t, store) != before {
		t.Errorf("Expected nothing deleted, got %d removed and %d rows "+
			"left of %d", removed, auditRowCount(t, store), before)
	}
}

// TestPurgeRefusesToLaunderAForgedRow performs the laundering attack:
// delete the head, then insert a row at a low id with an old timestamp
// so that the next unattended purge removes something and records a
// new head. The forged row does not verify, so the purge refuses.
func TestPurgeRefusesToLaunderAForgedRow(t *testing.T) {
	store, _ := purgedLog(t)
	rows := auditRowStates(t, store)
	deleteAuditRows(t, store, rows[0].ID, rows[1].ID, rows[2].ID)
	insertAuditRowAtID(t, store, -5, time.Now().UTC().Add(-90*time.Hour))

	removed, err := store.PurgeAuditEvents(
		time.Now().UTC().Add(-48 * time.Hour))
	if !errors.Is(err, ErrAuditChainBroken) ||
		!strings.Contains(err.Error(), "does not verify under the key") {
		t.Fatalf("Expected the purge to refuse the forged row, got %v", err)
	}
	if removed != 0 {
		t.Errorf("Expected nothing removed, got %d", removed)
	}
	if _, _, err := store.VerifyAuditChain(); err == nil {
		t.Error("Expected the tampered log still to fail verification")
	}
}

// TestPurgeRefusesADeletedGenesisOnTheFirstPurge checks the first purge
// a log has ever had insists on starting at the genesis row, since no
// earlier purge has recorded a head.
func TestPurgeRefusesADeletedGenesisOnTheFirstPurge(t *testing.T) {
	store, _ := newReopenableStore(t)
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		recordAt(t, store, "old", now.Add(-48*time.Hour))
	}
	recordAt(t, store, "recent", now)
	deleteAuditRows(t, store, auditRowStates(t, store)[0].ID)

	_, err := store.PurgeAuditEvents(now.Add(-time.Hour))
	if !errors.Is(err, ErrAuditChainBroken) ||
		!strings.Contains(err.Error(), "no audit.purge event accounts") {
		t.Fatalf("Expected the purge to refuse a missing genesis, got %v",
			err)
	}
}

// TestPurgeRefusesARestoredGenesisWithAGapBehindIt checks a genuine
// row restored from a copy does not satisfy the purge on its own: the
// rows it deletes must also chain through to the new head.
func TestPurgeRefusesARestoredGenesisWithAGapBehindIt(t *testing.T) {
	store, _ := newReopenableStore(t)
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		recordAt(t, store, "old", now.Add(-48*time.Hour))
	}
	recordAt(t, store, "recent", now)
	rows := auditRowStates(t, store)
	// Deleting the second row leaves the genesis row in place, genuine
	// and verifying, with the third no longer linked to anything.
	deleteAuditRows(t, store, rows[1].ID)

	_, err := store.PurgeAuditEvents(now.Add(-time.Hour))
	if !errors.Is(err, ErrAuditChainBroken) ||
		!strings.Contains(err.Error(), "does not link to event") {
		t.Fatalf("Expected the purge to refuse the gap, got %v", err)
	}
}

// TestPurgeAcceptsAnyStartAfterALegacyPurge checks the transition from
// a log whose newest purge event predates the head record: the first
// purge this build runs starts wherever the head is, and records it.
func TestPurgeAcceptsAnyStartAfterALegacyPurge(t *testing.T) {
	store, _ := newReopenableStore(t)
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		recordAt(t, store, "old", now.Add(-48*time.Hour))
	}
	legacy := newEvent(systemActor, auditActionPurge, "", nil, "",
		map[string]any{"older_than": now.Format(time.RFC3339),
			"removed": 1})
	legacy.OccurredAt = now.Add(-47 * time.Hour)
	if err := store.recordAuditInOwnTx(legacy); err != nil {
		t.Fatalf("Failed to record a legacy purge event: %v", err)
	}
	recordAt(t, store, "recent", now)
	deleteAuditRows(t, store, auditRowStates(t, store)[0].ID)

	removed, err := store.PurgeAuditEvents(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Expected the purge to proceed, got %v", err)
	}
	if removed != 3 {
		t.Errorf("Expected 3 rows purged, got %d", removed)
	}
	if head := newestPurgeHead(t, store); head.OldestRetainedHash == "" {
		t.Error("Expected the purge to record the head it left")
	}
	if _, firstBad, err := store.VerifyAuditChain(); err != nil {
		t.Errorf("Expected the log to verify, got %v (row %d)", err,
			firstBad)
	}
}

// TestReplayedLegacyPurgeDoesNotReopenTheWeakCheck replays a genuine,
// keyed purge event written before the head record into a log that
// already has one, after deleting the head. The newer event records
// nothing, so it must not stand in for the older record: the verifier
// still reports the head, and the purge still refuses.
func TestReplayedLegacyPurgeDoesNotReopenTheWeakCheck(t *testing.T) {
	store, next := purgedLog(t)
	deleteAuditRows(t, store, auditRowStates(t, store)[0].ID)
	legacy := newEvent(systemActor, auditActionPurge, "", nil, "",
		map[string]any{"older_than": next.Format(time.RFC3339),
			"removed": 1})
	if err := store.recordAuditInOwnTx(legacy); err != nil {
		t.Fatalf("Failed to record a legacy purge event: %v", err)
	}

	if _, _, err := store.VerifyAuditChain(); !errors.Is(err,
		ErrAuditChainBroken) || !strings.Contains(err.Error(),
		"deleted from the start of the log") {
		t.Errorf("Expected the head deletion still to be reported, got %v",
			err)
	}
	if _, err := store.PurgeAuditEvents(next); !errors.Is(err,
		ErrAuditChainBroken) || !strings.Contains(err.Error(),
		"not the one the previous purge left") {
		t.Errorf("Expected the purge still to refuse, got %v", err)
	}
}

// TestRecordAuditRefusesADiscardedEvent checks an insert that a trigger
// turns into a no-op fails the write rather than committing the change
// with no event.
func TestRecordAuditRefusesADiscardedEvent(t *testing.T) {
	store, _ := newReopenableStore(t)
	if _, err := store.db.Exec("CREATE TRIGGER audit_drop BEFORE INSERT " +
		"ON audit_events BEGIN SELECT RAISE(IGNORE); END"); err != nil {
		t.Fatalf("Failed to create the discarding trigger: %v", err)
	}

	ev := newEvent(systemActor, "user.create", "user", nil, "alice", nil)
	if err := store.recordAuditInOwnTx(ev); err == nil ||
		!strings.Contains(err.Error(), "0 rows written") {
		t.Errorf("Expected the discarded event to fail the write, got %v",
			err)
	}
}

// insertForgedPurgeRow writes an audit.purge row with a hash nobody
// holding the key computed.
func insertForgedPurgeRow(t *testing.T, s *AuthStore) {
	t.Helper()

	if _, err := s.db.Exec(`
        INSERT INTO audit_events (
            occurred_at, actor_type, actor_name, action, outcome, details,
            prev_hash, hash, hash_version
        ) VALUES (?, 'system', 'system', ?, 'success', '{}',
                  'forged-prev', 'forged-hash', 2)`,
		time.Now().UTC().Format(auditTimeLayout),
		auditActionPurge); err != nil {
		t.Fatalf("Failed to insert a forged purge row: %v", err)
	}
}

// TestPurgeRefusesAForgedPurgeEvent checks the head record is trusted
// only from a purge event whose hash verifies.
func TestPurgeRefusesAForgedPurgeEvent(t *testing.T) {
	store, next := purgedLog(t)
	insertForgedPurgeRow(t, store)

	_, err := store.PurgeAuditEvents(next)
	if !errors.Is(err, ErrAuditChainBroken) ||
		!strings.Contains(err.Error(), "does not verify under the key") {
		t.Fatalf("Expected the forged purge event to be refused, got %v",
			err)
	}
}

// TestUnreadablePurgeDetailsAreReported checks a purge event whose
// details are not JSON is reported by both the verifier and the purge,
// rather than read as a purge that recorded nothing.
func TestUnreadablePurgeDetailsAreReported(t *testing.T) {
	store, _ := newReopenableStore(t)
	now := time.Now().UTC()
	recordAt(t, store, "old", now.Add(-48*time.Hour))
	bad := newEvent(systemActor, auditActionPurge, "", nil, "", nil)
	bad.OccurredAt = now.Add(-47 * time.Hour)
	bad.Details = json.RawMessage("not json")
	if err := store.recordAuditInOwnTx(bad); err != nil {
		t.Fatalf("Failed to record the purge event: %v", err)
	}
	recordAt(t, store, "recent", now)

	if _, firstBad, err := store.VerifyAuditChain(); err == nil ||
		!strings.Contains(err.Error(), "failed to read the details") {
		t.Errorf("Expected the verifier to report the details, got %v", err)
	} else if firstBad != bad.ID {
		t.Errorf("Expected row %d to be reported, got %d", bad.ID, firstBad)
	}

	if _, err := store.PurgeAuditEvents(now.Add(-time.Hour)); err == nil ||
		!strings.Contains(err.Error(), "failed to read the details") {
		t.Errorf("Expected the purge to report the details, got %v", err)
	}
}

// TestParseAuditPurgeHeadNoDetails checks a purge event with no details
// reads as one that recorded no head.
func TestParseAuditPurgeHeadNoDetails(t *testing.T) {
	head, err := parseAuditPurgeHead(&AuditEvent{ID: 3})
	if err != nil || head.OldestRetainedID != nil ||
		head.OldestRetainedHash != "" {
		t.Errorf("Expected an empty record, got %+v, %v", head, err)
	}
}

// TestAuditHeadCheckEmptyLog checks an empty log has no head to
// account for.
func TestAuditHeadCheckEmptyLog(t *testing.T) {
	var head auditHeadCheck
	if firstBad, err := head.check(); err != nil || firstBad != 0 {
		t.Errorf("Expected an empty log to pass, got %v (row %d)", err,
			firstBad)
	}
}

// TestVerifyAuditPurgePrefixErrors covers the prefix check's failures
// that the purge itself cannot reach: a cut that names no row, a row
// that cannot be scanned and the lookups failing outright.
func TestVerifyAuditPurgePrefixErrors(t *testing.T) {
	inTx := func(t *testing.T, s *AuthStore,
		fn func(tx *sql.Tx) error) error {
		t.Helper()
		tx, err := s.db.Begin()
		if err != nil {
			t.Fatalf("Failed to begin: %v", err)
		}
		defer func() {
			//nolint:errcheck // The test only reads.
			tx.Rollback()
		}()
		return fn(tx)
	}

	t.Run("cut names no row", func(t *testing.T) {
		store, _ := newReopenableStore(t)
		recordAt(t, store, "alice", time.Now().UTC())
		err := inTx(t, store, func(tx *sql.Tx) error {
			_, err := store.verifyAuditPurgePrefix(tx, 1000)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "could not be read") {
			t.Errorf("Expected a missing head to be refused, got %v", err)
		}
	})

	t.Run("empty log", func(t *testing.T) {
		store, _ := newReopenableStore(t)
		err := inTx(t, store, func(tx *sql.Tx) error {
			_, err := store.verifyAuditPurgePrefix(tx, 1)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "could not be read") {
			t.Errorf("Expected an empty log to be refused, got %v", err)
		}
	})

	t.Run("row cannot be scanned", func(t *testing.T) {
		store, _ := newReopenableStore(t)
		if _, err := store.db.Exec(`
            INSERT INTO audit_events (
                id, occurred_at, actor_type, actor_name, action, outcome,
                prev_hash, hash, hash_version
            ) VALUES (-1, 'not a time', 'system', 'system', 'user.create',
                      'success', '', 'h', 2)`); err != nil {
			t.Fatalf("Failed to insert the unreadable row: %v", err)
		}
		err := inTx(t, store, func(tx *sql.Tx) error {
			_, err := store.verifyAuditPurgePrefix(tx, 1)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "failed to scan") {
			t.Errorf("Expected the scan failure to be returned, got %v", err)
		}
	})

	t.Run("table missing", func(t *testing.T) {
		store, _ := newReopenableStore(t)
		if _, err := store.db.Exec("DROP TABLE audit_events"); err != nil {
			t.Fatalf("Failed to drop audit_events: %v", err)
		}
		err := inTx(t, store, func(tx *sql.Tx) error {
			_, err := store.verifyAuditPurgePrefix(tx, 1)
			return err
		})
		if err == nil ||
			!strings.Contains(err.Error(), "failed to read the newest") {
			t.Errorf("Expected the lookup failure to be returned, got %v",
				err)
		}
	})
}

// TestCheckAuditRowVerifiesUnknownVersion covers a row the purge cannot
// verify at all, as distinct from one whose hash is wrong.
func TestCheckAuditRowVerifiesUnknownVersion(t *testing.T) {
	store, _ := newReopenableStore(t)
	ev := AuditEvent{ID: 7, HashVersion: 9}
	err := store.checkAuditRowVerifies(&ev)
	if !errors.Is(err, ErrAuditChainBroken) ||
		!strings.Contains(err.Error(), "cannot be verified") {
		t.Errorf("Expected an unknown version to be refused, got %v", err)
	}
}

// TestVerifyAuditSchemaChecksDefinitions checks the schema objects are
// judged by what they do, not by their names: each case swaps one for
// a same-named object that no longer protects the chain.
func TestVerifyAuditSchemaChecksDefinitions(t *testing.T) {
	// Each tamper is a fixed literal, so that the statements a case runs
	// are visible at a glance and nothing is assembled at run time.
	cases := []struct {
		name    string
		tamper  string
		wantErr string
	}{
		{"unique index on another column",
			`DROP INDEX idx_audit_prev_hash;
             CREATE UNIQUE INDEX idx_audit_prev_hash ON audit_events(id)`,
			"not an index on prev_hash alone"},
		{"unique index on prev_hash and another column",
			`DROP INDEX idx_audit_prev_hash;
             CREATE UNIQUE INDEX idx_audit_prev_hash
                 ON audit_events(prev_hash, id)`,
			"not an index on prev_hash alone"},
		{"unique index on an expression",
			`DROP INDEX idx_audit_prev_hash;
             CREATE UNIQUE INDEX idx_audit_prev_hash
                 ON audit_events(lower(prev_hash))`,
			"not an index on prev_hash alone"},
		{"partial unique index",
			`DROP INDEX idx_audit_prev_hash;
             CREATE UNIQUE INDEX idx_audit_prev_hash
                 ON audit_events(prev_hash) WHERE id < 0`,
			"is partial"},
		{"trigger that does nothing",
			`DROP TRIGGER audit_events_no_update;
             CREATE TRIGGER audit_events_no_update
                 BEFORE UPDATE ON audit_events BEGIN SELECT 1; END`,
			"does not match the definition"},
		{"trigger with a WHEN clause",
			`DROP TRIGGER audit_events_no_update;
             CREATE TRIGGER audit_events_no_update
                 BEFORE UPDATE ON audit_events WHEN 0 BEGIN
                 SELECT RAISE(ABORT, 'audit_events is append-only'); END`,
			"does not match the definition"},
		{"trigger on some columns only",
			`DROP TRIGGER audit_events_no_update;
             CREATE TRIGGER audit_events_no_update
                 BEFORE UPDATE OF details ON audit_events BEGIN
                 SELECT RAISE(ABORT, 'audit_events is append-only'); END`,
			"does not match the definition"},
		{"extra trigger discarding events as they arrive",
			`CREATE TRIGGER audit_drop BEFORE INSERT ON audit_events
                 WHEN NEW.actor_name = 'mallory' BEGIN
                 SELECT RAISE(IGNORE); END`,
			"is not one this server creates"},
		{"extra trigger on another table deleting events",
			`CREATE TRIGGER user_hide AFTER INSERT ON users BEGIN
                 DELETE FROM audit_events
                     WHERE id = (SELECT MAX(id) FROM audit_events); END`,
			"is not one this server creates"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := newReopenableStore(t)
			recordAt(t, store, "alice", time.Now().UTC())
			if _, err := store.db.Exec(tc.tamper); err != nil {
				t.Fatalf("Failed to tamper with the schema: %v", err)
			}

			_, _, err := store.VerifyAuditChain()
			if !errors.Is(err, ErrAuditChainBroken) {
				t.Fatalf("Expected tampering, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Expected %q in the error, got %v", tc.wantErr, err)
			}
		})
	}
}

// TestVerifyAuditSchemaAcceptsItsOwnObjects checks the definitions this
// server creates pass, including after a reopen, which re-runs the
// IF NOT EXISTS statements and must leave the stored text matching.
func TestVerifyAuditSchemaAcceptsItsOwnObjects(t *testing.T) {
	store, dir := newReopenableStore(t)
	if err := store.verifyAuditSchema(); err != nil {
		t.Fatalf("Expected a fresh schema to pass, got %v", err)
	}
	store.Close()

	reopened, err := reopenStore(t, dir)
	if err != nil {
		t.Fatalf("Failed to reopen: %v", err)
	}
	if err := reopened.verifyAuditSchema(); err != nil {
		t.Errorf("Expected a reopened schema to pass, got %v", err)
	}
}

// TestVerifyAuditNoUpdateTriggerQueryError covers the trigger lookup
// failing outright, which must be returned rather than read as absence.
func TestVerifyAuditNoUpdateTriggerQueryError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open in-memory database: %v", err)
	}
	db.Close()

	store := &AuthStore{db: db}
	if err := store.verifyAuditNoUpdateTrigger(); err == nil ||
		!strings.Contains(err.Error(), "failed to look for trigger") {
		t.Errorf("Expected the trigger lookup failure, got %v", err)
	}
}
