/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package ratelimit provides a keyed token-bucket limiter for capping
// how often a caller may trigger an expensive operation, such as an
// LLM provider call. Each key (typically a username) gets its own
// bucket, which holds up to burst tokens and earns one token every
// interval. Buckets that have refilled completely are discarded, since
// a full bucket behaves exactly like a new one, so memory stays
// proportional to the number of recently active keys.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// sweepInterval is how often Allow scans for full buckets to discard.
const sweepInterval = time.Minute

// bucket is the token count for one key as of last.
type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter is a set of token buckets keyed by string. It is safe for
// concurrent use. A nil *Limiter allows everything, so a disabled
// limit can be represented without special-casing the callers.
type Limiter struct {
	mu        sync.Mutex
	interval  time.Duration
	burst     float64
	buckets   map[string]*bucket
	lastSweep time.Time
	now       func() time.Time
}

// New returns a limiter whose buckets hold up to burst tokens and earn
// one token every interval. It returns nil, which allows everything,
// when interval or burst is not positive.
func New(interval time.Duration, burst int) *Limiter {
	if interval <= 0 || burst <= 0 {
		return nil
	}
	return &Limiter{
		interval: interval,
		burst:    float64(burst),
		buckets:  make(map[string]*bucket),
		now:      time.Now,
	}
}

// PerHour returns a limiter allowing a sustained count operations per
// hour per key, with bursts of up to burst. A count that is not
// positive disables the limit and returns nil.
func PerHour(count, burst int) *Limiter {
	if count <= 0 {
		return nil
	}
	return New(time.Hour/time.Duration(count), burst)
}

// Allow spends one token from key's bucket if one is available. When
// none is, it reports false together with how long the caller must
// wait before a token will be available, rounded up to a whole second
// so it can be sent as a Retry-After header.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = l.refilled(b, now)
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	wait := time.Duration((1 - b.tokens) * float64(l.interval))
	return false, time.Duration(math.Ceil(wait.Seconds())) * time.Second
}

// refilled returns b's token count at now, capped at the burst size.
func (l *Limiter) refilled(b *bucket, now time.Time) float64 {
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return b.tokens
	}
	return math.Min(l.burst, b.tokens+float64(elapsed)/float64(l.interval))
}

// sweepLocked discards buckets that have refilled completely, at most
// once per sweepInterval. The caller must hold l.mu.
func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	for key, b := range l.buckets {
		if l.refilled(b, now) >= l.burst {
			delete(l.buckets, key)
		}
	}
}
