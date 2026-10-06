/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - RBAC Patterns
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# RBAC Gating Patterns for HTTP Handlers

This document captures the authorization-gate patterns the server's
HTTP handlers use, when each variant applies, and the test patterns
that lock them in. The cluster-handler audit for GitHub issue #207
established three canonical models; new handlers MUST pick one
deliberately rather than invent a fourth.

## When To Gate

Every mutating handler (POST, PUT, PATCH, DELETE) must perform an
authorization check before any of the following:

- Decoding the request body (`DecodeJSONBody`).
- Reading or writing the datastore.
- Issuing any side-effecting call (logging, metrics tag emission with
  user-supplied keys, outbound HTTP).

Placing the gate first prevents denied callers from probing payload
shape via validation error messages, and avoids needless datastore
load on rejected requests.

GET handlers gate on visibility (`resolveVisibleConnections` +
`clusterMembersVisible` / `clusterHasVisibleConnection`) rather than
admin permission; that is a different surface and is documented in
the issue-#35 regression test files.

## Variant 1: Plain Admin Gate

Use this when the handler creates a new object (no owner yet), or
when the resource is a system-wide concern with no per-object owner
concept. Examples: cluster creation, topology relationship rewrites,
server attach/detach, auto-detected group/cluster mutations.

The canonical form, copied as a literal block at the top of the
handler body:

```go
if !h.rbacChecker.HasAdminPermission(r.Context(),
    auth.PermManageConnections) {
    RespondError(w, http.StatusForbidden,
        "Permission denied: requires manage_connections permission")
    return
}
```

The reference implementation is `updateAutoDetectedCluster` in
`server/src/internal/api/cluster_handlers.go`. Use the same error
wording so the client-facing message stays uniform across endpoints.

When a handler must stamp the caller's username onto the new row
(e.g. `owner_username`), call `getUserInfoCompat` first to extract
the username, then place the Variant 1 gate immediately afterward
and BEFORE any request-body decoding. The 401 from
`getUserInfoCompat` is authentication, not authorization: a missing
or invalid bearer token must short-circuit before any permission
check, but a valid token must still pass the gate before reaching
`DecodeJSONBody` or any datastore call. The reference for this
two-step pattern is `createConnection` in
`server/src/internal/api/connection_handlers.go`, gated for
GitHub issue #233 as the follow-up to #207.

A regression-family caution: when issue #207 introduced this gate
across cluster handlers, the audit missed the connection-create
handler because that handler already had a gate on the IsShared
branch. A partial gate is NOT a full gate; if a mutating endpoint
performs writes regardless of an input flag, the gate must precede
the branch on that flag, not sit inside it. When auditing a
handler that has any existing `HasAdminPermission` call, verify the
gate covers every write path, not just the "obviously sensitive"
ones.

When a mutating per-resource handler already calls
`rc.CanAccessConnection` (or another visibility check) at the top to
return 403 on non-visible callers, place the Variant 1 gate AFTER
the visibility check and BEFORE `DecodeJSONBody`. The visibility
check must run first so a non-visible caller still gets the
existing "Access denied" response and learns nothing about the
resource's existence (or absence). The admin gate then catches the
distinct bug class where a caller who CAN see the resource (e.g.
via a group share) tries to mutate it; without the gate, any
visible caller could re-home or rewrite the resource. The
reference is `handleUpdateConnectionCluster` in
`server/src/internal/api/connection_handlers.go` (PUT
`/api/v1/connections/{id}/cluster`), gated as part of the issue
`#233` follow-up audit. The order is: visibility check, admin gate,
decode, then the datastore mutation.

## Variant 2: Owner-Fallback Gate

Use this for per-object mutations where the row has an
`owner_username` column populated at creation time. A non-admin
caller who owns the row may still mutate it; everyone else needs
`PermManageConnections`. The reference is `updateClusterGroup` /
`deleteClusterGroup` in `cluster_handlers.go`:

```go
username, _, err := getUserInfoCompat(r, h.authStore)
if err != nil {
    RespondError(w, http.StatusUnauthorized,
        "Invalid or missing authentication token")
    return
}

hasManageConns := h.rbacChecker.HasAdminPermission(r.Context(),
    auth.PermManageConnections)

ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
defer cancel()

existing, err := h.datastore.Get<Resource>(ctx, id)
if err != nil {
    log.Printf("[ERROR] <resource> not found for <action> (id=%d): %v",
        id, err)
    RespondError(w, http.StatusNotFound, "<Resource> not found")
    return
}

isOwner := existing.OwnerUsername.Valid &&
    existing.OwnerUsername.String == username
if !hasManageConns && !isOwner {
    RespondError(w, http.StatusForbidden,
        "You do not have permission to <verb> this <resource>")
    return
}
```

`getUserInfoCompat` validates the bearer token against the auth
store, while `HasAdminPermission` reads the user ID from the request
context. Both must succeed: handler-level tests bypass the middleware
that normally populates the context, so callers must set BOTH the
`Authorization` header (via `withBearer`) AND the user context (via
`withUser`).

A 404 leak here is acceptable: callers who can authenticate but who
do not own the row and lack `PermManageConnections` cannot distinguish
"row exists but I can't touch it" from "row doesn't exist" because
the 404 happens before the 403 check. That matches the existing
issue-#35 visibility contract.

Note on handler comments: the Variant 2 implementation must call
`Get<Resource>` to read `owner_username` before it can decide
ownership, so the authorization check is NOT strictly "before any
datastore read". Describe the property as "authorization enforced
early (before request decoding and any response body write)" rather
than "before any datastore read". The OpenAPI description for a
Variant 2 endpoint must reflect the owner fallback, e.g. "Requires
manage_connections permission or ownership of the &lt;resource&gt;",
and the 403 response wording in the spec must match.

## Variant 3: Plain Admin Gate (visibility-stake reasoning)

Use this for per-object cluster mutations (`updateCluster`,
`deleteCluster`) where there is no `owner_username` column on the
underlying table. The `clusters` table (see
`collector/src/database/schema.go`) deliberately omits ownership;
cluster authorship is encoded through the cluster's member
connections, not directly on the cluster row.

The gate is the plain admin variant. The handler's existing
visibility check (`clusterHasVisibleConnection`) still returns 404
for callers who cannot see the cluster, so denied callers are
correctly hidden from the topology. The gate adds a separate
"non-admin callers cannot mutate" rule on top.

