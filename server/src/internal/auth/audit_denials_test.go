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
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

// denialKeyFor builds the coalescing key for a denial of action, for
// reason, by the user name.
func denialKeyFor(name, action, reason string) denialKey {
	return denialKeyOf(Actor{Type: ActorUser, Name: name}, action, reason)
}

// checkDenialList fails the test unless the eviction list holds every
// entry of the map exactly once, linked both ways and ordered by
// firstSeen, oldest first.
func checkDenialList(t *testing.T, c *denialCoalescer) {
	t.Helper()

	seen := 0
	var prev *denialState
	for state := c.oldest; state != nil; state = state.next {
		if state.prev != prev {
			t.Fatalf("entry %+v has a broken back link", state.key)
		}
		if prev != nil && state.firstSeen.Before(prev.firstSeen) {
			t.Fatalf("entry %+v is out of firstSeen order", state.key)
		}
		if c.entries[state.key] != state {
			t.Fatalf("entry %+v is listed but not mapped", state.key)
		}
		prev = state
		seen++
	}
	if c.newest != prev {
		t.Fatal("newest does not point at the end of the list")
	}
	if seen != len(c.entries) {
		t.Fatalf("list holds %d entries, map %d", seen, len(c.entries))
	}
}

// captureLog sends the standard logger to a buffer for the rest of the
// test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	return &logged
}

// TestDenialCoalescerCoalescesWithinWindow checks that identical
// denials inside the window are counted rather than recorded, and that
// the first denial after the window reports how many it stands for.
func TestDenialCoalescerCoalescesWithinWindow(t *testing.T) {
	var c denialCoalescer
	key := denialKeyFor("mallory", "user.create", "Permission denied")
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)

	if record, closed, _ := c.admit(key, "users", start); !record ||
		closed.count != 0 {
		t.Fatalf("first denial: expected (true, 0), got (%v, %d)", record,
			closed.count)
	}
	for i := 1; i <= 4; i++ {
		at := start.Add(time.Duration(i) * time.Second)
		if record, closed, _ := c.admit(key, "users", at); record ||
			closed.count != 0 {
			t.Errorf("repeat %d: expected (false, 0), got (%v, %d)", i, record,
				closed.count)
		}
	}
	if got := c.entries[key].lastSeen; !got.Equal(start.Add(4 * time.Second)) {
		t.Errorf("expected lastSeen at the latest repeat, got %v", got)
	}

	after := start.Add(denialCoalesceWindow + time.Second)
	record, closed, _ := c.admit(key, "users", after)
	if !record {
		t.Fatal("expected the denial after the window to be recorded")
	}
	if closed.count != 5 {
		t.Errorf("expected repeat_count 5 (4 suppressed plus this one), got %d",
			closed.count)
	}

	// The window has been reopened, so the next repeat is suppressed
	// again and carries no stale count.
	if record, closed, _ := c.admit(key, "users",
		after.Add(time.Second)); record || closed.count != 0 {
		t.Errorf("expected the reopened window to suppress, got (%v, %d)",
			record, closed.count)
	}
	checkDenialList(t, &c)
}

// TestDenialCoalescerWithoutRepeatsCarriesNoCount checks that a denial
// following a quiet window records no repeat_count at all.
func TestDenialCoalescerWithoutRepeatsCarriesNoCount(t *testing.T) {
	var c denialCoalescer
	key := denialKeyFor("mallory", "user.create", "Permission denied")
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)

	c.admit(key, "users", start)
	if record, closed, _ := c.admit(key, "users",
		start.Add(2*denialCoalesceWindow)); !record || closed.count != 0 {
		t.Errorf("expected (true, 0), got (%v, %d)", record, closed.count)
	}
}

// TestDenialCoalescerSeparatesKeys checks that one actor's repeats
// never suppress another's denial, nor another action or reason.
func TestDenialCoalescerSeparatesKeys(t *testing.T) {
	var c denialCoalescer
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	keys := []denialKey{
		denialKeyFor("mallory", "user.create", "Permission denied"),
		denialKeyFor("trudy", "user.create", "Permission denied"),
		denialKeyFor("mallory", "group.create", "Permission denied"),
		denialKeyFor("mallory", "user.create", "Something else"),
	}

	for _, key := range keys {
		if record, _, _ := c.admit(key, "users", start); !record {
			t.Errorf("expected the first denial for %+v to be recorded", key)
		}
	}
	for _, key := range keys {
		if record, _, _ := c.admit(key, "users",
			start.Add(time.Second)); record {
			t.Errorf("expected the repeat for %+v to be suppressed", key)
		}
	}
}

