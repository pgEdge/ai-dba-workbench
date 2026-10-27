/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
)

// Issue #742: http.auth.oidc.allow_api_tokens decides whether accounts
// that sign in through an identity provider may own API tokens.

// linkFederated links username to an identity provider.
func linkFederated(t *testing.T, store *auth.AuthStore, username, subject string) {
	t.Helper()
	if _, err := store.LinkFederatedIdentity(username, "https://idp.example.com",
		subject, false); err != nil {
		t.Fatalf("LinkFederatedIdentity failed: %v", err)
	}
}

func TestCreateUserInfoHandlerReportsAPITokensAllowed(t *testing.T) {
	store := newUserInfoTestStore(t)
	for _, name := range []string{"local", "fed"} {
		if err := store.CreateUser(name, "Testpass1234", "", "", ""); err != nil {
			t.Fatalf("failed to create user: %v", err)
		}
	}
	localSession, _, err := store.CreateSessionForUser("local")
	if err != nil {
		t.Fatalf("CreateSessionForUser failed: %v", err)
	}
	// Linking ends the account's sessions, so its session is minted
	// after, as a federated login mints one.
	linkFederated(t, store, "fed", "subject-1")
	fedSession, _, err := store.CreateSessionForUser("fed")
	if err != nil {
		t.Fatalf("CreateSessionForUser failed: %v", err)
	}

	var allowed atomic.Bool
	store.SetFederatedAPITokenPolicy(allowed.Load)

	tests := []struct {
		name    string
		session string
		allowed bool
		want    bool
	}{
		{"local user, setting off", localSession, false, true},
		{"federated user, setting off", fedSession, false, false},
		{"federated user, setting on", fedSession, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed.Store(tt.allowed)
			body := callUserInfo(t, store, func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: "session_token", Value: tt.session})
			})
			if body["authenticated"] != true {
				t.Fatalf("authenticated = %v, want true", body["authenticated"])
			}
			if body["api_tokens_allowed"] != tt.want {
				t.Errorf("api_tokens_allowed = %v, want %v", body["api_tokens_allowed"], tt.want)
			}
		})
	}
}

func TestLoadCLITokenPolicy(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("Failed to write the configuration file: %v", err)
		}
		return path
	}
	t.Cleanup(func() { cliTokenPolicy.allowFederatedAPITokens = false })

	tests := []struct {
		name    string
		path    string
		want    bool
		wantErr bool
	}{
		{"no configuration file", filepath.Join(dir, "absent.yaml"), false, false},
		{"setting absent", write("absent-setting.yaml", "http:\n  auth:\n    oidc:\n      enabled: true\n"), false, false},
		{"setting on", write("on.yaml", "http:\n  auth:\n    oidc:\n      allow_api_tokens: true\n"), true, false},
		{"setting off", write("off.yaml", "http:\n  auth:\n    oidc:\n      allow_api_tokens: false\n"), false, false},
		{"malformed", write("broken.yaml", "http: [\n"), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cliTokenPolicy.allowFederatedAPITokens = !tt.want
			err := loadCLITokenPolicy(tt.path)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), tt.path) {
					t.Fatalf("Expected an error naming the file, got %v", err)
				}
				if cliTokenPolicy.allowFederatedAPITokens {
					t.Error("A failed read left the setting on")
				}
				return
			}
			if err != nil {
				t.Fatalf("loadCLITokenPolicy failed: %v", err)
			}
			if cliTokenPolicy.allowFederatedAPITokens != tt.want {
				t.Errorf("allowFederatedAPITokens = %v, want %v",
					cliTokenPolicy.allowFederatedAPITokens, tt.want)
			}
		})
	}
}

func TestAddTokenCommandForFederatedOwner(t *testing.T) {
	t.Cleanup(func() { cliTokenPolicy.allowFederatedAPITokens = false })

	dataDir, _ := newTokenCommandStore(t)
	withTokenStore(t, dataDir, func(store *auth.AuthStore) {
		linkFederated(t, store, "owner", "subject-1")
	})

	cliTokenPolicy.allowFederatedAPITokens = false
	err := addTokenCommand(dataDir, "owner", "refused", time.Hour)
	if !errors.Is(err, auth.ErrFederatedAPITokensDisabled) {
		t.Fatalf("Expected the federated refusal, got %v", err)
	}
	if !strings.Contains(err.Error(), "allow_api_tokens") {
		t.Errorf("Expected the error to name the setting, got %v", err)
	}
	withTokenStore(t, dataDir, func(store *auth.AuthStore) {
		events, _, listErr := store.ListAuditEvents(auth.AuditFilter{
			Outcome: string(auth.OutcomeDenied), Action: "token.create"})
		if listErr != nil {
			t.Fatalf("ListAuditEvents failed: %v", listErr)
		}
		if len(events) != 1 || events[0].Error != auth.FederatedAPITokenRefusalReason {
			t.Fatalf("Expected one audited refusal, got %+v", events)
		}
	})

	// A local service account is unaffected.
	captureStdout(t, func() {
		err = addTokenCommand(dataDir, "svc", "automation", time.Hour)
	})
	if err != nil {
		t.Fatalf("addTokenCommand for a service account failed: %v", err)
	}

	cliTokenPolicy.allowFederatedAPITokens = true
	captureStdout(t, func() {
		err = addTokenCommand(dataDir, "owner", "allowed", time.Hour)
	})
	if err != nil {
		t.Fatalf("addTokenCommand with the setting on failed: %v", err)
	}
	var found bool
	for _, token := range tokensOwnedBy(t, dataDir, "owner") {
		found = found || token.Annotation == "allowed"
	}
	if !found {
		t.Fatal("Expected the token to be created with the setting on")
	}
}

// Whilst allow_api_tokens is off, linking an account says that its tokens
// are refused rather than that they keep working.
func TestLinkOIDCUserCommandReportsRefusedTokens(t *testing.T) {
	cliTokenPolicy.allowFederatedAPITokens = false
	dataDir := seedLinkTestUser(t, "alice")

	var err error
	output := captureStdout(t, func() {
		err = linkOIDCUserCommand(dataDir, "alice", cliTestIssuer, cliTestSubject, false)
	})
	if err != nil {
		t.Fatalf("linkOIDCUserCommand: %v", err)
	}
	if !strings.Contains(output, refusedTokenCaveat) {
		t.Errorf("output %q does not say the tokens are refused", output)
	}
	if strings.Contains(output, tokenCaveat) {
		t.Errorf("output %q says the tokens keep working", output)
	}
}

// SetupHandlers registers a policy that reads the live configuration,
// so a reload switching the setting takes effect without a restart.
func TestSetupHandlersWiresTheFederatedTokenPolicy(t *testing.T) {
	store := newUserInfoTestStore(t)
	var live atomic.Pointer[config.Config]
	live.Store(&config.Config{})

	deps := &HandlerDependencies{
		Config:     &config.Config{},
		AuthStore:  store,
		LiveConfig: live.Load,
	}
	if err := SetupHandlers(deps)(http.NewServeMux()); err != nil {
		t.Fatalf("SetupHandlers: %v", err)
	}
	if store.FederatedAPITokensAllowed() {
		t.Fatal("federated API tokens allowed by default")
	}

	on := &config.Config{}
	on.HTTP.Auth.OIDC.AllowAPITokens = config.BoolPtr(true)
	live.Store(on)
	if !store.FederatedAPITokensAllowed() {
		t.Fatal("the reloaded setting was not seen")
	}
}
