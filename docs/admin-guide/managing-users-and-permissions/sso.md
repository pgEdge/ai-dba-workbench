# Single Sign-On

The Workbench can delegate interactive login to an OpenID Connect
identity provider, so that people sign in with the account they already
hold at the provider rather than with a Workbench password. This page
describes what federated login changes, how to configure it, how to map
provider groups onto Workbench groups, and the operational limits an
administrator needs to know about before turning it on.

Federated login changes only how a person proves who they are.
Authorisation is unchanged: a federated user reaches connections, MCP
tools and administrative operations through exactly the same groups,
privileges and token scopes as a local user, and the
[permission model](permission_model.md) applies to both in the same way.

Federated login is also a browser flow, so it does not help a headless
MCP client. Service-account API tokens remain the mechanism for
machine-to-machine access, as described in
[Token Management](tokens.md). Full OAuth 2.0 support for MCP clients
is out of scope.

## How a Federated Login Works

A federated login is an OpenID Connect authorisation code flow with
PKCE, driven by two server endpoints.

The sequence is as follows:

1. The login page shows a button whose label comes from
   `button_label`, pointing at `/api/v1/auth/oidc/start`.
2. The server mints a short-lived login state, seals it into an
   `HttpOnly` cookie that expires after ten minutes, and redirects the
   browser to the identity provider.
3. The person authenticates at the provider, which redirects the
   browser back to `/api/v1/auth/oidc/callback`.
4. The server exchanges the authorisation code for an ID token, verifies
   the token, and reads the configured claims from it.
5. The server matches the identity to a Workbench account, reconciles
   the account's mapped group memberships, and issues a session.

The account is matched on the issuer and the `sub` claim together, held
in the `users.external_subject` column, and never on the username:
matching on the username would let an identity whose username claim is
`admin` take over the local `admin` account.

A failed login takes one of two shapes, and which one an operator sees
says where the failure happened. The callback answers `400` with a
generic JSON error when the request never reached the provider
successfully: no state cookie, a state cookie that does not open, a
state parameter that does not match the sealed one, or neither an
authorisation code nor a provider error, since the provider's `error`
parameter is read first and takes the redirect branch below. It
redirects the browser back to the login page with a `login_error`
query parameter when the request did come back
from the provider: `login_error=provider` when the provider itself
declined, which includes the person cancelling at the consent screen,
and `login_error=login` when the code exchange or any later step
refused the login. Either way the real reason goes to the server log
only, and nothing derived from the provider's response reaches the
browser.

## Configuring the Server

Federated login is configured in the `http.auth` section of
`ai-dba-server.yaml`, alongside the settings that govern local login.
The following example enables both login methods, provisions accounts
on first login, and maps two provider groups:

```yaml
http:
  trusted_proxies:
    - "10.0.0.0/8"
  auth:
    local:
      enabled: true
    oidc:
      enabled: true
      issuer: "https://idp.example.com"
      client_id: "pgedge-workbench"
      client_secret_file: "/etc/pgedge/oidc-client-secret.txt"
      redirect_url: "https://workbench.example.com/api/v1/auth/oidc/callback"
      scopes: ["openid", "email", "profile", "groups"]
      username_claim: "sub"
      display_name_claim: "name"
      groups_claim: "groups"
      button_label: "Sign in with Example SSO"
      provision_users: true
      allowed_email_domains:
        - "example.com"
      superuser_group: "workbench-admins"
      group_map:
        workbench-dba: DBAs
        workbench-readonly: Read Only
```

Every setting in the `http.auth` section is read once, while the server
is starting, and is then held for the life of the process. None of them
can be changed by reloading the configuration: sending `SIGHUP` re-reads
the file and reports each changed authentication setting as requiring a
restart, but the running server goes on using the values it started
with. That applies to `local.enabled` and to every OIDC option, the
claim names and the authorisation mapping included, so narrowing
`allowed_email_domains` or removing a group from `group_map` whilst
containing an incident takes effect only once the server has been
restarted.

