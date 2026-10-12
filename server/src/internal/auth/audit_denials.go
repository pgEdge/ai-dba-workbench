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
)

// denialCoalesceWindow is how long one recorded denial stands for the
// identical denials that follow it. An unauthenticated or under-
// privileged client can retry a refused request as fast as it likes,
// and every attempt used to append a row, so a single loop could grow
// the audit log without bound and bury the events that matter. Within
// the window the repeats are counted rather than written, and the next
// denial after it reports how many it stands for.
const denialCoalesceWindow = 60 * time.Second

// maxDenialKeys caps the coalescing map, so that a client varying the
// actor name or the action cannot turn the memory saved on audit rows
// into unbounded memory here instead. When the cap is reached the
// oldest entry is dropped, and any repeats it held are written as a
// summary row.
const maxDenialKeys = 10000

// maxDenialTargets caps how many distinct targets one window lists in
// its audit row. Further targets are only counted, in
// targets_truncated, so that a flood across many objects still costs
// one bounded row.
const maxDenialTargets = 20

// maxDenialActionLen caps the action name a denial key carries. The
// action is drawn from a fixed vocabulary, but the cap keeps the key
// bounded whatever the caller passes.
const maxDenialActionLen = 64

// maxDenialIPLen caps the client address a denial key carries. The
// address extractor only returns text that parses as an IP address,
// the longest of which is 45 bytes, but the cap keeps the key bounded
// whatever the address source, so that a client cannot open a fresh
// group, and with it a fresh audit row, with every request by varying
// a long address (issue #589).
const maxDenialIPLen = 64

// denialKey identifies a repeated denial. Two refusals coalesce only
// when the same principal, identified by actor id as well as by name
// so that two tokens of one user stay apart, is refused the same action
// for the same reason from the same client address. The target is
// deliberately not part of the key: a client may vary the object id
// without limit, so keying on it would let one loop write a row per id.
// The targets a window covers are listed in its row instead.
type denialKey struct {
	actorType ActorType
	actorID   int64
	actorName string
	actorIP   string
	action    string
	reason    string
}

// denialKeyOf builds the coalescing key for one refusal. An actor with
// no id, such as an unauthenticated caller, keys on zero.
func denialKeyOf(actor Actor, action, reason string) denialKey {
	var actorID int64
	if actor.ID != nil {
		actorID = *actor.ID
	}
	if len(action) > maxDenialActionLen {
		action = action[:maxDenialActionLen]
	}
	ip := actor.IP
	if len(ip) > maxDenialIPLen {
		ip = ip[:maxDenialIPLen]
	}

	return denialKey{
		actorType: actor.Type,
		actorID:   actorID,
		actorName: actor.Name,
		actorIP:   ip,
		action:    action,
		reason:    reason,
	}
}

// event rebuilds the denial event the key describes, carrying details,
// so that every row the coalescer writes is attributed exactly as the
// denials it counts.
func (k denialKey) event(details any) *AuditEvent {
	actor := Actor{Type: k.actorType, Name: k.actorName, IP: k.actorIP}
	if k.actorID != 0 {
		id := k.actorID
		actor.ID = &id
	}

	ev := newEvent(actor, k.action, "", nil, "", details)
	ev.Outcome = OutcomeDenied
	ev.Error = capAuditError(k.reason)

	return ev
}

// denialWindow counts the denials a row stands for, and lists the
// distinct targets they were aimed at, up to maxDenialTargets. truncated
// counts the denials whose target was new once the list was full.
type denialWindow struct {
	count     int
	targets   []string
	truncated int
}

// note counts one more denial against target.
func (w *denialWindow) note(target string) {
	w.count++
	for _, t := range w.targets {
		if t == target {
			return
		}
	}
	if len(w.targets) < maxDenialTargets {
		w.targets = append(w.targets, target)
		return
	}
	w.truncated++
}

// addTo writes the window's count and targets into an audit row's
// details.
func (w *denialWindow) addTo(details map[string]any) {
	details["repeat_count"] = w.count
	details["targets"] = w.targets
	details["targets_truncated"] = w.truncated
}

