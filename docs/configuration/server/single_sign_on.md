# Configuring Single Sign-On

The Workbench can delegate interactive login to an OpenID Connect identity
provider, so that users sign in with the account they already hold at the
provider rather than with a Workbench password. This page describes how a
federated login works, how to configure the server and register the Workbench
at the provider, and how to map provider groups onto Workbench groups.

!!! hint

    [Managing Single Sign-On](../../admin-guide/managing-users-and-permissions/sso.md)
    describes how to link and identify federated accounts, and the operational
    limits an administrator needs to know about before turning federated login
    on.

Federated login changes only how a person proves who they are.
Authorisation is unchanged: a federated user reaches connections, MCP
tools and administrative operations through exactly the same groups,
privileges and token scopes as a local user, and the
[permission model](../../admin-guide/managing-users-and-permissions/permission_model.md) applies to both in the
same way.

Federated login is also a browser flow, so it does not help a headless
MCP client. Service-account API tokens remain the mechanism for
machine-to-machine access, as described in
[Managing Tokens](../../admin-guide/managing-users-and-permissions/tokens.md); by default a federated user
cannot own API tokens at all, as described in
[API Tokens and Federated Users](../../admin-guide/managing-users-and-permissions/sso.md#api-tokens-and-federated-users).
Full OAuth 2.0 support for MCP clients is out of scope.

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
4. The server exchanges the authorisation code for an ID token and,
   where the provider issues one, a refresh token; it verifies the ID
   token and reads the configured claims from it.
5. The server matches the identity to a Workbench account, reconciles
   the account's mapped group memberships, and issues a session that
   expires when the ID token does, as described in
   [Federated Sessions Follow the Provider](../../admin-guide/managing-users-and-permissions/sso.md#federated-sessions-follow-the-provider).

The account is matched on the issuer and the `sub` claim together, held
in the `users.external_subject` column, and never on the username:
matching on the username would let an identity whose username claim is
`admin` take over the local `admin` account.

A failed login takes one of two shapes, and which one an operator sees
says where the failure happened. The callback answers `400` with a
generic JSON error when the request never reached the provider
successfully: no state cookie, a state cookie that does not open, a
state parameter that does not match the sealed one, a login state that
has already been accepted once, or neither an authorisation code nor a
provider error, since the provider's `error`
parameter is read first and takes the redirect branch below. It
redirects the browser back to the login page with a `login_error`
query parameter when the request did come back
from the provider: `login_error=provider` when the provider itself
declined, which includes the person cancelling at the consent screen,
and `login_error=login` when the code exchange or any later step
refused the login. Either way the real reason goes to the server log
only, and nothing derived from the provider's response reaches the
browser.

## Configuring the Server for Federated Login

Configure federated login in the `http.auth` section of
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

Sending `SIGHUP` re-reads the configuration file, and the server logs
each changed authentication setting as either applied or requiring a
restart. The login policy applies from the next login without a
restart: `provision_users`, `allowed_email_domains`, `superuser_group`,
`group_map`, `button_label`, `auth_params`, and setting `enabled` to
`false`, which
switches federated login off at once and refuses a login that is
already in progress. A reload therefore lets you stop just-in-time
provisioning, narrow the allowed email domains or remap a Workbench
group away from a compromised provider group whilst containing an
incident. The changes take effect at each person's next login, as
described in
[Revocation and Its Limits](../../admin-guide/managing-users-and-permissions/sso.md#revocation-and-its-limits), and a Workbench
group
removed from `group_map` altogether stops being reconciled and keeps
its current members, so remap the group rather than removing it when
the aim is to revoke membership. Clearing `superuser_group` likewise
revokes nothing: federated users keep their current superuser status,
and no later login changes it, so point the setting at a group that no
one belongs to when the aim is to demote every federated superuser.

The identity provider decides the superuser flag, so a login is not
refused when removing the flag leaves no enabled superuser; the server
logs a warning instead. To restore access, add the account you need to
the superuser group at the provider, or add a local superuser with the
`-add-user` and `-set-superuser` command line options.

Switching federated login off stops new federated logins, and a
federated session already established ends when its ID token expires,
because the server no longer refreshes it. A session whose ID token
has not yet expired stays valid until then, and so do API tokens
already issued where `allow_api_tokens` is on, so follow the steps in
[Revocation and Its Limits](../../admin-guide/managing-users-and-permissions/sso.md#revocation-and-its-limits) to cut them off
at once. When the server started with `local.enabled` set to
`false`, a reload that switches federated login off is refused and the
previous configuration is kept, because local login cannot be switched
back on without a restart and the server would otherwise be left with
no way to sign in.

The server logs a warning, as well as the note that the change applied,
when a reload widens access or looks like a revocation without being
one: setting `enabled` back to `true`, switching `provision_users` on,
emptying `allowed_email_domains` or adding a domain to it, setting,
changing or clearing `superuser_group`, mapping a provider group in
`group_map` to a Workbench group it was not mapped to before, or
removing a Workbench group from `group_map`, or switching
`allow_api_tokens` on.

The identity provider connection is built once, at start-up, from
`issuer`, `client_id`, `client_secret`, `client_secret_file`,
`redirect_url`, `scopes` and the three claim names, so a change to any
of those requires a restart. Setting `enabled` to `true` applies on
reload only when the server started with federated login switched on;
otherwise no provider was discovered and the server must be restarted.
`local.enabled` and `max_failed_attempts_before_lockout` also require a
restart.

The server compares the identity provider settings, and whether
federated login was switched on, with the configuration it started
with, so a change to one of them is reported as needing a restart on
every reload until the server restarts or the setting is restored.

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
below is ignored while `enabled` is `false`, although the server still
refuses an `auth_params` entry that names a parameter it sets itself.

The following table describes the federated authentication options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `oidc.enabled` | bool | `false` | Enables federated login. |
| `oidc.issuer` | string | | Issuer URL of the identity provider; must use `https`, and discovery runs against it at start-up. Required. |
| `oidc.client_id` | string | | OAuth 2.0 client identifier registered at the provider. Required. |
| `oidc.client_secret` | string | | Client secret supplied inline in the configuration file. |
| `oidc.client_secret_file` | string | | Path to a file holding the client secret, used when `client_secret` is empty. |
| `oidc.redirect_url` | string | | Absolute URL the provider returns the browser to; its path must end with `/api/v1/auth/oidc/callback`. Required. |
| `oidc.scopes` | list | `[openid, email, profile]` | Scopes requested at the provider; `openid` is added when absent, and so is `offline_access` unless the provider's discovery document lists its supported scopes without it. |
| `oidc.username_claim` | string | `email` | Claim whose value becomes the Workbench username. |
| `oidc.display_name_claim` | string | `name` | Claim whose value becomes the account's display name. |
| `oidc.groups_claim` | string | `groups` | Claim carrying the user's provider group names. |
| `oidc.button_label` | string | `Sign in with your identity provider` | Label of the federated login button. |
| `oidc.provision_users` | bool | `false` | Creates a Workbench account the first time an unknown identity logs in. |
| `oidc.allowed_email_domains` | list | `[]` | Restricts login to verified addresses in these exact domains; empty means no restriction. |
| `oidc.superuser_group` | string | | Provider group whose members hold the Workbench superuser flag; empty leaves the flag under local control. |
| `oidc.group_map` | map | `{}` | Maps provider group names to Workbench group names. |
| `oidc.allow_unprefixed_state_cookie` | bool | `false` | Accepts a login state cookie without the `__Host-` prefix behind a proxy; see [Requiring a Trusted Proxy List](#requiring-a-trusted-proxy-list). |
| `oidc.allow_api_tokens` | bool | `false` | Lets accounts that sign in through the provider own API tokens; see [API Tokens and Federated Users](../../admin-guide/managing-users-and-permissions/sso.md#api-tokens-and-federated-users). |
| `oidc.auth_params` | map | `{}` | Extra parameters appended to the authorisation request; see [Adding Authorisation Request Parameters](#adding-authorisation-request-parameters). |

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
deployment it always does. The setting is required, not advisory, for a
production deployment behind a proxy; without it, any anonymous caller
can block federated login for every user, and the login state cookie
loses the protection that stops login CSRF from a sibling subdomain.

The start and callback endpoints are each rate limited per client
address, with an allowance of 240 requests a minute; a completed login
hands back the one unit it spent on each, so the limits bound failed
and abandoned logins rather than working ones. Every `GET` request to
the start endpoint spends one unit of the start allowance while
federated login is enabled; a request with another method, or one made
whilst federated login is disabled, is answered before the limit is
checked and costs nothing. The callback allowance is spent only by
requests that reach the identity provider, so a request refused
before that point costs nothing; such requests include one with no
login state cookie, one with a mismatched state and one that replays a
state already accepted.

Each login state is accepted by the callback once only. The server
remembers a digest of each state the callback has accepted for as long
as the state could still be opened, which is the ten minute lifetime of
the state plus a minute of clock skew. A captured state cookie cannot
therefore be replayed with further authorisation codes. The record
holds up to 100,000 states at a time; under a sustained flood the
oldest are forgotten early, and each forgotten state could then be
presented once more. The record is held in the server's memory, so a
restart forgets it and a state accepted before the restart could be
presented once more until it expires; a deployment that runs several
server processes with the same server secret refuses a replay only at
the process that first accepted the state.

Behind a proxy, every request carries the proxy's address unless
`http.trusted_proxies` names it, so each limit collapses to a single
allowance shared by the whole deployment. An anonymous caller can then
spend that allowance with repeated requests to the start endpoint and
deny everyone else a login until the window passes; on a deployment
where local login is switched off, federated login is the only way in.
Setting `http.trusted_proxies` stops the allowance being shared across
every client, because each limit then applies to the caller's own
address.

The same list decides how far the server believes the
`X-Forwarded-Proto` header. Every cookie the server sets carries the
`Secure` attribute whenever the header says `https`, whichever address
the request came from, because a forged header can only cost the forger
their own cookie. The login state cookie additionally uses the
`__Host-` name prefix, which stops a sibling subdomain overwriting the
cookie, but only when the server terminates TLS itself or when the
header arrived from an address on `http.trusted_proxies`. Without the
list, a server behind a TLS-terminating proxy writes the cookie under
its plain name. A host that can set cookies for a sibling subdomain can
then plant the attacker's own login state over the user's cookie and
send the user's browser to the callback with the attacker's code; the
user ends up logged in to the attacker's account. The attack is login
CSRF (cross-site request forgery): the attacker gains no access to the
user's own account, but the user may enter data into an account the
attacker controls.

The server therefore refuses to start when federated login is enabled,
`redirect_url` uses `https`, `http.tls.enabled` is `false` and
`http.trusted_proxies` is empty, which is a TLS-terminating proxy with
no trusted proxy list. Set `http.trusted_proxies` to fix it, or, if
no sibling subdomain can be used to set cookies for the parent domain,
set `oidc.allow_unprefixed_state_cookie` to `true` to accept the risk.
A `redirect_url` using `http` on a loopback host is exempt, because a
browser never keeps a `__Host-` cookie over plain HTTP. A `SIGHUP`
reload applies the same rule and keeps the previous configuration when
it fails.

When the list is empty and federated login is enabled, the server
prints a warning at start-up about the shared rate-limit allowance;
when `http.tls.enabled` is also `false`, a second warning names the
login CSRF risk.

## Registering the Workbench at the Provider

Register the Workbench as a confidential OAuth 2.0 client at the
identity provider, with the authorisation code flow and PKCE enabled.

The provider needs the following from the Workbench:

- the redirect URI, which is the public URL of the Workbench with
  `/api/v1/auth/oidc/callback` appended, for example
  `https://workbench.example.com/api/v1/auth/oidc/callback`.
- the scopes the Workbench requests, which are `openid`, `email` and
  `profile` by default, plus whatever scope the provider requires
  before it releases group membership, and `offline_access`, which the
  Workbench adds to ask for a refresh token unless the provider's
  discovery document publishes a `scopes_supported` list that leaves
  it out.
- permission to issue refresh tokens to the client, which some
  providers grant only when it is switched on in the client's
  registration; without a refresh token every federated session ends
  when its first ID token expires.
- a claim carrying group membership, if group mapping or a superuser
  group is to be used.

The provider issues a client identifier and a client secret in return,
which become `client_id` and `client_secret_file`.

The redirect URI registered at the provider and the `redirect_url` in
the configuration must be the same string, and both must be the URL the
browser actually reaches, not the server's internal address. Where TLS
terminates at a reverse proxy, that is the proxy's public URL; see
[TLS and Reverse Proxy Requirements](../../admin-guide/tls-and-reverse-proxy.md) for
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

### Adding Authorisation Request Parameters

Some providers need a parameter in the authorisation request before
they behave as the Workbench expects. The `auth_params` option takes a
map of parameter names to values, and the Workbench appends each one
to the URL it sends the browser to when a login starts.

Google is the common case: it does not support the `offline_access`
scope, which its discovery document does not list, so the Workbench
does not request it. Google issues a refresh token only to a request
that carries `access_type=offline`, and only when the user grants
consent, which it skips after the first login unless the request
carries `prompt=consent`. Without both, a federated session ends when
its first ID token expires, typically after an hour. The following
example configures them:

```yaml
http:
  auth:
    oidc:
      enabled: true
      issuer: "https://accounts.google.com"
      client_id: "pgedge-workbench.apps.googleusercontent.com"
      client_secret_file: "/etc/pgedge/oidc-client-secret.txt"
      redirect_url: "https://workbench.example.com/api/v1/auth/oidc/callback"
      auth_params:
        access_type: "offline"
        prompt: "consent"
```

With `prompt: consent`, Google shows its consent screen at every
login rather than only the first.

The Workbench sets some parameters itself, and refuses to start, or to
apply a reload, when `auth_params` names any of them: `state`,
`nonce`, `code_challenge`, `code_challenge_method`, `redirect_uri`,
`client_id`, `response_type`, `scope`, `response_mode`, `request` and
`request_uri`. The comparison ignores case. Request scopes through the
`scopes` option instead. Parameter names may contain only letters,
digits, `_`, `-` and `.`; a value may not contain control characters
or exceed 256 bytes, and the map may hold at most 16 entries. A reload
applies a change to `auth_params` from the next login.

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

Point `username_claim` at a claim whose values satisfy the rule. The `sub`
claim is also the claim the account is matched on, so it is often the safest
choice on a provider whose email addresses or usernames are not strictly
controlled; OpenID Connect allows a `sub` value of up to 255 characters in any
character set, however, so confirm that the provider's `sub` values fit the
128-character limit and the allowed characters before you set `username_claim`
to `sub`.

## Provisioning and Email Domains

Set `provision_users` to `true` to have the Workbench create an account
the first time an unknown identity logs in. The account is created with
an unusable password hash, so it can never be reached through password
login, and it starts with no groups and no superuser flag until
reconciliation grants them. The new account is recorded in the
[audit log](../../admin-guide/audit-log.md) as `user.create`, attributed to the `system`
actor with a `details.reason` of `identity_provider`, the provider's
issuer in `details.issuer` and the identity's subject in
`details.subject`. If the event cannot be written, the account is not
created and the login fails.

Leave `provision_users` at `false`, which is the default, to pre-create
every account instead. A login by an identity that no account is linked
to is then refused, and an administrator links each account to its
provider identity with the server command line, as described in
[Linking an Existing Account](../../admin-guide/managing-users-and-permissions/sso.md#linking-an-existing-account).

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
[Managing Groups](../../admin-guide/managing-users-and-permissions/groups.md), and check the server log after the first
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

Every change reconciliation makes is recorded in the
[audit log](../../admin-guide/audit-log.md), attributed to the `system` actor with a
`details.reason` of `identity_provider` and the provider's issuer in
`details.issuer`: a superuser grant or revocation as `user.update`, and
a mapped group membership as `group.member.add` or
`group.member.remove`. A login that changes nothing records nothing. A
grant whose event cannot be written is rolled back and the login fails,
whereas a revocation stands even when its event cannot be written, and
the failure is reported in the server log.

Setting `superuser_group` hands out more than the flag's name suggests,
and the server prints a warning at start-up whenever it is configured. A
superuser bypasses every group grant, reaching every connection, every
MCP tool and every administrative permission from a browser session. An
API token the account owns is bound by the scope of that token, so a
scoped token stays within its scope, whereas an unscoped token carries
the same full privilege as the session. `superuser_group` changes who
can hand out that privilege, from a Workbench administrator to anyone
able to add a member to one provider group. Revocation is no faster than
any other mapped group: the flag is withdrawn at the person's next
login, and until then a browser session they already hold keeps full
privilege, as does an unscoped API token they own, whilst a scoped token
keeps whatever its scope allows; disable the account to revoke any of
them sooner. Leave the option empty unless the provider's group
administrators are trusted to that extent.

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