Do NOT attempt to fake ownership from visibility (e.g. "caller can
see at least one member connection"). Visibility includes connections
shared with the caller via groups; a shared-stake user is not the
cluster's owner and should not be granted mutation rights.

If a future schema change adds `owner_username` to the `clusters`
table, migrate these two handlers to Variant 2.

## System Alerts Have No Connection (Issue #582)

Alerts with `alert_type = 'system'` carry a NULL `connection_id`
(collector migration v19, CHECK `alerts_system_connection_check`), so
connection grants cannot govern them. The gate is
`RBACChecker.CanSeeSystemAlerts` in
`server/src/internal/auth/system_alerts.go`: it denies a nil checker,
a checker with no auth store (as every RBACChecker method does since
issue #477), an incomplete token context, a context with no user ID and
a token whose scope cannot be read, and grants every other
authenticated caller, whatever their connection scope.

Callers apply it through two filter flags on
`database.AlertListFilter`: `IncludeSystem` ORs system alerts into the
connection-restricted result, and `SystemOnly` returns system alerts
alone, which `handleAlerts` uses for a zero-grant caller instead of
the old early empty response (so such a caller now reaches the
datastore). `GetAlertCounts` takes an `includeSystem` bool and reports
system alerts under `system`, never in `by_server`. An explicit
`connection_id` or `connection_ids` query parameter always excludes
system alerts. Seeing a system alert and acting on one are separate
decisions. Acknowledge and unacknowledge go through
`AlertHandler.canActOnAlert`, which sends a nil connection to
`RBACChecker.CanManageSystemAlerts` (the same file; it is
`HasAdminPermission(ctx, PermManageAlertRules)` behind a nil-checker
guard, so a superuser or a `manage_alert_rules` holder passes, a
token's admin scope bounds it, and every lookup error denies) and
anything else to `CanAccessConnection`; `GetAlertConnectionID`
returns `*int` for this. `handleSaveAnalysis` never saves on a system
alert: it answers 403 when `CanSeeSystemAlerts` denies and 400
otherwise, because the refusal concerns the alert, not the caller. No
MCP tool mutates alerts, so there is no tool-side counterpart. The
`get_alert_history` MCP tool uses the same gate. The timeline count
queries in `timeline_queries.go` add `connection_id IS NOT NULL` and
leave system alerts out. The tests are `system_alerts_test.go` in
`internal/auth`, `system_alert_handlers_test.go` in `internal/api` and
`system_alerts_integration_test.go` in `internal/database`.

## Test Patterns

The regression tests live in
`server/src/internal/api/rbac_issue207_clusters_test.go`. Mirror them
when adding a new gated handler.

### Never use a nil auth store for a permissive checker

An `auth.RBACChecker` built on a nil `*auth.AuthStore` denies every
check, superuser context included (issue #477). A test that only needs
to get past the gate builds the checker on a real store and marks the
request superuser; in package `api` that is `newTestRBACChecker(t)` or
`newTestAuthStore(t)` (in `rbac_test_store_test.go`) plus
`withSuperuser(req)`. A request helper that marks its request superuser
must say so in its doc comment, and denial tests inline an
unauthenticated request instead of calling it.

### A nil checker denies; never skip a check because it is nil

A nil `*auth.RBACChecker` denies exactly as one on a nil store does:
every method guards `rc == nil || rc.authStore == nil`, so
`VisibleConnectionIDs` returns an empty set with `all == false` and
`CanAccessConnection`, `HasAdminPermission` and the rest return false
(issue #561). Call sites must therefore call the checker directly and
never wrap the check in `if rbacChecker != nil { ... }`, which turns a
missing checker into unrestricted access; handler constructors assign
`checkPermission = RequireAdminPermission(...)` unconditionally. Any
new method on `RBACChecker` needs the same nil-receiver guard. The
exceptions are `store_memory.go` and `query_database.go`, which test
for nil explicitly and deny. In the tools package, tests that need to
get past the check use `asSuperuser(Tool(..., testRBACChecker(t), ...))`
from `rbac_helpers_test.go`; nil-checker denial tests are in
`nil_rbac_test.go`.

The overview stream in `internal/overview/handler.go` uses the empty
scope key for the estate-wide feed, so a restricted caller must always
get a `connections:` key, even when their visible set is empty; an
empty set left as `""` would subscribe them to every connection.

### Tools: check access before existence, and answer both alike

An MCP tool taking an explicit `connection_id` must check access before
looking the connection up, and must give one answer for a missing ID, a
forbidden ID and an access check that fails; otherwise the response is
an existence oracle. `resolveAccessibleConnection` in
`internal/tools/connection_access.go` does this for `get_alert_history`,
`get_blackouts`, `get_metric_baselines` and `get_timeline_events`
(issue #571): it calls `CanAccessConnection` (which folds its own lookup
errors into a denial), then reads the name, and on any failure returns
"connection not found or not accessible" plus suggestions drawn only
from `VisibleConnectionIDs`. The message never echoes the requested ID,
so tests compare the missing and forbidden responses byte for byte
(`connection_access_test.go`). `query_metrics`, `get_alert_rules` and
the connection resolver use the same wording without suggestions.

### Denial test (no Postgres required)

```go
func TestHandler_FeatureName_Issue207_Denied(t *testing.T) {
    handler, store, cleanup := newIssue207Handler(t)
    defer cleanup()
    userID := newIssue207UnprivilegedUser(t, store, "issue207_feature")

    body, _ := json.Marshal(SomeRequest{...})
    req := httptest.NewRequest(http.MethodPost, "/api/v1/...",
        bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req = withUser(req, userID)
    rec := httptest.NewRecorder()

    handler.theHandler(rec, req, ...)

    assertForbiddenWithMessage(t, rec)
}
```

The handler is built with a nil datastore so the test asserts that
the 403 happens BEFORE any datastore call. A panic from a nil
datastore is a regression: it means the gate was placed after the
datastore call.

`assertForbiddenWithMessage` checks that the 403 response body
contains the canonical substring `"Permission denied"`, which is the
stable prefix of the plain admin gate (Variant 1/3). New plain-admin
handlers MUST emit the canonical wording
(`"Permission denied: requires manage_connections permission"`) so
the helper continues to lock in a regression to an empty or
differently-worded message. Variant 2 handlers use a different,
resource-specific wording (e.g.
`"You do not have permission to delete this cluster group"`) and
their tests should assert their own canonical substring inline
rather than reuse `assertForbiddenWithMessage`.

`assertGatePassed` rejects both `403 Forbidden` and `401
Unauthorized` so a regression where the auth lookup itself starts
denying a previously valid request also surfaces as a test failure.
Mirror the same shape in any inline "allowed caller" assertion:

```go
if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
    t.Fatalf("Permitted caller failed auth (status %d): %s",
        rec.Code, rec.Body.String())
}
```

### "Denied skips decode" test

For handlers that decode a JSON body, add a second denial test that
sends invalid JSON. Without the gate ordering rule the handler would
return 400 ("Invalid request body"); with the gate first it must
return 403. This catches a regression where someone moves the gate
below `DecodeJSONBody`.

### Admin-allowed sanity test (no Postgres required)

```go
func TestHandler_FeatureName_Issue207_AdminAllowed(t *testing.T) {
    _, store, cleanup := createTestRBACHandler(t)
    defer cleanup()
    userID := setupUserWithPermission(t, store, "issue207_admin",
        auth.PermManageConnections)
    checker := auth.NewRBACChecker(store)
    handler := NewClusterHandler(nil, store, checker)

    body, _ := json.Marshal(SomeRequest{...})
    req := httptest.NewRequest(http.MethodPost, "/api/v1/...",
        bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req = withUser(req, userID)
    rec := httptest.NewRecorder()

    assertGatePassed(t, rec, func() {
        handler.theHandler(rec, req, ...)
    })
}
```

`assertGatePassed` runs the handler with a deferred recover() so the
test asserts only that the gate did not return 403. Coverage of the
post-gate datastore code path lives in the integration tests gated on
`TEST_AI_WORKBENCH_SERVER`.

### Owner-fallback test (requires Postgres)

For Variant 2 handlers, add an integration test that:

1. Creates a fresh row owned by the caller (UPDATE
   `owner_username = $caller` if `CreateX` does not set it).
2. Sends the mutating request with `withBearer` AND `withUser`.
3. Asserts 200/204 (allowed), then verifies the row was actually
   mutated/deleted via a follow-up datastore query.

A second test should cover the denial case: a non-owner non-admin
caller, with the same row, gets 403 and the row is unchanged.

## Minimum Tests per Gate

The coverage floor in `CLAUDE.md` applies; cluster handlers lean on
the datastore, so most of the uplift comes from the integration
suite, whilst the gate itself is covered by the unit-style tests
above. When you add a gate, add at minimum:

- One "denied" unit test (negative path, no Postgres).
- One "admin allowed" unit test (gate passes, no Postgres).
- For Variant 2 handlers: one "owner allowed" integration test
  (Postgres-gated).

The denial test plus the gate body (5 statements) covers the new
lines; the admin-allowed test covers the not-taken branch.

## A Superuser's Token Is Bounded By Its Scope

Since GitHub issue `#471`, a superuser's API token no longer carries
its owner's superuser status unconditionally: each check in
`server/src/internal/auth/access.go` intersects the owner's superuser
rights with the acting token's scope for the surface being reached.
A superuser holds everything, so the intersection is simply the scope
itself, and a token scoped to one permission, one connection or one
tool may still use exactly that.

Two shapes, and the difference matters:

- A check that names the thing being reached
  (`HasAdminPermission`, `CanAccessConnection`, `CanAccessMCPItem`,
  `VisibleConnectionIDs`, `GetEffectivePrivileges`) reads the raw
  context flag `auth.IsSuperuserFromContext` and consults the scope of
  its own kind. The named thing is allowed when it is in scope, when
  the token has no scope of that kind, or when the scope holds the
  wildcard, and no group grant on the owning account is needed.
  Crucially, a narrowed scope of one kind must never narrow another
  kind: an admin-scoped token still reaches every connection and tool.
- A blanket gate, which names nothing and would hand over everything
  at once, goes through `RBACChecker.IsSuperuser`. That returns false
  when the token's admin scope has been narrowed, meaning it names
  specific permissions rather than being empty or holding `*`, because
  there is nothing to intersect against. `requireSuperuser` in
  `internal/api/rbac_handlers.go` is the blanket gate; the MCP listing
  filters are not, since they name each item and go through
  `CanAccessMCPItem` per item.

The apparent inconsistency, `IsSuperuser` false whilst
`HasAdminPermission` allows the scoped permission, is deliberate and
is documented on `IsSuperuser`; do not "fix" it.

A public MCP privilege is bounded the same way. `IsPrivilegePublic`
removes the need for a group grant, not the token's scope, so
`CanAccessMCPItem` intersects the public path with the MCP scope as
well: a token scoped to one tool cannot call every public tool, which
was issue `#482`. The listing paths follow from that. Neither
`ListForContext` in `internal/tools/context_aware_provider.go` nor the
one in `internal/resources/context_aware_registry.go` short-circuits on
`IsSuperuser` any more, because that flag comes from the admin scope
and says nothing about tools; every caller goes through the per-item
filter, which already admits sessions, unscoped tokens and wildcard
scopes (a nil auth store denies, as above), and `rbacExemptTools` is
still applied.

A token may change its own scope, under the same ceiling rule as any
other scope change (below): narrowing is allowed, widening or clearing
beyond its own access is refused. `refuseSelfScopeMutation` was
retired in favour of that rule (Dave's ruling of 06-10-2026).

A handler that authorises a connection by ownership or an admin
permission rather than `CanAccessConnection` (the Variant 2 gate on
`updateConnection` and `deleteConnection`) must also call
`RBACChecker.ConnectionInTokenScope`, before the row is loaded, since
neither ownership nor `manage_connections` says which connections a
token was issued for; it admits only a `read_write` scope entry,
because its callers mutate the connection. `SetTokenMCPScopeByNames`
returns `auth.ErrUnknownMCPPrivilege` for an unregistered identifier,
which the scope handler maps to 400, because silently dropping it
could store an empty, and therefore unrestricted, MCP scope. The
handler also runs `auth.ValidateScopedConnections` before writing any
scope kind, so a bad access level cannot leave a partial update.
Every `VisibleConnectionIDs` caller, `list_connections` included,
must return on error rather than skip filtering, and
`VisibleConnectionIDs` itself checks `TokenScopeError` before the
group wildcard return, since a failed scope read leaves that
wildcard in place.

Handlers gated on an admin permission that change something attached
to connections, namely blackouts, blackout schedules, alert, probe and
channel overrides and cluster writes, apply the connection scope
through the helpers in `internal/api/token_scope_targets.go`.
`requireTargetInTokenScope` admits a `server` target with an id when
that connection is in scope, and sends a cluster, group or estate
target (or a server target with no id) to
`RBACChecker.AllConnectionsInTokenScope`, which needs a scope covering
every connection because such a target reaches connections the token
does not name, including ones added later. Creating a cluster or a
cluster group, and any cluster definition or relationship change, go
through `requireAllConnectionsInTokenScope` (a new cluster, and any
connection later moved into it, inherits its group's settings; setting
or clearing a source's relationships deletes all of them first,
whatever the target), and adding or removing a server goes through
`requireConnectionsInTokenScope` for that connection. The cluster-group
create, update and delete handlers resolve the caller through
`getUserInfoCompat`, which accepts session tokens only, so an API
token gets 401 there after the scope gate; the gate is kept in case
API tokens are ever admitted. A write addressed by id (blackout or
schedule update and delete) checks the stored record, and a schedule
update checks the body as well, so a token cannot move a record into
or out of its scope; `requireBlackoutInTokenScope` skips the database
read when the token's scope covers everything. Blackouts and blackout
schedules are the exception to `read_write`: they go through
`requireBlackoutTargetInTokenScope` / `blackoutTargetInTokenScope`,
which use `RBACChecker.ConnectionReadableInTokenScope`, so a `read`
entry admits a server blackout and the wildcard at either level admits
a wider one. The user ruled (28-09-2026) that `read` means read-only
access to the monitored server, and blackouts, alert acknowledgement
and alert analysis are Workbench metadata; do not tighten these back
to `read_write`. A new handler of this kind must call one of these
helpers, and `token_scope_targets_test.go` and
`token_scope_blackout_test.go` hold the table-driven cases to extend.

Administrative grants and account changes made with an API token are
bounded by the token's effective access, its owner's privileges
narrowed by its scope, judged per connection and level, MCP item and
admin permission (Dave's ruling of 06-10-2026; sessions are bounded by
their admin permissions alone). Gate 1 is the route's existing admin
permission check; gate 2 is the ceiling. The one comparison lives in
`tokenCeiling` (`internal/auth/token_ceiling.go`, built by
`RBACChecker.actorCeiling`; unrelated to `applyTokenCeiling` in
`access.go`, which only clamps one connection level to the scope): `connectionLevel` uses
`CanAccessConnection`, so an unscoped token owned by a non-superuser is
bounded by its owner; `allConnectionsLevel` is the owner's
all-connections grant (read_write for a superuser) narrowed by the
token's all-connections entry; `coversReach` compares a whole
`principalReach`. The exported checks in `internal/auth/grant_scope.go`
are the only entry points; handlers call them through
`internal/api/rbac_grant_scope.go`, and each refusal goes through
`requireGrantInTokenScope` with a fixed `refuse*` reason saying what
exceeded "this token's access" (fixed so the denial audit coalesces).

- Connection grant (`CanGrantConnection`): the token must hold the
  connection at that level; read can grant only read. The
  all-connections entry needs every connection at that level.
- Connection revoke (`CanRevokeConnection`): read on the connection,
  refused identically whether hidden or missing (#574), plus read_write
  when `AuthStore.RevokeLiftsConnectionRestriction` says the grant is
  the connection's last (a group-less connection opens to every user).
  Accepted trade-off, not gated: a read-level token with
  `manage_permissions` may revoke another group's read_write.
- Group delete (`CanDeleteGroup`): read on every connection membership
  confers, plus the lift rule for each of the group's own grants.
  Member removal (`CanRemoveGroupMember`): read on every connection
  membership confers. MCP and admin revokes stay ungated.
- MCP grant (`CanGrantMCPItem`): the token must be able to call it;
  `*` needs an owner holding `*` and an unrestricted MCP scope.
- Admin grant (`CanGrantAdminPermission`): `coversReach` of a principal
  holding only that permission, so the token needs the permission,
  every connection at read_write, and, for `manage_users`,
  `manage_groups`, `manage_permissions`, `manage_token_scopes` or `*`
  (`reachEverythingAdminPermissions`), every MCP item and admin
  permission. `TestAdminPermissionGrantNeedsUnrestrictedToken` keeps
  Ant's 05-10-2026 chain refused.
- Membership add (`GroupWithinTokenScope`, ancestors included), user
  takeover (`UserWithinTokenScope`: password set, re-enable, delete,
  `createToken` for that owner), new users
  (`NewUserWithinTokenScope`): the principal's whole reach must fit.
- Superuser create or promote, a token for a superuser owner, and
  create, rename or delete of a group named in the OIDC `group_map`:
  `TokenHoldsEverything`, a superuser's token unrestricted in every
  kind (this also closed #522).
- Token scope set (`TokenScopeChangeWithinCeiling`, self included):
  only kinds present are judged; an entry the target does not already
  allow must be covered; narrowing an unrestricted kind is free; a
  connection entry kept, narrowed or dropped needs read on it.
  Clear (`TokenScopeClearWithinCeiling`): for each kind the target is
  restricted in, the owner's reach in that kind must fit. Both return
  `ErrTokenScopeUnreadable` (500 in the handler) when the stored scope
  or the owner's reach cannot be read.
- A user's reach counts group grants, public MCP items, every
  unrestricted connection the user can see, every connection their
  username owns (restricted or not, because `updateConnection` and
  `deleteConnection` admit the owner), and every member connection of
  a cluster group the username owns (`ownedClusterGroupsCovered`,
  through `auth.OwnedClusterGroupLister`), all at read_write.
  `RBACHandler.SetConnectionLister` must be wired; a nil lister, a
  lister error or one without the owned-group method fails closed.
- `manage_users`, `manage_groups`, `manage_permissions`,
  `manage_token_scopes` and `*` each let a holder acquire every MCP
  item and admin permission, so `principalReach.reachesEverything`
  makes a holder count as reaching everything in those kinds.
- An empty MCP name list is not proof of an unrestricted MCP scope:
  `GetTokenMCPScope` joins away orphaned rows, so `actorMCPScope` and
  `loadStoredTokenScope` ask `AuthStore.HasTokenMCPScope`.
- Every check fails closed on a nil checker, a nil store or a token
  context without an id.

Known gaps, deliberately left: `query_datastore` reaches beyond any
connection scope (#566), and editing or disabling a user and deleting a
token are not ceiling-gated. Tests: `internal/auth/token_ceiling_test.go`
and `internal/auth/grant_scope_test.go` (unit, table-driven),
`internal/api/rbac_token_ceiling_test.go`,
`internal/api/rbac_grant_scope_test.go`,
`internal/api/rbac_grant_reach_test.go` and
`internal/api/rbac_token_self_scope_test.go` (integration, real auth
store); each gate fails its test when reverted. Assert refusals with
`assertRefusedWith(t, rec, refuseX)`.

The scope PUT refuses an empty array for any kind with 400
(`emptyScopeKind` in `rbac_token_handlers.go`), because an empty kind
means unrestricted and the request almost always meant the opposite;
an omitted or null kind is left unchanged, and DELETE clears the whole
scope. The CLI still clears a kind given an empty list. The client's
`AdminTokenScopes.tsx` follows the same rule through `buildScopeBody`.

Everything fails closed: a scope lookup error denies (or, in
`VisibleConnectionIDs`, is an error rather than "everything"), an
API-token context carrying no token id (`tokenContextIncomplete`)
denies in `GetEffectivePrivileges` as in its five siblings, an
unreadable scope withdraws `IsSuperuser` from the report as well, and
`applyTokenCeiling` treats an access level it does not recognise as
read. Session callers carry no token id and are unaffected throughout.

`GetEffectivePrivileges` reports what the checks will actually allow:
`applySuperuserTokenScope` fills `TokenScope`, `TokenScopeError` and
the connection, MCP and admin maps from the token's scope, instead of
returning empty maps that every consumer reads as unrestricted. A kind
the token does not scope, or scopes with a wildcard, leaves its map
empty.

The audit endpoint needs no gate of its own: `requireUnscopedTokenForAudit`
was retired in the same change because `requireSuperuser` refuses
exactly the tokens it refused. `auth.IsSuperuserFromContext` itself is
unchanged and remains the raw context accessor, reported as such by
`cmd/mcp-server/handlers.go`.

The rules are pinned by
`server/src/internal/auth/access_superuser_scope_test.go` at the
checker level and by
`server/src/internal/api/rbac_issue471_test.go` plus
`server/src/internal/api/rbac_audit_gate_test.go` at the HTTP
boundary.

## Denial Auditing in the RBAC Management Handlers

The `/api/v1/rbac/*` handlers do not inline the gate. They call the
shared helpers `requirePermission` and `requireSuperuser` in
`server/src/internal/api/rbac_handlers.go`. Since GitHub issue `#65`
these helpers also write a `denied` row to the audit log before
responding 403.
The action name comes from `deniedAction(r)`, which maps the request
method and path to the same dotted action the change would have
recorded had it been allowed (`group.delete`, `token.scope.set`,
`permission.admin.grant` and so on), falling back to
`rbac.<lowercase method>` for an unrecognised route shape. A recording
failure is logged with `[ERROR]` and never changes the response.

Denials are coalesced before they reach the store. `recordDenial`
consults `admitDenial`, which keeps an in-memory map on `RBACHandler`
keyed by `denialKey` (actor type, actor id, actor name, client IP,
action, capped at `maxDenialActionLen`, and reason) under `denialMu`,
so that two tokens of one user, or one token used from two addresses,
never suppress each other's denials. The key must never hold a value
the caller chooses without limit: the path is deliberately not in it,
and `deniedRoute` maps an unrecognised method to `rbac.other` (only
the nine standard methods get `rbac.<lowercase method>`), since
either would let a client mint a fresh key, and so a fresh row, per
request. The target instead goes in the row: `deniedRoute` normalises
it to `kind/<id>` (a positive integer id) or `kind`, and
`unmatchedDenialTarget` for a route it does not recognise. The first
denial for a key is written at once with `details.target`, identical
denials within `denialCoalesceWindow` (60s) are counted in a
`denialWindow`, which keeps up to `maxDenialTargets` (20) distinct
targets and counts the rest in `targets_truncated`, and the first
denial after the window closes is written through
`auth.RecordDeniedWithDetails` carrying `repeat_count`, `targets` and
`targets_truncated`. Entries sit on an intrusive list ordered by
`firstSeen` (`denialOldest`/`denialNewest`, kept by `pushNewestDenial`
and `unlinkDenial`; not `container/list`, whose element type
assertions errcheck flags), so `evictDenials` walks from the oldest
end and stops at the first open window, which keeps each call
amortised O(1) however many keys are held. The map is capped at
`maxDenialKeys` (10 000); an evicted entry that still held suppressed
repeats is returned as a `denialSummary` and written by
`recordDenialSummaries` once `denialMu` is released, as a row carrying
`window_closed` plus the window's fields, so a burst that stops
before its window closes is still counted. Any new denial path must
go through `recordDenial` rather than calling `RecordDenied`
directly, or it loses the bound on how many rows one client can
append. A handler outside `RBACHandler` records through
`RBACHandler.RecordDenial`, injected as a function:
`ConnectionHandler` takes it via `SetDenialRecorder` and refuses
through `refuseOutOfTokenScope` (its `manage_connections` refusal on
`PUT /connections/{id}/cluster` records too), and `ClusterHandler`,
`AlertRuleHandler` and `NotificationChannelHandler` embed
`denialAuditor`, whose recorder they pass to
`requireAllConnectionsInTokenScope` and
`requireConnectionsInTokenScope`; `cmd/mcp-server/handlers.go` wires
all four. `requireTargetInTokenScope` and
`requireBlackoutTargetInTokenScope` do not record yet.
`deniedConnectionAction` names the `connection.update`,
`connection.delete` and `connection.cluster.update` actions, and
`deniedAPIRoute` names `cluster.*`, `cluster_group.*`,
`notification_channel.*` and `alert_rule.*` (a write to a
sub-resource is recorded as an update of its parent).
`updateConnection` and `deleteConnection` call the session-only
`getUserInfoCompat` first, so a real token gets 401 there; only the
cluster move reaches an audited connection token-scope refusal, and
the other two gates are defensive, so do not document them as
reachable.

Mutations in these handlers go through `h.actorStore(r)` rather than
`h.authStore`, so the audit row names the acting user or token:
`actorStore` wraps `auth.ActorFromContext(r.Context())`, which reads
the username, user or token id and client IP that
`auth.AuthenticateRequest` and `createAuthWrapper` place in the
request context. The token id is `auth.TokenIDContextKey`, the same
key `RBACChecker` reads to enforce token scope; there is deliberately
no attribution-only key, so a token is named in the log exactly when
its scope is enforced, and `RBACChecker.IsSuperuser` refuses an
API-token context that carries no id rather than passing it. The
composition is pinned by
`server/src/internal/api/rbac_token_scope_regression_test.go`.
Read-only calls stay on `h.authStore`. When adding a
new mutating RBAC endpoint, use `h.actorStore(r)` and extend the
`deniedAction` mapping in the same change; the wiring is locked in by
`server/src/internal/api/rbac_audit_wiring_test.go`.

## Superuser Status Needs a Superuser

`manage_users` is grantable to a group, so it must not be a route to
superuser. `createUser` and `updateUser` in `rbac_user_handlers.go`
call `requireSuperuser` right after decoding whenever the body carries
`is_superuser` at all, true or false, before any store read (#497). The
field's presence, not whether it would change the row, is the test, so
the rule never depends on target state read outside the transaction.
`updateUser` and `deleteUser` also call `requireSuperuser` when the
target account is a superuser, since resetting its password or
disabling it is as good as holding the role. Pinned by
`rbac_user_superuser_gate_test.go`. Any new endpoint that can change
superuser status, or write to a superuser account, needs the same gate.

## Store Refusals Map to 400

The auth store marks a refusal that is the caller's to fix by wrapping
it in `*auth.InvalidInputError` (via `invalidInput` in
`auth/invalid_input.go`), which matches `auth.ErrInvalidInput` under
`errors.Is`. The user handlers pass every store error to
`respondUserStoreError`, which answers such an error with a 400
carrying its message and anything else with a logged 500 and a fixed
message (#485). Put a new store-level validation rule in the store and
mark it there rather than duplicating it in a handler; the federated
account password refusal in `writePasswordHashLocked` is the model.

## Audit Writes Are Fail-Closed, With One Exception

Every audited mutation in `server/src/internal/auth` writes its event
in the same transaction as the change, so a failure to record leaves
the change unapplied: `insertUserAudited`, `setUserSuperuser` and the
group, token and privilege mutations all call `recordAudit(tx, ev)`
before their `tx.Commit()`, and roll back through `failAudit` when
anything on the way fails. Keep new mutations on that shape.

The sole exception is `disableForLockout` in `actor_store_users.go`.
That change is the server locking an account against repeated failed
sign-ins rather than a change an operator asked for, so refusing it on
an audit failure would hand the attacker a working account. It calls
`commitLockout`, which commits the `UPDATE users SET enabled = FALSE`
on its own, and only then writes the `user.disable` event through
`recordAuditInOwnTx`, logging a failure rather than returning it.
`TestLockoutSurvivesAuditFailure` in `audit_users_test.go` and the
`NoAuditTable` case of `TestAuditTokensLockoutHelperErrorPaths` pin
both halves. Do not "fix" this back into one transaction.

Two schema-level invariants back the chain, both in `auditSchemaDDL`
in `audit.go`. `idx_audit_prev_hash` is a unique index, so no two
events can name the same predecessor and only one event can be the
genesis row with an empty `prev_hash`; a test fixture that inserts
`audit_events` rows by hand must chain them rather than reuse a
placeholder. And `auditCanonical` length-prefixes each field rather
than joining with a separator, so a value containing the separator
cannot render identically to a different pair of columns.

The audit DDL is not gated on the schema version. `initSchema` in
`store.go` ends by calling `ensureAuditSchema`, which re-runs the
`IF NOT EXISTS` table, trigger and unique-index statements on every
open; the version gate alone left a database stamped with the current
version but created before the index existed without it for good.
`VerifyAuditChain` asserts through `verifyAuditSchema` the definitions,
not just the names, before it reads a row: `verifyAuditChainIndex`
requires `pragma_index_list` to report the index unique and not
partial, and `pragma_index_info` to report exactly one key column,
`prev_hash` (an expression column has a NULL name, so it fails too);
`verifyAuditNoUpdateTrigger` refuses any other trigger on
`audit_events` or whose SQL mentions it (a `RAISE(IGNORE)` trigger
would drop chosen events with the chain intact; `recordAudit` also
fails unless its INSERT wrote one row), and compares the trigger's
`sqlite_master.sql` against `expectedAuditTriggerSQL()`, which is
`auditNoUpdateTriggerDDL` with whitespace collapsed, `IF NOT EXISTS`
removed and the trailing
`;` trimmed, because SQLite stores the statement minus those two. So
changing `auditNoUpdateTriggerDDL` changes what verification accepts;
an existing database keeps its old trigger text, since the DDL is
`IF NOT EXISTS`, so a change there also needs a migration that drops
and re-creates the trigger. `TestVerifyAuditSchemaChecksDefinitions`
in `audit_head_test.go` holds the same-named impostor cases.
`TestReopenRestoresAuditSchemaObjects` and
`TestReopenRefusesForkedChain` in `audit_test.go` go through
`NewAuthStore` rather than calling a migration directly; keep any new
schema test on that path, because a test that calls the migration
skips the gate it is meant to check.

Each row stores the rendering its hash was computed under in
`hash_version` (schema v6, `migrateV5ToV6`; the audit table itself
arrived in v5, after the federation columns of v4), and `auditHash`
dispatches on `ev.HashVersion`, returning `errUnknownAuditHashVersion`
for a version it has no case for. Changing the rendering is not a
free, additive act, whatever the dispatch suggests: version 2 made
every version 1 row unverifiable and forced the re-chain upgrade step.
A row naming a version `auditHash` will not compute is reported as
tampering, wrapped in `ErrAuditChainBroken` by `VerifyAuditChain`
(the error branch after its `auditHash` call), deliberately, because
the version is a column
in the file and relabelling a row must not move it out of the
verifier's reach.

`auditHashVersion` is 2: an HMAC-SHA256 over the canonical rendering,
keyed by `AuthStore.auditKey`, so rewriting a row needs the server
secret as well as write access to `auth.db`. Version 1, the unkeyed
SHA-256, is no longer admissible anywhere: `auditHash` refuses to
compute it and returns `ErrAuditUnkeyedRow`, and the only code that
still computes it is `verifyLegacyAuditChain`, reporting on a log the
re-chain is about to replace. The key comes from
`auth.DeriveAuditKey(serverSecret)`, PBKDF2-HMAC-SHA256 through
`pkg/crypto.DeriveKey` under the fixed salt
`pgedge-ai-workbench/audit-hash-chain/v1`, which must never change and
must never be reused by another subsystem.

`NewAuthStore` takes the key as its fourth argument and refuses one
shorter than `minAuditKeyBytes` (32), which is what `DeriveAuditKey`
produces. Both paths that open the store supply it: the server derives
it in `initAuthStore`, which is why `NewServer` loads the server secret
before it opens the store rather than after, and the command line
derives it in `resolveCLIAuditKey`, which reads the secret through the
shared `loadServerSecretFile` in `server.go` and fails the command
outright when there is none, rather than writing an unkeyed row.
`main` reads `secret_file` ahead of the full configuration load with
`config.LoadConfigSecretFile`, the same trick `LoadConfigDataDir`
plays, and hands it to `RunCLICommands`; a configuration file that
cannot be read is fatal there, because the default search order would
otherwise pick a different secret, or a different `auth.db`.

The version number is inside the digest (`auditCanonical` renders
`ev.HashVersion` straight after the label), so a version 2 row cannot
be relabelled as version 1 and recomputed unkeyed. `VerifyAuditChain`
also refuses a version that falls as the chain advances
(`ErrAuditChainDowngraded`).

Behind both, `ensureNoUnkeyedAuditRows` runs at the end of every
`initSchema` and refuses to open a database holding any version 1 row,
naming `-rechain-audit-log` and saying that deleting `auth.db` is not
the remedy. It counts rows rather than reading `schema_version`, which
is a value inside the file and so writable by whoever can write the
log.

Both that gate and `auditRechainPlan` first call
`checkNoKeyedAuditRows`, which counts keyed rows and refuses, wrapping
`ErrAuditUnkeyedRow`, when the log holds any at all whilst unkeyed rows
remain. The re-chain serves exactly one database shape, a log inherited
wholly from a pre-key release, which holds unkeyed rows and nothing
else; a log mixing the two renderings had its unkeyed rows written by
something with write access to `auth.db`, and re-chaining would sign
them. Do not reintroduce an ordering test here: `id` is `INTEGER
PRIMARY KEY AUTOINCREMENT`, which governs only the values SQLite
assigns, so an explicit `INSERT` naming id 0 or -1 is accepted and
sorts below every genuine row, which is how VULN-307 defeated the
prefix check this replaced. The two refusals are worded differently on
purpose, so that the start-up message for the mixed case points at a
restore rather than at a command that will refuse it in turn.
`rechainAuditLogCommand` also refuses a `-confirm-rechain` run when
`plan.LegacyChainOK` is false, leaving the interactive path free to
proceed on a human's judgement.
`verifyLegacyAuditChain` carries the same `ev.HashVersion <
highestVersion` check `VerifyAuditChain` has, so `LegacyChainOK` cannot
be true for a downgraded log.

`auth.RechainAuditLog` is the one way past that gate, and the
`-rechain-audit-log` subcommand is its only caller. It opens through
the unexported `newAuthStore(..., allowUnkeyedAuditRows: true)`, builds
an `AuditRechainPlan` (row count, span, unkeyed count, and whether the
log recomputes under `verifyLegacyAuditChain`), passes it to an
`AuditRechainConfirm` callback that must return true, and then
`rechainAuditLogTx` re-hashes every row as version 2 in one
transaction, re-linking each `prev_hash` to the new hash of the row
before it, keeping the first row's `prev_hash` as given (which only
`TestRechainKeepsThePurgedPrefixLink` can see, because every other
fixture starts from a genesis row whose `prev_hash` is empty), and
appending
an `audit.rechain` event. It drops the append-only trigger and
re-creates it inside that transaction; SQLite makes DDL transactional,
`_txlock=immediate` holds the write lock from BEGIN and WAL readers see
the last committed snapshot, so no other connection observes the table
without the trigger and a rollback restores rows and trigger together.
The walk pages through `forEachAuditEvent` because a statement executed
on the connection an open query is streaming from would deadlock.

`rechainAuditLogTx` opens with `checkAuditRechainPlanStillHolds`, which
re-reads `COUNT(*)`, `MIN(id)` and `MAX(id)` under the transaction's
write lock and returns `ErrAuditRechainChanged` if any differs from the
plan the operator approved, because the confirmation prompt may have
waited whilst another writer appended rows. It is an id-range check
rather than a second full verification, which would double the work
without settling anything the range does not.

There is deliberately no boundary machinery between inherited and keyed
rows, and none should be added. Three successive designs kept a signed
watermark, and each gave the server a way to sign a forgery; the last
let the retention purge re-anchor the boundary onto a backdated row, so
an attacker who could write `auth.db` had the server attest a
fabricated history under the real key. `PurgeAuditEvents` is a
delete plus its own keyed event and writes no hash over a row it did
not create. Treat any proposal that has an unattended path re-sign
existing rows as that bug returning;
`TestPurgeDoesNotReanchorAForgedPrefix` in `audit_rechain_test.go` is
the regression test.

Head deletion (#502) is handled in `audit_head.go` without re-signing
anything. The purge event's details carry `oldest_retained_id` and
`oldest_retained_hash`, the existing hash of the row it left as the
oldest, as a value inside an event the purge itself creates; the
verifier still recomputes that row like any other. `auditHeadCheck`
(fed by `verifyAuditLog` only with rows that verified) requires the
oldest row's hash to equal the newest purge's record, and, when no
purge records one, reports a non-empty leading `prev_hash` only if no
`audit.purge` row survives at all (the weak fallback for purge events
written before the record). With no head record, an oldest row with
`prev_hash` "" must be id 1 or an `audit.purge` row
(`auditGenesisAllowed`): AUTOINCREMENT never reuses ids, so a new chain
above 1 is an emptied table (unless someone reinserted the row at id 1:
ids are not hashed). A rechain row is not admitted, because a re-anchor
refuses an empty log (`auditReanchorPlan`), so a replayed re-anchor
event cannot start a chain, and a re-hash appends after its rows. Tests
that empty `audit_events` by hand must expect `ErrAuditChainBroken`.
The record is worthless unless the purge
cannot be steered into writing a new one over a deletion, so before
deleting, `verifyAuditPurgePrefix` checks, in the purge transaction,
that the rows up to and including the new head start at the previous
purge's recorded hash (or at a genesis row with `prev_hash` "" that
`auditGenesisAllowed` admits when no purge exists; anywhere only when
no purge event records a head, since a
record-less purge never overrides an older recorded one, in either the
purge or `auditHeadCheck`, or a replayed pre-record event would reopen
the weak case), that
each verifies under the key (`checkAuditRowVerifies`) and that each
links to the one before. Any failure refuses the purge and deletes
nothing, which stalls retention by design; the refusal wraps
`ErrAuditChainBroken` and starts `errAuditPurgeRefused`. The cases,
including the one-INSERT laundering attempt, are in
`audit_head_test.go`.

The purge deletes a contiguous id-prefix and nothing else: it reads the
cut in the transaction, verifies the prefix, then deletes below it.

```go
SELECT MIN(id) FROM audit_events WHERE occurred_at >= ?  -- cut
DELETE FROM audit_events WHERE id < ?                     -- cut
```

`occurred_at` is a column an attacker who can write `auth.db` chooses;
`id` is insertion order, assigned by SQLite on every server insert and
fixed on existing rows by the append-only trigger; a writer can name
an `id` on INSERT, but that only moves the boundary earlier. Deleting on
`occurred_at` alone let backdated rows steer the purge into removing
the newest events, which relinked the chain and, through the appended
`audit.purge` event, put `MAX(id)` back into agreement with
`sqlite_sequence`, the disagreement `verifyAuditTail` exists to read.
When no row falls inside the window `MIN(id)` is NULL and
nothing is deleted, which is deliberate: emptying the log instead would
restore the same oracle, and a fixed cut-off such as 0 would delete
rows inserted at negative ids. `forEachAuditEvent` likewise starts its
first page with no lower bound on `id`, and `rechainAuditLogTx`
refuses to commit when the rows it reached differ from the plan's
count. `TestPurgeIgnoresBackdatedNewestRows` in
`audit_rechain_guard_test.go` covers both halves, the refusal and an
honest prefix still being purged.

`verifyAuditLog` (`audit_verify.go`; `VerifyAuditChain` is a thin
wrapper over `VerifyAuditLog`) separates a wrong key from tampering,
because an operator's response differs. A row whose HMAC does not
recompute is classified by `classifyUnverifiedRow`: only when no row
outside the history has yet verified (`keyProven`), no verified anchor
recording a head was found (`anchorFound`, since such an anchor
postdates any rotation; the purge's `auditPurgeUnverified` takes the
same flag), and `looksLikeKeyChange` finds a run of failing rows,
each linked to the
one before, followed by the end of the log or by rows that all verify
and link (it walks the whole log, then requires `verifyAuditTail`),
is it `ErrAuditKeyMismatch` (CLI exit 3,
message names `-rechain-audit-log`); anything else is
`ErrAuditChainBroken`, and `ErrAuditChainDowngraded` or
`ErrAuditUnkeyedRow` map to exit 2 too, in `describeAuditVerifyFailure`
(`cmd/mcp-server/audit.go`). Anything else stays 1. The purge's
repeating log line comes from `auditPurgeFailureMessage` in
`cmd/mcp-server/server.go` and names both commands for either sentinel.

The re-anchor (`audit_reanchor.go`) is the recovery for a keyed log the
purge refuses. `RechainAuditLog` picks it when the log holds no unkeyed
rows; a log that verifies returns `UpToDate` without calling `confirm`.
It rewrites nothing: `reanchorAuditLogTx` re-checks the plan under the
write lock, rescans (`scanAuditForReanchor`, compared with the plan's
scan so an in-place replacement is caught), and appends one keyed
`audit.rechain` event carrying `oldest_retained_id`/`_hash`, and, when
rows fail, `history_through_id`, `history_events` and
`history_digest`, an unkeyed SHA-256 over every row through the last
that fails to verify or link (`auditHistoryDigest`). Purge and rechain
events are both anchors (`isAuditAnchorAction`); `findAuditAnchor`
takes the newest that records a head and requires it to verify, and a
record-less anchor never overrides. History rows skip the key and link
checks in the verifier and in `verifyAuditPurgePrefix` but must match
the digest and count, and a purge keeping some history carries the
digest of the survivors forward. `checkAuditAnchorLinks` is the purge's
anti-replay check: the row before the anchor must match its
`prev_hash` and the row after must name its hash, so a re-inserted old
anchor cannot take over. The CLI (`confirmAuditReanchor`) lets
`-confirm-rechain` proceed only on `KeyMismatch`, which
`auditReanchorPlan` clears when the scan's `failsAfterVerified` shows a
failing row after one beyond the existing history verified. Tests are
in `audit_reanchor_test.go`, `audit_reanchor_scope_test.go` and
`cmd/mcp-server/reanchor_audit_test.go`.

A successful `GET /api/v1/rbac/audit` is not written to `audit_events`,
because reading is not a change; `handleAudit` logs one `[AUDIT]` line
naming the actor, the filters in effect (`describeAuditFilter`, which
caps each value at `auditFilterValueMax` runes) and the row count
instead.

Tail truncation is caught by `verifyAuditTail`, which compares
`MAX(id)` against the `sqlite_sequence` row. Deleting that row whilst
events remain is itself reported, because SQLite writes it with the
first insert and never removes it, and so is a database with no
`sqlite_sequence` table at all, since every table this store creates
is `AUTOINCREMENT`. `MAX(id)` and the sequence are read by a single
statement, which SQLite evaluates against one snapshot, so a server
insert whilst the CLI (which opens its own store on the same file) is
verifying cannot leave the sequence a step ahead of the newest id; the
pure comparison is `checkAuditTail`. Every disagreement wraps
`ErrAuditChainBroken`, so the CLI exits with the tampering status; any
other query error is returned unwrapped rather than swallowed, and
exits 1. `sqlite_sequence` itself is unprotected, so a tail deleted and
then matched by writing the sequence down passes without the secret.
