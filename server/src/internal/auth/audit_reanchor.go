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

// A keyed log that stops verifying stops the retention purge with it,
// by design: audit_head.go explains why the purge refuses rather than
// delete what it cannot account for, and nothing the purge does on its
// own clears the refusal. The two ordinary causes are a server secret
// changed or lost since the older rows were written, and a head deleted
// or a row altered by someone able to write auth.db. The re-anchor is
// the operator's way out of both.
//
// It rewrites nothing. It appends one keyed audit.rechain event, which
// records the oldest row as where the log now begins, in the fields a
// purge uses, and accepts the oldest rows, through the last one that
// does not verify under the key in use or does not link to the row
// before it, as history, bound by a digest of their contents. The
// verifier and the purge treat that event as the newest anchor, and
// check the history against the digest instead of the key.
//
// What is given up for those rows is attribution. They are no longer
// shown to have been written by this server, and anything done to them
// before the re-anchor, by anyone, is accepted with them; the digest
// shows only that they have not changed since. That is why it needs an
// operator's confirmation, and why the plan shows what was found.

// AuditRechainMode says which re-chain a plan describes.
type AuditRechainMode string

const (
	// AuditRechainRehash re-hashes a log inherited from a release that
	// predates the keyed chain.
	AuditRechainRehash AuditRechainMode = "rehash"

	// AuditRechainReanchor records a new starting point for a keyed log
	// that no longer verifies, and rewrites nothing.
	AuditRechainReanchor AuditRechainMode = "reanchor"
)

// AuditRechainHead is where an existing purge or re-chain event said the
// log began.
type AuditRechainHead struct {
	// EventID and Action identify the event.
	EventID int64
	Action  string

	// HeadID and HeadHash are the oldest row it recorded; HeadID is
	// zero if it recorded no id.
	HeadID   int64
	HeadHash string

	// Verified reports whether the event itself verifies under the key
	// in use. The plan shows an event that does not, since it is what
	// the operator is replacing, but nothing ever trusts it.
	Verified bool
}

// auditReanchorScan is what one pass over the log finds for a
// re-anchor: the oldest row, and the history the re-anchor would
// accept.
type auditReanchorScan struct {
	events   int64
	headID   int64
	headHash string

	// through is the id of the last row that does not verify or does
	// not link to its predecessor, and throughHash its hash, valid only
	// when historyEvents is above zero; digest covers every row up to
	// it.
	through       int64
	throughHash   string
	historyEvents int64
	digest        string

	// tail is the tail anchor as the scan found it, so that the
	// transaction that writes the re-anchor sees whether it has changed
	// since the plan checked it.
	tail auditTailState

	// failsAfterVerified records that a row failed after an earlier row
	// had verified and linked, which a changed server secret does not
	// produce.
	failsAfterVerified bool
}

// scanAuditForReanchor reads the whole log, in id order, and finds the
// rows a re-anchor would accept as history.
func (s *AuthStore) scanAuditForReanchor(q auditRowQuerier) (auditReanchorScan,
	error) {

	var scan auditReanchorScan
	digest := newAuditHistoryDigest()
	prevHash := ""
	verifiedBeyond := false
	err := forEachAuditEvent(q, func(ev AuditEvent) error {
		if scan.events == 0 {
			scan.headID = ev.ID
			scan.headHash = ev.Hash
		}
		bad := !s.auditRowVerifies(&ev) ||
			(scan.events > 0 && ev.PrevHash != prevHash)

		if !bad {
			verifiedBeyond = true
		} else if verifiedBeyond {
			scan.failsAfterVerified = true
		}

		digest.add(&ev)
		if bad {
			scan.through = ev.ID
			scan.throughHash = ev.Hash
			scan.historyEvents = digest.events
			scan.digest = digest.sum()
		}
		prevHash = ev.Hash
		scan.events++

		return nil
	})
	if err != nil {
		return scan, err
	}
	if scan.tail, err = readAuditTailState(q); err != nil {
		return scan, err
	}

	return scan, nil
}

