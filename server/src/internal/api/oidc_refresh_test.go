/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/oidc/oidctest"
)

// loginWithRefreshToken runs a federated login during which the provider
// issues refreshToken, and returns the session cookie's value.
func (e *oidcTestEnv) loginWithRefreshToken(t *testing.T, refreshToken string,
	claims map[string]any) *http.Cookie {

	t.Helper()

	e.idp.IssueRefreshToken(refreshToken)
	rec := e.login(t, "/", claims)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") == loginFailedTarget {
		t.Fatalf("login failed: %s", describeResponse(rec))
	}
	session := findCookie(rec, SessionCookieName)
	if session == nil || session.Value == "" {
		t.Fatal("the login set no session cookie")
	}
	return session
}

// shortLivedIDTokenLifetime is how long the ID token from
// loginUntilExpired lasts. It has to outlast the login itself, which can
// take a few seconds on a loaded host, and the exp claim's whole-second
// resolution.
const shortLivedIDTokenLifetime = 6 * time.Second

// loginUntilExpired logs in with an ID token that expires shortly, and
// returns the session cookie once that expiry has passed.
func (e *oidcTestEnv) loginUntilExpired(t *testing.T, refreshToken string) *http.Cookie {
	t.Helper()

	exp := time.Now().Add(shortLivedIDTokenLifetime).Unix()
	session := e.loginWithRefreshToken(t, refreshToken, map[string]any{"exp": exp})
	time.Sleep(time.Until(time.Unix(exp, 0)) + 100*time.Millisecond)
	return session
}

// A session that can be refreshed lives as long as the provider keeps
// agreeing to refresh it, so its cookie lasts for the browser session
// rather than carrying an expiry the server will extend.
func TestCallbackWithARefreshTokenSetsABrowserSessionCookie(t *testing.T) {
	env := newTestOIDCEnv(t)

	session := env.loginWithRefreshToken(t, "rt-1", nil)
	if !session.Expires.IsZero() {
		t.Fatalf("cookie Expires = %v, want none", session.Expires)
	}
}

// The whole path: a federated session reaches its ID token's expiry, the
// next request redeems the refresh token, and the session carries on.
func TestFederatedSessionIsRefreshedThroughTheProvider(t *testing.T) {
	env := newTestOIDCEnv(t)
	env.store.SetFederatedSessionRefresher(env.handler)

	session := env.loginUntilExpired(t, "rt-1")
	env.idp.SetRefreshResponse(oidctest.RefreshResponse{
		IDToken:             env.idp.MintIDToken(t, map[string]any{"email": testUsername}),
		RotatedRefreshToken: "rt-2",
	})

	username, err := env.store.ValidateSessionToken(session.Value)
	if err != nil || username != testUsername {
		t.Fatalf("ValidateSessionToken = %q, %v", username, err)
	}
	if got := env.idp.RefreshCount(); got != 1 {
		t.Fatalf("refresh grants = %d, want 1", got)
	}
	if got := env.idp.LastTokenRequestForm().Get("refresh_token"); got != "rt-1" {
		t.Fatalf("refresh_token presented = %q, want rt-1", got)
	}
	// The refreshed session is good for the new ID token's hour.
	if _, err := env.store.ValidateSessionToken(session.Value); err != nil {
		t.Fatalf("ValidateSessionToken after the refresh: %v", err)
	}
	if got := env.idp.RefreshCount(); got != 1 {
		t.Fatalf("an unexpired session was refreshed again (%d grants)", got)
	}
}

// The provider refusing the refresh, as it does once the user has been
// removed there, ends the session.
func TestFederatedSessionEndsWhenTheProviderRefuses(t *testing.T) {
	env := newTestOIDCEnv(t)
	env.store.SetFederatedSessionRefresher(env.handler)

	session := env.loginUntilExpired(t, "rt-1")
	env.idp.RevokeRefreshTokens()

	if _, err := env.store.ValidateSessionToken(session.Value); err == nil {
		t.Fatal("a session the provider refused to refresh was accepted")
	}
	if _, err := env.store.ValidateSessionToken(session.Value); err == nil {
		t.Fatal("the ended session came back")
	}
	if got := env.idp.RefreshCount(); got != 1 {
		t.Fatalf("refresh grants = %d, want 1", got)
	}
}

