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
	"bytes"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

// auditRowHash reads the hash of audit event id.
func auditRowHash(t *testing.T, s *AuthStore, id int64) string {
	t.Helper()

	var hash string
	if err := s.db.QueryRow("SELECT hash FROM audit_events WHERE id = ?",
		id).Scan(&hash); err != nil {
		t.Fatalf("Failed to read the hash of event %d: %v", id, err)
	}

	return hash
}

// threeSecretLog writes four events under each of three secrets in turn,
// the last four under rotatedAuditKey, and leaves the store closed
// before any event is written under thirdAuditKey.
func threeSecretLog(t *testing.T) string {
	t.Helper()

	store, dir := writeUnderKeys(t, 4, rotatedAuditKey)
	store.Close()

	return dir
}

// expectTailUnproven checks whether the plan's evidence under the
// previous secret includes a failed tail proof.
func expectTailUnproven(t *testing.T, plan AuditRechainPlan, want bool) {
	t.Helper()

	if plan.HistoryProven {
		t.Fatalf("Expected a history spanning two secrets to be unproven, "+
			"got %+v", plan)
	}
	if got := errors.Is(plan.HistoryProofErr, errAuditTailUnproven); got !=
		want {
		t.Errorf("Expected the tail proof to fail: %v, got %v", want,
			plan.HistoryProofErr)
	}
}

// TestPlantedCurrentKeyAnchorIsRefusedByThePreviousSecret checks the
// attack the security review found: between a second change of secret
// and the first event written under it, the newest events written under
// the previous secret are cut, and the current-key anchor pointed at the
// new newest row under a MAC no secret produced. Verification cannot
// tell, before or after the next event promotes the planted value, but
// the previous secret refuses it in both states, whilst it accepts the
// anchors of the same log left alone.
func TestPlantedCurrentKeyAnchorIsRefusedByThePreviousSecret(t *testing.T) {
	genuine := threeSecretLog(t)
	expectTailUnproven(t, planWithPreviousKey(t, genuine, thirdAuditKey,
		rotatedAuditKey), false)

	store := openWithKey(t, genuine, thirdAuditKey)
	recordN(t, store, 1)
	store.Close()
	expectTailUnproven(t, planWithPreviousKey(t, genuine, thirdAuditKey,
		rotatedAuditKey), false)

	planted := threeSecretLog(t)
	store = openWithKey(t, planted, thirdAuditKey)
	deleteAuditRows(t, store, 7, 8)
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 6
        WHERE name = 'audit_events'`)
	tamperDB(t, store.db, `UPDATE audit_tail SET event_id = 6,
        event_hash = ?, mac = 'x' WHERE id = 2`, auditRowHash(t, store, 6))
	expectKeyMismatch(t, store)
	store.Close()
	expectTailUnproven(t, planWithPreviousKey(t, planted, thirdAuditKey,
		rotatedAuditKey), true)

	store = openWithKey(t, planted, thirdAuditKey)
	recordN(t, store, 1)
	if st := readTail(t, store); st.anchorID != 6 || !st.bound.valid {
		t.Fatalf("Expected the planted anchor to be promoted, got %+v", st)
	}
	expectKeyMismatch(t, store)
	store.Close()
	expectTailUnproven(t, planWithPreviousKey(t, planted, thirdAuditKey,
		rotatedAuditKey), true)
}

// forgePromotion does by hand what a promotion does: it copies the
// current-key anchor into the primary slot, with the binding columns
// naming the primary anchor it was signed beside, and deletes the
// current-key anchor.
func forgePromotion(t *testing.T, s *AuthStore) {
	t.Helper()

	st := readTail(t, s)
	if !st.hasCurrent {
		t.Fatalf("Expected a current-key anchor to copy, got %+v", st)
	}
	tamperDB(t, s.db, `UPDATE audit_tail SET event_id = ?, event_hash = ?,
        mac = ?, bound_event_id = ?, bound_event_hash = ?, bound_mac = ?
        WHERE id = 1`, st.currentID, st.currentHash, st.currentMAC,
		st.anchorID, st.anchorHash, st.anchorMAC)
	tamperDB(t, s.db, "DELETE FROM audit_tail WHERE id = 2")
}

// TestForgedPromotionInANeverRotatedLogIsCaught is the first case from
// the third review of #565. In a log written under one secret, the
// newest five events are cut and a decoy put in their place, so that
// the server starts a current-key anchor beside a primary anchor naming
// the decoy. Once the decoy is deleted, that current-key anchor is
// copied into the primary slot with the binding it was signed over. A
// promoted primary anchor never verifies under the key in use, so the
// log reads as tampering, and goes on doing so as events are written.
func TestForgedPromotionInANeverRotatedLogIsCaught(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	recordN(t, store, 10)
	keptHash := auditRowHash(t, store, 5)
	deleteAuditRows(t, store, 6, 7, 8, 9, 10)
	tamperDB(t, store.db, `INSERT INTO audit_events (id, occurred_at,
            actor_type, actor_name, action, outcome, prev_hash, hash,
            hash_version)
        VALUES (6, ?, 'system', 'system', 'user.create', 'success',
            'unused', ?, 3)`,
		time.Now().UTC().Format(auditTimeLayout), keptHash)
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 6
        WHERE name = 'audit_events'`)
	tamperDB(t, store.db, `UPDATE audit_tail SET event_id = 6,
        event_hash = ?, mac = 'x' WHERE id = 1`, keptHash)

	recordN(t, store, 1)
	deleteAuditRows(t, store, 6)
	forgePromotion(t, store)
	if st := readTail(t, store); !st.namesNewest() || !st.bound.valid ||
		!primaryTailVerifiesUnder(store.auditKey, st) {
		t.Fatalf("Expected a forged promotion the key in use signed, got "+
			"%+v", st)
	}
	expectTailError(t, store, "tail anchor altered")
	expectTampering(t, store)

	recordN(t, store, 2)
	expectTampering(t, store)
}