// auditReanchorPlan fills in the re-anchor's part of the plan, and is
// called with s.mu held.
func (s *AuthStore) auditReanchorPlan(plan *AuditRechainPlan) error {
	// A missing or altered index or trigger is not something a new
	// starting point can account for: the guarantees the anchor rests
	// on depend on both. Every open re-creates them, so this is seen
	// only when something removed them in the meantime.
	if err := s.verifyAuditSchema(); err != nil {
		return fmt.Errorf("refusing to re-chain the audit log: %w", err)
	}

	_, problem := s.verifyAuditLog()
	if problem != nil && !errors.Is(problem, ErrAuditChainBroken) &&
		!errors.Is(problem, ErrAuditKeyMismatch) &&
		!errors.Is(problem, ErrAuditChainDowngraded) {

		// The verifier could not read the log, which says nothing about
		// what is in it; a starting point recorded now would rest on
		// nothing.
		return fmt.Errorf("failed to verify the audit log before the "+
			"re-chain: %w", problem)
	}
	plan.Problem = problem
	plan.KeyMismatch = errors.Is(problem, ErrAuditKeyMismatch)
	if problem == nil {
		return nil
	}

	scan, err := s.scanAuditForReanchor(s.db)
	if err != nil {
		return err
	}
	// An empty log that fails verification has had its events deleted,
	// and leaves no event for a starting point to record, so the
	// re-anchor would start a chain of its own with an empty prev_hash.
	// Such an event, kept and put back after a later wipe, would verify
	// wherever it stood, so none is written. The first event the server
	// writes gives a later re-anchor something to record.
	if scan.events == 0 {
		return fmt.Errorf("refusing to re-chain the audit log, which "+
			"fails verification (%w) but holds no events, so there is no "+
			"event for a new starting point to record. Restore auth.db "+
			"from a known-good copy, or re-chain again once the server "+
			"has written an event", problem)
	}
	// The verifier read the log before this scan did, and a writer in
	// between could have added a failing row that the scan would accept
	// as history. -confirm-rechain trusts KeyMismatch only together
	// with the evidence proveAuditReanchorPlan gathers, so it must
	// describe the rows the re-anchor accepts, not the ones the verifier
	// saw.
	if scan.failsAfterVerified {
		plan.KeyMismatch = false
	}
	plan.reanchor = scan
	plan.HeadID = scan.headID
	plan.HeadHash = scan.headHash
	plan.HistoryEvents = scan.historyEvents
	plan.HistoryThroughID = scan.through

	previous, err := s.previousAuditHead()
	if err != nil {
		return err
	}
	plan.PreviousHead = previous

	return nil
}

// proveAuditReanchorPlan fills in the plan's evidence for a re-anchor:
// whether any row after the history verifies under the key in use, and,
// given the previous key, whether the history is what that key wrote.
// It is called with s.mu held, after auditReanchorPlan. The transaction
// that writes the re-anchor repeats the scan and refuses if a row it
// covers has changed, so the proof still holds when the event is signed.
func (s *AuthStore) proveAuditReanchorPlan(plan *AuditRechainPlan,
	previousKey []byte) error {

	if plan.Problem == nil {
		return nil
	}
	plan.LaterEventsVerify = plan.reanchor.events >
		plan.reanchor.historyEvents
	if len(previousKey) == 0 {
		return nil
	}
	plan.PreviousKeyGiven = true
	if plan.reanchor.historyEvents == 0 {
		plan.HistoryProofErr = errors.New("no event would be accepted as " +
			"history, so there is nothing for the previous secret to prove")
		return nil
	}

	proof, err := s.proveAuditHistory(s.db, previousKey,
		plan.reanchor.through)
	if err != nil {
		return err
	}
	// The history is not proven while the tail anchor the previous
	// secret left behind it does not show that nothing written under
	// that secret is missing from its end.
	if proof == nil {
		proof, err = s.proveAuditReanchorTail(s.db, previousKey,
			plan.reanchor.through, plan.reanchor.throughHash)
		if err != nil {
			return err
		}
	}
	plan.HistoryProofErr = proof
	plan.HistoryProven = proof == nil

	return nil
}

