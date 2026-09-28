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
