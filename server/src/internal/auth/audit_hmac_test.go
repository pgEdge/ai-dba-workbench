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
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// insertAuditRowAsV1 appends an event hashed under version 1, the way a
// build predating the keyed chain wrote every row. recordAudit cannot
// be asked to do this, because it stamps auditHashVersion on everything
// it writes, and that is deliberate: the only way to produce a version
// 1 row now is to go round it, which is what a database left behind by
// an older build amounts to.
//
// Nothing this build writes produces such a row, and a database
// holding one will not open, so every caller here is standing in for
// either a database written by an older build or someone editing
// auth.db.
func insertAuditRowAsV1(t *testing.T, s *AuthStore, ev *AuditEvent) {
	t.Helper()

	var prevHash string
	err := s.db.QueryRow(
		"SELECT hash FROM audit_events ORDER BY id DESC LIMIT 1").Scan(&prevHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Failed to read the previous audit hash: %v", err)
	}

	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	if ev.Outcome == "" {
		ev.Outcome = OutcomeSuccess
	}
	ev.PrevHash = prevHash
	ev.HashVersion = 1
	ev.Hash = auditHashV1(ev)

	result, err := s.db.Exec(`
        INSERT INTO audit_events (
            occurred_at, actor_type, actor_id, actor_name, actor_ip,
            action, target_type, target_id, target_name, outcome,
            error, details, prev_hash, hash, hash_version
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.OccurredAt.UTC().Format(auditTimeLayout),
		string(ev.ActorType), ev.ActorID, ev.ActorName,
		nullableText(ev.ActorIP), ev.Action, nullableText(ev.TargetType),
		ev.TargetID, nullableText(ev.TargetName), string(ev.Outcome),
		nullableText(ev.Error), nullableText(string(ev.Details)),
		ev.PrevHash, ev.Hash, ev.HashVersion)
	if err != nil {
		t.Fatalf("Failed to insert a version 1 audit row: %v", err)
	}
	if id, err := result.LastInsertId(); err == nil {
		ev.ID = id
	}
}

// distinctPrevHash renders id as a 64 character hexadecimal string, so
// that every row a test plants below the genuine ones carries a
// prev_hash of its own; the chain index admits one row per predecessor,
// and the value is otherwise arbitrary.
func distinctPrevHash(id int64) string {
	var digest [32]byte
	digest[30] = byte(id >> 8)
	digest[31] = byte(id)
	return hex.EncodeToString(digest[:])
}

// insertAuditRowAsV1AtID writes an unkeyed row at an id of the caller's
// choosing, which is what someone with write access to auth.db can do
// and what the id-prefix reasoning VULN-307 retired assumed away:
// AUTOINCREMENT governs the ids SQLite assigns, not the ones an INSERT
// may name, so a row below every genuine row is a plain INSERT. Its
// prev_hash is arbitrary but must be unique, because the chain index
// admits one row per predecessor.
func insertAuditRowAsV1AtID(t *testing.T, s *AuthStore, ev *AuditEvent,
	id int64) {

	t.Helper()

	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	if ev.Outcome == "" {
		ev.Outcome = OutcomeSuccess
	}
	ev.ID = id
	ev.PrevHash = distinctPrevHash(id)
	ev.HashVersion = 1
	ev.Hash = auditHashV1(ev)

	if _, err := s.db.Exec(`
        INSERT INTO audit_events (
            id, occurred_at, actor_type, actor_id, actor_name, actor_ip,
            action, target_type, target_id, target_name, outcome,
            error, details, prev_hash, hash, hash_version
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, ev.OccurredAt.UTC().Format(auditTimeLayout),
		string(ev.ActorType), ev.ActorID, ev.ActorName,
		nullableText(ev.ActorIP), ev.Action, nullableText(ev.TargetType),
		ev.TargetID, nullableText(ev.TargetName), string(ev.Outcome),
		nullableText(ev.Error), nullableText(string(ev.Details)),
		ev.PrevHash, ev.Hash, ev.HashVersion); err != nil {
		t.Fatalf("Failed to insert a version 1 audit row at id %d: %v", id,
			err)
	}
}

// sampleAuditEvent is a fully populated event, so that every field the
// canonical rendering covers actually carries a value.
func sampleAuditEvent() *AuditEvent {
	id := int64(42)
	return &AuditEvent{
		OccurredAt:  time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
		ActorType:   ActorUser,
		ActorID:     &id,
		ActorName:   "alice",
		ActorIP:     "192.0.2.10",
		Action:      "user.create",
		TargetType:  "user",
		TargetID:    &id,
		TargetName:  "bob",
		Outcome:     OutcomeSuccess,
		Details:     []byte(`{"note":"test"}`),
		HashVersion: 2,
	}
}

// inheritV1Rows writes n unkeyed rows, which is what a database
// upgraded from a build predating the keyed chain holds before the
// re-chain runs. The store it is called on stays usable for the length
// of the test; a store reopened on the same directory would refuse.
//
// It goes through the same exported seeding helper the cmd/mcp-server
// tests use, so that there is one description of what an inherited log
// looks like rather than two that can drift apart.
func inheritV1Rows(t *testing.T, s *AuthStore, n int) {
	t.Helper()

	if err := SeedUnkeyedAuditLogForTesting(s, n,
		time.Now().UTC().Add(-time.Hour), 0); err != nil {
		t.Fatalf("Failed to write the unkeyed rows: %v", err)
	}
}

// rewriteRowAsV1 does what someone with write access to auth.db can do
// without the key: it takes an existing row, relabels it as version 1
// and recomputes the unkeyed digest over it, so the row is internally
// consistent under the rendering it now claims. The append-only trigger
// refuses an UPDATE, so this goes through a DELETE and a re-INSERT.
func rewriteRowAsV1(t *testing.T, s *AuthStore, id int64) {
	t.Helper()

	row := s.db.QueryRow(auditSelectByID, id)
	ev, err := scanAuditEvent(row.Scan)
	if err != nil {
		t.Fatalf("Failed to read audit row %d: %v", id, err)
	}

	ev.HashVersion = 1
	ev.Hash = auditHashV1(&ev)

	if _, err := s.db.Exec("DELETE FROM audit_events WHERE id = ?", id); err != nil {
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
		nullableText(ev.ActorIP), ev.Action, nullableText(ev.TargetType),
		ev.TargetID, nullableText(ev.TargetName), string(ev.Outcome),
		nullableText(ev.Error), nullableText(string(ev.Details)),
		ev.PrevHash, ev.Hash, ev.HashVersion); err != nil {
		t.Fatalf("Failed to rewrite audit row %d as version 1: %v", id, err)
	}
}

// TestAuditHashV2DiffersFromV1 checks that the keyed rendering is a
// different digest over the same fields, which is what stops a row
// being moved between versions unchallenged.
func TestAuditHashV2DiffersFromV1(t *testing.T) {
	ev := sampleAuditEvent()

	v2, err := auditHashV2(ev, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("auditHashV2 failed: %v", err)
	}

	ev.HashVersion = 1
	v1 := auditHashV1(ev)

	if v1 == v2 {
		t.Error("Version 1 and version 2 produced the same digest")
	}
	if len(v2) != 64 {
		t.Errorf("Expected a 64-character hex digest, got %q", v2)
	}

	// The same fields under a different key must give a different
	// digest, or the key is not doing anything.
	other, err := auditHashV2(sampleAuditEvent(), []byte("a different key"))
	if err != nil {
		t.Fatalf("auditHashV2 failed under the second key: %v", err)
	}
	if other == v2 {
		t.Error("Two different keys produced the same digest")
	}

	// The same fields under the same key must give the same digest,
	// since a chain that cannot be recomputed cannot be verified.
	again, err := auditHashV2(sampleAuditEvent(), AuditKeyForTesting())
	if err != nil {
		t.Fatalf("auditHashV2 failed on the repeat: %v", err)
	}
	if again != v2 {
		t.Error("The same event under the same key produced two digests")
	}
}

// TestAuditHashV2RequiresKey checks that the keyed rendering refuses to
// hash under an empty key rather than quietly producing a digest anyone
// could reproduce.
func TestAuditHashV2RequiresKey(t *testing.T) {
	for _, key := range [][]byte{nil, {}} {
		if _, err := auditHashV2(sampleAuditEvent(), key); !errors.Is(err, errNoAuditKey) {
			t.Errorf("Expected errNoAuditKey for key %v, got %v", key, err)
		}
		if _, err := auditHash(sampleAuditEvent(), key); !errors.Is(err, errNoAuditKey) {
			t.Errorf("Expected auditHash to refuse key %v, got %v", key, err)
		}
	}
}

// TestRecordAuditWritesVersion2 checks that new rows carry the keyed
// version, since that is the whole point of the bump.
func TestRecordAuditWritesVersion2(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	ev := newEvent(systemActor, "user.create", "user", nil, "alice", nil)
	mustRecord(t, store, ev)

	if ev.HashVersion != 2 {
		t.Errorf("Expected the recorded row to be version 2, got %d",
			ev.HashVersion)
	}

	var stored int
	if err := store.db.QueryRow(
		"SELECT hash_version FROM audit_events WHERE id = ?", ev.ID).
		Scan(&stored); err != nil {
		t.Fatalf("Failed to read hash_version: %v", err)
	}
	if stored != 2 {
		t.Errorf("Expected hash_version 2 on disk, got %d", stored)
	}

	want, err := auditHashV2(ev, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("auditHashV2 failed: %v", err)
	}
	if ev.Hash != want {
		t.Error("The recorded hash is not the keyed digest of the row")
	}
}

// TestFreshInstallAdmitsNoUnkeyedRow checks the case that matters most,
// because it is the one every installation is in once it has been
// re-chained: an unkeyed row is refused wherever it appears, so the
// downgrade this whole mechanism exists to stop cannot even begin.
func TestFreshInstallAdmitsNoUnkeyedRow(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	insertAuditRowAsV1(t, store, newEvent(systemActor, "user.create", "user",
		int64Ptr(1), "mallory", nil))

	_, firstBad, err := store.VerifyAuditChain()
	if !errors.Is(err, ErrAuditUnkeyedRow) {
		t.Fatalf("Expected an unkeyed row to be refused, got %v", err)
	}
	if firstBad != 1 {
		t.Errorf("Expected row 1 to be reported, got %d", firstBad)
	}
	// The unkeyed digest is refused rather than recomputed, so the
	// failure must not read as the wrong server secret.
	if errors.Is(err, ErrAuditKeyMismatch) {
		t.Error("An unkeyed row must not be reported as a key mismatch")
	}
	if !errors.Is(err, ErrAuditChainBroken) {
		t.Error("An unkeyed row must also carry the tampering sentinel, " +
			"so that a monitor keyed on it sees this")
	}
}

// TestAuditCanonicalBindsHashVersion checks that the version number is
// inside the digest, not merely alongside it. Without this, the only
// thing separating the renderings is a label the verifier picks from
// the row it is checking, so a row could name the algorithm it wanted
// to be checked with.
func TestAuditCanonicalBindsHashVersion(t *testing.T) {
	ev := sampleAuditEvent()
	ev.HashVersion = 1
	asOne := auditHashV1(ev)

	ev.HashVersion = 2
	asTwo := auditHashV1(ev)

	if asOne == asTwo {
		t.Error("The unkeyed digest ignores hash_version, so a version 2 " +
			"row could be relabelled as version 1 and recomputed")
	}

	// The rendering must open with the label and then the number, so
	// that neither can be moved without disturbing the digest.
	wantPrefix := string(canonicalFields(auditHashV1Label, "2"))
	if !strings.HasPrefix(string(auditCanonical(auditHashV1Label, ev)),
		wantPrefix) {
		t.Errorf("Expected the canonical rendering to open with %q", wantPrefix)
	}
}

// TestVerifyRejectsVersionDowngradeAlongChain checks the ordering rule
// on its own: once a keyed row has been seen, no later row may claim a
// lower version. It is reported in its own
// words so that an operator can tell a downgrade from a hash that
// simply does not match.
func TestVerifyRejectsVersionDowngradeAlongChain(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	for i := 1; i <= 3; i++ {
		mustRecord(t, store, newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i)), "alice", nil))
	}

	rewriteRowAsV1(t, store, 3)

	_, firstBad, err := store.VerifyAuditChain()
	if !errors.Is(err, ErrAuditChainDowngraded) {
		t.Fatalf("Expected a version downgrade to be refused, got %v", err)
	}
	if firstBad != 3 {
		t.Errorf("Expected row 3 to be reported, got %d", firstBad)
	}
	for _, want := range []string{"downgraded at row 3", "version 1 after version 2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expected the error to say %q, got %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "chain broken") {
		t.Errorf("A downgrade must not read as a broken chain: %v", err)
	}
}

