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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// createTestAuthStoreForAudit creates a temporary auth store for the
// audit-log tests, together with a cleanup function that closes the
// store and removes its temporary directory.
func createTestAuthStoreForAudit(t *testing.T) (*AuthStore, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "auth-audit-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create auth store: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// mustRecord records an event in its own transaction and fails the test
// if any step returns an error.
func mustRecord(t *testing.T, s *AuthStore, ev *AuditEvent) {
	t.Helper()

	tx, err := s.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	if err := s.recordAudit(tx, ev); err != nil {
		tx.Rollback()
		t.Fatalf("recordAudit failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}
}

func int64Ptr(v int64) *int64 { return &v }

// mustHash hashes an event under the current format, defaulting an
// unset HashVersion the way recordAudit does, and fails the test if
// the version has no rendering.
func mustHash(t *testing.T, ev *AuditEvent) string {
	t.Helper()

	if ev.HashVersion == 0 {
		ev.HashVersion = auditHashVersion
	}
	hash, err := auditHash(ev, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("auditHash failed: %v", err)
	}

	return hash
}

// =============================================================================
// Schema
// =============================================================================

func TestAuditSchemaV5Migration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-audit-migrate-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}

	// Pretend the database was created by a v4 build, before the audit
	// log existed.
	if _, err := store.db.Exec("DROP TABLE audit_events"); err != nil {
		t.Fatalf("Failed to drop audit_events: %v", err)
	}
	if _, err := store.db.Exec("DELETE FROM schema_version"); err != nil {
		t.Fatalf("Failed to clear schema_version: %v", err)
	}
	if _, err := store.db.Exec(
		"INSERT INTO schema_version (version) VALUES (4)"); err != nil {
		t.Fatalf("Failed to set schema_version: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	reopened, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to reopen auth store: %v", err)
	}
	defer reopened.Close()

	var name string
	err = reopened.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='audit_events'").
		Scan(&name)
	if err != nil {
		t.Fatalf("audit_events table missing after migration: %v", err)
	}

	var version int
	if err := reopened.db.QueryRow(
		"SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("Failed to read schema version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("Expected schema version %d, got %d", schemaVersion, version)
	}

	// The append-only trigger must be re-created by the migration too.
	var trigger string
	err = reopened.db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='trigger' AND name='audit_events_no_update'").
		Scan(&trigger)
	if err != nil {
		t.Fatalf("audit_events_no_update trigger missing after migration: %v", err)
	}
}

// =============================================================================
// Hash chain
// =============================================================================

func TestRecordAuditChain(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		ev := newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", map[string]string{"k": "v"})
		mustRecord(t, store, ev)
	}

	rows, err := store.db.Query(
		"SELECT id, prev_hash, hash FROM audit_events ORDER BY id")
	if err != nil {
		t.Fatalf("Failed to query audit_events: %v", err)
	}
	defer rows.Close()

	var prevStored string
	count := 0
	for rows.Next() {
		var id int64
		var prevHash, hash string
		if err := rows.Scan(&id, &prevHash, &hash); err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}
		if count == 0 {
			if prevHash != "" {
				t.Errorf("Expected empty prev_hash on first row, got %q", prevHash)
			}
		} else if prevHash != prevStored {
			t.Errorf("Row %d prev_hash %q does not match previous hash %q",
				id, prevHash, prevStored)
		}
		if hash == "" {
			t.Errorf("Row %d has an empty hash", id)
		}
		prevStored = hash
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Row iteration failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("Expected 3 rows, got %d", count)
	}

	n, firstBad, err := store.VerifyAuditChain()
	if err != nil {
		t.Fatalf("VerifyAuditChain returned an error: %v", err)
	}
	if n != 3 {
		t.Errorf("Expected 3 verified rows, got %d", n)
	}
	if firstBad != 0 {
		t.Errorf("Expected firstBad 0, got %d", firstBad)
	}
}

func TestRecordAuditSetsOccurredAt(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	before := time.Now().UTC().Add(-time.Second)
	ev := newEvent(systemActor, "user.create", "user", nil, "alice", nil)
	mustRecord(t, store, ev)
	if ev.OccurredAt.Before(before) {
		t.Errorf("Expected OccurredAt to be set to now, got %v", ev.OccurredAt)
	}
	if ev.ID == 0 {
		t.Error("Expected the inserted row id to be populated")
	}

	// A preset timestamp is kept as given, and stored in UTC.
	backdated := time.Now().Add(-48 * time.Hour)
	ev2 := newEvent(systemActor, "user.delete", "user", nil, "bob", nil)
	ev2.OccurredAt = backdated
	mustRecord(t, store, ev2)
	if !ev2.OccurredAt.Equal(backdated) {
		t.Errorf("Expected OccurredAt %v to be preserved, got %v",
			backdated, ev2.OccurredAt)
	}

	var stored string
	if err := store.db.QueryRow(
		"SELECT occurred_at FROM audit_events WHERE id = ?", ev2.ID).
		Scan(&stored); err != nil {
		t.Fatalf("Failed to read occurred_at: %v", err)
	}
	if stored != backdated.UTC().Format(auditTimeLayout) {
		t.Errorf("Expected stored timestamp %q, got %q",
			backdated.UTC().Format(auditTimeLayout), stored)
	}
	if len(stored) != len(time.Now().UTC().Format(auditTimeLayout)) {
		t.Errorf("Expected a fixed-width timestamp, got %q", stored)
	}
}

func TestRecordAuditErrors(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	if err := store.recordAudit(tx, nil); err == nil {
		t.Error("Expected an error for a nil event")
	}

	// An invalid outcome trips the CHECK constraint.
	bad := newEvent(systemActor, "user.create", "user", nil, "alice", nil)
	bad.Outcome = AuditOutcome("bogus")
	if err := store.recordAudit(tx, bad); err == nil {
		t.Error("Expected an error for an invalid outcome")
	}
}

func TestAuditRowsImmutable(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustRecord(t, store, newEvent(systemActor, "user.create", "user",
		int64Ptr(1), "alice", nil))

	_, err := store.db.Exec("UPDATE audit_events SET action = 'x'")
	if err == nil {
		t.Fatal("Expected the append-only trigger to reject the update")
	}
	if !strings.Contains(err.Error(), "audit_events is append-only") {
		t.Errorf("Expected an append-only error, got %v", err)
	}
}

func TestVerifyAuditChainDetectsTamper(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		mustRecord(t, store, newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil))
	}

	if _, err := store.db.Exec(
		"DROP TRIGGER audit_events_no_update"); err != nil {
		t.Fatalf("Failed to drop trigger: %v", err)
	}
	if _, err := store.db.Exec(
		"UPDATE audit_events SET action = 'user.delete' WHERE id = 2"); err != nil {
		t.Fatalf("Failed to tamper with row: %v", err)
	}
	if _, err := store.db.Exec(auditSchemaDDL); err != nil {
		t.Fatalf("Failed to re-create trigger: %v", err)
	}

	_, firstBad, err := store.VerifyAuditChain()
	if err == nil {
		t.Fatal("Expected VerifyAuditChain to report a broken chain")
	}
	if firstBad != 2 {
		t.Errorf("Expected firstBad 2, got %d", firstBad)
	}
	if !strings.Contains(err.Error(), "audit chain broken at row 2") {
		t.Errorf("Unexpected error text: %v", err)
	}
}

func TestVerifyAuditChainDetectsBrokenLink(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		mustRecord(t, store, newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil))
	}

	// Re-link row 3 to a hash that is not row 2's, keeping row 3's own
	// hash consistent with the forged prev_hash so that only the link
	// check can catch it.
	var ev AuditEvent
	var occurredAt string
	if err := store.db.QueryRow(`
        SELECT occurred_at, actor_type, actor_name, action, outcome, hash
        FROM audit_events WHERE id = 3`).Scan(
		&occurredAt, &ev.ActorType, &ev.ActorName, &ev.Action,
		&ev.Outcome, &ev.Hash); err != nil {
		t.Fatalf("Failed to read row 3: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, occurredAt)
	if err != nil {
		t.Fatalf("Failed to parse occurred_at: %v", err)
	}
	ev.OccurredAt = parsed
	ev.TargetType = "user"
	ev.TargetID = int64Ptr(3)
	ev.TargetName = "alice"
	ev.PrevHash = strings.Repeat("a", 64)
	forged := mustHash(t, &ev)

	if _, err := store.db.Exec(
		"DROP TRIGGER audit_events_no_update"); err != nil {
		t.Fatalf("Failed to drop trigger: %v", err)
	}
	if _, err := store.db.Exec(
		"UPDATE audit_events SET prev_hash = ?, hash = ? WHERE id = 3",
		ev.PrevHash, forged); err != nil {
		t.Fatalf("Failed to re-link row: %v", err)
	}
	if _, err := store.db.Exec(auditSchemaDDL); err != nil {
		t.Fatalf("Failed to re-create trigger: %v", err)
	}

	_, firstBad, verifyErr := store.VerifyAuditChain()
	if verifyErr == nil {
		t.Fatal("Expected VerifyAuditChain to report a broken chain")
	}
	if firstBad != 3 {
		t.Errorf("Expected firstBad 3, got %d", firstBad)
	}
}

func TestVerifyAuditChainEmpty(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	rows, firstBad, err := store.VerifyAuditChain()
	if err != nil {
		t.Fatalf("VerifyAuditChain returned an error: %v", err)
	}
	if rows != 0 || firstBad != 0 {
		t.Errorf("Expected 0 rows and no bad row, got rows=%d firstBad=%d",
			rows, firstBad)
	}
}

func TestVerifyAuditChainQueryError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	store.db.Close()
	if _, _, err := store.VerifyAuditChain(); err == nil {
		t.Error("Expected an error from a closed database")
	}
}

