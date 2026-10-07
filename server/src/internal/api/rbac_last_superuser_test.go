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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// TestLastSuperuserIsKept checks that a superuser session and an
// unscoped superuser token alike are refused with a 409 when they try
// to demote, disable or delete the last enabled superuser, and that the
// account is left as it was.
func TestLastSuperuserIsKept(t *testing.T) {
	changes := map[string]func(h *RBACHandler, id int64,
		wrap func(*http.Request) *http.Request) *httptest.ResponseRecorder{
		"demote": func(h *RBACHandler, id int64,
			wrap func(*http.Request) *http.Request) *httptest.ResponseRecorder {
			return putUser(t, h, id, map[string]any{"is_superuser": false}, wrap)
		},
		"disable": func(h *RBACHandler, id int64,
			wrap func(*http.Request) *http.Request) *httptest.ResponseRecorder {
			return putUser(t, h, id, map[string]any{"enabled": false}, wrap)
		},
		"delete": func(h *RBACHandler, id int64,
			wrap func(*http.Request) *http.Request) *httptest.ResponseRecorder {
			req := wrap(httptest.NewRequest(http.MethodDelete,
				"/api/v1/rbac/users/"+strconv.FormatInt(id, 10), nil))
			rec := httptest.NewRecorder()
			h.deleteUser(rec, req, id)
			return rec
		},
	}
	for name, change := range changes {
		for _, caller := range []string{"session", "token"} {
			t.Run(name+" by "+caller, func(t *testing.T) {
				handler, store, cleanup := createTestRBACHandler(t)
				defer cleanup()
				mustCreateSuperuser(t, store, "root")
				rootID, err := store.GetUserID("root")
				if err != nil {
					t.Fatalf("GetUserID failed: %v", err)
				}
				wrap := withSuperuser
				if caller == "token" {
					_, token, err := store.AsActor(auth.SystemActor()).CreateToken("root", "root token", nil, true)
					if err != nil {
						t.Fatalf("CreateToken failed: %v", err)
					}
					wrap = func(r *http.Request) *http.Request {
						return withSuperuserToken(r, token.ID)
					}
				}

				rec := change(handler, rootID, wrap)
				if rec.Code != http.StatusConflict {
					t.Fatalf("Expected 409, got %d: %s", rec.Code,
						rec.Body.String())
				}
				const want = "Cannot demote, disable or delete the last " +
					"enabled superuser"
				if !strings.Contains(rec.Body.String(), want) {
					t.Errorf("Expected %q in the body, got %s", want,
						rec.Body.String())
				}
				user, err := store.GetUser("root")
				if err != nil || user == nil || !user.IsSuperuser ||
					!user.Enabled {
					t.Errorf("The refused change altered the account: %+v, %v",
						user, err)
				}
			})
		}
	}
}

// TestLastSuperuserGuardAllowsWithSpare checks that the same changes
// pass once another enabled superuser exists.
func TestLastSuperuserGuardAllowsWithSpare(t *testing.T) {
	handler, store, cleanup := createTestRBACHandler(t)
	defer cleanup()
	mustCreateSuperuser(t, store, "root")
	mustCreateSuperuser(t, store, "spare-admin")
	rootID, err := store.GetUserID("root")
	if err != nil {
		t.Fatalf("GetUserID failed: %v", err)
	}
	rec := putUser(t, handler, rootID, map[string]any{"is_superuser": false},
		withSuperuser)
	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if user, _ := store.GetUser("root"); user == nil || user.IsSuperuser {
		t.Errorf("Expected root to be demoted, got %+v", user)
	}
}
