/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package overview

import (
	"sync"
	"testing"
	"time"
)

func TestSubscribeAndReceive(t *testing.T) {
	hub := NewHub()
	sub := hub.Subscribe("")

	ov := newTestOverview("test")
	hub.Broadcast(ov, "")

	select {
	case got := <-sub.C:
		if got != ov {
			t.Fatalf("expected broadcast overview, got %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}

	hub.Unsubscribe(sub)
}

func TestUnsubscribeCleanup(t *testing.T) {
	hub := NewHub()
	sub := hub.Subscribe("")

	hub.Unsubscribe(sub)

	if hub.Count() != 0 {
		t.Fatalf("expected 0 subscribers after unsubscribe, got %d", hub.Count())
	}

	// Channel should be closed.
	_, open := <-sub.C
	if open {
		t.Fatal("expected subscriber channel to be closed")
	}
}

func TestDoubleUnsubscribe(t *testing.T) {
	hub := NewHub()
	sub := hub.Subscribe("")

	hub.Unsubscribe(sub)
	// Second unsubscribe should be a no-op, not panic.
	hub.Unsubscribe(sub)

	if hub.Count() != 0 {
		t.Fatalf("expected 0 subscribers, got %d", hub.Count())
	}
}

func TestScopedFilteringEstateWide(t *testing.T) {
	hub := NewHub()
	estate := hub.Subscribe("")
	scoped := hub.Subscribe("conn-123")

	ov := newTestOverview("test")
	hub.Broadcast(ov, "")

	// Estate-wide subscriber should receive it.
	select {
	case got := <-estate.C:
		if got != ov {
			t.Fatal("estate subscriber did not receive broadcast")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for estate broadcast")
	}

	// Scoped subscriber should NOT receive an estate-wide broadcast.
	select {
	case <-scoped.C:
		t.Fatal("scoped subscriber should not receive estate-wide broadcast")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}

	hub.Unsubscribe(estate)
	hub.Unsubscribe(scoped)
}

func TestScopedFilteringMatchingScope(t *testing.T) {
	hub := NewHub()
	estate := hub.Subscribe("")
	matching := hub.Subscribe("conn-123")
	other := hub.Subscribe("conn-456")

	ov := newTestOverview("test")
	hub.Broadcast(ov, "conn-123")

	// Matching subscriber should receive it.
	select {
	case got := <-matching.C:
		if got != ov {
			t.Fatal("matching subscriber did not receive scoped broadcast")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for scoped broadcast")
	}

	// Estate-wide subscriber should NOT receive a scoped broadcast.
	select {
	case <-estate.C:
		t.Fatal("estate subscriber should not receive scoped broadcast")
	case <-time.After(50 * time.Millisecond):
	}

	// Different-scope subscriber should NOT receive it.
	select {
	case <-other.C:
		t.Fatal("other-scope subscriber should not receive scoped broadcast")
	case <-time.After(50 * time.Millisecond):
	}

	hub.Unsubscribe(estate)
	hub.Unsubscribe(matching)
	hub.Unsubscribe(other)
}

func TestSlowClientNonBlocking(t *testing.T) {
	hub := NewHub()
	sub := hub.Subscribe("")

	// Fill the buffer.
	ov1 := newTestOverview("test")
	hub.Broadcast(ov1, "")

	// Second broadcast should not block; the message is dropped.
	ov2 := &Overview{Summary: "second"}
	done := make(chan struct{})
	go func() {
		hub.Broadcast(ov2, "")
		close(done)
	}()

	select {
	case <-done:
		// Broadcast returned without blocking.
	case <-time.After(time.Second):
		t.Fatal("broadcast blocked on slow client")
	}

	// Drain the first message.
	got := <-sub.C
	if got != ov1 {
		t.Fatal("expected first overview in buffer")
	}

	hub.Unsubscribe(sub)
}

func TestConcurrentSafety(t *testing.T) {
	hub := NewHub()
	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Concurrent subscribers.
	subs := make(chan *Subscriber, goroutines*iterations)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				s := hub.Subscribe("")
				subs <- s
			}
		}()
	}

	// Concurrent broadcasters.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ov := newTestOverview("test")
			for j := 0; j < iterations; j++ {
				hub.Broadcast(ov, "")
			}
		}()
	}

	// Concurrent unsubscribers.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case s := <-subs:
					hub.Unsubscribe(s)
				default:
				}
			}
		}()
	}

	wg.Wait()

	// Drain remaining subscribers.
	close(subs)
	for s := range subs {
		hub.Unsubscribe(s)
	}

	if hub.Count() != 0 {
		t.Fatalf("expected 0 subscribers after cleanup, got %d", hub.Count())
	}
}

func TestCountAccuracy(t *testing.T) {
	hub := NewHub()

	if hub.Count() != 0 {
		t.Fatalf("expected 0, got %d", hub.Count())
	}

	s1 := hub.Subscribe("")
	s2 := hub.Subscribe("scope-a")
	s3 := hub.Subscribe("scope-b")

	if hub.Count() != 3 {
		t.Fatalf("expected 3, got %d", hub.Count())
	}

	hub.Unsubscribe(s2)
	if hub.Count() != 2 {
		t.Fatalf("expected 2, got %d", hub.Count())
	}

	hub.Unsubscribe(s1)
	hub.Unsubscribe(s3)
	if hub.Count() != 0 {
		t.Fatalf("expected 0, got %d", hub.Count())
	}
}
