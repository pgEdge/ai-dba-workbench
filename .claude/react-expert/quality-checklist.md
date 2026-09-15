/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# React Quality Checklist

This document records the client conventions that are specific to
this repository and cannot be derived from the code alone. Generic
React and TypeScript practice is assumed; the local ESLint and
Prettier configuration under `client/` enforces the mechanical
rules.

## CRITICAL: Font Size Rules

**The target minimum font size is 14px (0.875rem) for new code.**

This target applies to:

- ECharts axis labels (`fontSize` in `axisLabel` options)
- Chart legends and tooltips
- Small labels, captions, footnotes
- Any text rendered in the UI

Avoid `fontSize: 10`, `fontSize: 11`, `fontSize: 12`, or
`fontSize: 13` in new code. These are below the target minimum
defined in `typography-guidelines.md`.

When configuring chart axis labels, use the shared
`CHART_AXIS_LABEL_FONTSIZE` token from
`client/src/theme/tokens.ts` (currently 14) rather than a bare
numeric literal. Reach for the token directly inside ECharts or
Recharts option objects; the chart libraries accept a plain
number and do not pick up MUI Typography variants.

Note: many existing components use 12px and smaller sizes. These
are known technical debt. Do not introduce new violations, but
existing instances do not need to be fixed in unrelated changes.

## AdminPanel Error Handling

AdminPanel components must use the shared `extractErrorMessage`
helper from `client/src/components/AdminPanel/_shared/errors.ts`
for every catch block that surfaces an error string to the user.
Do not reintroduce ad-hoc patterns; the helper guarantees a
uniform fallback wording for non-`Error` throws.

Banned patterns in AdminPanel catch blocks:

- `String(err)` or `String(apiErr)` — produces unfriendly
  `[object Object]`-style output for non-`Error` rejections.
- Inline `err instanceof Error ? err.message : '...'` ternaries
  that hard-code a divergent fallback wording.
- Verbose `if (err instanceof Error) { setError(err.message) }
  else { setError('An unexpected error occurred') }` blocks.

Required pattern for the generic case:

```ts
try {
    await apiPost('/api/v1/something', body);
} catch (err: unknown) {
    setError(extractErrorMessage(err));
}
```

When the call site needs a more descriptive fallback (for
example, a recipient-add handler), pass the context as the
second argument; the helper still returns the `Error.message`
when available:

```ts
crud.setDialogError(
    extractErrorMessage(err, 'Failed to add recipient'),
);
```

Reference implementations:

- `client/src/components/AdminPanel/_shared/errors.ts` defines
  the helper and the `DEFAULT_ERROR_MESSAGE` constant.
- `client/src/components/AdminPanel/AdminUsers.tsx`,
  `AdminPermissions.tsx`, `AdminMemories.tsx`, `AdminProbes.tsx`,
  `AdminTokenScopes.tsx`, `AdminEmailChannels.tsx`,
  `AdminWebhookChannels.tsx`, `AdminMessagingChannels.tsx`,
  `AdminAlertRules.tsx`, `AdminGroups.tsx`, and
  `channels/useChannelCRUD.ts` all route their catch blocks
  through `extractErrorMessage`.

Tests for AdminPanel mutation paths must cover both the
`Error`-instance branch and the non-`Error` fallback. Reject a
mock with a plain string and assert that
`'An unexpected error occurred'` (or the contextual fallback
passed at the call site) renders in the appropriate alert or
dialog.

## TextField Native Input Attributes

## Dashboard Chart Semantics

Metric series carry units and semantics, and a chart must not mix
kinds of quantity on one shared axis. The two kinds seen most often
in this project are gauges, such as `numbackends`, which are
point-in-time values bounded by a configured limit, and cumulative
counters, such as `sessions` or `xact_commit`, which only ever
climb until `stats_reset`. Plotting one of each on a single axis
lets the counter set the scale, flattens the gauge into a
featureless line, and invites the reader to compare two numbers
that are not comparable.

The rules for new or modified charts are as follows:

- Give a gauge and a counter their own charts, even when the
  underlying metric query fetches both in one request; splitting
  the presentation does not require splitting the query.

- Draw a reference line for a gauge whose limit is known. The
  `Chart` component does not register the ECharts `MarkLine`
  component, so add the limit as an extra constant series with the
  same units instead of reaching for `markLine`; passing `series`
  through `echartsOptions` would replace the real data, because
  `deepMerge` assigns arrays rather than merging them.

- Say what the numbers cover in the chart title and legend. The
  `pg_stat_database` probe filters on `current_database()`, so
  anything derived from it describes the monitored database rather
  than the whole server, and the title should say so.

- Never plot a raw `pg_stat_*` counter on a time-series chart. The
  metrics API derives `<column>_per_sec` (the LAG delta over the
  elapsed seconds, aggregated across the bucket, so query it with
  `aggregation: 'avg'`) and `<column>_delta` (the per-bucket sum of
  counter increases, with the `aggregation` parameter ignored). Use
  `_per_sec` for anything that reads as a rate and `_delta` for
  anything counted per interval, such as checkpoints or bytes spilled
  to temporary files, and carry the unit in the legend and the tile
  ('Commits/s', 'WAL Bytes/s', unit `/s`). Cumulative sessions, which
  are deliberately shown as a running total, are the one remaining
  exception, and its legend says 'Cumulative Sessions' so that a
  reader does not mistake it for a rate.

