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
	"testing"
	"time"
)

// newRateLimiterForTest creates a rate limiter with direct time.Duration parameters
// This avoids data races from modifying fields after the cleanup goroutine starts
func newRateLimiterForTest(windowDuration, cleanupInterval time.Duration, maxAttempts int) *RateLimiter {
	rl := &RateLimiter{
		attempts:        make(map[string][]time.Time),
		windowDuration:  windowDuration,
		maxAttempts:     maxAttempts,
		cleanupInterval: cleanupInterval,
		stopCleanup:     make(chan bool),
	}

	// Start background cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(15, 10)
	if rl == nil {
		t.Fatal("Expected non-nil rate limiter")
	}

	if rl.windowDuration != 15*time.Minute {
		t.Errorf("Expected window duration of 15 minutes, got %v", rl.windowDuration)
	}

	if rl.maxAttempts != 10 {
		t.Errorf("Expected max attempts of 10, got %d", rl.maxAttempts)
	}

	// Clean up
	rl.Stop()
}

func TestRateLimiter_IsAllowed(t *testing.T) {
	rl := NewRateLimiter(1, 3) // 1 minute window, 3 attempts max
	defer rl.Stop()

	ipAddress := "192.168.1.100"

	// First 3 attempts should be allowed
	for i := 0; i < 3; i++ {
		if !rl.IsAllowed(ipAddress) {
			t.Errorf("Attempt %d should be allowed", i+1)
		}
		rl.RecordFailedAttempt(ipAddress)
	}

	// 4th attempt should be blocked
	if rl.IsAllowed(ipAddress) {
		t.Error("4th attempt should be blocked")
	}
}

func TestRateLimiter_MultipleIPs(t *testing.T) {
	rl := NewRateLimiter(1, 2) // 1 minute window, 2 attempts max
	defer rl.Stop()

	ip1 := "192.168.1.100"
	ip2 := "192.168.1.101"

	// Block IP1
	rl.RecordFailedAttempt(ip1)
	rl.RecordFailedAttempt(ip1)

	// IP1 should be blocked
	if rl.IsAllowed(ip1) {
		t.Error("IP1 should be blocked after 2 attempts")
	}

	// IP2 should still be allowed
	if !rl.IsAllowed(ip2) {
		t.Error("IP2 should be allowed (independent from IP1)")
	}
}

func TestRateLimiter_Reset(t *testing.T) {
	rl := NewRateLimiter(1, 3)
	defer rl.Stop()

	ipAddress := "192.168.1.100"

	// Record 3 attempts
	for i := 0; i < 3; i++ {
		rl.RecordFailedAttempt(ipAddress)
	}

	// Should be blocked
	if rl.IsAllowed(ipAddress) {
		t.Error("IP should be blocked after 3 attempts")
	}

	// Reset
	rl.Reset(ipAddress)

	// Should be allowed again
	if !rl.IsAllowed(ipAddress) {
		t.Error("IP should be allowed after reset")
	}
}

func TestRateLimiter_GetRemainingAttempts(t *testing.T) {
	rl := NewRateLimiter(1, 5)
	defer rl.Stop()

	ipAddress := "192.168.1.100"

	// Initially should have 5 remaining
	remaining := rl.GetRemainingAttempts(ipAddress)
	if remaining != 5 {
		t.Errorf("Expected 5 remaining attempts, got %d", remaining)
	}

	// Record 2 attempts
	rl.RecordFailedAttempt(ipAddress)
	rl.RecordFailedAttempt(ipAddress)

	// Should have 3 remaining
	remaining = rl.GetRemainingAttempts(ipAddress)
	if remaining != 3 {
		t.Errorf("Expected 3 remaining attempts, got %d", remaining)
	}

	// Record 3 more attempts
	rl.RecordFailedAttempt(ipAddress)
	rl.RecordFailedAttempt(ipAddress)
	rl.RecordFailedAttempt(ipAddress)

	// Should have 0 remaining
	remaining = rl.GetRemainingAttempts(ipAddress)
	if remaining != 0 {
		t.Errorf("Expected 0 remaining attempts, got %d", remaining)
	}
}

