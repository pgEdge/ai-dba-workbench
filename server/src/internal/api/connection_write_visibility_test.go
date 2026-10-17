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
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// Tests for issues #677 and #710: PUT and DELETE /api/v1/connections/{id}
// answer a caller exactly as for a missing connection when the caller
// cannot see the connection, so the ownership 403 no longer confirms that
// an ID exists, and manage_connections does not reach a connection the
// caller cannot see. A visible connection the caller does not own still
// draws the 403 without manage_connections.

// connWriteVisibilityCase is one PUT or DELETE request against
// /api/v1/connections/{id}.
type connWriteVisibilityCase struct {
	method string
	body   any
	// forbidden is the ownership refusal for a visible connection.
	forbidden string
}

// connWriteVisibilityCases returns the update and delete requests, each
// with the refusal it gives a visible connection the caller does not own.
func connWriteVisibilityCases() []connWriteVisibilityCase {
	name := "renamed"
	return []connWriteVisibilityCase{
		{http.MethodPut, ConnectionFullUpdateRequest{Name: &name},
			"Permission denied: you must be the owner or have the manage_connections permission to update this connection"},
		{http.MethodDelete, nil,
			"Permission denied: you must be the owner or have the manage_connections permission to delete this connection"},
	}
}

// newGroupGrantedCaller creates a user holding the given access level on
// connID through a group grant, and signs them in.
func (f *connCovFixture) newGroupGrantedCaller(t *testing.T, username string, connID int, level string) connCovCaller {
	t.Helper()
	id := newGroupGrantedUser(t, f.store, username, connID, level)
	token, _, err := f.store.AuthenticateUser(username, "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser(%s): %v", username, err)
	}
	return connCovCaller{id: id, username: username, token: token}
}

// grantAdminPermission gives the caller an admin permission through a
// group of its own.
func (f *connCovFixture) grantAdminPermission(t *testing.T, c connCovCaller, permission string) {
	t.Helper()
	groupID, err := f.store.CreateGroup(c.username+"_admin", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := f.store.AddUserToGroup(groupID, c.id); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := f.store.GrantAdminPermission(groupID, permission); err != nil {
		t.Fatalf("GrantAdminPermission: %v", err)
	}
}

// connectionName returns the stored name of a connection.
func (f *connCovFixture) connectionName(t *testing.T, id int) string {
	t.Helper()
	var name string
	if err := f.pool.QueryRow(context.Background(),
		"SELECT name FROM connections WHERE id = $1", id).Scan(&name); err != nil {
		t.Fatalf("Read connection %d: %v", id, err)
	}
	return name
}

// TestConnectionWritesHideInvisibleConnections checks that a connection
// the caller cannot see gets the same status and body as a missing ID,
// before the body is decoded and without recording a refusal, and that
// the row is left alone.
func TestConnectionWritesHideInvisibleConnections(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	var recorded []string
	f.handler.SetDenialRecorder(func(_ *http.Request, reason string) {
		recorded = append(recorded, reason)
	})

	owner := f.newCaller(t, "vis677_owner")
	stranger := f.newCaller(t, "vis677_stranger")
	private := f.seed(t, "private", owner.username, false)
	restricted := f.seed(t, "restricted", owner.username, true)
	missing := restricted + 1000

	// A group grant naming the shared connection restricts it to that
	// group, so the stranger can no longer see it.
	_ = f.newGroupGrantedCaller(t, "vis677_granted", restricted, auth.AccessLevelRead)

	for _, tc := range connWriteVisibilityCases() {
		for _, body := range []any{tc.body, "{not json"} {
			if tc.method == http.MethodDelete && body != nil {
				continue
			}
			missingRec := f.serve(asCaller(connCovRequest(t, tc.method,
				connCovPath(missing, ""), body), stranger))
			assertError(t, missingRec, http.StatusNotFound, "Connection not found")

			for _, id := range []int{private, restricted} {
				rec := f.serve(asCaller(connCovRequest(t, tc.method,
					connCovPath(id, ""), body), stranger))
				if rec.Code != missingRec.Code || rec.Body.String() != missingRec.Body.String() {
					t.Errorf("%s connection %d (body %v): got %d %q, want the missing-ID answer %d %q",
						tc.method, id, body, rec.Code, rec.Body.String(),
						missingRec.Code, missingRec.Body.String())
				}
			}
		}
	}

	if len(recorded) != 0 {
		t.Errorf("Hidden connections recorded refusals: %v", recorded)
	}
	for _, id := range []int{private, restricted} {
		if !f.rowExists(t, id) {
			t.Fatalf("Connection %d was deleted", id)
		}
		if got := f.connectionName(t, id); got == "renamed" {
			t.Errorf("Connection %d was renamed", id)
		}
	}
}

// TestConnectionWritesRefuseVisibleUnownedConnections checks that a
// caller without manage_connections who can see a connection, through
// sharing or a read grant, still gets the ownership 403, and that the
// row is left alone.
func TestConnectionWritesRefuseVisibleUnownedConnections(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "vis677_vowner")
	stranger := f.newCaller(t, "vis677_vstranger")
	shared := f.seed(t, "shared", owner.username, true)
	granted := f.seed(t, "granted", owner.username, false)
	reader := f.newGroupGrantedCaller(t, "vis677_reader", granted, auth.AccessLevelRead)

	callers := []struct {
		caller connCovCaller
		id     int
	}{
		{stranger, shared},
		{reader, granted},
	}
	for _, tc := range connWriteVisibilityCases() {
		for _, c := range callers {
			rec := f.serve(asCaller(connCovRequest(t, tc.method,
				connCovPath(c.id, ""), tc.body), c.caller))
			assertError(t, rec, http.StatusForbidden, tc.forbidden)
		}
	}

	for _, id := range []int{shared, granted} {
		if !f.rowExists(t, id) {
			t.Fatalf("Connection %d was deleted", id)
		}
		if got := f.connectionName(t, id); got == "renamed" {
			t.Errorf("Connection %d was renamed", id)
		}
	}
}

