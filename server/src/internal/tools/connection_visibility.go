/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// datastoreVisibilityLister adapts *database.Datastore.GetAllConnections to
// the auth.ConnectionVisibilityLister interface. A single call loads sharing
// metadata for every connection; this lets auth.RBACChecker.VisibleConnectionIDs
// compute the visible set without issuing one query per connection.
//
// The adapter lives here (rather than being imported from the api package)
// to avoid a circular dependency: the api package already imports tools in
// some wire-ups, and the auth package must not import database.
type datastoreVisibilityLister struct {
	ds *database.Datastore
}

// newDatastoreVisibilityLister returns a lister that wraps the given
// datastore. A nil datastore yields a nil lister so callers can pass the
// result directly to VisibleConnectionIDs, which tolerates a nil lister by
// falling back to the group/token-granted IDs only.
func newDatastoreVisibilityLister(ds *database.Datastore) auth.ConnectionVisibilityLister {
	if ds == nil {
		return nil
	}
	return &datastoreVisibilityLister{ds: ds}
}

// GetAllConnections implements auth.ConnectionVisibilityLister by projecting
// database.ConnectionListItem into the minimal struct the auth package needs.
func (l *datastoreVisibilityLister) GetAllConnections(ctx context.Context) ([]auth.ConnectionVisibilityInfo, error) {
	conns, err := l.ds.GetAllConnections(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]auth.ConnectionVisibilityInfo, 0, len(conns))
	for i := range conns {
		result = append(result, auth.ConnectionVisibilityInfo{
			ID:            conns[i].ID,
			IsShared:      conns[i].IsShared,
			OwnerUsername: conns[i].OwnerUsername,
		})
	}
	return result, nil
}
