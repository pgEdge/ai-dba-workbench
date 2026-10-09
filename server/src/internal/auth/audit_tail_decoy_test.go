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

// startAnchorOverDecoy is the decoy from the fourth review of #565, on
// a store holding ten events under its key: the newest five are cut, a
// decoy copying the hash of event 5 is put in at decoyID, and the
// primary anchor is pointed at it under a MAC no secret produced, so
// that the server's next event starts a current-key anchor, signed
// under the key in use, beside a primary anchor naming the decoy. The
// decoy is left in place for the caller to delete.
func startAnchorOverDecoy(t *testing.T, s *AuthStore, decoyID int64) {
	t.Helper()

	keptHash := auditRowHash(t, s, 5)
	deleteAuditRows(t, s, 6, 7, 8, 9, 10)
	tamperDB(t, s.db, `INSERT INTO audit_events (id, occurred_at,
            actor_type, actor_name, action, outcome, prev_hash, hash,
            hash_version)
        VALUES (?, ?, 'system', 'system', 'user.create', 'success',
            'unused', ?, 3)`,
		decoyID, time.Now().UTC().Format(auditTimeLayout), keptHash)
	tamperDB(t, s.db, `UPDATE sqlite_sequence SET seq = ?
        WHERE name = 'audit_events'`, decoyID)
	tamperDB(t, s.db, `UPDATE audit_tail SET event_id = ?,
        event_hash = ?, mac = 'x' WHERE id = 1`, decoyID, keptHash)

	recordN(t, s, 1)
	if st := readTail(t, s); !st.currentNamesNewest() ||
		st.anchorID != decoyID {
		t.Fatalf("Expected a current-key anchor beside the decoy, got %+v",
			st)
	}
}

// startAnchorOverAlteredRow does what startAnchorOverDecoy does without
// a decoy: event 5, left as the newest after the cut, is altered so
// that it fails, the server starts a current-key anchor beside a
// primary anchor naming it, and the alteration is then undone, so that
// every surviving row verifies again and the row the binding names is
// still in the log.
func startAnchorOverAlteredRow(t *testing.T, s *AuthStore) {
	t.Helper()

	keptHash := auditRowHash(t, s, 5)
	deleteAuditRows(t, s, 6, 7, 8, 9, 10)
	tamperDB(t, s.db, `UPDATE sqlite_sequence SET seq = 5
        WHERE name = 'audit_events'`)
	var actor string
	if err := s.db.QueryRow("SELECT actor_name FROM audit_events " +
		"WHERE id = 5").Scan(&actor); err != nil {
		t.Fatalf("Failed to read event 5: %v", err)
	}
	setActor := func(name string) {
		withAppendOnlyLifted(t, s, func() (sql.Result, error) {
			return s.db.Exec("UPDATE audit_events SET actor_name = ? "+
				"WHERE id = 5", name)
		})
	}
	setActor("mallory")
	tamperDB(t, s.db, `UPDATE audit_tail SET event_id = 5,
        event_hash = ?, mac = 'x' WHERE id = 1`, keptHash)

	recordN(t, s, 1)
	setActor(actor)
	if st := readTail(t, s); !st.currentNamesNewest() || st.anchorID != 5 {
		t.Fatalf("Expected a current-key anchor beside event 5, got %+v",
			st)
	}
	expectTampering(t, s)
}

// expectBindingRefused checks that the log in s, whose rows through
// event last fail under the key in use, reads as tampering because the
// binding of a tail anchor names an event that is not in the log.
func expectBindingRefused(t *testing.T, s *AuthStore, last int64) {
	t.Helper()

	expectTampering(t, s)
	err := s.verifyAuditTailAfterKeyChange(s.db, AuditEvent{ID: last,
		Hash: auditRowHash(t, s, last)})
	if !errors.Is(err, ErrAuditChainBroken) ||
		!strings.Contains(err.Error(), "is bound to a tail anchor naming") {
		t.Errorf("Expected the binding to be refused, got %v", err)
	}
}

