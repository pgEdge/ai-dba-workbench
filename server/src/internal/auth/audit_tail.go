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
	"strings"
)

// The tail of the audit log, its newest row, is the other place the
// hash chain cannot vouch for: deleting the newest rows leaves every
// surviving row linked to the one before it. verifyAuditTail compares
// MAX(id) with sqlite_sequence, but nothing protects sqlite_sequence,
// so someone able to write auth.db can delete the newest rows and write
// the sequence down to match. What follows closes that.
//
// The tail anchor is a row in audit_tail naming an event, by id and
// hash, under an HMAC keyed like the chain itself. While the server
// secret stays the same there is one, the primary anchor (id 1):
// recordAudit moves it forward in the transaction that inserts each
// event, so that it names the newest row, and VerifyAuditChain requires
// that it does. Deleting rows from the end of the log leaves it naming
// a row that is gone, and it cannot be rewritten to name the new newest
// row without the server secret.
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
// recordAudit only ever moves an anchor on from a state it can vouch
// for: one that verifies under the key in use and names the row the new
// event links to. An anchor that is missing, names some other row or
// does not verify is left exactly as it is, so the evidence of a
// deletion survives every event written after it rather than being
// overwritten by the next one. The exceptions are the first event of a
// log that has never held one, which starts the primary anchor, and the
// changed secret described next. Only the re-chain, an operator's
// deliberate act, writes an anchor over one that does not qualify.
//
// A changed server secret leaves the primary anchor signed under a key
// the server no longer holds, so it can neither be checked nor moved on.
// Re-signing it under the new secret whenever neither it nor the row it
// names verified would let anyone able to write auth.db have the server
// re-sign it over a truncated tail: put back a row that copies the hash
// of the new newest row and fails, point the anchor at it, wait for the
// server's next event, which links past it, and delete it. The primary
// anchor therefore stays where the old secret left it, naming the last
// row written under that secret; only the previous secret can check it,
// which the re-anchor does when given one (proveAuditReanchorTail).
//
// The events written under the new secret are covered by the second
// slot, the current-key anchor (id 2). recordAudit starts it at the
// first event written while the primary anchor names the newest row and
// both fail under the key in use, which is the state a rotation leaves,
// and from then on moves it forward by the same rule as the primary:
// only while it verifies and names the newest row. For a log with the
// shape a changed secret leaves, verifyAuditTailAfterKeyChange requires
// the primary anchor at the last row that fails and the current-key
// anchor at the newest row, verifying under the key in use, so a cut
// from the events written since the rotation is caught exactly as one
// from a log never rotated. A log in which every row verifies is checked
// against the primary anchor alone. Starting the current-key anchor from
// a decoy, as above, gains nothing there, because the primary anchor
// still names the decoy once it has been deleted.
//
// The current-key anchor's HMAC covers the primary anchor beside it as
// well as the event it names (auditTailCurrentMAC). Without that, a
// writer could repeat the decoy against a rotated log: save the primary
// anchor, delete the newest rows and the current-key anchor, point the
// primary at a decoy so the server starts a current-key anchor over the
// cut, then delete the decoy and put the saved primary back. Bound to
// the decoy's primary anchor, the current-key anchor fails once the
// saved one returns.
//
// Design decision: a second rotation before the log is re-anchored.
// When the current-key anchor names the newest row and both fail under
// the key in use, the secret has changed again, and its value is
// promoted unchanged into the primary slot, whilst the current-key
// anchor starts again at the new event. The two slots therefore always
// name the last row written under the previous secret and the newest
// row under the current one, which is what the verifier asks of them,
// so a log written under three secrets reads as a key mismatch rather
// than as tampering. What the primary slot named before is given up,
// but by then it no longer protects anything: the rows written under
// the oldest secret are not at the end of the log, and the first row
// written under the next secret links to the last of them, so the chain
// itself catches their deletion. Nor does promotion help a writer
// without the secret: a value they plant in the current-key anchor
// carries a MAC that verifies under no secret, and once promoted into
// the primary slot the re-anchor's check under the previous secret
// refuses it, as it refuses one planted in the primary slot directly;
// and the current-key anchor written alongside is bound to that value,
// so putting the earlier primary anchor back afterwards fails it. A
// promoted primary anchor carries the current-key rendering, so the
// re-anchor's check under the previous secret refuses it as well, but
// no unattended re-anchor of such a log can succeed anyway: its history
// spans two earlier secrets, and the one given proves only part of it.
//
// The re-chain and the re-anchor accept the log as it stands, so they
// reset both slots: the primary anchor names the event they write, and
// the current-key anchor is removed.
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

