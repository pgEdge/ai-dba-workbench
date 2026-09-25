/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package oidc

import (
	"crypto/sha256"
	"sync"
	"time"
)

// usedStateRetention is how long UsedStates remembers a state after it
// was first presented. A sealed state is accepted by OpenState until
// StateTTL after its IssuedAt, and IssuedAt may be up to clockSkew ahead
// of this server's clock, so a state presented now can still be opened
// until at most now + StateTTL + clockSkew. Remembering it for that long
// is therefore enough to refuse every later presentation of it, and
// measuring from the moment of insertion (rather than from IssuedAt)
// keeps the expiry times in insertion order, which is what lets expired
// entries be dropped from the front of a queue.
const usedStateRetention = StateTTL + clockSkew

// defaultUsedStatesCapacity bounds how many used states are remembered
// at once. Each entry costs about 120 bytes of heap (a SHA-256 digest
// held in a map and a queue, plus its expiry time), so a full set holds
// roughly 12 MB. A state is added only when the callback accepts one, and
// minting one needs a rate-limited call to the start endpoint, so the
// set comes near this size only under a sustained flood from many
// clients.
const defaultUsedStatesCapacity = 100_000

// usedState is one queued entry: the digest of a presented state and the
// time after which it no longer needs remembering.
type usedState struct {
	digest  [sha256.Size]byte
	expires time.Time
}

// UsedStates records which login states the callback has already
// accepted, so that each sealed state cookie can be presented
// successfully once and never again. The sealed cookie is stateless and
// stays valid for StateTTL, so without this record one cookie could be
// replayed against the callback with as many codes as its holder cared
// to send, each of them driving a request to the identity provider's
// token endpoint.
//
// Only a SHA-256 digest of the state value is kept, never the value
// itself. Memory is bounded: entries expire after usedStateRetention and
// are dropped as later calls arrive, and once the set holds its capacity
// the oldest entry is evicted to make room. Eviction degrades gracefully
// rather than failing closed: the only states ever held are ones that
// have already been used, so an evicted state could at worst be replayed
// once more, at the cost of a unit of the callback's rate limit, whereas
// refusing new logins would let a flood deny federated login to
// everybody.
//
// The record lives in this process's memory. A deployment that runs
// several server processes sharing one server secret, and so able to
// open each other's state cookies, would need a shared record to refuse
// a replay presented to a different process.
//
// A UsedStates is safe for concurrent use.
type UsedStates struct {
	mu       sync.Mutex
	digests  map[[sha256.Size]byte]struct{}
	queue    []usedState
	capacity int
	now      func() time.Time
}

// NewUsedStates returns an empty record with the default capacity.
func NewUsedStates() *UsedStates {
	return newUsedStates(defaultUsedStatesCapacity, time.Now)
}

// newUsedStates is the constructor the tests use to choose a small
// capacity and control the clock. A capacity below one is raised to one.
func newUsedStates(capacity int, now func() time.Time) *UsedStates {
	if capacity < 1 {
		capacity = 1
	}
	return &UsedStates{
		digests:  make(map[[sha256.Size]byte]struct{}),
		capacity: capacity,
		now:      now,
	}
}

// MarkUsed records state as used and reports whether this is the first
// time it has been presented. A false result means the state was seen
// before within usedStateRetention and the caller must refuse it.
//
// The check and the record are one operation under one lock, so of two
// concurrent presentations of the same state exactly one is accepted.
func (u *UsedStates) MarkUsed(state string) bool {
	digest := sha256.Sum256([]byte(state))

	u.mu.Lock()
	defer u.mu.Unlock()

	now := u.now()
	u.dropExpired(now)

	if _, seen := u.digests[digest]; seen {
		return false
	}

	for len(u.queue) >= u.capacity {
		u.dropOldest()
	}

	u.digests[digest] = struct{}{}
	u.queue = append(u.queue, usedState{digest: digest, expires: now.Add(usedStateRetention)})
	return true
}

// Len returns the number of states currently remembered.
func (u *UsedStates) Len() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.queue)
}

// dropExpired removes every entry whose expiry has passed. Expiry times
// are in insertion order, so they are all at the front of the queue.
// The caller holds u.mu.
func (u *UsedStates) dropExpired(now time.Time) {
	for len(u.queue) > 0 && now.After(u.queue[0].expires) {
		u.dropOldest()
	}
}

// dropOldest removes the entry at the front of the queue. Reslicing
// leaves the dropped element in the backing array until the next append
// reallocates it, so the element is zeroed first. Because append
// reallocates to a small multiple of the live length, the backing array
// stays within a small multiple of capacity. The caller holds u.mu and ensures the queue is
// not empty.
func (u *UsedStates) dropOldest() {
	delete(u.digests, u.queue[0].digest)
	u.queue[0] = usedState{}
	u.queue = u.queue[1:]
}
