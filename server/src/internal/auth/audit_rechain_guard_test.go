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

// TestRechainPlanRefusesUnkeyedRowsAboveKeyedRows covers VULN-301: the
// re-chain exists for a log inherited wholly from a release that
// predates the key, and must refuse any other shape.
//
// An attacker with write access to auth.db but no read access to the
// server secret can compute the unkeyed rendering as easily as the
// server can, so relabelling a log to version 1 and recomputing it is
// within reach. The refusal to open that the relabelling causes is the
// pressure that gets an operator to run the re-chain, which would sign
// the result under the real key. Refusing the interleaved shape in the
// plan, before the operator is shown any figures, is what closes that.
func TestRechainPlanRefusesUnkeyedRowsAboveKeyedRows(t *testing.T) {
	store, dir := newReopenableStore(t)
	for i := 0; i < 3; i++ {
		if err := store.recordAuditInOwnTx(newEvent(systemActor,
			"user.create", "user", nil, "alice", nil)); err != nil {
			t.Fatalf("Failed to record a keyed event: %v", err)
		}
	}
	insertAuditRowAsV1(t, store, newEvent(systemActor, "user.delete",
		"user", nil, "alice", nil))
	store.Close()

	inspect := openUnkeyedStore(t, dir)
	_, err := inspect.auditRechainPlan()
	if !errors.Is(err, ErrAuditUnkeyedRow) {
		t.Fatalf("Expected the plan to refuse the interleaved log, got %v",
			err)
	}
	for _, want := range []string{"already keyed",
		"restore auth.db from a known-good copy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expected the refusal to mention %q, got %v", want, err)
		}
	}
	inspect.Close()

	// And the same through the public entry point, which must write
	// nothing: the rows are exactly as they were.
	before := rawAuditRowStates(t, openRawAuditDB(t, dir))
	result, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(AuditRechainPlan) (bool, error) {
			t.Error("Expected no confirmation prompt for an interleaved log")
			return true, nil
		})
	if !errors.Is(err, ErrAuditUnkeyedRow) {
		t.Fatalf("Expected RechainAuditLog to refuse, got %v", err)
	}
	if result.Confirmed {
		t.Error("Expected the result to report nothing confirmed")
	}
	after := rawAuditRowStates(t, openRawAuditDB(t, dir))
	if len(before) != len(after) {
		t.Fatalf("Expected %d rows to survive untouched, got %d",
			len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("Row %d changed: %+v became %+v", before[i].ID,
				before[i], after[i])
		}
	}
}

// TestMixedAuditLogRefusedWhateverTheRowOrder covers VULN-307. The
// check this replaced counted keyed rows below the lowest unkeyed one,
// which an attacker satisfies without the key: an explicit
// INSERT ... (id, ...) VALUES (-1, ...) is accepted whatever
// AUTOINCREMENT has assigned so far, so one unkeyed row written below
// the genuine first row makes the lowest row unkeyed and leaves nothing
// keyed beneath it. The forged event then goes on the top of the log,
// also unkeyed, and the operator is shown the ordinary upgrade message
// pointing at -rechain-audit-log rather than a refusal, after which the
// re-chain signs both forged rows under the real secret.
func TestMixedAuditLogRefusedWhateverTheRowOrder(t *testing.T) {
	store, dir := newReopenableStore(t)
	for i := 0; i < 3; i++ {
		if err := store.recordAuditInOwnTx(newEvent(systemActor,
			"user.create", "user", nil, "alice", nil)); err != nil {
			t.Fatalf("Failed to record a keyed event: %v", err)
		}
	}

	// Below the genuine first row, so that the lowest row in the log is
	// unkeyed, and above the keyed rows, where the forged event goes.
	insertAuditRowAsV1AtID(t, store, newEvent(systemActor, "user.create",
		"user", nil, "backdated", nil), -1)
	insertAuditRowAsV1(t, store, newEvent(systemActor, "user.delete",
		"user", nil, "alice", nil))

	lowest, keyedBelow := lowestAuditRowID(t, store), keyedAuditRowsBelow(t,
		store, 0)
	if lowest >= 1 || keyedBelow != 0 {
		t.Fatalf("The fixture is wrong: it must leave the lowest row (%d) "+
			"unkeyed with no keyed row beneath it, got %d keyed below",
			lowest, keyedBelow)
	}
	store.Close()

	// Both gates must refuse, and in the mixed-log terms rather than the
	// upgrade terms: an operator told to run -rechain-audit-log here
	// would have the server sign the forged rows.
	inspect := openUnkeyedStore(t, dir)
	for name, err := range map[string]error{
		"ensureNoUnkeyedAuditRows": inspect.ensureNoUnkeyedAuditRows(),
		"auditRechainPlan":         planError(inspect),
	} {
		if !errors.Is(err, ErrAuditUnkeyedRow) {
			t.Fatalf("Expected %s to refuse the mixed log, got %v", name, err)
		}
		for _, want := range []string{"already keyed",
			"restore auth.db from a known-good copy"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("Expected the %s refusal to mention %q, got %v",
					name, want, err)
			}
		}
		if strings.Contains(err.Error(),
			"re-hash the existing events under the server secret") {
			t.Errorf("Expected %s not to offer the re-chain as the remedy, "+
				"got %v", name, err)
		}
	}
}

