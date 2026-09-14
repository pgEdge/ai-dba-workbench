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

# Typography Guidelines

The theme in `client/src/theme/pgedgeTheme.ts` is the single source of
truth for font families, the type scale and per-variant weights and
spacing; the shared size tokens live in `client/src/theme/tokens.ts`.
This file records only the decisions behind those values and the rules
that a reader cannot infer from them. Do not copy values from the theme
into this file; read the theme.

## Decisions Behind the Scale

- The base size is 16px (1rem) and `body1` is 16px, `body2` 14px.
  An earlier scale set both at 18px; it was reduced after user
  feedback that the interface rendered uniformly oversized, so
  `subtitle1` (18px) now sits above body text and provides a real
  hierarchy. Do not raise the body sizes again without that context.
- 14px (0.875rem) is the minimum for new text, including chart axis
  labels, legends, tooltips and captions. Many existing components
  still carry 10px to 13px sizes; that is known debt, and it is not
  to be fixed in unrelated changes, but no new instances are allowed.
- The primary face is Inter with system fallbacks; the monospace face
  is JetBrains Mono with fallbacks and is reserved for code and
  technical values (connection strings, SQL, query output, OIDs).

## Rules for Component Code

- Use MUI `Typography` variants from the theme rather than hand-wired
  `fontSize` values. Where an override on a `Typography` matches a
  variant (`1.125rem`, `1rem`, `0.875rem`), replace the override with
  `variant="subtitle1"`, `"body1"` or `"body2"` and delete it.
- Keep explicit numeric `fontSize` only where variant inheritance does
  not apply: chart option objects, Chips, MUI inputs and other
  non-`Typography` elements. Chart axis labels use
  `CHART_AXIS_LABEL_FONTSIZE`; monospace caption rows use
  `MONO_CAPTION_SX`; icon sizes use `ICON_10_SX`, `ICON_14_SX` and
  `ICON_16_SX`. Add a token only when the same literal repeats three
  or more times.
- Uppercase labels use the `overline` variant's letter-spacing
  (`0.08em`). Existing components also use `0.05em`; both are
  acceptable, but stay consistent within a component and avoid other
  values.
- If a new size is genuinely needed, add it to the theme first rather
  than introducing an arbitrary `sx` value.

## Heading Capitalisation

| Level    | Capitalisation | Example                        |
|----------|----------------|--------------------------------|
| h1 to h3 | Title Case     | "Server Performance Overview"  |
| h4 to h6 | Sentence case  | "Connection details"           |
| overline | UPPERCASE      | "ADMIN PERMISSIONS"            |
| caption  | Sentence case  | "Last updated 5 min ago"       |

Title Case capitalises every significant word but not articles,
conjunctions or prepositions under four letters; sentence case
capitalises only the first word and proper nouns.
