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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// alwaysConfirmRechain agrees to whatever plan it is shown. Tests that
// are checking the confirmation gate itself use their own callback.
func alwaysConfirmRechain(AuditRechainPlan) (bool, error) {
	return true, nil
}

// newReopenableStore returns an open store and the directory holding
// its auth.db, so that a test can close it, tamper with the file and
// open it again the way a restart would.
func newReopenableStore(t *testing.T) (*AuthStore, string) {
	t.Helper()

	dir := t.TempDir()
	store, err := NewAuthStore(dir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create the auth store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return store, dir
}

// reopenStore opens a second store on a directory, returning the error
// rather than failing, because most callers here are checking that the
// open is refused.
func reopenStore(t *testing.T, dir string) (*AuthStore, error) {
	t.Helper()

	store, err := NewAuthStore(dir, 0, 0, AuditKeyForTesting())
	if store != nil {
		t.Cleanup(func() { store.Close() })
	}

	return store, err
}

// auditRowState is the part of a row the re-chain rewrites, which is
// what the rollback tests compare before and after.
type auditRowState struct {
	ID          int64
	PrevHash    string
	Hash        string
	HashVersion int
}

// auditRowStates reads every row's chain columns in id order.
func auditRowStates(t *testing.T, s *AuthStore) []auditRowState {
	t.Helper()

	rows, err := s.db.Query(
		"SELECT id, prev_hash, hash, hash_version FROM audit_events ORDER BY id")
	if err != nil {
		t.Fatalf("Failed to read the audit rows: %v", err)
	}
	defer rows.Close()

	var states []auditRowState
	for rows.Next() {
		var st auditRowState
		if err := rows.Scan(&st.ID, &st.PrevHash, &st.Hash,
			&st.HashVersion); err != nil {
			t.Fatalf("Failed to scan an audit row: %v", err)
		}
		states = append(states, st)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Failed to walk the audit rows: %v", err)
	}

	return states
}

// openUnkeyedStore opens a store on a database that still holds version
// 1 rows, which an ordinary open refuses. It stands in for the
// privileged open RechainAuditLog performs, and lets a test inspect or
// damage such a database.
func openUnkeyedStore(t *testing.T, dir string) *AuthStore {
	t.Helper()

	store, err := newAuthStore(dir, 0, 0, AuditKeyForTesting(), true)
	if err != nil {
		t.Fatalf("Failed to open the store for inspection: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return store
}

// openRawAuditDB opens auth.db directly, without going through
// NewAuthStore. The distinction matters for the trigger assertions:
// every store open runs ensureAuditSchema, which re-creates the trigger
// with CREATE TRIGGER IF NOT EXISTS, so a check made through a reopened
// store would pass whether the re-chain restored the trigger or not.
func openRawAuditDB(t *testing.T, dir string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(dir, "auth.db")+
		"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("Failed to open auth.db directly: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

// rawTriggerPresent reports whether the append-only trigger is on the
// table, read through a connection that has not re-created it.
func rawTriggerPresent(t *testing.T, db *sql.DB) bool {
	t.Helper()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
         WHERE type = 'trigger' AND name = ?`,
		auditNoUpdateTrigger).Scan(&count); err != nil {
		t.Fatalf("Failed to look for the append-only trigger: %v", err)
	}

	return count > 0
}

// rawAuditRowStates reads every row's chain columns through a
// connection that has not touched the schema.
func rawAuditRowStates(t *testing.T, db *sql.DB) []auditRowState {
	t.Helper()

	rows, err := db.Query(
		"SELECT id, prev_hash, hash, hash_version FROM audit_events ORDER BY id")
	if err != nil {
		t.Fatalf("Failed to read the audit rows: %v", err)
	}
	defer rows.Close()

	var states []auditRowState
	for rows.Next() {
		var st auditRowState
		if err := rows.Scan(&st.ID, &st.PrevHash, &st.Hash,
			&st.HashVersion); err != nil {
			t.Fatalf("Failed to scan an audit row: %v", err)
		}
		states = append(states, st)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Failed to walk the audit rows: %v", err)
	}

	return states
}

// assertRawUpdatesRefused checks the trigger is not merely present in
// sqlite_master but actually refusing an UPDATE.
func assertRawUpdatesRefused(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(
		"UPDATE audit_events SET action = 'tampered' WHERE id = 1")
	if err == nil {
		t.Fatal("Expected the append-only trigger to refuse the update")
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Errorf("Expected the append-only trigger to refuse the update, "+
			"got %v", err)
	}
}

// corruptAuditRow rewrites a row's action without touching its hash,
// which is what a careless editor of auth.db leaves behind: a row that
// no longer recomputes. The append-only trigger refuses an UPDATE, so
// it goes through a DELETE and a re-INSERT.
func corruptAuditRow(t *testing.T, s *AuthStore, id int64) {
	t.Helper()

	row := s.db.QueryRow(auditSelectByID, id)
	ev, err := scanAuditEvent(row.Scan)
	if err != nil {
		t.Fatalf("Failed to read audit row %d: %v", id, err)
	}

	if _, err := s.db.Exec("DELETE FROM audit_events WHERE id = ?",
		id); err != nil {
		t.Fatalf("Failed to remove audit row %d: %v", id, err)
	}
	if _, err := s.db.Exec(`
        INSERT INTO audit_events (
            id, occurred_at, actor_type, actor_id, actor_name, actor_ip,
            action, target_type, target_id, target_name, outcome,
            error, details, prev_hash, hash, hash_version
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.OccurredAt.UTC().Format(auditTimeLayout),
		string(ev.ActorType), ev.ActorID, ev.ActorName,
		nullableText(ev.ActorIP), "tampered", nullableText(ev.TargetType),
		ev.TargetID, nullableText(ev.TargetName), string(ev.Outcome),
		nullableText(ev.Error), nullableText(string(ev.Details)),
		ev.PrevHash, ev.Hash, ev.HashVersion); err != nil {
		t.Fatalf("Failed to corrupt audit row %d: %v", id, err)
	}
}

// TestRechainProducesAVerifyingChain is the upgrade path end to end: a
// database of inherited unkeyed rows will not open, the re-chain
// rewrites every one of them under the key, and the result verifies.
func TestRechainProducesAVerifyingChain(t *testing.T) {
	store, dir := newReopenableStore(t)
	inheritV1Rows(t, store, 5)
	store.Close()

	if _, err := reopenStore(t, dir); !errors.Is(err, ErrAuditUnkeyedRow) {
		t.Fatalf("Expected the open to refuse the unkeyed rows, got %v", err)
	}

	result, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		alwaysConfirmRechain)
	if err != nil {
		t.Fatalf("Failed to re-chain the log: %v", err)
	}
	if !result.Confirmed {
		t.Error("Expected the re-chain to report that it was confirmed")
	}
	if result.Events != 5 {
		t.Errorf("Expected 5 rows re-hashed, got %d", result.Events)
	}

	reopened, err := reopenStore(t, dir)
	if err != nil {
		t.Fatalf("Failed to reopen the re-chained store: %v", err)
	}

	count, firstBad, err := reopened.VerifyAuditChain()
	if err != nil {
		t.Fatalf("Expected the re-chained log to verify, got %v (row %d)",
			err, firstBad)
	}
	// Five inherited rows plus the audit.rechain event the re-chain
	// appends to account for itself.
	if count != 6 {
		t.Errorf("Expected 6 verified rows, got %d", count)
	}

	unkeyed, _, _, err := reopened.countUnkeyedAuditRows()
	if err != nil {
		t.Fatalf("Failed to count the unkeyed rows: %v", err)
	}
	if unkeyed != 0 {
		t.Errorf("Expected no unkeyed rows after the re-chain, got %d",
			unkeyed)
	}
}

// TestRechainRecordsItself checks the log accounts for the one event
// that rewrote every hash in it.
func TestRechainRecordsItself(t *testing.T) {
	store, dir := newReopenableStore(t)
	inheritV1Rows(t, store, 3)
	store.Close()

	if _, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		alwaysConfirmRechain); err != nil {
		t.Fatalf("Failed to re-chain the log: %v", err)
	}

	reopened, err := reopenStore(t, dir)
	if err != nil {
		t.Fatalf("Failed to reopen the re-chained store: %v", err)
	}

	var action string
	var details []byte
	if err := reopened.db.QueryRow(
		"SELECT action, details FROM audit_events ORDER BY id DESC LIMIT 1").
		Scan(&action, &details); err != nil {
		t.Fatalf("Failed to read the newest audit row: %v", err)
	}
	if action != auditActionRechain {
		t.Fatalf("Expected the newest row to be %s, got %s",
			auditActionRechain, action)
	}

	var parsed struct {
		Events        int64 `json:"events"`
		UnkeyedEvents int64 `json:"unkeyed_events"`
		LegacyChainOK bool  `json:"legacy_chain_ok"`
	}
	if err := json.Unmarshal(details, &parsed); err != nil {
		t.Fatalf("Failed to parse the re-chain details %q: %v", details, err)
	}
	if parsed.Events != 3 || parsed.UnkeyedEvents != 3 {
		t.Errorf("Expected 3 events and 3 unkeyed, got %d and %d",
			parsed.Events, parsed.UnkeyedEvents)
	}
	if !parsed.LegacyChainOK {
		t.Error("Expected the inherited chain to have recomputed cleanly")
	}
}

// TestOpenRefusesAnUnkeyedRowAnywhere checks the gate reads every row
// rather than only the oldest: a version 1 row appended above keyed
// rows, which is what someone editing auth.db produces, refuses the
// open just as an inherited prefix does.
func TestOpenRefusesAnUnkeyedRowAnywhere(t *testing.T) {
	store, dir := newReopenableStore(t)
	if err := store.recordAuditInOwnTx(newEvent(systemActor, "user.create",
		"user", nil, "alice", nil)); err != nil {
		t.Fatalf("Failed to record a keyed event: %v", err)
	}
	insertAuditRowAsV1(t, store, newEvent(systemActor, "user.delete",
		"user", nil, "alice", nil))
	store.Close()

	_, err := reopenStore(t, dir)
	if !errors.Is(err, ErrAuditUnkeyedRow) {
		t.Fatalf("Expected the open to refuse the unkeyed row, got %v", err)
	}
	for _, want := range []string{"restore auth.db from a known-good copy",
		"'ai-dba-server -rechain-audit-log' is refused here",
		"already keyed", "Deleting auth.db is NOT the remedy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expected the refusal to mention %q, got %v", want, err)
		}
	}
}