// auditTailCurrentLabel opens the rendering the current-key anchor's
// HMAC is computed over, which also covers the primary anchor it was
// written alongside; see auditTailCurrentMAC.
const auditTailCurrentLabel = "pgedge-ai-workbench/audit-tail-current/v1"

// The two rows audit_tail can hold; see the comment at the top of this
// file.
const (
	// auditTailPrimary is the primary anchor.
	auditTailPrimary = 1

	// auditTailCurrent is the current-key anchor, present only after a
	// change of server secret and until the next re-chain.
	auditTailCurrent = 2
)

// auditTailDDL creates the table holding the tail anchors. The CHECK
// keeps it to the two slots.
const auditTailDDL = `
    -- The newest audit event, by id and hash, under an HMAC keyed by
    -- the server secret, and after a change of secret the newest event
    -- under the new one; see audit_tail.go
    CREATE TABLE IF NOT EXISTS audit_tail (
        id INTEGER PRIMARY KEY CHECK (id IN (1, 2)),
        event_id INTEGER NOT NULL,
        event_hash TEXT NOT NULL,
        mac TEXT NOT NULL
    );
`

// auditTailSingleSlotCheck is the CHECK clause, as normalizeSchemaSQL
// renders it, of an audit_tail table created before the current-key
// anchor existed, which has room for the primary anchor only.
const auditTailSingleSlotCheck = "CHECK (id = 1)"

// auditSelectTailState reads the newest row and both anchors in a
// single statement, so that they come from one consistent snapshot: a
// server insert landing between two separate reads would otherwise
// leave an anchor naming a row one behind the newest. The outer SELECT
// returns exactly one row whether or not any of them exists.
const auditSelectTailState = `SELECT e.id, e.hash, e.hash_version,
        t.event_id, t.event_hash, t.mac,
        c.event_id, c.event_hash, c.mac
    FROM (SELECT 1) AS one
    LEFT JOIN (SELECT id, hash, hash_version FROM audit_events
               ORDER BY id DESC LIMIT 1) AS e ON 1 = 1
    LEFT JOIN audit_tail AS t ON t.id = 1
    LEFT JOIN audit_tail AS c ON c.id = 2`

// auditUpsertTail writes one anchor slot.
const auditUpsertTail = `INSERT INTO audit_tail (id, event_id, event_hash, mac)
    VALUES (?, ?, ?, ?)
    ON CONFLICT (id) DO UPDATE SET event_id = excluded.event_id,
        event_hash = excluded.event_hash, mac = excluded.mac`

// auditTailState is the newest row and the anchors, as read together.
// The anchor fields describe the primary anchor, and the current fields
// the current-key anchor.
type auditTailState struct {
	hasNewest     bool
	newestID      int64
	newestHash    string
	newestVersion int

	hasAnchor  bool
	anchorID   int64
	anchorHash string
	anchorMAC  string

	hasCurrent  bool
	currentID   int64
	currentHash string
	currentMAC  string
}

// namesNewest reports whether the primary anchor names the newest row.
func (t auditTailState) namesNewest() bool {
	return t.hasAnchor && t.hasNewest && t.anchorID == t.newestID &&
		t.anchorHash == t.newestHash
}

// currentNamesNewest reports whether the current-key anchor names the
// newest row.
func (t auditTailState) currentNamesNewest() bool {
	return t.hasCurrent && t.hasNewest && t.currentID == t.newestID &&
		t.currentHash == t.newestHash
}

// auditRowQuerier is what the tail needs of a database or transaction.
type auditRowQuerier interface {
	auditQuerier
	QueryRow(query string, args ...any) *sql.Row
}