// TestDenialCoalescerEvictsExpiredEntries checks that entries whose
// window has closed do not accumulate, and that those holding repeats
// come back as summaries bounding their window.
func TestDenialCoalescerEvictsExpiredEntries(t *testing.T) {
	var c denialCoalescer
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		c.admit(denialKeyFor("mallory", "user.create",
			fmt.Sprintf("reason-%d", i)), "users", start)
	}
	repeated := denialKeyFor("mallory", "user.create", "reason-3")
	c.admit(repeated, "users/4", start.Add(time.Second))

	_, _, expired := c.admit(denialKeyFor("trudy", "user.create", "late"),
		"users", start.Add(2*denialCoalesceWindow))
	if got := len(c.entries); got != 1 {
		t.Errorf("expected the expired entries to be evicted, %d remain", got)
	}
	if len(expired) != 1 || expired[0].key != repeated ||
		expired[0].window.count != 1 || !expired[0].firstSeen.Equal(start) ||
		!expired[0].lastSeen.Equal(start.Add(time.Second)) {
		t.Errorf("unexpected summaries %+v", expired)
	}
	checkDenialList(t, &c)
}

// TestDenialCoalescerCapsMapSize checks that a client varying the
// reason cannot grow the map without bound: the cap holds, and the
// oldest entry is the one dropped.
func TestDenialCoalescerCapsMapSize(t *testing.T) {
	var c denialCoalescer
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < maxDenialKeys+50; i++ {
		// Every key is inside the same window, so nothing expires and
		// only the cap can bound the map.
		c.admit(denialKeyFor("mallory", "user.create",
			fmt.Sprintf("reason-%d", i)), "users",
			start.Add(time.Duration(i)*time.Microsecond))
	}

	if got := len(c.entries); got > maxDenialKeys {
		t.Errorf("expected at most %d tracked keys, got %d", maxDenialKeys, got)
	}
	if _, ok := c.entries[denialKeyFor("mallory", "user.create",
		"reason-0")]; ok {
		t.Error("expected the oldest entry to have been evicted")
	}
	checkDenialList(t, &c)
}

// TestDenialKeyOfCapsAction checks that the key's action is capped
// however long the action given to it, and that an actor's id is kept.
func TestDenialKeyOfCapsAction(t *testing.T) {
	id := int64(42)
	key := denialKeyOf(Actor{Type: ActorToken, ID: &id},
		strings.Repeat("a", 4096), "reason")
	if len(key.action) != maxDenialActionLen {
		t.Errorf("expected the action capped at %d bytes, got %d",
			maxDenialActionLen, len(key.action))
	}
	if key.actorID != 42 {
		t.Errorf("expected actor id 42, got %d", key.actorID)
	}
}

// TestDenialSummaryEventCarriesWindow checks that a summary row is
// attributed as the denials it counts and carries the window's count,
// targets and bounds.
func TestDenialSummaryEventCarriesWindow(t *testing.T) {
	id := int64(9)
	key := denialKeyOf(Actor{Type: ActorToken, ID: &id, Name: "mallory",
		IP: "192.0.2.5"}, "audit.read", "Permission denied")
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	summary := denialSummary{
		key:       key,
		window:    denialWindow{count: 4, targets: []string{"audit"}},
		firstSeen: start,
		lastSeen:  start.Add(30 * time.Second),
	}

	ev := summary.event()
	if ev.Outcome != OutcomeDenied || ev.Action != "audit.read" ||
		ev.Error != "Permission denied" || ev.ActorType != ActorToken ||
		ev.ActorID == nil || *ev.ActorID != 9 || ev.ActorName != "mallory" ||
		ev.ActorIP != "192.0.2.5" {
		t.Errorf("unexpected attribution %+v", ev)
	}
	want := `{"first_seen":"2026-09-15T10:00:00.000000000Z",` +
		`"last_seen":"2026-09-15T10:00:30.000000000Z","repeat_count":4,` +
		`"targets":["audit"],"targets_truncated":0,"window_closed":true}`
	if string(ev.Details) != want {
		t.Errorf("expected details %s, got %s", want, ev.Details)
	}

	text := summary.String()
	if !strings.Contains(text, `"mallory"`) ||
		!strings.Contains(text, `"192.0.2.5"`) ||
		!strings.Contains(text, ": 4 denial(s)") {
		t.Errorf("unexpected log description %q", text)
	}

	if anonymous := denialKeyFor("", "rbac.get", "no").event(nil); anonymous.ActorID != nil {
		t.Errorf("expected no actor id for a key without one, got %d",
			*anonymous.ActorID)
	}
}