// errAuditHistoryUnproven wraps each reason proveAuditHistory gives.
var errAuditHistoryUnproven = errors.New("the events that would be " +
	"accepted as history are not what the previous secret wrote")

// proveAuditHistory checks the rows through the last one a re-anchor
// would accept as history, in id order, under previousKey: each must
// verify under it, link to the row before it, and never go down in hash
// version, and the head must be accounted for exactly as the verifier
// requires of a log written under one key, so that a head deleted before
// the secret changed is not carried into history with the rest. The
// returned error is the reason the proof fails, or nil when it holds; a
// failure to read the log is returned as the second value instead.
func (s *AuthStore) proveAuditHistory(q auditQuerier, previousKey []byte,
	through int64) (proof error, err error) {

	var head auditHeadCheck
	var prev AuditEvent
	seen := false
	err = forEachAuditEvent(q, func(ev AuditEvent) error {
		if ev.ID > through {
			return errStopAuditWalk
		}
		if seen && ev.PrevHash != prev.Hash {
			return fmt.Errorf("%w: event %d does not link to event %d "+
				"before it", errAuditHistoryUnproven, ev.ID, prev.ID)
		}
		if seen && ev.HashVersion < prev.HashVersion {
			return fmt.Errorf("%w: event %d has a lower hash version than "+
				"event %d before it", errAuditHistoryUnproven, ev.ID, prev.ID)
		}
		if want, hashErr := auditHash(&ev, previousKey); hashErr != nil ||
			want != ev.Hash {

			return fmt.Errorf("%w: event %d does not verify under it",
				errAuditHistoryUnproven, ev.ID)
		}
		if obsErr := head.observe(&ev); obsErr != nil {
			return fmt.Errorf("%w: %w", errAuditHistoryUnproven, obsErr)
		}
		prev = ev
		seen = true

		return nil
	})
	switch {
	case errors.Is(err, errAuditHistoryUnproven):
		return err, nil
	case err != nil && !errors.Is(err, errStopAuditWalk):
		return nil, err
	}
	if _, headErr := head.check(); headErr != nil {
		return fmt.Errorf("%w: %w", errAuditHistoryUnproven, headErr), nil
	}

	return nil, nil
}

// previousAuditHead returns the newest anchor event that records a
// head, whether or not it verifies, for the plan to show.
func (s *AuthStore) previousAuditHead() (*AuditRechainHead, error) {
	rows, err := s.db.Query(auditSelectAnchorsNewestFirst, auditActionPurge,
		auditActionRechain)
	if err != nil {
		return nil, fmt.Errorf("failed to read the audit purge events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		ev, err := scanAuditEvent(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("failed to read the audit purge events: %w",
				err)
		}
		anchor, err := parseAuditAnchor(&ev)
		if err != nil || !anchor.recordsHead() {
			// A record that cannot be read is no record; the plan shows
			// the newest one that can be.
			continue
		}
		head := &AuditRechainHead{
			EventID:  ev.ID,
			Action:   ev.Action,
			HeadHash: anchor.OldestRetainedHash,
			Verified: s.auditRowVerifies(&ev),
		}
		if anchor.OldestRetainedID != nil {
			head.HeadID = *anchor.OldestRetainedID
		}
		return head, nil
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read the audit purge events: %w", err)
	}

	return nil, nil
}

// reanchorAuditLog is the re-anchor half of rechainAuditLog, called
// with s.mu held and the plan already taken.
func (s *AuthStore) reanchorAuditLog(actor Actor, plan AuditRechainPlan,
	confirm AuditRechainConfirm) (AuditRechainResult, error) {

	result := AuditRechainResult{Mode: AuditRechainReanchor}
	if plan.Problem == nil {
		result.UpToDate = true
		return result, nil
	}

	proceed, err := confirm(plan)
	if err != nil {
		return result, err
	}
	if !proceed {
		return result, nil
	}

	if err := s.reanchorAuditLogTx(actor, plan); err != nil {
		return result, err
	}
	result.Confirmed = true

	// The event is committed, and the log should now verify. If it does
	// not, the failure was of a kind a new starting point does not
	// account for, and the operator must hear so now rather than from
	// the next purge.
	if _, err := s.verifyAuditLog(); err != nil {
		return result, fmt.Errorf("the re-chain was recorded, but the audit "+
			"log still does not verify: %w", err)
	}

	return result, nil
}

