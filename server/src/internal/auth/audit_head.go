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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
)

// The head of the audit log, its oldest surviving row, is the one place
// the hash chain cannot vouch for, because the retention purge
// legitimately removes the rows before it and so leaves its prev_hash
// pointing at nothing. What follows accounts for the head instead.
//
// Every purge that removes events records, in its own keyed audit.purge
// event, the id and hash of the row it left as the oldest. Rows are
// deleted by nothing but the purge, so until the next purge the oldest
// row must be exactly that one, and VerifyAuditChain checks it is: a
// deletion from the start of the log after the last purge leaves a
// different row at the head, and the record of the right one cannot be
// altered without the server secret.
//
// That record would be worthless if an attacker could have the purge
// write a fresh one over their deletion, which one INSERT would do: a
// forged row at a low id with an old timestamp is itself purged at the
// next tick, and a purge that removed anything records a new head. So
// the purge also verifies what it is about to delete before it deletes
// it. The prefix must begin where the previous purge left the head, or
// at the genesis row when there has been no purge, every row in it must
// carry a hash that verifies under the key, and each must link to the
// one before, through to the row that becomes the new head. A purge
// that finds anything else refuses and deletes nothing.
//
// A purge that refuses stays refused, since nothing it can do on its
// own clears the cause. The operator's way out is -rechain-audit-log,
// which appends a keyed audit.rechain event recording the head as it
// now stands, in the same fields a purge uses. Where some of the oldest
// rows do not verify under the key in use, as after the server secret
// has been changed, that event also accepts them as history: the rows
// up to a named id, bound by a digest of their contents. The verifier
// and the purge then check those rows against the digest instead of the
// key. That is weaker than the key, and deliberately visible: a digest
// shows that a row has not changed since the re-chain, and nothing at
// all about what happened to it before.
//
// Purge and re-chain events are the anchors of the log. The newest one
// that records a head is the one that counts, and an anchor recording
// nothing never replaces an older one that does, because a copy of a
// genuine early event replayed into the log would otherwise reopen the
// weaker checks that applied before the record existed.
//
// None of this re-signs an existing row. The purge and the re-chain
// verify rows and record one row's existing hash, and a digest of
// others, as values inside events they create themselves, and the
// verifier still checks every surviving row, so the record exempts
// nothing. That distinction is the one PurgeAuditEvents sets out: an
// unattended path that re-signs rows can be steered into signing a
// forgery under the real key, whereas this one can at worst be made to
// refuse.

// auditAnchor is what an anchor event records about the log it left
// behind. Every field is absent from purge events written by builds that
// predate the record, which the checks below treat as saying nothing
// about the head rather than as a mismatch.
type auditAnchor struct {
	OldestRetainedID   *int64 `json:"oldest_retained_id,omitempty"`
	OldestRetainedHash string `json:"oldest_retained_hash,omitempty"`

	// HistoryThroughID, HistoryEvents and HistoryDigest describe the
	// rows a re-chain accepted without the key: every row with an id up
	// to HistoryThroughID, how many of them remain, and the digest of
	// their contents in id order. They are absent when there are none.
	HistoryThroughID *int64 `json:"history_through_id,omitempty"`
	HistoryEvents    int64  `json:"history_events,omitempty"`
	HistoryDigest    string `json:"history_digest,omitempty"`
}

// recordsHead reports whether the anchor names the oldest row.
func (a auditAnchor) recordsHead() bool { return a.OldestRetainedHash != "" }

// hasHistory reports whether the anchor accepts any rows as history.
func (a auditAnchor) hasHistory() bool {
	return a.HistoryThroughID != nil && a.HistoryDigest != ""
}

// inHistory reports whether the row with this id is one the anchor
// accepts as history.
func (a auditAnchor) inHistory(id int64) bool {
	return a.hasHistory() && id <= *a.HistoryThroughID
}

// isAuditAnchorAction reports whether an action is one whose events may
// record the head.
func isAuditAnchorAction(action string) bool {
	return action == auditActionPurge || action == auditActionRechain
}