// TestAuditHashRejectsVersion1 checks the rendering itself is gone:
// nothing in this build will compute an unkeyed digest for a row it is
// asked to hash or verify.
func TestAuditHashRejectsVersion1(t *testing.T) {
	ev := sampleAuditEvent()
	ev.ID = 7
	ev.HashVersion = 1

	if _, err := auditHash(ev, AuditKeyForTesting()); !errors.Is(err,
		ErrAuditUnkeyedRow) {
		t.Fatalf("Expected auditHash to refuse version 1, got %v", err)
	}
}

// TestVerifyRejectsAVersion1RowAfterARechain is the same refusal seen
// from the verifier: having re-chained, a row put back into the unkeyed
// rendering is tampering, not history. It is the first row, because a
// version 1 row anywhere above a version 2 row is caught earlier still,
// by the downgrade check.
func TestVerifyRejectsAVersion1RowAfterARechain(t *testing.T) {
	store, dir := newReopenableStore(t)
	inheritV1Rows(t, store, 3)
	store.Close()

	if _, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		alwaysConfirmRechain); err != nil {
		t.Fatalf("Failed to re-chain the log: %v", err)
	}

	tampered := openUnkeyedStore(t, dir)
	rewriteRowAsV1(t, tampered, 1)

	_, firstBad, err := tampered.VerifyAuditChain()
	if !errors.Is(err, ErrAuditUnkeyedRow) {
		t.Fatalf("Expected the relabelled row to be rejected as unkeyed, "+
			"got %v", err)
	}
	if !errors.Is(err, ErrAuditChainBroken) {
		t.Errorf("Expected the relabelled row to read as tampering, got %v",
			err)
	}
	if firstBad != 1 {
		t.Errorf("Expected row 1 to be reported, got %d", firstBad)
	}
}

