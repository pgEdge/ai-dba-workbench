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
	"database/sql"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// =============================================================================
// Token ownership (issue #522)
//
// Creating a token, or widening or clearing a token's scope, is allowed
// only on tokens the caller owns, for sessions and token callers alike;
// a holder of manage_token_scopes may narrow or delete any token. A
// superuser is unrestricted. A token caller stays bounded by its ceiling
// as well, siblings with the same owner included.
// =============================================================================

// ownershipFixture holds carol, who is not a superuser but holds
// manage_token_scopes, manage_users and manage_groups, with an
// unscoped token, and one target token each for carol and for dave.
type ownershipFixture struct {
	handler *RBACHandler
	store   *auth.AuthStore
	dir     string
	carolID int64
	carolTk int64
	own     int64
	other   int64
}

// targetAdminScope is the admin scope each target token starts with.
var targetAdminScope = []string{auth.PermManageUsers,
	auth.PermManageTokenScopes}

func newOwnershipFixture(t *testing.T) (*ownershipFixture, func()) {
	t.Helper()
	handler, store, dir, cleanup := createTestRBACHandlerWithDir(t)
	f := &ownershipFixture{handler: handler, store: store, dir: dir}
	f.carolID = setupUserWithPermissions(t, store, "carol", []string{
		auth.PermManageTokenScopes, auth.PermManageUsers,
		auth.PermManageGroups})
	if err := store.CreateUser("dave", "Password1234", "", "", ""); err != nil {
		cleanup()
		t.Fatalf("CreateUser failed: %v", err)
	}
	if _, err := store.RegisterMCPPrivilege("known_tool",
		auth.MCPPrivilegeTypeTool, "known", false); err != nil {
		cleanup()
		t.Fatalf("RegisterMCPPrivilege failed: %v", err)
	}
	f.carolTk = f.mint(t, "carol", nil)
	f.own = f.mint(t, "carol", targetAdminScope)
	f.other = f.mint(t, "dave", targetAdminScope)
	return f, cleanup
}

// mint creates a token for owner, with the given admin scope when it is
// not nil, and returns its id.
func (f *ownershipFixture) mint(t *testing.T, owner string,
	adminScope []string) int64 {

	t.Helper()
	actor := f.store.AsActor(auth.SystemActor())
	_, token, err := actor.CreateToken(owner, "ownership test", nil, true)
	if err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}
	if adminScope != nil {
		if err := actor.SetTokenAdminScope(token.ID, adminScope,
			true); err != nil {
			t.Fatalf("SetTokenAdminScope failed: %v", err)
		}
	}
	return token.ID
}

// callers returns the three principals the matrix is run as.
func (f *ownershipFixture) callers() map[string]func(*http.Request) *http.Request {
	return map[string]func(*http.Request) *http.Request{
		"session": func(r *http.Request) *http.Request {
			return withUser(r, f.carolID)
		},
		"token": func(r *http.Request) *http.Request {
			return withToken(r, f.carolID, f.carolTk)
		},
		"superuser": withSuperuserSession,
	}
}

func tokenPathOf(id int64) string {
	return "/api/v1/rbac/tokens/" + strconv.FormatInt(id, 10)
}

// TestTokenOwnershipMatrix runs every scope write and delete, as a
// session, a token and a superuser, against a token the caller owns
// and one it does not.
func TestTokenOwnershipMatrix(t *testing.T) {
	ops := []struct {
		name, method, suffix, body string
		widens                     bool
	}{
		{"narrow", http.MethodPut, "/scope",
			`{"admin_permissions":["manage_users"]}`, false},
		{"keep", http.MethodPut, "/scope",
			`{"admin_permissions":["manage_users","manage_token_scopes"]}`,
			false},
		{"widen", http.MethodPut, "/scope",
			`{"admin_permissions":["manage_users","manage_groups"]}`, true},
		{"clear", http.MethodDelete, "/scope", "", true},
		{"delete", http.MethodDelete, "", "", false},
	}
	for _, callerName := range []string{"session", "token", "superuser"} {
		for _, target := range []string{"own", "other"} {
			for _, op := range ops {
				name := callerName + " " + op.name + " " + target
				t.Run(name, func(t *testing.T) {
					f, cleanup := newOwnershipFixture(t)
					defer cleanup()
					id := f.own
					if target == "other" {
						id = f.other
					}
					rec := tokenRouteRequest(f.handler, op.method,
						tokenPathOf(id)+op.suffix, op.body,
						f.callers()[callerName])
					refusal := ""
					switch {
					case callerName == "token" && op.name == "clear" &&
						target == "own":
						// Clearing hands the token carol's whole reach,
						// and her admin permissions act on every
						// connection, which her own token's ceiling does
						// not cover (#528): the ceiling refuses it before
						// the owner rule is reached.
						refusal = refuseTokenScope
					case op.widens && target == "other" &&
						callerName != "superuser":
						refusal = refuseTokenNotOwned
					}
					if refusal == "" {
						assertStatus(t, rec, http.StatusNoContent)
						return
					}
					assertRefusedWith(t, rec, refusal)
					if got := adminScopeOf(t, f.store, id); len(got) != 2 {
						t.Errorf("Expected the scope to be untouched, got %v",
							got)
					}
				})
			}
		}
	}
}