// TestVerifyDetectsTamperedVersion2Row checks that rewriting a keyed row
// is caught. The append-only trigger refuses an UPDATE, so the
// tampering goes through a DELETE and re-INSERT, which is what someone
// with write access to the file would do.
func TestVerifyDetectsTamperedVersion2Row(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	for i := 1; i <= 3; i++ {
		mustRecord(t, store, newEvent(systemActor, "user.create", "user",
			int64Ptr(int64(i)), "alice", nil))
	}

	// Rewrite the middle row's target name, keeping every other column
	// and the hash exactly as they were. The append-only trigger
	// refuses an UPDATE, so this goes through a DELETE and a re-INSERT,
	// which is what someone with write access to the file would do.
	if _, err := store.db.Exec(
		"UPDATE audit_events SET target_name = 'mallory' WHERE id = 2"); err == nil {
		t.Fatal("Expected the append-only trigger to refuse the UPDATE")
	}

	var occurredAt, actorType, actorName, action, outcome string
	var prevHash, hash string
	var targetID sql.NullInt64
	if err := store.db.QueryRow(`
        SELECT occurred_at, actor_type, actor_name, action, outcome,
               target_id, prev_hash, hash
        FROM audit_events WHERE id = 2`).Scan(&occurredAt, &actorType,
		&actorName, &action, &outcome, &targetID, &prevHash,
		&hash); err != nil {
		t.Fatalf("Failed to read the row to rewrite: %v", err)
	}

	if _, err := store.db.Exec("DELETE FROM audit_events WHERE id = 2"); err != nil {
		t.Fatalf("Failed to remove the row: %v", err)
	}
	if _, err := store.db.Exec(`
        INSERT INTO audit_events (
            id, occurred_at, actor_type, actor_name, action, target_type,
            target_id, target_name, outcome, prev_hash, hash, hash_version
        ) VALUES (2, ?, ?, ?, ?, 'user', ?, 'mallory', ?, ?, ?, 2)`,
		occurredAt, actorType, actorName, action, targetID, outcome,
		prevHash, hash); err != nil {
		t.Fatalf("Failed to rewrite the row: %v", err)
	}

	_, firstBad, err := store.VerifyAuditChain()
	if !errors.Is(err, ErrAuditChainBroken) {
		t.Fatalf("Expected the rewritten row to read as a broken chain, "+
			"got %v", err)
	}
	if firstBad != 2 {
		t.Errorf("Expected row 2 to be reported, got %d", firstBad)
	}
	// Row 1 verified under the key before row 2 failed, so this cannot
	// be the wrong secret and must not be reported as one.
	if errors.Is(err, ErrAuditKeyMismatch) {
		t.Errorf("A rewritten row must not read as a key mismatch: %v", err)
	}
}