func TestAuditDetailsHashCovers(t *testing.T) {
	base := AuditEvent{
		OccurredAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
		ActorType:  ActorSystem,
		ActorName:  "system",
		Action:     "user.update",
		TargetType: "user",
		TargetID:   int64Ptr(7),
		TargetName: "alice",
		Outcome:    OutcomeSuccess,
	}

	a := base
	a.Details = json.RawMessage(`{"after":{"enabled":true}}`)
	b := base
	b.Details = json.RawMessage(`{"after":{"enabled":false}}`)

	if mustHash(t, &a) == mustHash(t, &b) {
		t.Error("Expected differing details to produce differing hashes")
	}

	c := base
	c.Details = json.RawMessage(`{"after":{"enabled":true}}`)
	if mustHash(t, &a) != mustHash(t, &c) {
		t.Error("Expected identical events to produce identical hashes")
	}
}

// =============================================================================
// Purge
// =============================================================================

func TestPurgeAuditEventsKeepsChainVerifiable(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	now := time.Now().UTC()
	ages := []time.Duration{
		100 * 24 * time.Hour,
		50 * 24 * time.Hour,
		24 * time.Hour,
	}
	for i, age := range ages {
		ev := newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil)
		ev.OccurredAt = now.Add(-age)
		mustRecord(t, store, ev)
	}

	removed, err := store.PurgeAuditEvents(now.Add(-60 * 24 * time.Hour))
	if err != nil {
		t.Fatalf("PurgeAuditEvents failed: %v", err)
	}
	if removed != 1 {
		t.Fatalf("Expected 1 row removed, got %d", removed)
	}

	rows, firstBad, err := store.VerifyAuditChain()
	if err != nil {
		t.Fatalf("VerifyAuditChain failed after purge: %v", err)
	}
	// Two retained rows plus the audit.purge event the purge itself
	// records.
	if rows != 3 || firstBad != 0 {
		t.Errorf("Expected 3 verified rows, got rows=%d firstBad=%d",
			rows, firstBad)
	}
}

// TestPurgeAuditEventsRecordsPurgeEvent checks that a purge which
// removed rows leaves an audit.purge event behind, and that the event
// names the cutoff and the number of rows removed.
func TestPurgeAuditEventsRecordsPurgeEvent(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	now := time.Now().UTC()
	cutoff := now.Add(-60 * 24 * time.Hour)
	for i, age := range []time.Duration{100 * 24 * time.Hour, 24 * time.Hour} {
		ev := newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil)
		ev.OccurredAt = now.Add(-age)
		mustRecord(t, store, ev)
	}

	removed, err := store.PurgeAuditEvents(cutoff)
	if err != nil {
		t.Fatalf("PurgeAuditEvents failed: %v", err)
	}
	if removed != 1 {
		t.Fatalf("Expected 1 row removed, got %d", removed)
	}

	events, _, err := store.ListAuditEvents(AuditFilter{Action: auditActionPurge})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 audit.purge event, got %d", len(events))
	}

	ev := events[0]
	if ev.ActorType != ActorSystem || ev.ActorName != "system" {
		t.Errorf("Expected a system actor, got %q/%q", ev.ActorType, ev.ActorName)
	}
	if ev.TargetType != "" || ev.TargetID != nil {
		t.Errorf("Expected no target, got %q/%v", ev.TargetType, ev.TargetID)
	}
	if ev.Outcome != OutcomeSuccess {
		t.Errorf("Expected outcome success, got %q", ev.Outcome)
	}

	var details struct {
		OlderThan string `json:"older_than"`
		Removed   int64  `json:"removed"`
	}
	if err := json.Unmarshal(ev.Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", ev.Details, err)
	}
	if details.OlderThan != cutoff.UTC().Format(time.RFC3339) {
		t.Errorf("Expected older_than %q, got %q",
			cutoff.UTC().Format(time.RFC3339), details.OlderThan)
	}
	if details.Removed != 1 {
		t.Errorf("Expected removed 1, got %d", details.Removed)
	}

	if _, firstBad, err := store.VerifyAuditChain(); err != nil || firstBad != 0 {
		t.Errorf("Chain should verify after a purge: firstBad=%d err=%v",
			firstBad, err)
	}
}

// TestPurgeAuditEventsNoRowsRecordsNothing checks that a purge which
// matched nothing stays silent rather than filling the log with empty
// retention events.
func TestPurgeAuditEventsNoRowsRecordsNothing(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustRecord(t, store, newEvent(systemActor, "user.create", "user",
		int64Ptr(1), "alice", nil))

	removed, err := store.PurgeAuditEvents(time.Now().UTC().Add(-365 * 24 * time.Hour))
	if err != nil {
		t.Fatalf("PurgeAuditEvents failed: %v", err)
	}
	if removed != 0 {
		t.Fatalf("Expected 0 rows removed, got %d", removed)
	}

	events, _, err := store.ListAuditEvents(AuditFilter{Action: auditActionPurge})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Expected no audit.purge event, got %d", len(events))
	}
}

// TestVerifyAuditChainDetectsMissingTail checks that deleting the
// newest row, which leaves the surviving chain internally consistent,
// is still caught by the sqlite_sequence comparison.
func TestVerifyAuditChainDetectsMissingTail(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		mustRecord(t, store, newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil))
	}

	if _, firstBad, err := store.VerifyAuditChain(); err != nil || firstBad != 0 {
		t.Fatalf("Chain should verify before tampering: firstBad=%d err=%v",
			firstBad, err)
	}

	if _, err := store.db.Exec(
		"DELETE FROM audit_events WHERE id = (SELECT MAX(id) FROM audit_events)",
	); err != nil {
		t.Fatalf("Failed to delete the newest row: %v", err)
	}

	_, firstBad, err := store.VerifyAuditChain()
	if err == nil {
		t.Fatal("Expected a tail-missing error")
	}
	if firstBad != 0 {
		t.Errorf("Expected firstBad 0 for a missing tail, got %d", firstBad)
	}
	if !strings.Contains(err.Error(), "audit chain tail missing") {
		t.Errorf("Expected a tail-missing error, got %v", err)
	}
}

// TestVerifyAuditTailSequenceRowRemoved checks that deleting the
// sqlite_sequence row whilst events remain is reported rather than
// accepted. SQLite writes that row with the first insert and never
// removes it, so its absence alongside surviving rows can only be the
// work of something outside the server.
func TestVerifyAuditTailSequenceRowRemoved(t *testing.T) {
	cases := []struct {
		name string
		stmt string
	}{
		{"deleted", "DELETE FROM sqlite_sequence WHERE name = 'audit_events'"},
		{"nulled", "UPDATE sqlite_sequence SET seq = NULL WHERE name = 'audit_events'"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			mustRecord(t, store, newEvent(systemActor, "user.create", "user",
				int64Ptr(1), "alice", nil))
			if _, err := store.db.Exec(tc.stmt); err != nil {
				t.Fatalf("Failed to tamper with sqlite_sequence: %v", err)
			}

			err := store.verifyAuditTail()
			if err == nil {
				t.Fatal("Expected a tail-missing error with events still present")
			}
			if !strings.Contains(err.Error(), "audit chain tail missing") {
				t.Errorf("Expected a tail-missing error, got %v", err)
			}
		})
	}
}

// TestVerifyAuditTailSequenceBelowNewestRow checks the other half of
// the comparison. Rewriting sqlite_sequence downwards is how a
// truncation is disguised once the deletion alone is caught, and
// overshooting leaves the sequence behind the newest surviving row,
// which ordinary inserts can never produce: SQLite raises the sequence
// before it writes the row.
func TestVerifyAuditTailSequenceBelowNewestRow(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		mustRecord(t, store, newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil))
	}

	if _, err := store.db.Exec(
		"UPDATE sqlite_sequence SET seq = 1 WHERE name = 'audit_events'",
	); err != nil {
		t.Fatalf("Failed to rewrite sqlite_sequence: %v", err)
	}

	err := store.verifyAuditTail()
	if err == nil {
		t.Fatal("Expected a sequence below the newest row to be reported")
	}
	if !strings.Contains(err.Error(), "audit sequence inconsistent") {
		t.Errorf("Expected an inconsistency error, got %v", err)
	}
}

// TestVerifyAuditChainDetectsResequencedTruncation covers the attack
// that survives the two-delete fix: truncate the log, then rewrite
// sqlite_sequence rather than delete it. Setting it to the exact
// surviving maximum is undetectable by arithmetic alone and the
// function comment says so; anything else is caught.
func TestVerifyAuditChainDetectsResequencedTruncation(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		mustRecord(t, store, newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil))
	}

	if _, err := store.db.Exec("DELETE FROM audit_events WHERE id > 2"); err != nil {
		t.Fatalf("Failed to truncate the log: %v", err)
	}
	// Overshooting, which is the likely outcome of guessing at the
	// value rather than reading it.
	if _, err := store.db.Exec(
		"UPDATE sqlite_sequence SET seq = 1 WHERE name = 'audit_events'",
	); err != nil {
		t.Fatalf("Failed to rewrite sqlite_sequence: %v", err)
	}

	if _, _, err := store.VerifyAuditChain(); err == nil {
		t.Error("Expected a rewritten sequence below the newest row to fail")
	}
}