// TestDenialCoalescerDrain checks that a drain takes only closed
// windows, oldest first, unless asked for all of them, and that only
// windows with suppressed repeats come back as summaries.
func TestDenialCoalescerDrain(t *testing.T) {
	var c denialCoalescer
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	old := denialKeyFor("bob", "user.create", "no")
	older := denialKeyFor("carol", "user.create", "no")
	quiet := denialKeyFor("dave", "user.create", "no")
	open := denialKeyFor("erin", "user.create", "no")

	c.admit(older, "users", start)
	c.admit(older, "users", start.Add(time.Second))
	c.admit(old, "users", start.Add(2*time.Second))
	c.admit(old, "users", start.Add(3*time.Second))
	c.admit(quiet, "users", start.Add(4*time.Second))
	c.admit(open, "users", start.Add(15*time.Second))
	c.admit(open, "users", start.Add(16*time.Second))

	summaries := c.drain(start.Add(denialCoalesceWindow+10*time.Second), false)
	if len(summaries) != 2 || summaries[0].key != older ||
		summaries[1].key != old {
		t.Fatalf("expected older then old, got %+v", summaries)
	}
	if _, ok := c.entries[quiet]; ok {
		t.Error("expected the closed window without repeats to be dropped")
	}
	if _, ok := c.entries[open]; !ok {
		t.Error("expected the open window to be kept")
	}
	checkDenialList(t, &c)

	summaries = c.drain(start, true)
	if len(summaries) != 1 || summaries[0].key != open {
		t.Errorf("expected the open window flushed, got %+v", summaries)
	}
	if len(c.entries) != 0 || c.oldest != nil || c.newest != nil {
		t.Error("expected a full drain to empty the coalescer")
	}
}

// TestDenialCoalescerRestore checks that restore puts entries back in
// firstSeen order among those already present, leaves an entry that was
// re-created since alone, stops at the cap, and logs each count it
// cannot keep.
func TestDenialCoalescerRestore(t *testing.T) {
	var empty denialCoalescer
	empty.restore([]denialSummary{{key: denialKeyFor("carol", "a", "b"),
		window: denialWindow{count: 1}}})
	if len(empty.entries) != 1 {
		t.Errorf("restore into an empty coalescer kept %d entries",
			len(empty.entries))
	}

	var c denialCoalescer
	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	present := denialKeyFor("present", "user.create", "no")
	c.admit(present, "users", start.Add(10*time.Second))
	fresh := c.entries[present]

	logged := captureLog(t)
	c.restore([]denialSummary{
		{key: denialKeyFor("late", "user.create", "no"),
			window: denialWindow{count: 2}, firstSeen: start.Add(20 * time.Second)},
		{key: denialKeyFor("early", "user.create", "no"),
			window: denialWindow{count: 3}, firstSeen: start},
		{key: present, window: denialWindow{count: 5},
			firstSeen: start.Add(5 * time.Second)},
	})
	checkDenialList(t, &c)
	if c.entries[present] != fresh {
		t.Error("restore replaced an entry re-created since the drain")
	}
	if c.oldest.key.actorName != "early" || c.newest.key.actorName != "late" {
		t.Errorf("unexpected order: oldest %q, newest %q",
			c.oldest.key.actorName, c.newest.key.actorName)
	}
	if state := c.entries[denialKeyFor("late", "user.create", "no")]; state.suppressed.count != 2 {
		t.Errorf("expected late restored with 2 repeats, got %+v", state)
	}
	if line := logged.String(); !strings.Contains(line, "window opened since") ||
		!strings.Contains(line, ": 5 denial(s)") {
		t.Errorf("expected the displaced summary logged, got %q", line)
	}

	logged.Reset()
	var full denialCoalescer
	for i := 0; i < maxDenialKeys; i++ {
		full.admit(denialKeyFor(fmt.Sprintf("user-%d", i), "user.create",
			"no"), "users", start)
	}
	full.restore([]denialSummary{{key: denialKeyFor("evicted",
		"user.create", "no"), window: denialWindow{count: 7}}})
	if line := logged.String(); !strings.Contains(line, "coalescing cap") ||
		!strings.Contains(line, `"evicted"`) ||
		!strings.Contains(line, ": 7 denial(s)") {
		t.Errorf("expected the dropped summary's key and count logged, got %q",
			line)
	}
	if len(full.entries) != maxDenialKeys {
		t.Errorf("restore took the map to %d entries, cap %d",
			len(full.entries), maxDenialKeys)
	}
}