// parseAuditAnchor reads the record from an anchor event's details. An
// event with no details, or none of the fields, returns an empty
// record.
func parseAuditAnchor(ev *AuditEvent) (auditAnchor, error) {
	var anchor auditAnchor
	if len(ev.Details) == 0 {
		return anchor, nil
	}
	if err := json.Unmarshal(ev.Details, &anchor); err != nil {
		return anchor, fmt.Errorf("failed to read the details of %s "+
			"event %d: %w", ev.Action, ev.ID, err)
	}

	return anchor, nil
}

// auditHistoryLabel opens the rendering of each row in a history
// digest, so that it can never be mistaken for a row hash.
const auditHistoryLabel = "pgedge-ai-workbench/audit-history/v1"

// auditHistoryDigest accumulates the digest of a run of rows, in id
// order. It is an unkeyed SHA-256, which is enough because the value
// is only ever trusted from inside an anchor event whose own hash is
// keyed. Each row contributes its canonical rendering and its stored
// hash, so a change to any column the row hash covers, or to the hash
// itself, changes the digest.
type auditHistoryDigest struct {
	h      hash.Hash
	events int64
}

func newAuditHistoryDigest() *auditHistoryDigest {
	return &auditHistoryDigest{h: sha256.New()}
}

func (d *auditHistoryDigest) add(ev *AuditEvent) {
	d.h.Write(canonicalFields(string(auditCanonical(auditHistoryLabel, ev)),
		ev.Hash))
	d.events++
}

func (d *auditHistoryDigest) sum() string {
	return hex.EncodeToString(d.h.Sum(nil))
}

// matches reports whether the rows added so far are the ones the anchor
// accepted as history.
func (d *auditHistoryDigest) matches(a auditAnchor) bool {
	return d.events == a.HistoryEvents && d.sum() == a.HistoryDigest
}

// errAuditHistoryChanged is the message for a history that no longer
// matches its digest, shared by the verifier and the purge.
func errAuditHistoryChanged(anchor *AuditEvent, rec auditAnchor) error {
	return fmt.Errorf("%w: the %d event(s) up to event %d that %s event "+
		"%d accepted as history have changed since; events accepted by a "+
		"re-chain have been altered, deleted or added to",
		ErrAuditChainBroken, rec.HistoryEvents, *rec.HistoryThroughID,
		anchor.Action, anchor.ID)
}

// auditHeadCheck accumulates, during VerifyAuditChain's walk, what it
// needs to decide whether the head of the log is accounted for. observe
// must be called only with rows that have already verified, since an
// anchor's record is worth something only because its hash does;
// noteOldest takes the rows accepted as history, which say nothing.
type auditHeadCheck struct {
	seenRows bool
	oldest   AuditEvent

	// purgeSeen records that some audit.purge event survives, which is
	// all a build that predates the head record left to go on.
	purgeSeen bool
	// anchorEvent and anchor describe the newest anchor that records a
	// head, and are empty while none has been seen. A later anchor
	// without a record does not replace them: once a head has been
	// recorded, only a later record may move it.
	anchorEvent AuditEvent
	anchor      auditAnchor
}

// noteOldest takes note of a row without trusting anything it says.
func (h *auditHeadCheck) noteOldest(ev *AuditEvent) {
	if !h.seenRows {
		h.seenRows = true
		h.oldest = *ev
	}
}

// observe takes note of one verified row, in id order.
func (h *auditHeadCheck) observe(ev *AuditEvent) error {
	h.noteOldest(ev)
	if !isAuditAnchorAction(ev.Action) {
		return nil
	}

	anchor, err := parseAuditAnchor(ev)
	if err != nil {
		return err
	}
	if ev.Action == auditActionPurge {
		h.purgeSeen = true
	}
	if anchor.recordsHead() {
		h.anchorEvent = *ev
		h.anchor = anchor
	}

	return nil
}

