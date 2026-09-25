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
	"encoding/json"
	"errors"
	"fmt"
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
// None of this re-signs an existing row. The purge verifies rows and
// records one row's existing hash as a value inside an event it creates
// itself, and the verifier still recomputes every surviving row,
// including that one, so the record exempts nothing from the chain.
// That distinction is the one PurgeAuditEvents sets out: an unattended
// path that re-signs rows can be steered into signing a forgery under
// the real key, whereas this one can at worst be made to refuse.

// auditPurgeHead is what an audit.purge event records about the head it
// left behind. Both fields are absent from events written by builds
// that predate the record, which the checks below treat as saying
// nothing about the head rather than as a mismatch.
type auditPurgeHead struct {
	OldestRetainedID   *int64 `json:"oldest_retained_id,omitempty"`
	OldestRetainedHash string `json:"oldest_retained_hash,omitempty"`
}

// parseAuditPurgeHead reads the head record from an audit.purge event's
// details. An event with no details, or none of the head fields,
// returns an empty record.
func parseAuditPurgeHead(ev *AuditEvent) (auditPurgeHead, error) {
	var head auditPurgeHead
	if len(ev.Details) == 0 {
		return head, nil
	}
	if err := json.Unmarshal(ev.Details, &head); err != nil {
		return head, fmt.Errorf("failed to read the details of audit "+
			"purge event %d: %w", ev.ID, err)
	}

	return head, nil
}

// auditHeadCheck accumulates, during VerifyAuditChain's walk, what it
// needs to decide whether the head of the log is accounted for. observe
// must be called only with rows that have already verified, since the
// purge record is worth something only because its hash does.
type auditHeadCheck struct {
	seenRows bool
	oldest   AuditEvent

	// purgeSeen records that some audit.purge event survives, which is
	// all a build that predates the head record left to go on.
	purgeSeen bool
	// purgeID and head describe the newest audit.purge event and the
	// head it recorded; head is empty when that event predates the
	// record.
	purgeID int64
	head    auditPurgeHead
}

// observe takes note of one verified row, in id order.
func (h *auditHeadCheck) observe(ev *AuditEvent) error {
	if !h.seenRows {
		h.seenRows = true
		h.oldest = *ev
	}
	if ev.Action != auditActionPurge {
		return nil
	}

	head, err := parseAuditPurgeHead(ev)
	if err != nil {
		return err
	}
	h.purgeSeen = true
	h.purgeID = ev.ID
	h.head = head

	return nil
}