// TestRechainRestoresTheTrigger checks the append-only trigger the
// re-chain drops is not merely re-created in sqlite_master but back in
// force.
func TestRechainRestoresTheTrigger(t *testing.T) {
	store, dir := newReopenableStore(t)
	inheritV1Rows(t, store, 3)
	store.Close()

	if _, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		alwaysConfirmRechain); err != nil {
		t.Fatalf("Failed to re-chain the log: %v", err)
	}

	// Read through a raw connection: reopening the store would
	// re-create the trigger itself and prove nothing.
	db := openRawAuditDB(t, dir)
	if !rawTriggerPresent(t, db) {
		t.Fatal("Expected the append-only trigger to be back")
	}
	assertRawUpdatesRefused(t, db)
}

// TestRechainRollsBackCleanly checks a failure part-way through leaves
// neither a half-rewritten log nor a table with no trigger on it. The
// failure is injected with a second trigger that aborts the update of
// one row, and the page size is lowered so that the walk has committed
// several rows and crossed a page boundary before it hits that row.
func TestRechainRollsBackCleanly(t *testing.T) {
	original := auditPageSize
	auditPageSize = 2
	defer func() { auditPageSize = original }()

	store, dir := newReopenableStore(t)
	inheritV1Rows(t, store, 6)
	before := auditRowStates(t, store)
	store.Close()

	blocker := openUnkeyedStore(t, dir)
	if _, err := blocker.db.Exec(`
        CREATE TRIGGER audit_block_row_5 BEFORE UPDATE ON audit_events
        WHEN NEW.id = 5
        BEGIN
            SELECT RAISE(ABORT, 'injected failure');
        END;`); err != nil {
		t.Fatalf("Failed to install the fault-injecting trigger: %v", err)
	}
	blocker.Close()

	_, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		alwaysConfirmRechain)
	if err == nil {
		t.Fatal("Expected the re-chain to fail on the blocked row")
	}
	if !strings.Contains(err.Error(), "injected failure") {
		t.Errorf("Expected the injected failure to be reported, got %v", err)
	}

	after := openRawAuditDB(t, dir)
	if !rawTriggerPresent(t, after) {
		t.Fatal("Expected the append-only trigger to survive the rollback")
	}
	assertRawUpdatesRefused(t, after)

	if _, err := after.Exec("DROP TRIGGER audit_block_row_5"); err != nil {
		t.Fatalf("Failed to remove the fault-injecting trigger: %v", err)
	}

	rolledBack := rawAuditRowStates(t, after)
	if len(rolledBack) != len(before) {
		t.Fatalf("Expected %d rows after the rollback, got %d",
			len(before), len(rolledBack))
	}
	for i, row := range rolledBack {
		if row != before[i] {
			t.Errorf("Row %d was left rewritten: %+v, want %+v",
				row.ID, row, before[i])
		}
	}

}

