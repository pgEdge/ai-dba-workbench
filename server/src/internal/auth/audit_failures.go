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
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
	"unicode/utf8"
)

// failureCoalesceWindow is how long one recorded failure stands for the
// identical failures that follow it. A caller holding a mutation
// permission can repeat a request that fails deterministically, such as
// creating a group that already exists, and every attempt used to append
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

// maxAuditIPLen caps the client address a denial or failure key
// carries. The address extractor only returns text that parses as an
// IP address, the longest of which is 45 bytes, but the cap keeps the
// key bounded whatever the address source, so that a client cannot
// open a fresh group, and with it a fresh audit row and map entry,
// with every request by varying a long address (issues #589, #651).
const maxAuditIPLen = 64

// capAuditIP shortens a client address to at most maxAuditIPLen bytes
// for use in a coalescing key, cutting on a UTF-8 rune boundary, as
// capAuditError does, so that the stored address stays valid text.
func capAuditIP(ip string) string {
	if len(ip) <= maxAuditIPLen {
		return ip
	}

	cut := maxAuditIPLen
	for cut > 0 && !utf8.RuneStart(ip[cut]) {
		cut--
	}

	return ip[:cut]
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
		actorIP:    capAuditIP(actor.IP),
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
// the window opened. closedFirstSeen keeps the start of the window that
// the latest admitted failure closed, so that reopen can put that window
// back if the failure's row cannot be written. Every state is also on a
// list ordered by firstSeen, oldest first, so that eviction looks only
// at the entries it drops rather than scanning the map.
type failureState struct {
	key             failureKey
	firstSeen       time.Time
	lastSeen        time.Time
	suppressed      int
	closedFirstSeen time.Time
	evictionLinks[failureState]
}

// links gives the eviction list access to the state's neighbors.
//
//nolint:unused // called through the evictionEntry constraint
func (s *failureState) links() *evictionLinks[failureState] {
	return &s.evictionLinks
}

// windowStart is the time the state's current window opened, by which
// the eviction list is ordered.
//
//nolint:unused // called through the evictionEntry constraint
func (s *failureState) windowStart() time.Time {
	return s.firstSeen
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

// String describes the summary for the server log, quoting every value
// that a caller could have shaped, so that a count which cannot reach the
// audit log is at least attributed there.
func (f failureSummary) String() string {
	return fmt.Sprintf("actor %s %q (id %d) from %q, action %q, target "+
		"%s %q, error %q: %d failure(s) between %s and %s",
		f.key.actorType, f.key.actorName, f.key.actorID, f.key.actorIP,
		f.key.action, f.key.targetType, f.key.targetName, f.key.errText,
		f.suppressed, f.firstSeen.UTC().Format(auditTimeLayout),
		f.lastSeen.UTC().Format(auditTimeLayout))
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
	evictionList[failureState, *failureState]
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
		state = &failureState{key: key, firstSeen: now, lastSeen: now}
		c.entries[key] = state
		c.pushNewest(state)
	case now.Sub(state.firstSeen) < failureCoalesceWindow:
		state.suppressed++
		state.lastSeen = now
		record = false
	default:
		if state.suppressed > 0 {
			repeats = state.suppressed + 1
		}
		state.closedFirstSeen = state.firstSeen
		state.firstSeen = now
		state.lastSeen = now
		state.suppressed = 0
		c.unlink(state)
		c.pushNewest(state)
	}

	return record, repeats, c.evict(key, now)
}

// evict drops entries from the oldest end of the eviction list while
// their window has closed or the map is over its cap. The list is
// ordered by firstSeen, so it stops at the first entry that is neither,
// and the work done is proportional to the entries dropped rather than
// to the size of the map, which matters because it runs on every
// failure. The key just handled is kept, because its window has only
// now been opened. Every dropped entry that still held suppressed
// repeats is returned as a summary, oldest first. The caller must hold
// c.mu.
func (c *failureCoalescer) evict(keep failureKey,
	now time.Time) []failureSummary {

	var expired []failureSummary

	for state := c.oldest; state != nil; {
		next := state.next
		closed := now.Sub(state.firstSeen) >= failureCoalesceWindow
		if !closed && len(c.entries) <= maxFailureKeys {
			break
		}
		if state.key != keep {
			expired = c.drop(expired, state)
		}
		state = next
	}

	return expired
}

// drain removes the entries whose window has closed by now, or every
// entry when all is true, and returns a summary, oldest first, for each
// one that still held suppressed repeats. It lets a periodic sweep
// write the counts of a burst that has stopped, rather than waiting for
// an unrelated later failure to evict it, and lets shutdown write the
// counts of windows that are still open rather than discarding them.
func (c *failureCoalescer) drain(now time.Time, all bool) []failureSummary {
	c.mu.Lock()
	defer c.mu.Unlock()

	var expired []failureSummary
	for state := c.oldest; state != nil; {
		next := state.next
		if !all && now.Sub(state.firstSeen) < failureCoalesceWindow {
			break
		}
		expired = c.drop(expired, state)
		state = next
	}

	return expired
}

// restore puts back the entries behind summaries that could not be
// written, in their place on the eviction list, so that a later sweep,
// or Close, can try again rather than
// the counts being lost with a failed transaction. An entry that has
// been re-created since the drain is left as it is, and nothing is put
// back once the map is at its cap, so that a store that keeps failing
// its writes cannot grow the map without bound; each count so dropped
// is written to the server log instead, with its key, since it cannot
// reach the audit log.
func (c *failureCoalescer) restore(summaries []failureSummary) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil {
		c.entries = make(map[failureKey]*failureState)
	}

	// The summaries are in firstSeen order, so one pass along the list
	// finds the place of each in turn.
	sortSummaries(summaries)
	cursor := c.oldest
	for i := range summaries {
		summary := &summaries[i]
		if _, ok := c.entries[summary.key]; ok {
			continue
		}
		if len(c.entries) >= maxFailureKeys {
			log.Printf("[ERROR] Dropped an unrecorded audit failure "+
				"summary at the coalescing cap: %s", summary)
			continue
		}
		for cursor != nil && !cursor.firstSeen.After(summary.firstSeen) {
			cursor = cursor.next
		}
		state := &failureState{
			key:        summary.key,
			firstSeen:  summary.firstSeen,
			lastSeen:   summary.lastSeen,
			suppressed: summary.suppressed,
		}
		c.entries[summary.key] = state
		c.insertBefore(state, cursor)
	}
}

