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
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCapAuditError(t *testing.T) {
	exact := strings.Repeat("a", maxAuditErrorBytes)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"short", "boom", "boom"},
		{"exactly the cap", exact, exact},
		{"one over the cap", exact + "b",
			strings.Repeat("a", maxAuditErrorBytes-len(auditErrorTruncated)) +
				auditErrorTruncated},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := capAuditError(tc.in); got != tc.want {
				t.Errorf("capAuditError(%d bytes) = %d bytes %q, want %q",
					len(tc.in), len(got), got, tc.want)
			}
		})
	}
}

// TestCapAuditErrorKeepsRunesWhole checks that the cut never splits a
// multi-byte character, whatever its alignment against the cap.
func TestCapAuditErrorKeepsRunesWhole(t *testing.T) {
	for pad := 0; pad < 4; pad++ {
		in := strings.Repeat("x", pad) + strings.Repeat("€", maxAuditErrorBytes)
		got := capAuditError(in)
		if len(got) > maxAuditErrorBytes {
			t.Errorf("pad %d: capped text is %d bytes", pad, len(got))
		}
		if !utf8.ValidString(got) {
			t.Errorf("pad %d: capped text is not valid UTF-8: %q", pad, got)
		}
		if !strings.HasSuffix(got, auditErrorTruncated) {
			t.Errorf("pad %d: capped text lacks the marker: %q", pad, got)
		}
	}
}

func failureKeyFor(target string) failureKey {
	return failureKeyOf(testActor(), "user.create", "user", nil, target,
		"user already exists")
}

func TestFailureCoalescerCoalescesWithinWindow(t *testing.T) {
	var c failureCoalescer
	key := failureKeyFor("bob")
	start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

	if record, repeats, _ := c.admit(key, start); !record || repeats != 0 {
		t.Fatalf("first failure: record=%v repeats=%d", record, repeats)
	}
	for i := 1; i <= 3; i++ {
		at := start.Add(time.Duration(i) * time.Second)
		if record, _, _ := c.admit(key, at); record {
			t.Fatalf("repeat %d inside the window was recorded", i)
		}
	}

	record, repeats, _ := c.admit(key, start.Add(failureCoalesceWindow))
	if !record || repeats != 4 {
		t.Errorf("after the window: record=%v repeats=%d, want true/4",
			record, repeats)
	}

	// The window reopened, and nothing was suppressed in it, so the
	// next failure after it carries no count.
	record, repeats, _ = c.admit(key, start.Add(2*failureCoalesceWindow))
	if !record || repeats != 0 {
		t.Errorf("quiet window: record=%v repeats=%d, want true/0",
			record, repeats)
	}
}

func TestFailureCoalescerSeparatesKeys(t *testing.T) {
	var c failureCoalescer
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	id := int64(7)
	other := testActor()
	other.IP = "192.0.2.2"

	keys := []failureKey{
		failureKeyFor("bob"),
		failureKeyFor("carol"),
		failureKeyOf(testActor(), "user.update", "user", nil, "bob",
			"user already exists"),
		failureKeyOf(testActor(), "user.create", "user", nil, "bob", "other"),
		failureKeyOf(testActor(), "user.create", "user", &id, "bob",
			"user already exists"),
		failureKeyOf(other, "user.create", "user", nil, "bob",
			"user already exists"),
	}
	for i, key := range keys {
		if record, _, _ := c.admit(key, now); !record {
			t.Errorf("key %d coalesced with an earlier, different key", i)
		}
	}
}

// TestFailureCoalescerSummarisesExpired checks that an entry whose
// window closed with suppressed repeats is returned as a summary when
// evicted, and one without repeats is dropped silently.
func TestFailureCoalescerSummarisesExpired(t *testing.T) {
	var c failureCoalescer
	start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	noisy := failureKeyFor("bob")
	quiet := failureKeyFor("carol")

	c.admit(noisy, start)
	c.admit(noisy, start.Add(time.Second))
	c.admit(noisy, start.Add(2*time.Second))
	c.admit(quiet, start)

	_, _, expired := c.admit(failureKeyFor("dave"),
		start.Add(failureCoalesceWindow))
	if len(expired) != 1 || expired[0].key != noisy ||
		expired[0].suppressed != 2 {
		t.Fatalf("expired = %+v, want one summary of 2 for bob", expired)
	}
	if !expired[0].firstSeen.Equal(start) ||
		!expired[0].lastSeen.Equal(start.Add(2*time.Second)) {
		t.Errorf("summary window = %v to %v, want %v to %v",
			expired[0].firstSeen, expired[0].lastSeen, start,
			start.Add(2*time.Second))
	}
	if len(c.entries) != 1 {
		t.Errorf("expected only the new key to remain, got %d", len(c.entries))
	}
}

