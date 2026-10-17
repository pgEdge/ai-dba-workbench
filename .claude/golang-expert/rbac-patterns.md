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

A Variant 1 gate is not a visibility check: a restricted caller holding
`manage_connections` must still see what they mutate. The cluster
relationship routes (issue #678) are the reference. After the admin
gate and the token-scope gate, and before `DecodeJSONBody`,
`restrictedVisibleSet` resolves the visible set (nil means
unrestricted) and `visibleSourceInCluster` answers 404 for a hidden
source and 400 for a source outside the addressed cluster (checked
before any existing row is considered, since a moved connection keeps
stale rows in its old cluster). The visible set is then passed to
`SetNodeRelationships` / `ClearNodeRelationships` as `permitted`; the
delete checks the targets in its own `RETURNING` list and rolls back
with `database.ErrHiddenRelationship` if one is hidden, so a hidden row
written concurrently cannot be lost (a pre-check followed by a separate
delete would race). `refuseHiddenRelationship` turns that error into a
403 through `h.denialRecorder.refuse`, so it is audited as
`cluster.update clusters/{id}` like the token-scope refusal on the same
routes; any new refusal on an audited handler must go through `refuse`,
not `RespondError`. Auto-detected rows are never deleted, so they never
cause the 403 (`TestIssue678_AutoDetectedHiddenRowDoesNotBlock`). A
hidden requested target gets the
same 400 as a non-member, a delete by id answers 404 unless the row is
in the addressed cluster with both ends visible, and every response
listing relationships goes through `filterRelationshipsByVisibility`
(both ends visible), including `GET /connections/{id}/cluster`, whose
cluster selector list goes through `filterClustersByVisibility` as
`GET /clusters/list` does. Writes that add a row to a connection other than
the source use the insert-only `AddNodeRelationship`, never the
replacing `SetNodeRelationships`, so they cannot delete hidden rows.

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
`deleteClusterGroup` in `cluster_handlers.go`, whose order is: caller
authentication, the caller-only token-scope check, the visibility
load, the lookup, the visibility decision, then the ownership check:

```go
username, _, err := getUserInfoCompat(r)
if err != nil {
    RespondError(w, http.StatusUnauthorized,
        "Invalid or missing authentication token")
    return
}

hasManageConns := h.rbacChecker.HasAdminPermission(r.Context(),
    auth.PermManageConnections)

// Depends only on the caller, so it runs before the lookup and its
// refusal says nothing about whether the row exists.
if !requireAllConnectionsInTokenScope(w, r, h.rbacChecker,
    h.denialRecorder) {
    return
}

ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
defer cancel()

// Runs the same queries whether or not the row exists.
v, ok := h.loadClusterGroupVisibility(ctx, w, id,
    "Failed to <verb> <resource>")
if !ok {
    return
}

existing, err := h.datastore.Get<Resource>(ctx, id)
if err != nil {
    RespondError(w, http.StatusNotFound, "<Resource> not found")
    return
}

isOwner := ownsClusterGroup(username, existing)
// 404 for a row the caller cannot see, identical to a missing one.
if v.hidden(isOwner) {
    RespondError(w, http.StatusNotFound, "<Resource> not found")
    return
}
if !hasManageConns && !isOwner {
    h.denialRecorder.refuse(w, r,
        "You do not have permission to <verb> this <resource>")
    return
}
```

`getUserInfoCompat` reads the username from the request context that
`auth.AuthenticateRequest` built, so a session and an API token are
accepted alike (issue #668; it used to validate the bearer as a
session token and answered 401 to every API token). Handler-level
tests bypass the middleware, so build the context the way it does
with `withAuth(t, store, req, rawToken)` in `cluster_handlers_test.go`,
which takes a session token or a raw API token; apply context helpers
such as `withSuperuser` after it. `TestConnectionLifecycleForEachCaller`
runs create, update and delete as both kinds of caller.

Visibility MUST come before ownership (issue #649): a caller who
cannot see the row gets the same 404 body as for a missing row, and
no refusal is recorded for it; only a visible row the caller does
not own draws the audited 403. `loadClusterGroupVisibility` resolves
the caller's visible connections and, for a restricted caller, the
group's members before the lookup, so a missing id (no members) and
a hidden group run the same queries; `getClusterGroup` and
`listClustersInGroup` use it in the same order, and both look the
group up before deciding, so a missing id is 404 for every caller.

One rule governs a group with no member connections on every route
(issue #705): it is visible to its owner and to callers who see every
connection, and to nobody else, whatever permissions they hold.
`hidden(isOwner)` takes only ownership; never pass `hasManageConns`
into it, which is what let a restricted `manage_connections` holder
rename another user's empty group. The writes match the owner on
`getUserInfoCompat`'s username (`ownsClusterGroup`). The reads
(`getClusterGroup`, `listClustersInGroup`, and `listClusterGroups`
through `filterGroupsByVisibilityFn`'s `owner` argument) use
`emptyGroupOwnerName`, the context username, or "" for an API token
narrowed to some connections, so a narrowed token never counts as its
owner; the writes refuse such a token before the lookup instead.
`requireVisibleTargetGroup` applies the same `hidden` rule.
`TestClusterGroupEmptyGroupOneRule`,
`TestClusterGroupListShowsOwnEmptyGroups`,
`TestClusterGroupWritesHideInvisibleGroups` and
`TestClusterGroupLookupsCostTheSameForHiddenAndMissing` in
`cluster_group_visibility_test.go` lock this in; the last counts
pool acquisitions through a `PrepareConn` hook.

Connection update and delete (`updateConnection`, `deleteConnection`)
follow the issue #677 model instead, through `loadConnectionForChange`:
token scope first (audited 403), then a 404 "Connection not found",
unaudited and identical to a missing id, when the caller cannot see
the connection (`CanAccessConnection`), whether or not it holds
`manage_connections` (issue #710, matching the cluster group handlers
of issue #649); only then the ownership check, which
`manage_connections` passes and whose 403 is audited through
`h.refuse`. A superuser sees every live connection, so is unaffected.
`CanAccessConnection` runs whether or not the row exists, so the work
done does not reveal it either. For that
to hold, `CanAccessConnection` reads the caller's group privileges
before the sharing lookup, which fails for a missing id; keep that
order, so a missing and a hidden connection run the same queries.
`TestCanAccessConnectionReadsPrivilegesBeforeSharingLookup` in
`internal/auth/access_order_test.go` locks in the order (a failed
privileges read must stop before the sharing lookup runs), since the
handler tests compare only status and body and pass with either order.
`connection_write_visibility_test.go` locks in the byte-identical 404
for sessions, API tokens and `manage_connections` holders, a manager
changing a connection it can see, the superuser, and the owner of a
group-restricted connection.

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

## Cluster Writes Check What They Point At (Issues #673, #708)

The admin gate is not enough on writes that place a cluster; the
caller must also see the target, and a hidden object answers exactly
as a missing one. The helpers live in
`server/src/internal/api/cluster_write_visibility.go`:

- `requireVisibleTargetGroup` runs before a cluster is created in, or
  moved into, a group (`createClusterInGroup`, `handleCreateCluster`,
  `updateCluster`). A missing or hidden group is 404 "Cluster group not
  found", and both paths run the same queries. An empty group is
  visible only to its owner and to callers who see every connection,
  through the same `clusterGroupVisibility.hidden` rule as the
  cluster-group routes (issue #705). The owner match uses
  `GetUsernameFromContext`, which for an API token is the token
  owner's name; a narrowed token never gets that far only because
  `requireAllConnectionsInTokenScope` runs first.
- The default group (`is_default`) is always writable: a cluster with
  no `group_id` lands there unchecked, and an explicit default
  `group_id` passes `requireVisibleTargetGroup` too. Requiring
  visibility would leave a restricted caller unable to create any
  cluster.
- `requireVisibleAutoDetectedCluster` guards PUT and DELETE on the
  transient IDs. `server-{id}` is visible when connection `id` is, and
  a supplied `auto_cluster_key` must be `binary:`, `standalone:` or
  `logical:` plus that id. `cluster-spock-{prefix}` is visible when a
  connection assigned to the `spock:{prefix}` row is
  (`GetConnectionIDsForAutoClusterKey`, which reads the persisted
  `connections.cluster_id`, so a Spock cluster with no row yet looks
  hidden). Hidden is 404 "Cluster not found"; a PUT `group_id` then
  goes through `requireVisibleTargetGroup`.

Unrestricted callers skip all of this apart from the group lookup.
The relationship routes are covered under "When To Gate" (issue #678).
Tests: `cluster_write_visibility_test.go` and
`cluster_write_visibility_auto_test.go`.

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
returns `*int` for this. All three mutations first run
`AlertHandler.resolveVisibleAlert`, which answers a system alert that
`CanSeeSystemAlerts` denies, or a connection alert outside
`VisibleConnectionIDs`, with the same 404 as a missing alert (#677);
only a visible alert reaches `canActOnAlert`, whose 403 is audited
through the embedded `denialAuditor`. `handleSaveAnalysis` never saves
on a visible system alert and answers 400, because the refusal
concerns the alert, not the caller. No
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
`checkPermission = requireAdminPermission(rbacChecker, &h.denialAuditor,
auth.PermX)` unconditionally. Any
new method on `RBACChecker` needs the same nil-receiver guard. The
exception is `store_memory.go`, which tests for nil explicitly and
denies. In the tools package, tests that need to
get past the check use `asSuperuser(Tool(..., testRBACChecker(t), ...))`
from `rbac_helpers_test.go`; nil-checker denial tests are in
`nil_rbac_test.go`.

### A marked connection is refused even to a superuser

`CanAccessConnection`'s superuser bypass (`internal/auth/access.go`)
still calls the checker's connection sharing lookup, which finds
neither a missing connection nor one marked for deletion, so a
superuser gets the same 403 as anyone else between a delete and its
purge (issue #669). A checker built without a sharing lookup cannot
make that test, so a handler that reads a connection's data must build
its checker with one: the metric, latest-snapshot and
performance-summary handlers go through `metricAccessChecker`, which
uses the handler's `sharingLookupFn` when a test sets one and the
datastore's lookup otherwise. Database-free handler tests set
`sharingLookupFn: liveConnectionLookup` (`metrics_handlers_test.go`).
Because the lookup refuses a missing connection first, a connection
handler's own not-found branch after the access check is reachable only
when the purge races the check; `connCovFixture.racingPurge`
(`connection_handlers_rbac_coverage_test.go`) simulates that by
swapping in a checker without the lookup.

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

REST reads of a record tied to a server, cluster or group reuse
`scopeVisibleToCaller` (`internal/api/rbac_visibility.go`) with the
record's own not-found message, so a hidden target answers 404 exactly
as a missing one; it applies the token scope through
`VisibleConnectionIDs`. A route addressed by connection ID calls it
before the query (probe override lists, `?connection_id=` on
`/api/v1/probe-configs`); a route addressed by record ID loads the
record first and then checks its target (`getProbeConfig`, via
`probeConfigVisibilityTarget`, issue #647). An admin-gated write on such
a record runs the same check after the permission gate and before the
token-scope gate and the body decode, so neither a 403 nor the echoed
record confirms a hidden one exists (`updateProbeConfig`, via
`requireProbeConfigVisible`). The probe, alert and channel override
writes do the same (see "Override Writes Check Visibility Before Token
Scope" below). Estate-wide (global) rows
stay readable by every caller. An unrestricted superuser session gets
200 with an empty list for a missing connection ID, since nothing is
hidden from it. `probe_config_visibility_test.go` holds the
caller-by-scope matrix to extend.

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
2. Sends the mutating request through `withAuth(t, store, req, token)`.
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
scopes (a nil auth store denies, as above), and `rbacExemptTools` and
`rbacCompanionTools` (through `canAccessTool`) are still applied.

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
`getUserInfoCompat`, which reads the authenticated context and so
admits API tokens (issue #668), making the scope gate reachable there.
A write addressed by id (blackout or
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
all-connections grant (read_write for a superuser) narrowed to the
lowest level of any entry in the token's connection scope
(`lowestScopedLevel`), since a specific entry overrides the wildcard
for its connection; `coversReach` compares a whole
`principalReach`. The exported checks in `internal/auth/grant_scope.go`
are the only entry points; handlers call them through
`internal/api/rbac_grant_scope.go`, and each refusal goes through
`requireGrantInTokenScope` with a fixed `refuse*` reason saying what
exceeded "this token's access" (fixed so the denial audit coalesces).

- Connection grant (`CanGrantConnection`): the token must hold the
  connection at that level; read can grant only read. The
  all-connections entry needs every connection at that level.
- Connection revoke (`CanRevokeConnection`): read on the connection,
  refused identically whether hidden or missing (#574). The lift rule,
  read_write when the grant is the connection's last grant naming it (a
  group-less connection opens to every user; an all-connections grant
  restricts nothing, so `liftsConnectionRestriction` never reports a lift
  for it, #592), is decided by the store inside the
  revoke's own transaction: the handler passes
  `RBACChecker.ConnectionLiftGuard` to
  `ActorStore.RevokeConnectionPrivilegeGuarded`, which returns
  `auth.ErrRevokeLiftsRestriction` (mapped to the same 403) so that two
  concurrent revokes cannot both pass a read-then-delete check. The
  guard is computed before the call because the store holds its write
  lock whilst applying it and must not call back into the checker.
  Accepted trade-off, not gated: a read-level token with
  `manage_permissions` may revoke another group's read_write.
- Group delete (`CanDeleteGroup`): read on every connection membership
  confers; the lift rule for each of the group's own grants runs in the
  delete's transaction through `GroupDeleteLiftGuard` and
  `ActorStore.DeleteGroupGuarded`. A grant added after the guard was
  computed is not in it, so the delete fails closed. Pinned by
  `internal/auth/revoke_lift_guard_test.go` (sequential and concurrent).
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
  `createToken` for that owner; since #522 a caller who is not a
  superuser reaches it only for its own owner), new users
  (`NewUserWithinTokenScope`): the principal's whole reach must fit.
- Superuser create or promote, a token for a superuser owner, and
  create, rename or delete of a group named in the OIDC `group_map`:
  `TokenHoldsEverything`, a superuser's token unrestricted in every
  kind.
- Token scope set (`JudgeTokenScopeChange`, self included):
  only kinds present are judged; an entry the target does not already
  allow must be covered; narrowing an unrestricted kind is free; a
  connection entry kept, narrowed or dropped needs read on it.
  Clear (`JudgeTokenScopeClear`): for each kind the target is
  restricted in, the owner's reach in that kind must fit. When the
  target's owner is a superuser, the target is bounded by its scope
  alone, so any widening (or any clear of a restricted target) also
  needs `holdsSuperuser()`: a session, or a superuser's token
  unrestricted in every kind. Both return `ErrTokenScopeUnreadable`
  (500 in the handler) when the stored scope, the target's owner or the
  owner's reach cannot be read. There is no exported verdict-only
  variant, so no handler can skip the snapshot; tests use the
  unexported helpers in `token_ceiling_judge_helpers_test.go`.
- Superuser-owned tokens: minting a token for a superuser owner, and
  setting, clearing or deleting a token whose owner is a superuser,
  need a superuser, for sessions and tokens alike. The owner is read in
  the store, inside the write's transaction (#607), not by the handler:
  `ActorStore.CreateToken` takes `callerIsSuperuser` and refuses on the
  `is_superuser` its own owner lookup reads; `DeleteToken`,
  `SetTokenScope`, `ClearTokenScope` and the single-kind setters take
  `superuserOwnerAllowed` and run `guardSuperuserOwnedTokenTx`
  (`token_superuser_owner.go`; a failed owner read refuses). Both
  return `auth.ErrSuperuserTargetForbidden`, which
  `respondTokenWriteError` maps to `denyNotSuperuser`; the store's
  deferred failure audit skips that error, so a refusal leaves only the
  coalesced denial row (`TestSuperuserOwnedTokenRefusalsAuditOnce`). The handlers
  pass `RBACChecker.IsSuperuser`, or `superuserOwnerAllowed(r, id)`,
  which also exempts a token acting on itself so it can narrow or
  delete itself; the CLI passes true. Do not add a handler pre-read of
  the owner back. `AuthStore` has no exported token writer, so every
  caller names an actor and the flag; tests use
  `store.AsActor(auth.SystemActor()).X(..., true)`, and
  `TestAuthStoreHasNoTokenWriters` fails if one is re-exported, or if
  the unguarded owner-scoped `DeleteUserToken` returns on either store
  (deleted in #607; delete through `DeleteToken`). The
  ceiling checks still run first in the handler, so a token caller
  widening or clearing a superuser's token gets the ceiling's 403, and
  any caller who is not a superuser minting for a superuser gets
  `refuseTokenForOtherOwner` (#522) before either check. `setTokenScope` writes all
  its kinds through one `ActorStore.SetTokenScope` transaction (one
  audit event per kind; any failure leaves every kind unchanged).
  Pinned by `internal/api/rbac_token_superuser_owner_test.go` and
  `internal/auth/token_superuser_owner_test.go` (owner promoted after
  the read, a concurrent promotion, atomicity).
- Token ownership (#522): a caller who is not a superuser
  (`RBACChecker.IsSuperuser`, so a superuser's token with any kind
  narrowed counts as not one) may create a token only for the user it
  acts as, and may widen or clear only tokens that user owns; to any
  other token it may only narrow the scope, or delete it. `createToken`
  calls `requireOwnTokenOwner` straight after the empty-owner check,
  before the ceiling; another owner, or an unknown one, gets
  `refuseTokenForOtherOwner` (403, recorded denial), and a failed
  `GetUser` 500. The insert then goes through
  `ActorStore.CreateTokenFor(..., tokenOwnerGuard(r))`, which refuses
  with `ErrTokenNotOwned` (mapped to `refuseTokenForOtherOwner`) an
  owner id, read in the insert's tx, other than the guard's user, so a
  user recreated under the name in between gets nothing.
  `ActorStore.CreateToken` is the unguarded wrapper (`AnyTokenOwner()`)
  for the CLI and tests. Scope writes pass `tokenOwnerGuard(r)` (an
  `auth.TokenOwnerGuard`: `AnyTokenOwner()` for a superuser and the
  CLI, else `TokensOwnedBy(userID)`; the zero value owns nothing) to
  `ActorStore.SetTokenScope` and `ClearTokenScope`, whose transaction
  runs `guardTokenOwnerTx` (`token_owner_guard.go`) after the superuser
  owner guard: it reads the owner and the stored scope in the tx (a
  concurrent narrowing cannot be undone), refuses a clear, and refuses
  a set for which `storedTokenScope.widens` is true, with
  `auth.ErrTokenNotOwned`. `widens` is "narrow" exactly: every
  connection entry at or below the stored level for it, every named
  entry already held in a restricted kind, no wildcard over a
  restricted kind, and no empty kind over a restricted one; omitted
  kinds and unrestricted kinds never widen. `respondTokenWriteError`
  maps `ErrTokenNotOwned` to `refuseTokenNotOwned` (403), and
  `failAudit` skips it, so a refusal leaves only the coalesced denial
  row. The ceiling reads the scope outside the write's tx, so
  `requireTokenScopeChange` returns the `*auth.TokenScopeSnapshot` it
  judged (`RBACChecker.JudgeTokenScopeChange` / `JudgeTokenScopeClear`;
  nil for a session or `TokenHoldsEverything`) and the handler passes it
  as the last argument of `SetTokenScope` / `ClearTokenScope`;
  `guardJudgedTokenScopeTx` (`token_scope_snapshot.go`, last guard in
  `beginGuardedTokenScopeChange`) re-reads the raw rows (connection id
  and level, MCP identifier ids orphans included, admin permissions)
  and refuses any difference with `ErrTokenScopeChanged`:
  `refuseTokenScopeChanged`, 409, recorded denial, no failure row.
  `tokenWriteChecked` (package var in `rbac_token_handlers.go`, nil in
  the server) runs between the checks and the write so tests can race
  them; tests that set it must not be parallel. Residuals, both read
  outside the tx and neither an escalation: the owner's reach that
  `JudgeTokenScopeClear` compares against, and the acting token's own
  scope (the ceiling), so a token narrowed mid-request can still give a
  sibling what it held when judged, which it could have copied there
  beforehand anyway. The single-kind setters (`SetTokenConnectionScope` and friends)
  are CLI-only and unguarded by owner; do not call them from a handler.
  The ceiling still runs first for token callers, siblings with the
  same owner included, so a narrowed token cannot mint or widen a
  sibling beyond itself. Delete stays open to any holder of
  `manage_token_scopes` (bar the superuser-owned rule below), and
  `listTokens` still lists every token, since any holder may narrow or
  revoke any of them. Pinned by `internal/api/rbac_token_ownership_test.go`,
  `internal/api/rbac_token_write_race_test.go`,
  `internal/auth/token_owner_guard_test.go` and
  `internal/auth/token_scope_snapshot_test.go`.
- Connection scope shape: `auth.ValidateScopedConnections` (store, HTTP
  and CLI alike) refuses a duplicate connection ID, a negative ID, an
  unknown level, and connection 0 (all connections) combined with any
  other entry, with `ErrInvalidConnectionScope` (400). Legacy rows that
  already mix them are still read with the specific entry taking
  precedence, in `IsConnectionInTokenScope`, `applyScopedConnections`
  and the ceiling alike. A scope edit reads the target's stored "all
  connections" entry at the lowest level any entry records
  (`storedTokenScope.connectionLevel`), so `{0: read_write}` written
  over a legacy `{0: read_write, 5: read}` raises connection 5 and needs
  every connection at read_write (`TestScopeChangeOverLegacyMixedScope`).
  Admin scope names must be known permissions or `*`
  (`ValidateAdminPermissions`, `ErrUnknownAdminPermission`, 400), and
  MCP scope names registered identifiers or `*`
  (`requireKnownMCPPrivileges` in the scope PUT, before the ceiling and
  owner checks so an unknown name is a 400 rather than a 403 widening;
  the store's `ErrUnknownMCPPrivilege` still catches one deleted in
  between).
- A user's reach counts group grants, public MCP items, every
  unrestricted connection the user can see, every connection their
  username owns (restricted or not, because `CanAccessConnection`,
  `updateConnection` and `deleteConnection` admit the owner), and every member connection of
  a cluster group the username owns (`ownedClusterGroupsCovered`,
  through `auth.OwnedClusterGroupLister`), all at read_write.
  `RBACHandler.SetConnectionLister` must be wired; a nil lister, a
  lister error or one without the owned-group method fails closed.
- `manage_users`, `manage_groups`, `manage_permissions`,
  `manage_token_scopes` and `*` each let a holder acquire every MCP
  item and admin permission, so `principalReach.reachesEverything`
  makes a holder count as reaching everything in those kinds.
- An empty MCP name list is not proof of an unrestricted MCP scope:
  `GetTokenMCPScope` joins away orphaned rows, so `actorMCPScope` asks
  `AuthStore.HasTokenMCPScope`, and `loadStoredTokenScope` (like
  `storedTokenScopeTx`) counts the raw IDs `GetTokenScope` returns.
- Every check fails closed on a nil checker, a nil store or a token
  context without an id.
- Every token scope read in `auth/token_scope.go` first calls
  `requireTokenLocked` and returns `ErrTokenNotFound` for a token that no
  longer exists, because the cascade leaves a deleted token with no
  scope rows, which reads as unrestricted. A token deleting itself
  between a handler's permission check and its ceiling check is then
  denied. A new scope read must make the same call. Pinned by
  `internal/api/rbac_acting_token_deleted_test.go` and
  `internal/auth/token_scope_missing_token_test.go`.

Known gaps, deliberately left: editing or disabling a user and deleting
a token not owned by a superuser are not ceiling-gated.

`query_datastore` (#566) takes no SQL: `buildDatastoreQuery` in
`internal/tools/datastore_query_builder.go` builds every statement from
the source catalog in `datastore_catalog.go`, binding the caller's
`VisibleConnectionIDs` set (and `CanSeeSystemAlerts`) as parameters
under each source's scoping rule. A visibility error fails closed, a
named inaccessible connection gets the generic not-found text, and an
empty visible set returns no connection-scoped rows whilst `global`
sources still return. A new datastore table becomes queryable only by
adding a catalog entry with an explicit scope and column list; never
add a credential column. A value that can hold a credential (the
`pg_settings` value columns for `primary_conninfo` and similar, and
secret entries in `pg_hba_file_rules.options`) is masked through
`metrics.ProbeRedactions` and `innerSelectList` (`metrics.MaskedSetting`
or `metrics.MaskedOptions`), and `pg_hba_file_rules.error` is masked whole
(`WholeColumns`/`metrics.MaskWhole`), inside the scoped subquery. Free-text columns that
can quote a password (query text, Spock row images, connection errors,
alert descriptions and AI analyses) are marked `RedactText` and pass
through `metrics.RedactedText`, which cuts the value to
`redact.MaxTextLength` characters and builds its `regexp_replace` chain
from `pkg/redact`'s `textRedactions` (also applied in Go by
`redact.Text`, which the alerter uses on connection errors). Those
patterns must stay within the regular expression syntax Go and
PostgreSQL share, use greedy quantifiers only (`redact.Text` compiles
them with `Longest`, as PostgreSQL takes the longest match), spell
white space as the ASCII class `ws`, bound any repetition of comments,
and have exactly one capturing group; their test inputs are
`redacttest.Cases`. A retyped column records its original type in
`StoredType`, which `datastoreEntityKeys` uses. The column masking
rules live in `internal/metrics/redaction.go`, shared with
`GET /api/v1/metrics/latest` and the latest-rows mode of
`GET /api/v1/metrics/query` (`ReturnableColumns`,
`MaskedProbeColumn`), `GET /api/v1/metrics/top-queries`
(`RedactStatementText`) and the timeline's configuration change events.
Statement text is lexed by `sqlredact.RedactQuery` as stored, never
after `RedactedText`, since a quote the patterns move lets the lexer
return the secret after it (#670): the latest routes select each
`metrics.RawStatementColumns` entry under `RawStatementAlias` beside
the masked column, which stays for `DISTINCT ON` and sorting, and
`RedactProbeRows` masks from it and deletes it; top-queries selects
`pss.query` as stored for `RedactStatementText`;
`MaskedProbeColumn` and the timeline use `metrics.RedactedSetting`,
which returns a connection string setting such as `primary_conninfo`
as stored when the patterns leave it unchanged (as they leave a value
the collector redacted) and whole as `********` otherwise, never partly
rewritten, because the Go `pgsettings.RedactValue` post-pass sees the
SQL's output and a rewritten quote can hide a password from it; the
latest routes refuse `order_by` on setting value columns
(`metrics.SortableColumn`). `query_datastore`, which has
no Go post-pass, keeps `MaskedSetting` and masks it whole; any new route returning probe rows must use them
and join `datastore_masking_routes_test.go`, and
`TestCredentialTableReadersAreReviewed` (`metrics/source_guard_test.go`)
fails on a SQL string literal that reads a credential table without
them unless it is on its reviewed list.
`TestMetricRedactionsMatchCollectorSchema` checks the real probe still
has the columns each `metrics.ProbeRedactions` rule names.
`TestConfigurationSourcesMatchCollectorSchema` checks every catalog
table, column and type against the collector's real schema, and fails
on a collector column of a catalogued table that is neither catalogued
nor listed in `catalogOmittedColumns`. A source's `LiveColumn`
(`connections.deleted_at`) adds `<col> IS NULL` to the server's own
conditions for every caller, a visible set of All included, and
`datastoreConnectionsExist` treats a marked connection as missing
(#669). The scope and time window go in
an inner `(SELECT ... WHERE <scope> OFFSET 0) t` (`scopedFrom`), and
the caller's filters, shape, ordering and paging apply outside it, so
no caller predicate is evaluated against a row the caller may not see;
the time window stays inside so partition pruning and the
`(connection_id, collected_at)` index still apply. Errors from a
running statement go through `datastoreQueryErrorFormatter` (general
message, detail logged). `explain` needs a visible set of All
(`checkDatastoreExplain`), since plan row estimates span hidden rows.
`describe_datastore` is admitted by a
`query_datastore` grant (`rbacCompanionTools` and `canAccessTool` in
`context_aware_provider.go`, one way only). Pinned by
`datastore_query_builder_test.go`, `query_datastore_integration_test.go`,
`query_datastore_scope_integration_test.go` and
`describe_datastore_access_test.go`. Tests: `internal/auth/token_ceiling_test.go`
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

Denials are coalesced in the auth store, beside the failure
coalescer, in `server/src/internal/auth/audit_denials.go`.
`recordDenial` calls `AuthStore.RecordCoalescedDenial(actor, action,
target, reason)`, whose `denialCoalescer` keeps an in-memory map keyed
by `denialKey` (actor type, actor id, actor name, client IP, capped
at `maxAuditIPLen` by `capAuditIP`, action, capped at
`maxDenialActionLen`, and reason; built by `denialKeyOf`), so that
two tokens of one user, or one token used from two addresses, never
suppress each other's denials. `maxAuditIPLen` and `capAuditIP` (which
cuts on a UTF-8 rune boundary) live in `audit_failures.go`, and
`failureKeyOf` applies the same cap to the failure key (issue #651).
The key must never hold a value
the caller chooses without limit: the path is deliberately not in it,
and `deniedRoute` maps an unrecognised method to `rbac.other` (only
the nine standard methods get `rbac.<lowercase method>`), since
either would let a client mint a fresh key, and so a fresh row, per
request. The target instead goes in the row: `deniedRoute` normalises
it to `kind/<id>` (a positive integer id) or `kind`, and
`unmatchedDenialTarget` for a route it does not recognise. The first
denial for a key is written at once with `details.target`; identical
denials within `denialCoalesceWindow` (60s) are counted in a
`denialWindow`, which keeps up to `maxDenialTargets` (20) distinct
targets and counts the rest in `targets_truncated`; and the first
denial after the window closes is written carrying `repeat_count`,
`targets` and `targets_truncated`. If a denial's own row cannot be
written, `reopen` puts its window back so later denials are not
suppressed behind a missing row. Entries sit on an intrusive list
ordered by `firstSeen` (not `container/list`, whose element type
assertions errcheck flags), so eviction walks from the oldest end and
stays amortised O(1). The map is capped at `maxDenialKeys` (10 000).
The list is the generic `evictionList` in `audit_eviction_list.go`,
which the failure coalescer (`failureCoalescer`, capped at
`maxFailureKeys`) shares; a state embeds `evictionLinks` and provides
`links()` and `windowStart()`. Anything that changes a state's
`firstSeen` must also move it on the list (`unlink` then `pushNewest`
or `insertOrdered`), and tests check the invariant with
`checkEvictionList`, so admit entries in time order in tests.

A window whose repeats were never reported is written as a summary
row carrying `window_closed`, `first_seen` and `last_seen` plus the
window's fields, in three ways: when eviction at the cap drops it;
on the five-minute cleanup tick (`runCleanupTick` in
`cmd/mcp-server/server.go`), where `SweepCoalescedAudit` drains every
closed window; and at shutdown, where `FlushCoalescedAudit` drains
every window, open or not. `AuthStore.Close` calls the flush, from
main's deferred `Server.Close`; `Server.serveHTTP` waits on the
`shutdownHandle` (`cmd/mcp-server/shutdown.go`) until the HTTP drain
has finished or `shutdownGracePeriod` lapsed, so that flush runs after
the last in-flight handler has recorded (issue #650). Do not let the
run path return before `waitForDrain`. `http.Server.Shutdown` cancels
no request, so a handler that waits for its client, such as the
overview SSE stream, must be ended through `mcp.HTTPConfig.OnShutdown`
(`Server.newHTTPConfig`, which builds the config `Run` serves, sets it
to `overviewHub.CloseAll` through `overviewStreamCloser`, and
`TestNewHTTPConfigInstallsTheOverviewStreamCloser` locks that in), or
every drain runs the full grace period.
The SIGTERM handler's closer flushes too, but only as a fallback when
main has not returned within `shutdownCleanupGrace` of the drain or a
further signal arrives; whichever flush runs second finds nothing to
write. Sweep and flush live in
`internal/auth/audit_coalesce.go` and write the closed denial and
failure summaries of one pass in a single transaction, putting the
entries back if it fails (and logging them with `[ERROR]` if a flush
fails). There is no exported way to write a denial row that bypasses
the coalescer; any new denial path must go through `recordDenial` or
`RecordCoalescedDenial`, or it loses the bound on how many rows one
client can append. A handler outside `RBACHandler` records through
`RBACHandler.RecordDenial`, injected as a function:
`ConnectionHandler` takes it via `SetDenialRecorder` into a
`denialRecorder` field and refuses through `h.denialRecorder.refuse`
(token scope, the `manage_connections` gate on create, the
owner-or-`manage_connections` gates on update and delete, and the
share gate; the `manage_connections` gate on
`PUT /connections/{id}/cluster` uses `refuseTarget`, below), and
`ClusterHandler`,
`AlertRuleHandler`, `NotificationChannelHandler`, `BlackoutHandler`,
`ProbeConfigHandler`, the three override handlers and `MemoryHandler`
(its `store_system_memory` gate on deleting a system-scoped memory,
recorded as `memory.delete` on `memories/<id>`) embed
`denialAuditor`, whose recorder they pass to
`requireAllConnectionsInTokenScope`,
`requireConnectionsInTokenScope`, `requireTargetInTokenScope` and
`requireBlackoutTargetInTokenScope` (each refuses even with a nil
recorder); `cmd/mcp-server/handlers.go` wires them in one loop, which
`TestSetupHandlersWiresDenialRecorders` pins. Never copy such a
handler by value: its gate holds a pointer to the original's
embedded `denialAuditor`.
Their `checkPermission` gate is
`requireAdminPermission(rbacChecker, &h.denialAuditor, perm)` in
`helpers.go`, which reads the recorder from the auditor at refusal
time (the server installs it after construction, so capturing it at
build time would record nothing) and refuses with
`Permission denied: requires <perm> permission` (issue #648); never
add a 403 for a missing permission that bypasses the recorder.
`ClusterHandler`'s cluster-group ownership and every
`manage_connections` 403 on the cluster and relationship routes
record through `h.denialRecorder.refuse` (a group the caller cannot
see answers 404 first and records nothing, issue #649), and
`BlackoutHandler.refuseOutOfScope` records its 403 but not its 404
for a record the token cannot see (issue #589).
`deniedConnectionAction` names the `connection.create` (POST
`/connections`, target `connections`), `connection.update`,
`connection.delete` and `connection.cluster.update` actions, and
`deniedAPIRoute` names `cluster.*`, `cluster_group.*`,
`notification_channel.*`, `alert_rule.*`, `blackout.*`,
`blackout_schedule.*`, `probe_config.*` and `memory.*` (a write to a
sub-resource is recorded as an update of its parent, and any `GET`
as `<resource>.read` on the object it names). Override
writes (`PUT`/`DELETE /<kind>-overrides/{scope}/{id}/{item}`) go
through `deniedScopedRoute`, which records `<kind>/<scope>/<id>` only
for a known scope and a positive id, and never the free-text item;
it maps `GET /<kind>-overrides/context/{connId}/{item}` to
`<resource>.read` on `<kind>/server/<connId>`. The route-level tests
are in `admin_refusal_audit_test.go`, `refusal_audit_test.go` and,
for memories, `memory_handlers_test.go`.
`IPExtractor.ExtractIP` returns only a parseable address (forwarded
entries go through `parseForwardedIP`, which strips a port), falling
back to the direct peer address.
`createConnection` calls `requireAllConnectionsInTokenScope` after the
`manage_connections` gate, as cluster and cluster-group creation do,
so a token scoped to part of the estate cannot add a shared connection;
the refusal is recorded as `connection.create`
(`TestCreateConnectionNeedsWholeTokenScope` in
`connection_handlers_caller_test.go`).
`updateConnection` and `deleteConnection` admit API tokens since
issue #668, so their connection token-scope refusals are reachable and
audited, as is the cluster move;
`TestConnectionMutationHonoursTokenConnectionScope` and
`TestConnectionScopeRefusalIsAudited` cover them.

Connection-access refusals (`CanAccessConnection` or
`canActOnAlert` says no) are audited too since issue #675, through
`server/src/internal/api/access_denial_targets.go`. Their routes name
the object outside the path (a query parameter or the body), so the
refusing handler calls `denialRecorder.refuseConnection` or
`refuseTarget`, which attach the action and a `connections/<id>` or
`alerts/<id>` target to the request context; `deniedRoute` returns that
before its path mapping. The reason passed is one of the fixed constants
`connectionAccessDenied`, `connectionWriteDenied`, `alertAccessDenied`
and `systemAlertManageDenied`, never the formatted response message,
because the reason is in the coalescer key and an id in it would write a
row per probed id. `alertRefusalReason` returns
`systemAlertManageDenied` for a system alert (`connection_id` NULL),
because acting on one needs `manage_alert_rules` rather than access to a
connection, and the constant matches the reason `requirePermission`
records; any other alert gets `alertAccessDenied`. The actions are the
`deniedAction*` constants in that file (`connection.read`,
`connection.query`, `metrics.read`, `server_info.read`,
`alert.acknowledge` and so on).
`MetricsHandler`, `LatestSnapshotHandler`, `PerfSummaryHandler`,
`ServerInfoHandler` and `AlertHandler` embed `denialAuditor` and are in
the wiring loop, which `TestSetupHandlersWiresDenialRecorders` pins.
An alert mutation refuses, and records, only an alert the caller can
see (`resolveVisibleAlert` answers a hidden one 404 with no row,
issue #677), so the alert cases are not in `accessRefusalCases`, whose
nil-store checker hides everything: `TestAlertRefusalsAreAudited` and
`TestHiddenAlertIsNotAudited` in `alert_refusal_audit_test.go` cover
them. `TestRefusalWritesOneAuditRow` (`admin_refusal_audit_test.go`)
runs each alert and override write refusal through the real
`RecordDenial` and asserts one row with its action and target, and
none for a hidden target.
`GET /connections/current` is audited as `connection.read` even
though the client reads it on every load; the coalescer bounds it. The
tests are in `access_denial_targets_test.go`.

MCP tools record refusals through `tools.DenialRecorder`
(`internal/tools/denial_recorder.go`), built once by
`NewDenialRecorder(authStore)` in `NewContextAwareProvider` and held
as `p.record` (`context_aware_provider.go`); it calls
`RecordCoalescedDenial` with `auth.ActorFromContext(ctx)`.
`record.deny(ctx, action, target, reason)` only records;
`record.refuse(...)` records and returns the tool error. A nil
recorder (no auth store) records nothing. Action, reason and target
strings are the constants in `denial_recorder.go`, never tool
arguments; connection targets come from `connectionDenialTarget(id)`
on a parsed integer (an ID that is not positive names the bare
`connections`, as REST `denialResourceTarget` does), and the tool gate in `Execute` names
`tools/<registry name>` or `unmatched` via `toolDenialTarget`.
Recorded refusals (issues #674, #706): `store_memory` system scope as
`memory.create` on `memories`; the tool gate as `mcp.tool.call`;
`ConnectionResolver.Resolve(ctx, action, args, fallback)` records the
caller's action on an RBAC deny (`connection.query`,
`connection.query.validate`, `connection.read`);
`resolveAccessibleConnection` takes the recorder and action
(`metrics.read` for metrics tools, otherwise `connection.read`);
`query_metrics` and `get_alert_rules` record directly; the
`query_database` write gate records `connection.query.write`, gating
on `ResolvedConnection.AccessLevel`, the level from the resolver's own
`CanAccessConnection` decision, rather than checking a second time; and
`query_datastore` records a refused `connection_ids` entry as
`connection.read` on the constant `connectionsTarget` (one call can
name several IDs); and the provider sets `database.ClientResolver.Denied`
to `sessionConnectionDenied(record)` on its own resolver and, through
`resources.ContextAwareRegistry.SetConnectionDenied`, on the resources
registry's (resources cannot import tools), so a lost selected
connection is `connection.read` from a tool or `read_resource`.
Reasons mirror the REST ones. `Execute` sends any call whose args hold
a `connection_id` key straight to the base registry before
`getClient`, so naming a connection never resolves, or records a
refusal of, the selected one. Every tool validates `connection_id`
with `connectionIDValue` (`connection_resolver.go`): only a float64,
int or int64 in 1..`MaxInt32` is valid, range-checked before
conversion, so 1.5, 2^63, `"5"` and null are refused, never truncated
or ignored; never use `parseIntArg` for it. `parseConnectionArgs` sets
`InvalidConnID` for any present but invalid value, and `Resolve`
answers it with `invalidConnectionIDMessage`, recording nothing, before
any lookup. Messages returned to the caller are otherwise unchanged. New tools that refuse
on a connection must take the recorder and pass `p.record` from the
provider. Tests: `denial_recorder_test.go`
(`TestProviderWiresStoreMemoryDenialRecorder`) and
`tool_refusal_audit_test.go` (`TestProviderRecordsToolAccessRefusal`,
`TestProviderWiresConnectionDenialRecorder`,
`TestProviderRecordsLostSessionConnection`,
`TestProviderRecordsLostSessionConnectionOnReadResource`,
`TestProviderExplicitConnectionSkipsSessionClient`) drive the provider,
so a nil recorder fails them;
`TestQueryDatastoreRecordsRefusedConnection` covers `query_datastore`
on Postgres; `tool_refusal_audit_db_test.go` covers the
write gate on Postgres.

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
Writing to an account that is already a superuser (resetting its
password, disabling it, deleting it) is as good as holding the role,
so it needs a superuser too, but that rule depends on target state and
so is checked in the store, not the handler (#588): `updateUser` and
`deleteUser` pass `RBACChecker.IsSuperuser` to
`ActorStore.UpdateUserAtomic` and `ActorStore.DeleteUser`, whose
transactions read the row under the store's write lock and refuse
with `auth.ErrSuperuserTargetForbidden` (`guardSuperuserTargetTx`,
before the last-superuser guard). `respondUserWriteError` maps it to
the same 403 and denial audit as `requireSuperuser`, through
`denyNotSuperuser`, and the store's deferred failure audit skips that
error so a refusal leaves only the handler's coalesced denial row. Do
not add a handler pre-check back on a target read outside the
transaction. Pinned by
`rbac_user_superuser_gate_test.go` and
`internal/auth/superuser_target_guard_test.go` (a target promoted
after the read, and a concurrent promotion). A superuser's tokens get
the same in-transaction gate (#607; see the superuser-owned tokens
entry under token scopes). Any new endpoint that can change superuser
status, or write to a superuser account or its tokens, needs the same
gate.

The store also refuses, with `auth.ErrLastSuperuser`, any demotion,
disable or delete that would leave no enabled superuser
(`guardLastSuperuserTx` in `actor_store_users.go`, counting the other
enabled superusers, service accounts included, inside the change's own
transaction). It applies to `updateUserAtomic`, `setUserEnabled`,
`setUserSuperuser` and `deleteUser`, so sessions, tokens and the CLI
alike; `respondUserStoreError` maps it to 409. Known exceptions, not
yet ruled on: the lockout disable in `disableForLockout`, and the
federated login's `is_superuser` sync in `federation.go`. Pinned by
`internal/auth/last_superuser_test.go` (including a concurrent mutual
demotion) and `internal/api/rbac_last_superuser_test.go`.

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

`auditHashVersion` is 3, rendered exactly as version 2 but under the
label `v3` (`auditHashKeyed`), and marks rows written by a build that
keeps the tail anchor described below. Both are an HMAC-SHA256 over
the canonical rendering,
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
then matched by writing the sequence down passes that comparison, and
`verifyAuditTail` then calls `verifyAuditTailAnchor` (#544).

The tail anchor lives in `audit_tail.go`: an `audit_tail` table
(`auditTailDDL`, created by `ensureAuditSchema`, which then refuses
any trigger acting on it (`verifyAuditTailNoTrigger`), rebuilds either
pre-release definition in `auditTailLegacyDDLs`
(`migrateAuditTailSchema`) and requires the normalised definition to
equal `auditTailDDL` (`verifyAuditTailSchema`, also called from
`verifyAuditSchema`); failures wrap `ErrAuditChainBroken`) with two
slots. Slot 1, the primary, holds the newest event's
id and hash under `auditTailMAC`, an HMAC with its own label. Slot 2,
the current-key anchor, exists only after a change of secret and holds
the newest row under `auditTailCurrentMAC`, which also covers the whole
of slot 1. `recordAudit` reads the newest row and both slots in one
statement (`readAuditTailState`), asks `planAuditTailMove` before its
INSERT, because the insert moves `sqlite_sequence`, and calls
`applyAuditTailMove` afterwards. The moves: advance slot 1 when it
names the newest row and verifies under `s.auditKey`, or for the first
event of a never-used log; advance slot 2 by the same rule; start slot
2 when slot 1 names the newest row, both fail and slot 2 is absent;
promote slot 2 unchanged into slot 1, keeping the overwritten slot-1
fields in slot 1's `bound_*` columns, and start slot 2 afresh, when
slot 2 names the newest row and both fail (a second rotation). Starting
and promoting slot 2 each `log.Printf` a `[WARN]` line. Check slot 1
under the key in use with `auditTailVerifies`, which refuses any slot 1
with a binding, because a genuine promoted value was written under an
earlier key and a writer can copy a server-signed slot 2 and its
binding into slot 1. `primaryTailVerifiesUnder`, which checks a
promoted slot 1 with `auditTailCurrentMAC` over its binding, is for the
previous key only; any other write to slot 1 passes an empty
`auditTailBinding`.
Anything else is left alone on purpose, so the evidence persists.
`verifyAuditTailAnchor` is reached only once every row verifies and
ignores slot 2; a rotation-shaped log goes to
`verifyAuditTailAfterKeyChange`, which requires slot 1 at the last
failing row and slot 2 at the newest row (`verifyAuditTailCurrent`),
and `proveAuditReanchorTail` checks, under the previous key, slot 2
while it fails under the current key (before promotion) and otherwise
slot 1, as part of `HistoryProven`; `proveAuditReanchorPlan` runs it
even when the history proof has already failed, inside one read
transaction whose re-scan must equal `plan.reanchor`. A MAC on a
binding is not trusted alone: case 3 of `checkRotatedAuditTail`
checks slot 1's `bound_*`, and case 4 slot 2's view of slot 1
(`primaryBinding`), with `checkAuditTailBinding`, which requires
`auditTailBoundRow` to find the bound row by id and hash below the
anchored id; `proveAuditReanchorTail` ends with
`proveAuditTailBinding`, which also requires that row to fail under
the previous key. `audit_tail_decoy_test.go` holds the attacks these
close, and pins two limits they do not (a cleared or repointed
`bound_*` reads as exit 3 in verify; a saved slot 1 put back passes
the re-anchor). New rows
carry `auditHashVersion` 3 (`auditTailHashVersion`), which renders as
version 2 does under the label `v3`; a v3 newest row with no anchor
fails verification. `initSchema` ends with `seedAuditTail`, which
anchors a v2 log only when `checkAuditTail` agrees and the newest row
verifies under `s.auditKey` (an anchor signed under a wrong secret
would read as altered under the right one and never advance). The
re-chain and re-anchor call `resetAuditTail` after `recordAudit`
unconditionally, which rewrites slot 1 and deletes slot 2.
Test fixtures that insert `audit_events` rows by hand at version 3
must also leave the anchor naming the newest row, or verification
fails; `audit_tail_test.go` has `insertVersion2Log` for a
pre-anchor log.

## Connection Access and Visibility Agree

`CanAccessConnection` and `VisibleConnectionIDs` decide a non-superuser's
base access with one function, `connectionAccessRule` in
`internal/auth/access.go`, before the token scope is applied (#592). Do not
add a visibility shortcut to either side. The rule, in order: the owner has
read_write whatever the restriction; a shared connection that no group grant
names is open at read_write; otherwise the group grant (specific or
all-connections, higher wins) decides. "Restricted" means a grant names the
connection itself (`IsConnectionAssignedToAnyGroup`, and
`RestrictedConnectionIDs` for the list path); an all-connections grant
restricts nothing. When `CanAccessConnection` has no sharing lookup, it
treats every connection as shared and unowned, as before;
`VisibleConnectionIDs` always takes the sharing facts from its lister.
`TestConnectionAccessAndVisibilityAgree` in
`internal/auth/connection_access_agreement_test.go` runs every case through
both functions and fails if they disagree.

## Override Writes Check Visibility Before Token Scope (Issue #672)

The probe, alert and channel override `upsertOverride` and
`deleteOverride` run, in order: the admin permission gate,
`scopeVisibleToCaller` (404 with the same message as `listOverrides`),
`requireTargetInTokenScope` (audited 403), then the body decode. A
hidden scope must cost a caller nothing it could learn from a missing
one, so keep visibility ahead of the token-scope 403 and the decode.
Because `VisibleConnectionIDs` intersects with the token scope, a
scoped token's out-of-scope server is hidden and gets 404; the 403
remains for a visible target the token may not write (a read-only
scope, or a cluster or group with any visible member). Cluster and
group visibility queries the datastore, so tests need a real one
(`newEstateGates` or `newCovFixture`); `runOverrideCoverage` in
`coverage_fixture_test.go` asserts hidden equals missing for every
override type and scope.
