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
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// ConnectionResolver resolves a connection_id and optional database_name
// from tool arguments into a usable database client and pool. When
// connection_id is absent, the resolver falls back to the captured
// per-token client.
type ConnectionResolver struct {
	clientManager *database.ClientManager
	datastore     *database.Datastore
	rbacChecker   *auth.RBACChecker
}

// ResolvedConnection holds the resolved database client, pool, and
// metadata needed by tool handlers.
type ResolvedConnection struct {
	Client  *database.Client
	Pool    *pgxpool.Pool
	ConnStr string
	ConnID  int
	DBName  string
}

// NewConnectionResolver creates a ConnectionResolver. All parameters
// are required for explicit connection_id resolution; pass nil only in
// tests that do not exercise connection resolution.
func NewConnectionResolver(
	clientManager *database.ClientManager,
	datastore *database.Datastore,
	rbacChecker *auth.RBACChecker,
) *ConnectionResolver {
	return &ConnectionResolver{
		clientManager: clientManager,
		datastore:     datastore,
		rbacChecker:   rbacChecker,
	}
}

// connectionArgs holds the parsed connection parameters from tool args.
type connectionArgs struct {
	ConnectionID int
	HasConnID    bool
	DatabaseName string
}

// parseConnectionArgs extracts connection_id and database_name from the
// tool argument map. connection_id arrives as float64 from JSON
// unmarshalling.
func parseConnectionArgs(args map[string]any) connectionArgs {
	var ca connectionArgs

	if raw, ok := args["connection_id"]; ok {
		switch v := raw.(type) {
		case float64:
			ca.ConnectionID = int(v)
			ca.HasConnID = true
		case int:
			ca.ConnectionID = v
			ca.HasConnID = true
		}
	}

	if dbName, ok := args["database_name"].(string); ok && dbName != "" {
		ca.DatabaseName = dbName
	}

	return ca
}

// Resolve determines the database client and pool to use for a tool
// invocation. When connection_id is present in args, the resolver
// looks up the connection, checks RBAC, and returns a dedicated client.
// When connection_id is absent, the resolver falls back to
// fallbackClient. If the resolver itself is nil and connection_id is
// absent, the fallback client is used directly. If the resolver is nil
// and connection_id is present, an error is returned.
func (r *ConnectionResolver) Resolve(
	ctx context.Context,
	args map[string]any,
	fallbackClient *database.Client,
) (*ResolvedConnection, *mcp.ToolResponse) {
	ca := parseConnectionArgs(args)

	// When connection_id is provided, resolve explicitly.
	if ca.HasConnID {
		if r == nil {
			resp := mcp.ToolResponse{
				Content: []mcp.ContentItem{{
					Type: "text",
					Text: "connection_id was specified but connection resolution is not available",
				}},
				IsError: true,
			}
			return nil, &resp
		}
		return r.resolveExplicit(ctx, ca)
	}

	// No connection_id: fall back to the captured per-token client.
	return resolveFallback(fallbackClient)
}

