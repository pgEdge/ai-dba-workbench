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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// newTokenCommandStore returns a data directory holding the account
// "owner", a superuser "root" and a service account "svc", together with
// a token "owner" holds.
func newTokenCommandStore(t *testing.T) (string, *auth.StoredToken) {
	t.Helper()

	dataDir := t.TempDir()
	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		t.Fatalf("openAuthStoreCLI failed: %v", err)
	}
	defer store.Close()

	for _, name := range []string{"owner", "root"} {
		if err := store.CreateUser(name, "Password1234", "", "", ""); err != nil {
			t.Fatalf("CreateUser %s failed: %v", name, err)
		}
	}
	if err := store.SetUserSuperuser("root", true); err != nil {
		t.Fatalf("SetUserSuperuser failed: %v", err)
	}
	if err := store.CreateServiceAccount("svc", "", "", ""); err != nil {
		t.Fatalf("CreateServiceAccount failed: %v", err)
	}
	if _, err := store.RegisterMCPPrivilege("tool_a",
		auth.MCPPrivilegeTypeTool, "Tool A", false); err != nil {
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
	_, token, err := store.AsActor(auth.SystemActor()).CreateToken("owner",
		"seed", nil, true)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	return dataDir, token
}

// withTokenStore opens the store in dataDir for fn and closes it after.
func withTokenStore(t *testing.T, dataDir string, fn func(*auth.AuthStore)) {
	t.Helper()

	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		t.Fatalf("openAuthStoreCLI failed: %v", err)
	}
	defer store.Close()
	fn(store)
}

// tokensOwnedBy returns the tokens the named account owns.
func tokensOwnedBy(t *testing.T, dataDir, username string) []*auth.StoredToken {
	t.Helper()

	var tokens []*auth.StoredToken
	withTokenStore(t, dataDir, func(store *auth.AuthStore) {
		var err error
		tokens, err = store.ListUserTokens(username)
		if err != nil {
			t.Fatalf("ListUserTokens failed: %v", err)
		}
	})
	return tokens
}

// unopenableDataDir returns a data directory path that the auth store
// cannot open, because it names a regular file.
func unopenableDataDir(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	return path
}

func TestAddTokenCommand(t *testing.T) {
	t.Run("from flags", func(t *testing.T) {
		dataDir, _ := newTokenCommandStore(t)
		var err error
		out := captureStdout(t, func() {
			err = addTokenCommand(dataDir, "root", "deploy key", 48*time.Hour)
		})
		if err != nil {
			t.Fatalf("addTokenCommand failed: %v", err)
		}
		for _, want := range []string{"Token created successfully!",
			"Owner: root", "Note:  deploy key", "Expires: "} {
			if !strings.Contains(out, want) {
				t.Errorf("Expected output to contain %q, got %q", want, out)
			}
		}
		tokens := tokensOwnedBy(t, dataDir, "root")
		if len(tokens) != 1 || tokens[0].ExpiresAt == nil ||
			tokens[0].Annotation != "deploy key" {
			t.Errorf("Expected one expiring token for root, got %+v", tokens)
		}
	})

	t.Run("interactive with an expiry", func(t *testing.T) {
		dataDir, _ := newTokenCommandStore(t)
		var err error
		withStdin(t, "owner\nprompted\n30d\n", func() {
			captureStdout(t, func() {
				err = addTokenCommand(dataDir, "", "", 0)
			})
		})
		if err != nil {
			t.Fatalf("addTokenCommand failed: %v", err)
		}
		tokens := tokensOwnedBy(t, dataDir, "owner")
		if len(tokens) != 2 {
			t.Fatalf("Expected owner to hold two tokens, got %d", len(tokens))
		}
		var found bool
		for _, tok := range tokens {
			if tok.Annotation == "prompted" {
				found = true
				if tok.ExpiresAt == nil {
					t.Errorf("Expected the prompted token to expire")
				}
			}
		}
		if !found {
			t.Errorf("Expected a token annotated 'prompted', got %+v", tokens)
		}
	})

	t.Run("interactive never expires", func(t *testing.T) {
		dataDir, _ := newTokenCommandStore(t)
		var err error
		var out string
		withStdin(t, "svc\n\nnever\n", func() {
			out = captureStdout(t, func() {
				err = addTokenCommand(dataDir, "", "", 0)
			})
		})
		if err != nil {
			t.Fatalf("addTokenCommand failed: %v", err)
		}
		if !strings.Contains(out, "Expires: Never") || strings.Contains(out, "Note:") {
			t.Errorf("Expected a never-expiring token with no note, got %q", out)
		}
	})

	t.Run("negative expiry skips the prompt", func(t *testing.T) {
		dataDir, _ := newTokenCommandStore(t)
		var err error
		withStdin(t, "", func() {
			captureStdout(t, func() {
				err = addTokenCommand(dataDir, "svc", "no expiry", -1)
			})
		})
		if err != nil {
			t.Fatalf("addTokenCommand failed: %v", err)
		}
		tokens := tokensOwnedBy(t, dataDir, "svc")
		if len(tokens) != 1 || tokens[0].ExpiresAt != nil {
			t.Errorf("Expected one non-expiring token for svc, got %+v", tokens)
		}
	})

	failures := []struct {
		name     string
		username string
		stdin    string
		want     string
	}{
		{"no owner", "", "", "owner username is required"},
		{"bad duration", "owner", "\nsoon\n", "invalid duration"},
		{"unknown owner", "nobody", "\nnever\n", "failed to create token"},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			dataDir, _ := newTokenCommandStore(t)
			var err error
			withStdin(t, tc.stdin, func() {
				captureStdout(t, func() {
					err = addTokenCommand(dataDir, tc.username, "", 0)
				})
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Expected an error containing %q, got %v", tc.want, err)
			}
		})
	}

	t.Run("unopenable store", func(t *testing.T) {
		err := addTokenCommand(unopenableDataDir(t), "owner", "x", time.Hour)
		if err == nil || !strings.Contains(err.Error(), "failed to open auth store") {
			t.Errorf("Expected an open failure, got %v", err)
		}
	})
}