func TestRefreshFederatedSessionReconcilesGroups(t *testing.T) {
	env := newTestOIDCEnv(t, func(cfg *config.OIDCConfig) {
		cfg.GroupMap = map[string]string{"idp-eng": "engineers", "idp-ops": "operators"}
	})
	engineers, err := env.store.CreateGroup("engineers", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	operators, err := env.store.CreateGroup("operators", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	env.loginWithRefreshToken(t, "rt-1", map[string]any{"groups": []any{"idp-eng"}})

	newExpiry := time.Now().Add(45 * time.Minute).Truncate(time.Second)
	env.idp.SetRefreshResponse(oidctest.RefreshResponse{
		IDToken: env.idp.MintIDToken(t, map[string]any{
			"email": testUsername, "groups": []any{"idp-ops"}, "exp": newExpiry.Unix(),
		}),
		RotatedRefreshToken: "rt-2",
	})

	refreshed, err := env.handler.RefreshFederatedSession(t.Context(), testUsername, "rt-1")
	if err != nil {
		t.Fatalf("RefreshFederatedSession: %v", err)
	}
	if refreshed.RefreshToken != "rt-2" {
		t.Errorf("refresh token = %q, want the rotated one", refreshed.RefreshToken)
	}
	if !refreshed.ExpiresAt.Equal(newExpiry) {
		t.Errorf("expiry = %v, want the new ID token's %v", refreshed.ExpiresAt, newExpiry)
	}

	userID, err := env.store.GetUserID(testUsername)
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	groups, err := env.store.GetUserGroups(userID)
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if slices.Contains(groups, engineers) || !slices.Contains(groups, operators) {
		t.Fatalf("groups after the refresh = %v, want only operators (%d)", groups, operators)
	}
}

// A refresh that returns no ID token extends the session to the access
// token's expiry, and reconciles nothing.
func TestRefreshFederatedSessionWithoutAnIDToken(t *testing.T) {
	env := newTestOIDCEnv(t)
	env.loginWithRefreshToken(t, "rt-1", nil)
	env.idp.SetRefreshResponse(oidctest.RefreshResponse{ExpiresIn: 600})

	refreshed, err := env.handler.RefreshFederatedSession(t.Context(), testUsername, "rt-1")
	if err != nil {
		t.Fatalf("RefreshFederatedSession: %v", err)
	}
	if until := time.Until(refreshed.ExpiresAt); until < 9*time.Minute || until > 11*time.Minute {
		t.Fatalf("expiry in %v, want about ten minutes", until)
	}
	if refreshed.RefreshToken != "rt-1" {
		t.Fatalf("refresh token = %q, want the unrotated one", refreshed.RefreshToken)
	}
}

func TestRefreshFederatedSessionRefusals(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.OIDCConfig)
		prepare func(t *testing.T, env *oidcTestEnv)
		want    string
	}{
		{
			name: "provider refuses",
			prepare: func(_ *testing.T, env *oidcTestEnv) {
				env.idp.RevokeRefreshTokens()
			},
			want: "failed to redeem refresh token",
		},
		{
			name: "different subject",
			prepare: func(t *testing.T, env *oidcTestEnv) {
				env.idp.SetRefreshResponse(oidctest.RefreshResponse{
					IDToken: env.idp.MintIDToken(t, map[string]any{
						"sub": "subject-2", "email": testUsername}),
				})
			},
			want: "not the one the account is linked to",
		},
		{
			name: "email domain no longer allowed",
			mutate: func(cfg *config.OIDCConfig) {
				cfg.AllowedEmailDomains = []string{"example.com"}
			},
			prepare: func(t *testing.T, env *oidcTestEnv) {
				env.idp.SetRefreshResponse(oidctest.RefreshResponse{
					IDToken: env.idp.MintIDToken(t, map[string]any{
						"email": "jane@other.example.net", "email_verified": true}),
				})
			},
			want: "not in an allowed domain",
		},
		{
			name: "federated login switched off",
			prepare: func(_ *testing.T, env *oidcTestEnv) {
				cfg := env.handler.cfg
				cfg.Enabled = config.BoolPtr(false)
				env.handler.SetConfigSource(func() config.OIDCConfig { return cfg })
			},
			want: "switched off",
		},
		{
			name: "account disabled",
			prepare: func(t *testing.T, env *oidcTestEnv) {
				if err := env.store.DisableUser(testUsername); err != nil {
					t.Fatalf("disabling the account: %v", err)
				}
			},
			want: "disabled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mutators []func(*config.OIDCConfig)
			if tt.mutate != nil {
				mutators = append(mutators, tt.mutate)
			}
			env := newTestOIDCEnv(t, mutators...)
			env.loginWithRefreshToken(t, "rt-1", map[string]any{"email_verified": true})
			tt.prepare(t, env)

			_, err := env.handler.RefreshFederatedSession(t.Context(), testUsername, "rt-1")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one mentioning %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "rt-1") {
				t.Fatal("the refresh token appeared in the error")
			}
		})
	}
}