// TestForgedPromotionAfterARotationIsCaught is the second case from the
// third review of #565. Ten events under one secret and four under the
// next; the newest nine are cut, the primary anchor pointed at the new
// newest row under a MAC no secret produced and the current-key anchor
// deleted, so that the server's next event starts a current-key anchor
// beside it, which is then copied into the primary slot. The log must
// read as tampering rather than as a key mismatch, and the unattended
// re-anchor must refuse it.
func TestForgedPromotionAfterARotationIsCaught(t *testing.T) {
	store, dir := writeUnderKeys(t, 10)
	store.Close()
	store = openWithKey(t, dir, rotatedAuditKey)
	recordN(t, store, 4)

	keptHash := auditRowHash(t, store, 5)
	deleteAuditRows(t, store, 6, 7, 8, 9, 10, 11, 12, 13, 14)
	tamperDB(t, store.db, `UPDATE sqlite_sequence SET seq = 5
        WHERE name = 'audit_events'`)
	tamperDB(t, store.db, `UPDATE audit_tail SET event_id = 5,
        event_hash = ?, mac = 'x' WHERE id = 1`, keptHash)
	tamperDB(t, store.db, "DELETE FROM audit_tail WHERE id = 2")

	recordN(t, store, 1)
	forgePromotion(t, store)
	expectTampering(t, store)
	store.Close()

	plan := planWithPreviousKey(t, dir, rotatedAuditKey,
		AuditKeyForTesting())
	if unattendedReanchorAccepts(plan) {
		t.Errorf("Expected the unattended re-anchor to refuse, got %+v",
			plan)
	}
	if !errors.Is(plan.HistoryProofErr, errAuditTailUnproven) {
		t.Errorf("Expected the tail proof to fail, got %v",
			plan.HistoryProofErr)
	}
}

// TestAuditTailViewIsRefused checks that a view put in place of the
// audit_tail table is reported as tampering when the store is opened.
func TestAuditTailViewIsRefused(t *testing.T) {
	store, dir := newReopenableStore(t)
	recordN(t, store, 2)
	tamperDB(t, store.db, "DROP TABLE audit_tail")
	tamperDB(t, store.db, `CREATE VIEW audit_tail AS SELECT 1 AS id,
        2 AS event_id, 'h' AS event_hash, 'x' AS mac`)
	store.Close()

	if _, err := reopenStore(t, dir); !errors.Is(err, ErrAuditChainBroken) ||
		!strings.Contains(err.Error(), "audit_tail is not a table") {
		t.Errorf("Expected the open to refuse the view, got %v", err)
	}
}

