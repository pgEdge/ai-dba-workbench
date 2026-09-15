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

import "context"

// SystemActor returns the actor that attributes an audited change to
// the server itself. It is the fallback for a request context that
// carries no authenticated identity, so that an event is still recorded
// with degraded attribution rather than lost.
func SystemActor() Actor {
	return systemActor
}

// ActorFromContext builds the audit Actor for an authenticated request
// from the values that AuthenticateRequest and the REST auth wrapper
// place in its context.
//
// A request authenticated with an API token yields an ActorToken whose
// ID is the token id and whose Name is the owning username; any other
// authenticated request yields an ActorUser whose ID is the user id.
// The ID is left nil when neither the middleware nor the store resolved
// it, and the IP, when present, comes from IPAddressContextKey.
//
// A context that names neither a user nor a token has no identity to
// attribute the change to, so the system actor is returned; a token
// whose owning user could not be read still yields a token actor,
// identified by its id with an empty name, because the token id alone
// is enough to trace the change.
func ActorFromContext(ctx context.Context) Actor {
	if ctx == nil {
		return systemActor
	}

	actor := Actor{Name: GetUsernameFromContext(ctx)}
	if ip, ok := ctx.Value(IPAddressContextKey).(string); ok {
		actor.IP = ip
	}

	if IsAPITokenFromContext(ctx) {
		tokenID := actingTokenID(ctx)
		if tokenID == 0 && actor.Name == "" {
			return systemActor
		}
		actor.Type = ActorToken
		if tokenID != 0 {
			id := tokenID
			actor.ID = &id
		}
		return actor
	}

	if actor.Name == "" {
		return systemActor
	}

	actor.Type = ActorUser
	if userID := GetUserIDFromContext(ctx); userID != 0 {
		id := userID
		actor.ID = &id
	}

	return actor
}

// actingTokenID returns the id of the token that made the request. It
// prefers AuditTokenIDContextKey, which the REST middleware sets purely
// for attribution, and falls back to TokenIDContextKey, which the MCP
// middleware sets to drive token-scope enforcement. Reading both means
// every authenticated path attributes its changes, whilst the REST path
// keeps its historical, unscoped authorisation.
func actingTokenID(ctx context.Context) int64 {
	if id := GetAuditTokenIDFromContext(ctx); id != 0 {
		return id
	}
	return GetTokenIDFromContext(ctx)
}