// TestRechainStopsWhenTheOperatorDeclines checks the confirmation is a
// gate and not a notification.
func TestRechainStopsWhenTheOperatorDeclines(t *testing.T) {
	store, dir := newReopenableStore(t)
	inheritV1Rows(t, store, 3)
	before := auditRowStates(t, store)
	store.Close()

	result, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(AuditRechainPlan) (bool, error) { return false, nil })
	if err != nil {
		t.Fatalf("Declining is not an error: %v", err)
	}
	if result.Confirmed {
		t.Error("Expected the result to report that nothing was confirmed")
	}
	if result.Events != 0 {
		t.Errorf("Expected no rows re-hashed, got %d", result.Events)
	}

	after := openUnkeyedStore(t, dir)
	unchanged := auditRowStates(t, after)
	if len(unchanged) != len(before) {
		t.Fatalf("Expected %d rows to remain, got %d", len(before),
			len(unchanged))
	}
	for i, row := range unchanged {
		if row != before[i] {
			t.Errorf("Row %d was rewritten without confirmation: %+v, "+
				"want %+v", row.ID, row, before[i])
		}
	}
}

// TestRechainReportsAConfirmationError checks a callback that cannot
// ask, rather than one that was told no, stops the rewrite too.
func TestRechainReportsAConfirmationError(t *testing.T) {
	store, dir := newReopenableStore(t)
	inheritV1Rows(t, store, 2)
	store.Close()

	wantErr := errors.New("no terminal")
	_, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(AuditRechainPlan) (bool, error) { return true, wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("Expected the confirmation error to be returned, got %v", err)
	}

	after := openUnkeyedStore(t, dir)
	unkeyed, _, _, err := after.countUnkeyedAuditRows()
	if err != nil {
		t.Fatalf("Failed to count the unkeyed rows: %v", err)
	}
	if unkeyed != 2 {
		t.Errorf("Expected both rows to be left unkeyed, got %d", unkeyed)
	}
}

// TestRechainRequiresAConfirmCallback checks the API cannot be called
// in a way that skips the gate.
func TestRechainRequiresAConfirmCallback(t *testing.T) {
	_, dir := newReopenableStore(t)

	if _, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		nil); err == nil {
		t.Fatal("Expected a nil confirmation callback to be refused")
	}
}

// TestRechainReportsAnUnusableDataDir covers the open failing before
// anything else can.
func TestRechainReportsAnUnusableDataDir(t *testing.T) {
	// A path whose parent is a file, not a directory, so MkdirAll
	// cannot create it.
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/blocker", nil, 0600); err != nil {
		t.Fatalf("Failed to write the blocking file: %v", err)
	}
	blocked := dir + "/blocker/data"
	if _, err := RechainAuditLog(blocked, AuditKeyForTesting(), systemActor,
		alwaysConfirmRechain); err == nil {
		t.Fatal("Expected an unusable data directory to be reported")
	}
}

// TestRechainPlanReportsACleanLegacyChain checks the operator is shown
// the figures the decision rests on.
func TestRechainPlanReportsACleanLegacyChain(t *testing.T) {
	store, dir := newReopenableStore(t)
	oldest := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		ev := newEvent(systemActor, "user.create", "user", nil, "old", nil)
		ev.OccurredAt = oldest.Add(time.Duration(i-1) * time.Hour)
		insertAuditRowAsV1(t, store, ev)
	}
	store.Close()

	var seen AuditRechainPlan
	if _, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(plan AuditRechainPlan) (bool, error) {
			seen = plan
			return false, nil
		}); err != nil {
		t.Fatalf("Failed to plan the re-chain: %v", err)
	}

	if seen.Events != 3 || seen.UnkeyedEvents != 3 {
		t.Errorf("Expected 3 events and 3 unkeyed, got %d and %d",
			seen.Events, seen.UnkeyedEvents)
	}
	if !seen.Oldest.Equal(oldest) {
		t.Errorf("Expected the oldest row at %s, got %s", oldest, seen.Oldest)
	}
	if !seen.Newest.Equal(oldest.Add(2 * time.Hour)) {
		t.Errorf("Expected the newest row at %s, got %s",
			oldest.Add(2*time.Hour), seen.Newest)
	}
	if !seen.LegacyChainOK || seen.LegacyFirstBad != 0 ||
		seen.LegacyChainErr != nil {
		t.Errorf("Expected a clean inherited chain, got ok=%v firstBad=%d "+
			"err=%v", seen.LegacyChainOK, seen.LegacyFirstBad,
			seen.LegacyChainErr)
	}
}

