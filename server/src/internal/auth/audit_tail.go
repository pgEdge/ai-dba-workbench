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
// for: an anchor that verifies under the key in use and names the row
// the new event links to. An anchor that is missing, names some other
// row or does not verify is left exactly as it is, so the evidence of a
// deletion survives every event written after it rather than being
// overwritten by the next one. The one exception is the first event of
// a log that has never held one, which starts the anchor. Only the
// re-chain, an operator's deliberate act, writes an anchor over one
// that does not qualify.
//
// A changed server secret is no exception. The anchor written under the
// old secret stays where it was, naming the last row written under it,
// and the rows written under the new secret go on without moving it.
// Moving it on whenever neither it nor the row it names verified would
// let anyone able to write auth.db have the server re-sign it over a
// truncated tail: put back a row that copies the hash of the new newest
// row and fails, point the anchor at it, wait for the server's next
// event, which links past it, and delete it. Verification instead
// accepts a stranded anchor in a log with the shape a changed secret
// leaves only where it names the last row that fails
// (verifyAuditTailAfterKeyChange), which is reported as a key mismatch,
// and the re-anchor that follows checks it under the previous secret
// when given one, which is what an unattended re-anchor needs.
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

	if !before.hasAnchor && !before.hasNewest {
		// The first event of a log. A log that once held events and
		// has been emptied still has its sqlite_sequence row, and is
		// not started afresh.
		return auditSequenceUnused(tx)
	}

	return before.namesNewest() && s.auditTailVerifies(before), nil
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
	seed, err := s.auditTailSeedable(tx, st)
	if err != nil || !seed {
		return err
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

// auditTailSeedable reports whether seedAuditTail may anchor the log
// in st: a version 2 newest row with no anchor, a sequence that agrees
// nothing is missing from the end, and a newest row that verifies under
// the key in use.
func (s *AuthStore) auditTailSeedable(tx *sql.Tx,
	st auditTailState) (bool, error) {

	if st.hasAnchor || !st.hasNewest ||
		st.newestVersion >= auditTailHashVersion || st.newestVersion < 2 {
		return false, nil
	}

	present, seq, err := readAuditSequence(tx)
	if err != nil {
		return false, err
	}
	if !present || checkAuditTail(st.newestID, seq) != nil {
		return false, nil
	}

	// An anchor signed over a row the key in use does not verify, as
	// when the store is opened with the wrong secret, would read as an
	// altered anchor under the right one and never be moved on.
	return s.newestAuditRowVerifies(tx, st)
}

// verifyAuditTailAnchor checks that the anchor names the newest row and
// verifies under the key, or that the log predates the anchor. It is
// reached only once every row has verified; a log whose oldest rows
// fail, as a changed server secret leaves them, has its anchor checked
// by verifyAuditTailAfterKeyChange instead.
func (s *AuthStore) verifyAuditTailAnchor(q auditRowQuerier) error {
	st, err := readAuditTailState(q)
	if err != nil {
		return err
	}
	if err := auditTailAnchorPresent(st); err != nil {
		return err
	}

	switch {
	case !st.hasAnchor:
		return nil
	case !st.namesNewest():
		return fmt.Errorf("%w: audit chain tail missing: the tail anchor "+
			"records event %d as the newest, but the newest event is now "+
			"%d; events have been deleted from the end of the log, or "+
			"written to it by something other than this server",
			ErrAuditChainBroken, st.anchorID, st.newestID)
	case !s.auditTailVerifies(st):
		return fmt.Errorf("%w: audit chain tail anchor altered: it names "+
			"the newest event, %d, but does not verify under the key in "+
			"use", ErrAuditChainBroken, st.newestID)
	}

	return nil
}

// auditTailAnchorPresent refuses the two states of the anchor that are
// wrong whatever key the log was written under: none behind a newest
// row that a build keeping the anchor wrote, and one naming an event in
// a log that is empty.
func auditTailAnchorPresent(st auditTailState) error {
	switch {
	case !st.hasAnchor && st.hasNewest &&
		st.newestVersion >= auditTailHashVersion:
		return fmt.Errorf("%w: audit chain tail anchor missing: the "+
			"newest event, %d, was written by a build that records "+
			"the newest event in audit_tail, but audit_tail is empty; "+
			"it has been deleted, and events may have been deleted "+
			"from the end of the log with it",
			ErrAuditChainBroken, st.newestID)
	case st.hasAnchor && !st.hasNewest:
		return fmt.Errorf("%w: audit chain tail missing: the log is empty, "+
			"but the tail anchor records event %d as the newest; every "+
			"event has been deleted", ErrAuditChainBroken, st.anchorID)
	}

	return nil
}

// verifyAuditTailAfterKeyChange is the tail check for a log with the
// shape a changed server secret leaves, whose oldest rows, through
// lastFailing, fail under the key in use and whose later rows verify.
// The anchor written under the old secret is never moved on under the
// new one, so it is accepted where it names lastFailing, by id and
// hash, without verifying under the key in use; it can be checked only
// under the previous secret, which proveAuditReanchorTail does. An
// anchor that verifies and names the newest row is accepted as well, as
// seedAuditTail writes it over a log a release before the anchor went
// on writing under the new secret. Anything else is a deletion from the
// end of the log, which a rotation does not explain.
func (s *AuthStore) verifyAuditTailAfterKeyChange(q auditRowQuerier,
	lastFailing AuditEvent) error {

	if err := verifyAuditSequence(q); err != nil {
		return err
	}
	st, err := readAuditTailState(q)
	if err != nil {
		return err
	}
	if err := auditTailAnchorPresent(st); err != nil {
		return err
	}

	switch {
	case !st.hasAnchor:
		return nil
	case st.anchorID == lastFailing.ID && st.anchorHash == lastFailing.Hash &&
		!s.auditTailVerifies(st):
		return nil
	case st.namesNewest() && s.auditTailVerifies(st):
		return nil
	}

	return fmt.Errorf("%w: audit chain tail missing: the tail anchor "+
		"records event %d, which is neither the newest event, %d, under "+
		"the key in use nor the last event that fails under it, %d; "+
		"events have been deleted from the end of the log, or the anchor "+
		"altered", ErrAuditChainBroken, st.anchorID, st.newestID,
		lastFailing.ID)
}

// errAuditTailUnproven wraps each reason proveAuditReanchorTail gives.
var errAuditTailUnproven = errors.New("the tail anchor is not what the " +
	"previous secret wrote")

// proveAuditReanchorTail checks, under previousKey, an anchor that does
// not verify under the key in use: it must verify under previousKey and
// name the last row the re-anchor would accept as history, through,
// whose hash is throughHash. That is where the previous secret left it,
// since nothing moves it on under another. An anchor that is missing or
// verifies under the key in use needs no proof. The returned error is
// the reason the proof fails, or nil when it holds; a failure to read
// the anchor is returned as the second value instead.
func (s *AuthStore) proveAuditReanchorTail(q auditRowQuerier,
	previousKey []byte, through int64, throughHash string) (proof error,
	err error) {

	st, err := readAuditTailState(q)
	if err != nil {
		return nil, err
	}
	if !st.hasAnchor || s.auditTailVerifies(st) {
		return nil, nil
	}

	want, macErr := auditTailMAC(previousKey, st.anchorID, st.anchorHash)
	if macErr != nil ||
		!hmac.Equal([]byte(want), []byte(st.anchorMAC)) {
		return fmt.Errorf("%w: it names event %d, but verifies under "+
			"neither secret", errAuditTailUnproven, st.anchorID), nil
	}
	if st.anchorID != through || st.anchorHash != throughHash {
		return fmt.Errorf("%w: it names event %d, but the last event "+
			"written under that secret is now %d; events written under it "+
			"have been deleted from the end of the log",
			errAuditTailUnproven, st.anchorID, through), nil
	}

	return nil, nil
}
