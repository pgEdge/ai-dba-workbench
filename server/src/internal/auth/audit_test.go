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
	"encoding/json"
	"log"
	"os"
	"strings"
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

	store, err := NewAuthStore(tmpDir, 0, 0)
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

// =============================================================================
// Schema
// =============================================================================

func TestAuditSchemaV4Migration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-audit-migrate-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}

	// Pretend the database was created by a v3 build.
	if _, err := store.db.Exec("DROP TABLE audit_events"); err != nil {
		t.Fatalf("Failed to drop audit_events: %v", err)
	}
	if _, err := store.db.Exec("DELETE FROM schema_version"); err != nil {
		t.Fatalf("Failed to clear schema_version: %v", err)
	}
	if _, err := store.db.Exec(
		"INSERT INTO schema_version (version) VALUES (3)"); err != nil {
		t.Fatalf("Failed to set schema_version: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	reopened, err := NewAuthStore(tmpDir, 0, 0)
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
	if version != 4 {
		t.Errorf("Expected schema version 4, got %d", version)
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
	if stored != backdated.UTC().Format(time.RFC3339Nano) {
		t.Errorf("Expected stored timestamp %q, got %q",
			backdated.UTC().Format(time.RFC3339Nano), stored)
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
	forged := auditHash(&ev)

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

	if auditHash(&a) == auditHash(&b) {
		t.Error("Expected differing details to produce differing hashes")
	}

	c := base
	c.Details = json.RawMessage(`{"after":{"enabled":true}}`)
	if auditHash(&a) != auditHash(&c) {
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
	if rows != 2 || firstBad != 0 {
		t.Errorf("Expected 2 verified rows, got rows=%d firstBad=%d",
			rows, firstBad)
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

func TestMigrateV3ToV4Errors(t *testing.T) {
	t.Run("DDL failure", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		store.db.Close()
		if err := store.migrateV3ToV4(); err == nil {
			t.Error("Expected an error from a closed database")
		}
	})

	t.Run("version clear failure", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		if _, err := store.db.Exec("DROP TABLE schema_version"); err != nil {
			t.Fatalf("Failed to drop schema_version: %v", err)
		}
		if err := store.migrateV3ToV4(); err == nil {
			t.Error("Expected an error when schema_version is missing")
		}
	})

	t.Run("version set failure", func(t *testing.T) {
		store, cleanup := createTestAuthStoreForAudit(t)
		defer cleanup()

		// A schema_version table that refuses version 4 makes the
		// final INSERT fail while the DELETE before it succeeds.
		if _, err := store.db.Exec(`
            DROP TABLE schema_version;
            CREATE TABLE schema_version (
                version INTEGER PRIMARY KEY CHECK (version < 4)
            );`); err != nil {
			t.Fatalf("Failed to rebuild schema_version: %v", err)
		}
		if err := store.migrateV3ToV4(); err == nil {
			t.Error("Expected an error when the version cannot be set")
		}
	})
}

func TestNewAuthStoreReportsV4MigrationFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-audit-migrate-fail-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}

	// A v3 database whose schema_version table cannot hold version 4.
	if _, err := store.db.Exec(`
        DROP TABLE audit_events;
        DROP TABLE schema_version;
        CREATE TABLE schema_version (
            version INTEGER PRIMARY KEY CHECK (version < 4)
        );
        INSERT INTO schema_version (version) VALUES (3);`); err != nil {
		t.Fatalf("Failed to prepare a v3 database: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	reopened, err := NewAuthStore(tmpDir, 0, 0)
	if err == nil {
		reopened.Close()
		t.Fatal("Expected NewAuthStore to fail when the migration fails")
	}
	if !strings.Contains(err.Error(), "v3 to v4") {
		t.Errorf("Unexpected error text: %v", err)
	}
}