// TestPromotionKeepsTheBinding checks that a promoted primary anchor
// keeps the primary anchor its HMAC was computed beside, verifies under
// the secret that wrote it only together with that, and loses it again
// when the tail is reset.
func TestPromotionKeepsTheBinding(t *testing.T) {
	dir := threeSecretLog(t)
	store := openWithKey(t, dir, thirdAuditKey)
	defer store.Close()
	before := readTail(t, store)
	if before.bound.valid {
		t.Fatalf("Expected no binding before a promotion, got %+v", before)
	}

	recordN(t, store, 1)
	after := readTail(t, store)
	want := auditTailBinding{valid: true, id: before.anchorID,
		hash: before.anchorHash, mac: before.anchorMAC}
	if after.bound != want || after.anchorMAC != before.currentMAC {
		t.Fatalf("Expected the promoted anchor bound to %+v, got %+v", want,
			after)
	}
	if !primaryTailVerifiesUnder(rotatedAuditKey, after) {
		t.Error("Expected the promoted anchor to verify under the secret " +
			"that wrote it")
	}
	if primaryTailVerifiesUnder(thirdAuditKey, after) {
		t.Error("Expected the promoted anchor to fail under another secret")
	}
	altered := after
	altered.bound.mac = "x"
	if primaryTailVerifiesUnder(rotatedAuditKey, altered) {
		t.Error("Expected an altered binding to fail")
	}

	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin: %v", err)
	}
	defer tx.Rollback()
	ev := AuditEvent{ID: after.newestID, Hash: after.newestHash}
	if err := store.resetAuditTail(tx, &ev); err != nil {
		t.Fatalf("Failed to reset the tail: %v", err)
	}
	if st, err := readAuditTailState(tx); err != nil || st.bound.valid ||
		st.hasCurrent {
		t.Errorf("Expected the reset to clear the binding, got %+v, %v", st,
			err)
	}
}

// TestCurrentKeyAnchorChangesAreLogged checks that starting and
// promoting the current-key anchor each write a warning to the server's
// log, and that moving it on does not.
func TestCurrentKeyAnchorChangesAreLogged(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	store, dir := writeUnderKeys(t, 3, rotatedAuditKey)
	store.Close()
	store = openWithKey(t, dir, thirdAuditKey)
	defer store.Close()
	recordN(t, store, 2)

	out := buf.String()
	for _, want := range []string{"starting a second tail anchor at event 4",
		"moving the second tail anchor into the first and starting it " +
			"again at event 7"} {
		if strings.Count(out, want) != 1 {
			t.Errorf("Expected one warning containing %q, got %q", want, out)
		}
	}
	if n := strings.Count(out, "[WARN] Audit log"); n != 2 {
		t.Errorf("Expected two warnings, got %d: %q", n, out)
	}
}

// expectTailSchemaRefused checks that verification and the next open
// both refuse the store in dir as tampered with, for the reason want.
func expectTailSchemaRefused(t *testing.T, store *AuthStore, dir,
	want string) {
	t.Helper()

	if _, err := store.VerifyAuditLog(); !errors.Is(err,
		ErrAuditChainBroken) || !strings.Contains(err.Error(), want) {
		t.Errorf("Expected verification to report %q, got %v", want, err)
	}
	store.Close()
	if reopened, err := reopenStore(t, dir); !errors.Is(err,
		ErrAuditChainBroken) || !strings.Contains(err.Error(), want) {
		if reopened != nil {
			reopened.Close()
		}
		t.Errorf("Expected the open to report %q, got %v", want, err)
	}
}

// TestAuditTailTriggersAreRefused checks that a trigger on audit_tail,
// or one elsewhere that names it, fails verification and the open.
func TestAuditTailTriggersAreRefused(t *testing.T) {
	for name, trigger := range map[string]string{
		"on audit_tail": `CREATE TRIGGER audit_tail_keep BEFORE UPDATE
            ON audit_tail BEGIN SELECT RAISE(IGNORE); END`,
		"naming audit_tail": `CREATE TRIGGER sv_tail AFTER INSERT
            ON schema_version BEGIN DELETE FROM audit_tail; END`,
	} {
		t.Run(name, func(t *testing.T) {
			store, dir := newReopenableStore(t)
			recordN(t, store, 2)
			tamperDB(t, store.db, trigger)
			expectTailSchemaRefused(t, store, dir, "acts on audit_tail")
		})
	}
}