func TestFailureCoalescerCapsMapSize(t *testing.T) {
	var c failureCoalescer
	start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

	oldest := failureKeyFor("target-0")
	c.admit(oldest, start)
	c.admit(oldest, start.Add(time.Millisecond))
	for i := 1; i < maxFailureKeys; i++ {
		c.admit(failureKeyFor(fmt.Sprintf("target-%d", i)),
			start.Add(time.Duration(i)*time.Millisecond))
	}

	last := failureKeyFor("newest")
	_, _, expired := c.admit(last, start.Add(time.Second))
	if len(c.entries) != maxFailureKeys {
		t.Errorf("expected %d entries, got %d", maxFailureKeys, len(c.entries))
	}
	if _, ok := c.entries[oldest]; ok {
		t.Error("expected the oldest entry to be evicted")
	}
	if _, ok := c.entries[last]; !ok {
		t.Error("expected the key just handled to be kept")
	}
	if len(expired) != 1 || expired[0].suppressed != 1 {
		t.Errorf("expected one summary for the evicted entry, got %+v", expired)
	}
}

func TestFailureKeyEventRestoresAttribution(t *testing.T) {
	id := int64(42)
	key := failureKeyOf(testActor(), "token.delete", "token", &id, "",
		"token not found")
	ev := key.event(nil)
	assertUserActor(t, *ev)
	if ev.TargetID == nil || *ev.TargetID != 42 {
		t.Errorf("expected target id 42, got %v", ev.TargetID)
	}
	if ev.Outcome != OutcomeFailure || ev.Error != "token not found" {
		t.Errorf("unexpected outcome/error %q/%q", ev.Outcome, ev.Error)
	}

	anon := failureKeyOf(Actor{Type: ActorSystem, Name: "system"},
		"user.create", "user", nil, "bob", "boom").event(nil)
	if anon.ActorID != nil || anon.TargetID != nil {
		t.Errorf("expected nil ids, got actor %v target %v",
			anon.ActorID, anon.TargetID)
	}
}