// TestVerifyAuditTailEmptyLogWithoutSequenceRow checks that a log which
// has never held an event verifies cleanly. Before the first insert
// there is no sequence row to compare against, and nothing to compare
// it with either.
func TestVerifyAuditTailEmptyLogWithoutSequenceRow(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	if _, err := store.db.Exec("DELETE FROM audit_events"); err != nil {
		t.Fatalf("Failed to empty audit_events: %v", err)
	}
	if _, err := store.db.Exec(
		"DELETE FROM sqlite_sequence WHERE name = 'audit_events'"); err != nil {
		t.Fatalf("Failed to clear sqlite_sequence: %v", err)
	}

	if err := store.verifyAuditTail(); err != nil {
		t.Errorf("Expected an empty log to verify, got %v", err)
	}
}

// TestVerifyAuditTailWithoutSequenceTable checks that a database with
// no sqlite_sequence table at all is reported rather than passed. Every
// database this store creates has one, because users and the other
// tables are AUTOINCREMENT, so its absence means it was removed.
func TestVerifyAuditTailWithoutSequenceTable(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open in-memory database: %v", err)
	}
	defer db.Close()

	// No AUTOINCREMENT anywhere, so SQLite never creates
	// sqlite_sequence.
	if _, err := db.Exec(
		"CREATE TABLE audit_events (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("Failed to create audit_events: %v", err)
	}
	if _, err := db.Exec("INSERT INTO audit_events (id) VALUES (7)"); err != nil {
		t.Fatalf("Failed to insert a row: %v", err)
	}

	store := &AuthStore{db: db}
	err = store.verifyAuditTail()
	if err == nil {
		t.Fatal("Expected a missing sqlite_sequence table to be reported")
	}
	if !strings.Contains(err.Error(), "sqlite_sequence table is missing") {
		t.Errorf("Unexpected error text: %v", err)
	}
}

// TestCheckAuditTail pins each verdict of the tail comparison. The
// newest id and the sequence are read by one statement, so a server
// insert whilst the command line verifies cannot split them; the pairs
// here are therefore the only ones a real read can produce, plus the
// tampered ones.
func TestCheckAuditTail(t *testing.T) {
	seq := func(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }

	cases := []struct {
		name    string
		newest  int64
		seq     sql.NullInt64
		wantErr string
	}{
		{"quiet log agrees", 5, seq(5), ""},
		{"empty log without a row", 0, sql.NullInt64{}, ""},
		{"tail deleted", 5, seq(8), "tail missing"},
		{"sequence written down", 5, seq(2), "inconsistent"},
		{"sequence row deleted or nulled", 5, sql.NullInt64{}, "removed or emptied"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkAuditTail(tc.newest, tc.seq)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Expected the tail to pass, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Expected an error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// TestVerifyAuditTailSurvivesConcurrentWriter checks the property the
// single-statement read exists for: a second store writing to the same
// file whilst the first verifies must never produce a false "tail
// missing" report.
func TestVerifyAuditTailSurvivesConcurrentWriter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-audit-tail-race-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	verifier, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create the verifying store: %v", err)
	}
	defer verifier.Close()
	writer, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create the writing store: %v", err)
	}
	defer writer.Close()

	done := make(chan struct{})
	writeErrs := make(chan error, 1)
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			ev := newEvent(systemActor, "user.create", "user",
				int64Ptr(int64(i+1)), "alice", nil)
			writer.mu.Lock()
			err := writer.recordAuditInOwnTx(ev)
			writer.mu.Unlock()
			if err != nil {
				writeErrs <- err
				return
			}
		}
	}()

	for i := 0; i < 200; i++ {
		if err := verifier.verifyAuditTail(); err != nil {
			t.Fatalf("Tail check failed against a live writer: %v", err)
		}
	}
	<-done
	select {
	case err := <-writeErrs:
		t.Fatalf("Concurrent write failed: %v", err)
	default:
	}
}

// TestVerifyAuditTailSequenceLookupError checks that a failure to
// determine whether sqlite_sequence exists is returned rather than
// treated as absence: a check that could not run has not passed.
func TestVerifyAuditTailSequenceLookupError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open in-memory database: %v", err)
	}
	db.Close()

	store := &AuthStore{db: db}
	if err := store.verifyAuditTail(); err == nil {
		t.Error("Expected an error when sqlite_master cannot be read")
	}
}

// TestVerifyAuditTailMaxQueryError covers the MAX(id) lookup failing.
func TestVerifyAuditTailMaxQueryError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustRecord(t, store, newEvent(systemActor, "user.create", "user",
		int64Ptr(1), "alice", nil))
	if _, err := store.db.Exec("DROP TABLE audit_events"); err != nil {
		t.Fatalf("Failed to drop audit_events: %v", err)
	}

	if err := store.verifyAuditTail(); err == nil {
		t.Error("Expected an error when the newest row cannot be read")
	}
}

// TestVerifyAuditChainDetectsTwoDeleteTruncation exercises the attack
// the single-statement tail check missed: delete the newest events,
// then delete the sqlite_sequence row that records how many ids were
// ever issued. Neither statement is blocked by the append-only trigger,
// which is BEFORE UPDATE only, and neither needs any knowledge of the
// hash format. The surviving rows still chain correctly, so only the
// sequence comparison can catch this.
func TestVerifyAuditChainDetectsTwoDeleteTruncation(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		mustRecord(t, store, newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil))
	}

	if _, firstBad, err := store.VerifyAuditChain(); err != nil || firstBad != 0 {
		t.Fatalf("Chain should verify before tampering: firstBad=%d err=%v",
			firstBad, err)
	}

	if _, err := store.db.Exec("DELETE FROM audit_events WHERE id > 2"); err != nil {
		t.Fatalf("Failed to truncate the log: %v", err)
	}
	if _, err := store.db.Exec(
		"DELETE FROM sqlite_sequence WHERE name = 'audit_events'"); err != nil {
		t.Fatalf("Failed to clear sqlite_sequence: %v", err)
	}

	count, firstBad, err := store.VerifyAuditChain()
	if err == nil {
		t.Fatal("Expected the two-statement truncation to fail verification")
	}
	if !strings.Contains(err.Error(), "audit chain tail missing") {
		t.Errorf("Expected a tail-missing error, got %v", err)
	}
	if count != 2 {
		t.Errorf("Expected the two surviving rows to be counted, got %d", count)
	}
	if firstBad != 0 {
		t.Errorf("Expected firstBad 0 for a missing tail, got %d", firstBad)
	}
}

// TestAuditOccurredAtIsFixedWidth checks that every stored timestamp
// uses the fixed-width layout, which is what makes the lexical string
// comparisons in auditWhere and PurgeAuditEvents order correctly.
func TestAuditOccurredAtIsFixedWidth(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	base := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	for i, at := range []time.Time{
		base,
		base.Add(400 * time.Millisecond),
		base.Add(time.Second),
		{},
	} {
		ev := newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil)
		ev.OccurredAt = at
		mustRecord(t, store, ev)
	}

	rows, err := store.db.Query(
		"SELECT id, occurred_at FROM audit_events ORDER BY id")
	if err != nil {
		t.Fatalf("Failed to query audit rows: %v", err)
	}
	defer rows.Close()

	seen := 0
	for rows.Next() {
		var id int64
		var occurredAt string
		if err := rows.Scan(&id, &occurredAt); err != nil {
			t.Fatalf("Failed to scan audit row: %v", err)
		}
		if len(occurredAt) != 30 {
			t.Errorf("Row %d: expected a 30-character occurred_at, got %q (%d)",
				id, occurredAt, len(occurredAt))
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Failed to read audit rows: %v", err)
	}
	if seen < 4 {
		t.Errorf("Expected at least 4 rows, got %d", seen)
	}
}

func TestPurgeAuditEventsError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	store.db.Close()
	if _, err := store.PurgeAuditEvents(time.Now()); err == nil {
		t.Error("Expected an error from a closed database")
	}
}

// =============================================================================
// Denials and failures
// =============================================================================

func TestRecordDenied(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	actor := Actor{Type: ActorUser, ID: int64Ptr(4), Name: "bob", IP: "192.0.2.10"}
	if err := store.RecordDenied(actor, "group.delete",
		"requires manage_groups"); err != nil {
		t.Fatalf("RecordDenied failed: %v", err)
	}

	events, total, err := store.ListAuditEvents(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if total != 1 || len(events) != 1 {
		t.Fatalf("Expected 1 event, got total=%d len=%d", total, len(events))
	}

	ev := events[0]
	if ev.Outcome != OutcomeDenied {
		t.Errorf("Expected outcome denied, got %q", ev.Outcome)
	}
	if ev.Action != "group.delete" {
		t.Errorf("Expected action group.delete, got %q", ev.Action)
	}
	if ev.Error != "requires manage_groups" {
		t.Errorf("Expected the reason in error, got %q", ev.Error)
	}
	if ev.TargetType != "" || ev.TargetID != nil || ev.TargetName != "" {
		t.Errorf("Expected an empty target, got %q/%v/%q",
			ev.TargetType, ev.TargetID, ev.TargetName)
	}
	if ev.ActorName != "bob" || ev.ActorIP != "192.0.2.10" {
		t.Errorf("Unexpected actor %q/%q", ev.ActorName, ev.ActorIP)
	}
	if ev.ActorID == nil || *ev.ActorID != 4 {
		t.Errorf("Unexpected actor id %v", ev.ActorID)
	}
}

func TestRecordDeniedError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	store.db.Close()
	err := store.RecordDenied(Actor{Type: ActorUser, Name: "bob"},
		"group.delete", "nope")
	if err == nil {
		t.Error("Expected an error from a closed database")
	}
}

func TestRecordFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	store.mu.Lock()
	store.recordFailure(systemActor, "user.delete", "user", int64Ptr(9),
		"carol", os.ErrNotExist)
	store.mu.Unlock()

	events, total, err := store.ListAuditEvents(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if total != 1 {
		t.Fatalf("Expected 1 event, got %d", total)
	}
	ev := events[0]
	if ev.Outcome != OutcomeFailure {
		t.Errorf("Expected outcome failure, got %q", ev.Outcome)
	}
	if ev.Error != os.ErrNotExist.Error() {
		t.Errorf("Unexpected error text %q", ev.Error)
	}
	if ev.TargetName != "carol" || ev.TargetType != "user" {
		t.Errorf("Unexpected target %q/%q", ev.TargetType, ev.TargetName)
	}
}

func TestRecordFailureLogsAndSwallowsErrors(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	store.db.Close()
	store.mu.Lock()
	store.recordFailure(systemActor, "user.delete", "user", nil, "carol",
		os.ErrNotExist)
	store.mu.Unlock()

	if !strings.Contains(buf.String(),
		"Failed to record audit failure event") {
		t.Errorf("Expected a logged failure, got %q", buf.String())
	}
}

func TestRecordFailureNilCause(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	store.mu.Lock()
	store.recordFailure(systemActor, "user.delete", "user", nil, "carol", nil)
	store.mu.Unlock()

	events, _, err := store.ListAuditEvents(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Error != "unknown error" {
		t.Errorf("Expected a placeholder error text, got %q", events[0].Error)
	}
}

// =============================================================================
// newEvent and AsActor
// =============================================================================

func TestNewEventMarshalsDetails(t *testing.T) {
	ev := newEvent(Actor{Type: ActorToken, ID: int64Ptr(3), Name: "ci",
		IP: "198.51.100.4"}, "token.delete", "token", int64Ptr(3), "ci",
		map[string]any{"before": map[string]any{"name": "ci"}})

	if ev.Outcome != OutcomeSuccess {
		t.Errorf("Expected outcome success, got %q", ev.Outcome)
	}
	if ev.ActorType != ActorToken || ev.ActorName != "ci" {
		t.Errorf("Unexpected actor %q/%q", ev.ActorType, ev.ActorName)
	}
	if ev.ActorIP != "198.51.100.4" {
		t.Errorf("Unexpected actor IP %q", ev.ActorIP)
	}
	if string(ev.Details) != `{"before":{"name":"ci"}}` {
		t.Errorf("Unexpected details %q", string(ev.Details))
	}
}

func TestNewEventNilDetails(t *testing.T) {
	ev := newEvent(systemActor, "user.create", "user", nil, "alice", nil)
	if ev.Details != nil {
		t.Errorf("Expected nil details, got %q", string(ev.Details))
	}
}

func TestNewEventUnmarshalableDetails(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	ev := newEvent(systemActor, "user.create", "user", nil, "alice",
		make(chan int))
	if ev.Details != nil {
		t.Errorf("Expected details to be dropped, got %q", string(ev.Details))
	}
	if !strings.Contains(buf.String(), "Failed to marshal audit details") {
		t.Errorf("Expected a logged marshal failure, got %q", buf.String())
	}
}

func TestAsActor(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	actor := Actor{Type: ActorUser, ID: int64Ptr(1), Name: "alice",
		IP: "192.0.2.1"}
	as := store.AsActor(actor)
	if as == nil {
		t.Fatal("AsActor returned nil")
	}
	if as.s != store {
		t.Error("AsActor did not carry the store")
	}
	if as.actor.Name != "alice" || as.actor.Type != ActorUser {
		t.Errorf("AsActor did not carry the actor: %+v", as.actor)
	}
}

// =============================================================================
// Listing
// =============================================================================

// seedAuditEvents inserts six events that differ in actor, action,
// target, outcome and time, oldest first.
func seedAuditEvents(t *testing.T, store *AuthStore) time.Time {
	t.Helper()

	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	type seed struct {
		actor   Actor
		action  string
		tType   string
		tID     *int64
		tName   string
		outcome AuditOutcome
	}
	seeds := []seed{
		{Actor{Type: ActorUser, ID: int64Ptr(1), Name: "alice", IP: "192.0.2.1"},
			"user.create", "user", int64Ptr(10), "dave", OutcomeSuccess},
		{Actor{Type: ActorUser, ID: int64Ptr(1), Name: "alice", IP: "192.0.2.1"},
			"user.delete", "user", int64Ptr(10), "dave", OutcomeFailure},
		{Actor{Type: ActorToken, ID: int64Ptr(2), Name: "ci", IP: "192.0.2.2"},
			"group.create", "group", int64Ptr(20), "ops", OutcomeSuccess},
		{Actor{Type: ActorToken, ID: int64Ptr(2), Name: "ci", IP: "192.0.2.2"},
			"group.delete", "group", int64Ptr(20), "ops", OutcomeDenied},
		{systemActor, "token.create", "token", int64Ptr(30), "bot",
			OutcomeSuccess},
		{Actor{Type: ActorCLI, Name: "root"}, "user.update", "user",
			int64Ptr(11), "erin", OutcomeSuccess},
	}

	for i, s := range seeds {
		ev := newEvent(s.actor, s.action, s.tType, s.tID, s.tName, nil)
		ev.Outcome = s.outcome
		ev.OccurredAt = base.Add(time.Duration(i) * time.Hour)
		mustRecord(t, store, ev)
	}

	return base
}

func TestListAuditEventsFilters(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	base := seedAuditEvents(t, store)

	t.Run("no filter returns all, newest first", func(t *testing.T) {
		events, total, err := store.ListAuditEvents(AuditFilter{})
		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		if total != 6 || len(events) != 6 {
			t.Fatalf("Expected 6 events, got total=%d len=%d", total, len(events))
		}
		for i := 1; i < len(events); i++ {
			if events[i-1].ID <= events[i].ID {
				t.Fatalf("Expected descending ids, got %d then %d",
					events[i-1].ID, events[i].ID)
			}
		}
		if events[0].Action != "user.update" {
			t.Errorf("Expected the newest event first, got %q", events[0].Action)
		}
	})

	t.Run("actor name", func(t *testing.T) {
		events, total, err := store.ListAuditEvents(
			AuditFilter{ActorName: "alice"})
		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		if total != 2 || len(events) != 2 {
			t.Fatalf("Expected 2 events, got total=%d len=%d", total, len(events))
		}
	})

	t.Run("actor type", func(t *testing.T) {
		_, total, err := store.ListAuditEvents(
			AuditFilter{ActorType: string(ActorToken)})
		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		if total != 2 {
			t.Errorf("Expected 2 events, got %d", total)
		}
	})

	t.Run("action", func(t *testing.T) {
		_, total, err := store.ListAuditEvents(
			AuditFilter{Action: "group.create"})
		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		if total != 1 {
			t.Errorf("Expected 1 event, got %d", total)
		}
	})

	t.Run("target type and id", func(t *testing.T) {
		_, total, err := store.ListAuditEvents(
			AuditFilter{TargetType: "user", TargetID: int64Ptr(10)})
		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		if total != 2 {
			t.Errorf("Expected 2 events, got %d", total)
		}
	})

	t.Run("outcome", func(t *testing.T) {
		_, total, err := store.ListAuditEvents(
			AuditFilter{Outcome: string(OutcomeDenied)})
		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		if total != 1 {
			t.Errorf("Expected 1 event, got %d", total)
		}
	})

	t.Run("time range", func(t *testing.T) {
		since := base.Add(2 * time.Hour)
		until := base.Add(3 * time.Hour)
		events, total, err := store.ListAuditEvents(
			AuditFilter{Since: &since, Until: &until})
		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		if total != 2 || len(events) != 2 {
			t.Fatalf("Expected 2 events, got total=%d len=%d", total, len(events))
		}
		if events[0].Action != "group.delete" {
			t.Errorf("Unexpected newest event %q", events[0].Action)
		}
	})

	t.Run("paging", func(t *testing.T) {
		events, total, err := store.ListAuditEvents(
			AuditFilter{Limit: 2, Offset: 2})
		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		if total != 6 {
			t.Errorf("Expected total 6, got %d", total)
		}
		if len(events) != 2 {
			t.Fatalf("Expected 2 events, got %d", len(events))
		}
		if events[0].Action != "group.delete" {
			t.Errorf("Unexpected first event on page 2: %q", events[0].Action)
		}
	})

	t.Run("negative offset is clamped", func(t *testing.T) {
		events, _, err := store.ListAuditEvents(
			AuditFilter{Limit: 1, Offset: -5})
		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		if len(events) != 1 || events[0].Action != "user.update" {
			t.Errorf("Expected the newest event, got %+v", events)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		events, total, err := store.ListAuditEvents(
			AuditFilter{ActorName: "nobody"})
		if err != nil {
			t.Fatalf("ListAuditEvents failed: %v", err)
		}
		if total != 0 || len(events) != 0 {
			t.Errorf("Expected no events, got total=%d len=%d", total, len(events))
		}
	})
}

func TestListAuditEventsLimits(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{"zero uses the default", 0, defaultAuditLimit},
		{"negative uses the default", -1, defaultAuditLimit},
		{"oversized is capped", 1000, maxAuditLimit},
		{"in range is preserved", 25, 25},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveAuditLimit(tc.limit); got != tc.want {
				t.Errorf("Expected limit %d, got %d", tc.want, got)
			}
		})
	}

	// A limit beyond the row count simply returns everything present.
	seedAuditEvents(t, store)
	events, _, err := store.ListAuditEvents(AuditFilter{Limit: 1000})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 6 {
		t.Errorf("Expected 6 events, got %d", len(events))
	}
}