// expectReanchorRefusedOnTheTail checks that the unattended re-anchor
// of the log at dir under key, given previousKey, refuses it because
// the tail anchor is not proven.
func expectReanchorRefusedOnTheTail(t *testing.T, dir string, key,
	previousKey []byte) {
	t.Helper()

	plan := planWithPreviousKey(t, dir, key, previousKey)
	if unattendedReanchorAccepts(plan) {
		t.Fatalf("Expected the unattended re-anchor to refuse, got %+v",
			plan)
	}
	if !errors.Is(plan.HistoryProofErr, errAuditTailUnproven) {
		t.Errorf("Expected the tail proof to fail, got %v",
			plan.HistoryProofErr)
	}
}

// TestDecoyStartedAnchorCannotProveARotatedCut is the case from the
// fourth review of #565 and its variants. Under one secret, five events
// are cut and the server led to start a current-key anchor beside a
// decoy; once the secret changes, the next event promotes that anchor,
// which the old secret really signed, with the decoy's primary anchor
// in its binding. The binding names an event that is not in the log,
// so verification reads the cut as tampering, and the previous secret
// refuses to prove it, whichever id the decoy took and whether it was
// deleted before or after the promotion.
func TestDecoyStartedAnchorCannotProveARotatedCut(t *testing.T) {
	for _, tt := range []struct {
		name        string
		decoyID     int64
		deleteAfter bool
	}{
		{name: "decoy at the next id", decoyID: 6},
		{name: "decoy past a gap", decoyID: 9},
		{name: "decoy deleted after the promotion", decoyID: 6,
			deleteAfter: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, dir := writeUnderKeys(t, 10)
			startAnchorOverDecoy(t, store, tt.decoyID)
			if !tt.deleteAfter {
				deleteAuditRows(t, store, tt.decoyID)
				expectTampering(t, store)
			}
			store.Close()

			store = openWithKey(t, dir, rotatedAuditKey)
			recordN(t, store, 1)
			if st := readTail(t, store); !st.bound.valid ||
				st.bound.id != tt.decoyID {
				t.Fatalf("Expected the promoted anchor to be bound to the "+
					"decoy, got %+v", st)
			}
			if tt.deleteAfter {
				deleteAuditRows(t, store, tt.decoyID)
			}
			expectBindingRefused(t, store, tt.decoyID+1)
			store.Close()

			expectReanchorRefusedOnTheTail(t, dir, rotatedAuditKey,
				AuditKeyForTesting())
		})
	}
}

// TestDecoyStartedAnchorBeforeTheFirstNewEvent checks the same decoy
// when the secret changes and nothing has yet been written under the
// new one: the current-key anchor still names the newest row, and the
// primary anchor beside it names the deleted decoy.
func TestDecoyStartedAnchorBeforeTheFirstNewEvent(t *testing.T) {
	store, dir := writeUnderKeys(t, 10)
	startAnchorOverDecoy(t, store, 6)
	deleteAuditRows(t, store, 6)
	store.Close()

	store = openWithKey(t, dir, rotatedAuditKey)
	expectBindingRefused(t, store, 7)
	store.Close()

	expectReanchorRefusedOnTheTail(t, dir, rotatedAuditKey,
		AuditKeyForTesting())
}

// TestAlteredRowStartedAnchorCannotProveARotatedCut checks the variant
// that keeps the row the binding names: event 5 is altered for the
// server's write and put back, so after the rotation verification can
// tell nothing from the rows, which all fail under the new secret, and
// reads the log as a key mismatch. The previous secret refuses it,
// because the row the binding names verifies under the secret that
// signed the anchor, which a genuine change of secret never leaves.
func TestAlteredRowStartedAnchorCannotProveARotatedCut(t *testing.T) {
	for _, write := range []bool{false, true} {
		store, dir := writeUnderKeys(t, 10)
		startAnchorOverAlteredRow(t, store)
		store.Close()

		store = openWithKey(t, dir, rotatedAuditKey)
		if write {
			recordN(t, store, 1)
		}
		expectKeyMismatch(t, store)
		store.Close()

		plan := planWithPreviousKey(t, dir, rotatedAuditKey,
			AuditKeyForTesting())
		if unattendedReanchorAccepts(plan) ||
			!errors.Is(plan.HistoryProofErr, errAuditTailUnproven) ||
			!strings.Contains(plan.HistoryProofErr.Error(),
				"verifies under the previous secret") {
			t.Errorf("Expected the re-anchor to refuse the binding (write "+
				"under the new secret: %v), got %+v", write, plan)
		}
	}
}

