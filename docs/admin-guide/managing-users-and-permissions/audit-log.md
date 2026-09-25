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

The log also records two actions of its own:

| Action | Description |
|--------|-------------|
| `audit.purge` | The retention purge removed events from the log. |
| `audit.rechain` | The log was re-hashed, or given a new starting point. |

An `audit.purge` event is attributed to the `system` actor, carries no
target, and records the cutoff it applied in `details.older_than` and
the number of events it removed in `details.removed`. It also records
the event it left as the oldest in the log, by identifier in
`details.oldest_retained_id` and by that event's own hash in
`details.oldest_retained_hash`, which is what verification later checks
the start of the log against. The event is written in the same
transaction as the deletion, so a log that has shrunk always explains
why.

An `audit.rechain` event is attributed to the operator who ran
`-rechain-audit-log` and records how many events were re-hashed in
`details.events`, how many of them were unkeyed in
`details.unkeyed_events`, and whether the log recomputed under its own
rules beforehand in `details.legacy_chain_ok`. It is written in the same
transaction as the rewrite and becomes the newest link of the rebuilt
chain, so a log whose every hash has changed carries the event that
explains why.

The re-anchor, which `-rechain-audit-log` performs on a keyed log that
no longer verifies, also writes an `audit.rechain` event, with
`details.mode` set to `reanchor`. That event rewrites nothing; it
records why verification failed in `details.reason`, whether the failure
looked like a changed server secret in `details.key_mismatch`, and the
oldest event it accepted in `details.oldest_retained_id` and
`details.oldest_retained_hash`, the same fields a purge records, and
the number of events the log held in `details.events`. Where
it accepted events as history, it records the last of them in
`details.history_through_id`, their number in `details.history_events`
and a digest of their contents in `details.history_digest`. Where an
earlier purge or re-anchor had recorded a starting point, the
`details.previous_*` fields record which event it was and whether it
verified, and, only where it verified, what it named: what an event
that does not verify says is whatever its writer chose, so it is not
signed into the new one. A purge that runs whilst history events
survive copies the three `history_*` fields into its own event for the
events it keeps.

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

The lockout is the one change that is applied even when its event
cannot be recorded. Everywhere else a change is rolled back if the
server cannot write the event that describes it, because refusing a
change an operator asked for is the safe answer; here the server is
defending an account against a password guessing attack, so the lockout
is committed first and the event written after it. If the write fails,
the account stays locked and the failure is reported in the server log.

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
endpoint adds no events to the log. Each successful read is instead
noted in the server log, on a line prefixed `[AUDIT]` that names the
user or token that made the request, the filters in effect and the
number of events returned.

## Reading the Audit Log at the Command Line

The server command line reads the audit log directly from the
authentication store, which helps when the console is unavailable or
when you want to pipe events into another tool. It needs the server
secret file to do so, as does every other subcommand without exception,
the read-only ones included: the hash chain is keyed by a key derived
from that secret, and the command line resolves the key before it opens
the store at all. A command that cannot find the secret reports the
paths it searched and makes no change. The following table describes
the audit flags:

| Flag | Description |
|------|-------------|
| `-list-audit` | List RBAC audit log events |
| `-verify-audit-log` | Verify the audit log hash chain |
| `-rechain-audit-log` | Re-hash an inherited log, or re-anchor a failing one |
| `-confirm-rechain` | Confirm `-rechain-audit-log` without a prompt |
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
`jq` or a log shipper. The example below is one such line, indented
here so that it fits the page:

```json
{
  "id": 2,
  "occurred_at": "2026-09-15T11:09:17.648856224Z",
  "actor_type": "cli",
  "actor_id": null,
  "actor_name": "dba-ops",
  "action": "user.create",
  "target_type": "user",
  "target_id": 1,
  "target_name": "jane.doe",
  "outcome": "success",
  "details": {
    "after": {
      "id": 1,
      "username": "jane.doe",
      "display_name": "",
      "email": "",
      "annotation": "",
      "enabled": true,
      "is_superuser": false,
      "is_service_account": false
    }
  },
  "prev_hash":
    "76bed6cc892595f6035701e0b68b3c112088d4a14b2e2db9173043735f9f85e7",
  "hash":
    "5438335f26c3c01a4f42976957dca65f25b48cf44c637ff38ac8bd042736a092",
  "hash_version": 2
}
```

## Tamper Evidence