// TestAuditTailTriggerOnALegacyTableIsRefused checks that a trigger on
// an audit_tail table the open would migrate is refused rather than
// dropped with the old table.
func TestAuditTailTriggerOnALegacyTableIsRefused(t *testing.T) {
	store, dir := newReopenableStore(t)
	tamperDB(t, store.db, "DROP TABLE audit_tail")
	tamperDB(t, store.db, auditTailLegacyDDLs[0])
	tamperDB(t, store.db, `CREATE TRIGGER audit_tail_keep BEFORE UPDATE
        ON audit_tail BEGIN SELECT RAISE(IGNORE); END`)
	store.Close()

	if _, err := reopenStore(t, dir); !errors.Is(err, ErrAuditChainBroken) ||
		!strings.Contains(err.Error(), "audit_tail_keep") {
		t.Errorf("Expected the open to refuse the trigger, got %v", err)
	}
}

// TestAlteredAuditTailDefinitionIsRefused checks that an audit_tail
// table that is not the one the server creates fails verification and
// the open.
func TestAlteredAuditTailDefinitionIsRefused(t *testing.T) {
	store, dir := newReopenableStore(t)
	recordN(t, store, 2)
	tamperDB(t, store.db, "DROP TABLE audit_tail")
	tamperDB(t, store.db, `CREATE TABLE audit_tail (
        id INTEGER PRIMARY KEY,
        event_id INTEGER NOT NULL,
        event_hash TEXT NOT NULL,
        mac TEXT NOT NULL,
        bound_event_id INTEGER,
        bound_event_hash TEXT,
        bound_mac TEXT
    )`)
	expectTailSchemaRefused(t, store, dir, "does not match the definition")
}

// TestAuditTailSchemaChecksReportFailures checks the read failures of
// the audit_tail schema checks.
func TestAuditTailSchemaChecksReportFailures(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	if err := store.verifyAuditTailSchema(); err != nil {
		t.Fatalf("Expected a fresh store to pass, got %v", err)
	}
	tamperDB(t, store.db, "DROP TABLE audit_tail")
	if err := store.verifyAuditTailSchema(); err == nil {
		t.Error("Expected a missing audit_tail table to be reported")
	}

	store.db.Close()
	if err := store.verifyAuditTailNoTrigger(); err == nil ||
		!strings.Contains(err.Error(), "failed to look for triggers") {
		t.Errorf("Expected a closed database to be reported, got %v", err)
	}
}

// TestMigrateTwoSlotAuditTail checks that the two-slot audit_tail a
// pre-release build created is rebuilt with the binding columns when
// the store is opened, keeping both anchors.
func TestMigrateTwoSlotAuditTail(t *testing.T) {
	store, dir := writeUnderKeys(t, 2, rotatedAuditKey)
	before := readTail(t, store)
	if !before.hasCurrent {
		t.Fatalf("Expected a current-key anchor, got %+v", before)
	}
	tamperDB(t, store.db, "DROP TABLE audit_tail")
	tamperDB(t, store.db, auditTailLegacyDDLs[1])
	tamperDB(t, store.db, `INSERT INTO audit_tail VALUES (1, ?, ?, ?),
        (2, ?, ?, ?)`, before.anchorID, before.anchorHash, before.anchorMAC,
		before.currentID, before.currentHash, before.currentMAC)
	store.Close()

	store = openWithKey(t, dir, rotatedAuditKey)
	defer store.Close()
	if def := normalizeSchemaSQL(auditTailDefinition(t, store)); def !=
		expectedTableSQL(auditTailDDL) {
		t.Fatalf("Expected the current definition, got %s", def)
	}
	if after := readTail(t, store); after != before {
		t.Errorf("Expected both anchors to survive, got %+v, want %+v",
			after, before)
	}
	expectKeyMismatch(t, store)
}