// TestDecoyPutBackAfterThePromotion checks the variant that satisfies
// the binding by putting the decoy back, at the id and hash the
// promoted anchor is bound to, after the write under the new secret.
// The binding then names a row that exists, but the decoy does not
// verify under the previous secret either, so the unattended re-anchor
// still refuses the history, whatever verification makes of the log.
func TestDecoyPutBackAfterThePromotion(t *testing.T) {
	store, dir := writeUnderKeys(t, 10)
	startAnchorOverDecoy(t, store, 6)
	deleteAuditRows(t, store, 6)
	store.Close()

	store = openWithKey(t, dir, rotatedAuditKey)
	recordN(t, store, 1)
	keptHash := auditRowHash(t, store, 5)
	tamperDB(t, store.db, `INSERT INTO audit_events (id, occurred_at,
            actor_type, actor_name, action, outcome, prev_hash, hash,
            hash_version)
        VALUES (6, ?, 'system', 'system', 'user.create', 'success',
            'unused', ?, 3)`,
		time.Now().UTC().Format(auditTimeLayout), keptHash)
	if _, _, err := store.VerifyAuditChain(); err == nil {
		t.Error("Expected verification to refuse the restored decoy")
	}
	store.Close()

	plan := planWithPreviousKey(t, dir, rotatedAuditKey,
		AuditKeyForTesting())
	if unattendedReanchorAccepts(plan) {
		t.Errorf("Expected the re-anchor to refuse the decoy, got %+v", plan)
	}
}

// TestDecoyStartedAnchorAcrossTwoRotations checks the decoy followed by
// two changes of secret: neither earlier secret proves the log, since
// the history spans both, and the tail proof refuses under each.
func TestDecoyStartedAnchorAcrossTwoRotations(t *testing.T) {
	store, dir := writeUnderKeys(t, 10)
	startAnchorOverDecoy(t, store, 6)
	deleteAuditRows(t, store, 6)
	store.Close()
	for _, key := range [][]byte{rotatedAuditKey, thirdAuditKey} {
		store = openWithKey(t, dir, key)
		recordN(t, store, 1)
		store.Close()
	}

	for _, previous := range [][]byte{AuditKeyForTesting(),
		rotatedAuditKey} {
		plan := planWithPreviousKey(t, dir, thirdAuditKey, previous)
		if unattendedReanchorAccepts(plan) {
			t.Errorf("Expected the unattended re-anchor to refuse, got %+v",
				plan)
		}
	}
}

// TestHonestRotationsStillProveTheirBinding checks that the binding
// rules leave genuine logs alone: a log written under three secrets,
// whose current-key anchor or promoted primary anchor is bound to the
// last event under the oldest secret, still exists in the log and fails
// under the previous secret, so the tail proof holds there even though
// the history, spanning two secrets, cannot be proven.
func TestHonestRotationsStillProveTheirBinding(t *testing.T) {
	store, dir := writeUnderKeys(t, 3, rotatedAuditKey)
	store.Close()
	for _, write := range []bool{false, true} {
		store = openWithKey(t, dir, thirdAuditKey)
		if write {
			recordN(t, store, 1)
		}
		expectKeyMismatch(t, store)
		store.Close()
		expectTailUnproven(t, planWithPreviousKey(t, dir, thirdAuditKey,
			rotatedAuditKey), false)
	}
}

