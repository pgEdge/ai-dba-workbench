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
	"errors"
	"fmt"
)

// AuditVerifyReport is what VerifyAuditLog found.
type AuditVerifyReport struct {
	// Events is the number of rows examined.
	Events int

	// FirstBad is the id of the first row that failed, or 0. It means
	// the log verified only when the error is nil, because a failure to
	// read the log also reports 0.
	FirstBad int64

	// HistoryEvents is how many of the rows were accepted as history by
	// a re-chain, and so were checked against its digest rather than
	// under the key; HistoryAnchorID is the id of the event that
	// accepted them. Both are zero when there are none.
	HistoryEvents   int64
	HistoryAnchorID int64
}

// VerifyAuditLog is VerifyAuditChain, reporting also how much of the
// log a re-chain accepted as history, which a caller showing the result
// to an operator should say: those rows verified against a digest taken
// when they were accepted, which proves nothing about them before then.
func (s *AuthStore) VerifyAuditLog() (AuditVerifyReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.verifyAuditLog()
}

// verifyAuditLog is VerifyAuditLog for a caller already holding s.mu.
func (s *AuthStore) verifyAuditLog() (AuditVerifyReport, error) {
	var report AuditVerifyReport

	// A dropped index or trigger is strong evidence of tampering, not a
	// configuration mishap: ensureAuditSchema re-creates both on every
	// open, so their absence means they were removed since the last
	// one. It carries the tampering sentinel so a monitor keyed on it
	// sees this too.
	if err := s.verifyAuditSchema(); err != nil {
		return report, fmt.Errorf("%w: %w", ErrAuditChainBroken, err)
	}

	// The anchor is found first because it decides which rows are
	// history, and those come first in the log. An anchor that cannot
	// be read or does not verify grants nothing: the walk then holds
	// every row to the key, which is stricter, and reports whichever row
	// fails.
	anchor, err := s.findAuditAnchor(s.db)
	if err != nil {
		anchor = auditAnchorEvent{}
	}
	walk := auditChainWalk{
		s:       s,
		anchor:  anchor.record,
		history: newAuditHistoryDigest(),
	}
	if anchor.record.hasHistory() {
		report.HistoryEvents = anchor.record.HistoryEvents
		report.HistoryAnchorID = anchor.event.ID
	}

	err = walk.run()
	report.Events = walk.count
	report.FirstBad = walk.firstBad
	if errors.Is(err, errAuditRowUnverified) {
		return report, s.classifyUnverifiedRow(&walk)
	}
	if err != nil {
		return report, err
	}

	if anchor.record.hasHistory() && !walk.history.matches(anchor.record) {
		report.FirstBad = anchor.event.ID
		return report, errAuditHistoryChanged(&anchor.event, anchor.record)
	}

	if firstBad, err := walk.head.check(); err != nil {
		report.FirstBad = firstBad
		return report, err
	}

	if err := s.verifyAuditTail(); err != nil {
		report.FirstBad = 0
		return report, err
	}

	report.FirstBad = 0
	return report, nil
}

// classifyUnverifiedRow reports a row whose hash did not recompute
// under the key. Where the log from its start, or from the end of the
// history, has the shape a changed server secret leaves, it is reported
// as a key mismatch; anything else is tampering.
func (s *AuthStore) classifyUnverifiedRow(walk *auditChainWalk) error {
	var after *int64
	if walk.anchor.hasHistory() {
		after = walk.anchor.HistoryThroughID
	}

	keyChange := false
	if !walk.keyProven {
		var err error
		keyChange, err = s.looksLikeKeyChange(s.db, after)
		if err != nil {
			return err
		}
	}
	if keyChange {
		return fmt.Errorf("%w: audit row %d does not verify under the key "+
			"in use, and neither does any row before it, whilst every row "+
			"after them that was checked does. That is what a server "+
			"secret changed or replaced since those rows were written "+
			"looks like, rather than tampering: check that the server "+
			"secret file is the one these rows were written under. If the "+
			"secret was changed deliberately, or the old one is lost, run "+
			"'ai-dba-server -rechain-audit-log' to accept the older rows "+
			"as history", ErrAuditKeyMismatch, walk.firstBad)
	}

	return fmt.Errorf("%w at row %d", ErrAuditChainBroken, walk.firstBad)
}

// auditChainWalk is VerifyAuditLog's pass over the rows, in id order.
type auditChainWalk struct {
	s      *AuthStore
	anchor auditAnchor

	// history digests the rows the anchor accepted as history.
	history *auditHistoryDigest
	head    auditHeadCheck

	count          int
	firstBad       int64
	prevHash       string
	first          bool
	highestVersion int

	// keyProven records that at least one row outside the history has
	// verified under the key in use, which is what separates a log
	// written under a different secret from a log that has been
	// altered.
	keyProven bool
}

// run reads every row and checks it, stopping at the first failure. The
// rows are closed before it returns, so the caller may query again.
func (w *auditChainWalk) run() error {
	rows, err := w.s.db.Query(auditSelectInIDOrder)
	if err != nil {
		return fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	w.first = true
	for rows.Next() {
		ev, err := scanAuditEvent(rows.Scan)
		if err != nil {
			return fmt.Errorf("failed to scan audit event: %w", err)
		}
		w.count++

		if err := w.step(&ev); err != nil {
			return err
		}
		w.prevHash = ev.Hash
		w.first = false
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read audit events: %w", err)
	}

	return nil
}

// step checks one row. A row accepted as history only goes into the
// digest; its hash and its link were accepted as they stood when the
// re-chain ran, and the digest shows whether they have changed since.
// Every other row must link to the row before it, history included, and
// verify under the key.
func (w *auditChainWalk) step(ev *AuditEvent) error {
	if w.anchor.inHistory(ev.ID) {
		w.history.add(ev)
		w.head.noteOldest(ev)
		return nil
	}

	if !w.first && ev.PrevHash != w.prevHash {
		w.firstBad = ev.ID
		return fmt.Errorf("%w at row %d", ErrAuditChainBroken, ev.ID)
	}
	if ev.HashVersion < w.highestVersion {
		w.firstBad = ev.ID
		return fmt.Errorf("%w at row %d: version %d after version %d",
			ErrAuditChainDowngraded, ev.ID, ev.HashVersion, w.highestVersion)
	}

	want, err := auditHash(ev, w.s.auditKey)
	if err != nil {
		// The row names a rendering this store will not compute: the
		// unkeyed version 1 digest, which anyone able to write the file
		// can produce; a hash_version no release ever wrote; or the
		// keyed rendering on a store opened without a key, which
		// NewAuthStore no longer permits. All three are reported as
		// tampering, so that a monitor watching for it sees a row
		// relabelled out of reach of the verifier.
		w.firstBad = ev.ID
		return fmt.Errorf("%w: audit row %d, written under hash version %d, "+
			"cannot be verified by this store: %w",
			ErrAuditChainBroken, ev.ID, ev.HashVersion, err)
	}
	if want != ev.Hash {
		w.firstBad = ev.ID
		return fmt.Errorf("event %d %w", ev.ID, errAuditRowUnverified)
	}
	w.keyProven = true

	if err := w.head.observe(ev); err != nil {
		w.firstBad = ev.ID
		return err
	}
	w.highestVersion = ev.HashVersion

	return nil
}