func TestRemoveTokenCommand(t *testing.T) {
	dataDir, token := newTokenCommandStore(t)

	id := strconv.FormatInt(token.ID, 10)
	var err error
	out := captureStdout(t, func() { err = removeTokenCommand(dataDir, id) })
	if err != nil {
		t.Fatalf("removeTokenCommand failed: %v", err)
	}
	if !strings.Contains(out, "Token removed successfully: "+id) {
		t.Errorf("Expected a removal message, got %q", out)
	}
	if tokens := tokensOwnedBy(t, dataDir, "owner"); len(tokens) != 0 {
		t.Errorf("Expected the token to be gone, got %+v", tokens)
	}

	err = removeTokenCommand(dataDir, id)
	if err == nil || !strings.Contains(err.Error(), "failed to remove token") {
		t.Errorf("Expected a second removal to fail, got %v", err)
	}

	err = removeTokenCommand(unopenableDataDir(t), id)
	if err == nil || !strings.Contains(err.Error(), "failed to open auth store") {
		t.Errorf("Expected an open failure, got %v", err)
	}
}

func TestListTokensCommand(t *testing.T) {
	t.Run("no tokens", func(t *testing.T) {
		dataDir := t.TempDir()
		var err error
		out := captureStdout(t, func() { err = listTokensCommand(dataDir) })
		if err != nil {
			t.Fatalf("listTokensCommand failed: %v", err)
		}
		if !strings.Contains(out, "No tokens found.") {
			t.Errorf("Expected the empty message, got %q", out)
		}
	})

	t.Run("owners, expiry and notes", func(t *testing.T) {
		dataDir, _ := newTokenCommandStore(t)
		past := time.Now().Add(-time.Hour)
		future := time.Now().Add(time.Hour)
		withTokenStore(t, dataDir, func(store *auth.AuthStore) {
			as := store.AsActor(auth.SystemActor())
			for _, tc := range []struct {
				owner, note string
				expiry      *time.Time
			}{
				{"root", "a note well over twenty characters", &future},
				{"svc", "expired", &past},
			} {
				if _, _, err := as.CreateToken(tc.owner, tc.note, tc.expiry,
					true); err != nil {
					t.Fatalf("CreateToken for %s failed: %v", tc.owner, err)
				}
			}
		})

		var err error
		out := captureStdout(t, func() { err = listTokensCommand(dataDir) })
		if err != nil {
			t.Fatalf("listTokensCommand failed: %v", err)
		}
		for _, want := range []string{"owner", "root", "svc", "EXPIRED",
			"Active", "a note well over ...", "Never", "Yes"} {
			if !strings.Contains(out, want) {
				t.Errorf("Expected output to contain %q, got %q", want, out)
			}
		}
	})

	t.Run("unopenable store", func(t *testing.T) {
		err := listTokensCommand(unopenableDataDir(t))
		if err == nil || !strings.Contains(err.Error(), "failed to open auth store") {
			t.Errorf("Expected an open failure, got %v", err)
		}
	})
}