func TestListAuditEventsRoundTripsNullableFields(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	ev := newEvent(Actor{Type: ActorCLI, Name: "root"}, "user.create", "",
		nil, "", nil)
	mustRecord(t, store, ev)

	events, _, err := store.ListAuditEvents(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	got := events[0]
	if got.ActorID != nil || got.ActorIP != "" || got.TargetType != "" ||
		got.TargetID != nil || got.TargetName != "" || got.Error != "" ||
		got.Details != nil {
		t.Errorf("Expected empty optional fields, got %+v", got)
	}
	if !got.OccurredAt.Equal(ev.OccurredAt) {
		t.Errorf("Expected occurred_at %v, got %v", ev.OccurredAt, got.OccurredAt)
	}
	if got.ActorType != ActorCLI || got.ActorName != "root" {
		t.Errorf("Unexpected actor %q/%q", got.ActorType, got.ActorName)
	}
}

func TestListAuditEventsErrors(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	store.db.Close()
	if _, _, err := store.ListAuditEvents(AuditFilter{}); err == nil {
		t.Error("Expected an error from a closed database")
	}
}

func TestRecordAuditDefaultsActorAndOutcome(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	// An event built by hand, without an actor or outcome, is
	// attributed to the system actor and recorded as a success rather
	// than being rejected by the CHECK constraints.
	mustRecord(t, store, &AuditEvent{Action: "user.create"})

	events, _, err := store.ListAuditEvents(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].ActorType != ActorSystem || events[0].ActorName != "system" {
		t.Errorf("Unexpected actor %q/%q",
			events[0].ActorType, events[0].ActorName)
	}
	if events[0].Outcome != OutcomeSuccess {
		t.Errorf("Expected outcome success, got %q", events[0].Outcome)
	}
}

func TestRecordAuditPreviousHashQueryError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DROP TABLE audit_events"); err != nil {
		t.Fatalf("Failed to drop audit_events: %v", err)
	}

	ev := newEvent(systemActor, "user.create", "user", nil, "alice", nil)
	if err := store.recordAudit(tx, ev); err == nil {
		t.Error("Expected an error when the audit table is missing")
	}
}

func TestRecordDeniedInsertError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	if _, err := store.db.Exec("DROP TABLE audit_events"); err != nil {
		t.Fatalf("Failed to drop audit_events: %v", err)
	}

	err := store.RecordDenied(Actor{Type: ActorUser, Name: "bob"},
		"group.delete", "nope")
	if err == nil {
		t.Error("Expected an error when the audit table is missing")
	}
}

func TestListAuditEventsRejectsUnparseableTimestamp(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	if _, err := store.db.Exec(`
        INSERT INTO audit_events (
            occurred_at, actor_type, actor_name, action, outcome,
            prev_hash, hash
        ) VALUES ('not-a-timestamp', 'system', 'system', 'user.create',
            'success', '', 'deadbeef')`); err != nil {
		t.Fatalf("Failed to insert row: %v", err)
	}

	_, _, err := store.ListAuditEvents(AuditFilter{})
	if err == nil {
		t.Fatal("Expected an error for an unparseable timestamp")
	}
	if !strings.Contains(err.Error(), "invalid occurred_at") {
		t.Errorf("Unexpected error text: %v", err)
	}

	if _, _, err := store.VerifyAuditChain(); err == nil {
		t.Error("Expected VerifyAuditChain to reject the row too")
	}
}

func TestMigrateV4ToV5Errors(t *testing.T) {
	t.Run("DDL failure", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		store.db.Close()
		if err := store.migrateV4ToV5(); err == nil {
			t.Error("Expected an error from a closed database")
		}
	})

	t.Run("version clear failure", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		if _, err := store.db.Exec("DROP TABLE schema_version"); err != nil {
			t.Fatalf("Failed to drop schema_version: %v", err)
		}
		if err := store.migrateV4ToV5(); err == nil {
			t.Error("Expected an error when schema_version is missing")
		}
	})

	t.Run("version set failure", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		// A schema_version table that refuses version 5 makes the
		// final INSERT fail while the DELETE before it succeeds.
		if _, err := store.db.Exec(`
            DROP TABLE schema_version;
            CREATE TABLE schema_version (
                version INTEGER PRIMARY KEY CHECK (version < 5)
            );`); err != nil {
			t.Fatalf("Failed to rebuild schema_version: %v", err)
		}
		if err := store.migrateV4ToV5(); err == nil {
			t.Error("Expected an error when the version cannot be set")
		}
	})
}

func TestNewAuthStoreReportsV5MigrationFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-audit-migrate-fail-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}

	// A v4 database whose schema_version table cannot hold version 5.
	if _, err := store.db.Exec(`
        DROP TABLE audit_events;
        DROP TABLE schema_version;
        CREATE TABLE schema_version (
            version INTEGER PRIMARY KEY CHECK (version < 5)
        );
        INSERT INTO schema_version (version) VALUES (4);`); err != nil {
		t.Fatalf("Failed to prepare a v3 database: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	reopened, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err == nil {
		reopened.Close()
		t.Fatal("Expected NewAuthStore to fail when the migration fails")
	}
	if !strings.Contains(err.Error(), "v4 to v5") {
		t.Errorf("Unexpected error text: %v", err)
	}
}

func TestAuditSchemaVersionOnFreshInstall(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	var version int
	if err := store.db.QueryRow(
		"SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("Failed to read schema version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("Expected schema version %d, got %d", schemaVersion, version)
	}
	if schemaVersion != 6 {
		t.Errorf("Expected schemaVersion 6, got %d", schemaVersion)
	}

	// The audit table, its append-only trigger and the anti-fork index
	// are part of the fresh-install schema, not only of the migrations.
	for _, want := range []struct{ kind, name string }{
		{"table", "audit_events"},
		{"trigger", auditNoUpdateTrigger},
		{"index", auditChainIndexName},
	} {
		var name string
		if err := store.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type = ? AND name = ?",
			want.kind, want.name).Scan(&name); err != nil {
			t.Errorf("%s %s missing on a fresh install: %v",
				want.kind, want.name, err)
		}
	}
}

func TestListAuditEventsSubSecondTimeRange(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	// Timestamps either side of a whole second. time.RFC3339Nano would
	// render the whole second as "...T10:00:00Z" and the fractional
	// ones as "...T10:00:00.4Z", which compare in the wrong order
	// because '.' sorts before 'Z'; the fixed-width auditTimeLayout
	// keeps string comparison in timestamp order.
	base := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	stamps := []time.Time{
		base.Add(-100 * time.Millisecond),
		base,
		base.Add(400 * time.Millisecond),
		base.Add(1500 * time.Millisecond),
	}
	for i, at := range stamps {
		ev := newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil)
		ev.OccurredAt = at
		mustRecord(t, store, ev)
	}

	since := base
	until := base.Add(500 * time.Millisecond)
	events, total, err := store.ListAuditEvents(
		AuditFilter{Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if total != 2 || len(events) != 2 {
		t.Fatalf("Expected 2 events in range, got total=%d len=%d",
			total, len(events))
	}
	for _, ev := range events {
		if ev.OccurredAt.Before(since) || ev.OccurredAt.After(until) {
			t.Errorf("Event at %v is outside the requested range", ev.OccurredAt)
		}
	}
}

func TestPurgeAuditEventsSubSecondCutoff(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	base := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	for i, at := range []time.Time{
		base.Add(-400 * time.Millisecond),
		base,
		base.Add(400 * time.Millisecond),
	} {
		ev := newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil)
		ev.OccurredAt = at
		mustRecord(t, store, ev)
	}

	removed, err := store.PurgeAuditEvents(base)
	if err != nil {
		t.Fatalf("PurgeAuditEvents failed: %v", err)
	}
	if removed != 1 {
		t.Fatalf("Expected 1 row removed, got %d", removed)
	}

	events, _, err := store.ListAuditEvents(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	// Two retained rows plus the audit.purge event.
	if len(events) != 3 {
		t.Fatalf("Expected 3 remaining events, got %d", len(events))
	}
	for _, ev := range events {
		if ev.Action == auditActionPurge {
			continue
		}
		if ev.OccurredAt.Before(base) {
			t.Errorf("Event at %v should have been purged", ev.OccurredAt)
		}
	}
}

func TestRecordAuditKeepsCallerActorName(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	// An event with a name but no type keeps the name and only has the
	// type defaulted.
	mustRecord(t, store, &AuditEvent{Action: "user.create",
		ActorName: "bootstrap"})

	events, _, err := store.ListAuditEvents(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if events[0].ActorType != ActorSystem {
		t.Errorf("Expected actor type system, got %q", events[0].ActorType)
	}
	if events[0].ActorName != "bootstrap" {
		t.Errorf("Expected the caller's actor name, got %q", events[0].ActorName)
	}
}

// TestAuditChainSurvivesTwoStores exercises the cross-process case that
// the _txlock=immediate DSN option exists for: two AuthStore instances
// on the same data directory, each with its own s.mu, writing audit
// events concurrently. Without the write lock taken at BEGIN, two
// transactions can both read the same "previous" hash before either
// inserts, and the chain forks; with it, one of them blocks until the
// other commits.
func TestAuditChainSurvivesTwoStores(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-audit-concurrent-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	first, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create the first auth store: %v", err)
	}
	defer first.Close()

	second, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create the second auth store: %v", err)
	}
	defer second.Close()

	const perStore = 25
	var wg sync.WaitGroup
	errs := make(chan error, 2*perStore)

	for _, s := range []*AuthStore{first, second} {
		wg.Add(1)
		go func(store *AuthStore) {
			defer wg.Done()
			for i := 0; i < perStore; i++ {
				ev := newEvent(systemActor, "user.create", "user",
					int64Ptr(int64(i+1)), "alice", nil)
				store.mu.Lock()
				err := store.recordAuditInOwnTx(ev)
				store.mu.Unlock()
				if err != nil {
					errs <- err
				}
			}
		}(s)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Concurrent audit write failed: %v", err)
	}

	rows, firstBad, err := first.VerifyAuditChain()
	if err != nil {
		t.Fatalf("VerifyAuditChain failed after concurrent writes: %v", err)
	}
	if firstBad != 0 {
		t.Errorf("Expected an intact chain, first bad row %d", firstBad)
	}

	events, total, err := first.ListAuditEvents(AuditFilter{Limit: maxAuditLimit})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if total != 2*perStore {
		t.Errorf("Expected %d events, got %d", 2*perStore, total)
	}
	if rows != total {
		t.Errorf("Verified %d rows but the log holds %d", rows, total)
	}
	if len(events) != total {
		t.Errorf("Expected %d events in the page, got %d", total, len(events))
	}
}

