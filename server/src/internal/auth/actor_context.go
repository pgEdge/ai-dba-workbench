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
// The ID is left nil when the store could not resolve it, and the IP,
// when present, comes from IPAddressContextKey. A context with no
// username has no identity to attribute the change to, so the system
// actor is returned.
func ActorFromContext(ctx context.Context) Actor {
	if ctx == nil {
		return systemActor
	}

	username := GetUsernameFromContext(ctx)
	if username == "" {
		return systemActor
	}

	actor := Actor{Name: username}
	if ip, ok := ctx.Value(IPAddressContextKey).(string); ok {
		actor.IP = ip
	}

	if IsAPITokenFromContext(ctx) {
		actor.Type = ActorToken
		if tokenID := GetTokenIDFromContext(ctx); tokenID != 0 {
			id := tokenID
			actor.ID = &id
		}
		return actor
	}

	actor.Type = ActorUser
	if userID := GetUserIDFromContext(ctx); userID != 0 {
		id := userID
		actor.ID = &id
	}

	return actor
}