func TestParseDuration(t *testing.T) {
	day := 24 * time.Hour
	for input, want := range map[string]time.Duration{
		"12h": 12 * time.Hour,
		"30d": 30 * day,
		"2w":  14 * day,
		"1m":  30 * day,
		"1y":  365 * day,
	} {
		got, err := parseDuration(input)
		if err != nil || got != want {
			t.Errorf("parseDuration(%q) = %v, %v; want %v", input, got, err, want)
		}
	}
	for input, want := range map[string]string{
		"d":   "invalid duration format",
		"xd":  "invalid number in duration",
		"10s": "invalid duration unit",
	} {
		if _, err := parseDuration(input); err == nil ||
			!strings.Contains(err.Error(), want) {
			t.Errorf("parseDuration(%q): expected %q, got %v", input, want, err)
		}
	}
}

func TestTokenScopeCommands(t *testing.T) {
	dataDir, token := newTokenCommandStore(t)

	scopeOf := func() *auth.TokenScope {
		t.Helper()
		var scope *auth.TokenScope
		withTokenStore(t, dataDir, func(store *auth.AuthStore) {
			var err error
			scope, err = store.GetTokenScope(token.ID)
			if err != nil {
				t.Fatalf("GetTokenScope failed: %v", err)
			}
		})
		return scope
	}
	run := func(fn func() error) (string, error) {
		var err error
		out := captureStdout(t, func() { err = fn() })
		return out, err
	}

	out, err := run(func() error { return showTokenScopeCommand(dataDir, token.ID) })
	if err != nil || !strings.Contains(out, "No scope restrictions") {
		t.Errorf("Expected an unrestricted scope, got %q, %v", out, err)
	}

	out, err = run(func() error {
		return scopeTokenConnectionsCommand(dataDir, token.ID, "3,4")
	})
	if err != nil || !strings.Contains(out, "Set connection scope for token") {
		t.Fatalf("Scoping connections: got %q, %v", out, err)
	}
	if scope := scopeOf(); scope == nil || len(scope.Connections) != 2 ||
		scope.Connections[0].AccessLevel != auth.AccessLevelReadWrite {
		t.Errorf("Expected two read_write connections, got %+v", scope)
	}

	out, err = run(func() error {
		return scopeTokenToolsCommand(dataDir, token.ID, " tool_a , ,")
	})
	if err != nil || !strings.Contains(out, "Set MCP scope for token") {
		t.Fatalf("Scoping tools: got %q, %v", out, err)
	}

	out, err = run(func() error { return showTokenScopeCommand(dataDir, token.ID) })
	for _, want := range []string{"Connection 3 (read_write)",
		"MCP Privilege Scope (restricted to):", "- tool_a"} {
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("Expected the scope to show %q, got %q, %v", want, out, err)
		}
	}

	out, err = run(func() error { return scopeTokenToolsCommand(dataDir, token.ID, "") })
	if err != nil || !strings.Contains(out, "Cleared MCP scope") {
		t.Errorf("Clearing tools: got %q, %v", out, err)
	}
	out, err = run(func() error {
		return scopeTokenConnectionsCommand(dataDir, token.ID, "")
	})
	if err != nil || !strings.Contains(out, "Cleared connection scope") {
		t.Errorf("Clearing connections: got %q, %v", out, err)
	}

	// An admin-only scope reports both other kinds as unrestricted.
	withTokenStore(t, dataDir, func(store *auth.AuthStore) {
		if err := store.AsActor(auth.SystemActor()).SetTokenAdminScope(
			token.ID, []string{"manage_users"}, true); err != nil {
			t.Fatalf("SetTokenAdminScope failed: %v", err)
		}
	})
	out, err = run(func() error { return showTokenScopeCommand(dataDir, token.ID) })
	for _, want := range []string{"Connection Scope: Unrestricted",
		"MCP Privilege Scope: Unrestricted"} {
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("Expected the scope to show %q, got %q, %v", want, out, err)
		}
	}

	if _, err := run(func() error {
		return scopeTokenConnectionsCommand(dataDir, token.ID, "3")
	}); err != nil {
		t.Fatalf("Scoping connections: %v", err)
	}
	out, err = run(func() error { return clearTokenScopeCommand(dataDir, token.ID) })
	if err != nil || !strings.Contains(out, "Cleared all scope restrictions") {
		t.Errorf("Clearing the scope: got %q, %v", out, err)
	}
	if scope := scopeOf(); scope != nil {
		t.Errorf("Expected no scope after clearing, got %+v", scope)
	}
}