// TestPurgeAuditEventsRecordFailureRollsBack checks that a purge whose
// own audit event cannot be written abandons the DELETE as well, so
// that rows are never removed without the event that accounts for them.
func TestPurgeAuditEventsRecordFailureRollsBack(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	now := time.Now().UTC()
	for i, age := range []time.Duration{100 * 24 * time.Hour, 24 * time.Hour} {
		ev := newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil)
		ev.OccurredAt = now.Add(-age)
		mustRecord(t, store, ev)
	}

	if _, err := store.db.Exec(`
        CREATE TRIGGER audit_events_block_insert
        BEFORE INSERT ON audit_events
        BEGIN
            SELECT RAISE(ABORT, 'no inserts');
        END`); err != nil {
		t.Fatalf("Failed to create the blocking trigger: %v", err)
	}

	if _, err := store.PurgeAuditEvents(now.Add(-60 * 24 * time.Hour)); err == nil {
		t.Fatal("Expected the purge to fail when its event cannot be written")
	}

	if _, err := store.db.Exec(
		"DROP TRIGGER audit_events_block_insert"); err != nil {
		t.Fatalf("Failed to drop the blocking trigger: %v", err)
	}

	events, total, err := store.ListAuditEvents(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if total != 2 {
		t.Errorf("Expected both rows to survive the rolled-back purge, got %d",
			total)
	}
	if len(events) != 2 {
		t.Errorf("Expected 2 events in the page, got %d", len(events))
	}
}

// TestAuditHashFieldsAreUnambiguous checks that moving a separator
// character across a field boundary changes the hash. The canonical
// rendering length-prefixes each field precisely so that an actor name
// containing the old "|" separator cannot be made to stand for a
// different pair of column values.
func TestAuditHashFieldsAreUnambiguous(t *testing.T) {
	occurred := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)

	split := &AuditEvent{
		OccurredAt: occurred,
		ActorType:  ActorUser,
		ActorName:  "alice",
		ActorIP:    "198.51.100.9",
		Action:     "user.create",
		Outcome:    OutcomeSuccess,
	}
	merged := &AuditEvent{
		OccurredAt: occurred,
		ActorType:  ActorUser,
		ActorName:  "alice|198.51.100.9",
		ActorIP:    "",
		Action:     "user.create",
		Outcome:    OutcomeSuccess,
	}

	if mustHash(t, split) == mustHash(t, merged) {
		t.Error("Two different events hash identically: the canonical " +
			"rendering is ambiguous across field boundaries")
	}

	// An empty trailing field must still be distinguishable from an
	// absent one, which is the other half of the same property.
	withName := &AuditEvent{
		OccurredAt: occurred,
		ActorType:  ActorCLI,
		ActorName:  "ops",
		Action:     "user.delete",
		TargetType: "user",
		TargetName: "bob",
		Outcome:    OutcomeSuccess,
	}
	withoutName := &AuditEvent{
		OccurredAt: occurred,
		ActorType:  ActorCLI,
		ActorName:  "ops",
		Action:     "user.delete",
		TargetType: "userbob",
		Outcome:    OutcomeSuccess,
	}
	if mustHash(t, withName) == mustHash(t, withoutName) {
		t.Error("Adjacent fields can be shifted without changing the hash")
	}
}

// TestAuditChainCannotFork checks the unique index on prev_hash: no two
// events may claim the same predecessor, and only one event may be the
// genesis row. The single writer already makes a fork unlikely; the
// index is what makes it impossible.
func TestAuditChainCannotFork(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustRecord(t, store, newEvent(systemActor, "user.create", "user",
		int64Ptr(1), "alice", nil))
	mustRecord(t, store, newEvent(systemActor, "user.create", "user",
		int64Ptr(2), "bob", nil))

	var genesisHash, headPrev string
	if err := store.db.QueryRow(
		"SELECT hash FROM audit_events ORDER BY id LIMIT 1").
		Scan(&genesisHash); err != nil {
		t.Fatalf("Failed to read the genesis hash: %v", err)
	}
	if err := store.db.QueryRow(
		"SELECT prev_hash FROM audit_events ORDER BY id DESC LIMIT 1").
		Scan(&headPrev); err != nil {
		t.Fatalf("Failed to read the newest prev_hash: %v", err)
	}
	if headPrev != genesisHash {
		t.Fatalf("Expected the second row to chain onto the first")
	}

	insert := `INSERT INTO audit_events (
            occurred_at, actor_type, actor_name, action, outcome,
            prev_hash, hash
        ) VALUES (?, 'system', 'system', 'user.create', 'success', ?, ?)`
	now := time.Now().UTC().Format(auditTimeLayout)

	// A second row claiming the genesis row as its predecessor forks
	// the chain, and must be refused.
	if _, err := store.db.Exec(insert, now, genesisHash, "forked"); err == nil {
		t.Error("Expected a forked branch to be refused")
	}

	// So must a second genesis row, whose prev_hash is empty.
	if _, err := store.db.Exec(insert, now, "", "second-genesis"); err == nil {
		t.Error("Expected a second genesis row to be refused")
	}

	if _, firstBad, err := store.VerifyAuditChain(); err != nil || firstBad != 0 {
		t.Errorf("Chain should still verify: firstBad=%d err=%v", firstBad, err)
	}
}

// TestAuditChainGenesisAfterFullPurge checks that emptying the log
// leaves the next event free to become the genesis row again, which the
// unique index must permit now that the earlier empty prev_hash is
// gone.
//
// The retention purge no longer produces this shape: it deletes a
// prefix of ids and stops at the oldest row inside the window, so with
// every row outside the window it removes nothing. An emptied table is
// still reachable, by an operator clearing the log by hand or by a
// database restored from one, and the index has to admit a fresh
// genesis row when it happens, so the table is emptied here directly.
func TestAuditChainGenesisAfterFullPurge(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	old := time.Now().UTC().Add(-48 * time.Hour)
	for i := 0; i < 3; i++ {
		ev := newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i+1)), "alice", nil)
		ev.OccurredAt = old
		mustRecord(t, store, ev)
	}

	removed, err := store.PurgeAuditEvents(time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("PurgeAuditEvents failed: %v", err)
	}
	if removed != 0 {
		t.Fatalf("Expected the purge to keep a log with no row inside the "+
			"window, got %d removed", removed)
	}

	if _, err := store.db.Exec("DELETE FROM audit_events"); err != nil {
		t.Fatalf("Failed to empty the audit log: %v", err)
	}

	mustRecord(t, store, newEvent(systemActor, "user.create", "user",
		int64Ptr(9), "carol", nil))

	// The next event written is the new genesis row.
	var prevHash string
	if err := store.db.QueryRow(
		"SELECT prev_hash FROM audit_events ORDER BY id LIMIT 1").
		Scan(&prevHash); err != nil {
		t.Fatalf("Failed to read the new genesis row: %v", err)
	}
	if prevHash != "" {
		t.Errorf("Expected an empty prev_hash on the new genesis row, got %q",
			prevHash)
	}

	if _, firstBad, err := store.VerifyAuditChain(); err != nil || firstBad != 0 {
		t.Errorf("Chain should verify after a full purge: firstBad=%d err=%v",
			firstBad, err)
	}
}

// TestPurgeAuditEventsDeleteFailure covers the branch where the purge
// itself fails, which must leave the transaction rolled back and the
// error reported rather than a count of zero.
func TestPurgeAuditEventsDeleteFailure(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustRecord(t, store, newEvent(systemActor, "user.create", "user",
		int64Ptr(1), "alice", nil))
	if _, err := store.db.Exec("DROP TABLE audit_events"); err != nil {
		t.Fatalf("Failed to drop audit_events: %v", err)
	}

	removed, err := store.PurgeAuditEvents(time.Now().UTC())
	if err == nil {
		t.Fatal("Expected an error when the delete cannot run")
	}
	if !strings.Contains(err.Error(), "failed to purge audit events") {
		t.Errorf("Expected a purge error, got %v", err)
	}
	if removed != 0 {
		t.Errorf("Expected 0 rows removed, got %d", removed)
	}
}