// TestReanchorProofIsOfTheScannedLog checks that the proof refuses a log
// that has changed since the plan scanned it, rather than proving a log
// other than the one the re-anchor will accept.
func TestReanchorProofIsOfTheScannedLog(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(t *testing.T, s *AuthStore)
	}{
		{name: "the tail anchor", change: func(t *testing.T, s *AuthStore) {
			tamperDB(t, s.db, "DELETE FROM audit_tail WHERE id = 2")
		}},
		{name: "an event", change: func(t *testing.T, s *AuthStore) {
			recordN(t, s, 1)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir, store, _ := rotatedLog(t, 3, 2)
			store.Close()
			store = openWithKey(t, dir, rotatedAuditKey)

			var plan AuditRechainPlan
			if err := store.auditReanchorPlan(&plan); err != nil {
				t.Fatalf("Failed to plan the re-anchor: %v", err)
			}
			tt.change(t, store)
			if err := store.proveAuditReanchorPlan(&plan,
				AuditKeyForTesting()); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if plan.HistoryProven || plan.HistoryProofErr == nil ||
				!strings.Contains(plan.HistoryProofErr.Error(),
					"changed since") {
				t.Errorf("Expected the proof to refuse a changed log, got "+
					"%+v", plan)
			}
		})
	}
}

// TestAuditTailBoundRow checks each binding the bound-row lookup
// refuses, and that a read failure is reported by it and by both of its
// callers rather than taken as a missing row.
func TestAuditTailBoundRow(t *testing.T) {
	store, _ := newReopenableStore(t)
	recordN(t, store, 3)
	rows := auditRowStates(t, store)

	for _, tt := range []struct {
		name  string
		b     auditTailBinding
		id    int64
		found bool
	}{
		{name: "an earlier row", b: auditTailBinding{valid: true,
			id: rows[1].ID, hash: rows[1].Hash}, id: rows[2].ID, found: true},
		{name: "the anchor's own row", b: auditTailBinding{valid: true,
			id: rows[2].ID, hash: rows[2].Hash}, id: rows[2].ID},
		{name: "a later row", b: auditTailBinding{valid: true,
			id: rows[2].ID, hash: rows[2].Hash}, id: rows[1].ID},
		{name: "another hash", b: auditTailBinding{valid: true,
			id: rows[1].ID, hash: rows[0].Hash}, id: rows[2].ID},
		{name: "a missing row", b: auditTailBinding{valid: true, id: -5,
			hash: rows[0].Hash}, id: rows[2].ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			row, err := auditTailBoundRow(store.db, tt.b, tt.id)
			if err != nil || (row != nil) != tt.found {
				t.Errorf("Expected found %v, got %+v, %v", tt.found, row, err)
			}
			err = checkAuditTailBinding(store.db, "tail anchor", tt.id, tt.b)
			if (err == nil) != tt.found {
				t.Errorf("Expected the binding check to pass: %v, got %v",
					tt.found, err)
			}
		})
	}

	if err := checkAuditTailBinding(store.db, "tail anchor", rows[2].ID,
		auditTailBinding{}); err != nil {
		t.Errorf("Expected no binding to pass, got %v", err)
	}
	if proof, err := proveAuditTailBinding(store.db, rotatedAuditKey,
		"tail anchor", rows[2].ID, auditTailBinding{}); proof != nil ||
		err != nil {
		t.Errorf("Expected no binding to need no proof, got %v, %v", proof,
			err)
	}

	store.db.Close()
	b := auditTailBinding{valid: true, id: rows[1].ID, hash: rows[1].Hash}
	if _, err := auditTailBoundRow(store.db, b, rows[2].ID); err == nil {
		t.Error("Expected a closed database to be reported")
	}
	if err := checkAuditTailBinding(store.db, "tail anchor", rows[2].ID,
		b); err == nil || errors.Is(err, ErrAuditChainBroken) {
		t.Errorf("Expected a read error, got %v", err)
	}
	if proof, err := proveAuditTailBinding(store.db, rotatedAuditKey,
		"tail anchor", rows[2].ID, b); err == nil || proof != nil {
		t.Errorf("Expected a read error, got %v, %v", proof, err)
	}
}
