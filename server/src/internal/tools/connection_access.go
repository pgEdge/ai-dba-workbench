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
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// connectionNotAccessibleMsg is the wording every connection-scoped tool
// uses for a connection that is missing or that the caller may not access.
const connectionNotAccessibleMsg = "connection not found or not accessible"

// maxConnectionSuggestions caps how many visible connections the
// "not found or not accessible" message lists.
const maxConnectionSuggestions = 20

// resolveAccessibleConnection checks that the caller may access
// connectionID and only then looks up the connection's name.
//
// Access is checked before the lookup, and a denied check, a missing
// connection and a failed lookup all produce the same response, so the
// tool cannot be used to learn whether a connection the caller cannot see
// exists. CanAccessConnection folds its own lookup errors into a denial,
// so an access check that fails denies. The suggestions appended to the
// response are drawn from the caller's visible connections only; they do
// not depend on connectionID, so they cannot distinguish the cases either.
//
// A nil pool skips the name lookup and the suggestions, returning an
// empty name once access is granted. A non-nil response means the caller
// should return it immediately.
func resolveAccessibleConnection(
	ctx context.Context,
	pool *pgxpool.Pool,
	rbacChecker *auth.RBACChecker,
	visibilityLister auth.ConnectionVisibilityLister,
	connectionID int,
) (string, *mcp.ToolResponse) {
	canAccess, _ := rbacChecker.CanAccessConnection(ctx, connectionID)
	if canAccess {
		if pool == nil {
			return "", nil
		}
		var name string
		err := pool.QueryRow(ctx, "SELECT name FROM connections WHERE id = $1", connectionID).Scan(&name)
		if err == nil {
			return name, nil
		}
	}

	resp := connectionNotAccessibleResponse(ctx, pool, rbacChecker, visibilityLister)
	return "", &resp
}

// connectionNotAccessibleResponse builds the single error returned for a
// connection that is missing or that the caller may not access, listing
// up to maxConnectionSuggestions of the connections the caller can see.
// It does not echo the requested ID, so the text is the same whichever
// ID was asked for.
func connectionNotAccessibleResponse(
	ctx context.Context,
	pool *pgxpool.Pool,
	rbacChecker *auth.RBACChecker,
	visibilityLister auth.ConnectionVisibilityLister,
) mcp.ToolResponse {
	msg := connectionNotAccessibleMsg + "."
	suggestions := visibleConnectionSuggestions(ctx, pool, rbacChecker, visibilityLister)
	if len(suggestions) > 0 {
		msg += fmt.Sprintf(" Connections you can access include: %s. "+
			"Use list_connections to see all of them.", strings.Join(suggestions, ", "))
	} else {
		msg += " Use list_connections to see the connections you can access."
	}
	return mcp.ToolResponse{
		Content: []mcp.ContentItem{{Type: "text", Text: msg}},
		IsError: true,
	}
}

// visibleConnectionSuggestions returns "id (name)" entries for up to
// maxConnectionSuggestions connections the caller can see, ordered by ID.
// Any failure, and a caller who can see nothing, yields no suggestions.
func visibleConnectionSuggestions(
	ctx context.Context,
	pool *pgxpool.Pool,
	rbacChecker *auth.RBACChecker,
	visibilityLister auth.ConnectionVisibilityLister,
) []string {
	if pool == nil {
		return nil
	}
	ids, all, err := rbacChecker.VisibleConnectionIDs(ctx, visibilityLister)
	if err != nil || (!all && len(ids) == 0) {
		return nil
	}

	query := "SELECT id, name FROM connections ORDER BY id LIMIT $1"
	args := []any{maxConnectionSuggestions}
	if !all {
		query = "SELECT id, name FROM connections WHERE id = ANY($2) ORDER BY id LIMIT $1"
		args = append(args, ids)
	}
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil
	}
	// CollectRows closes rows.
	suggestions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (string, error) {
		var id int
		var name string
		err := row.Scan(&id, &name)
		return fmt.Sprintf("%d (%s)", id, name), err
	})
	if err != nil {
		return nil
	}
	return suggestions
}