// TestTokenOwnershipCreate checks that a caller who is not a superuser
// may mint a token for itself alone, an unknown owner being refused
// exactly as another user is, and that a superuser may mint one for
// anyone. An empty refusal means the token is created.
func TestTokenOwnershipCreate(t *testing.T) {
	for callerName, owners := range map[string]map[string]string{
		"session": {"carol": "", "dave": refuseTokenForOtherOwner,
			"nobody": refuseTokenForOtherOwner},
		// carol's own token passes the owner rule for carol, but her
		// admin permissions act on every connection, which its ceiling
		// does not cover (#528), so the ceiling refuses the new token.
		"token": {"carol": refuseTokenOwner,
			"dave": refuseTokenForOtherOwner, "nobody": refuseTokenForOtherOwner},
		"superuser": {"carol": "", "dave": ""},
	} {
		for owner, refusal := range owners {
			t.Run(callerName+" for "+owner, func(t *testing.T) {
				f, cleanup := newOwnershipFixture(t)
				defer cleanup()
				rec := tokenRouteRequest(f.handler, http.MethodPost,
					"/api/v1/rbac/tokens/",
					`{"owner_username":"`+owner+`"}`,
					f.callers()[callerName])
				if refusal == "" {
					assertStatus(t, rec, http.StatusCreated)
					return
				}
				assertRefusedWith(t, rec, refusal)
			})
		}
	}
}

// TestTokenOwnershipSiblingEscape checks that a narrowed token cannot
// use its owner's ownership to mint, widen or clear a sibling beyond
// itself: the ceiling still bounds it, whoever owns the target.
func TestTokenOwnershipSiblingEscape(t *testing.T) {
	f, cleanup := newOwnershipFixture(t)
	defer cleanup()
	narrowed := f.mint(t, "carol", []string{auth.PermManageTokenScopes})
	sibling := f.mint(t, "carol", []string{auth.PermManageTokenScopes})
	as := func(r *http.Request) *http.Request {
		return withToken(r, f.carolID, narrowed)
	}

	assertRefusedWith(t, tokenRouteRequest(f.handler, http.MethodPost,
		"/api/v1/rbac/tokens/", `{"owner_username":"carol"}`, as),
		refuseTokenOwner)
	for _, id := range []int64{sibling, narrowed} {
		assertRefusedWith(t, tokenRouteRequest(f.handler, http.MethodPut,
			tokenPathOf(id)+"/scope",
			`{"admin_permissions":["manage_token_scopes","manage_users"]}`,
			as), refuseTokenScope)
		assertRefusedWith(t, tokenRouteRequest(f.handler, http.MethodDelete,
			tokenPathOf(id)+"/scope", "", as), refuseTokenScope)
		if got := adminScopeOf(t, f.store, id); len(got) != 1 ||
			got[0] != auth.PermManageTokenScopes {
			t.Errorf("Expected token %d to keep its scope, got %v", id, got)
		}
	}
}

// TestTokenOwnershipRefusalsAuditOnce checks that repeated ownership
// refusals each leave exactly one audit row, the handler's coalesced
// denial, with no failure row from the store beside it.
func TestTokenOwnershipRefusalsAuditOnce(t *testing.T) {
	const attempts = 5
	f, cleanup := newOwnershipFixture(t)
	defer cleanup()
	session := f.callers()["session"]

	requests := []struct {
		name, method, path, body, action, refusal string
	}{
		{"create", http.MethodPost, "/api/v1/rbac/tokens/",
			`{"owner_username":"dave"}`, "token.create",
			refuseTokenForOtherOwner},
		{"widen", http.MethodPut, tokenPathOf(f.other) + "/scope",
			`{"admin_permissions":["*"]}`, "token.scope.set",
			refuseTokenNotOwned},
		{"clear", http.MethodDelete, tokenPathOf(f.other) + "/scope", "",
			"token.scope.clear", refuseTokenNotOwned},
	}
	for _, rq := range requests {
		t.Run(rq.name, func(t *testing.T) {
			_, before, err := f.store.ListAuditEvents(auth.AuditFilter{Limit: 1})
			if err != nil {
				t.Fatalf("ListAuditEvents failed: %v", err)
			}
			for i := 0; i < attempts; i++ {
				assertRefusedWith(t, tokenRouteRequest(f.handler, rq.method,
					rq.path, rq.body, session), rq.refusal)
			}
			events, after, err := f.store.ListAuditEvents(auth.AuditFilter{
				Limit: attempts * 2})
			if err != nil {
				t.Fatalf("ListAuditEvents failed: %v", err)
			}
			if after-before != 1 || events[0].Outcome != auth.OutcomeDenied ||
				events[0].Action != rq.action {
				t.Errorf("%d refusals added %d rows, newest %s %q; want one denied %s row",
					attempts, after-before, events[0].Action,
					events[0].Outcome, rq.action)
			}
		})
	}
}