// TestTokenCommandsOnSuperuserToken checks that the CLI acts with
// superuser authority on a token a superuser owns, for each command that
// writes a token: the store refuses such a write unless the caller says
// it is allowed, so a command passing false would fail here.
func TestTokenCommandsOnSuperuserToken(t *testing.T) {
	dataDir, _ := newTokenCommandStore(t)

	var token *auth.StoredToken
	withTokenStore(t, dataDir, func(store *auth.AuthStore) {
		var err error
		_, token, err = store.AsActor(auth.SystemActor()).CreateToken("root",
			"root seed", nil, true)
		if err != nil {
			t.Fatalf("CreateToken failed: %v", err)
		}
	})

	steps := []struct {
		name string
		call func() error
		want string
	}{
		{"scope connections", func() error {
			return scopeTokenConnectionsCommand(dataDir, token.ID, "3")
		}, "Set connection scope for token"},
		{"scope tools", func() error {
			return scopeTokenToolsCommand(dataDir, token.ID, "tool_a")
		}, "Set MCP scope for token"},
		{"clear scope", func() error {
			return clearTokenScopeCommand(dataDir, token.ID)
		}, "Cleared all scope restrictions"},
		{"remove", func() error {
			return removeTokenCommand(dataDir,
				strconv.FormatInt(token.ID, 10))
		}, "Token removed successfully"},
	}
	for _, step := range steps {
		var err error
		out := captureStdout(t, func() { err = step.call() })
		if err != nil || !strings.Contains(out, step.want) {
			t.Fatalf("%s on root's token: got %q, %v", step.name, out, err)
		}
	}
	if tokens := tokensOwnedBy(t, dataDir, "root"); len(tokens) != 0 {
		t.Errorf("Expected root's token to be gone, got %+v", tokens)
	}
}

func TestTokenScopeCommandFailures(t *testing.T) {
	dataDir, token := newTokenCommandStore(t)
	bad := unopenableDataDir(t)
	const missing = int64(999999)

	cases := []struct {
		name string
		call func() error
		want string
	}{
		{"connections: no id", func() error {
			return scopeTokenConnectionsCommand(dataDir, 0, "1")
		}, "valid token ID is required"},
		{"connections: unopenable", func() error {
			return scopeTokenConnectionsCommand(bad, token.ID, "1")
		}, "failed to open auth store"},
		{"connections: unparseable", func() error {
			return scopeTokenConnectionsCommand(dataDir, token.ID, "one")
		}, "failed to parse connection IDs"},
		{"connections: unknown token", func() error {
			return scopeTokenConnectionsCommand(dataDir, missing, "1")
		}, "failed to set token connection scope"},
		{"tools: no id", func() error {
			return scopeTokenToolsCommand(dataDir, -1, "tool_a")
		}, "valid token ID is required"},
		{"tools: unopenable", func() error {
			return scopeTokenToolsCommand(bad, token.ID, "tool_a")
		}, "failed to open auth store"},
		{"tools: unknown tool", func() error {
			return scopeTokenToolsCommand(dataDir, token.ID, "no_such_tool")
		}, "failed to set token MCP scope"},
		{"clear: no id", func() error {
			return clearTokenScopeCommand(dataDir, 0)
		}, "valid token ID is required"},
		{"clear: unopenable", func() error {
			return clearTokenScopeCommand(bad, token.ID)
		}, "failed to open auth store"},
		{"show: no id", func() error {
			return showTokenScopeCommand(dataDir, 0)
		}, "valid token ID is required"},
		{"show: unopenable", func() error {
			return showTokenScopeCommand(bad, token.ID)
		}, "failed to open auth store"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			captureStdout(t, func() { err = tc.call() })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Expected an error containing %q, got %v", tc.want, err)
			}
		})
	}
}