// deniedReason is the reason repeatDenial gives.
const deniedReason = "Permission denied: requires superuser privileges"

// deniedActor is the principal repeatDenial records, named name.
func deniedActor(name string) Actor {
	return Actor{Type: ActorUser, Name: name, IP: "192.0.2.1"}
}

// repeatDenial records the same denial n times through the store,
// alternating between two targets, and returns the error of the last
// one.
func repeatDenial(s *AuthStore, name string, n int) error {
	var err error
	for i := 0; i < n; i++ {
		err = s.RecordCoalescedDenial(deniedActor(name), "audit.read",
			fmt.Sprintf("audit/%d", i%2), deniedReason)
	}

	return err
}

// mustRepeatDenial is repeatDenial for a store whose writes succeed.
func mustRepeatDenial(t *testing.T, s *AuthStore, name string, n int) {
	t.Helper()

	if err := repeatDenial(s, name, n); err != nil {
		t.Fatalf("RecordCoalescedDenial failed: %v", err)
	}
}

// TestRecordCoalescedDenialWritesFirstAndCount checks the store path: a
// burst writes one row naming its target, and the row closing the
// window carries the count.
func TestRecordCoalescedDenialWritesFirstAndCount(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	before := auditEventCount(t, store)
	mustRepeatDenial(t, store, "mallory", 3)
	if got := auditEventCount(t, store) - before; got != 1 {
		t.Fatalf("expected the burst to write one row, got %d", got)
	}
	ev := lastAuditEvent(t, store)
	if ev.Outcome != OutcomeDenied || ev.ActorName != "mallory" ||
		string(ev.Details) != `{"target":"audit/0"}` {
		t.Errorf("unexpected first row %+v", ev)
	}

	AgeCoalescedAuditForTesting(store, time.Hour)
	mustRepeatDenial(t, store, "mallory", 1)
	details := auditDetails(t, lastAuditEvent(t, store))
	if details["repeat_count"] != float64(3) || details["target"] != "audit/0" {
		t.Errorf("unexpected closing row %v", details)
	}
}

// TestSweepCoalescedAuditWritesClosedDenials checks that the periodic
// sweep writes the count of a denial burst that has stopped, bounded by
// first_seen and last_seen, in the same batch as a failure burst, and
// leaves a window still open alone.
func TestSweepCoalescedAuditWritesClosedDenials(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustRepeatDenial(t, store, "mallory", 4)
	repeatFailure(store, "bob", 3)
	AgeCoalescedAuditForTesting(store, time.Hour)
	// A further denial would evict the closed window itself, so the
	// window still open is put in place directly.
	trudy := denialKeyOf(deniedActor("trudy"), "audit.read", deniedReason)
	store.denials.restore([]denialSummary{{key: trudy,
		window: denialWindow{count: 1}, firstSeen: time.Now()}})

	before := auditEventCount(t, store)
	store.SweepCoalescedAudit()
	if got := auditEventCount(t, store) - before; got != 2 {
		t.Fatalf("expected summaries for mallory and bob, got %d rows", got)
	}
	ev := lastAuditEvent(t, store)
	details := auditDetails(t, ev)
	if ev.ActorName != "mallory" || ev.Outcome != OutcomeDenied ||
		details["repeat_count"] != float64(3) ||
		details["window_closed"] != true || details["first_seen"] == nil ||
		details["last_seen"] == nil {
		t.Errorf("unexpected denial summary %q %v", ev.ActorName, details)
	}
	if targets := stringsOf(t, details["targets"]); strings.Join(targets, ",") !=
		"audit/1,audit/0" {
		t.Errorf("unexpected summary targets %v", targets)
	}
	if _, ok := store.denials.entries[trudy]; !ok {
		t.Error("the sweep dropped a denial window that is still open")
	}

	_, firstBad, err := store.VerifyAuditChain()
	if err != nil || firstBad != 0 {
		t.Errorf("chain broken after a batched sweep: firstBad %d, err %v",
			firstBad, err)
	}
}

