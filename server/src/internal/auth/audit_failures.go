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
	"sort"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// failureCoalesceWindow is how long one recorded failure stands for the
// identical failures that follow it. A caller holding a mutation
// permission can repeat a request that fails deterministically, such as
// creating a user that already exists, and every attempt used to append
// a row, so a loop could grow the audit log without bound and bury the
// events that matter. Within the window the repeats are counted rather
// than written, and the next failure after it reports how many it
// stands for. It matches the window the REST layer applies to denials.
const failureCoalesceWindow = 60 * time.Second

// maxFailureKeys caps the coalescing map, so that a caller varying the
// target name cannot turn the rows saved in the audit log into
// unbounded memory here instead. When the cap is reached the oldest
// entry is dropped, which at worst records one extra row.
const maxFailureKeys = 10000

// maxAuditErrorBytes caps the error text stored on a failure or denial
// event. The text comes from an error the caller may be able to shape,
// and an uncapped value lets every row carry as much of it as the
// caller likes.
const maxAuditErrorBytes = 500

// auditErrorTruncated marks error text that capAuditError shortened.
const auditErrorTruncated = "... (truncated)"

// capAuditError shortens text to at most maxAuditErrorBytes, cutting on
// a UTF-8 rune boundary and ending with auditErrorTruncated so that a
// reader can tell the text was shortened.
func capAuditError(text string) string {
	if len(text) <= maxAuditErrorBytes {
		return text
	}

	cut := maxAuditErrorBytes - len(auditErrorTruncated)
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return text[:cut] + auditErrorTruncated
}

// failureKey identifies a repeated failure. Two failures coalesce only
// when the same principal, from the same client address, fails the same
// action against the same target with the same error text.
type failureKey struct {
	actorType   ActorType
	actorID     int64
	actorName   string
	actorIP     string
	action      string
	targetType  string
	targetID    int64
	hasTargetID bool
	targetName  string
	errText     string
}

// failureKeyOf builds the coalescing key for one failure. An actor with
// no id keys on zero; a missing target id is kept distinct from any
// real one by hasTargetID.
func failureKeyOf(actor Actor, action, targetType string, targetID *int64,
	targetName, errText string) failureKey {

	key := failureKey{
		actorType:  actor.Type,
		actorName:  actor.Name,
		actorIP:    actor.IP,
		action:     action,
		targetType: targetType,
		targetName: targetName,
		errText:    errText,
	}
	if actor.ID != nil {
		key.actorID = *actor.ID
	}
	if targetID != nil {
		key.targetID = *targetID
		key.hasTargetID = true
	}

	return key
}

// event rebuilds the failure event the key describes, carrying details,
// so that a summary row is attributed exactly as the failures it counts.
func (k failureKey) event(details any) *AuditEvent {
	actor := Actor{Type: k.actorType, Name: k.actorName, IP: k.actorIP}
	if k.actorID != 0 {
		id := k.actorID
		actor.ID = &id
	}

	var targetID *int64
	if k.hasTargetID {
		id := k.targetID
		targetID = &id
	}

	ev := newEvent(actor, k.action, k.targetType, targetID, k.targetName,
		details)
	ev.Outcome = OutcomeFailure
	ev.Error = k.errText

	return ev
}

// failureState tracks one key's current window: when it opened, which
// is when the failure that was recorded happened, when the latest
// identical failure arrived, and how many have been suppressed since
// the window opened.
type failureState struct {
	firstSeen  time.Time
	lastSeen   time.Time
	suppressed int
}

// failureSummary carries the repeats an evicted entry never got to
// report, so that they are written as one summary row rather than
// discarded with the entry. firstSeen and lastSeen bound the window the
// repeats fell in, because the summary row itself is stamped when it is
// written, which can be long after the burst.
type failureSummary struct {
	key        failureKey
	suppressed int
	firstSeen  time.Time
	lastSeen   time.Time
}

// event builds the summary row for the entry, attributed exactly as the
// failures it counts.
func (f failureSummary) event() *AuditEvent {
	return f.key.event(map[string]any{
		"repeat_count":  f.suppressed,
		"window_closed": true,
		"first_seen":    f.firstSeen.UTC().Format(auditTimeLayout),
		"last_seen":     f.lastSeen.UTC().Format(auditTimeLayout),
	})
}

