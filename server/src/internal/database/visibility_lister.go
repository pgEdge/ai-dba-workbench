/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// connectionLister is an internal interface that *Datastore satisfies. Using
// an interface allows unit testing with a mock implementation.
type connectionLister interface {
	GetAllConnections(ctx context.Context) ([]ConnectionListItem, error)
}

// ConnectionsToVisibilityInfo projects a slice of ConnectionListItem into
// the minimal auth.ConnectionVisibilityInfo values that the auth package
// uses for visibility resolution. Both the datastore-backed and slice-
// backed visibility listers share this helper to guarantee a single,
// canonical mapping.
func ConnectionsToVisibilityInfo(conns []ConnectionListItem) []auth.ConnectionVisibilityInfo {
	result := make([]auth.ConnectionVisibilityInfo, 0, len(conns))
	for i := range conns {
		result = append(result, auth.ConnectionVisibilityInfo{
			ID:            conns[i].ID,
			IsShared:      conns[i].IsShared,
			OwnerUsername: conns[i].OwnerUsername,
		})
	}
	return result
}

// visibilityLister adapts *Datastore.GetAllConnections to the
// auth.ConnectionVisibilityLister interface. A single call loads sharing
// metadata for every connection; this lets auth.RBACChecker.VisibleConnectionIDs
// compute the visible set without issuing one query per connection.
type visibilityLister struct {
	ds connectionLister
}

// NewVisibilityLister returns a lister that wraps the given datastore. A nil
// datastore yields a nil lister so callers can pass the result directly to
// VisibleConnectionIDs, which tolerates a nil lister by falling back to the
// group/token-granted IDs only.
func NewVisibilityLister(ds *Datastore) auth.ConnectionVisibilityLister {
	if ds == nil {
		return nil
	}
	return &visibilityLister{ds: ds}
}

// newVisibilityListerWithSource creates a visibilityLister with a custom
// connectionLister. This is unexported and intended for testing only.
func newVisibilityListerWithSource(src connectionLister) *visibilityLister {
	return &visibilityLister{ds: src}
}

// GetAllConnections implements auth.ConnectionVisibilityLister by projecting
// ConnectionListItem into the minimal struct the auth package needs.
func (l *visibilityLister) GetAllConnections(ctx context.Context) ([]auth.ConnectionVisibilityInfo, error) {
	conns, err := l.ds.GetAllConnections(ctx)
	if err != nil {
		return nil, err
	}
	return ConnectionsToVisibilityInfo(conns), nil
}

// sliceVisibilityLister adapts an already-loaded slice of ConnectionListItem
// to auth.ConnectionVisibilityLister. Callers that have the full list in
// hand can reuse it instead of issuing a second datastore read.
type sliceVisibilityLister struct {
	connections []ConnectionListItem
}

// NewSliceVisibilityLister returns a visibility lister backed by the
// supplied slice. The caller retains ownership of the slice; the lister
// does not modify it. The returned lister never errors because no I/O
// is performed; the context argument is accepted only to satisfy the
// auth.ConnectionVisibilityLister interface.
func NewSliceVisibilityLister(conns []ConnectionListItem) auth.ConnectionVisibilityLister {
	return sliceVisibilityLister{connections: conns}
}

// GetAllConnections implements auth.ConnectionVisibilityLister over the
// pre-loaded slice.
func (l sliceVisibilityLister) GetAllConnections(_ context.Context) ([]auth.ConnectionVisibilityInfo, error) {
	return ConnectionsToVisibilityInfo(l.connections), nil
}
