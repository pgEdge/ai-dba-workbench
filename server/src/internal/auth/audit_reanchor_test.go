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
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// rotatedAuditKey is the key a store opens with once its server secret
// has been changed.
var rotatedAuditKey = DeriveAuditKey("a different server secret")

// openWithKey opens a store on dir under the given key.
func openWithKey(t *testing.T, dir string, key []byte) *AuthStore {
	t.Helper()

	store, err := NewAuthStore(dir, 0, 0, key)
	if err != nil {
		t.Fatalf("Failed to open the auth store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return store
}

// storeDir returns the directory holding a store's auth.db.
func storeDir(t *testing.T, s *AuthStore) string {
	t.Helper()

	return filepath.Dir(s.path)
}

// rotatedLog builds a log whose oldest events were written under the
// test key and whose newer ones were written after the server secret
// changed: old events from three days ago, then under the new secret
// events from a day and a half ago and one from now. It returns the
// directory, a store open under the new key, and the ids of the rows
// written under the old key.
func rotatedLog(t *testing.T, old, rotated int) (string, *AuthStore,
	[]int64) {
	t.Helper()

	store, dir := newReopenableStore(t)
	now := time.Now().UTC()
	for i := 0; i < old; i++ {
		recordAt(t, store, "old-secret", now.Add(-72*time.Hour))
	}
	var oldIDs []int64
	for _, row := range auditRowStates(t, store) {
		oldIDs = append(oldIDs, row.ID)
	}
	store.Close()

	store = openWithKey(t, dir, rotatedAuditKey)
	for i := 0; i < rotated; i++ {
		recordAt(t, store, "new-secret", now.Add(-36*time.Hour))
	}
	recordAt(t, store, "recent", now)

	return dir, store, oldIDs
}

// reanchor runs the re-chain on dir under key, confirming it and
// returning the plan it was shown.
func reanchor(t *testing.T, dir string, key []byte) AuditRechainPlan {
	t.Helper()

	var seen AuditRechainPlan
	result, err := RechainAuditLog(dir, key, systemActor,
		func(plan AuditRechainPlan) (bool, error) {
			seen = plan
			return true, nil
		})
	if err != nil {
		t.Fatalf("Failed to re-chain the audit log: %v", err)
	}
	if !result.Confirmed || result.Mode != AuditRechainReanchor ||
		result.Events != 0 {
		t.Fatalf("Expected a confirmed re-anchor rewriting nothing, got %+v",
			result)
	}

	return seen
}

// newestRechainAnchor reads the record of the newest audit.rechain
// event.
func newestRechainAnchor(t *testing.T, s *AuthStore) (AuditEvent,
	auditAnchor) {
	t.Helper()

	events, _, err := s.ListAuditEvents(AuditFilter{
		Action: auditActionRechain, Limit: 1})
	if err != nil || len(events) == 0 {
		t.Fatalf("Failed to read the newest re-chain event: %v (%d found)",
			err, len(events))
	}
	anchor, err := parseAuditAnchor(&events[0])
	if err != nil {
		t.Fatalf("Failed to parse the re-chain event: %v", err)
	}

	return events[0], anchor
}

// TestRotatedSecretIsReportedAsAKeyMismatch checks that a log whose
// oldest rows were written under a secret since replaced is reported as
// a probable secret change, by the verifier and the purge alike, with
// the command that recovers from it, and that the purge deletes nothing.
func TestRotatedSecretIsReportedAsAKeyMismatch(t *testing.T) {
	_, store, oldIDs := rotatedLog(t, 3, 2)

	_, firstBad, err := store.VerifyAuditChain()
	if !errors.Is(err, ErrAuditKeyMismatch) ||
		errors.Is(err, ErrAuditChainBroken) {
		t.Fatalf("Expected a key mismatch, got %v", err)
	}
	if firstBad != oldIDs[0] {
		t.Errorf("Expected row %d to be reported, got %d", oldIDs[0], firstBad)
	}
	if !strings.Contains(err.Error(), "-rechain-audit-log") {
		t.Errorf("Expected the message to name the recovery, got %v", err)
	}

	before := auditRowCount(t, store)
	removed, err := store.PurgeAuditEvents(time.Now().UTC().Add(
		-48 * time.Hour))
	if !errors.Is(err, ErrAuditKeyMismatch) || removed != 0 {
		t.Fatalf("Expected the purge to refuse with a key mismatch, got %d "+
			"removed and %v", removed, err)
	}
	if !strings.Contains(err.Error(), "-rechain-audit-log") {
		t.Errorf("Expected the purge refusal to name the recovery, got %v",
			err)
	}
	if after := auditRowCount(t, store); after != before {
		t.Errorf("Expected the refused purge to delete nothing, went from "+
			"%d to %d rows", before, after)
	}
}

// TestDeclinedReanchorChangesNothing checks that the operator declining
// leaves every row exactly as it was.
func TestDeclinedReanchorChangesNothing(t *testing.T) {
	dir, store, _ := rotatedLog(t, 2, 1)
	before := auditRowStates(t, store)
	store.Close()

	asked := false
	result, err := RechainAuditLog(dir, rotatedAuditKey, systemActor,
		func(AuditRechainPlan) (bool, error) {
			asked = true
			return false, nil
		})
	if err != nil {
		t.Fatalf("Failed to plan the re-chain: %v", err)
	}
	if !asked || result.Confirmed || result.UpToDate {
		t.Fatalf("Expected the operator to be asked and decline, got %+v",
			result)
	}

	reopened := openWithKey(t, dir, rotatedAuditKey)
	if after := auditRowStates(t, reopened); !reflect.DeepEqual(before,
		after) {
		t.Errorf("Expected the log unchanged, went from %v to %v", before,
			after)
	}
	if _, _, err := reopened.VerifyAuditChain(); !errors.Is(err,
		ErrAuditKeyMismatch) {
		t.Errorf("Expected the key mismatch to remain, got %v", err)
	}
}

// TestReanchorAcceptsARotatedSecretsHistory checks the whole recovery:
// the plan shows what was found, the re-anchor rewrites nothing, the log
// then verifies with the old rows accepted as history, and the purge
// resumes and removes them.
func TestReanchorAcceptsARotatedSecretsHistory(t *testing.T) {
	dir, store, oldIDs := rotatedLog(t, 3, 2)
	before := auditRowStates(t, store)
	store.Close()

	plan := reanchor(t, dir, rotatedAuditKey)
	if !plan.KeyMismatch || plan.Problem == nil {
		t.Errorf("Expected the plan to report a key mismatch, got %v",
			plan.Problem)
	}
	if plan.HeadID != before[0].ID || plan.HeadHash != before[0].Hash {
		t.Errorf("Expected the head to be row %d, got %d (%q)", before[0].ID,
			plan.HeadID, plan.HeadHash)
	}
	if plan.HistoryEvents != 3 || plan.HistoryThroughID != oldIDs[2] {
		t.Errorf("Expected rows up to %d accepted as history, got %d "+
			"through %d", oldIDs[2], plan.HistoryEvents, plan.HistoryThroughID)
	}
	if plan.PreviousHead != nil {
		t.Errorf("Expected no previous head, got %+v", plan.PreviousHead)
	}

	store = openWithKey(t, dir, rotatedAuditKey)
	after := auditRowStates(t, store)
	if len(after) != len(before)+1 ||
		!reflect.DeepEqual(after[:len(before)], before) {
		t.Fatalf("Expected the existing rows untouched and one appended, "+
			"went from %v to %v", before, after)
	}
	ev, anchor := newestRechainAnchor(t, store)
	if ev.ID != after[len(after)-1].ID || !anchor.hasHistory() ||
		anchor.OldestRetainedHash != before[0].Hash {
		t.Errorf("Expected the appended event to record the head and the "+
			"history, got %+v", anchor)
	}

	report, err := store.VerifyAuditLog()
	if err != nil {
		t.Fatalf("Expected the re-anchored log to verify, got %v", err)
	}
	if report.HistoryEvents != 3 || report.HistoryAnchorID != ev.ID {
		t.Errorf("Expected 3 history rows from event %d, got %+v", ev.ID,
			report)
	}

	now := time.Now().UTC()
	removed, err := store.PurgeAuditEvents(now.Add(-48 * time.Hour))
	if err != nil || removed != 3 {
		t.Fatalf("Expected the purge to resume and remove 3 rows, got %d "+
			"and %v", removed, err)
	}
	if head := newestPurgeHead(t, store); head.hasHistory() {
		t.Errorf("Expected no history to survive the purge, got %+v", head)
	}
	report, err = store.VerifyAuditLog()
	if err != nil || report.HistoryEvents != 0 {
		t.Fatalf("Expected the purged log to verify with no history, got "+
			"%+v and %v", report, err)
	}

	removed, err = store.PurgeAuditEvents(now.Add(-24 * time.Hour))
	if err != nil || removed != 2 {
		t.Fatalf("Expected the next purge to remove 2 rows, got %d and %v",
			removed, err)
	}
	if _, _, err := store.VerifyAuditChain(); err != nil {
		t.Fatalf("Expected the log to verify after the second purge, got %v",
			err)
	}
}

// TestPurgeCarriesSurvivingHistoryForward checks that history the purge
// does not reach stays accepted, under a digest of the rows that remain,
// and that tampering with those rows afterwards is caught.
func TestPurgeCarriesSurvivingHistoryForward(t *testing.T) {
	store, dir := newReopenableStore(t)
	now := time.Now().UTC()
	recordAt(t, store, "old-oldest", now.Add(-72*time.Hour))
	recordAt(t, store, "old-oldest", now.Add(-72*time.Hour))
	for i := 0; i < 3; i++ {
		recordAt(t, store, "old-older", now.Add(-36*time.Hour))
	}
	store.Close()

	store = openWithKey(t, dir, rotatedAuditKey)
	recordAt(t, store, "recent", now)
	rows := auditRowStates(t, store)
	store.Close()

	reanchor(t, dir, rotatedAuditKey)
	store = openWithKey(t, dir, rotatedAuditKey)

	removed, err := store.PurgeAuditEvents(now.Add(-48 * time.Hour))
	if err != nil || removed != 2 {
		t.Fatalf("Expected the purge to remove 2 rows, got %d and %v",
			removed, err)
	}
	head := newestPurgeHead(t, store)
	if !head.hasHistory() || head.HistoryEvents != 3 ||
		*head.HistoryThroughID != rows[4].ID {
		t.Fatalf("Expected the purge to carry 3 history rows forward, got "+
			"%+v", head)
	}
	report, err := store.VerifyAuditLog()
	if err != nil || report.HistoryEvents != 3 {
		t.Fatalf("Expected the log to verify with 3 history rows, got %+v "+
			"and %v", report, err)
	}

	// Deleting a history row from the middle breaks no link the
	// verifier checks, since history rows are not linked to one
	// another; the digest is what catches it.
	deleteAuditRows(t, store, rows[3].ID)
	_, _, err = store.VerifyAuditChain()
	if !errors.Is(err, ErrAuditChainBroken) ||
		!strings.Contains(err.Error(), "accepted as history") {
		t.Fatalf("Expected the altered history to be reported, got %v", err)
	}
	removed, err = store.PurgeAuditEvents(now.Add(-24 * time.Hour))
	if !errors.Is(err, ErrAuditChainBroken) || removed != 0 {
		t.Fatalf("Expected the purge to refuse the altered history, got %d "+
			"and %v", removed, err)
	}
}

// TestReanchorAfterAHeadDeletion checks the other ordinary cause of a
// stalled purge: rows deleted from the start of the log. The plan shows
// the head the previous purge recorded and the one now accepted, and
// nothing is accepted as history, since every row still verifies.
func TestReanchorAfterAHeadDeletion(t *testing.T) {
	store, cutoff := purgedLog(t)
	dir := storeDir(t, store)
	rows := auditRowStates(t, store)
	purge := rows[len(rows)-1]
	deleteAuditRows(t, store, rows[0].ID)

	if _, _, err := store.VerifyAuditChain(); !errors.Is(err,
		ErrAuditChainBroken) {
		t.Fatalf("Expected the deletion to be reported, got %v", err)
	}
	if _, err := store.PurgeAuditEvents(cutoff); !errors.Is(err,
		ErrAuditChainBroken) {
		t.Fatalf("Expected the purge to refuse, got %v", err)
	}
	store.Close()

	plan := reanchor(t, dir, AuditKeyForTesting())
	if plan.KeyMismatch || plan.HistoryEvents != 0 {
		t.Errorf("Expected tampering with no history, got %+v", plan)
	}
	if plan.HeadID != rows[1].ID {
		t.Errorf("Expected the new head to be row %d, got %d", rows[1].ID,
			plan.HeadID)
	}
	prev := plan.PreviousHead
	if prev == nil || prev.EventID != purge.ID || prev.HeadID != rows[0].ID ||
		!prev.Verified || prev.Action != auditActionPurge {
		t.Errorf("Expected the previous head from purge %d, got %+v",
			purge.ID, prev)
	}

	store = openWithKey(t, dir, AuditKeyForTesting())
	if _, _, err := store.VerifyAuditChain(); err != nil {
		t.Fatalf("Expected the re-anchored log to verify, got %v", err)
	}
	removed, err := store.PurgeAuditEvents(cutoff)
	if err != nil || removed != 2 {
		t.Fatalf("Expected the purge to resume and remove 2 rows, got %d "+
			"and %v", removed, err)
	}
	if _, _, err := store.VerifyAuditChain(); err != nil {
		t.Fatalf("Expected the log to verify after the purge, got %v", err)
	}
}

// TestReanchorOfAVerifyingLog checks that a log that verifies is left
// alone without asking.
func TestReanchorOfAVerifyingLog(t *testing.T) {
	store, _ := purgedLog(t)
	dir := storeDir(t, store)
	before := auditRowStates(t, store)
	store.Close()

	result, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(AuditRechainPlan) (bool, error) {
			t.Error("Expected the operator not to be asked")
			return true, nil
		})
	if err != nil || !result.UpToDate || result.Confirmed {
		t.Fatalf("Expected nothing to re-chain, got %+v and %v", result, err)
	}

	store = openWithKey(t, dir, AuditKeyForTesting())
	if after := auditRowStates(t, store); !reflect.DeepEqual(before, after) {
		t.Errorf("Expected the log unchanged, went from %v to %v", before,
			after)
	}
}

// saveAuditRow copies one row into a side table, so that a test can put
// it back later as an attacker replaying it would.
func saveAuditRow(t *testing.T, s *AuthStore, id int64) {
	t.Helper()

	if _, err := s.db.Exec(`CREATE TABLE saved_audit AS
        SELECT * FROM audit_events WHERE id = ?`, id); err != nil {
		t.Fatalf("Failed to save audit row %d: %v", id, err)
	}
}

// replaySavedAuditRow inserts the saved row again, at the given id, or at
// the next id when id is nil.
func replaySavedAuditRow(t *testing.T, s *AuthStore, id *int64) {
	t.Helper()

	var newID any
	if id != nil {
		newID = *id
	}
	if _, err := s.db.Exec(`INSERT INTO audit_events (id, occurred_at,
            actor_type, actor_id, actor_name, actor_ip, action, target_type,
            target_id, target_name, outcome, error, details, prev_hash, hash,
            hash_version)
        SELECT ?, occurred_at, actor_type, actor_id, actor_name, actor_ip,
            action, target_type, target_id, target_name, outcome, error,
            details, prev_hash, hash, hash_version
        FROM saved_audit`, newID); err != nil {
		t.Fatalf("Failed to replay the saved audit row: %v", err)
	}
}

// replayableRechain builds a log holding a genuine re-anchor event, then
// purges it away with everything before it, and returns the store with
// that event saved for replay.
func replayableRechain(t *testing.T) *AuthStore {
	t.Helper()

	store, _ := purgedLog(t)
	dir := storeDir(t, store)
	deleteAuditRows(t, store, auditRowStates(t, store)[0].ID)
	store.Close()
	reanchor(t, dir, AuditKeyForTesting())

	store = openWithKey(t, dir, AuditKeyForTesting())
	rechain, _ := newestRechainAnchor(t, store)
	saveAuditRow(t, store, rechain.ID)

	// The row after the re-chain event must go too, since it is the one
	// that names the event as its predecessor.
	recordAt(t, store, "between", time.Now().UTC())
	future := time.Now().UTC().Add(2 * time.Hour)
	recordAt(t, store, "later", future)
	recordAt(t, store, "later", future)
	removed, err := store.PurgeAuditEvents(future.Add(-time.Hour))
	if err != nil || removed == 0 {
		t.Fatalf("Expected the purge to remove the re-chain event, got %d "+
			"and %v", removed, err)
	}
	if _, _, err := store.VerifyAuditChain(); err != nil {
		t.Fatalf("Expected the log to verify before the replay, got %v", err)
	}

	return store
}

// TestReplayedRechainAtTheEndIsRefused checks that a copy of a genuine
// re-chain event, replayed as the newest row to make the head it
// recorded count again, is refused by the verifier and the purge.
func TestReplayedRechainAtTheEndIsRefused(t *testing.T) {
	store := replayableRechain(t)
	replaySavedAuditRow(t, store, nil)
	// The server goes on writing after the replay, and the purge only
	// reads the log once something is inside its window.
	recordAt(t, store, "after", time.Now().UTC().Add(4*time.Hour))

	if _, _, err := store.VerifyAuditChain(); !errors.Is(err,
		ErrAuditChainBroken) {
		t.Fatalf("Expected the replay to be reported, got %v", err)
	}
	before := auditRowCount(t, store)
	_, err := store.PurgeAuditEvents(time.Now().UTC().Add(3 * time.Hour))
	if !errors.Is(err, ErrAuditChainBroken) ||
		!strings.Contains(err.Error(), "not linked into the chain") {
		t.Fatalf("Expected the purge to refuse the replayed event, got %v",
			err)
	}
	if after := auditRowCount(t, store); after != before {
		t.Errorf("Expected the refused purge to delete nothing, went from "+
			"%d to %d rows", before, after)
	}
}

// TestReplayedRechainAtTheHeadIsRefused checks the same copy replayed
// before the head, where it is not the newest anchor and so cannot
// override the purge event that followed it.
func TestReplayedRechainAtTheHeadIsRefused(t *testing.T) {
	store := replayableRechain(t)
	low := int64(-1)
	replaySavedAuditRow(t, store, &low)
	recordAt(t, store, "after", time.Now().UTC().Add(4*time.Hour))

	if _, _, err := store.VerifyAuditChain(); !errors.Is(err,
		ErrAuditChainBroken) {
		t.Fatalf("Expected the replay to be reported, got %v", err)
	}
	_, err := store.PurgeAuditEvents(time.Now().UTC().Add(3 * time.Hour))
	if !errors.Is(err, ErrAuditChainBroken) {
		t.Fatalf("Expected the purge to refuse, got %v", err)
	}
}

// TestReanchorRefusesALogThatChanged checks that a row replaced in place
// between the plan and the transaction, which leaves the count and the
// id range as they were, is caught and nothing is written.
func TestReanchorRefusesALogThatChanged(t *testing.T) {
	dir, store, oldIDs := rotatedLog(t, 2, 1)
	saveAuditRow(t, store, oldIDs[1])
	store.Close()

	_, err := RechainAuditLog(dir, rotatedAuditKey, systemActor,
		func(AuditRechainPlan) (bool, error) {
			raw := openRawAuditDB(t, dir)
			if _, err := raw.Exec("DELETE FROM audit_events WHERE id = ?",
				oldIDs[1]); err != nil {
				t.Fatalf("Failed to delete the row: %v", err)
			}
			if _, err := raw.Exec(`UPDATE saved_audit
                SET actor_name = 'someone else'`); err != nil {
				t.Fatalf("Failed to alter the saved row: %v", err)
			}
			if _, err := raw.Exec(`INSERT INTO audit_events
                SELECT * FROM saved_audit`); err != nil {
				t.Fatalf("Failed to put the row back: %v", err)
			}
			return true, nil
		})
	if !errors.Is(err, ErrAuditRechainChanged) {
		t.Fatalf("Expected the change to be caught, got %v", err)
	}

	store = openWithKey(t, dir, rotatedAuditKey)
	events, _, err := store.ListAuditEvents(AuditFilter{
		Action: auditActionRechain})
	if err != nil || len(events) != 0 {
		t.Errorf("Expected no re-chain event, got %d and %v", len(events), err)
	}
}

// TestReanchorRefusesALogThatGrew checks the count and id range are
// re-read too.
func TestReanchorRefusesALogThatGrew(t *testing.T) {
	dir, store, _ := rotatedLog(t, 1, 1)
	store.Close()

	_, err := RechainAuditLog(dir, rotatedAuditKey, systemActor,
		func(AuditRechainPlan) (bool, error) {
			writer := openWithKey(t, dir, rotatedAuditKey)
			recordAt(t, writer, "meanwhile", time.Now().UTC())
			writer.Close()
			return true, nil
		})
	if !errors.Is(err, ErrAuditRechainChanged) {
		t.Fatalf("Expected the change to be caught, got %v", err)
	}
}

// TestReanchorRefusesADamagedSchema checks that a missing trigger is
// refused rather than re-anchored over.
func TestReanchorRefusesADamagedSchema(t *testing.T) {
	_, store, _ := rotatedLog(t, 1, 1)
	if _, err := store.db.Exec("DROP TRIGGER " +
		auditNoUpdateTrigger); err != nil {
		t.Fatalf("Failed to drop the trigger: %v", err)
	}

	_, err := store.rechainAuditLog(systemActor, alwaysConfirmRechain)
	if err == nil || !strings.Contains(err.Error(),
		"refusing to re-chain") {
		t.Fatalf("Expected the damaged schema to be refused, got %v", err)
	}
}

// TestReanchorReportsAConfirmationError checks an error from the prompt
// is returned and nothing is written.
func TestReanchorReportsAConfirmationError(t *testing.T) {
	dir, store, _ := rotatedLog(t, 1, 1)
	before := auditRowCount(t, store)
	store.Close()

	boom := errors.New("terminal went away")
	_, err := RechainAuditLog(dir, rotatedAuditKey, systemActor,
		func(AuditRechainPlan) (bool, error) { return false, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("Expected the prompt's error, got %v", err)
	}
	store = openWithKey(t, dir, rotatedAuditKey)
	if after := auditRowCount(t, store); after != before {
		t.Errorf("Expected nothing written, went from %d to %d rows", before,
			after)
	}
}

// TestKeyChangeClassification checks the shape looksLikeKeyChange
// accepts as a changed secret, and the shapes it does not.
func TestKeyChangeClassification(t *testing.T) {
	t.Run("rotation followed by verifying rows", func(t *testing.T) {
		_, store, _ := rotatedLog(t, 2, 1)
		got, err := store.looksLikeKeyChange(store.db, nil)
		if err != nil || !got {
			t.Errorf("Expected a key change, got %v and %v", got, err)
		}
	})

	t.Run("every row under another key", func(t *testing.T) {
		store, dir := newReopenableStore(t)
		recordAt(t, store, "a", time.Now().UTC())
		recordAt(t, store, "b", time.Now().UTC())
		store.Close()
		other := openWithKey(t, dir, rotatedAuditKey)
		got, err := other.looksLikeKeyChange(other.db, nil)
		if err != nil || !got {
			t.Errorf("Expected a key change, got %v and %v", got, err)
		}
	})

	t.Run("the head verifies", func(t *testing.T) {
		store, _ := purgedLog(t)
		got, err := store.looksLikeKeyChange(store.db, nil)
		if err != nil || got {
			t.Errorf("Expected no key change, got %v and %v", got, err)
		}
	})

	t.Run("a forged row the next row does not link to", func(t *testing.T) {
		store, _ := newReopenableStore(t)
		recordAt(t, store, "a", time.Now().UTC())
		if _, err := store.db.Exec(`INSERT INTO audit_events (id,
                occurred_at, actor_type, actor_name, action, outcome,
                prev_hash, hash, hash_version)
            VALUES (-5, ?, 'system', 'system', 'user.create', 'success',
                'forged-prev', 'forged-hash', 2)`,
			time.Now().UTC().Format(auditTimeLayout)); err != nil {
			t.Fatalf("Failed to insert a forged row: %v", err)
		}
		got, err := store.looksLikeKeyChange(store.db, nil)
		if err != nil || got {
			t.Errorf("Expected no key change, got %v and %v", got, err)
		}
	})

	t.Run("an empty log", func(t *testing.T) {
		store, _ := newReopenableStore(t)
		if _, err := store.db.Exec("DELETE FROM audit_events"); err != nil {
			t.Fatalf("Failed to empty the log: %v", err)
		}
		got, err := store.looksLikeKeyChange(store.db, nil)
		if err != nil || got {
			t.Errorf("Expected no key change, got %v and %v", got, err)
		}
	})
}

// TestReanchorAfterARehashIgnoresItsEvent checks that the audit.rechain
// event a re-hash leaves, which records no starting point, is not shown
// as the previous head, and does not stop a later re-anchor.
func TestReanchorAfterARehashIgnoresItsEvent(t *testing.T) {
	store, dir := newReopenableStore(t)
	inheritV1Rows(t, store, 3)
	store.Close()

	if _, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(AuditRechainPlan) (bool, error) { return true, nil }); err != nil {
		t.Fatalf("Failed to re-hash the log: %v", err)
	}

	store = openWithKey(t, dir, rotatedAuditKey)
	recordAt(t, store, "new-secret", time.Now().UTC())
	store.Close()

	plan := reanchor(t, dir, rotatedAuditKey)
	if plan.PreviousHead != nil {
		t.Errorf("Expected the re-hash event not to count as a head, got %+v",
			plan.PreviousHead)
	}
	if !plan.KeyMismatch {
		t.Errorf("Expected a key mismatch, got %v", plan.Problem)
	}

	store = openWithKey(t, dir, rotatedAuditKey)
	if _, err := store.VerifyAuditLog(); err != nil {
		t.Errorf("Expected the re-anchored log to verify: %v", err)
	}
}

// TestReanchorHelpersReportAClosedStore checks the re-anchor's reads and
// its transaction return an error, rather than an empty result, when
// the database cannot be reached.
func TestReanchorHelpersReportAClosedStore(t *testing.T) {
	store, _ := newReopenableStore(t)
	store.Close()

	if _, err := store.previousAuditHead(); err == nil {
		t.Error("Expected previousAuditHead to fail on a closed store")
	}
	if _, err := store.scanAuditForReanchor(store.db); err == nil {
		t.Error("Expected scanAuditForReanchor to fail on a closed store")
	}
	if err := store.reanchorAuditLogTx(systemActor,
		AuditRechainPlan{}); err == nil {
		t.Error("Expected reanchorAuditLogTx to fail on a closed store")
	}
}