// readAuditTailState reads the newest row and the anchors.
func readAuditTailState(q auditRowQuerier) (auditTailState, error) {
	var st auditTailState
	var newestID, newestVersion, anchorID, currentID sql.NullInt64
	var newestHash, anchorHash, anchorMAC sql.NullString
	var currentHash, currentMAC sql.NullString
	if err := q.QueryRow(auditSelectTailState).Scan(&newestID, &newestHash,
		&newestVersion, &anchorID, &anchorHash, &anchorMAC, &currentID,
		&currentHash, &currentMAC); err != nil {
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
	st.hasCurrent = currentID.Valid
	st.currentID = currentID.Int64
	st.currentHash = currentHash.String
	st.currentMAC = currentMAC.String

	return st, nil
}

// auditTailMAC computes the primary anchor's HMAC over an event's id
// and hash.
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

// auditTailCurrentMAC computes the current-key anchor's HMAC over an
// event's id and hash and over the whole of the primary anchor it was
// written alongside, which nothing changes while the current-key anchor
// lives. Binding the two keeps a writer without the secret from
// splicing them: a current-key anchor the server was led to start, or
// promote, over a decoy names the decoy's primary anchor too, so putting
// back a saved primary anchor afterwards leaves it failing.
func auditTailCurrentMAC(key []byte, id int64, hash string,
	primaryID int64, primaryHash, primaryMAC string) (string, error) {

	if len(key) == 0 {
		return "", errNoAuditKey
	}

	mac := hmac.New(sha256.New, key)
	// hash.Hash.Write is documented never to return an error.
	mac.Write(canonicalFields(auditTailCurrentLabel,
		strconv.FormatInt(id, 10), hash, strconv.FormatInt(primaryID, 10),
		primaryHash, primaryMAC))

	return hex.EncodeToString(mac.Sum(nil)), nil
}

// auditTailMACVerifies reports whether mac is the anchor HMAC of id and
// hash under key.
func auditTailMACVerifies(key []byte, id int64, hash, mac string) bool {
	want, err := auditTailMAC(key, id, hash)
	return err == nil && hmac.Equal([]byte(want), []byte(mac))
}

// auditTailVerifies reports whether the primary anchor's HMAC
// recomputes under the store's key.
func (s *AuthStore) auditTailVerifies(st auditTailState) bool {
	return auditTailMACVerifies(s.auditKey, st.anchorID, st.anchorHash,
		st.anchorMAC)
}

// currentTailVerifies reports whether the current-key anchor's HMAC
// recomputes under the store's key, over the primary anchor as it now
// stands.
func (s *AuthStore) currentTailVerifies(st auditTailState) bool {
	want, err := auditTailCurrentMAC(s.auditKey, st.currentID,
		st.currentHash, st.anchorID, st.anchorHash, st.anchorMAC)
	return err == nil && st.hasAnchor &&
		hmac.Equal([]byte(want), []byte(st.currentMAC))
}

// writeAuditTailSlot writes one anchor slot as given.
func writeAuditTailSlot(tx *sql.Tx, slot int, id int64, hash,
	mac string) error {

	if _, err := tx.Exec(auditUpsertTail, slot, id, hash, mac); err != nil {
		return fmt.Errorf("failed to write the audit tail anchor: %w", err)
	}

	return nil
}

// writeAuditTail makes the primary anchor name ev, which must be the
// newest row, signed under the store's key.
func (s *AuthStore) writeAuditTail(tx *sql.Tx, ev *AuditEvent) error {
	mac, err := auditTailMAC(s.auditKey, ev.ID, ev.Hash)
	if err != nil {
		return fmt.Errorf("failed to sign the audit tail anchor: %w", err)
	}

	return writeAuditTailSlot(tx, auditTailPrimary, ev.ID, ev.Hash, mac)
}

// writeAuditTailCurrent makes the current-key anchor name ev, which must
// be the newest row, signed under the store's key together with the
// primary anchor it stands beside.
func (s *AuthStore) writeAuditTailCurrent(tx *sql.Tx, ev *AuditEvent,
	primaryID int64, primaryHash, primaryMAC string) error {

	mac, err := auditTailCurrentMAC(s.auditKey, ev.ID, ev.Hash, primaryID,
		primaryHash, primaryMAC)
	if err != nil {
		return fmt.Errorf("failed to sign the audit tail anchor: %w", err)
	}

	return writeAuditTailSlot(tx, auditTailCurrent, ev.ID, ev.Hash, mac)
}

// resetAuditTail makes the primary anchor name ev, which must be the
// newest row, and removes the current-key anchor. The re-chain and the
// re-anchor call it, having accepted the log as it stands.
func (s *AuthStore) resetAuditTail(tx *sql.Tx, ev *AuditEvent) error {
	if err := s.writeAuditTail(tx, ev); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM audit_tail WHERE id = ?",
		auditTailCurrent); err != nil {
		return fmt.Errorf("failed to remove the current-key audit tail "+
			"anchor: %w", err)
	}

	return nil
}

// newestAuditRowVerifies reports whether the newest row, as read into
// st, verifies under the store's key. A row that cannot be read is
// reported as not verifying.
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

// auditTailMove is what recordAudit does to the anchors for the event
// it inserts.
type auditTailMove int

