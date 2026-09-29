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
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

// The tail of the audit log, its newest row, is the other place the
// hash chain cannot vouch for: deleting the newest rows leaves every
// surviving row linked to the one before it. verifyAuditTail compares
// MAX(id) with sqlite_sequence, but nothing protects sqlite_sequence,
// so someone able to write auth.db can delete the newest rows and write
// the sequence down to match. What follows closes that.
//
// The tail anchor is one row in audit_tail naming the newest event, by
// id and hash, under an HMAC keyed like the chain itself. recordAudit
// moves it forward in the transaction that inserts each event, so it
// always names the newest row, and VerifyAuditChain requires that it
// does. Deleting rows from the end of the log leaves it naming a row
// that is gone, and it cannot be rewritten to name the new newest row
// without the server secret.
//
// Deleting the anchor itself must not pass either, and here the hash
// version does the work. Rows written by a build that keeps the anchor
// carry hash version 3 (auditTailHashVersion), which the row's HMAC
// covers, so a row cannot be relabelled as an older version without
// failing; and a version 3 row as the newest with no anchor is
// refused. A log whose newest row is version 2 predates the anchor, and
// seedAuditTail anchors it when the store is opened, as long as
// sqlite_sequence agrees that nothing has been lost from its end.
//
// recordAudit only ever moves the anchor on from a state it can vouch
// for: an anchor that verifies and names the row the new event links
// to. An anchor that is missing, names some other row or does not
// verify is left exactly as it is, so the evidence of a deletion
// survives every event written after it rather than being overwritten
// by the next one. The two exceptions are the first event of a log
// that has never held one, which starts the anchor, and a changed
// server secret: an anchor written under the old secret names a row
// that was written under it too, so when neither verifies under the
// key in use, the anchor is moved on under the new one. Anyone can
// produce that shape by editing the newest row, but a row that does
// not verify, other than at the start of the log, is itself reported
// as tampering, so nothing is gained. Only the re-chain, an operator's
// deliberate act, writes an anchor over one that does not qualify.
//
// What it does not buy is set out at verifyAuditTail.

// auditTailHashVersion is the first hash version written by a build
// that keeps the tail anchor. A newest row at or above it without an
// anchor has had the anchor deleted.
const auditTailHashVersion = 3

// auditTailLabel opens the rendering the anchor's HMAC is computed
// over, so that it can never be mistaken for a row hash, which opens
// with a version label such as "v3".
const auditTailLabel = "pgedge-ai-workbench/audit-tail/v1"

// auditTailDDL creates the table holding the tail anchor. The CHECK
// keeps it to a single row.
const auditTailDDL = `
    -- The newest audit event, by id and hash, under an HMAC keyed by
    -- the server secret; see audit_tail.go
    CREATE TABLE IF NOT EXISTS audit_tail (
        id INTEGER PRIMARY KEY CHECK (id = 1),
        event_id INTEGER NOT NULL,
        event_hash TEXT NOT NULL,
        mac TEXT NOT NULL
    );
`

// auditSelectTailState reads the newest row and the anchor in a single
// statement, so that they come from one consistent snapshot: a server
// insert landing between two separate reads would otherwise leave the
// anchor naming a row one behind the newest. The outer SELECT returns
// exactly one row whether or not either exists.
const auditSelectTailState = `SELECT e.id, e.hash, e.hash_version,
        t.event_id, t.event_hash, t.mac
    FROM (SELECT 1) AS one
    LEFT JOIN (SELECT id, hash, hash_version FROM audit_events
               ORDER BY id DESC LIMIT 1) AS e ON 1 = 1
    LEFT JOIN audit_tail AS t ON t.id = 1`

// auditUpsertTail writes the anchor.
const auditUpsertTail = `INSERT INTO audit_tail (id, event_id, event_hash, mac)
    VALUES (1, ?, ?, ?)
    ON CONFLICT (id) DO UPDATE SET event_id = excluded.event_id,
        event_hash = excluded.event_hash, mac = excluded.mac`

// auditTailState is the newest row and the anchor, as read together.
type auditTailState struct {
	hasNewest     bool
	newestID      int64
	newestHash    string
	newestVersion int

	hasAnchor  bool
	anchorID   int64
	anchorHash string
	anchorMAC  string
}

// namesNewest reports whether the anchor names the newest row.
func (t auditTailState) namesNewest() bool {
	return t.hasAnchor && t.hasNewest && t.anchorID == t.newestID &&
		t.anchorHash == t.newestHash
}