// TestRecordFailureCoalescesRepeats drives the store end to end: a
// failure repeated inside the window writes one row, the next one
// after the window reports the count, and a burst that stops is
// written as a summary when a later failure evicts it.
func TestRecordFailureCoalescesRepeats(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	mustCreateUser(t, store, "bob")
	as := store.AsActor(testActor())

	before := auditEventCount(t, store)
	for i := 0; i < 5; i++ {
		if err := as.CreateUser("bob", "Str0ngPassphrase!", "", "", ""); err == nil {
			t.Fatal("expected a duplicate user to fail")
		}
	}
	if got := auditEventCount(t, store) - before; got != 1 {
		t.Fatalf("expected 1 row for 5 identical failures, got %d", got)
	}

	// Close the window by moving its start back rather than waiting.
	ageEntries(store, failureCoalesceWindow)
	if err := as.CreateUser("bob", "Str0ngPassphrase!", "", "", ""); err == nil {
		t.Fatal("expected a duplicate user to fail")
	}
	ev := lastAuditEvent(t, store)
	if got := auditDetails(t, ev)["repeat_count"]; got != float64(5) {
		t.Errorf("expected repeat_count 5, got %v", got)
	}

	// Two more repeats inside the new window, then the window closes
	// and a different failure evicts the entry.
	for i := 0; i < 2; i++ {
		_ = as.CreateUser("bob", "Str0ngPassphrase!", "", "", "")
	}
	ageEntries(store, failureCoalesceWindow)
	before = auditEventCount(t, store)
	if err := as.DeleteUser("nobody"); err == nil {
		t.Fatal("expected deleting a missing user to fail")
	}
	if got := auditEventCount(t, store) - before; got != 2 {
		t.Fatalf("expected a summary row and the new failure, got %d", got)
	}

	events, _, err := store.ListAuditEvents(AuditFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	summary := events[1]
	if summary.Action != "user.create" || summary.TargetName != "bob" ||
		summary.Outcome != OutcomeFailure {
		t.Errorf("unexpected summary row %+v", summary)
	}
	assertUserActor(t, summary)
	details := auditDetails(t, summary)
	if details["repeat_count"] != float64(2) || details["window_closed"] != true {
		t.Errorf("unexpected summary details %v", details)
	}
}

// ageEntries moves every tracked window's start back by d.
func ageEntries(s *AuthStore, d time.Duration) {
	s.failures.mu.Lock()
	defer s.failures.mu.Unlock()
	for _, state := range s.failures.entries {
		state.firstSeen = state.firstSeen.Add(-d)
	}
}

func TestRecordFailureCapsErrorText(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	store.mu.Lock()
	store.recordFailure(testActor(), "user.create", "user", nil, "bob",
		errors.New(strings.Repeat("e", 4*maxAuditErrorBytes)))
	store.recordFailure(testActor(), "user.create", "user", nil, "carol", nil)
	store.mu.Unlock()

	events, _, err := store.ListAuditEvents(AuditFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	if events[0].Error != "unknown error" {
		t.Errorf("expected unknown error for a nil cause, got %q",
			events[0].Error)
	}
	if got := len(events[1].Error); got != maxAuditErrorBytes {
		t.Errorf("expected error capped at %d bytes, got %d",
			maxAuditErrorBytes, got)
	}
}

func TestRecordDeniedCapsReason(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	if err := store.RecordDenied(testActor(), "user.create",
		strings.Repeat("r", 2*maxAuditErrorBytes)); err != nil {
		t.Fatalf("RecordDenied failed: %v", err)
	}
	ev := lastAuditEvent(t, store)
	if len(ev.Error) != maxAuditErrorBytes ||
		!strings.HasSuffix(ev.Error, auditErrorTruncated) {
		t.Errorf("expected a capped reason, got %d bytes", len(ev.Error))
	}
}

// TestWriteFailureLogsStoreError checks that a failure event that
// cannot be written is logged rather than returned or panicking.
func TestWriteFailureLogsStoreError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	if err := store.db.Close(); err != nil {
		t.Fatalf("closing the database failed: %v", err)
	}
	store.writeFailure(failureKeyFor("bob").event(nil))
}

// TestFailureCoalescerDrain checks that a sweep takes only the windows
// that have closed, that a final drain takes every window, that both
// report only entries holding repeats, and that summaries come back
// in the order their windows opened.
func TestFailureCoalescerDrain(t *testing.T) {
	var c failureCoalescer
	start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	early := failureKeyFor("early")
	late := failureKeyFor("late")
	quiet := failureKeyFor("quiet")
	open := failureKeyFor("open")

	// Admit late before early so that map order cannot pass for the
	// sort.
	c.admit(late, start.Add(time.Second))
	c.admit(late, start.Add(2*time.Second))
	c.admit(early, start)
	c.admit(early, start.Add(3*time.Second))
	c.admit(quiet, start)
	c.admit(open, start.Add(30*time.Second))
	c.admit(open, start.Add(31*time.Second))

	swept := c.drain(start.Add(failureCoalesceWindow+2*time.Second), false)
	if len(swept) != 2 || swept[0].key != early || swept[1].key != late {
		t.Fatalf("swept = %+v, want early then late", swept)
	}
	if len(c.entries) != 1 {
		t.Fatalf("expected only the open window to remain, got %d",
			len(c.entries))
	}

	final := c.drain(start, true)
	if len(final) != 1 || final[0].key != open || final[0].suppressed != 1 {
		t.Fatalf("final = %+v, want one summary of 1 for open", final)
	}
	if len(c.entries) != 0 {
		t.Errorf("expected a final drain to empty the map, got %d",
			len(c.entries))
	}
	if got := c.drain(start, true); len(got) != 0 {
		t.Errorf("draining an empty coalescer returned %+v", got)
	}
}

func TestFailureSummaryEventCarriesWindow(t *testing.T) {
	start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	summary := failureSummary{
		key:        failureKeyFor("bob"),
		suppressed: 3,
		firstSeen:  start,
		lastSeen:   start.Add(30 * time.Second),
	}

	ev := summary.event()
	details := auditDetails(t, *ev)
	if details["repeat_count"] != float64(3) || details["window_closed"] != true {
		t.Errorf("unexpected summary details %v", details)
	}
	if details["first_seen"] != start.Format(auditTimeLayout) ||
		details["last_seen"] != start.Add(30*time.Second).Format(auditTimeLayout) {
		t.Errorf("unexpected summary window %v to %v",
			details["first_seen"], details["last_seen"])
	}
}

// ageEntry moves one tracked window's start back by d.
func ageEntry(s *AuthStore, key failureKey, d time.Duration) {
	s.failures.mu.Lock()
	defer s.failures.mu.Unlock()
	state := s.failures.entries[key]
	state.firstSeen = state.firstSeen.Add(-d)
}

// repeatFailure records the same failure n times through the store.
func repeatFailure(s *AuthStore, target string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < n; i++ {
		s.recordFailure(testActor(), "user.create", "user", nil, target,
			errors.New("user already exists"))
	}
}

// TestSweepAuditFailuresWritesClosedWindows checks that the periodic
// sweep writes a burst that has stopped, in one batch that keeps the
// chain intact, and leaves a window that is still open alone.
func TestSweepAuditFailuresWritesClosedWindows(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	repeatFailure(store, "bob", 4)
	repeatFailure(store, "carol", 3)
	repeatFailure(store, "dave", 2)
	ageEntry(store, failureKeyFor("bob"), failureCoalesceWindow)
	ageEntry(store, failureKeyFor("carol"), failureCoalesceWindow)

	before := auditEventCount(t, store)
	store.SweepAuditFailures()
	if got := auditEventCount(t, store) - before; got != 2 {
		t.Fatalf("expected summaries for bob and carol, got %d rows", got)
	}

	events, _, err := store.ListAuditEvents(AuditFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	counts := map[string]any{}
	for _, ev := range events {
		details := auditDetails(t, ev)
		if details["window_closed"] != true || details["first_seen"] == nil ||
			details["last_seen"] == nil {
			t.Errorf("unexpected summary details %v", details)
		}
		counts[ev.TargetName] = details["repeat_count"]
	}
	if counts["bob"] != float64(3) || counts["carol"] != float64(2) {
		t.Errorf("unexpected summary counts %v", counts)
	}

	if _, ok := store.failures.entries[failureKeyFor("dave")]; !ok {
		t.Error("the sweep dropped a window that is still open")
	}

	_, firstBad, err := store.VerifyAuditChain()
	if err != nil || firstBad != 0 {
		t.Errorf("chain broken after a batched sweep: firstBad %d, err %v",
			firstBad, err)
	}
}

// TestCloseWritesOpenFailureWindows checks that a clean shutdown writes
// the counts of windows still open rather than discarding them.
func TestCloseWritesOpenFailureWindows(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAuthStore(dir, 0, 0, AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore failed: %v", err)
	}

	repeatFailure(store, "bob", 100)
	repeatFailure(store, "carol", 1)
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
	if ev.TargetName != "bob" || details["repeat_count"] != float64(99) ||
		details["window_closed"] != true {
		t.Errorf("unexpected shutdown summary %q %v", ev.TargetName, details)
	}

	_, firstBad, err := reopened.VerifyAuditChain()
	if err != nil || firstBad != 0 {
		t.Errorf("chain broken after the shutdown summary: firstBad %d, err %v",
			firstBad, err)
	}
}

// TestWriteFailureSummariesLogsStoreError checks that a batch of
// summaries that cannot be written is logged rather than returned or
// panicking.
func TestWriteFailureSummariesLogsStoreError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	if err := store.db.Close(); err != nil {
		t.Fatalf("closing the database failed: %v", err)
	}
	store.mu.Lock()
	store.writeFailureSummaries([]failureSummary{{
		key:        failureKeyFor("bob"),
		suppressed: 1,
	}})
	store.mu.Unlock()
}

// TestSweepAuditFailuresRestoresOnWriteError checks that a sweep whose
// write fails puts the drained entries back for a later attempt, and
// that restore leaves alone an entry re-created since the drain.
func TestSweepAuditFailuresRestoresOnWriteError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	repeatFailure(store, "bob", 3)
	ageEntry(store, failureKeyFor("bob"), failureCoalesceWindow)
	if err := store.db.Close(); err != nil {
		t.Fatalf("closing the database failed: %v", err)
	}

	store.sweepAuditFailures(time.Now(), false)

	state, ok := store.failures.entries[failureKeyFor("bob")]
	if !ok || state.suppressed != 2 {
		t.Fatalf("expected bob restored with 2 repeats, got %+v", state)
	}

	fresh := &failureState{firstSeen: time.Now()}
	store.failures.entries[failureKeyFor("bob")] = fresh
	store.failures.restore([]failureSummary{{
		key:        failureKeyFor("bob"),
		suppressed: 5,
	}})
	if store.failures.entries[failureKeyFor("bob")] != fresh {
		t.Error("restore replaced an entry re-created since the drain")
	}

	var empty failureCoalescer
	empty.restore([]failureSummary{{key: failureKeyFor("carol"),
		suppressed: 1}})
	if len(empty.entries) != 1 {
		t.Errorf("restore into an empty coalescer kept %d entries",
			len(empty.entries))
	}
}

