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
	tokenCtx = context.WithValue(tokenCtx, TokenIDContextKey, int64(42))
	tokenCtx = context.WithValue(tokenCtx, UserIDContextKey, int64(7))
	tokenCtx = context.WithValue(tokenCtx, IPAddressContextKey, "192.0.2.5")

	userCtx := context.WithValue(context.Background(),
		UsernameContextKey, "alice")
	userCtx = context.WithValue(userCtx, IsAPITokenContextKey, false)
	userCtx = context.WithValue(userCtx, UserIDContextKey, int64(3))
	userCtx = context.WithValue(userCtx, IPAddressContextKey, "198.51.100.9")

	noIDCtx := context.WithValue(context.Background(),
		UsernameContextKey, "bob")

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