// check reports whether the oldest row is accounted for, returning the
// id of the oldest row alongside an error when it is not.
func (h *auditHeadCheck) check() (int64, error) {
	if !h.seenRows {
		return 0, nil
	}

	if h.anchor.recordsHead() {
		if h.oldest.Hash == h.anchor.OldestRetainedHash {
			return 0, nil
		}
		return h.oldest.ID, fmt.Errorf(
			"%w: audit log head missing: %s event %d recorded event %s as "+
				"the oldest, but the oldest event is now %d; events have "+
				"been deleted from the start of the log since then",
			ErrAuditChainBroken, h.anchorEvent.Action, h.anchorEvent.ID,
			auditIDString(h.anchor.OldestRetainedID), h.oldest.ID)
	}

	// No surviving anchor records the head, either because no purge
	// has run since this build was installed or because none has ever
	// run. A non-empty prev_hash on the oldest row means a predecessor
	// once existed, and without any purge event to account for its
	// removal that is evidence of deletion.
	if h.oldest.PrevHash != "" && !h.purgeSeen {
		return h.oldest.ID, fmt.Errorf(
			"%w: audit log head missing: the oldest event, %d, follows an "+
				"event that is no longer in the log, and no audit.purge "+
				"event accounts for its removal",
			ErrAuditChainBroken, h.oldest.ID)
	}

	return 0, nil
}

// auditSelectAnchorsNewestFirst reads the anchor events, newest first,
// so that the newest head recorded can be found.
const auditSelectAnchorsNewestFirst = auditSelectAll +
	" WHERE action IN (?, ?) ORDER BY id DESC"

// auditSelectThroughID reads every row up to and including an id, in id
// order: the prefix a purge is about to delete, the row it will leave
// as the new head, and any history retained beyond it.
const auditSelectThroughID = auditSelectAll + " WHERE id <= ? ORDER BY id"

// errAuditPurgeRefused prefixes every refusal by verifyAuditPurgePrefix,
// so that the server log names what it declined to do.
const errAuditPurgeRefused = "refusing to purge the audit log"

// errAuditAnchorUnverified reports that the newest anchor event does
// not verify under the key, so nothing it records can be used.
var errAuditAnchorUnverified = errors.New("does not verify under the key " +
	"in use")

// errAuditRowUnverified reports a row whose hash does not verify under
// the key. It never leaves this package's purge and verifier: each
// replaces it with ErrAuditKeyMismatch or ErrAuditChainBroken once it
// has judged which the failure looks like.
var errAuditRowUnverified = errors.New("does not verify under the key " +
	"in use")

// auditAnchorEvent is the newest anchor that records a head, as found
// by findAuditAnchor.
type auditAnchorEvent struct {
	found  bool
	event  AuditEvent
	record auditAnchor

	// purgeSeen reports that an audit.purge event was read before the
	// anchor was found, or anywhere when none was.
	purgeSeen bool
}