- Anchor a time-series chart's x-axis to the window the server says
  it queried, never to the points that came back (issue #430).
  `/api/v1/metrics/query` answers its time-series mode with an
  envelope (`MetricsQueryResult` in
  `client/src/components/Dashboard/types.ts`) carrying `probe_name`,
  `connection_ids`, `time_range`, `time_start`, `time_end`,
  `bucket_seconds`, `buckets`, `aggregation` and `series`; its
  latest-rows mode (a request passing `limit` or `order_by`) still
  answers with flat rows. `useMetrics` keeps `data` as the series
  array and exposes the window separately as
  `{ start, end, bucketSeconds }`. Build categories and align series
  with `client/src/components/Dashboard/metricsChart.ts`
  (`buildWindowCategories`, `alignPointsToWindow`,
  `buildMetricChartData`, `buildDerivedChartData`) rather than
  mapping `d.time` and `d.value` in the component, so that a range
  the instance has no history for reads as missing history instead
  of redrawing the same few hours on every range.

- Treat a `null` data point as a gap, never as 0. Every bucket of the
  window is present in a `/api/v1/metrics/query` response and every
  series shares those bucket times, so a bucket with no reading (a
  collector gap, a counter reset or a rate that cannot be derived)
  carries `"value": null`; rates never carry forward and gauges carry
  forward for at most three probe intervals. A point whose value is
  the last observation carried forward rather than an observed sample
  also carries `"filled": true`, which `buildMetricChartData` passes
  to `ChartDataSeries.filled`: the option builders then draw those
  points with a hollow marker, shade each contiguous carried-forward
  stretch once with a `markArea` (from the first series only, so
  bands cannot compound) and append '(carried forward)' in the
  tooltip, so the distinction never rests on colour alone.
  `MetricDataPoint.value` and `ChartDataSeries.data` are therefore
  nullable, ECharts draws a null as a break in the line or a missing
  bar (leave `connectNulls` unset), and the shared tooltip labels it
  'no data'. Any arithmetic over points must skip nulls explicitly:
  `extractLatestValue` and `extractLatestRate` (copies in the
  `ServerDashboard`, `DatabaseDashboard` and `ObjectDashboard`
  `types.ts` files) return the last non-null point, a valid zero
  reading included, since only null marks a missing bucket; a ratio
  with a null on either side is null; a sum
  across a window ignores null buckets and is null when none carry a
  reading; and `Sparkline` renders nothing for an all-null series.
  Filling a null with 0 (`value ?? 0`) is only acceptable inside a
  running total, where a missing bucket genuinely contributes nothing.

- Keep a KPI tile's arithmetic consistent with the metric it reads:
  a `_per_sec` tile reports the latest bucket, whereas a `_delta`
  tile sums its buckets across the selected window (Temp Bytes) or
  derives a ratio from those sums (Requested Checkpoints, which is
  the requested share of all checkpoints and shows '--' when no
  checkpoint occurred). A `metricDescription` that describes a
  cumulative total whilst the tile plots a rate, or the reverse, is
  a bug: the AI analysis reads it.

- Label a derived or estimated series as one, everywhere it appears.
  `available_memory` on `pg_sys_memory_info` is the worked example: the
  `system_stats` extension exposes no equivalent of the kernel's
  `MemAvailable`, so the collector stores `free_memory + cache_total`
  and NULL when either input is missing, which overstates availability
  when the page cache is dirty or non-reclaimable slab is large. The
  client charts it as 'Available (est.)', says the same in the
  `metricDescription` the AI analysis reads, and carries a
  `Typography variant="caption"` line beneath the `ChartPanel` (there
  is no subtitle or description slot on `Chart` or `ChartPanel`)
  separating it from 'Free', which is `MemFree` and counts only wholly
  unused memory.

- Show a supporting figure on a KPI tile through `KpiTile`'s optional
  `secondaryText`, not through `trend`/`trendValue`, which mean a
  direction and carry a trend colour and icon. It renders as a
  `caption` line beneath the headline value and is appended to the
  tile's `aria-label`. Pass `undefined` when the figure is
  unavailable so no line renders at all; never a '0 B', 'NaN' or '--'
  placeholder in that slot.

- Report a failed metrics query rather than letting it read as an
  absence of data. `ChartPanel` takes an optional `errorMessage`,
  rendered in `color="error"` in place of `emptyMessage` whenever it
  is set and the query is not loading, and every chart should pass
  the `error` from its `useMetrics` result. An older server answers
  an unknown derived metric with HTTP 400 'metric not found in
  probe', which would otherwise show as a bare 'No data' message.

- Never average a probe that records one row per entity without
  filtering to a single entity first. `pg_sys_disk_info` stores a row
  per mounted filesystem, so an unfiltered `aggregation: 'avg'` blends
  the data volume, the WAL volume and every pseudo filesystem into a
  figure that matches no real volume (issue #428).
  `MetricQueryParams.mountPoint` emits the `mount_point` filter, and
  `client/src/components/Dashboard/ServerDashboard/diskMounts.ts`
  discovers the real mounts: `PSEUDO_FILESYSTEM_TYPES` lists the
  kernel-backed and image-backed types to exclude (kept in step with
  the alerter's SQL fragment in
  `alerter/src/internal/database/metric_registry.go`),
  `selectRealMounts` orders the rest fullest first so that the default
  selection matches what the alerter's disk threshold reports, and
  `useDiskMounts` reads them from the latest-row mode of
  `/api/v1/metrics/query`, which applies `DISTINCT ON` over the
  probe's entity keys and so returns one current row per mount. One
  piece of selection state drives both the tile and the chart, both
  name the mount in their label, and the selector is hidden on a
  single-disk host. `pg_sys_network_info` and
  `pg_sys_io_analysis_info` have the same shape and are still
  unfiltered; that is issue #400.

- Skip the query outright rather than sending an empty dimension.
  `buildMetricsUrl` omits a falsy filter, so params carrying
  `mountPoint: ''` ask for the averaged figure the filter exists to
  remove, both on first paint and for good on a host with no real
  filesystem. `useMetrics` takes `MetricQueryParams | null` and does
  not fetch on null, so pass null until the dimension is known.
  `useDiskMounts` returns `{ mounts, settled }` for the same reason:
  an in-flight lookup and a host with no real filesystem both carry
  an empty list but want different things on screen, so the panel
  shows its loading state until `settled` and only then the empty
  state, whose message names the real problem ('No real filesystem
  reported for this server.') rather than blaming `system_stats`,
  which is plainly working.

- Take a usage percentage from the reported total rather than from
  used plus free wherever the probe records one. A filesystem with
  reserved blocks holds back space counted in neither, so the two
  figures disagree; the Disk Usage tile therefore asks for
  `total_space` alongside `used_space`.

The reference implementation is
`client/src/components/Dashboard/ServerDashboard/PostgresOverviewSection.tsx`,
which draws backends with a `max_connections` reference series,
keeps cumulative sessions on a separate chart and queries rate
metrics for transactions, block I/O and tuple operations. It reads the limit
from the latest `pg_server_info` row through the latest-row mode of
`/api/v1/metrics/query` (`limit` and `order_by` parameters), since
that probe only stores a row when the server configuration changes
and a bucketed query would usually come back empty.

### Ratios and gaps

A ratio derived from counters must be computed per interval, never
from the lifetime totals, because a since-`stats_reset` average
cannot show a current problem (issue #401). `ChartDataSeries.data`
and the `SparklinePoint` type in `client/src/components/Dashboard/types.ts`
are `(number | null)[]` and `number | null` respectively: a null
entry is a bucket with no value, ECharts draws it as a gap, the
axis-range code in `Chart/options/common.ts` skips it, and
`useChartAnalysis` leaves it out of the statistics it sends to the
LLM. The rules are as follows:

- For a ratio scoped to one database, ask `/api/v1/metrics/query`
  for the derived `<counter>_per_sec` metrics with a `databaseName`
  and compute the ratio per bucket; the shared helpers live in
  `client/src/components/Dashboard/cacheHitRatio.ts`
  (`buildCacheHitRatioPoints`, `latestCacheHitRatio`). The Database
  dashboard's `PerformanceSection` is the worked example.

- Never derive a server-wide ratio from `blks_hit_per_sec` and
  `blks_read_per_sec` requested without a database. The
  derived-metrics query sums the counters across databases and only
  then differences them, so a database created inside the window
  adds its lifetime counter to one interval and a dropped one drives
  the delta negative. Read `cache_hit_ratio` from
  `/api/v1/metrics/performance-summary` instead, where the server
  differences per database before summing; the
  `useServerCacheHit` hook in `client/src/hooks/` does this for the
  server dashboard's `PostgresOverviewSection`, sending `time_start`
  and `time_end` for a custom range and making no request whilst a
  custom range still lacks a bound, as `useConnectionGroups` does.

- An idle bucket is null, never 0% and never 100%; a headline value
  is the latest non-null bucket of the ratio series, shown as `--`
  when there is none. Build the per-bucket ratio first and read its
  last non-null point; never scan for the numerator and the
  denominator separately with `extractLatestValue`, because the two
  scans stop at whichever bucket each series last filled and can
  pair readings from different buckets. Nor may a missing side
  default to 0 (`dead ?? 0`), which turns a gap into a reassuring
  0.0% built from no data. The Database dashboard's Dead Tuple Ratio
  and Transactions tiles both read `latestNonNull` over their paired
  sparkline for this reason; a bucket whose counts are genuinely
  both zero is still a real 0% and is reported as such.

- Render a null headline in a neutral colour (`text.secondary`)
  rather than the critical red, and keep nulls in bar data so the
  bar is left empty rather than drawn at zero.

- The server sends `cache_hit_ratio.current` and each
  `time_series[i].value` as `number | null`; the client types in
  `StatusPanel/PerformanceTiles/types.ts` and
  `Dashboard/ServerDashboard/types.ts` mirror that, and headline and
  worst-of computations skip nulls.

- Cache hit ratio descriptions carry the page-cache caveat from
  `CACHE_HIT_CAVEAT`: `blks_hit` counts `shared_buffers` hits only,
  and a read may still be served from the OS page cache, so a lower
  ratio does not by itself mean slow I/O.

## Dashboard Time Window

Every dashboard request that has a time dimension takes its window
from `useDashboard().timeRange`, a `TimeRangeState` of `range` plus
an optional `customStart` and `customEnd`. There is no shared helper
yet, so each call site repeats the same two-part pattern that
`useMetrics` (`client/src/hooks/useMetrics.ts`) establishes:

- Always send `time_range`, and send `time_start` and `time_end`
  only when the range is `custom` and both bounds are present; the
  server accepts the bounds for no other range.

- Skip the request entirely whilst a custom range has only one
  bound, leaving the existing data and error state alone. A
  half-specified custom window is a transient state the user passes
  through in the picker, and sending it earns a 400 and a visible
  error for no benefit. `useMetrics`, `useServerCacheHit`,
  `useConnectionGroups`, `useQueryStats`, `TopQueriesSection` and
  `QueryDetail` all do this.

- Put `range`, `customStart` and `customEnd` in the fetch callback's
  dependency list, so that moving the selector refetches.

`/api/v1/metrics/top-queries` is windowed, and both of its callers
pass the selected range: `TopQueriesSection` for the leaderboard and
`QueryDetail` for the header statistics behind the overlay a row
opens. The overlay takes the range from context rather than from the
overlay payload, so nothing has to be threaded through `pushOverlay`.

The windowed response aggregates `calls`, `rows`, `total_exec_time`
and the block counters as sums of non-negative deltas, and derives
`mean_exec_time` from those sums, but `min_exec_time` and
`max_exec_time` cannot be delta-aggregated and remain lifetime
`pg_stat_statements` values. The two `QueryDetail` tiles that show
them are therefore labelled `Min Time (All Time)` and `Max Time
(All Time)`, whilst the mean tile is plain `Mean Time` because it
now follows the selector. Any new tile reading a windowed endpoint
must say in its label which of the two it is.

Five summary-tile call sites still hardcode `time_range=24h`
(`usePerformanceSummary`, `useDatabaseCacheHit`,
`DatabaseSummariesSection`, `KpiTilesSection` and
`ComparativeChartsSection`); that is deliberate for now and is being
reviewed separately, so do not sweep them into an unrelated change.

## TypeScript Standards

`client/package.json` depends on `@mui/material` at `^5.14.20`. The
committed lockfile resolves that to `5.18.0`, where `slotProps` works
and a handful of components already use it (`ConnectionLostOverlay.tsx`,
`ClusterFields.tsx` and `TopQueriesSection.tsx`), but the declared
minimum of `5.14.20` does not guarantee `slotProps` on `TextField`, so
do not spread it further unless the minimum is raised. The convention
for native `<input>` attributes (`maxLength`, `min`, `max`,
`aria-label`, `autoComplete` when not already a top-level prop) is
`inputProps` on `TextField`, used across some twenty files (for
example, `ConnectionFields.tsx` uses `inputProps={{ min: 1, max:
65535 }}` for the port field). Follow the convention of the file
you are editing rather than mixing the two in one component.

Short free-text fields backed by `VARCHAR(255)` columns (names,
hosts, database names, usernames) must enforce a client-side
length cap so over-length input cannot reach the server. Use the
shared `MAX_FIELD_LENGTH` constant from `src/utils/formLimits.ts`
via `inputProps={{ maxLength: MAX_FIELD_LENGTH }}` rather than a
bare `255`. Tests assert the cap with
`expect(field).toHaveAttribute('maxlength', '255')` on the
underlying input (the rendered DOM attribute is lowercase
`maxlength`).

## AdminPanel Name-Field Validation

AdminPanel dialogs that expose a free-text "Name" field (the
Create/Edit Group dialog and the Create Token dialog, where the
"Name" label maps to the backend `annotation`) must validate the
field against the same contract the server enforces. Reuse the
canonical client helper at `src/utils/validateName.ts`, the same
one `GroupDialog.tsx` (the cluster group dialog) uses, rather than
re-implementing the rules per dialog:

- `validateName(value)` returns the exact inline error string, or
  `null` when the trimmed value is valid. Feed it the raw
  (untrimmed) field value; it trims internally. Empty input is
  treated as invalid and returns `NAME_ERROR_REQUIRED`.
- Drive the submit button's `disabled` state and the early-return
  guard in the submit handler with `validateName(name) !== null`;
  this also covers the empty/required case. For the token dialog,
  combine it with the existing owner check: `owner &&
  validateName(annotation) === null`.
- Do not surface the "Name is required" message while the field is
  empty; suppress the inline error for an empty trimmed value
  (`value.trim() === '' ? null : validateName(value)`) so the
  dialog never greets the user with a hard error before they type.
  The disabled submit button already communicates the requirement.
- `NAME_MAX_LENGTH` is `255`. Pass it via
  `inputProps={{ maxLength: NAME_MAX_LENGTH }}` to hard-cap typing,
  and surface the validator output through the TextField
  `error`/`helperText` props for the over-length and
  invalid-character cases.

The allowed character set is ASCII letters, digits, spaces,
periods, underscores, hyphens, and parentheses
(`NAME_PATTERN = /^[A-Za-z0-9 ._()-]+$/`). The error strings are
fixed and must match the canonical helper exactly: "Name must be
255 characters or fewer" and "Name may only contain letters,
numbers, spaces, and the characters . _ - ( )".

Server duplicate-name conflicts (HTTP 409) reach the dialog
automatically: `apiPost`/`apiPut` throw an `ApiError` whose
`message` carries the server body, and `useCrudPanel.runMutation`
routes that message to `dialogError` when no `errorFallback`
overrides it. Do not pass an `errorFallback` to the create/edit
group mutations, or the server's "A group with this name already
exists" message will be masked. Token names are intentionally not
unique, so no duplicate handling belongs in the token dialog.

## Messaging Channel Field Descriptors

`AdminMessagingChannels.tsx` is shared by the Slack, Mattermost and
Telegram admin panels. It renders no hardcoded credential field:
each platform passes a `MessagingChannelConfig` whose `fields` are
`MessagingChannelField` descriptors, defined with the pure helpers
in `client/src/components/AdminPanel/messagingChannelFields.ts`.
Add a platform by writing a new wrapper like
`AdminTelegramChannels.tsx`; do not add per-platform branches to
the shared panel.

Each descriptor names the API request field (`key`), its form
label, and how the API reports it back: `setFlag` for a secret the
server redacts (issue #187) and only reports as configured, e.g.
`webhook_url_set` or `telegram_bot_token_set`; `valueKey` for a
non-secret the server echoes, e.g. `telegram_chat_id`. The
resulting semantics are load-bearing and must not drift:

- A `secret` field is masked (`type="password"`), is never
  populated from the API when the edit dialog opens, and is
  omitted from the PUT body while blank. Sending an empty string
  would overwrite the stored secret with nothing.
- A non-secret field is pre-populated from the response on edit and
  is always sent, on both create and update.
- `required` secrets are mandatory on create and on an edit where
  the `setFlag` is false; they are optional once one is stored.
  `required` non-secret fields are mandatory in both cases.
  `findMissingRequiredField` is the single source of truth and
  drives both the submit button's `disabled` state and the save
  handler's guard.
- `showInTable` adds a table column: a `setFlag` field renders a
  Configured / Not configured chip, a `valueKey` field renders the
  value. Slack and Mattermost deliberately opt out, so their table
  keeps the five base columns.

Tests for a new platform belong in a per-platform file (see
`__tests__/AdminTelegramChannels.test.tsx`) and must assert that
the secret is absent from the PUT body when left blank and that it
never reaches the rendered document.

## Permission-Gated UI

Hide actions a user cannot perform; do not render disabled
placeholders. Read permissions from the `useAuth` context hook at
`client/src/contexts/useAuth.ts`. The hook returns
`hasPermission(perm: string)`, which already accounts for the
`isSuperuser` flag and the `*` wildcard in `adminPermissions`, so
call sites only need to ask for the specific permission they
require.

In the following example, the menu item only renders when the user
holds `manage_connections`:

```tsx
import { useAuth } from '../contexts/useAuth';

const { hasPermission } = useAuth();
const canManageConnections = hasPermission('manage_connections');

return (
    <Menu>
        <MenuItem>Always visible</MenuItem>
        {canManageConnections && (
            <MenuItem onClick={onAddCluster}>Add Cluster</MenuItem>
        )}
    </Menu>
);
```

Reference implementations:

- `client/src/components/AdminPanel/index.tsx` filters whole nav
  sections by `hasPermission(item.permission)`.
- `client/src/components/AddMenu.tsx` hides Add Server, Add Cluster,
  and Add Cluster Group when the user lacks `manage_connections`.

Permission strings must match the server-side constants exactly;
see `server/src/internal/auth/types.go` for the canonical list (for
example, `auth.PermManageConnections` maps to the string
`"manage_connections"`).

Tests for gated UI should cover three states:

- User with the permission renders the gated items.
- User without the permission renders neither the items nor any
  divider or affordance that only exists to separate them, but
  every ungated item still renders.
- The unauthenticated or still-loading state (where
  `hasPermission` returns `false` for everything) renders the
  same safe default as the "without permission" case.

Mock `useAuth` per test file rather than the full `AuthProvider`
so the permission shape is explicit at the call site:

```ts
const mockHasPermission = vi.fn<(perm: string) => boolean>(() => true);
vi.mock('../../contexts/useAuth', () => ({
    useAuth: () => ({ hasPermission: mockHasPermission }),
}));
```

## MUI Icon Imports

Always import icons as named exports from the package root, for
example `import { PauseCircleOutline } from '@mui/icons-material';`,
never from a deep path such as
`@mui/icons-material/PauseCircleOutline`. The package has no
`exports` map, so a deep default import resolves to the CommonJS
build in the production bundle and the imported value becomes the
whole exports object; React then throws error #130 when it renders.
Vitest resolves the deep path natively, so unit tests pass and only
`npm run build` plus a browser session shows the failure.

## LLM Chat Requests

Every `POST /api/v1/llm/chat` call goes through
`src/utils/llmChat.ts`: use `LLM_CHAT_PATH` for the path and
`buildChatRequestInit({ messages, tools, systemPrompt, signal })` for
the `RequestInit`. That builder is the only place that knows the
library wire contract (typed content blocks via `normaliseMessages`,
snake_case `tools[].input_schema` via `normaliseTools`, and
`system_prompt`), and it omits `tools` when the list is empty and
`signal` when none is given. Never hand-assemble the body at a call
site; issue #370 was a silently emptied tool schema caused by exactly
that, and #373 centralised the construction. Current callers are
`utils/agenticLoop.ts`, `hooks/chat/chatAgenticLoop.ts`,
`hooks/useChartAnalysis.ts` and `hooks/useQueryOverview.ts`.

## Coverage Requirements

The 90% line coverage floor in `CLAUDE.md` applies to all new and
modified client code; `cd client && make coverage` runs Vitest with
the thresholds configured in `client/vitest.config.js`, and a run
below them fails.

## Codacy and Local Lint Differences

Codacy runs Biome and type-aware ESLint rules that the local
`client/eslint.config.js` does not enable, so a clean `npm run lint`
does not guarantee a clean Codacy review. Standing rules that follow
from that:

- Do not bulk-apply auto-fixers for rules the local config does not
  enable; in particular `non-nullable-type-assertion-style` rewrites
  `(x as T)` to `x!` and trips the local `no-non-null-assertion`
  rule, and `prefer-nullish-coalescing` swaps of `||` for `??` need
  a per-site check of the left-hand type.
- For `no-confusing-void-expression` findings on one-line handlers
  of the form `() => cond && sideEffect()`, prefer
  `() => void (cond && sideEffect())` over a block body, which would
  add a branch to a function that may already sit near Codacy's
  complexity threshold; never wrap an expression that returns a
  real value in `void (...)`.
- A `.find()` flagged by that rule usually means `useState([])`
  without a type argument, which TypeScript infers as `never[]`; fix
  the state type rather than suppressing the rule.
- Any auto-fix pass can strip adjacent `eslint-disable` comments.
  Measure the `npm run lint` warning count before and after, and run
  `npm run format:check`, so the counts match the baseline.
