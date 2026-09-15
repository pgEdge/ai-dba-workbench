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

A login that fails for any reason returns the browser to the login page
with a generic marker, and the real reason goes to the server log.
Nothing derived from the provider's response reaches the browser.

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

The callback endpoint is rate limited per client address. Behind a
proxy, every request carries the proxy's address unless
`http.trusted_proxies` names it, so the limit collapses to a single
allowance shared by the whole deployment, which any unauthenticated
party can spend deliberately to deny everyone else a login. On a
deployment where local login is switched off, that is the only way in.
The server prints a warning at start-up when the list is empty and
federated login is enabled.

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
check the origin. The control that stops an authorisation code being
sent somewhere else is the provider's own list of registered redirect
URIs, so keep that list tight.

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

4. Ask the person to sign in again. The login now resolves to the
   linked account, and group reconciliation runs against it.

The command reports the account, the issuer, the subject and the stored
external subject key, and notes that password login is refused for the
account from that moment.

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

!!! warning

    Do not link the local break-glass administrator account. Keep it
    local, keep its password, and let the provider grant superuser to
    separate federated accounts through `superuser_group`.

Where an existing account must be linked, confirm before linking that
the provider asserts every group the account needs, including the
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

By default, unlinking also replaces the account's password hash with an
unusable one, so the account leaves federation reachable by nothing at
all: no federated login, because the identity is detached, and no
password login, until an administrator sets a password with
`-update-user -username <name>`. That is the same arrangement
provisioning makes when it creates a federated account, and it means an
unlink cannot quietly revive a credential.

Pass `-restore-password` to keep the password the account held before it
was linked, for an administrator who means to hand the account back to
its holder:

```bash
./bin/ai-dba-server -config /etc/pgedge/ai-dba-server.yaml \
    -unlink-oidc-user -username alice -restore-password
```

Weigh that option carefully. The hash has been inert for the whole
federated period, so the password it holds may be years old, will not
have been rotated while the account was federated, and may be known to
the person who has just left. An account the provider originally
created holds a hash of discarded random bytes, so `-restore-password`
on one of those restores nothing usable.

For a person who has left, disable the account with
`-disable-user -username <name>` rather than relying on the unlink
alone; disabling refuses the account's logins and its API tokens
immediately.

The command reports which of the two outcomes applied, and the server
log records it as well.

### Linking, Unlinking and Live Sessions

Both commands invalidate every session the account holds, so a change
takes effect at once rather than at the end of a session's 24-hour
life.

This matters most when relinking. Moving an account to a new subject
without cutting the sessions would leave whoever signed in under the
old identity holding the account's privileges, superuser included,
until their session expired. API tokens are unaffected by either
command, so review them separately.

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

Keep at least one local superuser account with a password, and keep
`local.enabled` set to `true`.

The identity provider is a dependency of every federated login, so an
expired client secret, a rotated signing key, a failed discovery
endpoint or an outright provider outage locks out every federated user
at once. Discovery failure is fatal at start-up, so a server restarted
during a provider outage does not come up at all with federated login
enabled. A local administrator who can still sign in is the difference
between editing the configuration calmly and rebuilding access under
pressure.

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

## Federated Accounts in the Console

Federated accounts appear on the
[Account Management](accounts.md) page alongside local ones, and are
administered there in the same way: they can be disabled, enabled,
deleted, added to groups and given API tokens.

Two differences matter. A federated account cannot be given a working
password, because password authentication is refused for any account
whose identity the provider owns, so setting a password on one has no
effect. The account listing also does not report which accounts are
federated, so use the provider as the record of who holds one until
that information is exposed.

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