### Local Login Settings

The `http.auth.local` section controls username and password login.

The following table describes the local authentication options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `local.enabled` | bool | `true` | Enables username and password login. |

At least one login method must be available. The server refuses to
start when `local.enabled` is `false` and `oidc.enabled` is also
`false`.

### Federated Login Settings

The `http.auth.oidc` section controls federated login. Every option
below is ignored while `enabled` is `false`.

The following table describes the federated authentication options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `oidc.enabled` | bool | `false` | Enables federated login. |
| `oidc.issuer` | string | | Issuer URL of the identity provider; must use `https`, and discovery runs against it at start-up. Required. |
| `oidc.client_id` | string | | OAuth 2.0 client identifier registered at the provider. Required. |
| `oidc.client_secret` | string | | Client secret supplied inline in the configuration file. |
| `oidc.client_secret_file` | string | | Path to a file holding the client secret, used when `client_secret` is empty. |
| `oidc.redirect_url` | string | | Absolute URL the provider returns the browser to; its path must end with `/api/v1/auth/oidc/callback`. Required. |
| `oidc.scopes` | list | `[openid, email, profile]` | Scopes requested at the provider; `openid` is added when it is absent. |
| `oidc.username_claim` | string | `email` | Claim whose value becomes the Workbench username. |
| `oidc.display_name_claim` | string | `name` | Claim whose value becomes the account's display name. |
| `oidc.groups_claim` | string | `groups` | Claim carrying the user's provider group names. |
| `oidc.button_label` | string | `Sign in with your identity provider` | Label of the federated login button. |
| `oidc.provision_users` | bool | `false` | Creates a Workbench account the first time an unknown identity logs in. |
| `oidc.allowed_email_domains` | list | `[]` | Restricts login to verified addresses in these exact domains; empty means no restriction. |
| `oidc.superuser_group` | string | | Provider group whose members hold the Workbench superuser flag; empty leaves the flag under local control. |
| `oidc.group_map` | map | `{}` | Maps provider group names to Workbench group names. |

Either `client_secret` or `client_secret_file` must be set. A secret
read from `client_secret_file` is held only in memory, so saving the
configuration cannot write it back to disk in plaintext; an inline
`client_secret` sits in the configuration file and should be protected
by file permissions accordingly.

The server refuses to start when `oidc.enabled` is `true` and `issuer`,
`client_id`, a client secret or `redirect_url` is missing, and it also
refuses to start when discovery against `issuer` fails. Failing at
start-up is deliberate: a provider that cannot be reached means nobody
can log in, which is better learned immediately than from a login page
whose button does not work.

### Requiring a Trusted Proxy List

Set `http.trusted_proxies` to the reverse proxy's address ranges
whenever the Workbench sits behind a proxy, which in a supported
deployment it always does.

The callback endpoint is rate limited per client address, and the
allowance is spent only by requests that reach the identity provider,
so a request refused before that point, one with no login state cookie
or a mismatched state, costs nothing. Behind a proxy, every request
carries the proxy's address unless `http.trusted_proxies` names it, so
the limit collapses to a single allowance shared by the whole
deployment. An anonymous caller can spend that allowance: the start
endpoint needs no credentials and is not itself rate limited, so one
`GET` to it mints a login state that replays against the callback for
the ten minute lifetime of the state plus a minute of clock skew, and a
handful of such requests deny everyone else a login until the window
passes. Setting `http.trusted_proxies` is what stops the allowance being
shared across every client, because the limit then applies to the
caller's own address. On a deployment where local login is switched
off, that is the only way in.

The same list decides how far the server believes the
`X-Forwarded-Proto` header. Every cookie the server sets carries the
`Secure` attribute whenever the header says `https`, whichever address
the request came from, because a forged header can only cost the forger
their own cookie. The login state cookie additionally uses the
`__Host-` name prefix, which stops a compromised sibling subdomain
overwriting it, but only when the header arrived from an address on
`http.trusted_proxies`; without the list the cookie is written under
its plain name. The server prints a warning at start-up when the list
is empty and federated login is enabled.