// TestVerifyFailsUnderTheWrongKey checks that a store opened with a key
// other than the one the rows were written under reports them as
// unverifiable, which is the property the HMAC buys: the digest cannot
// be recomputed without the server secret.
func TestVerifyFailsUnderTheWrongKey(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "auth-audit-key-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	mustRecord(t, store, newEvent(systemActor, "user.create", "user", nil,
		"alice", nil))
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	wrong, err := NewAuthStore(tmpDir, 0, 0, DeriveAuditKey("some other secret"))
	if err != nil {
		t.Fatalf("Failed to reopen the auth store: %v", err)
	}
	defer wrong.Close()

	_, _, err = wrong.VerifyAuditChain()
	if !errors.Is(err, ErrAuditKeyMismatch) {
		t.Fatalf("Expected a key mismatch under the wrong key, got %v", err)
	}
	// It must say so in those terms. An operator who had rotated the
	// secret and was told the log had been tampered with would either
	// waste a day on it or, having been told once too often, stop
	// believing the message when it was real.
	if !errors.Is(err, ErrAuditKeyMismatch) {
		t.Errorf("Expected a key mismatch rather than tampering, got %v", err)
	}
	if errors.Is(err, ErrAuditChainBroken) {
		t.Errorf("The wrong key must not read as a broken chain: %v", err)
	}
	if !strings.Contains(err.Error(), "server secret") {
		t.Errorf("Expected the error to point at the secret file, got %v", err)
	}

	right, err := NewAuthStore(tmpDir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to reopen the auth store: %v", err)
	}
	defer right.Close()

	if _, _, err := right.VerifyAuditChain(); err != nil {
		t.Errorf("Expected verification to pass under the right key, got %v", err)
	}
}