// denialState tracks one key's current window: when the window opened,
// which is when the denial that was recorded happened, when the latest
// identical denial arrived, and the identical denials suppressed since.
// closedFirstSeen keeps the start of the window that the latest
// admitted denial closed, so that reopen can put that window back if
// the denial's row cannot be written. Every state is also on a list
// ordered by firstSeen, oldest first, so that eviction looks only at
// the entries it drops rather than scanning the map.
type denialState struct {
	key             denialKey
	firstSeen       time.Time
	lastSeen        time.Time
	suppressed      denialWindow
	closedFirstSeen time.Time
	prev, next      *denialState
}

// denialSummary carries the repeats an evicted entry never got to
// report, so that they are written as one summary row rather than
// discarded with the entry. firstSeen and lastSeen bound the window the
// repeats fell in, because the summary row itself is stamped when it is
// written, which can be long after the burst.
type denialSummary struct {
	key       denialKey
	window    denialWindow
	firstSeen time.Time
	lastSeen  time.Time
}

// String describes the summary for the server log, quoting every value
// that a caller could have shaped, so that a count which cannot reach the
// audit log is at least attributed there.
func (d denialSummary) String() string {
	return fmt.Sprintf("actor %s %q (id %d) from %q, action %q, reason %q, "+
		"targets %q (+%d more): %d denial(s) between %s and %s",
		d.key.actorType, d.key.actorName, d.key.actorID, d.key.actorIP,
		d.key.action, d.key.reason, d.window.targets, d.window.truncated,
		d.window.count, d.firstSeen.UTC().Format(auditTimeLayout),
		d.lastSeen.UTC().Format(auditTimeLayout))
}

// event builds the summary row for the entry, attributed exactly as the
// denials it counts.
func (d denialSummary) event() *AuditEvent {
	details := map[string]any{
		"window_closed": true,
		"first_seen":    d.firstSeen.UTC().Format(auditTimeLayout),
		"last_seen":     d.lastSeen.UTC().Format(auditTimeLayout),
	}
	d.window.addTo(details)

	return d.key.event(details)
}

// denialCoalescer decides which denials are written and which are
// merely counted. Its zero value is ready to use. It has a lock of its
// own so that it does not depend on how the caller holds s.mu.
type denialCoalescer struct {
	mu      sync.Mutex
	entries map[denialKey]*denialState
	oldest  *denialState
	newest  *denialState
}

// admit decides whether a denial is written or merely counted. It
// returns true when the caller should record a row, together with the
// window that row closes, whose count is the number of denials the row
// stands for and is zero unless repeats were suppressed during the
// window that has just closed. It also returns the entries evicted by
// this call that still held suppressed repeats, for the caller to
// write as summaries.
//
// The first denial for a key opens a window and is recorded at once, so
// that a refusal is never invisible; identical denials inside the
// window are counted, with their targets, instead of written; and the
// first denial after the window closes is recorded, reporting the
// suppressed ones and its own target, and opens a fresh window.
func (c *denialCoalescer) admit(key denialKey, target string,
	now time.Time) (bool, denialWindow, []denialSummary) {

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil {
		c.entries = make(map[denialKey]*denialState)
	}

	record := true
	var closed denialWindow

	switch state, ok := c.entries[key]; {
	case !ok:
		state = &denialState{key: key, firstSeen: now, lastSeen: now}
		c.entries[key] = state
		c.pushNewest(state)
	case now.Sub(state.firstSeen) < denialCoalesceWindow:
		state.suppressed.note(target)
		state.lastSeen = now
		record = false
	default:
		if state.suppressed.count > 0 {
			closed = state.suppressed
			closed.note(target)
		}
		state.closedFirstSeen = state.firstSeen
		state.firstSeen = now
		state.lastSeen = now
		state.suppressed = denialWindow{}
		c.unlink(state)
		c.pushNewest(state)
	}

	return record, closed, c.evict(key, now)
}

