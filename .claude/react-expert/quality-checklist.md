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

- Label a cumulative series as cumulative in the legend, so that a
  reader does not mistake it for a rate.

The reference implementation is
`client/src/components/Dashboard/ServerDashboard/PostgresOverviewSection.tsx`,
which draws backends with a `max_connections` reference series and
keeps cumulative sessions on a separate chart. It reads the limit
from the latest `pg_server_info` row through the latest-row mode of
`/api/v1/metrics/query` (`limit` and `order_by` parameters), since
that probe only stores a row when the server configuration changes
and a bucketed query would usually come back empty.

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