// findAuditAnchor returns the newest anchor event that records a head,
// reading newest first. Every anchor it reads on the way must verify
// under the key, since the record is trusted only for that: an event
// written without the key is not the server's word on anything. An
// anchor recording nothing is passed over without replacing anything.
// The rows are closed before it returns, so the caller can issue its
// next statement.
func (s *AuthStore) findAuditAnchor(q auditQuerier) (auditAnchorEvent,
	error) {

	var found auditAnchorEvent
	rows, err := q.Query(auditSelectAnchorsNewestFirst, auditActionPurge,
		auditActionRechain)
	if err != nil {
		return found, fmt.Errorf("failed to read the newest audit purge "+
			"event: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		ev, err := scanAuditEvent(rows.Scan)
		if err != nil {
			return found, fmt.Errorf("failed to read the newest audit purge "+
				"event: %w", err)
		}
		if ev.Action == auditActionPurge {
			found.purgeSeen = true
		}
		if !s.auditRowVerifies(&ev) {
			return found, fmt.Errorf("%s event %d %w", ev.Action, ev.ID,
				errAuditAnchorUnverified)
		}
		anchor, err := parseAuditAnchor(&ev)
		if err != nil {
			return found, err
		}
		if anchor.recordsHead() {
			found.found = true
			found.event = ev
			found.record = anchor
			return found, nil
		}
	}
	if err := rows.Err(); err != nil {
		return found, fmt.Errorf("failed to read the newest audit purge "+
			"event: %w", err)
	}

	return found, nil
}

// auditRowVerifies reports whether one row's hash recomputes under the
// store's key.
func (s *AuthStore) auditRowVerifies(ev *AuditEvent) bool {
	want, err := auditHash(ev, s.auditKey)
	return err == nil && want == ev.Hash
}

// errStopAuditWalk ends a walk early without reporting a failure.
var errStopAuditWalk = errors.New("stop the audit walk")

// looksLikeKeyChange reports whether the log, read from its start, has
// the shape a changed server secret leaves: one or more rows that do not verify
// under the key in use, each linked to the one before it, followed
// either by the end of the log or by rows that all verify and each link
// to the one before, with no rows lost from the tail. The server keeps
// writing after its secret changes, and each new row chains onto the
// last one written under the old secret, so that is exactly what a
// rotation looks like; a row forged or altered by someone without the
// key usually breaks a link on one side of it.
//
// Every row after the leading run is checked, not just the first,
// because the re-anchor this verdict can let run unattended accepts as
// history everything through the last row that fails anywhere in the
// log. A verdict drawn from the first few rows would let a rotation, or
// an edit to the oldest row that mimics one, carry a deletion or an
// alteration further on through with it.
//
// It is a judgement, not a proof. Anyone able to write auth.db can
// forge rows that fit the shape, so the message it leads to says
// "probably", and the re-chain it points at shows the operator exactly
// which rows it would accept. Its callers do not ask it at all when a
// purge or re-chain event that verifies records where the log begins:
// see classifyUnverifiedRow.
func (s *AuthStore) looksLikeKeyChange(q auditQuerier) (bool, error) {

	run := int64(0)
	var last AuditEvent
	verifying := false
	err := forEachAuditEvent(q, func(ev AuditEvent) error {
		if (run > 0 || verifying) && ev.PrevHash != last.Hash {
			return errStopAuditWalk
		}
		last = ev
		if s.auditRowVerifies(&ev) {
			if run == 0 {
				// The first row verifies, so whatever failed, it was
				// not the oldest rows.
				return errStopAuditWalk
			}
			verifying = true
			return nil
		}
		if verifying {
			// A row failing after rows that verified is not what a
			// changed secret leaves behind.
			return errStopAuditWalk
		}
		run++
		return nil
	})
	if errors.Is(err, errStopAuditWalk) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if run == 0 {
		return false, nil
	}

	// A rotation loses nothing from the tail, so a log that has also
	// lost rows there is not explained by one.
	if err := s.verifyAuditTail(); err != nil {
		if errors.Is(err, ErrAuditChainBroken) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// auditPurgeCut is what verifyAuditPurgePrefix finds: the row the purge
// will leave as the head, and the part of any history that survives it.
type auditPurgeCut struct {
	newHead AuditEvent

	// history is set when rows accepted as history survive the purge,
	// with the id, count and digest of those that remain.
	history *auditAnchor
}

// verifyAuditPurgePrefix checks the rows a purge is about to delete,
// every row with an id below cut, and the row at cut that will become
// the head, together with any rows accepted as history beyond it. It
// reads inside the purge's transaction, which holds the write lock, so
// the rows it checks are the rows the DELETE removes.
func (s *AuthStore) verifyAuditPurgePrefix(tx *sql.Tx, cut int64) (
	auditPurgeCut, error) {

	anchor, err := s.findAuditAnchor(tx)
	if errors.Is(err, errAuditAnchorUnverified) {
		return auditPurgeCut{}, s.auditPurgeUnverified(tx, err)
	}
	if err != nil {
		return auditPurgeCut{}, err
	}
	if anchor.found {
		if err := checkAuditAnchorLinks(tx, &anchor.event); err != nil {
			return auditPurgeCut{}, err
		}
	}

	walk := auditPrefixWalk{
		s:       s,
		anchor:  anchor.record,
		genesis: !anchor.found && !anchor.purgeSeen,
		cut:     cut,
		all:     newAuditHistoryDigest(),
		kept:    newAuditHistoryDigest(),
	}
	if err := walk.run(tx); err != nil {
		if errors.Is(err, errAuditRowUnverified) {
			return auditPurgeCut{}, s.auditPurgeUnverified(tx, err)
		}
		return auditPurgeCut{}, err
	}

	return walk.result(&anchor.event)
}

// auditPurgeUnverified turns a row the purge found not verifying into
// its refusal, judging from the head of the log whether it looks like a
// changed server secret or like tampering. The two are told apart
// because they call for different responses, and an operator told the
// log had been tampered with every time a secret was rotated would soon
// stop believing it.
func (s *AuthStore) auditPurgeUnverified(q auditQuerier, cause error) error {
	keyChange, err := s.looksLikeKeyChange(q)
	if err != nil {
		return err
	}
	if keyChange {
		return fmt.Errorf("%w: %s: %v. The oldest events do not verify "+
			"under the key in use and every one after them does, "+
			"which is what a server secret changed or "+
			"replaced since they were written looks like, rather than "+
			"tampering. Confirm secret_file; if the secret was changed "+
			"deliberately or is lost, run 'ai-dba-server "+
			"-rechain-audit-log' to accept the older events as history",
			ErrAuditKeyMismatch, errAuditPurgeRefused, cause)
	}

	return fmt.Errorf("%w: %s: %v; run 'ai-dba-server -verify-audit-log'",
		ErrAuditChainBroken, errAuditPurgeRefused, cause)
}

// checkAuditAnchorLinks refuses an anchor that is not linked into the
// chain around it: the row before it must be the one its prev_hash
// names, and the row after it, if any, must name it. A copy of a
// genuine older anchor, replayed into the log to have the purge start
// from a head it recorded long ago, verifies on its own but cannot sit
// in the chain like that without breaking a link beside it.
func checkAuditAnchorLinks(tx *sql.Tx, ev *AuditEvent) error {
	var before string
	err := tx.QueryRow(`SELECT hash FROM audit_events WHERE id < ?
        ORDER BY id DESC LIMIT 1`, ev.ID).Scan(&before)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to read the event before %s event %d: %w",
			ev.Action, ev.ID, err)
	}
	linked := errors.Is(err, sql.ErrNoRows) || before == ev.PrevHash

	var after string
	err = tx.QueryRow(`SELECT prev_hash FROM audit_events WHERE id > ?
        ORDER BY id LIMIT 1`, ev.ID).Scan(&after)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to read the event after %s event %d: %w",
			ev.Action, ev.ID, err)
	}
	linked = linked && (errors.Is(err, sql.ErrNoRows) || after == ev.Hash)

	if !linked {
		return fmt.Errorf("%w: %s: %s event %d, which records where the "+
			"log begins, is not linked into the chain beside it; it may be "+
			"a copy of an earlier event replayed into the log",
			ErrAuditChainBroken, errAuditPurgeRefused, ev.Action, ev.ID)
	}

	return nil
}