// TestCoalesceFailureRestoresOnWriteError checks that a summary the
// evict path cannot write is put back, as the sweep does, rather than
// its count being lost with the failed transaction, and that a failure
// whose own row cannot be written does not leave a window open to
// suppress the identical failures after it.
func TestCoalesceFailureRestoresOnWriteError(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	repeatFailure(store, "bob", 3)
	ageEntry(store, failureKeyFor("bob"), failureCoalesceWindow)
	if err := store.db.Close(); err != nil {
		t.Fatalf("closing the database failed: %v", err)
	}

	repeatFailure(store, "carol", 1)

	state, ok := store.failures.entries[failureKeyFor("bob")]
	if !ok || state.suppressed != 2 {
		t.Fatalf("expected bob restored with 2 repeats, got %+v", state)
	}
	if _, ok := store.failures.entries[failureKeyFor("carol")]; ok {
		t.Error("a failure whose row was not written left its window open")
	}
}

// TestFailureCoalescerRestoreStopsAtCap checks that restore never takes
// the map past its cap, so a store whose writes keep failing cannot
// grow it without bound.
func TestFailureCoalescerRestoreStopsAtCap(t *testing.T) {
	var c failureCoalescer
	start := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	for i := 0; i < maxFailureKeys; i++ {
		c.admit(failureKeyFor(fmt.Sprintf("target-%d", i)), start)
	}

	c.restore([]failureSummary{{key: failureKeyFor("evicted"),
		suppressed: 1}})
	if len(c.entries) != maxFailureKeys {
		t.Errorf("restore took the map to %d entries, cap %d",
			len(c.entries), maxFailureKeys)
	}
	if _, ok := c.entries[failureKeyFor("evicted")]; ok {
		t.Error("restore added an entry past the cap")
	}
}

// TestFlushAuditFailuresWritesOnce checks that the exported sweep
// writes a window aged shut by AgeAuditFailuresForTesting, that a flush
// writes a window still open, and that a second flush finds nothing
// left to write.
func TestFlushAuditFailuresWritesOnce(t *testing.T) {
	store, cleanup := createTestAuthStoreForAudit(t)
	defer cleanup()

	repeatFailure(store, "bob", 3)
	AgeAuditFailuresForTesting(store, failureCoalesceWindow)
	before := auditEventCount(t, store)
	store.SweepAuditFailures()
	if got := auditEventCount(t, store) - before; got != 1 {
		t.Fatalf("expected the sweep to write one summary, got %d", got)
	}
	if details := auditDetails(t, lastAuditEvent(t, store)); details["repeat_count"] != float64(2) {
		t.Errorf("unexpected sweep summary %v", details)
	}

	repeatFailure(store, "carol", 2)
	before = auditEventCount(t, store)
	store.FlushAuditFailures()
	store.FlushAuditFailures()
	if got := auditEventCount(t, store) - before; got != 1 {
		t.Errorf("expected two flushes to write one summary, got %d", got)
	}
}