// TestRechainPlanReportsABrokenLegacyChain checks the pre-flight
// recomputation catches a row that was edited without its hash being
// fixed up, which is all it can catch: the unkeyed digest is computable
// by anyone, so a careful attacker leaves a chain that recomputes.
func TestRechainPlanReportsABrokenLegacyChain(t *testing.T) {
	store, dir := newReopenableStore(t)
	inheritV1Rows(t, store, 4)
	corruptAuditRow(t, store, 2)
	store.Close()

	var seen AuditRechainPlan
	if _, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(plan AuditRechainPlan) (bool, error) {
			seen = plan
			return false, nil
		}); err != nil {
		t.Fatalf("Failed to plan the re-chain: %v", err)
	}

	if seen.LegacyChainOK {
		t.Fatal("Expected the edited row to break the inherited chain")
	}
	if seen.LegacyFirstBad != 2 {
		t.Errorf("Expected row 2 to be reported, got %d", seen.LegacyFirstBad)
	}
	if !errors.Is(seen.LegacyChainErr, ErrAuditChainBroken) {
		t.Errorf("Expected a broken-chain error, got %v", seen.LegacyChainErr)
	}
}

// TestRechainPlanReportsABrokenLegacyLink checks the pre-flight catches
// a row that recomputes on its own but does not link to the row before
// it, which is what deleting a row from the middle of the log leaves.
func TestRechainPlanReportsABrokenLegacyLink(t *testing.T) {
	store, dir := newReopenableStore(t)
	if err := SeedUnkeyedAuditLogForTesting(store, 4,
		time.Now().UTC().Add(-time.Hour), 2); err != nil {
		t.Fatalf("Failed to write the unkeyed rows: %v", err)
	}
	store.Close()

	var seen AuditRechainPlan
	if _, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(plan AuditRechainPlan) (bool, error) {
			seen = plan
			return false, nil
		}); err != nil {
		t.Fatalf("Failed to plan the re-chain: %v", err)
	}

	if seen.LegacyChainOK || seen.LegacyFirstBad != 3 {
		t.Errorf("Expected row 3 to be reported as unlinked, got ok=%v "+
			"firstBad=%d", seen.LegacyChainOK, seen.LegacyFirstBad)
	}
}

// TestRechainPlanReportsAnUnknownHashVersion checks the pre-flight
// refuses to guess at a rendering no release ever wrote.
func TestRechainPlanReportsAnUnknownHashVersion(t *testing.T) {
	store, dir := newReopenableStore(t)
	if _, err := store.db.Exec(`
        INSERT INTO audit_events (
            occurred_at, actor_type, actor_name, action, outcome,
            prev_hash, hash, hash_version
        ) VALUES (?, 'system', 'system', 'user.create', 'success', '', ?, 9)`,
		time.Now().UTC().Format(auditTimeLayout),
		"future-hash"); err != nil {
		t.Fatalf("Failed to insert a future-version row: %v", err)
	}
	store.Close()

	var seen AuditRechainPlan
	if _, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(plan AuditRechainPlan) (bool, error) {
			seen = plan
			return false, nil
		}); err != nil {
		t.Fatalf("Failed to plan the re-chain: %v", err)
	}

	if !errors.Is(seen.LegacyChainErr, errUnknownAuditHashVersion) {
		t.Errorf("Expected an unknown-version error, got %v",
			seen.LegacyChainErr)
	}
}

// TestFreshInstallNeedsNoRechain checks the upgrade step is exactly
// that, and a database this build created has nothing for it to do.
func TestFreshInstallNeedsNoRechain(t *testing.T) {
	store, dir := newReopenableStore(t)
	if err := store.recordAuditInOwnTx(newEvent(systemActor, "user.create",
		"user", nil, "alice", nil)); err != nil {
		t.Fatalf("Failed to record a keyed event: %v", err)
	}
	store.Close()

	reopened, err := reopenStore(t, dir)
	if err != nil {
		t.Fatalf("Expected a fresh install to open, got %v", err)
	}
	if _, _, err := reopened.VerifyAuditChain(); err != nil {
		t.Fatalf("Expected a fresh install to verify, got %v", err)
	}
	reopened.Close()

	var seen AuditRechainPlan
	if _, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(plan AuditRechainPlan) (bool, error) {
			seen = plan
			return false, nil
		}); err != nil {
		t.Fatalf("Failed to plan the re-chain: %v", err)
	}
	if seen.UnkeyedEvents != 0 {
		t.Errorf("Expected a fresh install to hold no unkeyed rows, got %d",
			seen.UnkeyedEvents)
	}
}

