<!--
/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
-->

# Colour Contrast Guidelines

The light and dark palettes are defined in
`client/src/theme/pgedgeTheme.ts`; read the values there rather than
from this file. These rules exist so that new UI meets WCAG AA
contrast (4.5:1 for normal text, 3:1 for large text and for UI
components against adjacent colours) using that palette.

## Palette Is Mode-Dependent

Every `grey.*` key, `divider`, `text.*` and `action.hover` differs
between the light and dark palettes, so never reason about a palette
key as a single hex value and never hard-code a hex that happens to be
right in one mode. Reference palette keys through the theme and check
both modes in the browser.

## Persistent Fills

- Grey panel fills in light mode need an alpha of at least 0.12 to
  read as a panel on white; 0.05 to 0.08 is near-invisible and is
  reserved for transient hover states, which is what the theme's
  `action.hover` uses.
- Coloured status fills (info, warning, success, error) need at least
  0.10 in light mode as persistent backgrounds.
- Grey panel fills in dark mode need around 0.30.
- 0.05 is the absolute floor for any visible background; `alpha(x,
  0.04)` is never acceptable, even for hover.

## Borders

- Light mode: use `grey.300` or darker; `grey.200` against white
  fails the 3:1 component requirement.
- Dark mode: use `grey.600` or lighter.
- Dashed borders need a little more contrast than solid ones because
  less ink covers the edge.

## Text

- In dark mode `text.secondary` must hold 4.5:1 against
  `background.paper`, and `text.disabled` 3:1. Anything at `grey.500`
  or darker fails for text on dark backgrounds.
- Reserve `text.disabled` for genuinely disabled controls; never use
  it for content the user needs to read.