// TestVerifyReportsAMissingKeyAsSuch checks that a store with no key
// reports the keyed rows as unverifiable by this store rather than as
// tampered, so that an operator is not sent hunting for an attack that
// has not happened. NewAuthStore refuses to build such a store, so the
// state is reached the only way it can be.
func TestVerifyReportsAMissingKeyAsSuch(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustRecord(t, store, newEvent(systemActor, "user.create", "user", nil,
		"alice", nil))

	store.auditKey = nil
	_, firstBad, err := store.VerifyAuditChain()
	if !errors.Is(err, ErrAuditChainBroken) {
		t.Fatalf("Expected a store with no key to report the row as "+
			"unverifiable, got %v", err)
	}
	if !errors.Is(err, errNoAuditKey) {
		t.Errorf("Expected the missing key to be named, got %v", err)
	}
	if !strings.Contains(err.Error(), "cannot be verified by this store") {
		t.Errorf("Expected the error to say the store cannot verify it, got %v", err)
	}
	if firstBad != 1 {
		t.Errorf("Expected row 1 to be reported, got %d", firstBad)
	}

	// Writing under no key must fail too, rather than hashing under
	// nothing.
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck // the test is abandoning the transaction
	if err := store.recordAudit(tx, newEvent(systemActor, "user.create",
		"user", nil, "bob", nil)); !errors.Is(err, errNoAuditKey) {
		t.Errorf("Expected recordAudit to refuse to write without a key, "+
			"got %v", err)
	}
}