// TestCloseWritesOpenDenialWindows checks the case the issue reports: a
// burst of identical denials that stops, followed by a clean shutdown,
// leaves its count in the log rather than only its first row.
func TestCloseWritesOpenDenialWindows(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAuthStore(dir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore failed: %v", err)
	}

	mustRepeatDenial(t, store, "mallory", 100)
	mustRepeatDenial(t, store, "trudy", 1)
	before := auditEventCount(t, store)
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	reopened, err := NewAuthStore(dir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("reopening the store failed: %v", err)
	}
	defer reopened.Close()

	if got := auditEventCount(t, reopened) - before; got != 1 {
		t.Fatalf("expected one summary row written at Close, got %d", got)
	}
	ev := lastAuditEvent(t, reopened)
	details := auditDetails(t, ev)
	if ev.ActorName != "mallory" || details["repeat_count"] != float64(99) ||
		details["window_closed"] != true {
		t.Errorf("unexpected shutdown summary %q %v", ev.ActorName, details)
	}

	_, firstBad, err := reopened.VerifyAuditChain()
	if err != nil || firstBad != 0 {
		t.Errorf("chain broken after the shutdown summary: firstBad %d, err %v",
			firstBad, err)
	}
}

// TestSweepCoalescedAuditRestoresDenialsOnWriteError checks that a
// sweep whose write fails puts the drained denial windows back for a
// later attempt, and that only a flush, which may have no later
// attempt, also writes them to the server log.
func TestSweepCoalescedAuditRestoresDenialsOnWriteError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustRepeatDenial(t, store, "mallory", 4)
	if err := store.db.Close(); err != nil {
		t.Fatalf("closing the database failed: %v", err)
	}
	logged := captureLog(t)

	AgeCoalescedAuditForTesting(store, time.Hour)
	store.SweepCoalescedAudit()
	if strings.Contains(logged.String(), "Unrecorded audit denial summary") {
		t.Errorf("a periodic sweep logged its summaries: %q", logged.String())
	}
	if len(store.denials.entries) != 1 ||
		store.denials.oldest.suppressed.count != 3 {
		t.Fatalf("expected mallory restored with 3 repeats, got %d entries",
			len(store.denials.entries))
	}

	logged.Reset()
	store.FlushCoalescedAudit()
	line := logged.String()
	if !strings.Contains(line, "Unrecorded audit denial summary at shutdown") ||
		!strings.Contains(line, `"mallory"`) ||
		!strings.Contains(line, ": 3 denial(s)") {
		t.Errorf("expected the unwritten summary's key and count logged, got %q",
			line)
	}
	checkDenialList(t, &store.denials)
}

// TestCoalesceDenialRestoresOnWriteError checks that a summary the
// evict path cannot write is logged and put back, as the sweep does,
// and that a denial whose own row cannot be written is reported to the
// caller and does not keep a window open behind that missing row.
func TestCoalesceDenialRestoresOnWriteError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustRepeatDenial(t, store, "mallory", 3)
	AgeCoalescedAuditForTesting(store, time.Hour)
	if err := store.db.Close(); err != nil {
		t.Fatalf("closing the database failed: %v", err)
	}
	logged := captureLog(t)

	if err := repeatDenial(store, "trudy", 1); err == nil ||
		!strings.Contains(err.Error(), "failed to record denial") {
		t.Errorf("expected the unwritten denial reported, got %v", err)
	}

	if len(store.denials.entries) != 1 ||
		store.denials.oldest.key.actorName != "mallory" ||
		store.denials.oldest.suppressed.count != 2 {
		t.Fatalf("expected only mallory restored, with 2 repeats, got %d "+
			"entries", len(store.denials.entries))
	}
	checkDenialList(t, &store.denials)
	if line := logged.String(); !strings.Contains(line,
		"Failed to record 1 audit denial summary") {
		t.Errorf("expected the summary write failure logged, got %q", line)
	}
}