## Registering the Workbench at the Provider

Register the Workbench as a confidential OAuth 2.0 client at the
identity provider, with the authorisation code flow and PKCE enabled.

The provider needs the following from the Workbench:

- the redirect URI, which is the public URL of the Workbench with
  `/api/v1/auth/oidc/callback` appended, for example
  `https://workbench.example.com/api/v1/auth/oidc/callback`.
- the scopes the Workbench requests, which are `openid`, `email` and
  `profile` by default, plus whatever scope the provider requires
  before it releases group membership.
- a claim carrying group membership, if group mapping or a superuser
  group is to be used.

The provider issues a client identifier and a client secret in return,
which become `client_id` and `client_secret_file`.

The redirect URI registered at the provider and the `redirect_url` in
the configuration must be the same string, and both must be the URL the
browser actually reaches, not the server's internal address. Where TLS
terminates at a reverse proxy, that is the proxy's public URL; see
[TLS and Reverse Proxy Requirements](../tls-and-reverse-proxy.md) for
the headers the proxy has to forward.

The Workbench validates `redirect_url` only far enough to catch a typo.
It checks that the value is an absolute URL, that the scheme is `https`
(or `http` for a loopback host), that the path ends with the callback
path, and that there is no query string or fragment; a reverse proxy
prefix before the callback path is accepted. It does not and cannot
check the origin, so at start-up it prints the host the identity
provider will deliver authorisation codes to, which is where a typo
shows up. The control that stops an authorisation code being sent
somewhere else is the provider's own list of registered redirect URIs,
so keep that list tight.

## Choosing the Username Claim

The value of `username_claim` becomes the Workbench username, and it
must satisfy the same username rule the `-add-user` command applies: the
first character must be a letter or a digit, the remaining characters
must be letters, digits, or one of `_`, `.`, `@` and `-`, and the whole
value must be at most 128 characters.

A login whose username claim breaks that rule is refused, and the server
logs a diagnostic naming the claim and the class of character that
offended, so that the failure points at the configuration rather than
looking like a bug.

The default of `email` is convenient but not universally safe. Logins
fail in both of the following common cases:

- a plus-addressed email such as `jane+work@example.com` contains a
  character the rule does not allow.
- a Microsoft Entra ID guest account has a `preferred_username` of the
  form `jane_example.com#EXT#@tenant.onmicrosoft.com`, which contains
  `#`.

Point `username_claim` at a claim whose values satisfy the rule. The
`sub` claim always does, and it is also the claim the account is matched
on, so it is the safest choice on any provider whose email addresses or
usernames are not strictly controlled.

## Provisioning and Email Domains

Set `provision_users` to `true` to have the Workbench create an account
the first time an unknown identity logs in. The account is created with
an unusable password hash, so it can never be reached through password
login, and it starts with no groups and no superuser flag until
reconciliation grants them.

Leave `provision_users` at `false`, which is the default, to pre-create
every account instead. A login by an identity that no account is linked
to is then refused, and an administrator links each account to its
provider identity with the server command line, as described in
Linking an Existing Account below.

The address itself always comes from the standard `email` claim, which
is not configurable, and the verification flag from the standard
`email_verified` claim.

The `allowed_email_domains` list restricts who may log in at all. An
empty list means no restriction. A non-empty list requires the identity
to carry an email address, requires the provider to have asserted
`email_verified` for the address, and requires the domain after the `@`
to match a listed entry exactly. Matching is case-insensitive and a
leading `@` on a list entry is ignored, so `@example.com` and
`example.com` behave identically.

Domains match exactly and subdomains do not match: an address at
`sub.example.com` is refused by a list containing only `example.com`.
List every domain that should be allowed.

