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
	"log"
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
// itself catches their deletion.
//
// Promotion copies the current-key anchor's HMAC unchanged, but that
// HMAC was computed over the primary anchor it replaces, so the primary
// slot keeps those three overwritten values in its bound_* columns, and
// a promoted primary anchor is checked under the previous secret with
// the current-key rendering over them (primaryTailVerifiesUnder). The
// columns are set only by promotion, and only on the primary slot.
//
// A value planted in the current-key anchor between a change of secret
// and the first event written under the new one is accepted by
// verification: in that window both anchors fail under the key in use,
// so checkRotatedAuditTail can match the current-key anchor only by the
// event it names, and once promoted it is a primary anchor the key in
// use cannot check either. A writer can so cut events written under the
// previous secret from the end of the log and point the current-key
// anchor at the new newest row, and verification reports only the key
// mismatch, before and after the promotion. The previous secret refuses
// the plant in both states (proveAuditReanchorTail), so a re-anchor
// given it catches the cut; operators re-anchor with the previous
// secret after each change of secret, before the next, which also keeps
// a log from spanning more than two secrets. The current-key anchor
// written alongside a promotion is bound to the promoted value, so
// putting the earlier primary anchor back afterwards fails it.
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

// auditTailDDL creates the table holding the tail anchors. The first
// CHECK keeps it to the two slots; the bound_ columns hold, on a primary
// anchor promoted from the current-key slot, the primary anchor its HMAC
// was computed beside, and are set together or not at all.
const auditTailDDL = `
    -- The newest audit event, by id and hash, under an HMAC keyed by
    -- the server secret, and after a change of secret the newest event
    -- under the new one; see audit_tail.go
    CREATE TABLE IF NOT EXISTS audit_tail (
        id INTEGER PRIMARY KEY CHECK (id IN (1, 2)),
        event_id INTEGER NOT NULL,
        event_hash TEXT NOT NULL,
        mac TEXT NOT NULL,
        bound_event_id INTEGER,
        bound_event_hash TEXT,
        bound_mac TEXT,
        CHECK ((bound_event_id IS NULL) = (bound_event_hash IS NULL)
            AND (bound_event_id IS NULL) = (bound_mac IS NULL)
            AND (id = 1 OR bound_event_id IS NULL))
    );
`

// auditTailLegacyDDLs are the definitions of audit_tail that
// pre-release builds of the anchor created, which migrateAuditTailSchema
// rebuilds as auditTailDDL. No release created either. Any other
// definition is refused by verifyAuditTailSchema rather than rebuilt.
var auditTailLegacyDDLs = []string{
	// Room for the primary anchor only.
	`CREATE TABLE IF NOT EXISTS audit_tail (
        id INTEGER PRIMARY KEY CHECK (id = 1),
        event_id INTEGER NOT NULL,
        event_hash TEXT NOT NULL,
        mac TEXT NOT NULL
    );`,
	// Both slots, without the columns a promoted anchor needs.
	`CREATE TABLE IF NOT EXISTS audit_tail (
        id INTEGER PRIMARY KEY CHECK (id IN (1, 2)),
        event_id INTEGER NOT NULL,
        event_hash TEXT NOT NULL,
        mac TEXT NOT NULL
    );`,
}

// auditSelectTailState reads the newest row and both anchors in a
// single statement, so that they come from one consistent snapshot: a
// server insert landing between two separate reads would otherwise
// leave an anchor naming a row one behind the newest. The outer SELECT
// returns exactly one row whether or not any of them exists.
const auditSelectTailState = `SELECT e.id, e.hash, e.hash_version,
        t.event_id, t.event_hash, t.mac,
        t.bound_event_id, t.bound_event_hash, t.bound_mac,
        c.event_id, c.event_hash, c.mac
    FROM (SELECT 1) AS one
    LEFT JOIN (SELECT id, hash, hash_version FROM audit_events
               ORDER BY id DESC LIMIT 1) AS e ON 1 = 1
    LEFT JOIN audit_tail AS t ON t.id = 1
    LEFT JOIN audit_tail AS c ON c.id = 2`

// auditUpsertTail writes one anchor slot, binding columns included.
const auditUpsertTail = `INSERT INTO audit_tail (id, event_id, event_hash,
        mac, bound_event_id, bound_event_hash, bound_mac)
    VALUES (?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT (id) DO UPDATE SET event_id = excluded.event_id,
        event_hash = excluded.event_hash, mac = excluded.mac,
        bound_event_id = excluded.bound_event_id,
        bound_event_hash = excluded.bound_event_hash,
        bound_mac = excluded.bound_mac`