func TestRateLimiter_WindowExpiry(t *testing.T) {
	// Use very short window for testing (100ms)
	rl := newRateLimiterForTest(100*time.Millisecond, time.Minute, 2)
	defer rl.Stop()

	ipAddress := "192.168.1.100"

	// Record 2 attempts
	rl.RecordFailedAttempt(ipAddress)
	rl.RecordFailedAttempt(ipAddress)

	// Should be blocked
	if rl.IsAllowed(ipAddress) {
		t.Error("IP should be blocked after 2 attempts")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again
	if !rl.IsAllowed(ipAddress) {
		t.Error("IP should be allowed after window expiry")
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	// Use very short window and cleanup interval for testing
	rl := newRateLimiterForTest(50*time.Millisecond, 100*time.Millisecond, 2)
	defer rl.Stop()

	ipAddress := "192.168.1.100"

	// Record an attempt
	rl.RecordFailedAttempt(ipAddress)

	// Verify it's in the map
	rl.mu.RLock()
	if len(rl.attempts) == 0 {
		t.Fatal("Expected attempts to be recorded")
	}
	rl.mu.RUnlock()

	// Wait for cleanup to run (window expiry + cleanup interval)
	time.Sleep(200 * time.Millisecond)

	// Verify cleanup removed the old entry
	rl.mu.RLock()
	if len(rl.attempts) != 0 {
		t.Error("Expected old attempts to be cleaned up")
	}
	rl.mu.RUnlock()
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(1, 100)
	defer rl.Stop()

	ipAddress := "192.168.1.100"

	// Test concurrent reads and writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				rl.IsAllowed(ipAddress)
				rl.RecordFailedAttempt(ipAddress)
				rl.GetRemainingAttempts(ipAddress)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to finish
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic or deadlock
}

// TestRateLimiter_CheckAndRecord covers the combined operation: it spends
// allowance while any is left, refuses once none is, and never records
// a refused attempt.
func TestRateLimiter_CheckAndRecord(t *testing.T) {
	rl := NewRateLimiter(1, 3)
	defer rl.Stop()

	for attempt := 1; attempt <= 3; attempt++ {
		if !rl.CheckAndRecord("192.0.2.1") {
			t.Fatalf("attempt %d was refused before the allowance ran out", attempt)
		}
	}
	if rl.CheckAndRecord("192.0.2.1") {
		t.Fatal("a fourth attempt was allowed against a ceiling of three")
	}
	if got := rl.GetRemainingAttempts("192.0.2.1"); got != 0 {
		t.Errorf("remaining attempts = %d, want 0", got)
	}
	if len(rl.attempts["192.0.2.1"]) != 3 {
		t.Errorf("recorded %d attempts, want 3: a refused attempt must not be recorded",
			len(rl.attempts["192.0.2.1"]))
	}

	// Another address has its own allowance.
	if !rl.CheckAndRecord("192.0.2.2") {
		t.Error("a different address was refused by the first one's attempts")
	}
}

// TestRateLimiter_CheckAndRecordDoesNotOvershootConcurrently is the
// reason the operation exists: separate IsAllowed and RecordFailedAttempt
// calls let every request in flight pass the check before any of them
// recorded, so the ceiling could be exceeded by the concurrency. Under
// one lock, exactly maxAttempts of the concurrent calls may succeed.
func TestRateLimiter_CheckAndRecordDoesNotOvershootConcurrently(t *testing.T) {
	const ceiling = 25
	rl := NewRateLimiter(1, ceiling)
	defer rl.Stop()

	results := make(chan bool, 200)
	for range 200 {
		go func() { results <- rl.CheckAndRecord("192.0.2.3") }()
	}
	allowed := 0
	for range 200 {
		if <-results {
			allowed++
		}
	}
	if allowed != ceiling {
		t.Errorf("%d concurrent attempts were allowed, want exactly %d", allowed, ceiling)
	}
}

// TestRateLimiter_CheckAndRecordIgnoresExpiredAttempts confirms the
// window applies: attempts older than the window do not count against
// the ceiling.
func TestRateLimiter_CheckAndRecordIgnoresExpiredAttempts(t *testing.T) {
	rl := newRateLimiterForTest(50*time.Millisecond, time.Hour, 1)
	defer rl.Stop()

	if !rl.CheckAndRecord("192.0.2.4") {
		t.Fatal("the first attempt was refused")
	}
	if rl.CheckAndRecord("192.0.2.4") {
		t.Fatal("a second attempt inside the window was allowed against a ceiling of one")
	}
	time.Sleep(60 * time.Millisecond)
	if !rl.CheckAndRecord("192.0.2.4") {
		t.Error("an attempt after the window expired was refused")
	}
}

// TestRateLimiter_RefundReturnsOneUnit pins that a refund hands back only
// the most recent attempt, not the whole allowance, and that refunding an
// address with nothing recorded is harmless.
func TestRateLimiter_RefundReturnsOneUnit(t *testing.T) {
	rl := NewRateLimiter(1, 3)
	defer rl.Stop()
	const ip = "192.0.2.40"

	rl.Refund(ip)
	if got := rl.GetRemainingAttempts(ip); got != 3 {
		t.Fatalf("refunding an unknown address left %d remaining, want 3", got)
	}

	for range 3 {
		if !rl.CheckAndRecord(ip) {
			t.Fatal("an attempt within the allowance was refused")
		}
	}
	if rl.CheckAndRecord(ip) {
		t.Fatal("an attempt beyond the allowance was accepted")
	}

	rl.Refund(ip)
	if got := rl.GetRemainingAttempts(ip); got != 1 {
		t.Errorf("remaining after one refund = %d, want 1", got)
	}
	if !rl.CheckAndRecord(ip) {
		t.Fatal("the refunded unit could not be spent")
	}
	if rl.CheckAndRecord(ip) {
		t.Fatal("a refund returned more than one unit")
	}

	for range 3 {
		rl.Refund(ip)
	}
	if got := rl.GetRemainingAttempts(ip); got != 3 {
		t.Errorf("remaining after refunding everything = %d, want 3", got)
	}
}