The address stored on the account in `users.email` is asserted by the
provider rather than proven. It is checked against `email_verified`
only when `allowed_email_domains` is non-empty; with no allow-list
configured, an unverified address is still stored on the account.
Treat the stored address as a label, never as a credential or as
grounds for granting anything.

## Linking an Existing Account

An account is reached by a federated login only once the issuer and
subject of the provider identity are recorded against it. Automatic
provisioning records them when it creates the account; where
`provision_users` is `false`, an administrator records them with the
`-link-oidc-user` command.

Linking stamps the account exactly as provisioning would, so a linked
account and a provisioned one are indistinguishable to login, to group
reconciliation and to every later permission check.

The workflow starts with a failed login, because the server log is
where the issuer and subject come from:

1. Ask the person to attempt a federated login. It is refused, and the
   browser returns to the login page.
2. Read the refusal from the server log. The line begins
   `[OIDC] Refusing federated login:` and quotes both values, along
   with the command to run:

   ```text
   [OIDC] Refusing federated login: no account for federated identity
   issuer "https://idp.example.com", subject "8a7f-sub" (username
   "alice") and provisioning is disabled; link an existing account
   with: ai-dba-server -link-oidc-user -username <name> -issuer
   "https://idp.example.com" -subject "8a7f-sub"
   ```

3. Link the pre-created account to that identity, passing the issuer
   and subject exactly as they were logged:

   ```bash
   ./bin/ai-dba-server -config /etc/pgedge/ai-dba-server.yaml \
       -link-oidc-user -username alice \
       -issuer "https://idp.example.com" -subject "8a7f-sub"
   ```

   The command refuses an issuer that is not an `https://` URL with a
   host, since the server only accepts such an issuer in its own
   configuration and no login could reach an account linked to
   anything else.

4. Ask the person to sign in again. The login now resolves to the
   linked account, and group reconciliation runs against it.

The command reports the account, the issuer, the subject and the stored
external subject key, and notes that password login is refused for the
account from that moment. Run `-list-users` afterwards to confirm the
link: the account's `Authentication` column now reads the issuer rather
than `Local`.

### Linking Leaves Existing Credentials Working

Linking changes how the account is signed into. It does not revoke
anything the account already holds, and two kinds of credential
therefore survive it.

Every API token the account owns keeps working afterwards, at the
account's full privilege, and nothing about the link limits or expires
it. An administrator linking an account in order to tighten its
authentication has not tightened anything until those tokens are dealt
with: list them with `-list-tokens`, and remove any that should not
outlive the change with `-remove-token`. This is exactly why the
default unlink revokes them.

Any session the running server has already issued also keeps working,
for the reason given in Sessions and the Command Line below.

### Linking Hands Group Membership to the Provider

A linked account is deliberately indistinguishable from a provisioned
one, so the next federated login reconciles its mapped groups exactly
as it would for an account the provider created.

Linking a pre-existing local account therefore hands that account's
membership of every mapped group, and its superuser flag wherever
`superuser_group` is configured, to the identity provider from that
login onward. An administrator who links the local `admin` account
whilst `superuser_group` names a group the provider does not put them
in loses superuser at their next sign-in.

This is the reason the break-glass administrator account must stay
unlinked, as described in Keeping a Local Break-Glass Administrator
below.

Where any other existing account must be linked, confirm before linking
that the provider asserts every group the account needs, including the
superuser group.

### Refusals and Relinking

Linking is refused rather than allowed to do something surprising in
three cases.

The following table describes each refusal and the way past it:

| Situation | Outcome |
|-----------|---------|
| The account is already linked to a different identity | Refused, naming the identity currently in the way; pass `-relink` to move the account. |
| Another account already holds the subject | Refused, naming the account that holds it; unlink that account first. |
| The account is a service account | Refused outright, because a service account can never hold a session and the link would never work. |