// TestRechainOfAnEmptyLog checks the degenerate case, which a database
// whose whole log has been purged reaches.
func TestRechainOfAnEmptyLog(t *testing.T) {
	store, dir := newReopenableStore(t)
	if _, err := store.db.Exec("DELETE FROM audit_events"); err != nil {
		t.Fatalf("Failed to empty the audit log: %v", err)
	}
	store.Close()

	var seen AuditRechainPlan
	result, err := RechainAuditLog(dir, AuditKeyForTesting(), systemActor,
		func(plan AuditRechainPlan) (bool, error) {
			seen = plan
			return true, nil
		})
	if err != nil {
		t.Fatalf("Failed to re-chain an empty log: %v", err)
	}
	if result.Events != 0 || seen.Events != 0 {
		t.Errorf("Expected an empty log, got %d planned and %d re-hashed",
			seen.Events, result.Events)
	}
	if !seen.Oldest.IsZero() || !seen.Newest.IsZero() {
		t.Errorf("Expected no timestamps for an empty log, got %s and %s",
			seen.Oldest, seen.Newest)
	}

	reopened, err := reopenStore(t, dir)
	if err != nil {
		t.Fatalf("Failed to reopen the store: %v", err)
	}
	if _, _, err := reopened.VerifyAuditChain(); err != nil {
		t.Fatalf("Expected the re-chain event alone to verify, got %v", err)
	}
}

// TestPurgeDoesNotReanchorAForgedPrefix is the regression test for the
// attack the third security review demonstrated against the boundary
// machinery this design replaced.
//
// That design kept a signed watermark naming the newest inherited
// unkeyed row, and lowered it onto the highest-id surviving unkeyed row
// when retention deleted the row it named. Nothing tied occurred_at to
// id order, so an attacker who could write auth.db forged a prefix,
// backdated the row they wanted attested, and waited for the retention
// purge, which runs unattended every few minutes: the server then
// re-signed the boundary onto the forged row under the real key, and
// verification passed over a fabricated history.
//
// The test performs exactly that sequence. What it asserts is that
// there is no longer any outcome in which it works: the purge deletes
// by id and so declines to remove the backdated rows at all, it writes
// no hash over a row it did not create, unkeyed rows are not
// admissible, and the database will not even open.
func TestPurgeDoesNotReanchorAForgedPrefix(t *testing.T) {
	store, dir := newReopenableStore(t)

	// The forged prefix, with its timestamps running backwards so that
	// the highest-id row is the oldest and the purge takes it first.
	base := time.Now().UTC().Add(-24 * time.Hour)
	for i := 1; i <= 6; i++ {
		ev := newEvent(systemActor, "user.create", "user", nil, "forged", nil)
		ev.OccurredAt = base.Add(-time.Duration(i) * time.Hour)
		insertAuditRowAsV1(t, store, ev)
	}
	store.Close()

	// The server would not have opened this database at all; the
	// privileged open stands in for a build that did, so that the purge
	// gets its chance to re-sign something.
	purger := openUnkeyedStore(t, dir)
	removed, err := purger.PurgeAuditEvents(base.Add(-4 * time.Hour))
	if err != nil {
		t.Fatalf("Failed to purge: %v", err)
	}
	// The backdated rows carry the highest ids, so deleting a prefix of
	// ids reaches none of them.
	if removed != 0 {
		t.Fatalf("Expected the purge to remove nothing, got %d", removed)
	}

	if _, _, err := purger.VerifyAuditChain(); err == nil {
		t.Fatal("Verification passed over a forged unkeyed history")
	} else if !errors.Is(err, ErrAuditUnkeyedRow) {
		t.Errorf("Expected the surviving forged rows to be rejected as "+
			"unkeyed, got %v", err)
	}
	purger.Close()

	if _, err := reopenStore(t, dir); !errors.Is(err, ErrAuditUnkeyedRow) {
		t.Fatalf("Expected the purged database still to refuse to open, "+
			"got %v", err)
	}
}