// TestTokenScopeUnknownNamesRejected checks that a scope naming an
// admin permission or MCP item that does not exist is refused with 400,
// whoever the caller and whoever owns the token, rather than refused
// with 403 as a widening by the ceiling or the owner rule.
func TestTokenScopeUnknownNamesRejected(t *testing.T) {
	bodies := map[string]string{
		"admin": `{"admin_permissions":["manage_everything"]}`,
		"mcp":   `{"mcp_privileges":["known_tool","no_such_tool"]}`,
	}
	for callerName := range map[string]bool{"session": true, "token": true,
		"superuser": true} {
		for _, target := range []string{"own", "other"} {
			for kind, body := range bodies {
				t.Run(callerName+" "+target+" "+kind, func(t *testing.T) {
					f, cleanup := newOwnershipFixture(t)
					defer cleanup()
					id := f.own
					if target == "other" {
						id = f.other
					}
					// A restricted MCP kind, so that the unknown name
					// would otherwise read as a widening.
					if err := f.store.AsActor(auth.SystemActor()).
						SetTokenMCPScopeByNames(id, []string{"known_tool"},
							true); err != nil {
						t.Fatalf("SetTokenMCPScopeByNames failed: %v", err)
					}
					rec := tokenRouteRequest(f.handler, http.MethodPut,
						tokenPathOf(id)+"/scope", body,
						f.callers()[callerName])
					if rec.Code != http.StatusBadRequest ||
						!strings.Contains(rec.Body.String(), "unknown") {
						t.Fatalf("Expected 400, got %d: %s", rec.Code,
							rec.Body.String())
					}
				})
			}
		}
	}
}

// authTableRenames holds, for each auth table a test breaks, the
// literal statement that renames it, so that no SQL is built from a
// variable.
var authTableRenames = map[string]string{
	"users": "ALTER TABLE users RENAME TO users_gone",
	"mcp_privilege_identifiers": "ALTER TABLE mcp_privilege_identifiers " +
		"RENAME TO mcp_privilege_identifiers_gone",
}

// renameAuthTable renames a table in the auth database, so that every
// read of it fails.
func renameAuthTable(t *testing.T, dataDir, table string) {
	t.Helper()
	rename, ok := authTableRenames[table]
	if !ok {
		t.Fatalf("No rename statement for %s", table)
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "auth.db"))
	if err != nil {
		t.Fatalf("Failed to open the auth database: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(rename); err != nil {
		t.Fatalf("Failed to rename %s: %v", table, err)
	}
}

// TestTokenOwnershipLookupsFail checks that a failed owner or MCP item
// lookup is answered with 500 rather than taken to mean the name is
// someone else's or unknown.
func TestTokenOwnershipLookupsFail(t *testing.T) {
	t.Run("owner", func(t *testing.T) {
		f, cleanup := newOwnershipFixture(t)
		defer cleanup()
		renameAuthTable(t, f.dir, "users")
		rec := tokenRouteRequest(f.handler, http.MethodPost,
			"/api/v1/rbac/tokens/", `{"owner_username":"carol"}`,
			f.callers()["session"])
		assertError(t, rec, http.StatusInternalServerError,
			"Failed to get user")
	})
	t.Run("mcp", func(t *testing.T) {
		f, cleanup := newOwnershipFixture(t)
		defer cleanup()
		renameAuthTable(t, f.dir, "mcp_privilege_identifiers")
		rec := tokenRouteRequest(f.handler, http.MethodPut,
			tokenPathOf(f.own)+"/scope", `{"mcp_privileges":["known_tool"]}`,
			withSuperuserSession)
		assertError(t, rec, http.StatusInternalServerError,
			"Failed to set token scope")
	})
}