// failureCoalescer decides which failures are written and which are
// merely counted. Its zero value is ready to use. It has a lock of its
// own so that it does not depend on how the caller holds s.mu.
type failureCoalescer struct {
	mu      sync.Mutex
	entries map[failureKey]*failureState
}

// admit returns true when the caller should record a row for key,
// together with the number of identical failures that row stands for,
// which is zero unless repeats were suppressed during the window that
// has just closed. It also returns the entries evicted by this call
// that still held suppressed repeats, for the caller to summarize.
//
// The first failure for a key opens a window and is recorded at once,
// so that a failure is never invisible; identical failures inside the
// window are counted instead of written; and the first failure after
// the window closes is recorded, reporting the suppressed ones, and
// opens a fresh window.
func (c *failureCoalescer) admit(key failureKey, now time.Time) (bool, int,
	[]failureSummary) {

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil {
		c.entries = make(map[failureKey]*failureState)
	}

	record := true
	repeats := 0

	switch state, ok := c.entries[key]; {
	case !ok:
		c.entries[key] = &failureState{firstSeen: now, lastSeen: now}
	case now.Sub(state.firstSeen) < failureCoalesceWindow:
		state.suppressed++
		state.lastSeen = now
		record = false
	default:
		if state.suppressed > 0 {
			repeats = state.suppressed + 1
		}
		state.firstSeen = now
		state.lastSeen = now
		state.suppressed = 0
	}

	return record, repeats, c.evict(key, now)
}

// evict drops entries whose window has closed and then, while the map
// is over its cap, the oldest entry. The key just handled is kept in
// both passes. Every dropped entry that still held suppressed repeats
// is returned as a summary. The caller must hold c.mu.
func (c *failureCoalescer) evict(keep failureKey,
	now time.Time) []failureSummary {

	var expired []failureSummary

	drop := func(key failureKey, state *failureState) {
		expired = c.drop(expired, key, state)
	}

	for key, state := range c.entries {
		if key != keep && now.Sub(state.firstSeen) >= failureCoalesceWindow {
			drop(key, state)
		}
	}

	// Over the cap there is always an entry other than keep, so each
	// pass finds one to drop.
	for len(c.entries) > maxFailureKeys {
		var oldestKey failureKey
		var oldestState *failureState
		for key, state := range c.entries {
			if key != keep && (oldestState == nil ||
				state.firstSeen.Before(oldestState.firstSeen)) {
				oldestKey, oldestState = key, state
			}
		}
		drop(oldestKey, oldestState)
	}

	return sortSummaries(expired)
}

// drain removes the entries whose window has closed by now, or every
// entry when all is true, and returns a summary for each one that still
// held suppressed repeats. It lets a periodic sweep write the counts of
// a burst that has stopped, rather than waiting for an unrelated later
// failure to evict it, and lets shutdown write the counts of windows
// that are still open rather than discarding them.
func (c *failureCoalescer) drain(now time.Time, all bool) []failureSummary {
	c.mu.Lock()
	defer c.mu.Unlock()

	var expired []failureSummary
	for key, state := range c.entries {
		if all || now.Sub(state.firstSeen) >= failureCoalesceWindow {
			expired = c.drop(expired, key, state)
		}
	}

	return sortSummaries(expired)
}

// restore puts back the entries behind summaries that could not be
// written, so that a later sweep, or Close, can try again rather than
// the counts being lost with a failed transaction. An entry that has
// been re-created since the drain is left as it is, and nothing is put
// back once the map is at its cap, so that a store that keeps failing
// its writes cannot grow the map without bound; the counts so dropped
// have already been logged with the failed write.
func (c *failureCoalescer) restore(summaries []failureSummary) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil {
		c.entries = make(map[failureKey]*failureState)
	}
	for i := range summaries {
		summary := &summaries[i]
		if len(c.entries) >= maxFailureKeys {
			return
		}
		if _, ok := c.entries[summary.key]; ok {
			continue
		}
		c.entries[summary.key] = &failureState{
			firstSeen:  summary.firstSeen,
			lastSeen:   summary.lastSeen,
			suppressed: summary.suppressed,
		}
	}
}

// drop deletes one entry, appending a summary to expired if the entry
// still held suppressed repeats, and returns the extended slice. The
// caller must hold c.mu.
func (c *failureCoalescer) drop(expired []failureSummary, key failureKey,
	state *failureState) []failureSummary {

	if state.suppressed > 0 {
		expired = append(expired, failureSummary{
			key:        key,
			suppressed: state.suppressed,
			firstSeen:  state.firstSeen,
			lastSeen:   state.lastSeen,
		})
	}
	delete(c.entries, key)

	return expired
}