// auditTailBinding is a primary anchor as a current-key anchor's HMAC
// covers it: see auditTailCurrentMAC. A primary anchor promoted from the
// current-key slot keeps the one its HMAC was computed beside, because
// promotion overwrites it and the HMAC cannot be checked without it.
type auditTailBinding struct {
	valid bool
	id    int64
	hash  string
	mac   string
}

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

	// bound is set on a primary anchor promoted from the current-key
	// slot, whose HMAC is a current-key HMAC bound to it.
	bound auditTailBinding

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
	var boundID sql.NullInt64
	var boundHash, boundMAC sql.NullString
	if err := q.QueryRow(auditSelectTailState).Scan(&newestID, &newestHash,
		&newestVersion, &anchorID, &anchorHash, &anchorMAC, &boundID,
		&boundHash, &boundMAC, &currentID, &currentHash,
		&currentMAC); err != nil {
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
	st.bound = auditTailBinding{valid: boundID.Valid, id: boundID.Int64,
		hash: boundHash.String, mac: boundMAC.String}
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

// primaryTailVerifiesUnder reports whether the primary anchor's HMAC
// recomputes under key: as a current-key HMAC over the anchor it is
// bound to, for one promoted from the current-key slot, and as a
// primary HMAC otherwise.
func primaryTailVerifiesUnder(key []byte, st auditTailState) bool {
	if !st.bound.valid {
		return auditTailMACVerifies(key, st.anchorID, st.anchorHash,
			st.anchorMAC)
	}

	want, err := auditTailCurrentMAC(key, st.anchorID, st.anchorHash,
		st.bound.id, st.bound.hash, st.bound.mac)
	return err == nil && hmac.Equal([]byte(want), []byte(st.anchorMAC))
}

// currentTailVerifiesUnder reports whether the current-key anchor's
// HMAC recomputes under key, over the primary anchor as it now stands.
func currentTailVerifiesUnder(key []byte, st auditTailState) bool {
	want, err := auditTailCurrentMAC(key, st.currentID, st.currentHash,
		st.anchorID, st.anchorHash, st.anchorMAC)
	return err == nil && st.hasAnchor &&
		hmac.Equal([]byte(want), []byte(st.currentMAC))
}

// auditTailVerifies reports whether the primary anchor's HMAC
// recomputes under the store's key.
func (s *AuthStore) auditTailVerifies(st auditTailState) bool {
	return primaryTailVerifiesUnder(s.auditKey, st)
}

// currentTailVerifies reports whether the current-key anchor's HMAC
// recomputes under the store's key, over the primary anchor as it now
// stands.
func (s *AuthStore) currentTailVerifies(st auditTailState) bool {
	return currentTailVerifiesUnder(s.auditKey, st)
}

// writeAuditTailSlot writes one anchor slot as given, with bound as its
// binding columns, which only a promoted primary anchor sets.
func writeAuditTailSlot(tx *sql.Tx, slot int, id int64, hash,
	mac string, bound auditTailBinding) error {

	var boundID, boundHash, boundMAC any
	if bound.valid {
		boundID, boundHash, boundMAC = bound.id, bound.hash, bound.mac
	}
	if _, err := tx.Exec(auditUpsertTail, slot, id, hash, mac, boundID,
		boundHash, boundMAC); err != nil {
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

	return writeAuditTailSlot(tx, auditTailPrimary, ev.ID, ev.Hash, mac,
		auditTailBinding{})
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

	return writeAuditTailSlot(tx, auditTailCurrent, ev.ID, ev.Hash, mac,
		auditTailBinding{})
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

	return s.planAuditTailMoveAfterKeyChange(tx, before)
}

// planAuditTailMoveAfterKeyChange decides the moves planAuditTailMove
// leaves over, which follow a change of secret: the slot naming the
// newest row fails under the key in use. The row it names was written
// under the same secret as the anchor, so it must fail too; if it
// verifies, the anchor was altered, and nothing moves.
func (s *AuthStore) planAuditTailMoveAfterKeyChange(tx *sql.Tx,
	before auditTailState) (auditTailMove, error) {

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
//
// Starting or promoting the current-key anchor is logged as a warning
// to the server's log, since each means the secret has changed and the
// events written under the one before are protected only until the
// operator re-anchors with it (see verifyAuditTail). The line is
// written before the caller commits, so a transaction that then rolls
// back logs one for a move that did not happen; the next event repeats
// it.
func (s *AuthStore) applyAuditTailMove(tx *sql.Tx, move auditTailMove,
	before auditTailState, ev *AuditEvent) error {

	switch move {
	case auditTailAdvance:
		return s.writeAuditTail(tx, ev)
	case auditTailAdvanceCurrent:
		if !before.hasCurrent {
			log.Printf("[WARN] Audit log: event %d does not verify under "+
				"the server secret in use, so the secret has changed; "+
				"starting a second tail anchor at event %d. Run "+
				"'ai-dba-server -rechain-audit-log -previous-secret-file' "+
				"with the previous secret before the secret changes again",
				before.anchorID, ev.ID)
		}
		return s.writeAuditTailCurrent(tx, ev, before.anchorID,
			before.anchorHash, before.anchorMAC)
	case auditTailPromote:
		log.Printf("[WARN] Audit log: event %d does not verify under the "+
			"server secret in use, so the secret has changed again since "+
			"the last re-anchor; moving the second tail anchor into the "+
			"first and starting it again at event %d. Events written under "+
			"the secrets before can no longer be proven by one previous "+
			"secret: run 'ai-dba-server -rechain-audit-log' and review the "+
			"plan", before.currentID, ev.ID)
		if err := writeAuditTailSlot(tx, auditTailPrimary, before.currentID,
			before.currentHash, before.currentMAC, auditTailBinding{
				valid: true, id: before.anchorID, hash: before.anchorHash,
				mac: before.anchorMAC}); err != nil {
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

// expectedTableSQL is ddl, a CREATE TABLE IF NOT EXISTS statement
// opened by comments, as SQLite records it in sqlite_master, normalised
// for comparison: SQLite keeps the statement from CREATE onwards, less
// the IF NOT EXISTS clause and the terminating semicolon.
func expectedTableSQL(ddl string) string {
	text := normalizeSchemaSQL(ddl)
	if i := strings.Index(text, "CREATE TABLE"); i >= 0 {
		text = text[i:]
	}

	return strings.Replace(text, "CREATE TABLE IF NOT EXISTS ",
		"CREATE TABLE ", 1)
}

// readAuditTailDefinition reads the CREATE TABLE statement recorded for
// audit_tail, normalised for comparison.
func (s *AuthStore) readAuditTailDefinition() (string, error) {
	var definition string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master
         WHERE type = 'table' AND name = 'audit_tail'`).
		Scan(&definition); err != nil {
		return "", fmt.Errorf("failed to read the audit_tail definition: %w",
			err)
	}

	return normalizeSchemaSQL(definition), nil
}

// migrateAuditTailSchema rebuilds an audit_tail table that a
// pre-release build of the anchor created (auditTailLegacyDDLs) as
// auditTailDDL. A server running against one would fail every audited
// change once a rotation called for the second slot or a promotion for
// the binding columns. CHECK constraints cannot be altered in SQLite,
// so the table is renamed, created afresh and refilled, in one
// transaction that leaves the old table untouched if it fails. Any
// other definition is left alone, so ensureAuditSchema can call this on
// every open; verifyAuditTailSchema refuses one that is not auditTailDDL.
//
// The rename carries any trigger on audit_tail with it, and the drop
// then removes it, so ensureAuditSchema checks for triggers first:
// otherwise the migration would erase the evidence of one.
func (s *AuthStore) migrateAuditTailSchema() error {
	definition, err := s.readAuditTailDefinition()
	if err != nil {
		return err
	}
	legacy := false
	for _, ddl := range auditTailLegacyDDLs {
		legacy = legacy || definition == expectedTableSQL(ddl)
	}
	if !legacy {
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
		"ALTER TABLE audit_tail RENAME TO audit_tail_legacy",
		auditTailDDL,
		`INSERT INTO audit_tail (id, event_id, event_hash, mac)
         SELECT id, event_id, event_hash, mac FROM audit_tail_legacy`,
		"DROP TABLE audit_tail_legacy",
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("failed to migrate audit_tail to the current "+
				"definition: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit the audit_tail migration: %w",
			err)
	}
	committed = true

	return nil
}

// verifyAuditTailSchema checks that audit_tail is exactly the table
// auditTailDDL creates and that no trigger acts on it. The anchors mean
// what this file says only while the table holds what the server writes
// and nothing else changes it: a trigger could discard or rewrite every
// anchor write, or an altered definition refuse one, so that every
// audited change failed or no anchor ever moved. ensureAuditSchema
// calls it on every open, so the server refuses to start rather than
// run with either, and verifyAuditSchema calls it too.
func (s *AuthStore) verifyAuditTailSchema() error {
	if err := s.verifyAuditTailNoTrigger(); err != nil {
		return err
	}

	definition, err := s.readAuditTailDefinition()
	if err != nil {
		return err
	}
	if definition != expectedTableSQL(auditTailDDL) {
		return errors.New("audit chain unprotected: the audit_tail table " +
			"does not match the definition this server creates, so the " +
			"tail anchors it holds may not be what the server wrote")
	}

	return nil
}

// verifyAuditTailNoTrigger refuses any trigger on audit_tail, or that
// names it, since this server creates none.
func (s *AuthStore) verifyAuditTailNoTrigger() error {
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master
         WHERE type = 'trigger'
           AND (tbl_name = 'audit_tail' COLLATE NOCASE
                OR sql LIKE '%audit_tail%')
         ORDER BY name LIMIT 1`).Scan(&name)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("failed to look for triggers on audit_tail: %w", err)
	}

	return fmt.Errorf("audit chain unprotected: the trigger %q acts on "+
		"audit_tail, which this server gives no trigger, so the tail "+
		"anchors may have been discarded or rewritten as they were "+
		"written; drop it once you have found out who created it", name)
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

	return s.checkRotatedAuditTail(st, lastFailing)
}

// checkRotatedAuditTail applies the rules verifyAuditTailAfterKeyChange
// describes to st, the anchors of a log whose rows through lastFailing
// fail under the key in use.
func (s *AuthStore) checkRotatedAuditTail(st auditTailState,
	lastFailing AuditEvent) error {

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

// proveAuditReanchorTail checks, under previousKey, the anchor the
// previous secret left behind: it must verify under previousKey and
// name the last row the re-anchor would accept as history, through,
// whose hash is throughHash. That is where the previous secret left it,
// since nothing moves it on under another. Which anchor that is depends
// on the state the log is in:
//
//   - a current-key anchor that fails under the key in use was written
//     under the previous secret, which has changed again since without
//     an event being written under the new one, so it is that anchor,
//     checked bound to the primary anchor beside it;
//   - otherwise it is the primary anchor, checked as a current-key
//     anchor bound to the one it was promoted beside where it was
//     promoted (primaryTailVerifiesUnder), so that a value planted in
//     the current-key slot and then promoted is refused like one
//     planted in the primary slot directly.
//
// A primary anchor that is missing or verifies under the key in use,
// with no failing current-key anchor beside it, needs no proof. The
// returned error is the reason the proof fails, or nil when it holds; a
// failure to read the anchors is returned as the second value instead.
func (s *AuthStore) proveAuditReanchorTail(q auditRowQuerier,
	previousKey []byte, through int64, throughHash string) (proof error,
	err error) {

	st, err := readAuditTailState(q)
	if err != nil {
		return nil, err
	}

	slot, id, hash := "tail anchor", st.anchorID, st.anchorHash
	switch {
	case st.hasCurrent && !s.currentTailVerifies(st):
		slot, id, hash = "tail anchor for the newest events", st.currentID,
			st.currentHash
		if !currentTailVerifiesUnder(previousKey, st) {
			return fmt.Errorf("%w: the %s names event %d, but verifies "+
				"under neither secret", errAuditTailUnproven, slot, id), nil
		}
	case !st.hasAnchor || s.auditTailVerifies(st):
		return nil, nil
	case !primaryTailVerifiesUnder(previousKey, st):
		return fmt.Errorf("%w: the %s names event %d, but verifies under "+
			"neither secret", errAuditTailUnproven, slot, id), nil
	}

	if id != through || hash != throughHash {
		return fmt.Errorf("%w: the %s names event %d, but the last event "+
			"written under that secret is now %d; events written under it "+
			"have been deleted from the end of the log",
			errAuditTailUnproven, slot, id, through), nil
	}

	return nil, nil
}