// TestVerifyAcceptsAWhollyPurgedPrefix checks retention does not break
// verification: the oldest surviving row points at a hash that is no
// longer there, and that is expected rather than evidence of anything.
func TestVerifyAcceptsAWhollyPurgedPrefix(t *testing.T) {
	store, _ := newReopenableStore(t)

	old := time.Now().UTC().Add(-48 * time.Hour)
	for i := 1; i <= 3; i++ {
		ev := newEvent(systemActor, "user.create", "user", nil, "old", nil)
		ev.OccurredAt = old.Add(time.Duration(i) * time.Minute)
		if err := store.recordAuditInOwnTx(ev); err != nil {
			t.Fatalf("Failed to record an old event: %v", err)
		}
	}
	if err := store.recordAuditInOwnTx(newEvent(systemActor, "user.create",
		"user", nil, "recent", nil)); err != nil {
		t.Fatalf("Failed to record a recent event: %v", err)
	}

	removed, err := store.PurgeAuditEvents(time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Failed to purge: %v", err)
	}
	if removed != 3 {
		t.Fatalf("Expected 3 rows purged, got %d", removed)
	}

	if _, firstBad, err := store.VerifyAuditChain(); err != nil {
		t.Fatalf("Expected a purged log to verify, got %v (row %d)",
			err, firstBad)
	}
}

// The statements that remove the two schema objects the chain rests
// on, folded here so the names stay compile-time constants rather than
// being assembled at the point they are executed.
const (
	dropAuditNoUpdateTrigger = "DROP TRIGGER " + auditNoUpdateTrigger
	dropAuditChainIndex      = "DROP INDEX " + auditChainIndexName
)

// TestVerifyReportsSchemaDamageAsTampering checks the two schema
// objects the chain's guarantees rest on are themselves checked: a
// database with either removed accepts a forked chain or a rewritten
// row, so their absence is reported as tampering rather than as a
// configuration problem.
func TestVerifyReportsSchemaDamageAsTampering(t *testing.T) {
	tests := []struct {
		name string
		drop string
	}{
		{"trigger dropped", dropAuditNoUpdateTrigger},
		{"index dropped", dropAuditChainIndex},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, _ := newReopenableStore(t)
			if err := store.recordAuditInOwnTx(newEvent(systemActor,
				"user.create", "user", nil, "alice", nil)); err != nil {
				t.Fatalf("Failed to record an event: %v", err)
			}
			if _, err := store.db.Exec(tt.drop); err != nil {
				t.Fatalf("Failed to damage the schema: %v", err)
			}

			_, _, err := store.VerifyAuditChain()
			if !errors.Is(err, ErrAuditChainBroken) {
				t.Fatalf("Expected schema damage to read as tampering, "+
					"got %v", err)
			}
		})
	}
}

// TestVerifyReportsAnUnknownHashVersionAsTampering checks a row
// relabelled out of reach of the verifier is reported, rather than
// skipped as something a later release will understand.
func TestVerifyReportsAnUnknownHashVersionAsTampering(t *testing.T) {
	store, _ := newReopenableStore(t)
	if _, err := store.db.Exec(`
        INSERT INTO audit_events (
            occurred_at, actor_type, actor_name, action, outcome,
            prev_hash, hash, hash_version
        ) VALUES (?, 'system', 'system', 'user.create', 'success', '', ?, 9)`,
		time.Now().UTC().Format(auditTimeLayout),
		"future-hash"); err != nil {
		t.Fatalf("Failed to insert a future-version row: %v", err)
	}

	_, firstBad, err := store.VerifyAuditChain()
	if !errors.Is(err, ErrAuditChainBroken) {
		t.Fatalf("Expected an unknown hash version to read as tampering, "+
			"got %v", err)
	}
	if firstBad != 1 {
		t.Errorf("Expected row 1 to be reported, got %d", firstBad)
	}
}

// TestParseAuditTimeRejectsRubbish covers the one branch a database
// edited by hand can reach, since every timestamp this build writes is
// in the stored layout.
func TestParseAuditTimeRejectsRubbish(t *testing.T) {
	if _, err := parseAuditTime(sql.NullString{
		String: "not a timestamp", Valid: true}); err == nil {
		t.Fatal("Expected an invalid occurred_at to be reported")
	}

	parsed, err := parseAuditTime(sql.NullString{})
	if err != nil {
		t.Fatalf("Expected a NULL occurred_at to be accepted: %v", err)
	}
	if !parsed.IsZero() {
		t.Errorf("Expected the zero time for a NULL, got %s", parsed)
	}
}

