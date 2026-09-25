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
	"errors"
	"strings"
	"testing"
	"time"
)

// forgeAuditRow inserts a row, with the given id, action and details,
// that links to the newest row but whose hash no key produced: what an
// attacker who can write auth.db, but does not have the secret, can add.
func forgeAuditRow(t *testing.T, s *AuthStore, id int64, action,
	details string) {
	t.Helper()

	rows := auditRowStates(t, s)
	prev := rows[len(rows)-1].Hash
	if _, err := s.db.Exec(`INSERT INTO audit_events (id, occurred_at,
            actor_type, actor_name, action, outcome, details, prev_hash,
            hash, hash_version)
        VALUES (?, ?, 'system', 'system', ?, 'success', NULLIF(?, ''), ?,
            ?, 2)`, id, time.Now().UTC().Format(auditTimeLayout), action,
		details, prev, strings.Repeat("f", 64)); err != nil {
		t.Fatalf("Failed to insert a forged row: %v", err)
	}
}

// declinedReanchorPlan returns the plan a re-chain on dir would show,
// declining it.
func declinedReanchorPlan(t *testing.T, dir string,
	key []byte) AuditRechainPlan {
	t.Helper()

	var seen AuditRechainPlan
	if _, err := RechainAuditLog(dir, key, systemActor,
		func(plan AuditRechainPlan) (bool, error) {
			seen = plan
			return false, nil
		}); err != nil {
		t.Fatalf("Failed to plan the re-chain: %v", err)
	}

	return seen
}

// TestKeyMismatchRequiresTheWholeLog checks that a rotated secret does
// not excuse damage elsewhere in the log. The re-anchor accepts as
// history everything through the last row that fails, so a verdict of
// key mismatch, which lets -confirm-rechain run unattended, must hold
// for the whole log: otherwise a rotation, or an edit to the oldest row
// that looks like one, would carry a later deletion through with it.
func TestKeyMismatchRequiresTheWholeLog(t *testing.T) {
	tests := []struct {
		name   string
		damage func(t *testing.T, s *AuthStore, oldIDs []int64)
	}{
		{"a row deleted after the rotation",
			func(t *testing.T, s *AuthStore, oldIDs []int64) {
				deleteAuditRows(t, s, oldIDs[len(oldIDs)-1]+2)
			}},
		{"a forged row at the tail",
			func(t *testing.T, s *AuthStore, _ []int64) {
				forgeAuditRow(t, s, 1000, "user.create", "")
			}},
		{"rows lost from the tail",
			func(t *testing.T, s *AuthStore, _ []int64) {
				rows := auditRowStates(t, s)
				deleteAuditRows(t, s, rows[len(rows)-1].ID)
			}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, store, oldIDs := rotatedLog(t, 2, 3)
			tt.damage(t, store, oldIDs)

			if _, err := store.VerifyAuditLog(); !errors.Is(err,
				ErrAuditChainBroken) || errors.Is(err, ErrAuditKeyMismatch) {
				t.Errorf("Expected tampering, not a key mismatch, got %v", err)
			}
			if _, err := store.PurgeAuditEvents(time.Now().UTC().Add(
				-48 * time.Hour)); !errors.Is(err, ErrAuditChainBroken) ||
				errors.Is(err, ErrAuditKeyMismatch) {
				t.Errorf("Expected the purge to refuse as tampering, got %v",
					err)
			}
			store.Close()

			if plan := declinedReanchorPlan(t, dir,
				rotatedAuditKey); plan.KeyMismatch {
				t.Errorf("Expected the plan not to be a key mismatch, "+
					"which -confirm-rechain would act on: %v", plan.Problem)
			}
		})
	}
}

// TestForgedPurgeOnAReanchoredLogIsNotAKeyMismatch checks that one
// forged audit.purge row, which makes the verifier set the anchor aside
// and hold every row to the key, does not turn the history the
// re-anchor accepted into what looks like a changed secret.
func TestForgedPurgeOnAReanchoredLogIsNotAKeyMismatch(t *testing.T) {
	dir, store, _ := rotatedLog(t, 2, 1)
	store.Close()
	reanchor(t, dir, rotatedAuditKey)

	store = openWithKey(t, dir, rotatedAuditKey)
	forgeAuditRow(t, store, 1000, auditActionPurge,
		`{"oldest_retained_id":1,"oldest_retained_hash":"`+
			strings.Repeat("a", 64)+`"}`)
	if _, err := store.VerifyAuditLog(); !errors.Is(err,
		ErrAuditChainBroken) || errors.Is(err, ErrAuditKeyMismatch) {
		t.Errorf("Expected tampering, not a key mismatch, got %v", err)
	}
	store.Close()

	if plan := declinedReanchorPlan(t, dir, rotatedAuditKey); plan.KeyMismatch {
		t.Errorf("Expected the plan not to be a key mismatch: %v",
			plan.Problem)
	}
}

// TestReanchorScanFlagsAFailureAfterAVerifiedRow checks the scan's own
// record of whether its history has the shape of a key change, which
// the plan relies on in case the log changed after the verifier read it.
func TestReanchorScanFlagsAFailureAfterAVerifiedRow(t *testing.T) {
	store, _ := newReopenableStore(t)
	recordAt(t, store, "a", time.Now().UTC())
	recordAt(t, store, "b", time.Now().UTC())
	forgeAuditRow(t, store, 1000, "user.create", "")

	scan, err := store.scanAuditForReanchor(store.db)
	if err != nil || !scan.failsAfterVerified || scan.through != 1000 {
		t.Errorf("Expected a failure after verified rows through row 1000, "+
			"got %+v and %v", scan, err)
	}
}