Linking an account to the identity it already holds succeeds and
changes nothing, so a script that runs the command twice does not fail.
A disabled account may be linked, which is convenient when accounts are
prepared ahead of a rollout, though nothing works until the account is
enabled.

To move an account to a new identity, for example after a provider
migration changes the subject, pass `-relink`:

```bash
./bin/ai-dba-server -config /etc/pgedge/ai-dba-server.yaml \
    -link-oidc-user -username alice \
    -issuer "https://idp.example.com" -subject "new-sub" -relink
```

### Unlinking an Account

The `-unlink-oidc-user` command detaches an account from its provider
identity and returns it to local authentication:

```bash
./bin/ai-dba-server -config /etc/pgedge/ai-dba-server.yaml \
    -unlink-oidc-user -username alice
```

The command has two branches, and which one runs decides what is left
of the account's access.

The following table describes what each branch does to the three
credentials an account can hold:

| Credential | Default | With `-restore-password` |
|------------|---------|--------------------------|
| Password | Replaced with an unusable hash | The password the account last held as a local account works again |
| API tokens | Revoked | Left in place |
| Browser sessions | Left running, see below | Left running, see below |

The default is the offboarding branch. It detaches the identity, so no
federated login is possible; it replaces the password hash with an
unusable one, so no password login is possible until an administrator
sets a password with `-update-user -username <name>`; and it revokes
every API token the account held, along with each token's scope rows.
All three take effect immediately. A session the running server has
already issued is a separate matter, covered below.

Pass `-restore-password` to convert the account back to local login,
for an administrator who means to hand it back to its holder:

```bash
./bin/ai-dba-server -config /etc/pgedge/ai-dba-server.yaml \
    -unlink-oidc-user -username alice -restore-password
```

That branch is a conversion rather than a revocation, so it keeps the
account's API tokens as well as its password. Weigh it carefully. What
comes back is whatever hash the account last held as a local account:
the server refuses to write a password onto a federated account, so
nothing can have been set on it whilst it was linked, and the hash has
been inert for the whole federated period. The password it holds may
therefore be years old, will not have been rotated while the account was
federated, and may be known to the person who has just left; the tokens
have the same problem, and should be reviewed with `-list-tokens` and
removed with `-remove-token` where they are no longer wanted. An
account the provider originally created holds a hash of discarded
random bytes, so `-restore-password` on one of those
restores nothing usable.

### Offboarding a Federated User

Removing a person's access takes two commands, in this order:

1. Unlink the account without `-restore-password`, which makes the
   password unusable and revokes the account's API tokens:

   ```bash
   ./bin/ai-dba-server -config /etc/pgedge/ai-dba-server.yaml \
       -unlink-oidc-user -username alice
   ```

2. Disable the account, which ends any session the running server
   still holds and stops new credentials being minted:

   ```bash
   ./bin/ai-dba-server -config /etc/pgedge/ai-dba-server.yaml \
       -disable-user -username alice
   ```

The unlink alone is not enough, for the reason given in Sessions and
the Command Line below. Revoking tokens removes the credentials that
exist today; disabling the account refuses everything the account
tries, including a session that was issued before either command ran.

The unlink command reports which branch ran, including how many tokens
it revoked, and the server log records the same. The account's
`Authentication` column reads `Local` again once the command has run,
which is the quickest confirmation that the identity is detached.

### Sessions and the Command Line

Neither `-link-oidc-user` nor `-unlink-oidc-user` ends a session the
running server has already issued. Sessions live in the server
process's own memory, and these commands run in a separate process
against the account database, so a session that was minted before the
command runs keeps working until it expires, which may be up to 24
hours later. Both commands say so in their output.

Two commands do cut such a session, because the server re-reads the
account's enabled flag from the database on every request:

- `-disable-user -username <name>` disables the account, which refuses
  its sessions from the next request onward.
- restarting the server discards every session it holds, for local and
  federated users alike.