// TestAuditRechainReportsDatabaseFailures covers the error paths the
// re-chain takes when the table it walks is not there, which is the
// only way to reach them without a fault-injecting driver. Dropping
// audit_events stands in for a database damaged by whatever means.
func TestAuditRechainReportsDatabaseFailures(t *testing.T) {
	store, dir := newReopenableStore(t)
	inheritV1Rows(t, store, 2)
	store.Close()

	damaged := openUnkeyedStore(t, dir)
	if _, err := damaged.db.Exec("DROP TABLE audit_events"); err != nil {
		t.Fatalf("Failed to drop the audit table: %v", err)
	}

	if _, _, _, err := damaged.countUnkeyedAuditRows(); err == nil {
		t.Error("Expected counting unkeyed rows to fail")
	}
	if err := damaged.ensureNoUnkeyedAuditRows(); err == nil {
		t.Error("Expected the unkeyed-row gate to report the failure")
	}
	if _, err := damaged.auditRechainPlan(); err == nil {
		t.Error("Expected planning the re-chain to fail")
	}
	if err := forEachAuditEvent(damaged.db,
		func(AuditEvent) error { return nil }); err == nil {
		t.Error("Expected the paged walk to fail")
	}
	if _, err := damaged.verifyLegacyAuditChain(); err == nil {
		t.Error("Expected the pre-flight verification to fail")
	}
	if _, err := damaged.rechainAuditLogTx(systemActor,
		AuditRechainPlan{}); err == nil {
		t.Error("Expected the re-chain transaction to fail")
	}
	if _, err := damaged.rechainAuditLog(systemActor,
		alwaysConfirmRechain); err == nil {
		t.Error("Expected the re-chain to fail")
	}

	// A closed database is the one way to reach the failure to begin
	// the transaction at all.
	if err := damaged.db.Close(); err != nil {
		t.Fatalf("Failed to close the database: %v", err)
	}
	if _, err := damaged.rechainAuditLogTx(systemActor,
		AuditRechainPlan{}); err == nil ||
		!strings.Contains(err.Error(), "failed to begin") {
		t.Errorf("Expected the transaction not to begin, got %v", err)
	}
}

// TestAuditRechainPlanRejectsAnInvalidTimestamp covers the parse of
// occurred_at, which only a value written by something other than this
// build can fail.
func TestAuditRechainPlanRejectsAnInvalidTimestamp(t *testing.T) {
	store, _ := newReopenableStore(t)
	if _, err := store.db.Exec(`
        INSERT INTO audit_events (
            occurred_at, actor_type, actor_name, action, outcome,
            prev_hash, hash, hash_version
        ) VALUES ('yesterday', 'system', 'system', 'user.create',
                  'success', '', 'some-hash', 2)`); err != nil {
		t.Fatalf("Failed to insert the row: %v", err)
	}

	if _, err := store.auditRechainPlan(); err == nil {
		t.Fatal("Expected an unparseable occurred_at to be reported")
	} else if !strings.Contains(err.Error(), "invalid occurred_at") {
		t.Errorf("Expected the value to be named, got %v", err)
	}
}

// TestAuditEventPageReportsAScanFailure covers the scan error the paged
// walk can meet, using a row whose hash_version holds text: SQLite
// stores what it is given regardless of the declared column type, so
// this is a shape a hand-edited database really can have.
func TestAuditEventPageReportsAScanFailure(t *testing.T) {
	store, _ := newReopenableStore(t)
	if _, err := store.db.Exec(`
        INSERT INTO audit_events (
            occurred_at, actor_type, actor_name, action, outcome,
            prev_hash, hash, hash_version
        ) VALUES (?, 'system', 'system', 'user.create', 'success', '',
                  'some-hash', 'not a number')`,
		time.Now().UTC().Format(auditTimeLayout)); err != nil {
		t.Fatalf("Failed to insert the row: %v", err)
	}

	if _, err := auditEventPage(store.db, nil); err == nil {
		t.Fatal("Expected the scan to fail")
	} else if !strings.Contains(err.Error(), "failed to scan audit event") {
		t.Errorf("Expected a scan failure, got %v", err)
	}
}

// TestForEachAuditEventStopsOnTheCallbackError checks the walk reports
// a callback failure rather than carrying on to the next page.
func TestForEachAuditEventStopsOnTheCallbackError(t *testing.T) {
	original := auditPageSize
	auditPageSize = 2
	defer func() { auditPageSize = original }()

	store, _ := newReopenableStore(t)
	inheritV1Rows(t, store, 5)

	wantErr := errors.New("stop here")
	seen := 0
	err := forEachAuditEvent(store.db, func(AuditEvent) error {
		seen++
		if seen == 3 {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Expected the callback error to be returned, got %v", err)
	}
	if seen != 3 {
		t.Errorf("Expected the walk to stop at row 3, saw %d rows", seen)
	}
}

// TestRechainReportsAFailureToRecordItself covers the re-chain's own
// event failing to write. The log is emptied first so that the walk
// completes and the failure lands on the recording step; a store with
// no key is what makes recordAudit refuse.
func TestRechainReportsAFailureToRecordItself(t *testing.T) {
	store, _ := newReopenableStore(t)
	if _, err := store.db.Exec("DELETE FROM audit_events"); err != nil {
		t.Fatalf("Failed to empty the audit log: %v", err)
	}
	store.auditKey = nil

	_, err := store.rechainAuditLogTx(systemActor, AuditRechainPlan{})
	if err == nil {
		t.Fatal("Expected the re-chain event to fail to record")
	}
	if !strings.Contains(err.Error(), "failed to record the re-chain event") {
		t.Errorf("Expected the recording step to be named, got %v", err)
	}
}