Each event stores a keyed hash (HMAC-SHA256) computed over its own
fields and over the hash of the event before it, so the log forms a
chain in which altering any event invalidates every hash that follows.
The key is derived from the server secret, so the chain cannot be
recomputed by someone who has the database file but not the secret.
Each event also records, in its `hash_version` field, the version of
the encoding its hash was computed under. Verification reads that
field, and an event
claiming a version this server will not compute, whether a version no
release ever wrote or the unkeyed version 1 encoding that is no longer
admissible, is reported as tampering: a version number is a value in
the file like any other, so relabelling an event must not put it beyond
the verifier's reach. A future change to the encoding is therefore not
free, as version 2 shows: introducing the keyed hash left every
version 1 event unverifiable, and an upgraded database has to be
re-chained, as described below. Verification also refuses an event
claiming a lower version than one before it, because the encodings only
ever move forwards.

The encoding records the length of each field alongside its value, so
moving text
across a boundary between two columns changes the hash rather than
leaving it intact. A unique index on the link to the preceding event
means no two events can claim the same predecessor, so the chain cannot
be forked into two branches that each verify. A database trigger rejects
updates to the `audit_events` table, and the only deletions the server
issues come from the retention purge described below. The one path that
updates events is the re-chain, which drops the trigger and re-creates
it inside the single transaction that rewrites the log, so the trigger
is never absent from any committed state of the database and no other
connection sees it gone.

The server re-creates the unique index and the trigger every time it
opens the authentication store, whatever schema version the store
records, so a database from which either has been dropped is protected
again at the next start. Verification also refuses to pass a log whose
index or trigger is missing at the time it runs, because a chain that
recomputes cleanly without them has shown nothing, and it checks what
each one does as well as its name: the index must be unique, must not
be partial and must cover the link to the preceding event and nothing
else, and the trigger must be the one the server creates, word for
word, and the only trigger that acts on the table. An object that keeps
the expected name but no longer protects the log, or an extra trigger
that could discard or delete events as they are written, is reported
with status `2`, and the server refuses to record an event that such a
trigger discards. A store whose chain
has already forked cannot be given the index, and the server refuses to
open it, naming the index and the query that finds the duplicates.

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
can run unattended from a scheduled job. The status distinguishes the
two cases that call for opposite responses:

| Status | Meaning |
|--------|---------|
| `0` | The log verified. |
| `1` | The check could not run, for example the store would not open. |
| `2` | The chain is broken or has lost its tail. |
| `3` | The oldest events do not verify under the key in use. |

A store that holds an unkeyed event does not open, so the command
reports it with status `1` and the reason the store refused.