const (
	// auditTailStay leaves both anchors as they are.
	auditTailStay auditTailMove = iota

	// auditTailAdvance makes the primary anchor name the new event.
	auditTailAdvance

	// auditTailAdvanceCurrent makes the current-key anchor name the new
	// event, starting it if it was absent.
	auditTailAdvanceCurrent

	// auditTailPromote moves the current-key anchor, unchanged, into
	// the primary slot, and makes the current-key anchor name the new
	// event.
	auditTailPromote
)

// planAuditTailMove decides what recordAudit does to the anchors, from
// before, the state it reads ahead of its insert. It is asked before the
// insert, because the insert itself advances sqlite_sequence. Any state
// other than the ones below is left alone, so that the evidence it holds
// survives; see the comment at the top of this file.
func (s *AuthStore) planAuditTailMove(tx *sql.Tx,
	before auditTailState) (auditTailMove, error) {

	switch {
	case !before.hasAnchor && !before.hasNewest:
		// The first event of a log. A log that once held events and
		// has been emptied still has its sqlite_sequence row, and is
		// not started afresh.
		unused, err := auditSequenceUnused(tx)
		if err != nil || !unused {
			return auditTailStay, err
		}
		return auditTailAdvance, nil
	case before.namesNewest() && s.auditTailVerifies(before):
		return auditTailAdvance, nil
	case before.currentNamesNewest() && s.currentTailVerifies(before):
		return auditTailAdvanceCurrent, nil
	}

	// What is left that may move follows a change of secret: the slot
	// naming the newest row fails under the key in use. The row it
	// names was written under the same secret as the anchor, so it must
	// fail too; if it verifies, the anchor was altered.
	var move auditTailMove
	switch {
	case before.namesNewest() && !before.hasCurrent:
		move = auditTailAdvanceCurrent
	case before.currentNamesNewest():
		move = auditTailPromote
	default:
		return auditTailStay, nil
	}
	verifies, err := s.newestAuditRowVerifies(tx, before)
	if err != nil || verifies {
		return auditTailStay, err
	}

	return move, nil
}

