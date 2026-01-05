/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package auth

import (
	"context"
)

// DatabaseAccessChecker handles database access control based on authentication context
// With single database support, this is simplified to just check authentication
type DatabaseAccessChecker struct {
	tokenStore  *TokenStore
	authEnabled bool
}

// NewDatabaseAccessChecker creates a new database access checker
func NewDatabaseAccessChecker(tokenStore *TokenStore, authEnabled, _ bool) *DatabaseAccessChecker {
	return &DatabaseAccessChecker{
		tokenStore:  tokenStore,
		authEnabled: authEnabled,
	}
}

// CanAccessDatabase checks if the current request context has access to the database
// With single database, we just check that authentication is valid
func (dac *DatabaseAccessChecker) CanAccessDatabase(ctx context.Context) bool {
	// Auth disabled - database accessible
	if !dac.authEnabled {
		return true
	}

	// Check if API token
	if IsAPITokenFromContext(ctx) {
		return true
	}

	// Session user - check if authenticated
	username := GetUsernameFromContext(ctx)
	return username != ""
}

// GetBoundDatabase returns the database name that an API token is bound to
// Returns empty string - with single database, tokens are not bound to specific databases
func (dac *DatabaseAccessChecker) GetBoundDatabase(_ context.Context) string {
	return ""
}