// pushNewest appends state to the newest end of the eviction list. The
// caller must hold c.mu.
func (c *denialCoalescer) pushNewest(state *denialState) {
	state.prev = c.newest
	state.next = nil
	if c.newest != nil {
		c.newest.next = state
	} else {
		c.oldest = state
	}
	c.newest = state
}

// insertBefore links state into the eviction list just ahead of next,
// or at the newest end when next is nil. The caller must hold c.mu.
func (c *denialCoalescer) insertBefore(state, next *denialState) {
	if next == nil {
		c.pushNewest(state)
		return
	}
	state.next = next
	state.prev = next.prev
	if next.prev != nil {
		next.prev.next = state
	} else {
		c.oldest = state
	}
	next.prev = state
}

// unlink removes state from the eviction list. The caller must hold
// c.mu.
func (c *denialCoalescer) unlink(state *denialState) {
	if state.prev != nil {
		state.prev.next = state.next
	} else {
		c.oldest = state.next
	}
	if state.next != nil {
		state.next.prev = state.prev
	} else {
		c.newest = state.prev
	}
	state.prev, state.next = nil, nil
}

// reopen undoes the window that admit opened for key when the denial's
// own row could not be written, so that the next identical denial is
// recorded rather than suppressed behind a row that never reached the
// log. When that row would have closed a window holding repeats, the
// closed window is put back, in its place on the eviction list, holding
// them plus the unwritten denial, so that the next identical denial, or
// a sweep, reports them all; otherwise the entry is dropped, which also
// frees its slot at the cap.
func (c *denialCoalescer) reopen(key denialKey, closed denialWindow) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.entries[key]
	if !ok {
		return
	}
	c.unlink(state)
	if closed.count == 0 {
		delete(c.entries, key)
		return
	}
	state.firstSeen = state.closedFirstSeen
	state.suppressed = closed

	next := c.oldest
	for next != nil && !next.firstSeen.After(state.firstSeen) {
		next = next.next
	}
	c.insertBefore(state, next)
}

// drop deletes one entry, appending a summary to expired if the entry
// still held suppressed repeats, and returns the extended slice. The
// caller must hold c.mu.
func (c *denialCoalescer) drop(expired []denialSummary,
	state *denialState) []denialSummary {

	if state.suppressed.count > 0 {
		expired = append(expired, denialSummary{
			key:       state.key,
			window:    state.suppressed,
			firstSeen: state.firstSeen,
			lastSeen:  state.lastSeen,
		})
	}
	c.unlink(state)
	delete(c.entries, state.key)

	return expired
}

