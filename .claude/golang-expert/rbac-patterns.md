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

## Test Patterns

The regression tests live in
`server/src/internal/api/rbac_issue207_clusters_test.go`. Mirror them
when adding a new gated handler.

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

## Coverage Floor

Per `CLAUDE.md`, every modified file must reach at least 90% line
coverage. Cluster handlers depend heavily on the datastore, so most
of the coverage uplift comes from the integration suite. CI runs
`make coverage` with `TEST_AI_WORKBENCH_SERVER` set, exercising the
integration paths; local runs without Postgres will show lower
numbers but should still cover every newly-added gate via the
unit-style "denied" and "admin allowed" tests above.

When you add a gate, add at minimum:

- One "denied" unit test (negative path, no Postgres).
- One "admin allowed" unit test (gate passes, no Postgres).
- For Variant 2 handlers: one "owner allowed" integration test
  (Postgres-gated).

The denial test plus the gate body (5 statements) covers the new
lines; the admin-allowed test covers the not-taken branch.