// planError runs the plan for its error alone.
func planError(s *AuthStore) error {
	_, err := s.auditRechainPlan()
	return err
}

// lowestAuditRowID reports MIN(id) over the log.
func lowestAuditRowID(t *testing.T, s *AuthStore) int64 {
	t.Helper()

	var id int64
	if err := s.db.QueryRow("SELECT MIN(id) FROM audit_events").
		Scan(&id); err != nil {
		t.Fatalf("Failed to read the lowest audit row id: %v", err)
	}

	return id
}

// keyedAuditRowsBelow counts the keyed rows beneath the given id, which
// is what the check this replaced consulted.
func keyedAuditRowsBelow(t *testing.T, s *AuthStore, id int64) int64 {
	t.Helper()

	var count int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_events
        WHERE hash_version <> 1 AND id < ?`, id).Scan(&count); err != nil {
		t.Fatalf("Failed to count the keyed rows below %d: %v", id, err)
	}

	return count
}

// TestKeyedRowCheckReportsAQueryFailure covers the branch where the
// check cannot run at all. A check that could not run has not passed,
// so the error is returned rather than read as a clean log.
func TestKeyedRowCheckReportsAQueryFailure(t *testing.T) {
	store, _ := newReopenableStore(t)
	if _, err := store.db.Exec("DROP TABLE audit_events"); err != nil {
		t.Fatalf("Failed to drop audit_events: %v", err)
	}

	err := store.checkNoKeyedAuditRows()
	if err == nil {
		t.Fatal("Expected an error when the count cannot run")
	}
	if !strings.Contains(err.Error(), "failed to count keyed audit events") {
		t.Errorf("Expected the failing count to be named, got %v", err)
	}
}

// TestVerifyLegacyChainRejectsADowngrade checks the plan's report of
// the log it is about to replace cannot call a downgraded log clean.
// Each row is checked under the rendering it claims, so without the
// downgrade check a keyed log with one unkeyed row appended recomputes
// perfectly and reports LegacyChainOK.
func TestVerifyLegacyChainRejectsADowngrade(t *testing.T) {
	store, _ := newReopenableStore(t)
	for i := 0; i < 2; i++ {
		if err := store.recordAuditInOwnTx(newEvent(systemActor,
			"user.create", "user", nil, "alice", nil)); err != nil {
			t.Fatalf("Failed to record a keyed event: %v", err)
		}
	}
	insertAuditRowAsV1(t, store, newEvent(systemActor, "user.delete",
		"user", nil, "alice", nil))

	firstBad, err := store.verifyLegacyAuditChain()
	if !errors.Is(err, ErrAuditChainDowngraded) {
		t.Fatalf("Expected the downgraded row to be reported, got %v", err)
	}

	var lowestUnkeyed int64
	if err := store.db.QueryRow(
		"SELECT MIN(id) FROM audit_events WHERE hash_version = 1").
		Scan(&lowestUnkeyed); err != nil {
		t.Fatalf("Failed to find the unkeyed row: %v", err)
	}
	if firstBad != lowestUnkeyed {
		t.Errorf("Expected row %d to be reported, got %d", lowestUnkeyed,
			firstBad)
	}
}

// TestRechainRefusesWhenTheLogChangedSinceThePlan covers VULN-302. The
// plan is taken, the operator is asked, and the rewrite happens in a
// transaction opened afterwards; s.mu excludes other goroutines in this
// process and not another writer on the same file. The confirmation
// here stands in for that writer by appending a row whilst the prompt
// is notionally waiting.
func TestRechainRefusesWhenTheLogChangedSinceThePlan(t *testing.T) {
	store, dir := newReopenableStore(t)
	inheritV1Rows(t, store, 3)
	store.Close()

	_, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(plan AuditRechainPlan) (bool, error) {
			if plan.Events != 3 {
				t.Errorf("Expected a plan over 3 rows, got %d", plan.Events)
			}
			// A second writer on auth.db, which is all an attacker
			// needs to be.
			second := openUnkeyedStore(t, dir)
			insertAuditRowAsV1(t, second, newEvent(systemActor,
				"user.create", "user", nil, "forged", nil))
			second.Close()

			return true, nil
		})
	if !errors.Is(err, ErrAuditRechainChanged) {
		t.Fatalf("Expected the rewrite to refuse the changed log, got %v", err)
	}
	for _, want := range []string{"Nothing has been signed",
		"the plan you approved described 3 event(s)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expected the refusal to mention %q, got %v", want, err)
		}
	}

	// The appended row is still unkeyed, so nothing was signed.
	states := rawAuditRowStates(t, openRawAuditDB(t, dir))
	if len(states) != 4 {
		t.Fatalf("Expected 4 rows, got %d", len(states))
	}
	for _, st := range states {
		if st.HashVersion != 1 {
			t.Errorf("Row %d was re-hashed as version %d", st.ID,
				st.HashVersion)
		}
	}
}

// TestPurgeIgnoresBackdatedNewestRows covers VULN-303 on a keyed log.
// occurred_at is a value in the file and so is attacker-controlled;
// deleting by it alone would let a backdated row steer the purge into
// removing the newest events, relinking the chain and, through the
// audit.purge event it appends, restoring the sqlite_sequence agreement
// verifyAuditTail reads.
func TestPurgeIgnoresBackdatedNewestRows(t *testing.T) {
	store, _ := newReopenableStore(t)

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		ev := newEvent(systemActor, "user.create", "user", nil, "alice", nil)
		ev.OccurredAt = now.Add(-time.Duration(3-i) * time.Minute)
		if err := store.recordAuditInOwnTx(ev); err != nil {
			t.Fatalf("Failed to record a recent event: %v", err)
		}
	}
	// The rows an attacker wants gone: newest by id, backdated well
	// beyond any retention window.
	for i := 0; i < 2; i++ {
		ev := newEvent(systemActor, "user.delete", "user", nil, "alice", nil)
		ev.OccurredAt = now.Add(-72 * time.Hour)
		if err := store.recordAuditInOwnTx(ev); err != nil {
			t.Fatalf("Failed to record a backdated event: %v", err)
		}
	}

	before := auditRowStates(t, store)
	removed, err := store.PurgeAuditEvents(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Failed to purge: %v", err)
	}
	if removed != 0 {
		t.Fatalf("Expected the purge to remove nothing, got %d", removed)
	}

	after := auditRowStates(t, store)
	if len(after) != len(before) {
		t.Fatalf("Expected %d rows to survive, got %d", len(before),
			len(after))
	}

	// An honest prefix is still purged: the oldest rows by id are also
	// the oldest by time, so the window reaches them.
	honest, _ := newReopenableStore(t)
	for i := 0; i < 3; i++ {
		ev := newEvent(systemActor, "user.create", "user", nil, "alice", nil)
		ev.OccurredAt = now.Add(-48 * time.Hour)
		if err := honest.recordAuditInOwnTx(ev); err != nil {
			t.Fatalf("Failed to record an old event: %v", err)
		}
	}
	if err := honest.recordAuditInOwnTx(newEvent(systemActor, "user.create",
		"user", nil, "recent", nil)); err != nil {
		t.Fatalf("Failed to record a recent event: %v", err)
	}
	removed, err = honest.PurgeAuditEvents(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Failed to purge the honest log: %v", err)
	}
	if removed != 3 {
		t.Errorf("Expected the honest prefix of 3 rows to be purged, got %d",
			removed)
	}
}

// TestRechainKeepsThePurgedPrefixLink checks the one row the re-chain
// does not re-link. Retention deletes a prefix of the log, so the
// oldest surviving row points at a hash that is no longer in the table,
// and there is nothing to replace it with: its prev_hash must be
// carried over exactly as written. Every other fixture here seeds a log
// whose first row is the genesis row, whose prev_hash is empty, and so
// cannot tell a carried-over link from a blanked one.
func TestRechainKeepsThePurgedPrefixLink(t *testing.T) {
	store, dir := newReopenableStore(t)

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		ev := newEvent(systemActor, "user.create", "user", nil, "old", nil)
		ev.OccurredAt = now.Add(-48 * time.Hour)
		if err := store.recordAuditInOwnTx(ev); err != nil {
			t.Fatalf("Failed to record an old event: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := store.recordAuditInOwnTx(newEvent(systemActor,
			"user.create", "user", nil, "recent", nil)); err != nil {
			t.Fatalf("Failed to record a recent event: %v", err)
		}
	}

	removed, err := store.PurgeAuditEvents(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Failed to purge: %v", err)
	}
	if removed != 3 {
		t.Fatalf("Expected the 3 oldest rows to be purged, got %d", removed)
	}

	oldest := auditRowStates(t, store)[0]
	if oldest.PrevHash == "" {
		t.Fatal("The fixture is wrong: the oldest surviving row should " +
			"still point at the purged row before it")
	}
	store.Close()

	if _, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		alwaysConfirmRechain); err != nil {
		t.Fatalf("Failed to re-chain the log: %v", err)
	}

	reopened, err := reopenStore(t, dir)
	if err != nil {
		t.Fatalf("Failed to reopen the re-chained store: %v", err)
	}
	rechained := auditRowStates(t, reopened)[0]
	if rechained.ID != oldest.ID {
		t.Fatalf("Expected row %d to still be the oldest, got %d", oldest.ID,
			rechained.ID)
	}
	if rechained.PrevHash != oldest.PrevHash {
		t.Errorf("Expected the oldest row to keep prev_hash %q, got %q",
			oldest.PrevHash, rechained.PrevHash)
	}

	if _, firstBad, err := reopened.VerifyAuditChain(); err != nil {
		t.Errorf("Expected the re-chained log to verify, got %v (row %d)",
			err, firstBad)
	}
}