// evict drops entries from the oldest end of the eviction list while
// their window has closed or the map is over its cap. The list is
// ordered by firstSeen, so it stops at the first entry that is neither,
// and the work done is proportional to the entries dropped. The key
// just handled is kept, because its window has only now been opened.
// Every dropped entry that still held suppressed repeats is returned as
// a summary, oldest first. The caller must hold c.mu.
func (c *denialCoalescer) evict(keep denialKey,
	now time.Time) []denialSummary {

	var expired []denialSummary

	for state := c.oldest; state != nil; {
		next := state.next
		closed := now.Sub(state.firstSeen) >= denialCoalesceWindow
		if !closed && len(c.entries) <= maxDenialKeys {
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
// an unrelated later denial to evict it, and lets shutdown write the
// counts of windows that are still open rather than discarding them.
func (c *denialCoalescer) drain(now time.Time, all bool) []denialSummary {
	c.mu.Lock()
	defer c.mu.Unlock()

	var expired []denialSummary
	for state := c.oldest; state != nil; {
		next := state.next
		if !all && now.Sub(state.firstSeen) < denialCoalesceWindow {
			break
		}
		expired = c.drop(expired, state)
		state = next
	}

	return expired
}

// restore puts back the entries behind summaries that could not be
// written, in their place on the eviction list, so that a later sweep,
// or Close, can try again rather than the counts being lost with a
// failed transaction. Nothing is put back once the map is at its cap,
// so that a store that keeps failing its writes cannot grow the map
// without bound, and an entry that has been re-created since the drain
// is left as it is; each count so dropped is written to the server log
// instead, with its key, since it cannot reach the audit log.
func (c *denialCoalescer) restore(summaries []denialSummary) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil {
		c.entries = make(map[denialKey]*denialState)
	}

	sort.SliceStable(summaries, func(i, j int) bool {
		return summaries[i].firstSeen.Before(summaries[j].firstSeen)
	})

	// The summaries are in firstSeen order, so one pass along the list
	// finds the place of each in turn.
	cursor := c.oldest
	for i := range summaries {
		summary := &summaries[i]
		if _, ok := c.entries[summary.key]; ok {
			log.Printf("[ERROR] Dropped an unrecorded audit denial summary "+
				"for a window opened since: %s", summary)
			continue
		}
		if len(c.entries) >= maxDenialKeys {
			log.Printf("[ERROR] Dropped an unrecorded audit denial summary "+
				"at the coalescing cap: %s", summary)
			continue
		}
		for cursor != nil && !cursor.firstSeen.After(summary.firstSeen) {
			cursor = cursor.next
		}
		state := &denialState{
			key:        summary.key,
			firstSeen:  summary.firstSeen,
			lastSeen:   summary.lastSeen,
			suppressed: summary.window,
		}
		c.entries[summary.key] = state
		c.insertBefore(state, cursor)
	}
}

// denialSummaryEvents builds the summary row for each entry.
func denialSummaryEvents(summaries []denialSummary) []*AuditEvent {
	events := make([]*AuditEvent, len(summaries))
	for i := range summaries {
		events[i] = summaries[i].event()
	}

	return events
}

// RecordCoalescedDenial records an authorisation denial of action for
// reason, aimed at target, through the denial coalescer: the first
// denial of a burst is written at once, identical denials within
// denialCoalesceWindow are counted with their targets, and the counts
// reach the log on the next identical denial, when an unrelated denial
// evicts the entry, on the periodic SweepCoalescedAudit, or at
// shutdown through FlushCoalescedAudit. The target is stored in the
// row's details and should be normalised by the caller, so that nothing
// taken verbatim from a request reaches the log.
//
// It returns an error only when the denial's own row could not be
// written, so that the caller can log it with the request it refused; a
// summary row that cannot be written is logged here and kept for a
// later attempt.
func (s *AuthStore) RecordCoalescedDenial(actor Actor, action, target,
	reason string) error {

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.coalesceDenial(denialKeyOf(actor, action, reason), target,
		time.Now())
}

// coalesceDenial runs one denial through the coalescer at now, writing
// a summary row for every evicted entry that held suppressed repeats,
// in one transaction, and then, if admitted, the denial itself, and
// returns an error only for the denial's own row. If that row cannot be
// written its window is undone by reopen, so that the identical
// denials after it are recorded rather than suppressed behind a row
// that never reached the log, and the repeats it would have reported
// are kept. Summaries that cannot be written are put back, as the
// sweep does, but only after that, so that when the store is failing
// every write the slot the denial took is free again for the evicted
// window it displaced at the cap. The caller must hold s.mu, which
// keeps a sweep from draining an entry between the coalescer's
// decision and the writes.
func (s *AuthStore) coalesceDenial(key denialKey, target string,
	now time.Time) error {

	record, closed, expired := s.denials.admit(key, target, now)

	summariesWritten := s.writeAuditSummaries("denial",
		denialSummaryEvents(expired))

	var err error
	if record {
		details := map[string]any{"target": target}
		if closed.count > 0 {
			closed.addTo(details)
		}
		if err = s.recordAuditInOwnTx(key.event(details)); err != nil {
			s.denials.reopen(key, closed)
			err = fmt.Errorf("failed to record denial: %w", err)
		}
	}

	if !summariesWritten {
		s.denials.restore(expired)
	}

	return err
}