// drop deletes one entry, appending a summary to expired if the entry
// still held suppressed repeats, and returns the extended slice. The
// caller must hold c.mu.
func (c *failureCoalescer) drop(expired []failureSummary,
	state *failureState) []failureSummary {

	if state.suppressed > 0 {
		expired = append(expired, failureSummary{
			key:        state.key,
			suppressed: state.suppressed,
			firstSeen:  state.firstSeen,
			lastSeen:   state.lastSeen,
		})
	}
	c.unlink(state)
	delete(c.entries, state.key)

	return expired
}

// sortSummaries orders summaries by the start of their window, so that
// restore can put them back in one pass along the eviction list.
func sortSummaries(summaries []failureSummary) []failureSummary {
	sort.SliceStable(summaries, func(i, j int) bool {
		return summaries[i].firstSeen.Before(summaries[j].firstSeen)
	})

	return summaries
}

// reopen undoes the window that admit opened for key when the failure's
// own row could not be written, so that the next identical failure is
// recorded rather than suppressed behind a row that never reached the
// log. When that row carried repeats from the window it closed, the
// closed window is put back holding them plus the unwritten failure, so
// the next identical failure, or a sweep, reports them all, in its place
// on the eviction list; otherwise the entry is dropped, which also frees
// its slot at the cap.
func (c *failureCoalescer) reopen(key failureKey, repeats int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.entries[key]
	if !ok {
		return
	}
	c.unlink(state)
	if repeats == 0 {
		delete(c.entries, key)
		return
	}
	state.firstSeen = state.closedFirstSeen
	state.suppressed = repeats
	c.insertOrdered(state)
}

// writeFailure records one failure event in its own transaction,
// logging rather than returning an error, for the reason recordFailure
// gives, and reports whether the event was written.
func (s *AuthStore) writeFailure(ev *AuditEvent) bool {
	if err := s.recordAuditInOwnTx(ev); err != nil {
		log.Printf("[ERROR] Failed to record audit failure event: %v", err)
		return false
	}

	return true
}

// writeFailureSummaries records a summary row for each entry in one
// transaction, through writeAuditSummaries. The caller must hold s.mu.
func (s *AuthStore) writeFailureSummaries(summaries []failureSummary) bool {
	return s.writeAuditSummaries("failure", failureSummaryEvents(summaries))
}

// failureSummaryEvents builds the summary row for each entry.
func failureSummaryEvents(summaries []failureSummary) []*AuditEvent {
	events := make([]*AuditEvent, len(summaries))
	for i := range summaries {
		events[i] = summaries[i].event()
	}

	return events
}

// coalesceFailure runs one failure through the coalescer at now,
// writing a summary row for every evicted entry that held suppressed
// repeats and then, if admitted, the failure itself. If the failure
// itself cannot be written its window is undone by reopen, so that the
// identical failures after it are recorded rather than suppressed
// behind a row that never reached the log. Summaries that cannot be
// written are put back, as the sweep does, but only after that drop,
// so that when the store is failing every write the slot the failure
// took is free again for the evicted window it displaced at the cap.
// The caller must hold s.mu, as recordFailure requires, which keeps
// another failure for the same key from slipping in between the
// writes and the map updates.
func (s *AuthStore) coalesceFailure(key failureKey, now time.Time) {
	record, repeats, expired := s.failures.admit(key, now)

	summariesWritten := s.writeFailureSummaries(expired)

	if record {
		var details any
		if repeats > 0 {
			details = map[string]any{"repeat_count": repeats}
		}
		if !s.writeFailure(key.event(details)) {
			s.failures.reopen(key, repeats)
		}
	}

	if !summariesWritten {
		s.failures.restore(expired)
	}
}