This matters in two places. When offboarding, the unlink is not on its
own a revocation; follow the sequence in Offboarding a Federated User
above. When relinking an account to a new subject, whoever signed in
under the previous identity keeps a working session until it expires,
so disable the account or restart the server if that person must be
cut off at once.

Linking never touches the account's API tokens either, as described in
Linking Leaves Existing Credentials Working above; unlinking revokes
them unless `-restore-password` is passed. Linking an account to the
identity it already holds changes nothing at all, which keeps a
configuration-management run that reasserts every link from disturbing
anybody on each pass.

## Mapping Provider Groups to Workbench Groups

The `group_map` option maps a provider group name to a Workbench group
name. Nothing outside the map is consulted, so a provider can only
influence the Workbench groups an administrator has explicitly listed;
group names are never matched by equality across the two systems, and a
Workbench group is never created because the provider mentioned one.

Consider a provider that asserts the groups `workbench-dba` and
`workbench-readonly`, and a Workbench with existing groups named `DBAs`
and `Read Only`. The following configuration ties them together:

```yaml
http:
  auth:
    oidc:
      groups_claim: "groups"
      superuser_group: "workbench-admins"
      group_map:
        workbench-dba: DBAs
        workbench-readonly: Read Only
```

At each login, the Workbench compares the groups the provider asserted
against the map and reconciles membership of the mapped groups only, so
that:

- a user the provider puts in `workbench-dba` is added to `DBAs`.
- a user the provider no longer puts in `workbench-dba` is removed from
  `DBAs`.
- a Workbench group the map does not name is left untouched in both
  directions, so a locally managed group is neither emptied nor joined.

A mapped Workbench group that does not exist is logged and skipped
rather than failing the login, so a typo in the map cannot lock
everybody out. Create the Workbench groups first, as described in
[Group Management](groups.md), and check the server log after the first
federated login.

### Granting Superuser from a Provider Group

The `superuser_group` option names one provider group whose members
hold the Workbench superuser flag. The flag is granted while the
provider asserts that group and revoked as soon as it stops, and it is
never inferred from any other claim. Leaving `superuser_group` empty
leaves the flag entirely under local control, which is the right
setting wherever superuser should not be delegated to the provider's
group administrators.

The revocation is committed before any grant, so a reconciliation that
fails part way through can only leave a user holding fewer privileges
than the provider asserts, never more.

Setting `superuser_group` hands out more than the flag's name suggests,
and the server prints a warning at start-up whenever it is configured.
A superuser bypasses every group grant, and also every API token
connection scope: the superuser check in the access path returns before
a token's scope is consulted, so an API token minted from a superuser
account reaches every connection at `read_write` whatever scope it was
given. That short-circuit predates federated login and is tracked
separately, but `superuser_group` changes who can trigger it, from a
Workbench administrator to anyone able to add a member to one provider
group. Revocation is no faster than any other mapped group: the flag is
withdrawn at the person's next login, and a browser session or an API
token they already hold keeps full privilege until then, so disable the
account to revoke it sooner. Leave the option empty unless the
provider's group administrators are trusted to that extent.

### Keeping Mapped Groups Flat

Do not nest a mapped group above other Workbench groups. A mapped group
must have no member groups of its own.

Membership is resolved by walking upwards from a user's direct
memberships, so a user who is a member of a group nested inside a mapped
group holds the mapped group by inheritance. Reconciliation writes only
the direct membership row, so removing the direct row leaves the
inherited path intact, and the user keeps everything the mapped group
confers after the provider has stopped asserting it. Keep every mapped
group at the bottom of the hierarchy, and grant nested structure through
groups the map does not name.

## Revocation and Its Limits

Reconciliation runs at login and only at login. Nothing re-evaluates a
provider's assertions in the background, so the Workbench cannot notice
a change made at the provider until the person next logs in.

A user removed from a mapped group at the provider therefore keeps
whatever that group confers until their next Workbench login, which for
the holder of a long-lived API token may be never. The same applies to
the superuser flag when it comes from `superuser_group`.