// The account is checked before the provider is asked, so a session
// whose account has gone, or is no longer federated, ends without a
// call to the provider.
func TestRefreshFederatedSessionChecksTheAccountFirst(t *testing.T) {
	federated := func(mutate func(*auth.StoredUser)) *auth.StoredUser {
		user := &auth.StoredUser{ID: 7, Username: testUsername, Enabled: true,
			AuthSource: auth.AuthSourceOIDC}
		if mutate != nil {
			mutate(user)
		}
		return user
	}
	tests := []struct {
		name  string
		store *stubFederationStore
		want  string
	}{
		{"unreadable", &stubFederationStore{getUserErr: errors.New("disk I/O error")}, "could not be read"},
		{"missing", &stubFederationStore{}, "no longer exists"},
		{"disabled", &stubFederationStore{user: federated(func(u *auth.StoredUser) { u.Enabled = false })}, "disabled"},
		{"local", &stubFederationStore{user: federated(func(u *auth.StoredUser) { u.AuthSource = auth.AuthSourceLocal })}, "no longer federated"},
		{"service account", &stubFederationStore{user: federated(func(u *auth.StoredUser) { u.IsServiceAccount = true })}, "no longer federated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newStubbedOIDCEnv(t, tt.store)
			env.idp.IssueRefreshToken("rt-1")

			_, err := env.handler.RefreshFederatedSession(t.Context(), testUsername, "rt-1")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one mentioning %q", err, tt.want)
			}
			if got := env.idp.RefreshCount(); got != 0 {
				t.Fatalf("the provider was asked %d time(s)", got)
			}
		})
	}
}

func TestRefreshFederatedSessionFailsWhenReconciliationFails(t *testing.T) {
	store := &stubFederationStore{reconcileErr: errors.New("disk full")}
	env := newStubbedOIDCEnv(t, store)
	store.user = &auth.StoredUser{ID: 7, Username: testUsername, Enabled: true,
		AuthSource:      auth.AuthSourceOIDC,
		ExternalSubject: auth.ExternalSubjectKey(env.idp.Issuer(), "subject-1")}
	env.idp.IssueRefreshToken("rt-1")
	env.idp.SetRefreshResponse(oidctest.RefreshResponse{
		IDToken: env.idp.MintIDToken(t, map[string]any{"email": testUsername}),
	})

	_, err := env.handler.RefreshFederatedSession(t.Context(), testUsername, "rt-1")
	if err == nil || !strings.Contains(err.Error(), "group reconciliation failed") {
		t.Fatalf("error = %v, want the reconciliation failure", err)
	}
	if store.reconcileCalls != 1 {
		t.Fatalf("reconciliation ran %d times, want once", store.reconcileCalls)
	}
}