Status `3` means that the oldest events, and possibly all of them, fail
under the key in use, each still linked to the one before it, whilst
every event after them verifies and links to the one before it, the
log has lost nothing from its tail, and no earlier event verified under
the key. That is the shape a server secret changed since those events
were written leaves behind, and the command reports it as a probable
wrong or rotated secret rather than as tampering. It never does so
where a purge or re-anchor event that verifies under the key records
where the log begins: such an event was written under the key in use,
after every older event had been checked or accepted, so no change of
secret can explain an event failing after it, and any failure on such
a log is reported with status `2`. Check that `secret_file` names
the file the log was written under before treating it as an incident;
if the secret was changed deliberately, or the old one is lost, see
[Recovering a Log That No Longer Verifies](#recovering-a-log-that-no-longer-verifies).
A failure of any other shape, such as an event in the middle of the log
that no longer verifies, is reported with status `2`.

A log that a re-anchor has given a new starting point reports how many
of its events were accepted as history:

```text
Audit log verified: 2 event(s), chain intact
  1 of them were accepted as history by the re-chain recorded in event 2,
  and verify only as unchanged since then, not as written by this server
```

Verification also compares the newest event's identifier against the
highest identifier the table has ever issued, which SQLite records
separately, so that events deleted from the newest end of the log are
reported rather than left invisible: removing the tail leaves every
surviving event correctly linked to the one before it and so would
otherwise verify cleanly. A disagreement is reported with status `2`,
like any other contradiction of the chain. The two figures are read
together, so an event the server writes whilst the check is running
cannot separate
them, and they must agree exactly. A record of the highest identifier
that has gone missing, that sits above the newest surviving event, or
that sits below it, is reported, because none of the three is a state
the database produces by itself; so is a store with no such record at
all, because every table the server creates keeps one and removing it
takes deliberate effort.

Verification checks the start of the log in the same spirit. Every
purge records the event it left as the oldest, as described above, and
nothing but the purge deletes events, so until the next purge the
oldest event must be exactly that one; a log whose oldest event is any
other is reported with status `2`, because events have been deleted
from the start of it since the purge ran. Where no purge event records
the oldest event, because no purge has removed anything since the
server was upgraded to a release that writes the record, verification
falls back to a weaker check: an oldest event that follows another
event, which is no longer in the log, is reported only when no
`audit.purge` event survives at all to account for the removal. A log
whose first event follows a missing predecessor and which holds no
purge event is evidence of deletion, since the purge is the only thing
that legitimately produces that shape and it always leaves an event
behind.

### Upgrading a Log Written Before the Keyed Chain

A database created by a release that predates the keyed chain holds
events hashed under the earlier unkeyed encoding, and this release will
not open such a database at all. The refusal names the events involved
and what to do about them.

The reason is that the unkeyed hash can be computed by anyone who can
write `auth.db`, so a run of unkeyed events carries no evidence of who
wrote it: a genuine inherited log and a forged one are the same thing
to a verifier. Earlier designs tried to keep a signed boundary between
the events a database inherited and the events written since, and each
attempt turned out to be another way to have the server sign a forgery
under the real key. Nothing unkeyed is admissible now.

Treat a database that will not open for this reason as suspect, and
restore `auth.db` from a known-good copy. Only where this is the first
start after upgrading, and you are satisfied the file has not been
altered, re-hash the inherited events under the server secret:

```bash
./bin/ai-dba-server -rechain-audit-log
```

The command prints what it has found, which is the number of events,
how many of them are unkeyed, the oldest and newest timestamps, and
whether the log recomputes under the rules each event was written with,
and then asks you to type `rechain` before it writes anything. Pass
`-confirm-rechain` to answer in advance for an unattended run. It
re-hashes every event as a keyed event in a single transaction, links
each one to the new hash of the event before it, and records the
re-chain itself as an `audit.rechain` event at the end of the log.
Verify the result immediately afterwards with `-verify-audit-log`.

That the existing log recomputes cleanly, which the command reports
before asking, is worth very little on its own: the unkeyed hash is
computable by anyone, so a rewrite done carefully leaves a log that
recomputes just as cleanly. It rules out a careless edit and nothing
more.

Understand what the re-chain does and does not assert. It attests the
log exactly as the database holds it at the moment it runs: whatever
that file says becomes what the keyed chain then vouches for. It proves
that the log has not been altered since the re-chain, and nothing
whatever about what happened before it. That is why it is an operator's
deliberate act, taken on the figures, rather than something a start-up
does by itself.

A database created at this release or later never needs it, because
every event in it was keyed from the start.

The command refuses outright in three cases, and writes nothing in any
of them. The first is a log that holds keyed and unkeyed events
together, whatever order they sit in. An upgraded database holds its
inherited unkeyed events and nothing else, so a log holding both means
unkeyed events were written into a log that was already keyed, which no
server path does; re-chaining would sign them under the server secret
and make them indistinguishable from events this server recorded. The
ordering is not consulted, because an event's identifier is a value in
the file like any other and can be chosen to sit below every genuine
event. The start-up refusal names this case separately, so an operator
meeting it is not sent to a command that cannot help them. Restore
`auth.db` from a known-good copy instead.

The second is an unattended run, with `-confirm-rechain`, of a log that
does not recompute under the rules its own events were written with.
The command prints that finding either way, but an unattended run has
nobody to weigh it, so it stops rather than signing a log the server has
just reported as not agreeing with itself. Inspect the log, and if you
still judge it sound, re-chain it interactively, where the warning
reaches you before you confirm.

The third is a log that changed between the figures you were shown and
the rewrite itself. The command re-reads the number of events and the
range of identifiers inside the transaction that holds the database's
write lock, and abandons the rewrite if any of the three has moved,
because the prompt may have sat waiting whilst something else wrote to
the file. The message gives both the figures you approved and the ones
it found, and nothing has been signed.

### Recovering a Log That No Longer Verifies

A keyed log that stops verifying also stops the retention purge, as
described under [Retention](#retention), and nothing the purge does by
itself clears the refusal. The two ordinary causes are a server secret
that has been changed or lost since the older events were written, and
events deleted or altered by someone able to write `auth.db`. The
`-rechain-audit-log` command recovers from both by re-anchoring the
log, which gives the log a new trusted starting point.

In the following example, `-rechain-audit-log` examines a keyed log and
offers to re-anchor it:

```bash
./bin/ai-dba-server -rechain-audit-log
```

The command runs verification first. A log that already verifies needs
nothing, and the command says so without asking:

```text
The audit log verifies as it stands; there is nothing to re-chain.
```

Otherwise the command prints why verification fails, the oldest event
it would accept with that event's hash, any starting point an earlier
purge or re-anchor recorded, and the events it would accept as history.
It then asks you to type `rechain`, and changes nothing on any other
answer. The following output comes from a log written under a server
secret that has since been replaced, with the failure reason shortened
here to fit the page:

```text
Audit log in /var/lib/ai-workbench:
  Events:              1
  Oldest event:        2026-09-25T14:14:52Z
  Newest event:        2026-09-25T14:14:52Z
  Verification fails:  audit chain key mismatch: audit row 1 does not ...

The oldest events do not verify under the current server secret, and the events
after them do. That is what changing or replacing the secret leaves behind. If
you have not changed secret_file, do not proceed: find the right secret file
instead, and the log will verify as it stands.

The re-chain would record:
  Oldest event accepted: 1
  Its hash:              d0dd1659b885db7ca952077849fbcbca0b92be24d02b014a32b7386c81e4ffee
  Previously recorded:   (none)
  Accepted as history:   1 event(s), 1 to 1

Events accepted as history are no longer shown to have been written by this
server. Whatever was done to them before now is accepted with them; from now on
the log shows only whether they change again.

No existing event is changed, re-signed or deleted. One audit.rechain event is
appended, signed under the current secret, recording the above as where the log
now begins.

Type "rechain" to proceed, or anything else to abort:
```

The `-confirm-rechain` flag answers the prompt in advance, but only
where the failure has the shape a changed secret leaves, the case an
operator can foresee and script for: the oldest events fail, linked to
one another, and every event after them verifies and links, with
nothing lost from the tail. The whole log is checked, because the
re-anchor accepts everything through the last failing event as
history, so a rotated secret cannot carry a later deletion or edit
through an unattended run. For any other failure an unattended run
prints the findings and stops, because the failure may be tampering and
needs someone to read it; re-anchor such a log interactively once you
have accounted for it, or restore `auth.db` from a known-good copy
instead.

That shape is evidence, not proof. Someone able to write `auth.db` can
edit every event up to and including the newest purge or re-anchor
event, whilst keeping their stored hashes and links, and the result
looks exactly like a changed secret. Because the purge runs every five
minutes, that newest event may be only minutes old, so the edit can
reach almost the whole log, along with any deletion made before those
events. A scripted `-confirm-rechain` accepts all of it as history
without anyone reading the plan, so use it only where the secret is
known to have changed, and run the command interactively whenever it
has not.

Where every event verifies and the failure is in where the log begins
or ends, such as events lost from the tail, the re-anchor accepts no
history, and the plan warns that recording a new starting point removes
the evidence of the failure from the log; afterwards only the recorded
reason shows that it was there.

The re-anchor never deletes, rewrites or re-signs an existing event.
It appends one `audit.rechain` event, signed under the current server
secret, that records the oldest event as where the log now begins, in
the fields described under
[Recorded Actions](#recorded-actions). Every event from the start of
the log through the last one that fails verification, or fails to link
to the event before it, is accepted as history, and the event records a
digest of their contents. From then on, verification and the purge
check those events against the digest and their number rather than
under the key, and hold every later event to the key as before. The
purge can therefore remove history events again as they age out, and
copies the digest of any it keeps into its own event.

Accepting events as history gives something up, and the plan says so
before you confirm. History events are no longer shown to have been
written by this server; anything done to them before the re-anchor, by
anyone, is accepted with them, and the digest shows only that they have
not changed since. That is why the re-anchor needs an operator's
confirmation.

Verification and the purge treat the newest anchor, meaning the newest
purge or re-anchor event that records a starting point, as
authoritative. Every purge or re-anchor event from the newest down to
that anchor must verify under the current secret: if one does not,
verification holds every event to the key as if no anchor existed, and
the purge refuses, rather than falling back to an older anchor. An
older event cannot
override a newer one: a copy of an earlier event put back into the log
does not link into the chain where it sits, and both verification and
the purge refuse a log in that state.

The command refuses, and writes nothing, when the log's unique index or
append-only trigger is missing or altered, when verification could not
read the log, and when the log changed between the plan you approved
and the write, which the command checks inside the transaction that
holds the database's write lock. After recording the event, the command
verifies the log again and reports any failure that a new starting
point does not account for. Verify the log with `-verify-audit-log`
afterwards in any case.

### What Verification Does and Does Not Show

Read those checks for what they are. They catch deletions made without
recomputing the chain, events altered in place, and a table whose
newest identifier has been rolled back; taken together, they raise the
number of deliberate steps required to hide a deletion from one to
several. They are not a defence against an attacker with write access
to `auth.db`.

Six limits are worth stating plainly.

An attacker holding both the server secret and write access to
`auth.db` can forge the log freely. The hash is keyed, so the secret is
what they need to rewrite events; with it they can recompute every hash
after making a change. Keep the secret file and the data directory
apart, as far as the operating system allows, so that an account able
to read one cannot reach the other.

Deleting the newest events needs write access to `auth.db` alone, and
no secret. The events left behind still verify, and the record of the
highest identifier issued, which verification compares the newest
event against, is an ordinary SQLite table with no protection of its
own; an attacker who deletes the newest events and then sets that
record to the new highest identifier leaves a log that verifies
cleanly. The comparison catches a deletion that leaves the record
alone, and nothing more.

Deleting the oldest events is caught precisely only once a purge has
recorded where the log begins. On a log whose purge events were all
written by builds that predate the record, verification can say only
whether some purge event survives to explain a missing predecessor, so
a deletion from the start of that log still verifies until the next
purge removes something and records the oldest event. Once the record
exists, removing the oldest events and every purge
event that mentions them is no longer enough: the newest surviving
purge event names an oldest event that is not there, and it cannot be
rewritten without the server secret. Events deleted and later put
back exactly as they were, from a copy, leave nothing to find.

The re-chain blesses whatever the database contained at the moment it
ran, as described above, so on an upgraded installation the keyed chain
says nothing about the period before that.

A re-anchor likewise accepts its history events as they stood when it
ran, as described above, so for those events verification shows only
that they have not changed since, and nothing about who wrote them.

Nothing verifies the log at start-up. Verification runs only when you
run `-verify-audit-log`, so schedule it if you want the check to happen
at all, and compare the event count and the oldest retained event
against what you expect.

An offline verification that passes is therefore evidence that nothing
careless happened, not proof that nothing deliberate did.

Treat a failed verification as evidence of tampering, and never a
successful one as proof of its absence. What actually protects the log
is keeping it out of reach: restrict access to the server's data
directory, and copy events off the host, through the REST API or the
`-json` output, to storage the server itself cannot write to. An
independent copy is the only one of these measures that survives an
attacker who reaches `auth.db`.

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

Before it deletes anything, the purge checks the events it is about to
remove. They must begin with the oldest event that the newest purge or
re-anchor recorded, or with the very first event the log ever held if
no purge has run, every one of them must verify under the server secret, and each
must link to the one before it, through to the event that becomes the
new oldest. Without that check, an attacker who deleted the start of
the log could insert a single backdated event and wait for the next
purge to remove it and record a new oldest event over the deletion. A
purge that finds anything else deletes nothing and logs an error that
contains `refusing to purge the audit log`, and it keeps refusing at
every five-minute run until the log is dealt with. Where the refusal is
because the log does not verify, the error names
`-verify-audit-log`, which shows what the purge found, and
`-rechain-audit-log`, which records a new starting point once you have
accounted for it, as described under
[Recovering a Log That No Longer Verifies](#recovering-a-log-that-no-longer-verifies).
Retention stops whilst the purge refuses, so `auth.db` grows until then.
Events a re-anchor accepted as history are checked against its digest
rather than under the key, so the purge removes them as they age out.

The purge removes a run of the oldest events and can express nothing
else: it finds the oldest event inside the retention window and deletes
everything below it by identifier, rather than deleting by timestamp. On
a log the server wrote itself, where identifiers and timestamps rise
together, the two are the same set of events. They part company when an
event's timestamp has been altered in the database, which is the point:
identifiers are assigned by SQLite in insertion order and nothing the
server does writes them, so a backdated event cannot steer the purge
into removing the newest events instead of the oldest. One consequence
is worth knowing about, since it shows up on a server that has been idle
for longer than the retention period: when no event at all falls inside
the window, the purge removes nothing rather than emptying the log, and
the backlog clears at the first purge to run once an event falls
inside the window again, which is to say within five minutes of that
event, or at the next start of the server.

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