// resolveExplicit handles the case where connection_id is provided.
func (r *ConnectionResolver) resolveExplicit(
	ctx context.Context,
	ca connectionArgs,
) (*ResolvedConnection, *mcp.ToolResponse) {
	// RBAC: verify access before touching credentials. Using a generic
	// "not found or not accessible" message for both missing and denied
	// cases prevents the caller from using the resolver as a probe to
	// enumerate connection IDs they cannot see. CanAccessConnection
	// folds lookup errors into a denied decision; log the deny with the
	// connection ID so operators can correlate without widening the
	// surface the caller sees. Fail-closed: we keep the deny even if
	// the underlying cause was a transient lookup error.
	canAccess, _ := r.rbacChecker.CanAccessConnection(ctx, ca.ConnectionID)
	if !canAccess {
		fmt.Fprintf(os.Stderr, "WARNING: connection_resolver: RBAC denied access to connection %d\n", ca.ConnectionID)
		resp := mcp.ToolResponse{
			Content: []mcp.ContentItem{{
				Type: "text",
				Text: "connection not found or not accessible",
			}},
			IsError: true,
		}
		return nil, &resp
	}

	// Look up connection credentials.
	conn, password, err := r.datastore.GetConnectionWithPassword(ctx, ca.ConnectionID)
	if err != nil {
		resp := mcp.ToolResponse{
			Content: []mcp.ContentItem{{
				Type: "text",
				Text: "connection not found or not accessible",
			}},
			IsError: true,
		}
		return nil, &resp
	}

	// Build connection string, optionally overriding the database.
	connStr := r.datastore.BuildConnectionString(conn, password, ca.DatabaseName)

	// Determine the effective database name for the session key.
	effectiveDB := conn.DatabaseName
	if ca.DatabaseName != "" {
		effectiveDB = ca.DatabaseName
	}

	// Build session info for client caching.
	tokenHash := auth.GetTokenHashFromContext(ctx)
	var dbNamePtr *string
	if ca.DatabaseName != "" {
		dbNamePtr = &ca.DatabaseName
	}
	sessionInfo := &database.SessionInfo{
		TokenHash:    tokenHash,
		ConnectionID: ca.ConnectionID,
		DatabaseName: dbNamePtr,
	}

	client, err := r.clientManager.GetClientForSession(sessionInfo, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: connection_resolver: failed to connect to connection %d: %v\n", ca.ConnectionID, err)
		resp := mcp.ToolResponse{
			Content: []mcp.ContentItem{{
				Type: "text",
				Text: "Failed to establish database connection",
			}},
			IsError: true,
		}
		return nil, &resp
	}

	// Ensure metadata is loaded for this connection string.
	if !client.IsMetadataLoadedFor(connStr) {
		_ = client.LoadMetadataFor(connStr) //nolint:errcheck // metadata loading failure is non-fatal
	}

	pool := client.GetPoolFor(connStr)
	if pool == nil {
		fmt.Fprintf(os.Stderr, "ERROR: connection_resolver: pool not available for connection %d\n", ca.ConnectionID)
		resp := mcp.ToolResponse{
			Content: []mcp.ContentItem{{
				Type: "text",
				Text: "Failed to establish database connection",
			}},
			IsError: true,
		}
		return nil, &resp
	}

	return &ResolvedConnection{
		Client:  client,
		Pool:    pool,
		ConnStr: connStr,
		ConnID:  ca.ConnectionID,
		DBName:  effectiveDB,
	}, nil
}

// resolveFallback uses the captured per-token client when no
// connection_id was provided.
func resolveFallback(
	fallbackClient *database.Client,
) (*ResolvedConnection, *mcp.ToolResponse) {
	if fallbackClient == nil {
		resp := mcp.ToolResponse{
			Content: []mcp.ContentItem{{
				Type: "text",
				Text: "No database connection available. Specify connection_id or select a connection first.",
			}},
			IsError: true,
		}
		return nil, &resp
	}

	connStr := fallbackClient.GetDefaultConnection()
	if connStr == "" {
		resp := mcp.ToolResponse{
			Content: []mcp.ContentItem{{
				Type: "text",
				Text: "No default connection configured. Specify connection_id to target a database.",
			}},
			IsError: true,
		}
		return nil, &resp
	}

	pool := fallbackClient.GetPoolFor(connStr)
	if pool == nil {
		resp := mcp.ToolResponse{
			Content: []mcp.ContentItem{{
				Type: "text",
				Text: fmt.Sprintf("Connection pool not found for: %s", database.SanitizeConnStr(connStr)),
			}},
			IsError: true,
		}
		return nil, &resp
	}

	return &ResolvedConnection{
		Client:  fallbackClient,
		Pool:    pool,
		ConnStr: connStr,
	}, nil
}