// TestAuditHashCarriesFormatVersion checks that the digest covers the
// version of the encoding it was computed under, so that a later change
// to the rendering can be told from this one rather than reporting every
// older row as broken.
//
// It works on the unkeyed version 1 rendering because that one is a
// plain SHA-256 and so can be reconstructed here field by field. Nothing
// in this build computes it for a stored row any more; only
// verifyLegacyAuditChain does, when reporting on a log it is about to
// re-chain.
func TestAuditHashCarriesFormatVersion(t *testing.T) {
	ev := &AuditEvent{
		OccurredAt:  time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
		ActorType:   ActorSystem,
		ActorName:   "system",
		Action:      "user.create",
		Outcome:     OutcomeSuccess,
		HashVersion: 1,
	}
	before := auditHashV1(ev)

	if auditHashVersion != 2 {
		t.Fatalf("Expected the shipped version to be 2, got %d",
			auditHashVersion)
	}

	// The version label is the first field, so a row whose remaining
	// fields are identical still hashes differently under another
	// label. Rendering by hand with a different one stands in for that
	// future change.
	withPrefix := func(version string) string {
		var canonical strings.Builder
		for _, field := range []string{
			version, strconv.Itoa(ev.HashVersion), ev.PrevHash,
			ev.OccurredAt.UTC().Format(auditTimeLayout),
			string(ev.ActorType), "", ev.ActorName, ev.ActorIP,
			ev.Action, ev.TargetType, "", ev.TargetName,
			string(ev.Outcome), ev.Error, string(ev.Details),
		} {
			canonical.WriteString(strconv.Itoa(len(field)))
			canonical.WriteByte(':')
			canonical.WriteString(field)
		}
		sum := sha256.Sum256([]byte(canonical.String()))
		return hex.EncodeToString(sum[:])
	}

	if got := withPrefix(auditHashV1Label); got != before {
		t.Errorf("Expected the rendering to start with the version label:\n"+
			"auditHash gave %s\nreconstruction gave %s", before, got)
	}
	if withPrefix("v2") == before {
		t.Error("A different format version produced the same digest")
	}

	// The keyed entry point refuses the unkeyed rendering outright, so
	// no caller can be talked into computing a digest anyone able to
	// write auth.db could have produced.
	if _, err := auditHash(ev, AuditKeyForTesting()); !errors.Is(err,
		ErrAuditUnkeyedRow) {
		t.Errorf("Expected ErrAuditUnkeyedRow for version 1, got %v", err)
	}

	// A version this build has no rendering for is an error, not a
	// digest, so a verifier can tell it apart from a broken hash.
	ev.HashVersion = 3
	if _, err := auditHash(ev, AuditKeyForTesting()); !errors.Is(err, errUnknownAuditHashVersion) {
		t.Errorf("Expected errUnknownAuditHashVersion for version 3, got %v", err)
	}
	ev.HashVersion = 0
	if _, err := auditHash(ev, AuditKeyForTesting()); !errors.Is(err, errUnknownAuditHashVersion) {
		t.Errorf("Expected errUnknownAuditHashVersion for version 0, got %v", err)
	}
}

// TestRecordAuditStoresHashVersion checks that every recorded row
// carries the version it was hashed under, and that the version is read
// back with the row.
func TestRecordAuditStoresHashVersion(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	ev := newEvent(systemActor, "user.create", "user", int64Ptr(1), "alice", nil)
	mustRecord(t, store, ev)
	if ev.HashVersion != auditHashVersion {
		t.Errorf("Expected the event to carry hash version %d, got %d",
			auditHashVersion, ev.HashVersion)
	}

	var stored int
	if err := store.db.QueryRow(
		"SELECT hash_version FROM audit_events WHERE id = ?", ev.ID).
		Scan(&stored); err != nil {
		t.Fatalf("Failed to read hash_version: %v", err)
	}
	if stored != auditHashVersion {
		t.Errorf("Expected hash_version %d on disk, got %d", auditHashVersion, stored)
	}

	events, _, err := store.ListAuditEvents(AuditFilter{})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if len(events) != 1 || events[0].HashVersion != auditHashVersion {
		t.Errorf("Expected the listed event to carry hash version %d, got %+v",
			auditHashVersion, events)
	}
}

// TestVerifyAuditChainReportsUnknownHashVersion checks that a row
// written under a format this build does not know is reported as
// tampering, naming the row and the version.
func TestVerifyAuditChainReportsUnknownHashVersion(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	for i := 1; i <= 2; i++ {
		mustRecord(t, store, newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i)), "alice", nil))
	}

	if _, err := store.db.Exec("DROP TRIGGER " + auditNoUpdateTrigger); err != nil {
		t.Fatalf("Failed to drop trigger: %v", err)
	}
	if _, err := store.db.Exec(
		"UPDATE audit_events SET hash_version = 99 WHERE id = 2"); err != nil {
		t.Fatalf("Failed to rewrite hash_version: %v", err)
	}
	if _, err := store.db.Exec(auditSchemaDDL); err != nil {
		t.Fatalf("Failed to re-create trigger: %v", err)
	}

	_, firstBad, err := store.VerifyAuditChain()
	if err == nil {
		t.Fatal("Expected an unknown hash version to be reported")
	}
	if firstBad != 2 {
		t.Errorf("Expected firstBad 2, got %d", firstBad)
	}
	for _, want := range []string{"row 2", "hash version 99"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expected the error to mention %q, got %v", want, err)
		}
	}
	// A row relabelled out of reach of the verifier is tampering: only
	// something with write access to the file produces it, and the
	// sentinel is what maps it to the tampered exit status.
	if !errors.Is(err, ErrAuditChainBroken) {
		t.Errorf("Expected an unknown version to be reported as tampering: %v",
			err)
	}
}

// TestReopenRestoresAuditSchemaObjects goes through the real open path
// for the case the version gate missed: a database already stamped with
// the current schema version but lacking the anti-fork index and the
// append-only trigger, which is what a store created between the commit
// that set the version and the commit that added the index looks like,
// and also what DROP INDEX or DROP TRIGGER leaves behind. Reopening
// must put both back, and the chain must then be protected again.
func TestReopenRestoresAuditSchemaObjects(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-audit-reopen-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	mustRecord(t, store, newEvent(systemActor, "user.create", "user",
		int64Ptr(1), "alice", nil))
	for _, stmt := range []string{
		"DROP INDEX " + auditChainIndexName,
		"DROP TRIGGER " + auditNoUpdateTrigger,
	} {
		if _, err := store.db.Exec(stmt); err != nil {
			t.Fatalf("%s failed: %v", stmt, err)
		}
	}
	// The version is already current, so no migration will run.
	var version int
	if err := store.db.QueryRow(
		"SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("Failed to read schema version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("Expected the store to be at version %d, got %d", schemaVersion, version)
	}
	if _, _, err := store.VerifyAuditChain(); err == nil {
		t.Fatal("Expected verification to refuse a log without its index and trigger")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	reopened, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to reopen auth store: %v", err)
	}
	defer reopened.Close()

	for _, want := range []struct{ kind, name string }{
		{"index", auditChainIndexName},
		{"trigger", auditNoUpdateTrigger},
	} {
		var count int
		if err := reopened.db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?",
			want.kind, want.name).Scan(&count); err != nil {
			t.Fatalf("Failed to look for %s %s: %v", want.kind, want.name, err)
		}
		if count != 1 {
			t.Errorf("%s %s missing after reopen", want.kind, want.name)
		}
	}

	// A second genesis row, which forks the chain, must be refused.
	now := time.Now().UTC().Format(auditTimeLayout)
	if _, err := reopened.db.Exec(`INSERT INTO audit_events (
            occurred_at, actor_type, actor_name, action, outcome,
            prev_hash, hash
        ) VALUES (?, 'system', 'system', 'user.create', 'success', '', 'fork')`,
		now); err == nil {
		t.Error("A duplicate prev_hash was accepted after reopen")
	}
	// And a row must not be rewritable in place.
	if _, err := reopened.db.Exec(
		"UPDATE audit_events SET actor_name = 'mallory' WHERE id = 1"); err == nil {
		t.Error("An UPDATE was accepted after reopen")
	}

	if _, firstBad, err := reopened.VerifyAuditChain(); err != nil || firstBad != 0 {
		t.Errorf("Expected the restored log to verify, got firstBad %d, %v", firstBad, err)
	}
}

// TestReopenRefusesForkedChain checks the other half of the same gate:
// a database at the current version that already holds a forked chain
// cannot be given the index, and NewAuthStore must say so rather than
// open it unprotected.
func TestReopenRefusesForkedChain(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-audit-forked-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	if _, err := store.db.Exec("DROP INDEX " + auditChainIndexName); err != nil {
		t.Fatalf("Failed to drop the chain index: %v", err)
	}
	now := time.Now().UTC().Format(auditTimeLayout)
	for _, hash := range []string{"branch-a", "branch-b"} {
		if _, err := store.db.Exec(`INSERT INTO audit_events (
                occurred_at, actor_type, actor_name, action, outcome,
                prev_hash, hash
            ) VALUES (?, 'system', 'system', 'user.create', 'success', '', ?)`,
			now, hash); err != nil {
			t.Fatalf("Failed to seed a forked row: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	reopened, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err == nil {
		reopened.Close()
		t.Fatal("Expected NewAuthStore to refuse a forked chain")
	}
	for _, want := range []string{auditChainIndexName, "forked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expected the error to mention %q, got %v", want, err)
		}
	}
}

