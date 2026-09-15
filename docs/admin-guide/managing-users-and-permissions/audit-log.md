# Audit Log

The Workbench records changes to users, tokens, groups, group
memberships and privileges in an audit log, along with the requests
that were refused for want of a permission, so that you can answer
questions such as who revoked a colleague's access and when. Repeated
denials are coalesced rather than recorded one by one, as described
under [Recorded Actions](#recorded-actions). Each event
names the principal that made the change, the address the request came
from, the object that was targeted, the state the change replaced and
whether the change succeeded. Events live in the `audit_events` table of
the server's `auth.db` authentication store. A change that succeeds
records its event in the same database transaction as the change itself,
so the log can neither lose a change that was applied nor claim one that
was rolled back. An attempt that fails is rolled back with its event, so
the server records a separate event for it afterwards with an outcome of
`failure`, as described under
[Outcomes and Details](#outcomes-and-details).

Sign-in and sign-out events are outside the scope of the audit log, as
are housekeeping operations such as expired token cleanup and session
invalidation. The log begins when the feature is first deployed and does
not reconstruct changes made before that point.

## Recorded Actions

Every audited action carries a dotted `noun.verb` name. The following
table describes the actions recorded for user and service account
changes:

| Action | Description |
|--------|-------------|
| `user.create` | A user account was created. |
| `user.update` | A user account's details were changed. |
| `user.delete` | A user account was deleted. |
| `user.enable` | A user account was enabled. |
| `user.disable` | A user account was disabled. |
| `user.set_superuser` | A user was made a superuser. |
| `user.unset_superuser` | Superuser status was removed from a user. |
| `service_account.create` | A service account was created. |

The following table describes the actions recorded for API token
changes:

| Action | Description |
|--------|-------------|
| `token.create` | An API token was issued. |
| `token.delete` | An API token was deleted. |
| `token.scope.set_connections` | A token's connection scope was set. |
| `token.scope.set_tools` | A token's MCP tool scope was set. |
| `token.scope.set_admin` | A token's admin permission scope was set. |
| `token.scope.clear` | All scope restrictions were removed from a token. |

The following table describes the actions recorded for group and
membership changes:

| Action | Description |
|--------|-------------|
| `group.create` | A group was created. |
| `group.update` | A group's name or description was changed. |
| `group.delete` | A group was deleted. |
| `group.member.add` | A user or group was added to a group. |
| `group.member.remove` | A member was removed from a group. |

The following table describes the actions recorded for privilege and
permission changes, all of which target the group that holds the grant:

| Action | Description |
|--------|-------------|
| `privilege.mcp.grant` | An MCP privilege was granted to a group. |
| `privilege.mcp.revoke` | An MCP privilege was revoked from a group. |
| `privilege.connection.grant` | Connection access was granted to a group. |
| `privilege.connection.revoke` | Connection access was revoked from a group. |
| `permission.admin.grant` | An admin permission was granted to a group. |
| `permission.admin.revoke` | An admin permission was revoked from a group. |

The log also records one action of its own:

| Action | Description |
|--------|-------------|
| `audit.purge` | The retention purge removed events from the log. |

An `audit.purge` event is attributed to the `system` actor, carries no
target, and records the cutoff it applied in `details.older_than` and
the number of events it removed in `details.removed`. The event is
written in the same transaction as the deletion, so a log that has
shrunk always explains why.

A request that an authorisation check refuses is recorded with the
action the request would have used had it been allowed, so that a denial
and the change it was refused share a vocabulary. Two action names
appear only on denials: `audit.read`, for a refused read of the audit
log itself, and `token.scope.set`, for a refused scope update, because a
single endpoint covers all three scope types. A refused request that
matches no known route records `rbac.` followed by the lower-case HTTP
method, so that the denial is kept rather than dropped.

Repeated denials are coalesced rather than recorded one by one, because
a client that retries a refused request in a loop would otherwise fill
the log and bury the events that matter. The first denial for a given
combination of actor, action and reason is recorded at once; identical
denials in the next sixty seconds are counted instead of recorded; and
the first denial after that window is recorded with a
`details.repeat_count` giving the number of attempts it stands for. If
no further denial arrives after the window closes, the suppressed
attempts are written as a summary event of their own, carrying
`details.repeat_count` alongside `details.window_closed`, so a burst
that stops is still counted rather than lost.

## Actor Types

Each event names the kind of principal that caused the change in the
`actor_type` field. The following table describes the four actor types:

| Actor type | Description |
|------------|-------------|
| `user` | A signed-in user acting through the console or the REST API. |
| `token` | A request authenticated with an API token. |
| `cli` | An operator running a server command at the command line. |
| `system` | A change the server made with no principal to attribute it to. |

A `user` or `token` event also carries the client address the request
arrived from, resolved through the server's trusted proxy settings. A
`cli` event carries no address, because a command line has no peer
address, and takes the actor name from the operating system user running
the command. The `system` actor covers changes the server makes on its
own account; the clearest example is the automatic account lockout after
too many failed sign-in attempts, which records a `user.disable` event
attributed to `system` with a `details.reason` of `lockout`.

## Outcomes and Details

Each event carries an outcome of `success`, `failure` or `denied`. A
`success` event records a change that was applied, a `failure` event
records a change that was attempted and errored, and a `denied` event
records a change that an authorisation check refused before it reached
the store. Both `failure` and `denied` events carry the reason in the
`error` field, and a `denied` event names the permission the caller was
missing.

Most events also carry a `details` field holding a JSON object. A change
that replaces existing state, such as a user update or any delete,
places the previous state in `details.before` and the new state in
`details.after`, while a creation carries `details.after` alone. The
user snapshot lists the identifier, username, display name, email
address, annotation, enabled flag, superuser flag and service account
flag, and never the password hash. The token snapshot lists the
identifier, owner, annotation and expiry, and never the token or its
hash.

Two characteristics of the recorded state are worth knowing when you
read the log:

- A grant event records that the grant was requested rather than that
  the grant changed anything, because granting a privilege a group
  already holds succeeds without altering the stored grant.
- A `privilege.connection.grant` event records the connection identifier
  in `details.connection_id`, the access level the grant replaced in
  `details.before.access_level` and the level it set in
  `details.after.access_level`, so raising a group's access on a
  connection from `read` to `read_write` is visible as a change of
  level. A first grant carries a `details.before` of `null`. A
  `privilege.connection.revoke` event records the connection identifier
  and the level that was withdrawn in `details.before.access_level`.

## Reading the Audit Log in the Console

Superusers can read the audit log from the `Audit Log` tab in the
`Security` section of the Workbench console's admin panel. The tab is
hidden from users who are not superusers, and no admin permission grants
access to it.

The tab presents a filter bar offering an actor name, an action, a
target type, an outcome and a date range, above a paged table of
matching events shown newest first. Selecting the expander on a row
opens that event's `details` object as formatted JSON, which is the
quickest way to compare the state before a change with the state after
it.

## Reading the Audit Log with the REST API

The `GET /api/v1/rbac/audit` endpoint returns audit events as a JSON
array, newest first. The endpoint is restricted to superusers and
responds with `403 Forbidden` to everyone else. The `X-Total-Count`
response header carries the number of events matching the filters before
`limit` and `offset` are applied, so that a client can size its pager.

A request made with an API token is refused as well, with the same
status, when the token's admin scope has been narrowed to specific
permissions: a scope is an explicit statement that the token is not a
general-purpose stand-in for its owner, and no admin permission grants
audit access. A token with no admin scope, or one holding the `*`
wildcard, reads the log on the same terms as its owner.

The following table describes the query parameters the endpoint accepts:

| Parameter | Description |
|-----------|-------------|
| `actor` | Actor name, matched exactly. |
| `actor_type` | Actor type: `user`, `token`, `cli` or `system`. |
| `action` | Action name, matched exactly. |
| `target_type` | Target type: `user`, `group` or `token`. |
| `target_id` | Numeric identifier of the target object. |
| `outcome` | Outcome: `success`, `failure` or `denied`. |
| `since` | Only events at or after this RFC 3339 timestamp. |
| `until` | Only events at or before this RFC 3339 timestamp. |
| `limit` | Page size, defaulting to 50 and capped at 500. |
| `offset` | Number of events to skip before the page begins. |

The following request returns the most recent denied events:

```bash
curl -H "Authorization: Bearer $TOKEN" \
    "https://workbench.example.com/api/v1/rbac/audit?outcome=denied"
```

Reads of the audit log are not themselves audited, so querying this
endpoint adds no events to the log.

## Reading the Audit Log at the Command Line

The server command line reads the audit log directly from the
authentication store, which helps when the console is unavailable or
when you want to pipe events into another tool. The following table
describes the audit flags:

| Flag | Description |
|------|-------------|
| `-list-audit` | List RBAC audit log events |
| `-verify-audit-log` | Verify the audit log hash chain |
| `-audit-actor string` | Filter audit events by actor name |
| `-audit-action string` | Filter by action, such as `group.create` |
| `-audit-target-type string` | Filter by target type |
| `-audit-target-id int` | Filter by target identifier |
| `-audit-outcome string` | Filter by outcome |
| `-audit-since string` | Only events at or after this time |
| `-audit-until string` | Only events at or before this time |
| `-audit-limit int` | Maximum events to show (default: 50) |
| `-json` | Print one JSON object per event instead of a table |

The `-audit-since` and `-audit-until` flags accept either a full RFC
3339 timestamp, such as `2026-09-15T00:00:00Z`, or a bare `YYYY-MM-DD`
date, which the server reads as midnight UTC.

In the following example, `-list-audit` prints the most recent events as
a table:

```bash
./bin/ai-dba-server -list-audit
```

The command prints the events newest first, followed by a count of the
events shown against the total number matching the filters:

```text
Audit events:
====================================================================================================================================
ID       Time                 Actor                Action                   Target                   Outcome  Error
------------------------------------------------------------------------------------------------------------------------------------
3        2026-09-15 11:09:18  dba-ops (cli)        group.delete             group:docs-demo          success
2        2026-09-15 11:09:17  dba-ops (cli)        user.create              user:jane.doe            success
1        2026-09-15 11:09:12  dba-ops (cli)        group.create             group:docs-demo          success
====================================================================================================================================
Showing 3 of 3 event(s).
```

In the following example, the flags combine to show the group changes
one operator made since the start of the month, as JSON:

```bash
./bin/ai-dba-server -list-audit -audit-actor dba-ops \
    -audit-target-type group -audit-since 2026-09-01 -json
```

Each line of the JSON output is one complete event, including the
`details` object and both hashes, so the output pipes straight into
`jq` or a log shipper:

```json
{"id":2,"occurred_at":"2026-09-15T11:09:17.648856224Z","actor_type":"cli","actor_id":null,"actor_name":"dba-ops","action":"user.create","target_type":"user","target_id":1,"target_name":"jane.doe","outcome":"success","details":{"after":{"id":1,"username":"jane.doe","display_name":"","email":"","annotation":"","enabled":true,"is_superuser":false,"is_service_account":false}},"prev_hash":"76bed6cc892595f6035701e0b68b3c112088d4a14b2e2db9173043735f9f85e7","hash":"5438335f26c3c01a4f42976957dca65f25b48cf44c637ff38ac8bd042736a092"}
```

## Tamper Evidence

Each event stores a SHA-256 hash computed over its own fields and over
the hash of the event before it, so the log forms a chain in which
altering any event invalidates every hash that follows. A database
trigger rejects updates to the `audit_events` table outright, and the
only deletions the server issues come from the retention purge described
below.

In the following example, `-verify-audit-log` recomputes the whole chain
and reports whether the log is intact:

```bash
./bin/ai-dba-server -verify-audit-log
```

A healthy log reports the number of events checked:

```text
Audit log verified: 3 event(s), chain intact
```

When the chain does not verify, the command names the first event whose
hash does not match and exits with a non-zero status, so that the check
can run unattended from a scheduled job.

Verification also compares the newest event's identifier against the
highest identifier the table has ever issued, which SQLite records
separately, so that events deleted from the newest end of the log are
reported rather than left invisible: removing the tail leaves every
surviving event correctly linked to the one before it and so would
otherwise verify cleanly.

The chain shows that nobody altered or removed an event in place. It
does not show that nobody rewrote the log, because a party who can write
to `auth.db` can recompute every hash after making a change and leave a
log that verifies cleanly. Treat a failed verification as evidence of
tampering rather than a successful one as proof of its absence, and
protect the audit log as you would any other security record: restrict
access to the server's data directory, and copy events off the host,
through the REST API or the `-json` output, to storage the server itself
cannot write to.

## Retention

The `http.auth.audit_retention_days` setting controls how long the
server keeps audit events and defaults to 90 days. Setting the value to
`0` disables the purge and keeps events forever. The setting can also be
supplied through the `PGEDGE_AUDIT_RETENTION_DAYS` environment variable;
there is no command-line flag for it.

In the following example, the server keeps a year of audit events:

```yaml
http:
  auth:
    audit_retention_days: 365
```

The purge runs once when the server starts and then every five minutes,
alongside the expired token cleanup, deleting events older than the
retention period. Because each remaining event still carries the hash of
the event that preceded it, the chain stays verifiable across a purge;
verification treats the earliest remaining event's recorded previous
hash as its starting point.

If you must keep audit events for longer than the server retains them,
export them to external storage before the purge removes them, either
through the REST API or with `-list-audit -json`.

## Next Steps

The following documents cover the changes the audit log records and the
settings that control it.

- The [Account Management](accounts.md)
  document describes the user and service account changes that the
  audit log records.
- The [Group Management](groups.md)
  document describes the group and membership changes that the audit
  log records.
- The [Permission Management](permission_mgmt.md)
  document describes the privilege and permission grants that appear in
  the log.
- The
  [Server Configuration](../../getting-started/configuration/server.md)
  document describes `http.auth.audit_retention_days` alongside the
  other server settings.