// TestConnectionWritesHideInvisibleConnectionsFromManagers checks that
// manage_connections does not reach a connection the caller cannot see
// (issue #710): PUT and DELETE answer exactly as for a missing ID, record
// no refusal, write nothing, and return nothing of the connection.
func TestConnectionWritesHideInvisibleConnectionsFromManagers(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	var recorded []string
	f.handler.SetDenialRecorder(func(_ *http.Request, reason string) {
		recorded = append(recorded, reason)
	})

	owner := f.newCaller(t, "vis710_owner")
	manager := f.newCaller(t, "vis710_manager", auth.PermManageConnections)
	private := f.seed(t, "vis710-private", owner.username, false)
	restricted := f.seed(t, "vis710-restricted", owner.username, true)
	missing := restricted + 1000
	_ = f.newGroupGrantedCaller(t, "vis710_granted", restricted, auth.AccessLevelRead)

	shared := true
	for _, tc := range connWriteVisibilityCases() {
		bodies := []any{tc.body, ConnectionFullUpdateRequest{IsShared: &shared}, "{not json"}
		if tc.method == http.MethodDelete {
			bodies = []any{nil}
		}
		for _, body := range bodies {
			missingRec := f.serve(asCaller(connCovRequest(t, tc.method,
				connCovPath(missing, ""), body), manager))
			assertError(t, missingRec, http.StatusNotFound, "Connection not found")

			for _, id := range []int{private, restricted} {
				rec := f.serve(asCaller(connCovRequest(t, tc.method,
					connCovPath(id, ""), body), manager))
				if rec.Code != missingRec.Code || rec.Body.String() != missingRec.Body.String() {
					t.Errorf("%s connection %d (body %v): got %d %q, want the missing-ID answer %d %q",
						tc.method, id, body, rec.Code, rec.Body.String(),
						missingRec.Code, missingRec.Body.String())
				}
				for _, secret := range []string{"vis710-", "db.example.com", owner.username} {
					if strings.Contains(rec.Body.String(), secret) {
						t.Errorf("%s connection %d leaked %q: %s", tc.method, id, secret, rec.Body.String())
					}
				}
			}
		}
	}

	if len(recorded) != 0 {
		t.Errorf("Hidden connections recorded refusals: %v", recorded)
	}
	for _, id := range []int{private, restricted} {
		if !f.rowExists(t, id) {
			t.Fatalf("Connection %d was deleted", id)
		}
		if got := f.connectionName(t, id); got == "renamed" {
			t.Errorf("Connection %d was renamed", id)
		}
	}
	var privateShared bool
	if err := f.pool.QueryRow(context.Background(),
		"SELECT is_shared FROM connections WHERE id = $1", private).Scan(&privateShared); err != nil {
		t.Fatalf("Read is_shared: %v", err)
	}
	if privateShared {
		t.Error("Private connection was made shared")
	}
}

// TestConnectionWritesManagerChangesVisibleConnection checks that a
// manage_connections holder may still change and delete a connection it
// can see but does not own, whether through sharing or a read grant.
func TestConnectionWritesManagerChangesVisibleConnection(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "vis710_vowner")
	shared := f.seed(t, "vis710-shared", owner.username, true)
	granted := f.seed(t, "vis710-granted", owner.username, false)
	manager := f.newGroupGrantedCaller(t, "vis710_vmanager", granted, auth.AccessLevelRead)
	f.grantAdminPermission(t, manager, auth.PermManageConnections)

	for _, id := range []int{shared, granted} {
		name := fmt.Sprintf("managed-%d", id)
		rec := f.serve(asCaller(connCovRequest(t, http.MethodPut, connCovPath(id, ""),
			ConnectionFullUpdateRequest{Name: &name}), manager))
		assertStatus(t, rec, http.StatusOK)
		if got := f.connectionName(t, id); got != name {
			t.Errorf("Connection %d: name = %q, want %q", id, got, name)
		}

		rec = f.serve(asCaller(connCovRequest(t, http.MethodDelete, connCovPath(id, ""), nil), manager))
		assertStatus(t, rec, http.StatusNoContent)
		if f.rowExists(t, id) {
			t.Errorf("Connection %d was not deleted", id)
		}
	}
}