// sortSummaries orders summaries by the start of their window, so that
// the rows appear in the log in the order the bursts began rather than
// in map iteration order.
func sortSummaries(summaries []failureSummary) []failureSummary {
	sort.SliceStable(summaries, func(i, j int) bool {
		return summaries[i].firstSeen.Before(summaries[j].firstSeen)
	})

	return summaries
}

// writeFailure records one failure event in its own transaction,
// logging rather than returning an error, for the reason recordFailure
// gives.
func (s *AuthStore) writeFailure(ev *AuditEvent) {
	if err := s.recordAuditInOwnTx(ev); err != nil {
		log.Printf("[ERROR] Failed to record audit failure event: %v", err)
	}
}

// writeFailureSummaries records a summary row for each entry in one
// transaction, so that a large batch of closed windows holds s.mu for
// one commit rather than one per row. Errors are logged, as in
// writeFailure, and reported by returning false. The caller must hold
// s.mu.
func (s *AuthStore) writeFailureSummaries(summaries []failureSummary) bool {
	if len(summaries) == 0 {
		return true
	}

	events := make([]*AuditEvent, len(summaries))
	for i := range summaries {
		events[i] = summaries[i].event()
	}

	if err := s.recordAuditInOwnTx(events...); err != nil {
		log.Printf("[ERROR] Failed to record %d audit failure summary event(s): %v",
			len(events), err)
		return false
	}

	return true
}

// SweepAuditFailures writes a summary row for every coalesced failure
// whose window has closed and that still holds suppressed repeats, so
// that the count of a burst which simply stops reaches the log without
// waiting for a later failure to evict it. The server calls it on its
// periodic cleanup tick, and FlushAuditFailures writes the windows
// still open.
func (s *AuthStore) SweepAuditFailures() {
	s.sweepAuditFailures(time.Now(), false)
}

// FlushAuditFailures writes a summary row for every coalesced failure
// that still holds suppressed repeats, whether or not its window has
// closed, so that a clean shutdown does not discard their counts. Close
// calls it, and so does the server's SIGTERM and SIGINT handler, which
// exits the process without reaching Close. Calling it again, or
// after Close, finds nothing left to write.
func (s *AuthStore) FlushAuditFailures() {
	s.sweepAuditFailures(time.Now(), true)
}

// AgeAuditFailuresForTesting moves the start of every open coalescing
// window back by d, so that a test can close windows without waiting
// out failureCoalesceWindow. It is exported for the reason
// AuditKeyForTesting is, since its callers include the cmd/mcp-server
// test binary, and the same testing.Testing guard keeps it unreachable
// from the server.
func AgeAuditFailuresForTesting(s *AuthStore, d time.Duration) {
	if !testing.Testing() {
		panic("auth.AgeAuditFailuresForTesting was called outside a " +
			"test binary")
	}

	s.failures.mu.Lock()
	defer s.failures.mu.Unlock()

	for _, state := range s.failures.entries {
		state.firstSeen = state.firstSeen.Add(-d)
		state.lastSeen = state.lastSeen.Add(-d)
	}
}

// sweepAuditFailures drains the coalescer at now, every entry when all
// is true or only those whose window has closed otherwise, and writes
// the summaries. If the write fails the drained entries are put back,
// so that the next sweep or Close tries again.
func (s *AuthStore) sweepAuditFailures(now time.Time, all bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	summaries := s.failures.drain(now, all)
	if !s.writeFailureSummaries(summaries) {
		s.failures.restore(summaries)
	}
}

// coalesceFailure runs one failure through the coalescer at now,
// writing a summary row for every evicted entry that held suppressed
// repeats and then, if admitted, the failure itself. Summaries that
// cannot be written are put back, as the sweep does. The caller must
// hold s.mu, as recordFailure requires.
func (s *AuthStore) coalesceFailure(key failureKey, now time.Time) {
	record, repeats, expired := s.failures.admit(key, now)

	if !s.writeFailureSummaries(expired) {
		s.failures.restore(expired)
	}

	if !record {
		return
	}

	var details any
	if repeats > 0 {
		details = map[string]any{"repeat_count": repeats}
	}
	s.writeFailure(key.event(details))
}
