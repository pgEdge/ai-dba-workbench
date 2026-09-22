# Changelog

All notable changes to the pgEdge AI DBA Workbench are
documented in this file.

The format is based on
[Keep a Changelog](https://keepachangelog.com/), and this
project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Add federated login through an OpenID Connect identity
  provider, configured in the new `http.auth.oidc` section
  and offered as a second button on the login page. The
  Workbench discovers the provider at start-up, runs an
  authorisation code flow with PKCE, matches the account on
  the issuer and `sub` claim, and can provision an account on
  first login. Provider groups map onto Workbench groups
  through `group_map`, and `superuser_group` names one
  provider group that confers superuser; both are reconciled
  at each login, and a group the map does not name is left
  alone. Access may be restricted with
  `allowed_email_domains`, which matches exact domains and
  requires the provider to have verified the address.
  Authorisation is otherwise unchanged, and service-account
  API tokens remain the mechanism for MCP clients. Local
  username and password login is unaffected and stays on
  unless `http.auth.local.enabled` is set to `false`. The new
  Single Sign-On page in the Administrator's Guide covers the
  configuration, provider registration and the operational
  limits, including the need for `http.trusted_proxies`, the
  username rule the chosen claim has to satisfy, and why a
  local break-glass administrator should be kept. The server
  warns at start-up when federated login is enabled without a
  trusted proxy list, when `superuser_group` is set, and it
  names the host that `redirect_url` sends authorisation codes
  to, so that a typo there is caught before the first login
  fails. The authentication database schema moves to version
  4 on the first start after upgrading, adding an
  `auth_source` and an `external_subject` column to `users`;
  every existing installation runs that migration, and the
  database cannot afterwards be opened by an older server.
  (#261)

- Add `-link-oidc-user` and `-unlink-oidc-user` commands to
  the server, which attach an existing account to an identity
  provider subject and detach it again. Linking is how an
  account is reached by a federated login where
  `http.auth.oidc.provision_users` is left at its default of
  `false`: the operator reads the issuer and subject from the
  refused login's server log line, which now quotes both and
  spells out the command, and links the pre-created account.
  Moving an account that is already linked requires the
  `-relink` flag, and a service account cannot be linked at
  all. Linking leaves the account's existing API tokens
  working at full privilege, so an operator tightening an
  account's authentication has to remove them with
  `-remove-token` themselves. Unlinking returns the account to
  local authentication, replaces its password hash with an
  unusable one and revokes its API tokens, so the account is
  reachable by nothing until a password is set with
  `-update-user`; `-restore-password`
  converts the account back to local login instead, keeping
  both the password it held before it was linked and its
  tokens. Neither command ends a browser session the running
  server has already issued, because sessions live in that
  server's memory and the command runs in its own process, so
  offboarding means unlinking and then disabling the account
  with `-disable-user`, which the server honours from the next
  request onward. Note that linking an existing account hands
  its mapped group membership, and its superuser flag wherever
  `superuser_group` is configured, to the identity provider
  from the next federated login onward,
  which is why the local break-glass administrator should not
  be linked. (#261)

- Add a `-group-description` CLI flag that sets a group's
  description when creating it with `-add-group`, matching the
  description support already available in the web console. (#301)

- Add a `-list-members` CLI flag to the server, which lists the
  members of the group named with `-group`, including both member
  users and nested member groups. (#303)

- Add a Monitored Database Privileges page to the configuration
  documentation, describing the PostgreSQL grants a monitoring role
  needs on a monitored instance. The page recommends a
  least-privilege role based on `pg_monitor` plus per-database
  `CONNECT`, covers the optional `pg_stat_statements`, `system_stats`,
  and Spock grants, and documents the coverage gaps that remain
  without superuser access. (#351)

- Add pagination and database filtering to the Top Queries panel on
  the server dashboard. The panel footer now provides a page size
  selector offering 10, 20, 50, or 100 rows, defaulting to 20, along
  with previous and next controls and a "Showing X-Y of Z"
  indicator, so users can page beyond the first rows. The panel
  header shows a database filter when the connection monitors more
  than one database; the filter defaults to "All databases". The
  `GET /api/v1/metrics/top-queries` endpoint gained the `offset` and
  `database_name` query parameters, and now returns an
  `X-Total-Count` response header that reports the total number of
  matching rows; the JSON response body is unchanged. (#335)

- Add a custom time range to the monitoring dashboards and the
  event timeline, alongside the existing `1h`, `6h`, `24h`, `7d`,
  and `30d` presets. The new Custom option opens a picker with
  From and To fields, so that the graphs can be lined up with a
  known incident window rather than a rolling window ending at
  the present moment.
  The `/api/v1/metrics/query` endpoint now accepts
  `time_range=custom` together with RFC 3339 `time_start` and
  `time_end` parameters. The server rejects a span longer than
  366 days and a start at or after the present moment, and it
  clamps an end in the future to the present moment, since a
  picker set to the current day routinely overshoots by a few
  minutes. The `/api/v1/timeline/events` endpoint applies the
  same rules to its `start_time` and `end_time` parameters, and
  the picker applies them too, keeping Apply disabled and naming
  the rule that failed.
  Auto-refresh is suspended whilst a custom window is active,
  because re-fetching a fixed historical window returns identical
  data on every poll; the selector shows a pause indicator, and
  refreshes resume as soon as the user returns to a preset. The
  window is held in memory only, so it is neither persisted nor
  reflected in the page URL, and a reload returns to the last
  preset.
  The time-series charts honour the dashboard window, including
  the charts on the query detail overlay, whilst the event
  timeline honours the independent window set on its own control.
  The query leaderboards and the performance and database summary
  tiles do not yet follow the dashboard selector. (#345)

- Add a Connections section to the server dashboard, which groups
  the server's client connections by database user, client address,
  or database, and breaks each group down into total, active, idle,
  idle in transaction, and other backend states. The counts come
  from the most recent `pg_stat_activity` snapshot within the
  selected time range, and the By Client tab also shows the
  reverse-resolved client hostname where the server recorded one.
  A new `GET /api/v1/metrics/connection-groups` endpoint serves the
  section, returning at most 200 groups alongside a `total_groups`
  count so that a truncated response can be recognised, and it
  honours a custom time range through the same `time_start` and
  `time_end` parameters as the metrics query endpoint. (#346)
- Add an `llm.max_tokens` setting to the alerter, which caps the
  output tokens of a reasoning call. The cap applies to tier 3
  anomaly classification and to acknowledged-alert re-evaluation.
  The setting defaults to `4096` tokens and replaces a hardcoded
  500-token limit. A reasoning model could consume that smaller
  limit entirely on its thinking block, leaving no room for the
  classification verdict. A value of zero or less selects the
  default. (#399)

- Add a `_delta` derived metric to the metrics query API
  (`GET /api/v1/metrics/query`). A metric named `<column>_delta`
  reports the increase of a cumulative counter within each time
  bucket, which suits a bar chart of rare events such as checkpoints;
  a bucket containing no sample reports zero, a connection with no
  samples in the window returns no data points, and the `aggregation`
  parameter does not apply. The `_per_sec` and `_delta` forms compute
  the change of each monitored entity's counter separately before
  adding them together, so a network interface or database that
  appears or disappears between samples no longer produces a spike or
  a stale value, and the sample before the window is only used when
  it is recent enough not to inflate the first bucket. (#400)

- Add per-query detail to the query drill-down on the object
  dashboard. The drill-down now shows minimum and maximum
  execution time as KPI tiles, and adds an average execution
  time tile scoped to the selected time range and labelled with
  that range, such as "Avg Time (Last 24h)". The cumulative
  lifetime average is now labelled "Mean Time (All Time)" so
  that users can tell the two figures apart. The drill-down also
  shows the database role that ran the query beside the query
  text, and displays "Unknown" when the role cannot be resolved.
  The API gained a `GET /api/v1/metrics/query-stats` endpoint
  that returns the period-scoped average execution time for a
  single query, for a preset or a custom time window and
  optionally restricted to one database, and the `username`,
  `min_exec_time`, and
  `max_exec_time` fields on `GET /api/v1/metrics/top-queries`
  rows. The `queryid` parameter of that endpoint and of the new
  one must now be a 64-bit integer; any other value is rejected
  with a `400` instead of matching nothing. (#350)

- Add `_pct` and `_sessions` derived metrics to the metrics query
  API (`GET /api/v1/metrics/query`). A metric named `<column>_pct`
  reports the share of wall-clock time that a cumulative millisecond
  column such as `blk_read_time` advanced by, and `<column>_sessions`
  reports the average number of sessions in a state from the
  `session_time`, `active_time` and `idle_in_transaction_time` columns
  of `pg_stat_database`. Each series in the response now carries a
  `unit` field (`/s`, `ms`, `%`, `sessions` or empty). The collector
  records `pg_stat_statements_info.stats_reset` in a new
  `stats_reset` column of the `pg_stat_statements` probe where the
  extension is version 1.9 or later, as shipped with PostgreSQL 14;
  the column is added by collector schema migration 10. (#402)

- Add client attribution to the query drill-down on the object
  dashboard, which now shows a Last Observed Client value beside
  the Database User value. The value names the client last seen
  running the statement, as "hostname (address)" where the
  monitored server resolved a hostname, and as "local" where
  that client connected over a Unix-domain socket on the
  database host. The client is attributed per database and role
  as well as per query identifier, so a query in one database is
  never credited to a client that only ever connected to
  another. The collector's `pg_stat_activity` probe collects the
  `query_id` column to support this, added to
  `metrics.pg_stat_activity` by schema migration 12, and the rows
  returned by
  `GET /api/v1/metrics/top-queries` gained the nullable
  `client_addr`, `client_hostname`, and `client_observed_at`
  fields. The attribution is best effort, because
  `pg_stat_activity` is sampled periodically: a client is captured
  only for statements that were in flight when a sample was taken,
  and the value names the client seen most recently rather than
  the only one that ran the query; `client_observed_at` is null
  exactly when no sample ever caught the query in flight, which
  is the field to test for that case. Nothing is attributed on
  PostgreSQL releases before 14 or on servers that run with
  `compute_query_id` off, where the drill-down reads "Not
  observed", so operators who want the attribution should enable
  `compute_query_id` on each monitored server. (#384)
- Add an estimated available memory figure to the memory metrics.
  The collector schema migration 13 adds an `available_memory`
  column to `metrics.pg_sys_memory_info`, and the
  `pg_sys_memory_info` probe populates the column with free memory
  plus cached memory, storing NULL whenever either input is
  missing. The Memory Usage chart on the server dashboard plots the
  figure as a fourth series, "Available (est.)", alongside Used,
  Free and Cached, and the memory KPI tile keeps the usage
  percentage as its headline and shows the available figure as a
  secondary line. The value is an estimate rather than the kernel's
  `MemAvailable`, which the `system_stats` extension has never
  exposed; free memory alone reports `MemFree`, which counts only
  memory that is entirely unused, whilst the estimate also counts
  reclaimable page cache, and the estimate runs high on a host with
  a large non-reclaimable slab or a largely dirty page cache. (#429)

- Add Telegram as a notification channel type, alongside the
  existing email, Slack, Mattermost and webhook channels. A
  Telegram channel delivers through the Bot API rather than an
  incoming webhook, so it stores a bot token, encrypted with the
  server secret and never returned by the API, together with a
  chat ID that is either a numeric ID or an `@channelusername`.
  The create and update endpoints accept the `telegram_bot_token`
  and `telegram_chat_id` fields, a response reports
  `telegram_bot_token_set` in place of the token, and
  `POST /api/v1/notification-channels/{id}/test` sends a test
  message to the configured chat. Notifications are sent with the
  `sendMessage` method in HTML parse mode, so a Telegram template
  renders the message text rather than a JSON body. A message beyond
  Telegram's 4096-character limit is truncated without cutting an HTML
  entity or an opening tag in half, and a message Telegram refuses to
  parse is sent once more with no parse mode, so a template with
  broken markup delivers the alert as plain text rather than losing
  it. The admin panel gains a Telegram Channels tab, and collector
  schema migration 14 adds the `telegram_bot_token_encrypted` and
  `telegram_chat_id` columns to `notification_channels`. (#475)
- Add an audit log for RBAC changes. Every create, update, delete,
  enable, disable, grant, revoke and scope change on users, tokens,
  groups and permissions is recorded in the auth store with the
  acting user, token or CLI user, the client address, before and
  after state and a tamper-evident hash chain, and authorisation
  denials on RBAC endpoints are recorded too. Superusers can read
  the log from the new Audit Log tab in the admin panel, from
  `GET /api/v1/rbac/audit`, or with the `-list-audit` server flag;
  `-verify-audit-log` checks the chain and also reports events
  deleted from the newest end of the log. Repeated denials are
  coalesced into one event per minute carrying a
  `details.repeat_count`, so a client retrying a refused request
  cannot flood the log. Retention is controlled by the new
  `http.auth.audit_retention_days` setting, which defaults to 90
  days, and each purge records an `audit.purge` event naming the
  cutoff it applied and the number of events it removed. Each
  event's hash length-prefixes every field it covers, so a value
  containing a separator character cannot be made to stand for a
  different pair of columns, and records the version of that encoding
  in a `hash_version` field so that a later format change leaves
  earlier events verifiable. `prev_hash` carries a unique index, so no
  two events can claim the same predecessor and the chain cannot fork;
  the server re-creates that index and the append-only trigger every
  time it opens the store, and `-verify-audit-log` refuses to pass a
  log missing either. An account locked out after repeated failed logins
  stays locked even when the audit write fails, because that event
  is recorded after the lockout commits rather than alongside it;
  every other audited change is still rolled back when its event
  cannot be written. (#65)

- Add a Top Queries section to the Database Dashboard, listing the
  slowest statements recorded for the database you are viewing,
  ordered by total execution time. The section follows the
  dashboard's time range, pages through the results twenty rows at
  a time by default, and hides the Workbench's own monitoring
  queries unless you switch that off. Selecting a row opens the
  existing query detail overlay. The Server Dashboard's Top
  Queries section is unchanged, and keeps its database filter.
  (#369)

### Changed

- Change the alerter's default Gemini reasoning model from
  `gemini-2.5-flash` to `gemini-3.6-flash`. Google no longer offers
  `gemini-2.5-flash` to new API keys, answering every request with a
  404 that points at `gemini-3.6-flash`, so a fresh deployment that
  left `llm.gemini.reasoning_model` unset failed on every reasoning
  call. This is a behaviour change for existing deployments: one that
  leaves `reasoning_model` unset now uses `gemini-3.6-flash`, and an
  operator who needs to stay on `gemini-2.5-flash` should set it
  explicitly. The example configuration and the walkthrough script
  are updated to match. (#459)

- Report the queried time window in the `/api/v1/metrics/query`
  response. The endpoint returned a bare array of series and said
  nothing about the window behind the series, so charts derived
  their x-axis from the points that came back and every time range
  rendered identically on an instance holding little history. The
  bucketed response is now an object carrying `probe_name`,
  `connection_ids`, `time_range`, `time_start`, `time_end`,
  `bucket_seconds`, `buckets` and `aggregation` alongside the
  `series` array. A bucket that collected no data, and that has no
  earlier value to carry forward, now reports a null `value`
  instead of dropping the point, and a point carried forward from
  an earlier sample is marked `filled`. The latest-rows mode of
  the same endpoint, which a request selects by passing `limit` or
  `order_by`, is unchanged. The dashboard charts now anchor their
  x-axis to the returned window, draw null buckets as gaps, and
  mark carried-forward stretches distinctly. (#430)

- Enforce an API token's connection scope on connections that
  belong to no group. Both the access check and the visible
  connection list previously admitted an ungrouped connection
  on ownership or the shared flag alone, without consulting
  the caller's token scope, so for those connections a
  token's connection scope was dead configuration on every
  surface. Both paths now intersect the scope, denying an
  out-of-scope connection and applying the scope's access
  ceiling otherwise. This is a real change for anyone handing
  out narrowly scoped tokens: a token that has been reaching
  an ungrouped connection outside its scope, or reaching one
  at `read_write` whilst scoped to `read`, will now be
  refused or capped. Review the scope of every token that
  touches a connection with no group before upgrading. The
  change does not reach a token owned by a superuser: the
  superuser check returns before the scope is consulted, so
  such a token still reaches every connection at `read_write`
  whatever its scope says, which is the pre-existing bypass
  tracked in #482. A narrow automation credential should
  therefore be minted from a service account or an ordinary
  user, not from an administrator. (#261)

- Serve `GET /api/v1/capabilities` without authentication. The
  endpoint previously required a session or API token, although
  the login screen has to read it before either exists in order
  to learn which sign-in methods to offer, so it now joins the
  public paths alongside login, logout and the health check. It
  reports the AI feature flag, the iteration limit and the new
  `auth` block, which carries only `local_enabled`,
  `oidc_enabled` and the button label; the provider issuer,
  client secret and claim mapping are never included. (#261)

- Log sessions out when the authentication database cannot be
  read. A session whose user record failed to load previously
  produced a context carrying the username with no user ID
  and no superuser flag, which quietly narrowed what the
  session could do whilst still authenticating it. Such a
  session is now rejected outright, so a database blip ends
  sessions rather than silently changing their privileges.
  The failure is logged with the database error, and is told
  apart from a deleted account and from a plainly bad token,
  so a run of refused requests can be traced to its cause.
  (#261)

- Require a restart for every change under `http.auth`. The
  settings in that block, `local.enabled` and the whole `oidc`
  section included, are read once at start-up and held for the
  life of the process, so sending `SIGHUP` re-reads the file
  and reports each changed authentication setting as needing a
  restart but leaves the running server on the values it
  started with. An operator narrowing `allowed_email_domains`,
  removing an entry from `group_map` or switching local login
  off whilst containing an incident has to restart the server
  for the change to take effect. (#261)

- Count deadlocks and temporary files per hour in the alerter. The
  `deadlocks_detected` and `temp_files_created` rules compared the
  change in `pg_stat_database` between two consecutive samples
  against their thresholds, so the value they evaluated depended on
  the probe interval, and a counter reset could produce a negative
  delta. Both rules now sum the positive changes recorded in the
  last hour for each database, in line with the checkpoint and
  archive rules, and report their units as `deadlocks/hour` and
  `files/hour`. The thresholds themselves are untouched, and so are
  any operator overrides, but `temp_files_created` now compares its
  threshold of 100 against a much larger number: at the shipped
  `pg_stat_database` sampling interval of 300 seconds, twelve
  samples fall inside the window, so a database that steadily
  creates 300 temporary files an hour reported about 25 before this
  release and reports 300 now. That rule will fire on databases
  where it never fired before. To keep the previous sensitivity,
  multiply the threshold by 3600 divided by the sampling interval
  in seconds, which gives a threshold of 1200 at the default
  interval. The `deadlocks_detected` rule is unaffected, because
  its threshold of 0 fired on any positive change under the old
  comparison as well. (#409)

- Extend the `-show-group-privileges` CLI command to also display a
  group's admin permissions, alongside the MCP and connection
  privileges it already reports. (#303)

- Drop the runtime dependency on a knowledgebase package from the
  `pgedge-ai-dba-server` package in both the Debian and RPM
  packaging. The knowledgebase is now published as co-installable
  `pgedge-ai-kb-<provider>-<model>` packages, one per embedding
  provider and model, and it remains opt-in and disabled by
  default. Operators who want the knowledgebase install the
  package matching their chosen embedding provider and model, and
  set `database_path` themselves. (#316)

- Update the knowledgebase `database_path` documentation and the
  example server configuration for the renamed `pgedge-ai-kb`
  package. The packages install one database per embedding
  provider and model under `/usr/share/pgedge/pgedge-ai-kb/`,
  named `kb-<provider>-<model>.db`, and the built-in default
  still points at the legacy `postgres-mcp-kb` path. (#293)

- Relabel the token scope options in the API token create and edit
  screens so they now read "All the owner's MCP privileges" and
  "All the owner's admin permissions" rather than "All MCP
  Privileges" and "All Admin Permissions"; the new wording clarifies
  that a token grants the owner's privileges rather than all system
  privileges. (#323)

- Ellie now gives correct, deployment-aware guidance for restarting
  the Workbench's own components (server, collector, alerter, and
  web client), and no longer suggests commands for unrelated
  third-party tooling.
  The web client sends its own copy of Ellie's system prompt on
  every request, which always takes precedence over the server's
  default; both copies now carry the same guidance, since fixing
  only the server default left the client-facing bug unchanged for
  real users. (#329)

- Run the tests for the shared `pkg` module from the root `make test`
  and `make test-all` targets, which previously skipped that module
  entirely. This change affects test infrastructure only and does not
  alter application behavior. (#364)

- Plot rates rather than raw cumulative counters on the dashboard
  charts that are built on `pg_stat_*` counters, so that a chart
  reflects the current workload instead of an ever-rising total. The
  server dashboard now plots commits and rollbacks, blocks hit and
  read, tuple operations, WAL bytes and records, checkpoint buffers
  written, and network throughput per second; the database dashboard
  plots commits and rollbacks per second and reports transactions per
  second; and the object dashboard plots query calls per second. The
  Checkpoints chart is now a stacked bar chart of the timed and
  requested checkpoints in each interval, a Checkpoint Buffers Written
  chart has been added, and the Checkpoints KPI has become Requested
  Checkpoints, the percentage of checkpoints in the selected range
  that were requested rather than timed; a high percentage suggests
  that `max_wal_size` is too low. A chart whose metric a probe does
  not collect now shows the error the server returned instead of a
  generic "No data" message, and a bar or area chart whose values are
  all zero no longer draws a y-axis running below zero. (#400)

- Make the `value` of a data point in the metrics query response
  nullable, and emit every time bucket in every series so that all
  series in a response share the same bucket times. A bucket is
  `null` when two consecutive samples are more than three collection
  intervals apart, when a probe's `stats_reset` marker changed between
  them, or, for a raw column, when the last observed value is more
  than three collection intervals old; a rate or delta series no
  longer repeats a stale value into an empty bucket. The requested
  bucket count is clamped so that no bucket is narrower than the
  probe's collection interval. The `pg_sys_network_info` probe leaves
  the `lo` and `lo0` loopback interfaces out of every query. A
  `_per_sec` request on a column that is not a cumulative counter,
  such as a gauge or `mean_exec_time`, is now rejected with HTTP 400
  and a message naming the column's kind, rather than returning a
  meaningless series. The dashboards draw a `null` bucket as a break
  in a line or a missing bar. The interval a sample is judged against
  is the configured one widened to the spacing the samples in the
  window actually show, so tightening a probe's collection interval
  no longer reads its older, wider-spaced history as one long gap.
  A `_delta` bucket with no sample of its own reports zero only where
  an accepted sample interval spans it: through a collection outage,
  and before a probe's first sample or after its last, it is `null`
  like every other derived form, rather than a run of confident zero
  bars. (#402)

- Gate the shared `pkg` module with its own formatting, linting and
  test targets. The module is a separate Go module with no Makefile
  and no CI workflow of its own, and the per-sub-project workflows
  check only their own `src` directories, so six test files under
  `pkg/connstring`, `pkg/crypto`, `pkg/datastoreconfig`,
  `pkg/hostvalidation`, `pkg/logger` and `pkg/worker` had drifted out
  of `gofmt` shape unnoticed. Those files are now formatted, and two
  `staticcheck` findings in `pkg/fileutil/fileutil.go` are fixed: the
  tilde-path guard is simplified, and the error returned for an
  unsupported tilde path no longer ends in punctuation, so it now
  reads `unsupported tilde path %q: use ~ or ~/... rather than a
  ~user/... path`. A new `pkg/Makefile` and a new
  `.github/workflows/ci-pkg.yml` keep the module gated from now on,
  and the root `Makefile` delegates its `test`, `lint` and
  `test-all` targets to the new Makefile; without that gate the same
  files would simply drift again. Behavior is otherwise unchanged.
  (#423)

- Scope the top queries leaderboard and the query detail overlay to
  the dashboard time range. The
  `GET /api/v1/metrics/top-queries` endpoint had no time dimension
  and always reported the latest collected sample; it now accepts
  `time_range`, defaulting to `1h`, along with `time_start` and
  `time_end` for a custom window, resolved by the same code as
  `GET /api/v1/metrics/query` so that the rejection messages match
  every other windowed endpoint. Because `pg_stat_statements`
  reports its counters cumulatively, the endpoint sums the
  differences between consecutive samples in the window rather than
  reading one sample: `calls`, `total_exec_time`, `rows`,
  `shared_blks_hit` and `shared_blks_read` now cover the window,
  `mean_exec_time` is derived from those sums, and a sample pair
  whose counters went backwards is discarded as a
  `pg_stat_statements_reset()`. The `min_exec_time` and
  `max_exec_time` fields stay lifetime figures, because a window
  extreme cannot be recovered by differencing two lifetime extremes,
  so ordering on either one sorts a windowed list on a lifetime
  figure. A statement that ran no calls inside the window is left
  out of the response altogether, which is a change in what the
  existing presets return and not only in what a custom window
  returns. On the client, both the leaderboard and the overlay
  follow the dashboard selector, including a custom window, and
  changing the range returns the leaderboard to its first page,
  since a shorter window usually matches fewer rows. The overlay's
  "Mean Time (All Time)" tile is now labelled "Mean Time", whilst
  "Min Time (All Time)" and "Max Time (All Time)" keep their labels
  and their lifetime figures. (#387)

- Add `idx_pg_stat_statements_identity_time` to
  `metrics.pg_stat_statements` through collector schema migration
  15. The index keys the statement identity that the
  `pg_stat_statements` counters are cumulative within, followed by
  `collected_at` and the probing database name, and includes every
  counter the windowed
  aggregation reads, so the top queries query runs as an index-only
  scan in the order it needs instead of sorting a whole window of
  samples to disk. The index is not free: it costs roughly 17% more
  storage on that table and about 56% more write-ahead log volume on
  the collector's writes. Creating it builds an index on every
  attached partition and blocks the collector's inserts whilst it
  runs, which took about 15 seconds for 2.6 million rows on the test
  fixture. (#387)

### Fixed

- Fix the transaction throughput chart on the Performance Summary
  page counting a database's whole lifetime transaction count into
  one interval. The commits and rollbacks per second were derived by
  differencing the cluster-wide sum of the `pg_stat_database`
  counters, so a database created between two collector samples
  added its entire counter to that interval as a spike, whilst a
  database dropped between two samples turned the difference
  negative and flattened the interval to zero. The counters are now
  differenced for each database separately and only then summed, and
  an interval is discarded for a single database when its statistics
  were reset, when it was missing from the preceding sample, or when
  the sample has no predecessor in the selected range. Charts over a
  window in which databases were created or dropped therefore report
  lower, accurate rates where they previously showed a spike or a
  gap. (#469)

- Fix the `pg_stat_statements` collector probe discarding the block
  timing columns on every modern server. The probe chose its query
  shape by looking for the version-specific columns in
  `information_schema.columns` with `table_schema = 'pg_catalog'`, but
  `CREATE EXTENSION` installs the view into the current schema, normally
  `public`, so both checks failed and the probe fell back to the
  PostgreSQL 12 query. Rows arrived with `shared_blk_read_time`,
  `shared_blk_write_time`, `local_blk_read_time` and
  `local_blk_write_time` stored as NULL and `toplevel` fixed at `TRUE`.
  The checks now resolve the view through the search path, exactly as
  the probe's own query does, so a relocated extension is detected in
  whichever schema it was installed, provided that schema is on the
  collector role's search path. (#439)

- Correct the documented default for
  `http.auth.max_failed_attempts_before_lockout`, which the
  server configuration reference gave as `0`, meaning lockout
  disabled, in both its option table and its example
  configuration. The server has always defaulted the setting
  to `10`, so an operator who configured around the documented
  value was working from the wrong baseline. The same page
  also showed an `http.auth.enabled` key that has never
  existed on the configuration struct and that the loader
  silently discards; it has been removed from the
  documentation, from the server README and from the Docker
  and walkthrough sample configurations. (#261)

- Fix Ask Ellie failing with `Function call is missing a
  thought_signature in functionCall parts` on every question that
  needs a tool call when the LLM provider is Gemini. Google's current
  Gemini models attach an opaque `thoughtSignature` to each function
  call and reject the next request with a 400 error unless it is
  echoed back unchanged, and the pinned `pgedge-go-llm-lib` neither
  captured nor replayed it. The server and alerter now use library
  release v0.3.1, which carries the signature on the `tool_use` block
  through the chat endpoint and back to Gemini, so the existing client
  loop needs no change. The same release reports tool failures to Gemini
  instead of letting the model retry the identical call, and hides
  Gemini models that cannot hold a text conversation from the model
  list. (#425)

- Fix every cache hit ratio in the web client (the Cache Hit
  performance tile, the server dashboard's KPI sparkline and
  per-database cards, the database dashboard's KPI tile and Cache
  Hit Ratio Over Time chart, and the cluster dashboard's comparative
  chart) reporting a lifetime average since the last statistics
  reset, which could not move when a current problem appeared. The
  ratio is now computed from the change in `blks_hit` and
  `blks_read` between consecutive samples within each time bucket.
  Intervals with no block access render as a gap in the chart and
  '--' for the headline value instead of 0%, samples following a
  statistics reset are discarded rather than producing a bogus
  ratio, and the tooltips note that `blks_hit` counts only
  `shared_buffers` hits, so a lower ratio does not by itself
  indicate slow I/O. (#401)
- Add custom time range support to the
  `/api/v1/metrics/performance-summary` endpoint: `time_range=custom`
  with `time_start` and `time_end` resolves through the same rules as
  `/api/v1/metrics/query`, so the server dashboard's Cache Hit Ratio
  tile follows a custom range like every other panel. (#401)
- Enforce the `required_extension` field on alert rules. The alerter
  stored the field but never checked it, so rules that depend on
  `pg_stat_statements`, `system_stats` or Spock were evaluated against
  every connection. The alerter now reads the newest
  `metrics.pg_extension` snapshot for each connection and skips
  connections that lack the extension. An existing alert on a
  connection that lacks the extension is left active rather than
  resolved, so uninstalling an extension no longer reports the
  underlying condition as cleared. (#409)

- Stop the `test` and `coverage` targets in the server, collector and
  alerter Makefiles running `pkill -9` against every matching process
  on the host, which killed any developer-started dev server or
  service whenever a test suite ran. The standalone `killall` target
  remains as an explicit, developer-invoked action and now sends
  SIGTERM rather than SIGKILL. (#445)

- Fix the `metric_staleness` alert rule firing and clearing in a
  loop, which sent a notification pair every cycle for as long as a
  probe remained stale. The alert cleaner resolved the rule's metric
  through the metric registry, where the bespoke staleness metric
  has no entry, and treated the resulting lookup error as proof that
  the condition had gone away. The cleaner now distinguishes a
  metric it cannot evaluate, or a query that failed, from a metric
  that ran and reported nothing; only the latter clears an alert.
  Staleness alerts resolve through their own check against probe
  availability, the staleness evaluator applies the same cooldown
  guard as every other rule, and each stale probe on a connection
  now raises its own alert rather than overwriting a shared one.
  (#405)
- Fix five built-in alert rules that could never fire. The
  `wal_archive_failed` rule queried `metrics.pg_stat_archiver`, a
  table the collector never creates, and now reads the archiver
  columns of `metrics.pg_stat_wal`. The `transaction_wraparound`
  rule evaluated a hardcoded 50.0 and now reports the transaction
  ID age of the oldest database as a percentage of the
  wraparound limit, template databases included; `template0`
  does not allow connections, so autovacuum only reaches it on
  the anti-wraparound path, which makes it the database most
  likely to age while every user database stays fresh, and
  excluding templates left the rule silent in exactly that
  case. The dashboard's XID Age tile now counts templates for
  the same reason. The `high_max_connections` and
  `connection_utilization` rules required a `pg_settings` snapshot
  from the last hour, which a change-tracked probe stops
  producing on a stable server, and now read the newest stored
  snapshot. The `cpu_usage_high` rule keyed on a Windows-only
  column that is NULL on Linux and now derives the busy
  percentage on both platforms. The `checkpoint_warning` rule
  compared a per-probe-interval delta against an unreachable
  threshold and now counts requested checkpoints per hour, with a
  default threshold of 12. Tuned autovacuum settings are also
  honoured again by `autovacuum_not_running`, which previously
  always assumed the shipped defaults. (#406)

- Fix alerts flapping, latching, or clearing falsely when a metric
  stops reporting. The alert cleaner treated a latest-metric query
  that returned no row for an alert's connection and database, or no
  rows at all, as proof that the condition had resolved, so any gap
  in collection cleared the alert and the next sample raised it
  again. The metric registry now records, for each metric, whether a
  missing row means recovery. Metrics that emit a row for every
  healthy connection leave their alert active until fresh data shows
  the condition has ended, and the `metric_staleness` rule reports
  the probe that stopped; metrics that report only whilst the
  condition holds, or that describe an object an operator can
  legitimately drop, such as a replication slot or a standby, still
  clear on absence, but only when the collector probe behind the
  metric last collected inside the window that metric's query reads.
  Every metric bounds the age of the samples it reads, so a stopped
  collector empties a query exactly as a recovered condition does; the
  cleaner now checks that window first, and a probe that has stalled,
  that an operator has disabled, or that belongs to a connection which
  is no longer monitored leaves the alert active until somebody clears
  or acknowledges it. The check is made against the metric's own
  window rather than against a multiple of the probe's collection
  interval, so raising `collection_interval_seconds`, for every
  connection or for one, no longer leaves the cleaner quiet whilst
  ordinary collection empties the query. (#407)

- Fix the `slow_query_count` rule latching until a manual
  `pg_stat_statements_reset()`. The rule counted the queries whose
  `mean_exec_time` exceeded 1000 ms, a lifetime average since the
  last statistics reset that never decays, so a query that ran
  slowly once kept the count elevated whether or not it ran again.
  The count now covers only the queries that ran during the most
  recent probe interval, using an interval mean derived from the
  change in total execution time divided by the change in call
  count. The samples in the window are sorted once rather than
  twice, which cut the query's execution time by roughly a third on
  a fixture of 24,000 statement samples. (#407)

- Fix the `cache_hit_ratio_low` rule firing and clearing on
  identical data. The metric returned every delta row in its
  fifteen minute window with no ordering, and the threshold
  evaluator fired when any row violated the threshold whilst the
  alert cleaner stopped at the first row it saw, so one set of
  samples could produce a fire and a clear in the same cycle, or
  latch the alert when the oldest interval was the violating one.
  The metric now reduces to the newest qualifying delta for each
  connection and database, so both sides read the same value.
  (#407)

- Add a fifteen minute freshness cutoff to the
  `pg_replication_slots.inactive_count` and
  `pg_replication_slots.max_retained_bytes` metrics, which
  previously had none, so `replication_slot_retention_warn` and
  `replication_slot_retention_high` no longer keep firing on
  days-old samples after a collector stops or a connection stops
  being monitored. The windows on `pg_replication_slots.inactive` and
  `pg_node_role.subscription_worker_down` widened from five to
  fifteen minutes, because five minutes equalled the probe interval
  and a single late collection emptied the window, clearing the
  critical `replication_slot_inactive` and `subscription_worker_down`
  alerts. Unit tests now require every metric in the registry to
  bound `collected_at`, or to be allowlisted with a reason, and
  require every metric that clears on absent data to look back over
  at least three of its probe's seeded collection intervals;
  `pg_settings.max_connections` is the one allowlisted exception,
  since its change-tracked probe stores nothing whilst the
  configuration is unchanged. (#407)

- Split the server dashboard "Connections Over Time" chart into a
  Connections chart and a Sessions Established chart. The old chart
  plotted `numbackends`, a gauge bounded by `max_connections`,
  alongside `sessions`, a counter that climbs until the statistics
  are reset, so the counter set the scale and flattened the backend
  count into a featureless line. The Connections chart now shows
  backends against a `max_connections` reference line, and both
  titles state that the figures cover the monitored database, since
  the `pg_stat_database` probe reports on `current_database()` only.
  (#403)

- Stop the login rate limiter that `AuthHandler` creates for itself
  when the server shuts down. `NewAuthHandler` starts a background
  cleanup goroutine for an internal rate limiter covering total login
  attempts per IP, and only its `Close` method stops that goroutine.
  Nothing called `Close`, because `SetupHandlers` built the handler
  inside a closure and discarded the reference, so the goroutine and
  its map of recorded attempts survived for the remaining life of the
  process. `HandlerDependencies` now carries a `RegisterCloser`
  callback, which the server uses to collect cleanup functions from the
  handlers it wires and to run them from `Close`, alongside the
  overview generator, the shared rate limiter, the auth store, and the
  datastore it already stopped.

- Fix the collector's partition dropper never enforcing
  `retention_days`, which could fill the metrics datastore's disk
  and take the Workbench down with it. Garbage collection was
  scheduled five minutes after process start and every 24 hours
  thereafter, with no record of when it last ran, so retention was
  coupled to unbroken process uptime; a collector that restarted
  more often than the startup delay never collected garbage even
  once, and each restart returned it to the beginning of the five
  minutes. A reporter saw partitions persist nine days past a
  seven-day window across more than a hundred restarts, with no
  dropper activity in the logs at all. The collector now records
  each completed pass in a new `maintenance_runs` table and times
  the next pass from that record rather than from process start, so
  an overdue collection runs shortly after startup however many
  times the process has restarted. This also closes the same gap
  for rolling deployments and node drains, which failed
  identically. A failed pass now backs off rather than retrying
  immediately. (#437)

- Fix the Query Plan panel in the Query Detail view of the web
  client showing a raw database error, such as `syntax error at or
  near "VACUUM"`, for statements that PostgreSQL cannot explain.
  Because `pg_stat_statements` records utility and maintenance
  statements alongside `SELECT` and DML, a Top Queries row can hold
  text such as a bare `VACUUM`, `ANALYZE`, or `REINDEX`. The panel
  now reports that query plans are not available for that type of
  statement, and no longer attempts a request that cannot succeed.
  Statements that PostgreSQL can plan, including a bare `TABLE`, are
  unaffected and still show their plans. Where PostgreSQL accepts a
  statement but returns no plan, as with
  `REFRESH MATERIALIZED VIEW`, the panel now says so plainly instead
  of showing the database's internal notice. (#368)

- Fix the "Connection Count" bar chart in the cluster dashboard's
  comparative section, which plotted a hardcoded value of one for
  every server rather than a real measurement. The performance
  summary endpoint (`GET /api/v1/metrics/performance-summary`) now
  reports an `active_connections` count per connection, taken from
  the sum of `numbackends` across the databases in the server's
  most recent `pg_stat_database` snapshot, and the chart plots that
  value. (#404)

- Fix both charts on the query drill-down page, "Execution Time
  Over Time" and "Calls Over Time", aggregating across every
  statement in the database instead of the query the user selected.
  The metrics time-series API (`GET /api/v1/metrics/query`) now
  accepts a `queryid` filter, and the query detail page passes the
  identifier of the selected query so that both series describe
  that query alone. (#404)

- Fix the collector and the alerter overriding a configuration file
  value whenever that value happened to match a command-line flag's
  built-in default, and ignoring a flag that an operator passed
  explicitly with its default value. Both services decided whether a
  flag had been supplied by comparing the flag's value against its
  own default, which cannot tell the two cases apart; for example,
  a collector configuration file setting `datastore.port: 6000`
  ignored `-pg-port 5432` on the command line. Both services now
  detect the flags that were actually present on the command line,
  so the documented precedence of defaults, then configuration file,
  then flags holds in every case. (#389)

- Roll every database transaction back on a bounded, non-cancelable
  context across the server, collector, and alerter, through the
  shared `pkg/rollback` helper. When a rollback runs on a request
  context that the client has already cancelled, the pgx v5 driver
  fails the rollback and closes the connection, so each client that
  disconnected mid-request cost the pool a connection whilst the pool
  reconnected. The helper derives a five-second, non-cancelable
  context from the request context instead, so a disconnect no longer
  discards the pooled connection, and a rollback to a hung server
  cannot pin a pool slot indefinitely. A convention test in every Go
  module rejects direct `Rollback` calls and hand-written `ROLLBACK`
  SQL. (#381)

- Fix every chat request that included a tool list failing with
  `anthropic (400): tools.0.custom.input_schema: Input does not
  match the expected shape`, which broke Ask Ellie and the Server,
  Query, and Alert analysis panels for any interaction requiring a
  tool call. The client built each tool with a camelCase
  `inputSchema` field, but `POST /api/v1/llm/chat` decodes its
  request body via the vendored `pgedge-go-llm-lib` package, whose
  wire contract expects snake_case `input_schema`; the mismatched
  key silently unmarshalled to an empty schema for every tool,
  which Anthropic then rejected outright. The client now normalises
  tool definitions to the library's wire shape before sending them,
  mirroring the existing message normalisation added in the same
  migration. (#370)

- Connection-privilege chips in the Admin Groups and Users panels
  now show the connection name instead of the internal numeric
  connection id. (#309)

- Fix the metrics time-series API (`GET /api/v1/metrics/query`)
  silently returning an empty HTTP 200 response, which the web UI
  rendered as a generic "No data available" message, whenever a
  monitored server's metric sample contained a NaN or Infinity value.
  Go's JSON encoder cannot serialize non-finite floats, so the encode
  failed silently after the response status had already been sent. The
  query path now treats a non-finite sample the same way it already
  treats a missing value from an underlying LEFT JOIN gap; the code
  carries the last known good value forward when one exists and skips
  the sample when none does, so a single bad reading degrades
  gracefully instead of blanking the whole chart. A JSON-encoding
  failure on this path is now logged loudly rather than silently
  swallowed, so any future occurrence surfaces in the server logs.

- Fix blank overview tiles and a Maintenance Info panel stuck on
  "Never" in the Table and Index object-detail dashboards of the
  web client. These panels requested the single most recent row of
  stats through the `limit`, `order_by`, and `order` query
  parameters, but the metrics API ignored those parameters and
  returned time-bucketed chart data instead. The API now honors
  these parameters and returns the most recent raw stat rows as
  real column values, validating `order_by` against the table's
  known columns before use.

- Fix the end-to-end test suite intermittently failing at the "Start
  stack" step. The server logged "collector schema is at v2, server
  requires at least v4" and then timed out waiting for its health
  endpoint. The harness script that applies the collector schema
  stopped the collector as soon as it observed any recorded schema
  version. A poll that landed between two migrations killed the
  collector mid-migration, leaving the datastore below the version the
  server requires. The collector now accepts a
  `--print-latest-schema-version` flag that reports the newest known
  schema version without touching any database. The harness asks for
  that target up front and waits for the datastore to reach it. This
  change affects test infrastructure only and does not alter
  application behavior.

- Fix the Table Size and Index Size fields rendering permanently as
  "--" on the Table and Index object-detail dashboards of the web
  client. No collector probe ever gathered relation size, so the value
  genuinely never reached the datastore rather than failing at query
  time as the two prior dashboard fixes did. The `pg_stat_all_tables`
  probe now collects `table_size` via `pg_table_size`, which covers
  heap, TOAST, free space map, and visibility map storage whilst
  excluding indexes. The `pg_stat_all_indexes` probe now collects
  `index_size` via `pg_relation_size` for each index. Both values are
  stored as raw byte counts that the web client formats for display.
  Collector schema migration Version 7 adds the two new columns to the
  `metrics.pg_stat_all_tables` and `metrics.pg_stat_all_indexes`
  partitioned parent tables, and PostgreSQL propagates them to every
  existing and future partition automatically. The web client's
  existing latest-row request reads the new columns with no further
  server or client changes.

- Fix the Activity Charts on the Table and Index object-detail
  dashboards rendering permanently blank, with the messages "No tuple
  operation data available", "No scan data available", and "No dead
  tuple data available" appearing even when the monitored server had
  completely normal, healthy activity. Those charts request computed
  metrics, including per-second rates such as sequential scans, index
  scans, and inserted, updated, or deleted rows, together with a
  dead-tuple percentage. The metrics API understood only literal, raw
  stored columns and could not compute a rate or a ratio, so it
  rejected these requests with an error that the web client silently
  rendered as missing data. The metrics API now computes a per-second
  rate for any counter column a dashboard requests by name; a request
  for `seq_scan_per_sec` computes the rate of change of the underlying
  `seq_scan` counter between samples. The API also computes a
  dead-tuple ratio metric for tables, reusing the rate and ratio
  calculations already applied elsewhere in the product. (#342)

- Fix embedding storage rejecting non-OpenAI providers with a
  dimension-mismatch error. The chat memory and anomaly detection
  embedding columns were hardcoded to `vector(1536)`, OpenAI's
  dimension, which broke Gemini (3072), Voyage (1024 or 512), and
  Ollama (384, 768, or 1024). The columns are now `halfvec(4000)`,
  and the workbench zero-pads every embedding to 4000 dimensions, so
  embeddings from any supported provider store and compare correctly.
  This widening introduces a hard ceiling: an embedding model that
  produces vectors with more than 4000 dimensions is not supported,
  because 4000 is pgvector's HNSW index limit for the `halfvec` type.
  The workbench rejects such a model with a clear error rather than
  truncating the vector. (#294)

- Fix the Estate Overview dashboard's KPI tiles (XID Age, Cache Hit
  Ratio, Transactions, and Checkpoints) and Event Timeline getting
  stuck on "No data" indefinitely after a brief backend restart or
  any transient API failure. Recovering required the user to click the
  sidebar's Refresh button or reload the page, because nothing retried
  on its own. The client uses no React Query or SWR; every panel hook
  refetched only when a single shared `lastRefresh` timestamp changed,
  and that timestamp advanced only when the sidebar's
  `GET /api/v1/clusters` call succeeded. A transient failure in an
  individual panel's fetch, or in the shared refresh call itself, left
  the affected panels blank with no automatic recovery. A shared
  `useRetryingFetch` hook now provides capped exponential backoff (3s,
  6s, 12s, 24s, capped at 45s) for the Event Timeline, the
  performance-summary tiles, the Estate KPI tiles, and the alerts
  panel. A manual refresh always pre-empts any pending retry, and the
  Estate KPI tiles show a subtle "Reconnecting..." indicator while a
  retry is pending, distinguishing that state from genuinely empty
  data.

- Granting "All Connections" access no longer deletes a group's
  specific per-connection grants. Previously, adding an All
  Connections READ grant destructively removed existing
  per-connection grants, silently reducing a connection's
  READ/WRITE access to READ; specific and wildcard grants now
  coexist, and the effective access for each connection is the
  higher of the two. (#302)

- Fix the Table and Index object-detail dashboards showing stale,
  pre-maintenance values in their overview tiles and Maintenance Info
  panel indefinitely, even after the collector recorded fresh, correct
  data. Running ANALYZE on a table corrected its live-tuple-count
  estimate downward, yet the dashboard kept showing the old, larger
  count and a stale or missing Last Analyze value. The latest-row mode
  of the metrics API (`GET /api/v1/metrics/query` with `limit` and
  `order_by`) ranked the table's entire history by the requested
  `order_by` column and used `collected_at` only to break exact ties,
  so it returned whichever historical sample had the highest column
  value rather than the most recent sample. Any column that can
  decrease over time, such as a tuple count after ANALYZE, VACUUM, or
  DELETE, or a dead-tuple count after VACUUM, could therefore return a
  permanently stale historical row. The query now reduces to each
  monitored entity's newest sample by `collected_at` first, then ranks
  those already-latest rows by the requested `order_by` column, so a
  request filtered to one table or index always returns that entity's
  true latest sample regardless of the sort column.

- Fix the Index object-detail dashboard's Scan Activity chart always
  showing "No index scan data available" for every index. The web
  client sent the index's own name as the `table_name` query
  parameter, but the metrics time-series query filtered on `relname`,
  so no table ever matched an index name and the request returned no
  rows. The time-series query path also had no way to filter by index
  name, so a table with several indexes would have blended all of
  their scan activity together. The metrics API
  (`GET /api/v1/metrics/query`) now filters on `indexrelname`, the
  client's metrics-fetching hook accepts a new `indexName` parameter,
  and the Scan Activity chart sends it instead of misusing the
  table-name parameter.

- Fix several dashboard charts rendering blank whenever their time
  series was perfectly flat, most visibly the Cache Hit Ratio Over
  Time chart for a new, essentially read-only sample database whose
  ratio held steady at 100% for its whole history. A flat series gave
  the value axis a zero-height range, with `min` equal to `max`, so
  the line or area had nothing to draw against and the chart appeared
  empty. The value axis now pads a small visible range around a flat
  value so the series renders, and it does so in a baseline-aware way
  for zero-anchored charts, such as bars and stacked or area fills, so
  their fill still renders from zero. (#336)

- Fix the Cluster Group settings dialog so it shows and preserves a
  group's description and its "Share with all users" setting, which
  previously appeared empty or unchecked and were lost when editing;
  the share flag is now persisted by the API. (#304)

- Harden the collector against clean-exit restart loops that could
  stop the Dashboard from showing Top Queries. The collector now
  logs which signal triggered its shutdown, rather than exiting
  silently, and the default monitored probe and pool wait timeout
  (`monitored_max_wait_seconds`) was raised from 60 to 120 seconds
  to give slow probes such as `pg_stat_statements` on large
  monitored databases more headroom before timing out. (#308)

- Harden the shipped Docker Compose collector service by running an
  init process as PID 1, adding a stop grace period for graceful
  shutdown, and raising the memory limit modestly from 256M to
  512M; the collector connection-pool and timeout options are now
  documented in the sample configuration. (#308)

- Fix the "Hide monitoring queries" toggle on the Server dashboard's
  Top Queries panel failing to hide the collector's and alerter's
  datastore query overhead. Only the collector's probe queries against
  monitored databases carried a marker, so its bulk `metrics.*`
  inserts, partition maintenance, and change-detection reads, together
  with the alerter's metric-evaluation queries, stayed in the panel
  whenever the metadata datastore shared a PostgreSQL instance with
  the monitored databases, which is the usual deployment. The toggle
  now hides those statements as well. Each statement is tagged
  individually rather than excluding the metadata database wholesale,
  so queries that other tools run against that same database remain
  visible. Two classes stay untagged by design and still appear in the
  panel: the server's own datastore traffic for sessions, RBAC,
  conversations, and the timeline; and the collector's `probe_configs`
  resolution path, along with the alerter's remaining direct datastore
  queries in `alert_queries.go`, `anomaly_queries.go`,
  `notification_queries.go` and `queries.go`.

  On an existing installation the toggle only hides statements that
  PostgreSQL first recorded after the upgrade. `pg_stat_statements`
  identifies a statement by its parse tree, which ignores comments, so
  an entry already present keeps the untagged text it was first seen
  with and the filter never matches it; those entries run every
  collection cycle, so they are never evicted either. Run `SELECT
  pg_stat_statements_reset();` once on each monitored instance after
  upgrading for the toggle to take effect on statements already in the
  view. (#364)

- Fix the AI Overview and the server-info AI database analysis
  rendering blank with no error. The failure appeared when a local
  reasoning model served the request. Both paths capped their
  responses at a hardcoded 512 output tokens and ignored the
  configured `llm.max_tokens` value. A reasoning model counts its
  thinking tokens against that budget. The model therefore spent
  the whole budget before emitting any answer text. Both paths now
  honour `llm.max_tokens`, which defaults to `4096` tokens and
  previously governed only the streaming chat proxy. A response
  that carries no text is now reported rather than cached as an
  empty result. The `GET /api/v1/server-info/{id}/ai-analysis`
  endpoint returns `502` with a message that names the likely
  cause. (#399)

- Fix the collector testing the error from a failed configuration
  file load with `os.IsNotExist`, which does not unwrap the wrapped
  error and so never matched, leaving two branches unreachable. Both
  checks now use `errors.Is`, so a configuration file named with
  `-config` that does not exist reports "specified config file not
  found" along with the path rather than a generic load failure, and
  an auto-discovered configuration file removed between discovery
  and load falls back to the compiled-in defaults as intended
  instead of aborting startup. (#421)

- Fix the Disk Space chart and the Disk Usage KPI tile on the server
  dashboard averaging `used_space` and `free_space` across every
  mounted filesystem, including pseudo filesystems such as `tmpfs` and
  `devtmpfs`, so that the figures described no real filesystem. Both
  now describe a single real filesystem, chosen with a new Filesystem
  selector above the chart and defaulting to the filesystem closest to
  full, with the mount point shown in the tile label and the chart
  title; pseudo filesystems are excluded. The usage percentage is now
  derived from the filesystem's reported total size rather than from
  used plus free space, which differ on any filesystem with reserved
  blocks. The `GET /api/v1/metrics/query` endpoint gained a
  `mount_point` query parameter, which filters both the time-series
  and the latest-row responses. The alerter's disk usage threshold
  metric took the highest usage across all mounts and so was pinned at
  100% on any host with a squashfs mount, because a squashfs image is
  by construction fully used; the metric now ignores pseudo
  filesystems, matching the dashboard. (#428)

- Fix the collector running every past-due probe immediately at
  startup, with no stagger and no limit on how many ran at once. On a
  restart, every probe whose interval is shorter than the outage is
  past due, so concurrency scaled with the number of monitored
  connections multiplied by their probes; that burst exhausted a
  modest memory limit, and because the resulting restart put every
  probe back in the past-due state, the collector never recovered on
  its own. A probe that is past due or has never run now waits out a
  random delay, drawn from zero up to the smaller of its collection
  interval and the new `scheduler.startup_jitter_seconds` setting
  (60 seconds by default, with 0 disabling the stagger), and its timer
  is realigned afterwards so the offset also spreads out steady-state
  collection. Every probe execution now holds a slot in a global
  semaphore bounded by the new `scheduler.max_concurrent_probes`
  setting (8 by default), so the memory and connection demand of
  active probe execution is capped by that setting rather than
  growing with the number of monitored connections multiplied by
  their probes; the scheduler still runs one goroutine per
  connection and probe pair, so that baseline continues to scale
  with the number of monitored connections. A probe holds its slot
  until the execution finishes or reaches
  `pool.monitored_max_wait_seconds`, so the new
  `scheduler.max_concurrent_probes_per_connection` setting (2 by
  default, and clamped to `scheduler.max_concurrent_probes`) caps how
  many of those slots a single monitored connection may hold at once;
  without the ceiling, one server that accepts connections but answers
  slowly demands more slots than the cap provides and queues the
  probes of healthy connections behind its own. (#441)

- Fix anomaly detection ignoring the hourly and daily baselines the
  alerter had been writing. Baseline selection took the first row
  returned by a query ordered alphabetically by `period_type`, which
  was always the global `all` row, so seasonality was never applied
  and the per-period warmup thresholds were unreachable. The engine
  now prefers the hourly baseline for the current hour, then the
  daily baseline for the current weekday, then the global baseline,
  and uses the first of those that passes its warmup gate; when none
  is warm, detection is suppressed for that value. Hour and weekday
  are derived in UTC both when baselines are written and when one is
  selected. The default `baselines.lookback_days` rises from 7 to 15:
  a baseline's span can never exceed the lookback, so at 7 days the
  daily tier's 336 hour warmup span could never be met and the daily
  step was silently skipped. The alerter now logs a warning on each
  baseline cycle for any warmup tier whose `min_span_hours` exceeds
  the lookback. Operators who set `lookback_days` explicitly should
  raise it to at least 15, or shorten `warmup.daily.min_span_hours`
  to match. (#408)

- Fix anomaly detection being permanently disabled for the 13 of 31
  registry metrics that have no historical query. The alerter built a
  fallback baseline for those metrics from a single current sample,
  which could never pass the warmup gate, and rewrote it every
  baseline cycle. The fallback path is removed; such metrics are now
  skipped by baseline calculation and detection, and leftover
  baseline rows for them are deleted on each baseline cycle. (#408)

- Fix anomaly detection mixing up databases on multi-database
  connections. Baseline lookup ignored the database name, the current
  value was taken from whichever database happened to be listed
  first, and the anomaly candidate was written without a database
  name, so alert deduplication collapsed every database on the
  connection. Baseline lookup, value selection and candidate creation
  are now all scoped to the database, so per-database metrics such
  as `cache_hit_ratio`, `deadlocks_delta` and `temp_files_delta` are
  scored against their own database's baseline and deduplicated per
  database. Each metric's latest values are also fetched once per
  rule rather than once per rule per connection. Active anomaly
  alerts for these three metrics raised before the upgrade carry no
  database name and so no longer match the per-database
  deduplication; the first detection after upgrading may add a
  second alert alongside such a row, which should be cleared by
  hand as anomaly alerts are not resolved automatically. (#408)

- Fix a probe staleness alert clearing itself at the moment collection
  on that probe stopped, which left an operator with no active alert
  for a server that had gone quiet. The alerter's cleanup pass read a
  staleness view that omits any probe which is unavailable, disabled,
  attached to a connection that is no longer monitored, or has never
  collected, and it treated absence from that view as proof that
  nothing was left to be stale about, so a probe that went unavailable
  (a monitored server losing an extension, or a probe run timing out)
  cleared the `metric_staleness` alert silently at debug log level. A
  probe that has gone unavailable now keeps any staleness alert that
  has already fired for it, and the alert's description is rewritten
  to say that collection has stopped and to give the recorded reason;
  the title is unchanged, so the alert stays recognisable in
  notification history. The rewritten description reverts to the
  evaluator's own wording once the probe collects again, and also on
  the way out when a held alert clears because an operator disabled
  the probe or because its connection is no longer monitored, so
  neither the clear notification nor the stored alert history
  announces the resolution in the words "Collection has stopped ...".
  On that clearing path the wording is rebuilt from the connection and
  probe configuration rows the staleness view joins against, so a held
  alert whose connection has gone altogether, or whose lookup fails,
  still clears carrying the held text. The line
  reporting the hold is logged once, when the decision or the recorded
  reason changes, rather than on each 30 second cleanup cycle. A probe
  that is absent because an operator disabled it, or because its
  connection is no longer monitored, still clears the alert, since
  both are deliberate actions, and both outcomes are now logged at the
  normal log level rather than at debug level. The staleness evaluator
  is unchanged and still skips unavailable probes, because an
  unavailable probe is a normal steady state for a server without the
  extension a probe needs, and raising a new alert for one would leave
  a permanent alert on every such probe; a probe that goes unavailable
  before its staleness alert has fired therefore still raises no
  alert, which is tracked in issue #512. (#465)

### Removed

- Remove the hard-coded allow-list of embedding model names, which
  rejected any model the OpenAI, Voyage AI, or Gemini provider had
  not been told about, whether the model was newly published by the
  provider or served under its own name by a local model server. The
  configured model name is now passed to the provider unchanged, so
  any model the provider accepts can be used, including models served
  by an OpenAI-protocol-compatible local server such as llama.cpp or
  vLLM. The per-provider defaults still apply when the configuration
  leaves the model empty, and an embedding wider than 4000 dimensions
  is still rejected with a dimension error when it is stored or used
  as a query vector. The alerter previously cut every embedding to
  1536 dimensions before that check could run, silently truncating
  any wider model; it now keeps the model's native width, matching
  the server. Anomaly embeddings stored before this change by a model
  wider than 1536 dimensions remain at the truncated width, so their
  similarity to new embeddings is lower than it would otherwise be
  until they age out of the retention window. (#427)

- Retire the `table_bloat_ratio` alert rule, which duplicated the
  `dead_tuple_ratio` rule with a different denominator and fired on
  the same tables at a different threshold. The built-in rule is now
  disabled by default and its metric is no longer collected by the
  alerter; the rule definition remains so that historical alerts stay
  attributable, and any of its alerts that were active at upgrade
  time are cleared. Use `dead_tuple_ratio` to monitor table
  maintenance. (#409)

- Remove 48 unused functions from the collector, server, and alerter,
  along with the tests that existed only to exercise them. A
  reachability analysis with `govulncheck`'s sibling tool `deadcode`
  found 94 functions that no service entry point can reach; comparing
  the result against the v1.0.0 tag confirmed that every one had
  already been unreachable at that release, so none was recent work
  awaiting a caller. This change takes the safe subset: 698 lines of
  production code and 2,110 lines of dedicated tests.

- Delete the alerter's unused `secretManager` implementation and the
  `SecretManager` interface it satisfied, neither of which had any
  caller, and delete the unused resource `Registry` type along with
  its constructor and methods. The `Resource` and `Handler` types
  declared alongside `Registry` remain, since the MCP resource
  definitions still use them.

- Remove the unused `HeaderStatusIndicator` component from the web
  client, together with the `@dnd-kit/sortable` and `@dnd-kit/utilities`
  dependencies; the client imports only `@dnd-kit/core`, which stays.

- Remove a further 18 unused functions across the collector, server, and
  alerter, together with the tests that existed only to exercise them.
  The largest group is the alerter's notification-channel write API,
  where ten methods covering channel creation, updates, deletion, email
  recipients, connection links, notification history, and reminder state
  had no caller; the alerter reads notification channels, whilst the
  server owns every write. The rest are two pool accessors, two
  compaction analytics reporters, a probe-availability lookup, and three
  session tracing helpers.

- Remove the chat compactor's analytics entirely, rather than leaving a
  type that only ever writes. With its reporters gone, nothing could
  read what `RecordCompaction` accumulated, so the `Analytics` type,
  the `CompactionMetrics` and `EfficiencyReport` structures, the
  `analytics` field, the `EnableAnalytics` option and the two recording
  call sites all go together. `EnableAnalytics` defaulted to false, so
  no deployment was collecting these figures in any case.

### Removed

- Remove the superseded `NewConnectionHandler` and
  `NewNotificationChannelHandler` constructors, along with the
  `DefaultHostValidator` helper that only they used. The server has
  wired both handlers through the `NewConnectionHandlerWithSecurity` and
  `NewNotificationChannelHandlerWithSecurity` variants for some time, so
  the shorter forms were unreachable in production whilst 49 test call
  sites still used them. Those call sites now use the `WithSecurity`
  constructors directly, passing the same host-validation settings the
  removed helper supplied, so test behaviour is unchanged.

- Remove the period-scoped average execution time tile from the query
  detail overlay, which was labelled "Avg Time (Last 24h)" or "Avg
  Time (Custom Range)" according to the selected range. Now that
  `GET /api/v1/metrics/top-queries` derives `mean_exec_time` over the
  selected window, the Mean Time tile reports the same figure from
  the same delta pairs, so the overlay showed one number twice and
  paid for a second request on every open. The
  `GET /api/v1/metrics/query-stats` endpoint that fed the tile
  remains available to API consumers. (#387)

### Security

- Fix a configuration file that omits
  `http.auth.max_failed_attempts_before_lockout` silently disabling
  account lockout. The setting was a plain integer, so an omitted key
  arrived at the merge step as `0`, which passed the `>= 0` test and
  overwrote the default of `10`; every installation whose configuration
  did not name the key was therefore running with lockout turned off,
  leaving `POST /api/v1/auth/login`, which is unauthenticated by
  design, defended against password guessing only by rate limiting
  keyed on the client address: the failed-attempt allowance is cleared
  by any successful login, a cap of 20 login requests a minute for the
  address remains, and neither count is shared between server
  instances. The setting is now held as a pointer, in the same way as
  `http.auth.audit_retention_days`, so that an omitted key keeps the
  default of `10` whilst an explicit `0` still disables lockout
  deliberately. Restart the server to apply the fix: the threshold is
  read once at start-up, so a `SIGHUP` reload leaves the running server
  with the value it started with, and a reload that changes the setting
  now says so. A server that starts with lockout switched off warns at
  every start. (#473)

- Stop following HTTP redirects when the alerter delivers a
  notification. This changes the behaviour of every existing Slack,
  Mattermost, Telegram and generic webhook channel: a provider that
  answers a delivery with a 3xx response now fails that delivery,
  where the redirect was previously followed without comment. Such a
  delivery is retried on the usual backoff schedule and then recorded
  as `failed`, with the 3xx status code named in the stored error.
  The credential these channels use sits in the request URL, because
  the Slack and Mattermost webhook URLs are secrets in themselves and
  the Telegram URL carries the bot token in its path. Go copies the
  previous request's full URL into the `Referer` header of a
  redirected request, so following a redirect handed the credential to
  the host named in the `Location` header. A generic webhook channel
  gains a second protection: the server validates its endpoint host
  when the channel is saved, and a redirect could previously bounce
  the request past that check to a private or metadata host. The change is
  preventive:
  reaching the behaviour required a hostile or compromised endpoint
  answering a delivery with a redirect, and no credential is known to
  have been disclosed. The server's test-send path has always refused
  redirects, so delivery now matches it. Where an endpoint relies on a
  redirect, configure its final URL in the channel; email channels are
  unaffected, because they deliver over SMTP. (#475)

- Set a server-side `statement_timeout` on the server's datastore
  connection pool, defaulting to 30 seconds and configurable with the
  new `database.statement_timeout` option. The pool previously relied
  entirely on the handlers cancelling their own queries, which is a
  request sent over the connection: if it is lost, or the backend is
  not at an interruptible point, the query keeps running and keeps
  holding one of the pool's connections, and with `pool_max_conns`
  defaulting to 4 a handful of those starve the whole API. The default
  matches the longest deadline any handler grants a datastore query, so
  nothing that completed before now fails; the collector owns the
  schema migrations, including the partitioned-index build that can run
  for much longer than this, and they run on the collector's own pool,
  which is unaffected. (#387)

- Ignore a blank password when updating a database connection, so an
  empty or whitespace-only password can no longer overwrite the stored
  credential. The datastore now enforces the "leave blank to keep
  unchanged" rule server-side as defence-in-depth, rather than relying
  on the web client alone. The create path, which still requires a
  non-empty password, is unaffected. (#332)

- Move the collector, server, and alerter to Go 1.26.8, and update
  `golang.org/x/text` from v0.33.0 to v0.40.0. Scanning the previous
  toolchain and dependency set with `govulncheck` v1.6.0 reported ten
  advisories reachable from Workbench code in the server module, ten in
  the alerter, seven in `pkg`, and six in the collector. All but one are
  Go standard library issues fixed in the 1.26.4, 1.26.5, and 1.26.6
  patch releases, in `crypto/tls`, `crypto/x509`, `encoding/asn1`,
  `encoding/xml`, `net/http`, `net/textproto`, and `net/url`; the
  exception is an infinite loop in `golang.org/x/text` normalisation
  reached through connection-pool setup. After this change `govulncheck`
  reports no reachable vulnerabilities in any of the four modules. The
  Go version is raised in the module files, the three service
  `Dockerfile` builder stages, and the CI and release workflows; the
  release workflow matters most, because it previously built the
  published binaries with the affected toolchain. The server, alerter,
  and collector CI workflows now also run for changes under `pkg/`,
  matching the Docker workflow, so a change to the shared module can no
  longer publish service images without first passing their lint, vet,
  and test jobs.

- Update the web client's ECharts dependency to v6.1.0, which resolves a
  cross-site scripting advisory, and move the `vitest` development
  dependencies to v4.1.11, which refreshes the transitive `postcss`,
  `js-yaml`, `nanoid`, `brace-expansion`, `fflate`, and `vite` packages.
  Before this change `npm audit` reported eleven findings (six moderate,
  five high), of which only the ECharts advisory affected the shipped
  client bundle; the rest sat in the development dependency tree. After
  it, `npm audit` reports no known vulnerabilities.

## [1.0.0] - 2026-06-08

This release is the first general-availability release of the
pgEdge AI DBA Workbench, graduating the project from beta to a
stable, supported 1.0 line.

### Added

- Add a `password_file` option to the server's `database:` YAML
  block, allowing the server to read the datastore password from
  a file. The server uses this file only when no inline
  `password` value and no CLI password flag are set, bringing the
  server in line with the collector and alerter. (#267)

### Changed

- Unify how the server, collector, and alerter read secrets from
  files onto a single hardened helper. The change covers database
  and user passwords, service tokens, LLM and embedding API keys,
  server and notification secrets, proxy header values, and binary
  encryption keys. Several behaviours below are
  backward-incompatible, so operators upgrading the collector or
  alerter should review them before rolling out the new binaries.
  (#267)

- Trim only the trailing newline from a secret file, rather than
  stripping all surrounding whitespace. This applies to passwords,
  tokens, API keys, server and notification secrets, and proxy
  header values. Secrets that legitimately contain leading,
  trailing, or interior spaces are now preserved verbatim.
  Operational: operators who relied on the old
  whitespace-stripping behaviour may need to re-create affected
  secret files so the stored value matches the intended secret.
  (#267)

- Treat a configured-but-empty secret or password file as a hard
  startup error, instead of silently treating the file as "no
  value". Previously an empty file fell through to `.pgpass` or a
  passwordless connection, which could mask a misconfigured
  deployment. Operators must ensure every
  configured secret file contains a value before starting the new
  binaries. (#267)

- Accept any owner-only permission mode, such as `0400` or `0600`,
  for the binary key loaders that read the server encryption key
  and the alerter notification secret key, instead of requiring
  exactly `0600`. Group- or world-accessible modes such as `0640`
  and `0644` are still rejected. (#267)

- Expand a leading `~` in a secret or server-secret file path to
  the user's home directory consistently across all components.
  (#267)

- Allow the server's `-db-password-file` flag to accept paths
  containing `..`, so legitimate relative paths now work. The flag
  previously rejected any path containing `..`. (#267)

- Raise the web client's minimum Node.js version to 20.19 and
  enforce it through an `engines` field in the client
  `package.json` (`^20.19.0 || >=22.12.0`), with a matching
  `.nvmrc`. The client's build tooling, including Vite 8 and the
  `@csstools` packages, requires Node.js 20.19 or later, so older
  releases such as Node.js 18 failed the build with a cryptic
  error. The documented prerequisite now states the supported
  range (Node.js 20.19 or later on the 20.x line, or 22.12 or
  later) everywhere it appears. (#272)

### Fixed

- Fix the AI assistant "Ellie" and the client-side analysis prompts
  generating SQL against the old combined `pg_stat_bgwriter` columns
  such as `checkpoints_timed`, `checkpoints_req`, and
  `buffers_checkpoint`, which fail on PostgreSQL 17 and later. Those
  checkpoint statistics moved into the new `pg_stat_checkpointer` view
  in PostgreSQL 17. The system prompts now instruct the model to query
  `pg_stat_checkpointer` on PostgreSQL 17 and later and the combined
  `pg_stat_bgwriter` view on PostgreSQL 16 and earlier, choosing the
  view based on the target server's version. (#286)

- Fix the `get_metric_baselines` MCP tool returning no baselines
  when the `metric_name` filter used an unqualified shorthand such
  as `cache_hit_ratio`, because the tool matched the stored
  fully-qualified name such as `pg_stat_database.cache_hit_ratio`
  only on an exact match. The filter now applies a parameterised,
  case-insensitive containment match, with literal `%` and `_`
  matched literally, and a filter that matches nothing returns the
  available fully-qualified metric names within the caller's RBAC
  scope so the model can retry. (#287)

- Fix the alerter failing to load its server secret when
  `secret_file` was unset, with no fallback search. The alerter now
  resolves a default secret file the way the collector and server
  already do; when `secret_file` is not set, it searches the
  per-user config directory first and then `/etc/pgedge`. This
  brings the three services to parity and makes the search order
  documented in `examples/ai-dba-alerter.yaml` accurate. (#291)

- Fix the alerter's Gemini provider lacking a configurable embedding
  model. The `llm.gemini` section now exposes an `embedding_model`
  option that defaults to `gemini-embedding-001`; the alerter
  previously offered no embedding model field for Gemini. When Gemini
  is the embedding provider, the configured model must match the model
  that the KB Builder used to produce the knowledgebase. (#284)

- Fix the documented Gemini embedding model list, which referenced the
  deprecated `text-embedding-004` and `embedding-001` models that
  Google has retired. The supported models are now
  `gemini-embedding-001` (the default), `gemini-embedding-2`, and
  `gemini-embedding-2-preview`, all of which output 3072 dimensions
  rather than the previously documented 768. The server's
  knowledgebase status log now reports the Gemini API key as loaded.
  (#285)

- Fix the Admin Panel's Create Group, Edit Group, and Create
  Token dialogs accepting unsupported special characters and
  over-long Name values. RBAC group and token names now reuse the
  app's shared name validator on both the client and the server:
  after trimming, a name must be non-empty, at most 255
  characters, and may contain only letters, numbers, spaces, and
  the characters `. _ - ( )`. Creating a group with a name that
  already exists now returns a clear "A group with this name
  already exists" conflict (HTTP 409) instead of a generic server
  error. (#273)

- Fix the Admin Panel's Create User and Edit User dialogs
  accepting invalid usernames and email addresses and surfacing
  unhelpful errors. A username with special characters, an
  invalid email such as `notanemail` or `test@`, or a duplicate
  username previously returned a generic server error or stored
  silently. The client and server now apply matching rules: the
  server validates the username, validates the email format, and
  returns HTTP 400 with the specific rule, or HTTP 409 "Username
  already taken" for a duplicate. The dialogs show inline,
  field-level errors and block the save until the input is
  valid. (#271)

- Resolve two moderate-severity npm vulnerabilities reported in
  the web client's dependency tree by updating the affected
  transitive dependencies. (#272)

- Fix the server silently swallowing read errors for configured
  LLM, embedding, and knowledgebase API-key files. The server
  previously proceeded with no key when such a file was unreadable
  or empty; an unreadable or empty configured key file now fails
  loudly at startup. Leaving a key-file path unset remains valid
  and is not an error. (#267)

- Fix name fields for server groups, clusters, and servers
  accepting special characters such as `<>!@#$%`. Validation
  previously rejected only empty input, so disallowed
  characters were stored unchanged. The server and client now
  apply the same rule: after trimming, a name must be
  non-empty, at most 255 characters, and may contain only
  letters, numbers, spaces, and the characters `. _ - ( )`.
  The server returns HTTP 400 for invalid names across all
  cluster-group, cluster, and server-connection create and
  update endpoints, and the client shows an inline validation
  error and blocks the save. (#269)

- Fix the alerter intermittently and silently suppressing
  anomaly alerts that should have fired. The Tier 3
  LLM-based classifier parsed model responses
  non-deterministically, so the same response could resolve
  to either "alert" or "suppress" from one run to the next.
  The parser now strips markdown code fences and extracts an
  embedded JSON object before falling back to keyword
  matching, and the keyword fallback applies a deterministic,
  fail-safe precedence (alert before suppress, keep before
  clear) instead of iterating a randomly ordered map. An
  over-broad suppress keyword that matched phrases such as
  "deviation from normal behavior" was also tightened. The
  same anomaly condition now produces the same decision every
  time, so genuine alerts fire reliably. (#264)

- Fix the Add Cluster Group, Add Cluster, and Add Server
  dialogs accepting Name, Host, Maintenance Database, and
  Username values longer than 255 characters, which the
  database rejected through its `VARCHAR(255)` columns and
  surfaced as a generic, unhelpful server error. The web
  client now caps those fields at 255 characters so
  over-length input cannot be submitted, and the server
  validates their length and returns a clear 400 response (for
  example "Name must be 255 characters or less") instead of a
  generic 500. The server counts characters rather than bytes
  to match the `VARCHAR(255)` limit. (#270)
- Fix Ellie, the AI chat assistant, hanging in an effectively
  infinite tool-validation loop for regular, non-superuser
  users. When a regular user selected a connection shared with
  them and asked Ellie to describe it, the query-validation
  tool kept succeeding while the tools that execute the query
  failed with an access error. The assistant retried slightly
  different queries indefinitely, repeatedly showing
  `Validating query` and never responding. The chat loop now
  applies a repeated-failure circuit breaker: when the same
  tool fails with the same error three times, the assistant
  stops retrying and returns a clear message. The message names
  the failing tool, shows the underlying error, and suggests a
  likely permissions or connection-access problem to raise with
  an administrator. (#268)

### Security

- Log a warning recommending `chmod 600` when reading any secret
  file from a group- or world-readable path. The read still
  succeeds, so existing deployments continue to work while
  operators tighten the affected file permissions. (#267)

## [1.0.0-beta3] - 2026-05-26

### Added

- Add a hybrid variance floor and a per-`period_type` warmup
  gate to the alerter's Tier 1 anomaly detector, plus a
  symmetric z-score cap as defence in depth. Three new YAML
  blocks under `anomaly.tier1` expose the controls:
  `max_z_score` (default `100.0`, set to `0` to disable the
  clamp), `variance_floor` with `relative_pct` (default
  `0.05`) and `absolute_floor` (default `0.001`), and `warmup`
  with `min_samples` and `min_span_hours` pairs for `all`,
  `hourly`, and `daily` baselines. A new
  `metric_baselines.earliest_sample_at` column is added by an
  idempotent `ALTER TABLE` in the consolidated collector
  schema migration; the column is populated automatically by
  the alerter's baseline builder at the next refresh, and the
  warmup gate fails closed on NULL values until the column is
  populated. The `anomaly_candidates` and `alerts` tables are
  deliberately left untouched to preserve audit history.
  **Operational:** operators upgrading an existing deployment
  must `TRUNCATE TABLE metric_baselines` after stopping the
  alerter and before starting the new binary, so the detector
  rebuilds baselines under the new logic from a clean slate.

- Add a startup datastore schema health check to the MCP
  server. The server now reads the collector-owned
  `schema_version` table and probes a small set of critical
  dashboard tables before wiring any handlers; a missing or
  pre-v4 collector schema, or a partial drop of any probed
  relation, surfaces an operator-actionable error that
  names the affected datastore and the recommended action,
  then exits non-zero. This replaces the previous silent
  failure mode where the server would come up against an
  empty database and 500 on every dashboard endpoint.

- Add the `get_timeline_events` MCP tool, which exposes
  the unified incident-investigation timeline that was
  previously only reachable through the
  `/api/v1/timeline/events` REST endpoint. The tool
  returns configuration changes, HBA and ident edits,
  server restarts, extension changes, alert
  fired/cleared/acknowledged events, and blackout
  start/end markers in a single TSV result ordered most
  recent first. Ellie and other MCP clients can now
  correlate alerts with the underlying changes that may
  have caused them without falling back to direct REST
  calls. The tool gates results through the same RBAC
  checks as `get_alert_history`. (#250)

### Security

- Fix the server creating the SQLite `auth.db` file with
  world-readable mode `0644` on first start, exposing bcrypt
  password hashes and token hashes to other local users on
  the host. The previous `os.Chmod(0600)` call ran before the
  `modernc.org/sqlite` driver had created the file on disk,
  so the chmod silently failed with ENOENT and the file was
  left at the operating system's umask default. The fix moves
  the chmod after the `PRAGMA foreign_keys` query, which
  forces the driver to round-trip and create the file, and
  adds a post-chmod `os.Stat` verification that logs a
  warning if the resulting mode is not `0600`. The server
  applies the chmod unconditionally on every startup, so an
  existing `auth.db` left at `0644` by an earlier release is
  re-narrowed automatically and no manual operator action is
  required. On filesystems that silently ignore chmod (for
  example FAT or some FUSE mounts), the server now refuses to
  start and operators should consult the WARNING log line that
  reports the observed mode. (#249)

### Added

- Add Gemini as a supported embedding provider in the
  `embedding` and `knowledgebase` server configurations;
  the provider supports `text-embedding-004` (default)
  and `embedding-001`, both at 768 dimensions. (#246)

### Changed

- Update the default Gemini chat model in the alerter
  and server configurations from `gemini-2.0-flash`,
  which is no longer available to new API users, to
  `gemini-2.5-flash`. (#246)

### Fixed

- Fix the `get_timeline_events` MCP tool emitting a
  flat "Alert condition no longer active" summary on
  `alert_cleared` rows, which left a reviewer
  scanning the timeline to read the vivid
  `alert_fired` summary (carrying the alert's frozen
  `description` text) and risk misreading an already
  resolved alert as still active. Cleared rows now
  carry a self-contained summary of the form
  `Resolved after <duration>. Fired: <original alert
  description>`, where `<duration>` renders as
  `Ns`, `Nm Ms`, or `Nh Mm`; the underlying
  `alerts.description` column is preserved unchanged,
  so this is a presentation change in the timeline
  tool rather than a rewrite of historical alert
  text. The tool description visible to MCP clients
  now also states that `alert_fired.summary`
  describes the firing condition and not the current
  state, and instructs clients to pair each
  `alert_fired` row with its corresponding
  `alert_cleared` row (matching title prefix
  `'Alert Cleared: '`) before treating an alert as
  ongoing.

- Fix the collector entering a restart loop when its
  consolidated schema migration ran against a partially
  populated datastore. PostgreSQL has no
  `ALTER TABLE ... ADD CONSTRAINT IF NOT EXISTS`, so a
  re-run that found a pre-existing foreign key (such as
  `fk_anomaly_candidates_embedding` on
  `anomaly_candidates`) raised `duplicate_object`, aborted
  the surrounding `pgx.Tx`, and the next statement
  returned SQLSTATE 25P02 ("current transaction is
  aborted, commands ignored until end of transaction
  block") with a misleading error pointing at an
  unrelated table. The migration now guards every
  `ADD CONSTRAINT` with a catalog check against
  `pg_constraint`, runs the optional pgvector blocks
  inside SAVEPOINTs so a missing extension or schema
  drift cannot poison the parent transaction, and
  surfaces real failures with a `return` instead of
  swallowing them through `logger.Infof`. Fresh installs
  are unaffected; the change matters only for recovery
  cases.

- Fix the web client crashing into the "Something
  went wrong" error boundary after the user deleted
  an empty cluster. The server marshaled an empty
  `clusters` list (and the nested `servers` list
  inside each cluster) as JSON `null` rather than
  `[]`, and `MainLayout`'s selection `useMemo` then
  called `.some` on the null value without a guard.
  The server's `GetClusterTopology` now normalises
  both lists to a non-nil empty array, and the
  client's selection logic guards every read of
  `group.clusters` through a new `buildSelection`
  helper. The `ClusterGroup.clusters` TypeScript
  type is also tightened to `ClusterEntry[] | null`
  so this regression class is caught by the type
  system. (#242)

## [1.0.0-beta2] - 2026-05-14

### Added

- Document the TLS and reverse-proxy requirements for
  any network-accessible deployment in a new
  Administrator's Guide page at
  `docs/admin-guide/tls-and-reverse-proxy.md`; the
  page states explicitly that TLS termination, HTTP
  to HTTPS redirection, and HSTS are operator
  responsibilities at the reverse proxy layer, calls
  out the Vite dev server on port 5173 as
  localhost-only and unsupported for any
  network-accessible use, notes that the server's
  built-in TLS support remains available for
  operators who choose to terminate at the
  application, and enumerates the credential-exposure
  risks of running the workbench over plain HTTP.
  Cross-reference callouts now appear in the
  installation, quick-start, Docker, and web-client
  configuration pages. (#234)
- Add a Playwright-based end-to-end smoke-test suite
  that drives the production client bundle in a real
  browser against a real server and Postgres on every
  pull request; the suite runs across a
  Chromium/Firefox/WebKit matrix and is invoked
  locally with `make test-e2e`. See
  `docs/developer-guide/e2e/index.md` for details.
  (#236)
- Capture Go integration coverage from the running
  server during the end-to-end suite and merge it into
  the existing Codacy partial-upload pipeline, so unit
  and integration coverage combine into a single
  reported figure. (#236)
- Add the `spock_exception_log` and `spock_resolutions`
  collector probes; both probes capture a rolling
  15-minute window of the Spock extension's
  exception and conflict-resolution catalogs and
  no-op cleanly on databases without Spock installed.
  (#200)
- Add six built-in alert rules in the `replication`
  category: `spock_recent_exceptions_present`,
  `spock_recent_exceptions_high`,
  `spock_recent_resolutions_present`,
  `spock_recent_resolutions_high`,
  `replication_slot_retention_warn`, and
  `replication_slot_retention_high`; the Spock rules
  require the `spock` extension and the slot
  retention rules apply to every PostgreSQL
  deployment. (#200)

### Changed

- **Breaking change:** the web client container now
  runs as a non-root user and listens on port
  **8080** instead of port 80. The base image in
  `client/Dockerfile` switched from
  `nginx:stable-alpine` to
  `nginxinc/nginx-unprivileged:stable-alpine`, with
  an explicit `USER nginx` directive. Host-side port
  mappings in `docker-compose.yml`,
  `docker-compose.prod.yml`, and the walkthrough
  compose file are unchanged, so
  `http://localhost:3000` continues to work with the
  default `CLIENT_PORT`. Operators running custom
  reverse-proxy configurations, Kubernetes
  manifests, or external `proxy_pass` upstreams that
  target container port 80 must update those
  references to 8080; this includes Service
  `targetPort` values, health probes, and any
  direct container-to-container references.
- **Breaking change:** the collector, alerter, and
  server no longer auto-discover configuration or
  secret files in the binary directory or the current
  working directory; review the migration steps below
  before upgrading. (#195)

    - The new lookup order is the `--config` flag, the
      per-user config directory, and `/etc/pgedge/`;
      the first match wins, and missing files fall
      through to compiled-in defaults.
    - The per-user path resolves to
      `~/.config/pgedge/<binary>.yaml` on Linux
      (honouring `$XDG_CONFIG_HOME`),
      `~/Library/Application Support/pgedge/<binary>.yaml`
      on macOS, and `%AppData%\pgedge\<binary>.yaml`
      on Windows.
    - The same precedence applies to the collector and
      server secret files (`ai-dba-collector.secret`
      and `ai-dba-server.secret`); the alerter does not
      use a secret file.
    - Production deployments that already use
      `/etc/pgedge/` are unaffected.
    - Development setups that drop a YAML file next to
      the binary or in the current working directory
      will silently fall through to compiled-in
      defaults; move the file to `/etc/pgedge/` or the
      per-user directory, or pass `--config` with an
      explicit path.
    - The alerter's `SIGHUP` handler re-runs discovery
      on each reload, so installing a config at a
      default location after startup is picked up on
      the next signal.
- Replace the composition-rule password validator
  with a policy aligned to NIST SP 800-63B; the
  server now requires a minimum of 12 characters,
  enforces the 72-byte bcrypt upper bound, drops
  uppercase, lowercase, digit, and special-character
  requirements, and rejects passwords found in a
  built-in dictionary of approximately 10,000 common
  and breached entries. The web client shows live
  password-strength feedback as the user types, and
  the server remains the authoritative validator.
  (#177)
- Document installation paths for each deployment
  method (GitHub release, Docker, RPM/DEB) in the
  installation guide with a reference table. Add
  cross-reference notes to the quick start, Docker,
  and sub-project README files. Align manual-install
  systemd service names to `pgedge-ai-dba-*` to
  match RPM/DEB package conventions. (#173)
- Reject `cors_origin: "*"` at server startup when
  authentication is enabled. Browsers discard
  credentialed responses that combine
  `Access-Control-Allow-Origin: *` with
  `Access-Control-Allow-Credentials: true` per the
  Fetch spec. Operators should configure an explicit
  origin or leave the option empty for same-origin
  deployments. (#81)
- Migrate the `collector`, `alerter`, and `server`
  `.golangci.yml` configurations to the golangci-lint v2
  format, and update the CI workflows to install
  `golangci-lint/v2`; `make test-all` now works again on
  developer machines that have golangci-lint v2
  installed locally. (#66)
- Apply a Biome and ESLint auto-fix pass across
  `client/src/`, clearing roughly 600 Codacy findings
  across 294 files; the change is a mechanical refactor
  with no behavior changes, and existing lint and test
  baselines remain unchanged.
- Clear all `@typescript-eslint/no-confusing-void-expression`
  findings in `client/src/` across 80 files; ESLint's
  auto-fixer resolved 279 sites and 19 remaining cases were
  rewritten manually by expanding
  `() => cond && voidFn()` into explicit `if` blocks. No
  behavior changes, and all 2,604 Vitest tests pass.
- Raise line coverage of `server/internal/crypto` from
  86.8% to 100%. New tests cover four previously uncovered
  error branches. The branches are random source failure,
  `ReadFile` failure, `WriteFile` failure, and the GCM
  encrypt failure path. (#78)
- Add integration tests for `server/internal/memory.Store`
  against PostgreSQL. The tests cover all nine public
  methods: `NewStore`, `Store`, `Search`, `GetPinned`,
  `ListByUser`, `GetByID`, `Delete`, `DeleteByID`, and
  `UpdatePinned`. They also exercise pgvector similarity
  ordering, scope visibility, and ownership checks.
  Package line coverage for the memory store now reaches
  92.5%. (#78)
- Add the `-race` flag to the `test` and `coverage`
  Makefile targets in the `server`, `collector`, and
  `alerter` sub-projects. The race detector now runs in
  CI and on developer machines. (#78)
- Auto-collapse the Server Dashboard's "System
  Resources" section when its data is unavailable,
  typically because the `system_stats` PostgreSQL
  extension is not installed on the connected server;
  the section previously stayed expanded and rendered
  five empty CPU, Memory, Disk, Load, and Network IO
  panels that pushed the "PostgreSQL Overview"
  section far down the page. The collapsed header now
  shows the italic message "No data available. Is the
  system_stats extension installed?" next to the
  title, and the user can still expand the section
  manually to inspect the empty panels. The manual
  override is intentionally not persisted to
  `localStorage`, so the section returns to the
  user's previous expand or collapse preference once
  the extension is installed. The shared
  `CollapsibleSection` component gained two new
  props, `forceCollapsed` and `forceCollapsedMessage`,
  which temporarily override the persisted state
  without mutating storage and render the italic
  header message; an anti-flicker guard delays the
  force-collapsed state until the initial KPI fetch
  completes, so the section does not briefly collapse
  during loading.
- Bump the Go toolchain from 1.26.1 to 1.26.2
  across the server, collector, alerter, and `pkg`
  modules and the dev-container image; the upgrade
  picks up upstream fixes for seven Go security
  advisories listed in the Security section.
- Bump `github.com/jackc/pgx/v5` from 5.7.6 to
  5.9.2 in the server, collector, and alerter; the
  upgrade picks up the memory-safety and
  dollar-quoted-string fixes listed in the Security
  section.
- Add a `.codacy.yaml` configuration that suppresses
  confirmed false-positive findings from Codacy's
  Semgrep and ESLint8 engines; suppressions are
  scoped to specific files or to `__tests__/**`
  globs, were independently reviewed by the
  security-auditor agent, and mask no real
  vulnerabilities.
- Reduce the web client's default `body1` and `body2`
  typography sizes to the MUI standards of 16px and 14px,
  and remove the roughly 80 inline `fontSize` overrides
  that previously compensated for the oversized defaults;
  body text across the application now renders smaller and
  more consistently, while headings and subtitles are
  unchanged.
- Add a `CHART_AXIS_LABEL_FONTSIZE` token in
  `client/src/theme/tokens.ts` so chart axis labels render
  at a consistent 14px, and add a `MONO_CAPTION_SX` token
  that deduplicates the monospace caption styling shared
  by alert thresholds, Spock node info, and cron
  schedules.
- Consolidate four duplicated patterns in `server/src` as
  part of the codebase cleanup tracked in #77. The
  copy-pasted `getClient()` helper in
  `internal/tools/context_aware_provider.go` and
  `internal/resources/context_aware_registry.go` now
  delegates to a new
  `(*database.ClientResolver).ResolveOrError` method that
  returns the canonical "no database connection
  configured" error. The 14-field
  `chat.NewClientFromConfig` invocation repeated in
  `internal/llmproxy/proxy.go` (`HandleModels` and
  `HandleChat`) and `internal/overview/generator.go`
  (`createLLMClient`) collapses into a new
  `chat.NewClientFromLLMConfig` factory that takes an
  `LLMOptions` parameter for per-call overrides such as
  `Model`, `MaxTokens`, `Temperature`, `Debug`, and
  `Headers`, removing roughly 40 lines of boilerplate per
  call site. The two `auth.ConnectionVisibilityLister`
  adapters in `internal/api/helpers.go` and
  `internal/database/visibility_lister.go` share a single
  projection; the slice-based adapter moves into the
  `database` package as `database.NewSliceVisibilityLister`
  and the projection is exported as
  `database.ConnectionsToVisibilityInfo`. The five-line
  closure that wired
  `(*database.Datastore).GetConnectionSharingInfo` into an
  `auth.RBACChecker` from
  `internal/tools/context_aware_provider.go`,
  `internal/resources/context_aware_registry.go`, and
  `cmd/mcp-server/handlers.go` now flows through a new
  `auth.NewRBACCheckerForDatastore` constructor that
  accepts the datastore through a small
  `DatastoreSharingLookup` interface and handles nil and
  typed-nil cases internally. The change is internal-only
  and behavior-preserving; no public HTTP API, MCP tool,
  or configuration surface changes. (#77)

### Security

- Pick up upstream fixes for seven Go security
  advisories by bumping the toolchain to 1.26.2;
  the advisories are CVE-2026-32280 (certificate
  chain validation denial of service), CVE-2026-33810
  (DNS-constraint certificate validation bypass),
  CVE-2026-32281 (certificate chain validation denial
  of service), CVE-2026-32283 (TLS 1.3 key-update
  denial of service), CVE-2026-32289 (`html/template`
  cross-site scripting), CVE-2026-32288
  (`archive/tar` denial of service), and
  CVE-2026-32282 (`Root.Chmod` symlink escape).
- Pick up upstream fixes in `github.com/jackc/pgx/v5`
  by bumping to 5.9.2; the advisories are
  CVE-2026-33816 (Critical, memory safety) and
  GHSA-j88v-2chj-qfwx (Low, SQL injection through
  dollar-quoted-string and `$N` placeholder
  confusion). A code audit confirmed that no query
  in this project mixes `$$...$$` literals with `$N`
  placeholders, so the second advisory was
  theoretical for our code base; the bump is still
  warranted as a defence-in-depth measure.
- Bump the web client container's base images to
  pick up upstream fixes for high-severity CVEs
  flagged by Docker Scout. The builder stage moves
  from `node:22-slim` to `node:22-trixie-slim`
  (Debian 13), which closes CVE-2026-33845 and
  CVE-2026-33846 in `gnutls28`. The runtime stage
  moves from `nginxinc/nginx-unprivileged:stable-alpine`
  to `nginxinc/nginx-unprivileged:stable-alpine-slim`,
  which closes CVE-2026-3805 in `curl` on Alpine
  3.23; the slim variant omits `curl`, which the
  runtime does not need. One residual high finding,
  CVE-2026-33671 in the `picomatch` package bundled
  inside npm, persists across all Node 22 and 24
  tags pending an npm release; the residual lives
  only in the builder stage and never reaches the
  shipped image. The non-root UID 101 nginx user,
  port 8080, and other hardening from the earlier
  base-image change are preserved, and
  `docker scout cves` reports no vulnerable
  packages in the final image.
- Redact notification channel secrets from API responses;
  `GET /api/v1/notification-channels` and
  `GET /api/v1/notification-channels/{id}` no longer return
  `smtp_username`, `smtp_password`, `webhook_url`, or
  `auth_credentials`, all of which were previously emitted in
  plaintext after server-side decryption. Each response now
  includes the boolean indicators `smtp_username_set`,
  `smtp_password_set`, `webhook_url_set`, and
  `auth_credentials_set` so clients can show whether a secret
  is configured without ever reading the value. The
  `PUT /api/v1/notification-channels/{id}` endpoint applies a
  three-way merge to the four secret fields; omit a field to
  preserve the stored value, send an empty string to clear
  it, or send a new value to replace it. The web admin UI for
  the Email, Slack and Mattermost, and Webhook channel
  editors now leaves secret form fields blank when editing an
  existing channel and preserves the stored value unless the
  user types a replacement. (#187)

- Fix log, SQL, and SMTP injection findings surfaced by
  the golangci-lint v2 upgrade; the knowledgebase search
  now binds its filter values through `?` placeholders
  instead of string concatenation, the email test sender
  sanitizes envelope and header fields before writing
  them to the SMTP connection, and user-tainted values
  are escaped through a new `logging.SanitizeForLog`
  helper at log sites across the api, auth, and config
  packages. (#66)

- Hoist RBAC access-control checks above all
  datastore calls in the alert-counts, alert
  acknowledgement, alert unacknowledgement, alert
  analysis, and cluster-topology handlers; zero-grant
  callers now short-circuit to an empty response
  without touching the database, and HTTP-level
  regression tests cover every affected handler.
  (#67)

- Extend the `manage_connections` gate to the remaining
  connection paths missed by the earlier sweep under
  #207; the server's `createConnection` handler now
  requires the permission for every new connection
  rather than only for shared connections, the
  `handleUpdateConnectionCluster` handler behind
  `PUT /api/v1/connections/{id}/cluster` now also
  requires the permission so that a user with only
  read-visibility on a connection can no longer re-home
  it between clusters, and the web client's Add menu
  hides the "Add Server" entry from users who lack the
  permission. The new server-side gate sits after the
  existing visibility check and before any body decode
  or datastore mutation. Unauthorized callers receive a
  `403 Forbidden` response with a clear authorization
  error, and the OpenAPI specification and the static
  `docs/admin-guide/api/openapi.json` document the new
  response on the affected paths; the previously
  undocumented `/connections/{id}/cluster` path is now
  present in the specification with both its GET and
  PUT methods. (#233)

- Require the `manage_connections` permission on all
  cluster and cluster-group mutating endpoints; users
  without the permission previously could create
  cluster groups and clusters through the REST API,
  and the server silently committed the rows and
  returned a success status even though the resulting
  records were invisible to the creator while remaining
  visible to administrators. A follow-up audit found
  the same gap on additional mutating routes, so the
  fix now gates eleven endpoints in total: creating
  and deleting cluster groups, adding clusters to a
  group, creating, updating, and deleting clusters,
  adding and removing cluster servers, and creating,
  updating, and deleting cluster relationships.
  Unauthorized callers now receive a `403 Forbidden`
  response with a clear authorization error instead of
  a misleading success, and the group owner can still
  delete a group they own even without the permission.
  The web client's Add menu now hides the "Add Cluster
  Group" and "Add Cluster" entries from users who lack
  `manage_connections`, and the OpenAPI specification
  and static `docs/admin-guide/api/openapi.json`
  document the `403 Forbidden` response on the
  affected paths. Administrators see no functional
  change. (#207)

### Fixed

- Fix the Replication Type select in the Cluster Settings
  dialog rendering blank for auto-detected clusters whose
  `replication_type` column is NULL; the dialog now derives
  the value from the cluster's `auto_cluster_key` prefix when
  no explicit replication type is set, mirroring the existing
  behaviour of the Topology panel. (#235)
- Fix auto-detected clusters lingering in the cluster
  autocomplete dropdown after their last member connection
  was deleted; `DeleteConnection` now runs in a single
  transaction that marks the parent cluster `dismissed=TRUE`
  when the cluster was auto-detected (`auto_cluster_key IS
  NOT NULL`) and no connections still reference it. The
  delete itself uses `DELETE ... RETURNING cluster_id` to
  close a TOCTOU window between the lookup and the delete,
  and the rollback path uses `context.Background()` to avoid
  a pgx v5 close-of-closed-channel panic when the request
  context is canceled mid-transaction. User-created clusters
  and connections without a cluster assignment are
  unaffected. (#245)
- Fix anomaly alerts displaying raw dotted metric names such
  as `pg_replication_slots.max_retained_bytes` in the Status
  Panel, and six alert rules seeded by collector migration v3
  rendering as auto-title-cased fallbacks in the Admin Alert
  Rules panel; the `FRIENDLY_ALERT_TITLES` map now includes
  curated labels for 15 dotted anomaly metric names and the 6
  v3 rule names, with full test coverage of the map. (#244)
- Fix long, non-wrapping SQL queries flowing underneath the
  copy and run icons in the Remediation Steps panel of the
  alert AI analysis view; the shared markdown styles now
  reserve right-side padding on each code block sized to the
  number of overlaid action buttons, so the SQL text never
  collides with the icons. (#221)
- Fix the Active Alerts Restore button returning HTTP 500
  "Failed to unacknowledge alert" for alerts that were
  already non-acknowledged (for example after the alerter
  reactivated them following a severity change); the server
  now maps a missing alert to 404, an alert that is not
  currently acknowledged to 409 Conflict, and wraps every
  failure path with the alert ID for actionable logs. The
  alerter's auto-reactivation path is also hardened against
  panicking on alerts with a NULL `metric_value` column and
  captures the previous severity before the database write
  so the in-memory comparison cannot drift from the
  acknowledged state. (#227)

- Fix the alerter's `replication_slot_inactive` critical
  alert never firing because its metric query selected
  from a non-existent `metrics.pg_stat_replication_slots`
  table; the `pg_replication_slots.inactive` metric now
  reads directly from `metrics.pg_replication_slots` (the
  table the collector probe writes to) and derives the
  inactive state from the `active` column. New integration
  tests cover the happy path, the no-row case when every
  slot is active, slot deduplication per connection, and
  the 5-minute freshness cutoff. (#224)

- Fix Ask Ellie incorrectly reporting missing Spock
  replication slots on healthy Spock 6.x clusters; the
  assistant previously generated
  `WHERE plugin = 'spock'` against
  `pg_replication_slots`, but current Spock releases name
  the output plugin `spock_output`. The chat system prompt
  in `server/src/internal/chat/llm.go` now instructs Ellie
  to use `plugin LIKE 'spock%'` for cross-version
  compatibility. (#220)

- Fix three datastore schema and probe inefficiencies
  identified during a production performance review.
  Collector migration v4 adds a partial index on
  `anomaly_candidates(detected_at)` filtered to
  `processed_at IS NULL AND tier1_pass = TRUE` so the
  alerter sweeper stops sequential-scanning the full
  table on every poll, and drops the redundant
  `idx_pg_stat_all_indexes_conn_db_time` and
  `idx_pg_stat_statements_conn_db_time` indexes (along
  with their attached child indexes on every existing
  weekly partition) which were duplicated by the more
  specific `_object` indexes. The change-detection probes
  (`pg_settings`, `pg_extension`, `pg_hba_file_rules`,
  `pg_ident_file_mappings`) now strip the
  `ai_dba_wb_probe` marker column injected by
  `WrapQuery` before hashing, so the live snapshot hash
  matches the stored snapshot hash; previously the marker
  caused every hourly collection to look "changed" and
  write a fresh snapshot, inflating the `pg_settings`
  partitions by roughly an order of magnitude. (#219)

- Fix the Admin panels showing a success toast alongside a
  page-level refresh error when a save succeeded but the
  follow-on reload failed; the shared `useCrudPanel` hook
  now suppresses the success toast when the post-mutation
  refresh fails, so the user sees only the actionable
  refresh error. (#215)

- Fix the `AdminMessagingChannels` panel flipping the
  `deleteLoading` busy flag when a user toggled a
  channel's enabled state; the shared
  `useCrudPanel.runMutation` helper gained an
  independent `busyTarget` option
  (`'save' | 'delete' | 'inline'`) that decouples the
  busy-state flag from the `errorTarget`, and a new
  `'inline'` error target routes errors only to the
  caller through the returned result rather than to
  `crud.error` or `crud.dialogError`. Defaults preserve
  the previous behavior so existing call sites are
  unchanged, and `handleToggleEnabled` now uses
  `busyTarget: 'inline'`. (#216)
- Fix the divergent error fallback wording shown by the
  Admin panels when a thrown value is not an `Error`
  instance. The `AdminUsers`, `AdminMemories`,
  `AdminTokenScopes`, `AdminPermissions`, `AdminProbes`,
  `AdminEmailChannels`, `AdminWebhookChannels`,
  `AdminMessagingChannels`, and `AdminGroups` panels, along
  with the `useChannelCRUD` hook, now route non-`Error`
  throws through the shared `extractErrorMessage` helper in
  [`client/src/components/AdminPanel/_shared/errors.ts`](https://github.com/pgEdge/ai-dba-workbench/blob/main/client/src/components/AdminPanel/_shared/errors.ts).
  The helper returns the generic `'An unexpected error
  occurred'` message instead of leaking output such as
  `[object Object]` produced by `String(err)`. Panels that
  pass a contextual fallback (for example, "Failed to add
  recipient") to the helper's second argument retain that
  context-specific wording. (#212)
- Fix the `Chart.test.tsx` regression introduced in
  commit `aa28aa8` that has been failing the CI -
  Client workflow on every commit since; the
  vitest mock specifier `echarts-for-react/lib/core`
  was not updated to `echarts-for-react/esm/core`
  when production code switched to the ESM path,
  leaving the real `ReactEChartsCore` running in
  tests and tripping the deliberately-narrow
  `echarts/core` mock. The change updates only the
  test mock specifier; no production source code
  was modified.
- Fix the npm-install branch in
  `start_dev_web_client.sh` never firing because an
  intervening `echo` clobbered `$?` before the
  `if [ $? -eq 0 ]` check; the script now uses a
  direct `&&`/`||` pattern that tests the previous
  command's exit status without an intermediate
  command.
- Fix Ask Ellie entering a long retry loop ("Joining the
  relations..." / "Validating query") when the signed-in
  user has no MCP privileges; the chat now surfaces a
  clear permission-denied message immediately instead of
  cycling through planning steps. (#188)
- Fix wide markdown tables overflowing and clipping the
  right-side columns inside the Ask Ellie chat panel;
  tables returned by MCP tools such as
  `get_alert_history`, `get_alert_rules`, and
  `query_datastore` now sit inside a horizontally
  scrollable wrapper, so narrow tables still fill the
  bubble while wide tables scroll within it instead of
  spilling outside. The shared `MarkdownContent`
  helper used by other dialogs received the same
  treatment. (#185)
- Fix the web client rendering a blank screen on every
  navigation when the LLM proxy was enabled with a
  reasoning model that returns a structured `summary`
  object; `AIOverview` now coerces non-string summaries
  to text before rendering, removing the React error
  that triggered the blank screen. The top-level
  `<ErrorBoundary>` has also been rewritten to always
  show the error message and component stack in a
  collapsible details block and to expose a "Reload"
  button, so users can recover from a crash and file
  actionable bug reports without rebuilding the
  container. (#182)
- Fix stale auto-detected edges remaining in
  `cluster_node_relationships` after the cluster
  topology changed through failover, subscriber
  removal, or a new parent in a binary chain;
  `SyncAutoDetectedRelationships` now replaces the
  auto-detected set transactionally, deleting all
  existing `is_auto_detected = TRUE` rows for the
  cluster before inserting the freshly detected set,
  and the `syncRelationshipsFromTopology` caller no
  longer short-circuits on an empty detected slice.
  Manual relationships and auto-detected rows for
  other clusters are preserved, and a failure during
  the delete or insert rolls the transaction back to
  the prior state. (#152)
- Fix the cluster Topology tab dropping cascading
  standbys and marking empty auto-detected nodes as
  expandable; persisted and manual chains such as
  primary -> standby -> cascading standby now render
  every level regardless of input order, and nodes
  whose children are filtered out no longer display a
  disclosure arrow. (#153)
- Fix the collector probe config loader ignoring scope
  and silently re-enabling disabled parent overrides;
  `LoadProbeConfigs` now restricts its query to
  `scope IN ('global', 'server')` so cluster- and
  group-scoped rows no longer collapse into the
  `connection_id = 0` bucket and get misapplied as
  global defaults, and `EnsureProbeConfig` now inherits
  the parent config's `is_enabled` value when
  materializing a server-level row instead of
  hard-coding it to true. The SQL is extracted into a
  `loadProbeConfigsQuery` constant and the value
  resolution moves into a pure
  `resolveProbeConfigDefaults` helper, both covered by
  new unit tests. (#151)

- Fix MCP and admin scope privileges granted through a
  wildcard group grant (`"*"`) being silently dropped
  during token scope intersection; the intersection
  logic now recognises wildcard grants and preserves
  the scoped privileges. The fix also introduces an
  explicit `AccessLevelNone` constant to replace raw
  empty strings for "no access" semantics, improving
  code clarity and reducing error-prone comparisons.
  (#96)

- Fix the ClusterNavigator group-editing flow
  round-tripping string group ids (`"group-{x}"`)
  through numeric parses; the string id now travels
  unchanged through `handleConfigureGroup`,
  `handleSaveGroup`, and the cluster actions context,
  and the one `strconv.Atoi`-compatible conversion
  happens at the `GroupDialog` override-panel
  boundary. Auto-detected groups without a numeric
  backing row now display an info alert explaining
  that alert, probe, and channel overrides are
  unavailable instead of silently passing `NaN` to
  the override panels. This removes the root cause
  patched symptomatically in #59. (#63)

## [1.0.0-beta1] - 2026-04-21

### Added

- Add the `llm.timeout_seconds` server configuration
  option to control the HTTP client timeout for
  requests to the configured LLM provider; the
  default remains 120 seconds. (#60)
- Add a guided walkthrough example with pre-seeded
  demo data and an in-browser Driver.js tour covering
  the workbench's major features.

### Changed

- Default the knowledgebase `database_path` to the
  pgEdge package install path at
  `/usr/share/pgedge/postgres-mcp-kb/kb.db`. (#52)

### Fixed

- Remove misleading raw API key options from the
  example server configuration files; the server
  only accepts API keys through the corresponding
  `*_file` variants. (#54)
- Fix spurious "partition would overlap" errors from
  the collector on non-UTC hosts when the weekly
  partition rolled over. (#55)
- Fix foreign key violations during alerter baseline
  calculation when metric rows outlive their
  connection; historical metric queries now filter
  through the connections table. (#56)
- Fix MCP tool invocations failing with TLS
  certificate verification errors against servers
  that require a custom `sslrootcert`, `sslcert`, or
  `sslkey`; the server now forwards these fields on
  the database connection string. (#57)
- Fix reactivated alerts continuing to appear as
  acknowledged in the GUI by clearing stale
  `alert_acknowledgments` rows when an alert leaves
  the acknowledged state; the alerts API now also
  exposes a nullable `last_updated` RFC3339 timestamp
  and the StatusPanel surfaces it alongside
  "Triggered" when the two differ. (#64)
- Fix the cluster Topology tab "Add server" dropdown
  silently excluding servers that had been re-claimed
  by an auto-detected Spock cluster; the connections
  API now returns the `membership_source` field the
  client filter requires. (#25, #46)
- Fix dismissed auto-detected clusters reappearing
  after the collector's next auto-detection run;
  `UpsertAutoDetectedCluster` no longer clears the
  `dismissed` flag on rediscovery, and `GetCluster`
  now filters dismissed rows from single-cluster
  lookups. (#36)
- Fix dismissed auto-detected clusters reappearing in
  the Server creation dialog's cluster dropdown after
  alert or connection context was fetched; the
  connection hierarchy resolver now skips dismissed
  rows and no longer resurrects them through its
  upsert fallback. (#36)
- Fix partitions not being dropped at the appropriate
  time by the collector. (#62)
- Fix the copy-to-clipboard button in the Admin Tokens
  "Token created" dialog; the button now shows a check
  mark and "Copied!" tooltip on success and surfaces
  clipboard failures through the error alert. (#71)
- Fix the StatusPanel "Restore to active" action
  silently failing on error; the alert now leaves the
  acknowledged list optimistically, rolls back and
  surfaces a Snackbar error on API failure, and
  guards against double-click submissions. (#72)
- Fix servers assigned to a manually created cluster
  continuing to appear under a re-created
  auto-detected cluster after the next topology
  refresh; auto-detected Spock, binary-replication,
  and logical-replication grouping now skip
  connections with `membership_source = 'manual'`.
  (#74)
- Fix `GET /api/v1/connections` returning an empty
  array for scoped API tokens when the token owner's
  read access came from a wildcard group grant; the
  scoped connections are now returned as expected,
  and token scopes continue to restrict but not
  elevate the owner's privileges. (#83)

### Security

- Fix several REST endpoints and MCP tools leaking
  unshared connections owned by other users; the
  connection detail, database listing, connection
  context, cluster topology, alerts, alert
  acknowledgement, alert analysis, timeline, and
  current-connection endpoints now apply the same
  ownership, sharing, group, and token-scope checks
  as `GET /api/v1/connections`, returning
  `403 Forbidden` for single-resource requests and
  filtering list responses to the caller's visible
  connections. The `get_alert_history`,
  `get_metric_baselines`, and `get_blackouts` MCP
  tools applied no connection filter for callers
  with zero explicit grants; all three now restrict
  results to connections the caller is permitted to
  see. The OpenAPI specification and the static
  `docs/admin-guide/api/openapi.json` now document
  the `403 Forbidden` response on the affected
  single-resource endpoints. (#35)
- Fix server and cluster visibility leaks through the
  cluster list, cluster group, and cluster-membership
  REST endpoints; `GET /api/v1/clusters/list`,
  `GET /api/v1/clusters/{id}`,
  `GET /api/v1/clusters/{id}/servers`,
  `GET /api/v1/cluster-groups`, and
  `GET /api/v1/cluster-groups/{id}` now filter clusters,
  groups, and member servers to the caller's visible
  connections and return `404 Not Found` for clusters or
  groups that contain no visible members. (#35)
- Fix additional RBAC leaks surfaced by the follow-up
  security audit; the overview REST and SSE endpoints
  (`GET /api/v1/overview` and `GET /api/v1/overview/stream`)
  now verify scope visibility for scoped requests and
  filter `connection_ids` lists to the caller's visible
  set; the blackout list and get endpoints
  (`GET /api/v1/blackouts` and
  `GET /api/v1/blackouts/{id}`) hide blackouts whose
  referenced connection, cluster, or group is not
  visible to the caller; the alert, probe, and channel
  override list endpoints
  (`GET /api/v1/alert-overrides/{scope}/{scopeId}`,
  `GET /api/v1/probe-overrides/{scope}/{scopeId}`, and
  `GET /api/v1/channel-overrides/{scope}/{scopeId}`)
  return `404 Not Found` when the caller cannot see the
  requested scope; and the MCP connection resolver now
  checks RBAC before loading credentials and returns a
  generic "connection not found or not accessible"
  message for both missing and denied cases. (#35)
- Fix remaining RBAC leaks flagged by the third-round
  security audit; the `query_metrics` and `get_alert_rules`
  MCP tools now check `CanAccessConnection` before any
  datastore read and return a generic "connection not
  found or not accessible" error for both missing and
  denied connections, closing the ID/name enumeration
  that the previous error path exposed. The overview
  scoped-snapshot path
  (`GET /api/v1/overview?scope_type=cluster|group`) now
  intersects the scope's member connection IDs with the
  caller's visible set and generates the summary from
  the intersection through the existing connections-
  summary path, so two callers with different visibility
  never share a cache entry; scope denial now returns
  `404 Not Found` to match sibling handlers. The blackout
  schedule list and get endpoints
  (`GET /api/v1/blackout-schedules` and
  `GET /api/v1/blackout-schedules/{id}`) apply the same
  visibility filter as the blackout endpoints, and the
  alert override context endpoint
  (`GET /api/v1/alert-overrides/context/{connectionId}/{ruleId}`)
  now gates on `scopeVisibleToCaller` before reading
  override hierarchy. The MCP connection resolver logs
  RBAC denials so operators can correlate without
  widening the caller-visible surface. (#35)

## [1.0.0-alpha3] - 2026-04-08

### Added

- Add a Docker publish workflow that builds and pushes
  multi-platform images to GitHub Container Registry
  on version tags and main branch pushes.
- Add a production Docker Compose configuration using
  pre-built images with resource limits and log
  rotation.
- Add a Docker deployment guide to the documentation.
- Add a favicon to the web client.
- Replace the SQLite driver with a pure-Go
  implementation to support CGO-free builds.

### Changed

- Improve blackout status indicators and require
  confirmation before deleting a blackout. (#34)
- Limit blackout scope options to relevant entries
  only. (#33)
- Allow servers from auto-detected clusters to be
  reassigned to manual clusters. (#46)
- Hide alert threshold links for users who lack the
  required permission. (#40)
- Display errors to users when fetching unassigned
  servers fails.

### Fixed

- Fix the blackout dialog refreshing unnecessarily
  when underlying components update. (#47)
- Fix missing browser refresh after certain
  navigation actions and prevent title wrapping. (#45)
- Fix the replication type not carrying through to
  the edit dialog. (#44)
- Fix multiple potential crashes after a network
  failure and recovery. (#43)
- Fix a serialization error when updating server
  details. (#42)
- Fix a crash when clicking an empty cluster. (#39)
- Fix inconsistent alert operator values. (#38)
- Fix auto-detected clusters reappearing after
  deletion. (#36)
- Fix the is_shared flag for servers not being
  respected in all cases. (#35)
- Fix connection error alerts ignoring active
  blackouts. (#32)
- Fix MCP write access to databases. (#29)
- Fix SSL settings being silently dropped when
  creating or updating a server.
- Fix various issues with the database summary popup.
- Fix various TypeScript type safety issues in the
  web client.

### Security

- Fix MCP memory tools bypassing RBAC checks; all
  authenticated users could access the datastore
  without proper authorization.

## [1.0.0-alpha2] - 2026-03-04

### Fixed

- Fix a crash in the Add Server dialog when no clusters
  exist in the database.

## [1.0.0-alpha1] - 2026-03-02

Initial release.