// TestConnectionWritesSuperuserReachesAnyConnection checks that a
// superuser still changes and deletes a private connection another user
// owns, and that a missing ID still answers 404.
func TestConnectionWritesSuperuserReachesAnyConnection(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	su := f.newCaller(t, "vis710_su")
	id := f.seed(t, "vis710-elsewhere", "vis710_absent_owner", false)

	name := "superuser-renamed"
	rec := f.serve(asSuperuserCaller(connCovRequest(t, http.MethodPut, connCovPath(id, ""),
		ConnectionFullUpdateRequest{Name: &name}), su))
	assertStatus(t, rec, http.StatusOK)
	if got := f.connectionName(t, id); got != name {
		t.Errorf("Name = %q, want %q", got, name)
	}

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		var body any
		if method == http.MethodPut {
			body = ConnectionFullUpdateRequest{Name: &name}
		}
		rec = f.serve(asSuperuserCaller(connCovRequest(t, method,
			connCovPath(id+1000, ""), body), su))
		assertError(t, rec, http.StatusNotFound, "Connection not found")
	}

	rec = f.serve(asSuperuserCaller(connCovRequest(t, http.MethodDelete, connCovPath(id, ""), nil), su))
	assertStatus(t, rec, http.StatusNoContent)
	if f.rowExists(t, id) {
		t.Error("Connection was not deleted")
	}
}

// newCallerAPIToken creates an API token for the given user and returns
// its raw bearer.
func (f *connCovFixture) newCallerAPIToken(t *testing.T, username string) string {
	t.Helper()
	raw, _, err := f.store.AsActor(auth.SystemActor()).CreateToken(
		username, "issue 677 test", nil, false)
	if err != nil {
		t.Fatalf("CreateToken(%s): %v", username, err)
	}
	return raw
}

// TestConnectionWritesHideInvisibleConnectionsFromTokens checks that an
// API token, authenticated from the bearer alone as the middleware does
// it, gets the same 404 for a connection its owner cannot see as for a
// missing ID.
func TestConnectionWritesHideInvisibleConnectionsFromTokens(t *testing.T) {
	f := newConnCovFixture(t, connCovSecret)
	owner := f.newCaller(t, "vis677_towner")
	stranger := f.newCaller(t, "vis677_tstranger")
	bearer := f.newCallerAPIToken(t, stranger.username)
	id := f.seed(t, "token-private", owner.username, false)

	for _, tc := range connWriteVisibilityCases() {
		missingRec := f.serve(withAuth(t, f.store, connCovRequest(t, tc.method,
			connCovPath(id+1000, ""), tc.body), bearer))
		assertError(t, missingRec, http.StatusNotFound, "Connection not found")
		rec := f.serve(withAuth(t, f.store, connCovRequest(t, tc.method,
			connCovPath(id, ""), tc.body), bearer))
		if rec.Code != missingRec.Code || rec.Body.String() != missingRec.Body.String() {
			t.Errorf("%s hidden connection: got %d %q, want the missing-ID answer %d %q",
				tc.method, rec.Code, rec.Body.String(), missingRec.Code, missingRec.Body.String())
		}
	}
	if !f.rowExists(t, id) {
		t.Fatal("Connection was deleted")
	}
}

// TestConnectionWritesOwnerReachesRestrictedConnection checks that the
// owner of a connection a group grant restricts to others can still
// change and delete it, through a session or an API token, since the 404
// above relies on the owner always seeing their own connection.
func TestConnectionWritesOwnerReachesRestrictedConnection(t *testing.T) {
	for _, viaToken := range []bool{false, true} {
		f := newConnCovFixture(t, connCovSecret)
		owner := f.newCaller(t, "vis677_rowner")
		id := f.seed(t, "restricted-own", owner.username, false)
		_ = f.newGroupGrantedCaller(t, "vis677_rgranted", id, auth.AccessLevelRead)

		send := func(req *http.Request) *http.Request { return asCaller(req, owner) }
		if viaToken {
			bearer := f.newCallerAPIToken(t, owner.username)
			send = func(req *http.Request) *http.Request { return withAuth(t, f.store, req, bearer) }
		}

		name := "owner-renamed"
		rec := f.serve(send(connCovRequest(t, http.MethodPut, connCovPath(id, ""),
			ConnectionFullUpdateRequest{Name: &name})))
		assertStatus(t, rec, http.StatusOK)
		if got := f.connectionName(t, id); got != name {
			t.Errorf("token=%v: Name = %q, want %q", viaToken, got, name)
		}

		rec = f.serve(send(connCovRequest(t, http.MethodDelete, connCovPath(id, ""), nil)))
		assertStatus(t, rec, http.StatusNoContent)
		if f.rowExists(t, id) {
			t.Errorf("token=%v: connection was not deleted", viaToken)
		}
	}
}