Disabling or deleting the Workbench account is the only complete
revocation. Disabling an account refuses both its logins and its API
tokens immediately, and reconciliation refuses to touch a disabled
account, so an administrator containing an incident cannot have the
privileges quietly reinstated underneath them.

## Keeping a Local Break-Glass Administrator

Keep at least one local superuser account with a password, keep
`local.enabled` set to `true`, and never link that account to the
identity provider.

The identity provider is a dependency of every federated login, so an
expired client secret, a rotated signing key, a failed discovery
endpoint or an outright provider outage locks out every federated user
at once. Discovery failure is fatal at start-up, so a server restarted
during a provider outage does not come up at all with federated login
enabled. A local administrator who can still sign in is the difference
between editing the configuration calmly and rebuilding access under
pressure.

Linking that account would make it depend on the two things it exists
to be independent of. A linked account can be reached only through the
provider, so a provider outage takes it out with everything else, and
its superuser flag is reconciled at each login from
`superuser_group`, so it holds superuser only while the provider keeps
asserting that group. Leave the account local, let the provider grant
superuser to separate federated accounts, and the recovery path
survives whatever has happened to the provider.

Test the local account periodically, and store its password where it
can be reached without the Workbench. Switching `local.enabled` to
`false` is reasonable once federated login is proven, but it removes
this recovery path.

## Sessions Do Not Survive a Restart

Sessions are held in memory, so every session ends when the server
restarts and everyone signs in again. This applies to local and
federated logins alike, and it is pre-existing behaviour rather than
something federated login introduced.

A session lasts 24 hours, and each user may hold at most ten
simultaneous sessions; an eleventh login evicts that user's oldest
session.

## Identifying Federated Accounts

An account either holds a password in the Workbench or signs in through
the identity provider, and the console, the server command line and the
REST API each report which. None of the three reports the provider
subject, which is a long opaque identifier that tells an administrator
nothing, so only the issuer is shown; the subject appears only in the
output of `-link-oidc-user`, which echoes back the value the operator
has just supplied on the command line.

### In the Console

The `Users` page of the `Administration` console marks a federated
account in its `Type` column.

Such an account carries a `Federated` chip, with the issuer on one line
beneath the chip; a long issuer is shortened to fit the column and the
full value appears in a tooltip. A local account shows its usual type,
exactly as before.

Opening a federated account in the `Edit user` dialog replaces the
password field with a notice explaining why the field is absent. The
notice names the provider the account signs in through, says that the
account has to be unlinked before it can sign in locally, and warns that
the account's group membership, along with its superuser flag wherever
`superuser_group` grants one, is reconciled at every sign-in. The
`Superuser` toggle stays available, because an administrator may still
grant the flag, subject to the reconciliation described below.

### At the Command Line

The `-list-users` command reports the same thing in its `Authentication`
column, which is the quickest way to audit every account at once.

The following listing shows two federated accounts and four local ones:

```console
./bin/ai-dba-server -list-users
Auth store: /var/lib/ai-workbench/data/auth.db

Users:
=================================================================================================================
Username             Created           Last Login        Status               Notes                Authentication
-----------------------------------------------------------------------------------------------------------------
Alice                2026-06-10 13:24  Never             Enabled              Developer            https://idp.example.com
Bob                  2026-06-10 13:31  Never             DISABLED             developer            Local
Carol                2026-06-10 13:32  Never             Enabled              Management           https://idp.example.com
Dan                  2026-06-10 13:37  Never             Enabled              sales                Local
admin                2026-06-09 11:59  2026-06-10 12:27  Enabled              management           Local
inventory            2026-06-10 13:31  Never             Enabled              Software             Local
=================================================================================================================
```