// auditPrefixWalk checks the rows a purge reads, in id order.
type auditPrefixWalk struct {
	s       *AuthStore
	anchor  auditAnchor
	genesis bool
	cut     int64

	// all digests every row accepted as history, and kept those that
	// survive the purge.
	all  *auditHistoryDigest
	kept *auditHistoryDigest

	prev      AuditEvent
	seen      bool
	newHead   AuditEvent
	headFound bool
}

// readThrough is the highest id the walk reads: the new head, or the
// last row accepted as history when that is later, since the digest
// covers every such row.
func (w *auditPrefixWalk) readThrough() int64 {
	if w.anchor.hasHistory() && *w.anchor.HistoryThroughID > w.cut {
		return *w.anchor.HistoryThroughID
	}

	return w.cut
}

// run reads and checks the rows. They are read in a single statement
// and fully consumed, and the statement closed, before it returns.
func (w *auditPrefixWalk) run(tx *sql.Tx) error {
	rows, err := tx.Query(auditSelectThroughID, w.readThrough())
	if err != nil {
		return fmt.Errorf("failed to read the audit events to purge: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		ev, err := scanAuditEvent(rows.Scan)
		if err != nil {
			return fmt.Errorf("failed to scan an audit event to purge: %w",
				err)
		}
		if err := w.step(&ev); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read the audit events to purge: %w", err)
	}

	return nil
}

// step checks one row. A row accepted as history goes into the digest
// and is otherwise taken as it stands; any other row must verify under
// the key and link to the row before it. The first row must also be
// where the purge is allowed to begin.
func (w *auditPrefixWalk) step(ev *AuditEvent) error {
	if w.anchor.inHistory(ev.ID) {
		w.all.add(ev)
		if ev.ID >= w.cut {
			w.kept.add(ev)
		}
	} else if err := w.s.checkAuditRowVerifies(ev); err != nil {
		return err
	}

	if err := w.checkPlace(ev); err != nil {
		return err
	}

	if ev.ID == w.cut {
		w.newHead = *ev
		w.headFound = true
	}
	w.prev = *ev
	w.seen = true

	return nil
}

// checkPlace checks a row is where it should be: the first row where
// the purge must begin, and any later row outside the history linked to
// the one before it.
func (w *auditPrefixWalk) checkPlace(ev *AuditEvent) error {
	start := w.anchor.OldestRetainedHash
	switch {
	case w.seen:
		if !w.anchor.inHistory(ev.ID) && ev.PrevHash != w.prev.Hash {
			return fmt.Errorf("%w: %s: event %d does not link to event %d "+
				"before it", ErrAuditChainBroken, errAuditPurgeRefused,
				ev.ID, w.prev.ID)
		}
	case start != "" && ev.Hash != start:
		return fmt.Errorf("%w: %s: the oldest event, %d, is not the one "+
			"the previous purge left as the oldest, so events have been "+
			"deleted from the start of the log", ErrAuditChainBroken,
			errAuditPurgeRefused, ev.ID)
	case w.genesis && ev.PrevHash != "":
		return fmt.Errorf("%w: %s: the oldest event, %d, follows an event "+
			"that is no longer in the log, and no audit.purge event "+
			"accounts for its removal", ErrAuditChainBroken,
			errAuditPurgeRefused, ev.ID)
	}

	return nil
}

// result checks what the walk saw as a whole, the new head and the
// history, and returns the cut.
func (w *auditPrefixWalk) result(anchor *AuditEvent) (auditPurgeCut, error) {
	if !w.headFound {
		// cut came from the same transaction, so the row is there
		// unless something is badly wrong; a purge that cannot find the
		// head it is meant to keep must not delete anything.
		return auditPurgeCut{}, fmt.Errorf("%s: event %d, which the purge "+
			"would keep as the oldest, could not be read",
			errAuditPurgeRefused, w.cut)
	}

	cut := auditPurgeCut{newHead: w.newHead}
	if !w.anchor.hasHistory() {
		return cut, nil
	}
	if !w.all.matches(w.anchor) {
		return auditPurgeCut{}, fmt.Errorf("%s: %w", errAuditPurgeRefused,
			errAuditHistoryChanged(anchor, w.anchor))
	}
	if w.kept.events > 0 {
		cut.history = &auditAnchor{
			HistoryThroughID: w.anchor.HistoryThroughID,
			HistoryEvents:    w.kept.events,
			HistoryDigest:    w.kept.sum(),
		}
	}

	return cut, nil
}

// checkAuditRowVerifies recomputes one row's hash under the store's
// key, for the purge's check of what it is about to delete. A row whose
// hash does not match returns errAuditRowUnverified, which the purge
// judges once it has finished reading; a row whose hash cannot be
// computed at all, under a version no release writes, is tampering.
func (s *AuthStore) checkAuditRowVerifies(ev *AuditEvent) error {
	want, err := auditHash(ev, s.auditKey)
	if err != nil {
		return fmt.Errorf("%w: %s: event %d cannot be verified: %w",
			ErrAuditChainBroken, errAuditPurgeRefused, ev.ID, err)
	}
	if want != ev.Hash {
		return fmt.Errorf("event %d %w", ev.ID, errAuditRowUnverified)
	}

	return nil
}
