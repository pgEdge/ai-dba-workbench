# Documentation Writer Knowledge Base

This directory records where documentation lives in the pgEdge AI DBA
Workbench repository. The style rules themselves are not kept here: the
`pgedge-skills:pgedge-docs` plugin skill is the authoritative pgEdge style
guide and is preloaded into the documentation-writer agent. The
repo-specific deviations from that skill are listed in the agent prompt at
`.claude/agents/documentation-writer.md` and in `CLAUDE.md`.

## Layout

Documentation is organised by task rather than by sub-project, and the
navigation is defined in the `nav` section of `mkdocs.yml`:

| Content                               | Location                                  |
|---------------------------------------|-------------------------------------------|
| Site entry point                      | `docs/index.md`                           |
| Installation and first steps          | `docs/getting-started/`                   |
| Per-component configuration           | `docs/configuration/<component>/`         |
| Web client and systemd setup          | `docs/configuration/*.md`                 |
| Using the web client and tools        | `docs/user-guide/`                        |
| Administration, API, operations       | `docs/admin-guide/`                       |
| Notification channel pages            | `docs/admin-guide/notification-channels/` |
| Troubleshooting, one page per problem | `docs/troubleshooting/`                   |
| Architecture and contributing         | `docs/developer-guide/`                   |
| Per-sub-project developer pages       | `docs/developer-guide/<subproject>/`      |
| Changelog                             | `docs/changelog.md`                       |
| Licence                               | `docs/LICENSE.md` and `/LICENSE.md`       |
| Sub-project README                    | `/<subproject>/README.md`                 |
| Top-level README                      | `/README.md`                              |

The static OpenAPI file at `docs/admin-guide/api/openapi.json` is generated
with `cd server && make openapi`; do not edit it by hand. The endpoint
summary table in `docs/admin-guide/api/reference.md` is maintained
manually alongside it.

## Conventions Specific to This Repository

- README footers link to `docs/developer-guide/contributing.md` for
  contributions, `https://docs.pgedge.com` for online documentation and
  `LICENSE.md` for the licence.
- Each README's table of contents mirrors the `mkdocs.yml` nav section.
- Filenames under `docs/` are lowercase with hyphens between words.
- Prose wraps at 79 characters; long URLs and nav paths are exempt.
- Alerter configuration keys are documented in three places that must
  agree: `docs/configuration/alerter/configuration_file.md` (reference),
  `docs/developer-guide/alerter/anomaly-detection.md` (design) and the
  example configs `examples/ai-dba-alerter.yaml`,
  `docker/config/ai-dba-alerter.yaml` and
  `examples/walkthrough/config/ai-dba-alerter.yaml`.
  `TestExampleConfigsParse` in `alerter/src/internal/config/config_test.go`
  parses all three examples, so run it after editing them.

- Server configuration keys, flags and environment variables are documented in
  `docs/configuration/server/` (reference: one page each for configuration file
  properties, command-line options and environment variables) and in the
  example configs `examples/ai-dba-server.yaml` (complete),
  `docker/config/ai-dba-server.yaml` and
  `examples/walkthrough/config/ai-dba-server.yaml` (minimal). A change that
  breaks a 1.0.0 installation also needs an entry under Breaking Changes in
  `docs/admin-guide/upgrading-to-2.0.md`. Validation of monitored connection
  fields (`host`, `hostaddr`, SSL paths) is described in
  `docs/admin-guide/connections.md` and in the OpenAPI field descriptions in
  `server/src/internal/api/openapi.go`.

- Collector configuration keys are documented in
  `docs/configuration/collector/configuration_file.md` (reference) and in the
  example configs `examples/ai-dba-collector.yaml` and
  `docker/config/ai-dba-collector.yaml`. The ColdFront metrics storage mode
  (`use_coldfront_for_metrics`) has its own page,
  `docs/configuration/collector/coldfront-metrics.md`, which the retention and
  purge sections in `docs/admin-guide/probes/index.md`,
  `docs/admin-guide/connections.md` and `docs/developer-guide/collector/` link
  to; keep them in agreement. The table in
  `docs/troubleshooting/coldfront-errors.md` quotes the start of each startup
  error in `collector/src/database/coldfront_check.go` and
  `collector/src/database/coldfront_insert.go`; update it when those messages
  change.

- `connection_security` is documented for both components and must agree across
  them: the internal-range table, the connect-time address checks and the
  server `ssl_cert_dir` live in the "Connection Security Properties" section of
  `docs/configuration/server/configuration_file.md`; the collector's
  `ssl_cert_dir` and its fixed connect-time block list
  (`pkg/hostvalidation/dial_guard.go`) live in the "Specifying Connection
  Security Options" section of
  `docs/configuration/collector/configuration_file.md`.
  `docs/admin-guide/connections.md` and the "Connection SSL Files" and
  "Connection Host Addresses" sections of
  `docs/admin-guide/upgrading-to-2.0.md` summarise both; update all four when
  `pkg/hostvalidation` or `pkg/sslpaths` changes.

- The alerter's notification delivery address check
  (`notifications.allow_internal_networks`, implemented in
  `alerter/src/internal/notifications/dial_guard.go` on top of
  `pkg/hostvalidation/dial_guard.go`) is documented in the "Delivery
  Address Checks" section of
  `docs/configuration/alerter/configuration_file.md`, summarised in the
  "Checking Delivery Addresses" section of
  `docs/admin-guide/notification-channels/index.md`,
  the "Notification Delivery Addresses" section of
  `docs/admin-guide/upgrading-to-2.0.md` and the internal-range note in
  `docs/configuration/server/configuration_file.md`; keep all four in
  agreement. The quoted refusal text is
  `hostvalidation.ErrNoPermittedAddress`.

- The server's "send test notification" address check
  (`server/src/internal/api/test_send_guard.go`: the always-blocked
  floor, then `connection_security` via `HostValidator.CheckDialAddr`;
  no proxy, no redirects) is documented in the "Test Sends" section of
  `docs/admin-guide/notification-channels/index.md`, including the refusal
  status and message table and the warning that test sends and
  deliveries use different settings. That mismatch is repeated in the
  alerter page's "Delivery Address Checks", the server page's
  `connection_security` section and the upgrade guide's "Notification
  Delivery Addresses"; keep all four in agreement.

## Maintenance

Update this file in the same change whenever the documentation layout,
generated files or README conventions change. A stale entry is worse than
none.