// check reports whether the oldest row is accounted for, returning the
// id of the oldest row alongside an error when it is not.
func (h *auditHeadCheck) check() (int64, error) {
	if !h.seenRows {
		return 0, nil
	}

	if h.head.OldestRetainedHash != "" {
		if h.oldest.Hash == h.head.OldestRetainedHash {
			return 0, nil
		}
		return h.oldest.ID, fmt.Errorf(
			"%w: audit log head missing: the retention purge recorded as "+
				"event %d left event %s as the oldest, but the oldest "+
				"event is now %d; events have been deleted from the start "+
				"of the log since that purge",
			ErrAuditChainBroken, h.purgeID,
			auditIDString(h.head.OldestRetainedID), h.oldest.ID)
	}

	// No surviving purge event records the head, either because none
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

// auditSelectNewestPurge reads the newest audit.purge event, whose head
// record says where the next purge must begin.
const auditSelectNewestPurge = auditSelectAll +
	" WHERE action = ? ORDER BY id DESC LIMIT 1"

// auditSelectThroughID reads every row up to and including an id, in id
// order: the prefix a purge is about to delete and the row it will
// leave as the new head.
const auditSelectThroughID = auditSelectAll + " WHERE id <= ? ORDER BY id"

// errAuditPurgeRefused prefixes every refusal by verifyAuditPurgePrefix,
// so that the server log names what it declined to do.
const errAuditPurgeRefused = "refusing to purge the audit log"

// verifyAuditPurgePrefix checks the rows a purge is about to delete,
// every row with an id below cut, and the row at cut that will become
// the head, and returns that row. It reads inside the purge's
// transaction, which holds the write lock, so the rows it checks are
// the rows the DELETE removes.
//
// The rows are read in a single statement and fully consumed before the
// caller executes anything further on the transaction.
func (s *AuthStore) verifyAuditPurgePrefix(tx *sql.Tx, cut int64) (
	AuditEvent, error) {

	start, genesis, err := s.auditPurgeStart(tx)
	if err != nil {
		return AuditEvent{}, err
	}

	rows, err := tx.Query(auditSelectThroughID, cut)
	if err != nil {
		return AuditEvent{}, fmt.Errorf("failed to read the audit events "+
			"to purge: %w", err)
	}
	defer rows.Close()

	var prev AuditEvent
	first := true
	for rows.Next() {
		ev, err := scanAuditEvent(rows.Scan)
		if err != nil {
			return AuditEvent{}, fmt.Errorf("failed to scan an audit event "+
				"to purge: %w", err)
		}
		if err := s.checkAuditRowVerifies(&ev); err != nil {
			return AuditEvent{}, err
		}

		switch {
		case !first && ev.PrevHash != prev.Hash:
			return AuditEvent{}, fmt.Errorf("%w: %s: event %d does not "+
				"link to event %d before it", ErrAuditChainBroken,
				errAuditPurgeRefused, ev.ID, prev.ID)
		case first && start != "" && ev.Hash != start:
			return AuditEvent{}, fmt.Errorf("%w: %s: the oldest event, %d, "+
				"is not the one the previous purge left as the oldest, so "+
				"events have been deleted from the start of the log",
				ErrAuditChainBroken, errAuditPurgeRefused, ev.ID)
		case first && genesis && ev.PrevHash != "":
			return AuditEvent{}, fmt.Errorf("%w: %s: the oldest event, %d, "+
				"follows an event that is no longer in the log, and no "+
				"audit.purge event accounts for its removal",
				ErrAuditChainBroken, errAuditPurgeRefused, ev.ID)
		}

		prev = ev
		first = false
	}
	if err := rows.Err(); err != nil {
		return AuditEvent{}, fmt.Errorf("failed to read the audit events "+
			"to purge: %w", err)
	}
	if first || prev.ID != cut {
		// cut came from the same transaction, so the row is there
		// unless something is badly wrong; a purge that cannot find the
		// head it is meant to keep must not delete anything.
		return AuditEvent{}, fmt.Errorf("%s: event %d, which the purge "+
			"would keep as the oldest, could not be read",
			errAuditPurgeRefused, cut)
	}

	return prev, nil
}

// auditPurgeStart returns where the prefix a purge deletes must begin:
// the hash of the head the newest audit.purge event recorded, or, when
// no purge event exists at all, genesis set to say that it must begin
// at the genesis row. An empty start with genesis unset means the
// newest purge event predates the head record, so the prefix may begin
// anywhere; that lasts only until the first purge this build runs.
func (s *AuthStore) auditPurgeStart(tx *sql.Tx) (start string,
	genesis bool, err error) {

	ev, err := scanAuditEvent(
		tx.QueryRow(auditSelectNewestPurge, auditActionPurge).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return "", true, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to read the newest audit "+
			"purge event: %w", err)
	}

	// The record is trusted only because its hash verifies; a purge
	// event written without the key is not the server's word on
	// anything.
	if err := s.checkAuditRowVerifies(&ev); err != nil {
		return "", false, err
	}
	head, err := parseAuditPurgeHead(&ev)
	if err != nil {
		return "", false, err
	}

	return head.OldestRetainedHash, false, nil
}

// checkAuditRowVerifies recomputes one row's hash under the store's
// key, for the purge's check of what it is about to delete.
func (s *AuthStore) checkAuditRowVerifies(ev *AuditEvent) error {
	want, err := auditHash(ev, s.auditKey)
	if err != nil {
		return fmt.Errorf("%w: %s: event %d cannot be verified: %w",
			ErrAuditChainBroken, errAuditPurgeRefused, ev.ID, err)
	}
	if want != ev.Hash {
		return fmt.Errorf("%w: %s: event %d does not verify under the key "+
			"in use; run -verify-audit-log", ErrAuditChainBroken,
			errAuditPurgeRefused, ev.ID)
	}

	return nil
}