The column reads `Local` for an account that authenticates here, and the
issuer for an account the provider owns. The column comes last and is
printed in full, so two issuers that differ only near the end, such as
two tenants or realms of the same provider, remain distinguishable.
The column reads `OIDC` in the rare case of an account marked as
federated whose stored identity cannot be parsed, which is worth
investigating because a login can no longer match such an account.

### Through the REST API

The user objects returned by `GET /api/v1/rbac/users` and by
`GET /api/v1/rbac/users/{id}/privileges` carry the same information in
two fields.

The following table describes the two fields:

| Field | Description |
|-------|-------------|
| `auth_source` | Always present; reads `local` for an account that authenticates here and `oidc` for one the provider owns. |
| `auth_issuer` | Present only for a federated account; holds the issuer URL of the identity provider. |

An account stored before the `users.auth_source` column existed reports
as `local`, so the field is safe to branch on without a fallback.

### Why a Superuser Flag Keeps Reverting

A superuser flag that an administrator grants and that is gone again
shortly afterwards belongs to a federated account whilst
`superuser_group` is configured.

The flag is reconciled at every federated login from the provider group
named by `superuser_group`, as described in Granting Superuser from a
Provider Group above, so a grant made in the console or with
`-update-user` survives only until the person next signs in. Check the
`Authentication` column or the `Federated` chip first: where the account
is federated, grant the flag by adding the person to the provider group,
and where the account is local, the flag stays as it was set.

### Who Still Signs In Locally

Every account whose authentication reads `Local` depends on
`http.auth.local.enabled`, so review that list before setting the option
to `false`.

Turning local login off locks out every such account, including the
break-glass administrator described below, and the setting takes effect
only at a restart, so the mistake is discovered at the worst moment.
Note that a service account also reports as `Local` although the account
never signs in interactively; a service account reaches the Workbench
with an API token, which federated login does not change.

## Federated Accounts in the Console

Federated accounts appear on the
[Account Management](accounts.md) page alongside local ones, and are
administered there in the same way: they can be disabled, enabled,
deleted, added to groups and given API tokens.

One difference matters. A federated account cannot be given a working
password, because password authentication is refused for any account
whose identity the provider owns, so the `Edit user` dialog offers no
password field for one. Unlink the account, as described in Unlinking
an Account above, to return the account to local login.

## Troubleshooting

Federated login failures are deliberately opaque in the browser; the
server log carries the real reason. Check the log first.

### Every Login Is Refused Immediately

A username claim that breaks the Workbench username rule refuses the
login before any account is touched, and logs a diagnostic naming the
claim. Set `username_claim` to `sub`, or to another claim whose values
satisfy the rule.

A non-empty `allowed_email_domains` list also refuses a login when the
provider sends no email address, sends one the Workbench cannot use, or
has not set `email_verified`. Check that the requested scopes include
whatever the provider requires before it releases a verified address.

### A Pre-Created Account Is Not Recognised

A refusal logged as `no account for federated identity` means no
account carries that issuer and subject. With `provision_users` set to
`false` this is the expected first attempt: link the account as
described in Linking an Existing Account. With provisioning enabled it
means the account could not be created, usually because the asserted
username is already taken by another account, which is also logged.

### The Login Button Does Nothing

A button that returns to the login page means federated login is not
active on the server. Confirm that `oidc.enabled` is `true` and that
start-up reported `OIDC login: ENABLED` rather than a discovery
failure.

### The Provider Rejects the Redirect

A provider that refuses the redirect URI, usually with an error on its
own error page, has a registered URI that differs from `redirect_url`.
Compare the two character for character, including the scheme, the
host, any port and any reverse proxy path prefix.

### Groups Are Not Applied

A group that is asserted but not applied is usually a name that
`group_map` does not list, or a mapped Workbench group that does not
exist; both are logged. A groups claim of an unexpected shape, or with
values the Workbench refused, is logged too, and the login proceeds
with fewer groups than the provider sent.

A group that was applied and is not being removed is usually the
nesting problem described in Keeping Mapped Groups Flat.
