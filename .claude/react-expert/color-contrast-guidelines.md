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

## Brand Cyan Is Not A Text Colour In Light Mode

Measured against a white or near-white surface (the login card
composites to #FAFAFB over its background gradient):

- `primary.main` (#15AABF) is 2.67:1, which fails both the 4.5:1 that
  normal text needs and the 3:1 that a component boundary needs. White
  on #15AABF is 2.79:1, so a contained button in that colour fails
  too.
- `primary.dark` (#0C8599) is 4.17:1: enough for a border, a focus
  ring or any other boundary, still short for a text label.

So a coloured call to action in light mode needs either a much darker
cyan than the palette carries or, as the federated sign-in button in
`components/Login.tsx` does, a neutral treatment: `text.primary` for
the label (14.07:1) and `grey.500` for the border (4.63:1), keeping
the cyan for the hover tint and the focus ring where 3:1 applies.
There are older uses of `primary.main` as a text colour in this
client; they are a known debt, not a precedent to follow.

## Text

- In dark mode `text.secondary` must hold 4.5:1 against
  `background.paper`, and `text.disabled` 3:1. Anything at `grey.500`
  or darker fails for text on dark backgrounds.
- Reserve `text.disabled` for genuinely disabled controls; never use
  it for content the user needs to read.

## Status Chip Labels

Status chips across the client paint their label on an
`alpha(<status>.main, 0.15)` background. Never reuse the status colour
itself, nor its `.dark` shade, as the label colour: text and background
then share a hue and land between 1.9:1 and 4.0:1 in light mode, well
under 4.5:1.

Take the label colour from `theme.palette.custom.chipText` instead,
which carries a per-mode shade (a deep 800-level shade in light mode,
the `.light` shade in dark mode) chosen so that every status clears
4.5:1 against that background on `background.paper`. The theme's own
`MuiChip` `filled` overrides use the same shades, so a chip given
`color="success"`, `"error"` or `"warning"` is already correct and
needs no `sx` colour at all.

`chipText` carries `success`, `error` and `warning` only, so a chip
whose meaning is not one of those statuses has no safe filled
treatment. Give it the neutral outlined form instead:
`variant="outlined"` with `color: 'text.primary'` for the label and
`grey.500` in light mode or `grey.600` in dark mode for the border,
which is what the "Federated" chip in the Type column of
`components/AdminPanel/AdminUsers.tsx` does. The "Service Account"
chip in the same column still paints `info.main` on
`alpha(info.main, 0.15)`; that is known debt, not a precedent.

Measure rather than eyeball: `client/src/test/contrast.ts` provides
`compositeOver` and `contrastRatio`, and the tests in
`client/src/theme/__tests__/pgedgeTheme.test.ts` assert the ratio for
every status in both modes.