// auditRowQuerier is what the tail needs of a database or transaction.
type auditRowQuerier interface {
	auditQuerier
	QueryRow(query string, args ...any) *sql.Row
}

// readAuditTailState reads the newest row and the anchor.
func readAuditTailState(q auditRowQuerier) (auditTailState, error) {
	var st auditTailState
	var newestID, newestVersion, anchorID sql.NullInt64
	var newestHash, anchorHash, anchorMAC sql.NullString
	if err := q.QueryRow(auditSelectTailState).Scan(&newestID, &newestHash,
		&newestVersion, &anchorID, &anchorHash, &anchorMAC); err != nil {
		return st, fmt.Errorf("failed to read the audit tail anchor: %w", err)
	}

	st.hasNewest = newestID.Valid
	st.newestID = newestID.Int64
	st.newestHash = newestHash.String
	st.newestVersion = int(newestVersion.Int64)
	st.hasAnchor = anchorID.Valid
	st.anchorID = anchorID.Int64
	st.anchorHash = anchorHash.String
	st.anchorMAC = anchorMAC.String

	return st, nil
}

// auditTailMAC computes the anchor's HMAC over an event's id and hash.
func auditTailMAC(key []byte, id int64, hash string) (string, error) {
	if len(key) == 0 {
		return "", errNoAuditKey
	}

	mac := hmac.New(sha256.New, key)
	// hash.Hash.Write is documented never to return an error.
	mac.Write(canonicalFields(auditTailLabel, strconv.FormatInt(id, 10),
		hash))

	return hex.EncodeToString(mac.Sum(nil)), nil
}

// auditTailVerifies reports whether the anchor's HMAC recomputes under
// the store's key.
func (s *AuthStore) auditTailVerifies(st auditTailState) bool {
	want, err := auditTailMAC(s.auditKey, st.anchorID, st.anchorHash)
	return err == nil && hmac.Equal([]byte(want), []byte(st.anchorMAC))
}

// writeAuditTail makes the anchor name ev, which must be the newest row.
func (s *AuthStore) writeAuditTail(tx *sql.Tx, ev *AuditEvent) error {
	mac, err := auditTailMAC(s.auditKey, ev.ID, ev.Hash)
	if err != nil {
		return fmt.Errorf("failed to sign the audit tail anchor: %w", err)
	}
	if _, err := tx.Exec(auditUpsertTail, ev.ID, ev.Hash, mac); err != nil {
		return fmt.Errorf("failed to write the audit tail anchor: %w", err)
	}

	return nil
}

// newestAuditRowVerifies reports whether the newest row, as read into
// st, verifies under the store's key. A row that cannot be read is
// reported as not verifying, which is the answer that grants nothing:
// both callers take "verifies" as the condition for refusing.
func (s *AuthStore) newestAuditRowVerifies(q auditRowQuerier,
	st auditTailState) (bool, error) {

	ev, err := scanAuditEvent(q.QueryRow(auditSelectByID, st.newestID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read audit event %d: %w",
			st.newestID, err)
	}

	return s.auditRowVerifies(&ev), nil
}

// mayAdvanceAuditTail decides whether the anchor may be moved on from
// before, the state recordAudit reads ahead of its insert, to the event
// it inserts. It is asked before the insert, because the insert itself
// advances sqlite_sequence. Any state other than the ones below is left
// alone, so that the evidence it holds survives; see the comment at the
// top of this file.
func (s *AuthStore) mayAdvanceAuditTail(tx *sql.Tx,
	before auditTailState) (bool, error) {

	switch {
	case !before.hasAnchor && !before.hasNewest:
		// The first event of a log. A log that once held events and
		// has been emptied still has its sqlite_sequence row, and is
		// not started afresh.
		return auditSequenceUnused(tx)
	case !before.namesNewest():
		return false, nil
	case s.auditTailVerifies(before):
		return true, nil
	}

	// The anchor names the newest row but does not verify. That is
	// what a changed server secret leaves if the row does not verify
	// either, since the two were written together under one key; if
	// the row does verify, the anchor was altered.
	verifies, err := s.newestAuditRowVerifies(tx, before)
	if err != nil {
		return false, err
	}

	return !verifies, nil
}

// auditSequenceUnused reports whether SQLite has never issued an id to
// audit_events, which is so for a log that has never held an event.
func auditSequenceUnused(q auditRowQuerier) (bool, error) {
	present, seq, err := readAuditSequence(q)
	if err != nil {
		return false, err
	}

	return !present || !seq.Valid || seq.Int64 == 0, nil
}

