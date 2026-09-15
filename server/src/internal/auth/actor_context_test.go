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

import (
	"context"
	"testing"
)

func TestSystemActor(t *testing.T) {
	got := SystemActor()
	if got.Type != ActorSystem || got.Name != "system" {
		t.Errorf("expected the system actor, got %+v", got)
	}
	if got.ID != nil {
		t.Errorf("expected a nil actor id, got %v", *got.ID)
	}
}

func TestActorFromContext(t *testing.T) {
	tokenCtx := context.WithValue(context.Background(),
		UsernameContextKey, "svc")
	tokenCtx = context.WithValue(tokenCtx, IsAPITokenContextKey, true)
	tokenCtx = context.WithValue(tokenCtx, AuditTokenIDContextKey, int64(42))
	tokenCtx = context.WithValue(tokenCtx, UserIDContextKey, int64(7))
	tokenCtx = context.WithValue(tokenCtx, IPAddressContextKey, "192.0.2.5")

	userCtx := context.WithValue(context.Background(),
		UsernameContextKey, "alice")
	userCtx = context.WithValue(userCtx, IsAPITokenContextKey, false)
	userCtx = context.WithValue(userCtx, UserIDContextKey, int64(3))
	userCtx = context.WithValue(userCtx, IPAddressContextKey, "198.51.100.9")

	noIDCtx := context.WithValue(context.Background(),
		UsernameContextKey, "bob")

	// The MCP middleware sets TokenIDContextKey rather than the
	// audit-only key, so ActorFromContext must still attribute it.
	mcpTokenCtx := context.WithValue(context.Background(),
		UsernameContextKey, "mcpsvc")
	mcpTokenCtx = context.WithValue(mcpTokenCtx, IsAPITokenContextKey, true)
	mcpTokenCtx = context.WithValue(mcpTokenCtx, TokenIDContextKey, int64(11))

	// A token whose owning user could not be read: the id alone is
	// enough to trace the change, so this is not the system actor.
	namelessTokenCtx := context.WithValue(context.Background(),
		IsAPITokenContextKey, true)
	namelessTokenCtx = context.WithValue(namelessTokenCtx,
		AuditTokenIDContextKey, int64(77))

	// An API-token context with neither a name nor an id identifies
	// nothing, so it degrades to the system actor.
	emptyTokenCtx := context.WithValue(context.Background(),
		IsAPITokenContextKey, true)

	tests := []struct {
		name     string
		ctx      context.Context
		wantType ActorType
		wantName string
		wantID   *int64
		wantIP   string
	}{
		{
			name:     "api token actor",
			ctx:      tokenCtx,
			wantType: ActorToken,
			wantName: "svc",
			wantID:   int64Ptr(42),
			wantIP:   "192.0.2.5",
		},
		{
			name:     "session user actor",
			ctx:      userCtx,
			wantType: ActorUser,
			wantName: "alice",
			wantID:   int64Ptr(3),
			wantIP:   "198.51.100.9",
		},
		{
			name:     "user without a resolved id",
			ctx:      noIDCtx,
			wantType: ActorUser,
			wantName: "bob",
			wantID:   nil,
			wantIP:   "",
		},
		{
			name:     "mcp token actor via the scoping key",
			ctx:      mcpTokenCtx,
			wantType: ActorToken,
			wantName: "mcpsvc",
			wantID:   int64Ptr(11),
			wantIP:   "",
		},
		{
			name:     "token whose owner could not be resolved",
			ctx:      namelessTokenCtx,
			wantType: ActorToken,
			wantName: "",
			wantID:   int64Ptr(77),
			wantIP:   "",
		},
		{
			name:     "api token with neither name nor id",
			ctx:      emptyTokenCtx,
			wantType: ActorSystem,
			wantName: "system",
			wantID:   nil,
			wantIP:   "",
		},
		{
			name:     "empty context falls back to the system actor",
			ctx:      context.Background(),
			wantType: ActorSystem,
			wantName: "system",
			wantID:   nil,
			wantIP:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ActorFromContext(tt.ctx)
			if got.Type != tt.wantType {
				t.Errorf("type: got %q, want %q", got.Type, tt.wantType)
			}
			if got.Name != tt.wantName {
				t.Errorf("name: got %q, want %q", got.Name, tt.wantName)
			}
			if got.IP != tt.wantIP {
				t.Errorf("ip: got %q, want %q", got.IP, tt.wantIP)
			}
			switch {
			case tt.wantID == nil && got.ID != nil:
				t.Errorf("id: got %d, want nil", *got.ID)
			case tt.wantID != nil && got.ID == nil:
				t.Errorf("id: got nil, want %d", *tt.wantID)
			case tt.wantID != nil && *got.ID != *tt.wantID:
				t.Errorf("id: got %d, want %d", *got.ID, *tt.wantID)
			}
		})
	}
}

func TestActorFromContextNilContext(t *testing.T) {
	//nolint:staticcheck // SA1012: a nil context is exactly what this
	// guard exists to survive.
	got := ActorFromContext(nil)
	if got.Type != ActorSystem {
		t.Errorf("type: got %q, want %q", got.Type, ActorSystem)
	}
}
