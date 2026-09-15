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

- Treat a `null` data point as a gap, never as 0. Every series in a
  `/api/v1/metrics/query` response has the same bucket times, and a
  bucket with no reading (a collector gap, a counter reset or a rate
  that cannot be derived) carries `"value": null`; rates never carry
  forward and gauges carry forward for at most three probe intervals.
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

- Report a failed metrics query rather than letting it read as an
  absence of data. `ChartPanel` takes an optional `errorMessage`,
  rendered in `color="error"` in place of `emptyMessage` whenever it
  is set and the query is not loading, and every chart should pass
  the `error` from its `useMetrics` result. An older server answers
  an unknown derived metric with HTTP 400 'metric not found in
  probe', which would otherwise show as a bare 'No data' message.

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

## Bidirectional Isolation for Server-Supplied Names

Wrap any display name or email that the server may have taken from an
identity provider in `<bdi>` (or set `unicode-bidi: isolate` on the
element that holds it). The server is byte-transparent for these
values: it rejects control characters but permits the Unicode format
category, because a zero-width joiner is part of legitimate names in
several scripts, so the value can carry a bidirectional override such
as U+202E and reorder the interface text around it. React's JSX
escaping does not help, since no markup is involved and the
characters are ordinary text being rendered as instructed. The sites
that do this today are the display name and email columns in
`components/AdminPanel/AdminUsers.tsx`, and the federated sign-in
button label in `components/Login.tsx`. The authenticated `User`
object carries only `username`, which the server constrains, so the
header needs no isolation; add `<bdi>` at any new site that renders a
provider-supplied string.

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