// TestVerifyAuditChainRequiresSchemaObjects checks that verification
// looks at the schema and not only at the rows: a log whose index or
// trigger has been dropped, or whose index has been swapped for one
// that is not unique, is reported even though every row still hashes.
func TestVerifyAuditChainRequiresSchemaObjects(t *testing.T) {
	cases := []struct {
		name    string
		tamper  string
		wantErr string
	}{
		{"index dropped", "DROP INDEX " + auditChainIndexName,
			"unique index " + auditChainIndexName + " is missing"},
		{"index not unique", "DROP INDEX " + auditChainIndexName + "; " +
			"CREATE INDEX " + auditChainIndexName + " ON audit_events(prev_hash)",
			"is not unique"},
		{"trigger dropped", "DROP TRIGGER " + auditNoUpdateTrigger,
			"trigger " + auditNoUpdateTrigger + " is missing"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := createTestAuthStoreForAudit(t)
			defer cleanup()

			mustRecord(t, store, newEvent(systemActor, "user.create", "user",
				int64Ptr(1), "alice", nil))
			if _, err := store.db.Exec(tc.tamper); err != nil {
				t.Fatalf("Failed to tamper with the schema: %v", err)
			}

			rows, firstBad, err := store.VerifyAuditChain()
			if err == nil {
				t.Fatal("Expected verification to fail")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Expected the error to contain %q, got %v", tc.wantErr, err)
			}
			if rows != 0 || firstBad != 0 {
				t.Errorf("Expected no rows examined, got rows %d firstBad %d", rows, firstBad)
			}
		})
	}
}

// TestVerifyAuditSchemaQueryError covers the schema lookups failing
// outright, which must be returned rather than read as absence.
func TestVerifyAuditSchemaQueryError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open in-memory database: %v", err)
	}
	db.Close()

	store := &AuthStore{db: db}
	if err := store.verifyAuditSchema(); err == nil ||
		!strings.Contains(err.Error(), "failed to look for index") {
		t.Errorf("Expected the index lookup failure to be returned, got %v", err)
	}
}

// TestAuditSchemaV5ToV6Migration goes through the real open path with
// a database a v5 build left behind: audit_events without hash_version
// and the version stamped 5. Reopening must add the column, default the
// existing rows to version 1, and the chain must still verify.
func TestAuditSchemaV5ToV6Migration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-audit-v5-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	// The rows must be genuine version 1 rows: a v5 build predates the
	// keyed chain, so a database it left behind cannot hold a row the
	// migration would then default to version 1 whilst its hash says
	// otherwise.
	for i := 1; i <= 3; i++ {
		insertAuditRowAsV1(t, store, newEvent(systemActor, "user.create",
			"user", int64Ptr(int64(i)), "alice", nil))
	}
	if _, err := store.db.Exec(`
        ALTER TABLE audit_events DROP COLUMN hash_version;
        DELETE FROM schema_version;
        INSERT INTO schema_version (version) VALUES (5);`); err != nil {
		t.Fatalf("Failed to prepare a v5 database: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	// The migration brings the schema forward and defaults the existing
	// rows to hash version 1, which this build will not open: the
	// unkeyed rows have to be re-hashed under the key first, and that
	// is an operator's decision because it attests them.
	if refused, err := NewAuthStore(tmpDir, 0, 0,
		AuditKeyForTesting()); !errors.Is(err, ErrAuditUnkeyedRow) {
		if refused != nil {
			refused.Close()
		}
		t.Fatalf("Expected the upgrade to stop for the unkeyed rows, got %v",
			err)
	}
	if _, err := RechainAuditLog(tmpDir, AuditKeyForTesting(), systemActor,
		alwaysConfirmRechain); err != nil {
		t.Fatalf("Failed to re-chain the log: %v", err)
	}

	reopened, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to reopen a v5 auth store: %v", err)
	}
	defer reopened.Close()

	var version int
	if err := reopened.db.QueryRow(
		"SELECT MAX(version) FROM schema_version").Scan(&version); err != nil {
		t.Fatalf("Failed to read schema version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("Expected schema version %d, got %d", schemaVersion, version)
	}

	// The rows the migration defaulted to version 1 have all been
	// re-hashed under the key, so none is left claiming the unkeyed
	// rendering.
	var versions int
	if err := reopened.db.QueryRow(
		"SELECT COUNT(*) FROM audit_events WHERE hash_version = 1").
		Scan(&versions); err != nil {
		t.Fatalf("Failed to read hash_version: %v", err)
	}
	if versions != 0 {
		t.Errorf("Expected no unkeyed rows after the re-chain, got %d",
			versions)
	}

	// Three migrated rows plus the audit.rechain event the re-chain
	// records for itself.
	rows, firstBad, err := reopened.VerifyAuditChain()
	if err != nil || firstBad != 0 || rows != 4 {
		t.Errorf("Expected the migrated log to verify 4 rows, got rows %d "+
			"firstBad %d err %v", rows, firstBad, err)
	}

	// Re-running the migration on a database that already has the
	// column must be a no-op rather than a duplicate-column error.
	if err := reopened.migrateV5ToV6(); err != nil {
		t.Errorf("Expected a repeated v5 to v6 migration to succeed, got %v", err)
	}
}

// TestNewAuthStoreReportsV6MigrationFailure checks that a v5 database
// whose migration to v6 fails is refused at open, naming the step.
func TestNewAuthStoreReportsV6MigrationFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-audit-v5-fail-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	if _, err := store.db.Exec(`
        DROP TABLE schema_version;
        CREATE TABLE schema_version (
            version INTEGER PRIMARY KEY CHECK (version < 6)
        );
        INSERT INTO schema_version (version) VALUES (5);`); err != nil {
		t.Fatalf("Failed to prepare a v5 database: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	reopened, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err == nil {
		reopened.Close()
		t.Fatal("Expected NewAuthStore to fail when the migration fails")
	}
	if !strings.Contains(err.Error(), "v5 to v6") {
		t.Errorf("Unexpected error text: %v", err)
	}
}

// TestMigrateV5ToV6Errors covers the migration's failure paths: the
// column lookup, the ALTER, the audit schema and the version stamp.
func TestMigrateV5ToV6Errors(t *testing.T) {
	t.Run("column cannot be added", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		if _, err := store.db.Exec(
			"ALTER TABLE audit_events DROP COLUMN hash_version"); err != nil {
			t.Fatalf("Failed to drop hash_version: %v", err)
		}
		// A read-only connection lets the column lookup succeed and
		// refuses the ALTER that follows it.
		store.db.SetMaxOpenConns(1)
		if _, err := store.db.Exec("PRAGMA query_only = 1"); err != nil {
			t.Fatalf("Failed to make the connection read-only: %v", err)
		}
		err := store.migrateV5ToV6()
		if err == nil || !strings.Contains(err.Error(), "hash_version") {
			t.Errorf("Expected the ALTER failure to be reported, got %v", err)
		}
	})

	t.Run("forked chain refuses the index", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		if _, err := store.db.Exec("DROP INDEX " + auditChainIndexName); err != nil {
			t.Fatalf("Failed to drop the chain index: %v", err)
		}
		now := time.Now().UTC().Format(auditTimeLayout)
		for _, hash := range []string{"branch-a", "branch-b"} {
			if _, err := store.db.Exec(`INSERT INTO audit_events (
                    occurred_at, actor_type, actor_name, action, outcome,
                    prev_hash, hash
                ) VALUES (?, 'system', 'system', 'user.create', 'success', '', ?)`,
				now, hash); err != nil {
				t.Fatalf("Failed to seed a forked row: %v", err)
			}
		}
		err := store.migrateV5ToV6()
		if err == nil || !strings.Contains(err.Error(), "forked") {
			t.Errorf("Expected the forked chain to be reported, got %v", err)
		}
	})

	t.Run("audit table missing is created", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		if _, err := store.db.Exec("DROP TABLE audit_events"); err != nil {
			t.Fatalf("Failed to drop audit_events: %v", err)
		}
		if err := store.migrateV5ToV6(); err != nil {
			t.Fatalf("Expected the migration to recreate the table, got %v", err)
		}
		var present int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('audit_events')
             WHERE name = 'hash_version'`).Scan(&present); err != nil {
			t.Fatalf("Failed to inspect audit_events: %v", err)
		}
		if present != 1 {
			t.Error("Expected the recreated table to carry hash_version")
		}
	})

	t.Run("version set failure", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		if _, err := store.db.Exec(`
            DROP TABLE schema_version;
            CREATE TABLE schema_version (
                version INTEGER PRIMARY KEY CHECK (version < 6)
            );`); err != nil {
			t.Fatalf("Failed to rebuild schema_version: %v", err)
		}
		if err := store.migrateV5ToV6(); err == nil {
			t.Error("Expected an error when the version cannot be set")
		}
	})

	t.Run("closed database", func(t *testing.T) {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("Failed to open in-memory database: %v", err)
		}
		db.Close()
		store := &AuthStore{db: db}
		if err := store.migrateV5ToV6(); err == nil {
			t.Error("Expected an error on a closed database")
		}
	})
}

// TestMigrateV4ToV5NamesTheForkedIndex checks that an operator whose
// pre-merge database holds a forked chain is told which index failed
// and how to find the duplicates, rather than shown a bare constraint
// violation.
func TestMigrateV4ToV5NamesTheForkedIndex(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	// Reproduce a database written before the unique index existed, and
	// fork its chain.
	if _, err := store.db.Exec("DROP INDEX idx_audit_prev_hash"); err != nil {
		t.Fatalf("Failed to drop the chain index: %v", err)
	}
	insert := `INSERT INTO audit_events (
            occurred_at, actor_type, actor_name, action, outcome,
            prev_hash, hash
        ) VALUES (?, 'system', 'system', 'user.create', 'success', '', ?)`
	now := time.Now().UTC().Format(auditTimeLayout)
	for _, hash := range []string{"branch-a", "branch-b"} {
		if _, err := store.db.Exec(insert, now, hash); err != nil {
			t.Fatalf("Failed to seed a forked row: %v", err)
		}
	}

	err := store.migrateV4ToV5()
	if err == nil {
		t.Fatal("Expected the migration to refuse a forked chain")
	}
	for _, want := range []string{"idx_audit_prev_hash", "forked",
		"GROUP BY prev_hash"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expected the error to mention %q, got %v", want, err)
		}
	}
}
