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
	"log"
	"testing"
	"time"
)

// writeAuditSummaries records the summary rows of one coalescer in a
// single transaction, so that a large batch of closed windows holds
// s.mu for one commit rather than one per row. kind names the events
// in the server log. Errors are logged rather than returned, as on the
// rest of the audit path, and reported by returning false. The caller
// must hold s.mu.
func (s *AuthStore) writeAuditSummaries(kind string,
	events []*AuditEvent) bool {

	if len(events) == 0 {
		return true
	}

	if err := s.recordAuditInOwnTx(events...); err != nil {
		log.Printf("[ERROR] Failed to record %d audit %s summary event(s): %v",
			len(events), kind, err)
		return false
	}

	return true
}

// SweepCoalescedAudit writes a summary row for every coalesced failure
// and denial whose window has closed and that still holds suppressed
// repeats, so that the count of a burst which simply stops reaches the
// log without waiting for a later event to evict it. The server calls
// it on its periodic cleanup tick, and FlushCoalescedAudit writes the
// windows still open.
func (s *AuthStore) SweepCoalescedAudit() {
	s.sweepCoalescedAudit(time.Now(), false)
}

// FlushCoalescedAudit writes a summary row for every coalesced failure
// and denial that still holds suppressed repeats, whether or not its
// window has closed, so that a clean shutdown does not discard their
// counts. Close calls it, and so does the server's SIGTERM and SIGINT
// handler, which can race the deferred Close in main. Calling it
// again, or after Close, finds nothing left to write.
func (s *AuthStore) FlushCoalescedAudit() {
	s.sweepCoalescedAudit(time.Now(), true)
}

// AgeCoalescedAuditForTesting moves the start of every open coalescing
// window, failure and denial alike, back by d, so that a test can close
// windows without waiting out the coalescing window. It is exported for
// the reason AuditKeyForTesting is, since its callers include the
// cmd/mcp-server and internal/api test binaries, and the same
// testing.Testing guard keeps it unreachable from the server. Every
// denial window moves by the same amount, so the eviction list keeps
// its order.
func AgeCoalescedAuditForTesting(s *AuthStore, d time.Duration) {
	if !testing.Testing() {
		panic("auth.AgeCoalescedAuditForTesting was called outside a " +
			"test binary")
	}

	s.failures.mu.Lock()
	for _, state := range s.failures.entries {
		state.firstSeen = state.firstSeen.Add(-d)
		state.lastSeen = state.lastSeen.Add(-d)
	}
	s.failures.mu.Unlock()

	s.denials.mu.Lock()
	for _, state := range s.denials.entries {
		state.firstSeen = state.firstSeen.Add(-d)
		state.lastSeen = state.lastSeen.Add(-d)
	}
	s.denials.mu.Unlock()
}

// sweepCoalescedAudit drains both coalescers at now, every entry when
// all is true or only those whose window has closed otherwise, and
// writes the summaries in one transaction. If the write fails the
// drained entries are put back, so that the next sweep or Close tries
// again. A flush of every entry happens only at shutdown, when there
// may be no later attempt, so a failed flush also writes each summary
// to the server log, as restore does at the cap, leaving the counts
// attributed there at least.
func (s *AuthStore) sweepCoalescedAudit(now time.Time, all bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	failures := s.failures.drain(now, all)
	denials := s.denials.drain(now, all)

	events := append(failureSummaryEvents(failures),
		denialSummaryEvents(denials)...)
	if s.writeAuditSummaries("coalesced", events) {
		return
	}

	if all {
		for i := range failures {
			log.Printf("[ERROR] Unrecorded audit failure summary at "+
				"shutdown: %s", &failures[i])
		}
		for i := range denials {
			log.Printf("[ERROR] Unrecorded audit denial summary at "+
				"shutdown: %s", &denials[i])
		}
	}
	s.failures.restore(failures)
	s.denials.restore(denials)
}