// TestNewAuthStoreRequiresAuditKey checks that the key is not optional,
// because a store built without one could not write a single audited
// change.
func TestNewAuthStoreRequiresAuditKey(t *testing.T) {
	for _, key := range [][]byte{nil, {}} {
		store, err := NewAuthStore(t.TempDir(), 0, 0, key)
		if err == nil {
			store.Close()
			t.Fatalf("Expected NewAuthStore to refuse key %v", key)
		}
		if !strings.Contains(err.Error(), "audit hash key is required") {
			t.Errorf("Expected the error to name the key, got %v", err)
		}
	}

	// A key shorter than the hash output is refused too. Production
	// always passes the 32 bytes DeriveAuditKey returns, so this guards
	// only a future caller that derived one some other way.
	store, err := NewAuthStore(t.TempDir(), 0, 0, []byte("too short"))
	if err == nil {
		store.Close()
		t.Fatal("Expected NewAuthStore to refuse a short key")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("Expected the error to name the length, got %v", err)
	}

	// Exactly the minimum is accepted, so the boundary is not off by
	// one against the key production actually uses.
	atMinimum, err := NewAuthStore(t.TempDir(), 0, 0,
		[]byte(strings.Repeat("k", minAuditKeyBytes)))
	if err != nil {
		t.Fatalf("Expected a %d-byte key to be accepted, got %v",
			minAuditKeyBytes, err)
	}
	atMinimum.Close()
}

// TestDeriveAuditKey checks the derivation is separated from the other
// keys taken from the same server secret, and refuses an empty secret.
func TestDeriveAuditKey(t *testing.T) {
	key := DeriveAuditKey("a server secret")
	if len(key) != 32 {
		t.Fatalf("Expected a 32-byte key, got %d bytes", len(key))
	}

	if same := DeriveAuditKey("a server secret"); string(same) != string(key) {
		t.Error("The same secret produced two different keys")
	}
	if other := DeriveAuditKey("another server secret"); string(other) == string(key) {
		t.Error("Two different secrets produced the same key")
	}
	if DeriveAuditKey("") != nil {
		t.Error("Expected an empty secret to yield no key")
	}

	// The salt is what keeps this key away from every other key derived
	// from the same secret, so it must not have been left at another
	// subsystem's value.
	if auditHashKeySalt != "pgedge-ai-workbench/audit-hash-chain/v1" {
		t.Errorf("The audit key salt has changed to %q, which makes every "+
			"row already written unverifiable", auditHashKeySalt)
	}
}
