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
	"fmt"
	"os"
)

// ConnectionSession holds session data for resolving a database client.
// This mirrors auth.ConnectionSession but is defined in the database package
// to avoid a circular import from database -> auth.
type ConnectionSession struct {
	ConnectionID int
	DatabaseName *string
}

// SessionProvider retrieves and clears connection sessions by token hash.
// Implementations typically wrap auth.AuthStore to provide session data
// without requiring the database package to import auth.
type SessionProvider interface {
	// GetConnectionSession returns the connection session for the given token hash.
	// Returns (nil, nil) if no session exists.
	GetConnectionSession(tokenHash string) (*ConnectionSession, error)

	// ClearConnectionSession removes the connection session for the given token hash.
	ClearConnectionSession(tokenHash string) error
}

// AccessChecker decides whether a token may use a connection.
// Implementations typically wrap auth.RBACChecker.
type AccessChecker interface {
	// CanAccessConnection returns whether the context can access the connection
	// and, if so, the access level ("read" or "read_write").
	CanAccessConnection(ctx context.Context, connectionID int) (bool, string)
}

// ConnectionInfoProvider retrieves connection details and builds connection strings.
// Implementations typically wrap database.Datastore.
type ConnectionInfoProvider interface {
	// GetConnectionWithPassword returns the connection info and decrypted password.
	GetConnectionWithPassword(ctx context.Context, connectionID int) (*MonitoredConnection, string, error)

	// BuildConnectionString constructs a connection string from the connection info.
	BuildConnectionString(conn *MonitoredConnection, password string, databaseOverride string) string
}

// ClientResolver resolves the appropriate database Client for a request.
// It extracts the token hash from the context, looks up any active session,
// verifies RBAC access, and returns or creates the appropriate client.
type ClientResolver struct {
	// TokenExtractor extracts the token hash from the request context.
	// Typically auth.GetTokenHashFromContext.
	TokenExtractor func(context.Context) string

	// Sessions provides access to connection session data.
	// May be nil if session-based connections are not available.
	Sessions SessionProvider

	// Access checks RBAC permissions for connections.
	// May be nil if RBAC is not enabled.
	Access AccessChecker

	// ConnInfo provides connection details and builds connection strings.
	// May be nil if session-based connections are not available.
	ConnInfo ConnectionInfoProvider

	// ClientManager manages the pool of database clients.
	ClientManager *ClientManager
}

// ResolveClient returns the appropriate database client for the request context.
// The resolution order is:
//  1. Extract token hash from context (required)
//  2. If Sessions and ConnInfo are available, look up the active session
//  3. If a session exists, verify RBAC access, get connection info, and return client
//  4. If no session but Sessions/ConnInfo are configured, return "no connection selected" error
//  5. Fallback: use ClientManager.GetOrCreateClient for token-based default connection
func (r *ClientResolver) ResolveClient(ctx context.Context) (*Client, error) {
	tokenHash := r.TokenExtractor(ctx)
	if tokenHash == "" {
		return nil, fmt.Errorf("no authentication token found in request context")
	}

	// Check if session-based connection resolution is available
	if r.Sessions != nil && r.ConnInfo != nil {
		session, err := r.Sessions.GetConnectionSession(tokenHash)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to get connection session: %v\n", err)
		}

		if session != nil {
			// Verify RBAC access if checker is available
			if r.Access != nil {
				canAccess, _ := r.Access.CanAccessConnection(ctx, session.ConnectionID)
				if !canAccess {
					// Clear the stale session so subsequent calls get
					// a clean "no connection selected" error.
					//nolint:errcheck // Best effort cleanup; we return the access denied error regardless
					r.Sessions.ClearConnectionSession(tokenHash)
					return nil, fmt.Errorf("access denied: the selected connection is no longer accessible with this token's scope. Please select a permitted connection")
				}
			}

			// Get connection info from provider
			conn, password, err := r.ConnInfo.GetConnectionWithPassword(ctx, session.ConnectionID)
			if err != nil {
				return nil, fmt.Errorf("failed to get connection info: %w", err)
			}

			// Build connection string with optional database override
			var databaseOverride string
			if session.DatabaseName != nil {
				databaseOverride = *session.DatabaseName
			}
			connStr := r.ConnInfo.BuildConnectionString(conn, password, databaseOverride)

			// Get or create client using the session helper
			if r.ClientManager == nil {
				return nil, fmt.Errorf("no client manager configured")
			}
			sessionInfo := &SessionInfo{
				TokenHash:    tokenHash,
				ConnectionID: session.ConnectionID,
				DatabaseName: session.DatabaseName,
			}
			client, err := r.ClientManager.GetClientForSession(sessionInfo, connStr)
			if err != nil {
				return nil, fmt.Errorf("failed to connect to selected database: %w", err)
			}

			return client, nil
		}

		// No connection selected - return helpful error
		return nil, fmt.Errorf("no database connection selected. Please select a database connection using your client interface (CLI or web client)")
	}

	// Fallback: Get or create client for this token using default config
	if r.ClientManager == nil {
		return nil, fmt.Errorf("no client manager configured")
	}
	client, err := r.ClientManager.GetOrCreateClient(tokenHash, true)
	if err != nil {
		return nil, fmt.Errorf("no database connection configured for this token: %w", err)
	}

	return client, nil
}
