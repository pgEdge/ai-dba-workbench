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
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
)

// newTokenPolicyStore returns an audited store holding a local user
// (local), a local service account (robot) and a federated user (fed),
// and a switch for allow_api_tokens, initially off.
func newTokenPolicyStore(t *testing.T) (*AuthStore, *atomic.Bool) {
	t.Helper()

	store, cleanup := createTestAuthStoreForAudit(t)
	t.Cleanup(cleanup)

	for _, name := range []string{"local", "fed"} {
		if err := store.CreateUser(name, linkTestPassword, "", "", ""); err != nil {
			t.Fatalf("CreateUser %s: %v", name, err)
		}
	}
	if err := store.CreateServiceAccount("robot", "", "", ""); err != nil {
		t.Fatalf("CreateServiceAccount: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("fed", linkTestIssuer, linkTestSubject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}

	allowed := &atomic.Bool{}
	store.SetFederatedAPITokenPolicy(allowed.Load)
	return store, allowed
}

func TestCreateTokenFederatedPolicy(t *testing.T) {
	tests := []struct {
		owner   string
		allowed bool
		wantErr bool
	}{
		{"local", false, false},
		{"local", true, false},
		{"robot", false, false},
		{"robot", true, false},
		{"fed", false, true},
		{"fed", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.owner+"/allowed="+strconv.FormatBool(tt.allowed), func(t *testing.T) {
			store, allowed := newTokenPolicyStore(t)
			allowed.Store(tt.allowed)

			raw, _, err := store.AsActor(systemActor).CreateToken(tt.owner, "note", nil, true)
			if tt.wantErr {
				if !errors.Is(err, ErrFederatedAPITokensDisabled) {
					t.Fatalf("CreateToken error = %v, want ErrFederatedAPITokensDisabled", err)
				}
				// The refusal is the handler's to audit, as a denial; the
				// store writes no failure row of its own.
				if ev := lastAuditEvent(t, store); ev.Action == "token.create" {
					t.Fatalf("the store audited the refusal itself: %+v", ev)
				}
				tokens, listErr := store.ListUserTokens(tt.owner)
				if listErr != nil || len(tokens) != 0 {
					t.Fatalf("tokens after refusal = %d, %v", len(tokens), listErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateToken: %v", err)
			}
			if _, err := store.ValidateToken(raw); err != nil {
				t.Fatalf("ValidateToken: %v", err)
			}
		})
	}
}

// A token a federated user owns works only while the setting is on, and
// the setting is read on every use, so a reload takes effect at once.
func TestValidateTokenFederatedOwnerFollowsPolicy(t *testing.T) {
	store, allowed := newTokenPolicyStore(t)

	allowed.Store(true)
	raw, stored, err := store.AsActor(systemActor).CreateToken("fed", "note", nil, true)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if _, err := store.ValidateToken(raw); err != nil {
		t.Fatalf("ValidateToken with the setting on: %v", err)
	}

	allowed.Store(false)
	token, err := store.ValidateToken(raw)
	if !errors.Is(err, ErrFederatedAPITokensDisabled) || token != nil {
		t.Fatalf("ValidateToken with the setting off = %v, %v", token, err)
	}
	ev := lastAuditEvent(t, store)
	if ev.Action != tokenUseAction || ev.Outcome != OutcomeDenied {
		t.Fatalf("audit row = %s/%s, want %s/denied", ev.Action, ev.Outcome, tokenUseAction)
	}
	if ev.ActorType != ActorToken || ev.ActorID == nil || *ev.ActorID != stored.ID || ev.ActorName != "fed" {
		t.Fatalf("audit actor = %s/%v/%s", ev.ActorType, ev.ActorID, ev.ActorName)
	}
	if ev.Error != FederatedAPITokenRefusalReason {
		t.Fatalf("audit reason = %q", ev.Error)
	}
	if target := auditDetails(t, ev)["target"]; target != "tokens/"+strconv.FormatInt(stored.ID, 10) {
		t.Fatalf("audit target = %v", target)
	}

	allowed.Store(true)
	if _, err := store.ValidateToken(raw); err != nil {
		t.Fatalf("ValidateToken after switching the setting back on: %v", err)
	}
}

// A token created while the account was local is refused once the
// account is linked to an identity provider.
func TestValidateTokenRefusesTokenFromBeforeLinking(t *testing.T) {
	store, _ := newTokenPolicyStore(t)

	raw, _, err := store.AsActor(systemActor).CreateToken("local", "note", nil, true)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if _, err := store.LinkFederatedIdentity("local", linkTestIssuer, "subject-0002", false); err != nil {
		t.Fatalf("LinkFederatedIdentity: %v", err)
	}
	if _, err := store.ValidateToken(raw); !errors.Is(err, ErrFederatedAPITokensDisabled) {
		t.Fatalf("ValidateToken error = %v, want ErrFederatedAPITokensDisabled", err)
	}
}

// A refusal that cannot be audited is still a refusal.
func TestValidateTokenRefusalWhenAuditFails(t *testing.T) {
	store, allowed := newTokenPolicyStore(t)

	allowed.Store(true)
	raw, _, err := store.AsActor(systemActor).CreateToken("fed", "note", nil, true)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	allowed.Store(false)
	mustExec(t, store, "DROP TRIGGER audit_events_no_update")
	mustExec(t, store, "DROP TABLE audit_events")

	if _, err := store.ValidateToken(raw); !errors.Is(err, ErrFederatedAPITokensDisabled) {
		t.Fatalf("ValidateToken error = %v, want ErrFederatedAPITokensDisabled", err)
	}
}

func TestFederatedAPITokenPolicyDefaultsOff(t *testing.T) {
	store, cleanup := createTestAuthStoreForStore(t)
	defer cleanup()

	if store.FederatedAPITokensAllowed() {
		t.Fatal("federated API tokens allowed with no policy registered")
	}
	store.SetFederatedAPITokenPolicy(func() bool { return true })
	if !store.FederatedAPITokensAllowed() {
		t.Fatal("the registered policy was not consulted")
	}
	store.SetFederatedAPITokenPolicy(nil)
	if store.FederatedAPITokensAllowed() {
		t.Fatal("removing the policy did not restore the default")
	}
	if !store.APITokensAllowedFor(AuthSourceLocal) || store.APITokensAllowedFor(AuthSourceOIDC) {
		t.Fatal("APITokensAllowedFor disagrees with the default")
	}
}

func TestUserMayOwnAPITokens(t *testing.T) {
	store, allowed := newTokenPolicyStore(t)

	tests := []struct {
		user    string
		allowed bool
		want    bool
	}{
		{"local", false, true},
		{"robot", false, true},
		{"fed", false, false},
		{"fed", true, true},
		{"nobody", true, false},
	}
	for _, tt := range tests {
		allowed.Store(tt.allowed)
		got, err := store.UserMayOwnAPITokens(tt.user)
		if err != nil {
			t.Fatalf("%s: %v", tt.user, err)
		}
		if got != tt.want {
			t.Errorf("UserMayOwnAPITokens(%s) with allowed=%v = %v, want %v",
				tt.user, tt.allowed, got, tt.want)
		}
	}

	store.Close()
	if _, err := store.UserMayOwnAPITokens("local"); err == nil {
		t.Fatal("no error from a closed store")
	}
}
