# Upgrading the Workbench

This page describes how the Debian and RPM packages protect an
existing installation during an upgrade, and what to do when an upgrade
is refused because of the installed server configuration.

## Checking the Server Configuration Before an Upgrade

A new release can refuse a server configuration that an earlier release
accepted. The packages check the installed configuration with the new
server before they replace any installed files; if the new server would
refuse to start, the upgrade stops and the installed version stays in
place and keeps running.

The `pgedge-ai-dba-upgrade-check` package performs the check. The
package holds a copy of the new `ai-dba-server` binary under
`/usr/libexec/pgedge-ai-dba-upgrade-check/`, and every other package of
the suite requires the package at the same version before installation.
The check behaves as follows on each platform:

- On Debian and Ubuntu, the server, collector, alerter and client
  packages pre-depend on the check package, and the check runs when the
  check package is configured; `dpkg` does not unpack the other
  packages until the check succeeds.
- On RHEL-compatible systems, the server, collector, alerter and client
  packages each run the check before `rpm` unpacks any of their files;
  a failed check stops `rpm` installing the new package and removing
  the old one.

The check reads `/etc/pgedge/ai-dba-server.yaml` and runs as the
`pgedge` account from the server's working directory,
`/var/lib/pgedge/ai-dba-server`, so relative paths and file permissions
resolve as they do for the running service. The check does not read
`/etc/pgedge/ai-dba-server.env`, so a setting supplied only through an
environment variable in that file is not checked. A fresh installation
has no configuration file, so the check is skipped.

The first upgrade to a release that includes the check package installs
the package as a new dependency. On Debian and Ubuntu, `apt-get
upgrade` keeps packages back when an upgrade needs a new package;
use `apt upgrade`, `apt-get dist-upgrade` or `apt-get install
pgedge-ai-dba-server` for that first upgrade instead.

## Checking a Configuration by Hand

Run the server with the `-check-config` flag to validate a
configuration file without starting the server. The flag loads the
file and the datastore password file and applies the configuration
validation that start-up applies, but does not connect to the datastore
or the identity provider, and does not open a listening socket. The
command prints `Configuration OK` and exits with status zero when the
configuration is valid; otherwise the command prints the error the
server would report at start-up and exits with status one.

Run the check as the `pgedge` account from the server's working
directory, so that relative paths and file permissions resolve as they
do for the service. In the following example, the command checks the
installed configuration with the installed server:

```bash
cd /var/lib/pgedge/ai-dba-server
sudo -u pgedge -H ai-dba-server \
    -check-config -config /etc/pgedge/ai-dba-server.yaml
```

During a refused upgrade, the new server is already installed by the
check package. In the following example, the command checks the
configuration with the new server before you retry the upgrade:

```bash
cd /var/lib/pgedge/ai-dba-server
sudo -u pgedge -H /usr/libexec/pgedge-ai-dba-upgrade-check/ai-dba-server \
    -check-config -config /etc/pgedge/ai-dba-server.yaml
```

## Recovering from a Refused Upgrade

A refused upgrade prints the server's error, followed by a message
stating that the installed version is unchanged and still running. The
package manager exits with an error, and the check package remains
installed at the new version.

Use the following steps to complete the upgrade:

1. Correct `/etc/pgedge/ai-dba-server.yaml` as the error message
   describes.

2. Check the corrected file with the new server, as shown in
   [Checking a Configuration by Hand](#checking-a-configuration-by-hand).

3. Run the upgrade again with `apt upgrade` or `dnf upgrade`.

On Debian and Ubuntu, the check package is left unconfigured until the
check succeeds. Each later `apt` or `dpkg` run retries the check and
reports an error while the configuration remains invalid, although
unrelated packages still install. To defer the upgrade instead of
fixing the configuration, remove the check package with
`sudo apt-get remove pgedge-ai-dba-upgrade-check`. Removal is
possible only when the installed release predates the check package;
the packages of a later release depend on the package, and removing
the package would remove them as well.

Unattended upgrades with `unattended-upgrades` or `dnf-automatic` fail
in the same way, and keep failing on each run until the configuration
is corrected. The installed version continues to run throughout, but
monitor the logs of the unattended upgrade tool so that a refused
upgrade does not go unnoticed.

## Resolving the Trusted Proxy Error

The server refuses to start, and the check therefore refuses an
upgrade, when federated login runs behind a TLS-terminating reverse
proxy without a trusted proxy list. The configuration matches the
following conditions:

- `http.auth.oidc.enabled` is `true`.
- `http.auth.oidc.redirect_url` uses `https`.
- `http.tls.enabled` is `false`.
- `http.trusted_proxies` is empty.

Set `http.trusted_proxies` to the address ranges of the reverse proxy
to resolve the error; the
[Requiring a Trusted Proxy List](managing-users-and-permissions/sso.md#requiring-a-trusted-proxy-list)
section explains the setting. Setting
`http.auth.oidc.allow_unprefixed_state_cookie` to `true` also resolves
the error, but accepts the login CSRF risk that the rule exists to
prevent.

In the following example, the configuration trusts a reverse proxy at
a single address:

```yaml
http:
  trusted_proxies:
    - 192.0.2.10/32
```
