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

import "time"

// evictionLinks holds the neighbors of one entry on an evictionList.
// A coalescer's entry state embeds it, so that the list is intrusive and
// needs no allocation or type assertion of its own (container/list's
// element type assertions are what errcheck flags).
type evictionLinks[T any] struct {
	prev, next *T
}

// evictionEntry is the constraint an entry state meets to sit on an
// evictionList: a pointer to it gives access to its links and to the
// time its current window opened, by which the list is ordered.
type evictionEntry[T any] interface {
	*T
	links() *evictionLinks[T]
	windowStart() time.Time
}

// evictionList orders a coalescer's entries by the start of their
// window, oldest first, so that eviction and sweeps walk from the oldest
// end and stop at the first entry they keep, doing work proportional to
// the entries they drop rather than scanning the whole map. Its zero
// value is an empty list. The coalescer that owns it guards it with its
// own lock.
type evictionList[T any, P evictionEntry[T]] struct {
	oldest, newest *T
}

// pushNewest appends entry to the newest end of the list.
func (l *evictionList[T, P]) pushNewest(entry *T) {
	links := P(entry).links()
	links.prev = l.newest
	links.next = nil
	if l.newest != nil {
		P(l.newest).links().next = entry
	} else {
		l.oldest = entry
	}
	l.newest = entry
}

// insertBefore links entry into the list just ahead of next, or at the
// newest end when next is nil.
func (l *evictionList[T, P]) insertBefore(entry, next *T) {
	if next == nil {
		l.pushNewest(entry)
		return
	}
	links, nextLinks := P(entry).links(), P(next).links()
	links.next = next
	links.prev = nextLinks.prev
	if nextLinks.prev != nil {
		P(nextLinks.prev).links().next = entry
	} else {
		l.oldest = entry
	}
	nextLinks.prev = entry
}

// insertOrdered links entry into the list in its place by window start,
// after every entry whose window opened no later than its own. It walks
// from the oldest end, so it suits an entry put back rarely, such as a
// window reopened after a failed write.
func (l *evictionList[T, P]) insertOrdered(entry *T) {
	start := P(entry).windowStart()
	next := l.oldest
	for next != nil && !P(next).windowStart().After(start) {
		next = P(next).links().next
	}
	l.insertBefore(entry, next)
}

// unlink removes entry from the list.
func (l *evictionList[T, P]) unlink(entry *T) {
	links := P(entry).links()
	if links.prev != nil {
		P(links.prev).links().next = links.next
	} else {
		l.oldest = links.next
	}
	if links.next != nil {
		P(links.next).links().prev = links.prev
	} else {
		l.newest = links.prev
	}
	links.prev, links.next = nil, nil
}