// readAuditSequence reads the sqlite_sequence value for audit_events,
// reporting whether the sqlite_sequence table exists at all.
func readAuditSequence(q auditRowQuerier) (bool, sql.NullInt64, error) {
	var seq sql.NullInt64
	present, err := auditSequenceTablePresent(q)
	if err != nil || !present {
		return false, seq, err
	}

	err = q.QueryRow(`SELECT seq FROM sqlite_sequence
         WHERE name = 'audit_events'`).Scan(&seq)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return true, seq, fmt.Errorf("failed to read the audit sequence: %w",
			err)
	}

	return true, seq, nil
}

// seedAuditTail anchors a log written before the anchor existed, whose
// newest row is hash version 2 and which has no anchor, and is called
// on every open. It anchors nothing unless sqlite_sequence agrees that
// no row has been lost from the end of the log, because an anchor
// written over a truncated tail would vouch for it, and the newest row
// verifies under the key in use; such a log is left
// without one, and once the next version 3 row is written its absence
// is reported. Every other log is left as it is: one that is empty is
// anchored by its first event, and one whose newest row is version 3
// already has an anchor or has lost it.
func (s *AuthStore) seedAuditTail() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the audit tail transaction: %w",
			err)
	}
	committed := false
	defer func() {
		if !committed {
			//nolint:errcheck // Rollback error is not critical; the
			// outer error is already being returned.
			tx.Rollback()
		}
	}()

	st, err := readAuditTailState(tx)
	if err != nil {
		return err
	}
	if st.hasAnchor || !st.hasNewest ||
		st.newestVersion >= auditTailHashVersion || st.newestVersion < 2 {
		return nil
	}

	present, seq, err := readAuditSequence(tx)
	if err != nil {
		return err
	}
	if !present || checkAuditTail(st.newestID, seq) != nil {
		return nil
	}
	// An anchor signed over a row the key in use does not verify, as
	// when the store is opened with the wrong secret, would read as an
	// altered anchor under the right one and never be moved on.
	verifies, err := s.newestAuditRowVerifies(tx, st)
	if err != nil {
		return err
	}
	if !verifies {
		return nil
	}

	ev := AuditEvent{ID: st.newestID, Hash: st.newestHash}
	if err := s.writeAuditTail(tx, &ev); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit the audit tail anchor: %w", err)
	}
	committed = true

	return nil
}

// verifyAuditTailAnchor checks that the anchor names the newest row and
// verifies under the key, or that the log predates the anchor.
//
// An anchor that names the newest row but does not verify is accepted
// when that row does not verify either, because the two were written
// together under one key and a changed server secret leaves both
// failing. VerifyAuditChain reaches this only once every row has
// verified, so there it is always refused; the re-chain and the purge
// ask it of a log that may have been written under another secret,
// where the walk has already reported the rows that fail.
func (s *AuthStore) verifyAuditTailAnchor() error {
	st, err := readAuditTailState(s.db)
	if err != nil {
		return err
	}

	switch {
	case !st.hasAnchor:
		if st.hasNewest && st.newestVersion >= auditTailHashVersion {
			return fmt.Errorf("%w: audit chain tail anchor missing: the "+
				"newest event, %d, was written by a build that records "+
				"the newest event in audit_tail, but audit_tail is empty; "+
				"it has been deleted, and events may have been deleted "+
				"from the end of the log with it",
				ErrAuditChainBroken, st.newestID)
		}
		return nil
	case !st.hasNewest:
		return fmt.Errorf("%w: audit chain tail missing: the log is empty, "+
			"but the tail anchor records event %d as the newest; every "+
			"event has been deleted", ErrAuditChainBroken, st.anchorID)
	case !st.namesNewest():
		return fmt.Errorf("%w: audit chain tail missing: the tail anchor "+
			"records event %d as the newest, but the newest event is now "+
			"%d; events have been deleted from the end of the log, or "+
			"written to it by something other than this server",
			ErrAuditChainBroken, st.anchorID, st.newestID)
	case s.auditTailVerifies(st):
		return nil
	}

	verifies, err := s.newestAuditRowVerifies(s.db, st)
	if err != nil {
		return err
	}
	if verifies {
		return fmt.Errorf("%w: audit chain tail anchor altered: it names "+
			"the newest event, %d, but does not verify under the key in "+
			"use, although that event does", ErrAuditChainBroken,
			st.newestID)
	}

	return nil
}