// errAuditReanchorChanged is the refusal when the log the re-anchor
// finds under the write lock is not the one the operator approved.
var errAuditReanchorChanged = fmt.Errorf("%w: the events the plan you "+
	"approved would have accepted are no longer the ones in the log. "+
	"Nothing has been written. Something wrote to auth.db between the "+
	"plan and this re-chain; find out what before running it again",
	ErrAuditRechainChanged)

// reanchorAuditLogTx appends the audit.rechain event that records the
// new starting point, after checking under the write lock that the log
// is still the one the operator approved.
func (s *AuthStore) reanchorAuditLogTx(actor Actor,
	plan AuditRechainPlan) error {

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the re-chain transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			//nolint:errcheck // Rollback error is not critical; the
			// outer error is already being returned.
			tx.Rollback()
		}
	}()

	if err := s.checkAuditRechainPlanStillHolds(tx, plan); err != nil {
		return err
	}
	// The count and id range catch a row added or removed, but not one
	// replaced in place with the same id, which the digest does.
	scan, err := s.scanAuditForReanchor(tx)
	if err != nil {
		return err
	}
	if scan != plan.reanchor {
		return errAuditReanchorChanged
	}
	// A key mismatch loses nothing from the tail, and the verifier said
	// so, but rows could have been removed from it since, which neither
	// the scan nor the comparison above sees. The appended event would
	// cover the gap, so it is checked again under the write lock, where
	// no-one else can change it before the event is written.
	if plan.KeyMismatch {
		last := AuditEvent{ID: scan.through, Hash: scan.throughHash}
		if err := s.verifyAuditTailAfterKeyChange(tx, last); err != nil {
			return fmt.Errorf("%w (%w)", errAuditReanchorChanged, err)
		}
	}

	details := auditReanchorDetails(plan, scan)
	ev := newEvent(actor, auditActionRechain, "", nil, "", details)
	if err := s.recordAudit(tx, ev); err != nil {
		return fmt.Errorf("failed to record the re-chain event: %w", err)
	}
	// The operator has accepted the log as it stands, so the tail
	// anchor names the re-chain event from now on, whatever it named
	// before. What it named is in the reason when that was the problem.
	if err := s.writeAuditTail(tx, ev); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit the re-chain: %w", err)
	}
	committed = true

	return nil
}

// auditReanchorDetails builds the details the audit.rechain event of a
// re-anchor records, from the plan and the scan taken under the write
// lock.
func auditReanchorDetails(plan AuditRechainPlan,
	scan auditReanchorScan) map[string]any {
	details := map[string]any{
		"mode":                 string(AuditRechainReanchor),
		"reason":               plan.Problem.Error(),
		"key_mismatch":         plan.KeyMismatch,
		"events":               scan.events,
		"oldest_retained_id":   scan.headID,
		"oldest_retained_hash": scan.headHash,
	}
	if scan.historyEvents > 0 {
		details["history_through_id"] = scan.through
		details["history_events"] = scan.historyEvents
		details["history_digest"] = scan.digest
	}
	if plan.PreviousKeyGiven {
		details["history_proven_by_previous_secret"] = plan.HistoryProven
	}
	if p := plan.PreviousHead; p != nil {
		details["previous_head_event_id"] = p.EventID
		details["previous_head_verified"] = p.Verified
		// What an event that does not verify says is whatever its
		// writer chose, and signing it into this event would lend it
		// the key; only its id is kept.
		if p.Verified {
			details["previous_oldest_retained_id"] = p.HeadID
			details["previous_oldest_retained_hash"] = p.HeadHash
		}
	}

	return details
}