// applyAuditTailMove carries out move for ev, the event recordAudit has
// just inserted, from before, the state read ahead of the insert.
func (s *AuthStore) applyAuditTailMove(tx *sql.Tx, move auditTailMove,
	before auditTailState, ev *AuditEvent) error {

	switch move {
	case auditTailAdvance:
		return s.writeAuditTail(tx, ev)
	case auditTailAdvanceCurrent:
		return s.writeAuditTailCurrent(tx, ev, before.anchorID,
			before.anchorHash, before.anchorMAC)
	case auditTailPromote:
		if err := writeAuditTailSlot(tx, auditTailPrimary, before.currentID,
			before.currentHash, before.currentMAC); err != nil {
			return err
		}
		return s.writeAuditTailCurrent(tx, ev, before.currentID,
			before.currentHash, before.currentMAC)
	}

	return nil
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

// migrateAuditTailSlots rebuilds an audit_tail table created before the
// current-key anchor existed, whose CHECK allows the primary anchor
// only, so that it can hold both. No release created that table, only
// pre-release builds of the anchor, but a server running against one
// would fail every audited change once a rotation called for the second
// slot. CHECK constraints cannot be altered in SQLite, so the table is
// renamed, created afresh and refilled, in one transaction that leaves
// the old table untouched if it fails. A table that already allows both
// is left alone, so ensureAuditSchema can call this on every open.
func (s *AuthStore) migrateAuditTailSlots() error {
	var definition string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master
         WHERE type = 'table' AND name = 'audit_tail'`).
		Scan(&definition); err != nil {
		return fmt.Errorf("failed to read the audit_tail definition: %w", err)
	}
	if !strings.Contains(normalizeSchemaSQL(definition),
		auditTailSingleSlotCheck) {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the audit_tail migration: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			//nolint:errcheck // Rollback error is not critical; the
			// outer error is already being returned.
			tx.Rollback()
		}
	}()

	for _, stmt := range []string{
		"ALTER TABLE audit_tail RENAME TO audit_tail_single_slot",
		auditTailDDL,
		`INSERT INTO audit_tail (id, event_id, event_hash, mac)
         SELECT id, event_id, event_hash, mac FROM audit_tail_single_slot`,
		"DROP TABLE audit_tail_single_slot",
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("failed to migrate audit_tail to two anchor "+
				"slots: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit the audit_tail migration: %w",
			err)
	}
	committed = true

	return nil
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

// verifyAuditTailAnchor checks that the primary anchor names the newest
// row and verifies under the key, or that the log predates the anchor.
// It is reached only once every row has verified; a log whose oldest
// rows fail, as a changed server secret leaves them, has its anchors
// checked by verifyAuditTailAfterKeyChange instead. The current-key
// anchor is not consulted: in a log whose rows all verify under one
// key, the primary anchor names the newest of them.
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
// The primary anchor written under the old secret is never moved on
// under the new one, so it is accepted where it names lastFailing, by id
// and hash, without verifying under the key in use; it can be checked
// only under the previous secret, which proveAuditReanchorTail does. The
// rows after it must then be covered by the current-key anchor
// (verifyAuditTailCurrent). An anchor that verifies and names the newest
// row is accepted as well, as seedAuditTail writes it over a log a
// release before the anchor went on writing under the new secret.
// Anything else is a deletion from the end of the log, which a rotation
// does not explain.
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
	case st.namesNewest() && s.auditTailVerifies(st):
		return nil
	case st.anchorID == lastFailing.ID && st.anchorHash == lastFailing.Hash &&
		!s.auditTailVerifies(st):
		return s.verifyAuditTailCurrent(st, lastFailing)
	case st.newestID == lastFailing.ID && st.currentNamesNewest() &&
		!s.currentTailVerifies(st) && !s.auditTailVerifies(st):
		// The secret has changed twice and nothing has been written
		// under the newest: the current-key anchor still names the last
		// row written under the secret before, and the next event will
		// promote it. Nothing here verifies under the key in use, so
		// there is nothing more for it to check.
		return nil
	}

	return fmt.Errorf("%w: audit chain tail missing: the tail anchor "+
		"records event %d, which is neither the newest event, %d, under "+
		"the key in use nor the last event that fails under it, %d; "+
		"events have been deleted from the end of the log, or the anchor "+
		"altered", ErrAuditChainBroken, st.anchorID, st.newestID,
		lastFailing.ID)
}

// verifyAuditTailCurrent checks the current-key anchor of a log whose
// primary anchor names lastFailing, the last row that fails under the
// key in use. Where no row follows it, nothing has been written under
// the new secret and there must be no current-key anchor; otherwise the
// current-key anchor must name the newest row and verify under the key
// in use, exactly as the primary anchor must in a log never rotated.
func (s *AuthStore) verifyAuditTailCurrent(st auditTailState,
	lastFailing AuditEvent) error {

	if st.newestID == lastFailing.ID {
		if st.hasCurrent {
			return fmt.Errorf("%w: audit chain tail missing: the tail "+
				"anchor under the key in use records event %d, but no "+
				"event after %d, the last one that fails under that key, "+
				"survives; the events written under it have been deleted "+
				"from the end of the log", ErrAuditChainBroken,
				st.currentID, lastFailing.ID)
		}
		return nil
	}

	switch {
	case !st.hasCurrent:
		return fmt.Errorf("%w: audit chain tail anchor missing: events "+
			"after %d, the last one that fails under the key in use, "+
			"verify under it, but audit_tail holds no anchor for them; it "+
			"has been deleted, and events may have been deleted from the "+
			"end of the log with it", ErrAuditChainBroken, lastFailing.ID)
	case !st.currentNamesNewest():
		return fmt.Errorf("%w: audit chain tail missing: the tail anchor "+
			"under the key in use records event %d as the newest, but the "+
			"newest event is now %d; events have been deleted from the end "+
			"of the log, or written to it by something other than this "+
			"server", ErrAuditChainBroken, st.currentID, st.newestID)
	case !s.currentTailVerifies(st):
		return fmt.Errorf("%w: audit chain tail anchor altered: the "+
			"anchor under the key in use names the newest event, %d, but "+
			"does not verify under that key together with the anchor "+
			"beside it", ErrAuditChainBroken, st.newestID)
	}

	return nil
}

// errAuditTailUnproven wraps each reason proveAuditReanchorTail gives.
var errAuditTailUnproven = errors.New("the tail anchor is not what the " +
	"previous secret wrote")

// proveAuditReanchorTail checks, under previousKey, a primary anchor
// that does not verify under the key in use: it must verify under
// previousKey and name the last row the re-anchor would accept as
// history, through, whose hash is throughHash. That is where the
// previous secret left it, since nothing moves it on under another. An
// anchor that is missing or verifies under the key in use needs no
// proof. The returned error is the reason the proof fails, or nil when
// it holds; a failure to read the anchor is returned as the second
// value instead.
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

	if !auditTailMACVerifies(previousKey, st.anchorID, st.anchorHash,
		st.anchorMAC) {
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
