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
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// recordedDenial is one call a fakeDenialRecorder received.
type recordedDenial struct {
	actor  auth.Actor
	action string
	target string
	reason string
}

// fakeDenialRecorder records the denials it is given and returns err
// from each call.
type fakeDenialRecorder struct {
	calls []recordedDenial
	err   error
}

func (f *fakeDenialRecorder) RecordCoalescedDenial(actor auth.Actor,
	action, target, reason string) error {

	f.calls = append(f.calls, recordedDenial{actor, action, target, reason})
	return f.err
}

// storeMemoryArgs builds store_memory arguments for testuser calling
// from 192.0.2.1 with the given scope.
func storeMemoryArgs(scope string) map[string]any {
	ctx := context.WithValue(context.Background(),
		auth.UsernameContextKey, "testuser")
	ctx = context.WithValue(ctx, auth.IPAddressContextKey, "192.0.2.1")
	return map[string]any{
		"content":   "A system-wide memory.",
		"category":  "fact",
		"scope":     scope,
		"__context": ctx,
	}
}

// TestStoreMemorySystemScopeRefusalIsAudited checks that refusing a
// system-scoped store records a memory.create denial attributed to the
// caller, and that a failure to record it leaves the refusal unchanged
// (issue #674).
func TestStoreMemorySystemScopeRefusalIsAudited(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "recorded"},
		{name: "recording fails", err: errors.New("audit store unavailable")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &fakeDenialRecorder{err: tt.err}
			tool := StoreMemoryTool(nil, &config.Config{}, nil, recorder)

			response, err := tool.Handler(storeMemoryArgs("system"))
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			if !response.IsError || len(response.Content) == 0 ||
				!strings.Contains(response.Content[0].Text,
					"Permission denied") {
				t.Fatalf("expected a permission-denied error, got: %+v",
					response)
			}

			if len(recorder.calls) != 1 {
				t.Fatalf("expected one recorded denial, got %d",
					len(recorder.calls))
			}
			got := recorder.calls[0]
			want := recordedDenial{
				actor: auth.Actor{Type: auth.ActorUser, Name: "testuser",
					IP: "192.0.2.1"},
				action: "memory.create",
				target: "memories",
				reason: "Permission denied: requires store_system_memory permission",
			}
			if got.actor.Type != want.actor.Type ||
				got.actor.Name != want.actor.Name ||
				got.actor.IP != want.actor.IP ||
				got.action != want.action || got.target != want.target ||
				got.reason != want.reason {
				t.Errorf("recorded %+v, want %+v", got, want)
			}
		})
	}
}

// TestStoreMemoryRefusalAttributesToken checks that a refusal made with
// an API token is attributed to the token's id, as the REST refusals
// are.
func TestStoreMemoryRefusalAttributesToken(t *testing.T) {
	recorder := &fakeDenialRecorder{}
	tool := StoreMemoryTool(nil, &config.Config{}, nil, recorder)

	args := storeMemoryArgs("system")
	ctx := args["__context"].(context.Context)
	ctx = context.WithValue(ctx, auth.IsAPITokenContextKey, true)
	args["__context"] = context.WithValue(ctx, auth.TokenIDContextKey,
		int64(17))

	if _, err := tool.Handler(args); err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("expected one recorded denial, got %d",
			len(recorder.calls))
	}
	actor := recorder.calls[0].actor
	if actor.Type != auth.ActorToken || actor.ID == nil ||
		*actor.ID != 17 || actor.Name != "testuser" {
		t.Errorf("denial attributed to %+v, want token 17 of testuser",
			actor)
	}
}

// TestStoreMemoryUserScopeIsNotAudited checks that a user-scoped store,
// which needs no permission, records no denial.
func TestStoreMemoryUserScopeIsNotAudited(t *testing.T) {
	recorder := &fakeDenialRecorder{}
	tool := StoreMemoryTool(nil, &config.Config{}, nil, recorder)

	response, err := tool.Handler(storeMemoryArgs("user"))
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError || len(response.Content) == 0 ||
		!strings.Contains(response.Content[0].Text, "not configured") {
		t.Fatalf("expected the nil-store error, got: %+v", response)
	}
	if len(recorder.calls) != 0 {
		t.Errorf("expected no recorded denial, got %+v", recorder.calls)
	}
}

// TestDenialRecorderFor checks that a nil auth store yields a nil
// recorder rather than a non-nil interface holding a nil pointer.
func TestDenialRecorderFor(t *testing.T) {
	if got := denialRecorderFor(nil); got != nil {
		t.Errorf("denialRecorderFor(nil) = %v, want nil", got)
	}

	authStore, err := auth.NewAuthStore(t.TempDir(), 0, 0,
		auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()

	if got := denialRecorderFor(authStore); got != authStore {
		t.Errorf("denialRecorderFor(store) = %v, want the store", got)
	}
}

// TestStoreMemoryRefusalReachesAuditLog checks, against a real auth
// store, that a refused system-scoped store writes a denied memory.create
// row naming the caller, its address and the memories target.
func TestStoreMemoryRefusalReachesAuditLog(t *testing.T) {
	authStore, err := auth.NewAuthStore(t.TempDir(), 0, 0,
		auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()

	tool := StoreMemoryTool(nil, &config.Config{}, nil,
		denialRecorderFor(authStore))
	if _, err := tool.Handler(storeMemoryArgs("system")); err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}

	events, _, err := authStore.ListAuditEvents(auth.AuditFilter{
		Action:  "memory.create",
		Outcome: string(auth.OutcomeDenied),
	})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one denied memory.create event, got %d",
			len(events))
	}

	ev := events[0]
	if ev.ActorType != auth.ActorUser || ev.ActorName != "testuser" ||
		ev.ActorIP != "192.0.2.1" {
		t.Errorf("event attributed to %s %q from %q, want user testuser "+
			"from 192.0.2.1", ev.ActorType, ev.ActorName, ev.ActorIP)
	}
	if !strings.Contains(ev.Error, auth.PermStoreSystemMemory) {
		t.Errorf("event error %q does not name the permission", ev.Error)
	}

	var details map[string]any
	if err := json.Unmarshal(ev.Details, &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if details["target"] != "memories" {
		t.Errorf("details.target = %v, want memories", details["target"])
	}
}