// TestDenialCoalescerReopen checks that a denial whose row would have
// closed a window holding repeats, but could not be written, puts that
// window back in its place on the eviction list, holding those repeats
// plus itself, so the next identical denial is recorded at once and
// reports them all; and that one closing nothing drops its entry.
func TestDenialCoalescerReopen(t *testing.T) {
	var c denialCoalescer
	key := denialKeyFor("mallory", "audit.read", "denied")
	other := denialKeyFor("trudy", "audit.read", "denied")
	start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	c.admit(key, "audit/1", start)
	c.admit(other, "audit/1", start.Add(time.Second))
	c.admit(key, "audit/2", start.Add(2*time.Second))
	c.admit(key, "audit/3", start.Add(3*time.Second))

	closing := start.Add(denialCoalesceWindow)
	record, closed, _ := c.admit(key, "audit/4", closing)
	if !record || closed.count != 3 {
		t.Fatalf("expected the closing denial recorded for 3, got %v, %d",
			record, closed.count)
	}
	c.reopen(key, closed)

	state := c.entries[key]
	if !state.firstSeen.Equal(start) || !state.lastSeen.Equal(closing) ||
		state.suppressed.count != 3 ||
		strings.Join(state.suppressed.targets, ",") !=
			"audit/2,audit/3,audit/4" {
		t.Fatalf("expected the closed window back with 3 repeats, got %+v",
			state)
	}
	if c.oldest != state {
		t.Error("expected the reopened window back at the oldest end")
	}
	checkDenialList(t, &c)

	record, closed, _ = c.admit(key, "audit/5", closing.Add(time.Second))
	if !record || closed.count != 4 {
		t.Errorf("expected the next denial recorded for 4, got %v, %d",
			record, closed.count)
	}

	c.reopen(key, denialWindow{})
	if _, ok := c.entries[key]; ok {
		t.Error("reopen closing nothing kept the entry")
	}
	checkDenialList(t, &c)
	c.reopen(key, closed)
	if _, ok := c.entries[key]; ok {
		t.Error("reopen of a missing key created an entry")
	}
}

// TestCoalesceDenialReopensOnWriteError checks, through the store, that
// the repeats a closing denial would have reported survive that
// denial's row failing to be written, and reach the log in the summary
// that Close writes once the store can write again.
func TestCoalesceDenialReopensOnWriteError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustRepeatDenial(t, store, "mallory", 3)
	AgeCoalescedAuditForTesting(store, time.Hour)

	db := store.db
	broken, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatalf("opening a stand-in database failed: %v", err)
	}
	if err := broken.Close(); err != nil {
		t.Fatalf("closing the stand-in database failed: %v", err)
	}
	store.db = broken
	err = repeatDenial(store, "mallory", 1)
	store.db = db
	if err == nil {
		t.Fatal("expected the unwritten closing denial reported")
	}

	state := store.denials.entries[denialKeyOf(deniedActor("mallory"),
		"audit.read", deniedReason)]
	if state == nil || state.suppressed.count != 3 {
		t.Fatalf("expected the closed window kept with 3 repeats, got %+v",
			state)
	}

	before := auditEventCount(t, store)
	store.FlushCoalescedAudit()
	if got := auditEventCount(t, store) - before; got != 1 {
		t.Fatalf("expected one summary row at flush, got %d", got)
	}
	ev := lastAuditEvent(t, store)
	var details struct {
		RepeatCount int `json:"repeat_count"`
	}
	if err := json.Unmarshal(ev.Details, &details); err != nil {
		t.Fatalf("decoding details %s failed: %v", ev.Details, err)
	}
	if details.RepeatCount != 3 {
		t.Errorf("expected the summary to carry 3 repeats, got %s",
			ev.Details)
	}
}