// editAuditRow changes one row's content in place, keeping its stored
// hash and link, so that it fails to verify whilst still linking on
// both sides: the edit that mimics a change of secret.
func editAuditRow(t *testing.T, s *AuthStore, id int64) {
	t.Helper()

	for _, stmt := range []string{
		"DROP TRIGGER " + auditNoUpdateTrigger,
		"UPDATE audit_events SET actor_name = 'edited' WHERE id = ?",
		auditSchemaDDL,
	} {
		var err error
		if strings.Contains(stmt, "?") {
			_, err = s.db.Exec(stmt, id)
		} else {
			_, err = s.db.Exec(stmt)
		}
		if err != nil {
			t.Fatalf("Failed to edit audit row %d: %v", id, err)
		}
	}
}

// TestVerifiedAnchorRulesOutAKeyMismatch checks that where a purge or
// re-chain event that verifies records where the log begins, a failing
// row is tampering, however the rows around it look. That event was
// written under the key in use, after every older row had been checked
// or accepted, so a rotation cannot explain the failure, and an edit
// just past its head or history must not pass for one: -confirm-rechain
// would then accept whatever was deleted before it.
func TestVerifiedAnchorRulesOutAKeyMismatch(t *testing.T) {
	t.Run("history altered and the next row edited", func(t *testing.T) {
		dir, store, oldIDs := rotatedLog(t, 2, 1)
		store.Close()
		reanchor(t, dir, rotatedAuditKey)

		store = openWithKey(t, dir, rotatedAuditKey)
		deleteAuditRows(t, store, oldIDs[0])
		editAuditRow(t, store, oldIDs[len(oldIDs)-1]+1)
		assertTamperingNotKeyMismatch(t, dir, store, rotatedAuditKey)
	})

	t.Run("head deleted and the next row edited", func(t *testing.T) {
		store, _ := purgedLog(t)
		dir := storeDir(t, store)
		rows := auditRowStates(t, store)
		deleteAuditRows(t, store, rows[0].ID)
		editAuditRow(t, store, rows[1].ID)
		assertTamperingNotKeyMismatch(t, dir, store, AuditKeyForTesting())
	})
}

// assertTamperingNotKeyMismatch checks that the verifier and the
// re-chain plan, run under key, both treat the log in dir as tampered
// with, and closes store.
func assertTamperingNotKeyMismatch(t *testing.T, dir string,
	store *AuthStore, key []byte) {
	t.Helper()

	if _, err := store.VerifyAuditLog(); !errors.Is(err,
		ErrAuditChainBroken) || errors.Is(err, ErrAuditKeyMismatch) {
		t.Errorf("Expected tampering, not a key mismatch, got %v", err)
	}
	store.Close()

	if plan := declinedReanchorPlan(t, dir, key); plan.KeyMismatch {
		t.Errorf("Expected the plan not to be a key mismatch: %v",
			plan.Problem)
	}
}

// TestReanchorRechecksTheTailUnderTheLock checks that an unattended
// re-anchor refuses a log that has lost rows from its tail since it was
// verified, which neither its scan nor the comparison with the plan
// sees.
func TestReanchorRechecksTheTailUnderTheLock(t *testing.T) {
	dir, store, _ := rotatedLog(t, 2, 2)
	rows := auditRowStates(t, store)
	deleteAuditRows(t, store, rows[len(rows)-1].ID)
	store.Close()

	// The plan as it would stand had the verifier read the log before
	// the tail went.
	plan := declinedReanchorPlan(t, dir, rotatedAuditKey)
	plan.KeyMismatch = true

	store = openWithKey(t, dir, rotatedAuditKey)
	before := auditRowCount(t, store)
	err := store.reanchorAuditLogTx(systemActor, plan)
	if !errors.Is(err, ErrAuditRechainChanged) ||
		!errors.Is(err, ErrAuditChainBroken) {
		t.Fatalf("Expected the re-anchor to refuse the lost tail, got %v", err)
	}
	if after := auditRowCount(t, store); after != before {
		t.Errorf("Expected nothing written, went from %d to %d rows",
			before, after)
	}
}

// TestReanchorDoesNotSignAnUnverifiedPreviousHead checks that what a
// forged anchor says is not copied into the keyed re-chain event, which
// would lend it the key; only its id is recorded.
func TestReanchorDoesNotSignAnUnverifiedPreviousHead(t *testing.T) {
	store, dir := newReopenableStore(t)
	recordAt(t, store, "a", time.Now().UTC())
	forgeAuditRow(t, store, 1000, auditActionRechain,
		`{"oldest_retained_id":1,"oldest_retained_hash":"`+
			strings.Repeat("b", 64)+`"}`)
	store.Close()

	plan := reanchor(t, dir, AuditKeyForTesting())
	if plan.PreviousHead == nil || plan.PreviousHead.Verified ||
		plan.PreviousHead.EventID != 1000 {
		t.Fatalf("Expected the forged event as an unverified previous head, "+
			"got %+v", plan.PreviousHead)
	}

	store = openWithKey(t, dir, AuditKeyForTesting())
	ev, _ := newestRechainAnchor(t, store)
	var details map[string]any
	if err := json.Unmarshal(ev.Details, &details); err != nil {
		t.Fatalf("Failed to read the re-chain event's details: %v", err)
	}
	if details["previous_head_event_id"] != float64(1000) ||
		details["previous_head_verified"] != false {
		t.Errorf("Expected the forged event's id and status, got %v", details)
	}
	for _, key := range []string{"previous_oldest_retained_id",
		"previous_oldest_retained_hash"} {
		if _, ok := details[key]; ok {
			t.Errorf("Expected %s not to be signed from a forged event, got %v",
				key, details)
		}
	}
	if _, err := store.VerifyAuditLog(); err != nil {
		t.Errorf("Expected the re-anchored log to verify: %v", err)
	}
}
