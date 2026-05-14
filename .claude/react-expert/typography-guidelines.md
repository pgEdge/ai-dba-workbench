/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Typography Guidelines

This document defines the typography standards for the pgEdge AI DBA
Workbench frontend. All UI code must conform to these rules. The
react-expert agent references this file when implementing or reviewing
components.

## Font Family Rules

The application uses two font families with strict usage rules.

- **Primary font**: Inter with system fallbacks. Use for all UI text.
  The full stack is: `"Inter", "SF Pro Display", -apple-system,
  BlinkMacSystemFont, "Segoe UI", Roboto, Oxygen, Ubuntu, sans-serif`.

- **Monospace font**: JetBrains Mono with fallbacks. Use only for code,
  technical values (connection strings, SQL, query output, OIDs, etc.).
  The full stack is: `"JetBrains Mono", "Fira Code", "Source Code Pro",
  Consolas, "Courier New", monospace`.

- Never mix fonts arbitrarily. Any use of monospace must be for
  displaying code or technical content.

## Font Size Rules

The base font size is 16px (1rem). All rem values are relative to this.

### Allowed Sizes

Use MUI Typography variants exclusively. The canonical sizes are:

| Purpose              | Size             | MUI Variant | Notes                         |
|----------------------|------------------|-------------|-------------------------------|
| Page title           | 2.75rem (44px)   | h1          | Rare; main page heading only  |
| Section heading      | 2.25rem (36px)   | h2          | Major sections                |
| Subsection heading   | 1.875rem (30px)  | h3          |                               |
| Card/panel heading   | 1.5rem (24px)    | h4          |                               |
| Minor heading        | 1.375rem (22px)  | h5          |                               |
| Small heading        | 1.25rem (20px)   | h6          | Dialog titles, tab labels     |
| Subtitle             | 1.125rem (18px)  | subtitle1   | Emphasised body-adjacent text |
| Default body text    | 1rem (16px)      | body1       | Standard for all readable text|
| Small subtitle       | 1rem (16px)      | subtitle2   |                               |
| Button text          | 1rem (16px)      | button      |                               |
| Secondary body text  | 0.875rem (14px)  | body2       | Smaller, secondary content    |
| Caption/label        | 0.875rem (14px)  | caption     | Smallest allowed size         |
| Overline label       | 0.875rem (14px)  | overline    | Uppercase with letter-spacing |

### Strict Rules

1. **No text smaller than 14px (0.875rem) in new code.** This is the
   target minimum. Many existing components use 12px and smaller;
   these are known technical debt and do not need to be fixed in
   unrelated changes.

2. **Default readable text is 16px (1rem) via `body1`.** Do not use
   smaller sizes for primary content. The previous 18px default was
   reduced as part of the 2026 typography cleanup.

3. **Secondary body text is 14px (0.875rem) via `body2`.** Use the
   `body2` variant for less prominent prose, helper text, and
   captions. Do not hand-wire `fontSize: '0.875rem'` on Typography
   when `variant="body2"` produces the same result.

4. **Prefer MUI Typography variants from the theme.** Avoid arbitrary
   `fontSize` values in `sx` props or inline styles where a theme
   variant would suffice. If a new size is needed, consider adding it
   to the theme first.

5. **Custom `fontSize` in sx props is discouraged** unless the value
   matches a theme variant or addresses a specific layout constraint
   (e.g., chart labels, dense data tables). Many existing components
   still carry custom `fontSize` values from the pre-cleanup era; new
   code must prefer theme variants and tokens.

6. **Use shared tokens for repeating patterns.** Chart axis labels use
   `CHART_AXIS_LABEL_FONTSIZE` from `client/src/theme/tokens.ts`;
   monospace caption rows use `MONO_CAPTION_SX`. Icon-size literals
   should use `ICON_14_SX`, `ICON_16_SX`, or `ICON_10_SX` from the
   same module. Add new tokens only when the same pattern repeats
   three or more times.

## Header Capitalization Rules

Apply these consistently throughout the application.

| Level    | Capitalization | Example                        | Usage               |
|----------|---------------|--------------------------------|---------------------|
| h1       | Title Case    | "Server Performance Overview"  | Page titles         |
| h2       | Title Case    | "Active Connections"           | Major sections      |
| h3       | Title Case    | "Query Statistics"             | Subsections         |
| h4       | Sentence case | "Connection details"           | Card/panel headings |
| h5       | Sentence case | "Recent activity"              | Minor headings      |
| h6       | Sentence case | "Token scope"                  | Dialog titles, tabs |
| overline | UPPERCASE     | "ADMIN PERMISSIONS"            | Category labels     |
| caption  | Sentence case | "Last updated 5 min ago"       | Footnotes, times    |

**Title Case** means capitalise the first letter of every significant
word. Do not capitalise articles, prepositions under 4 letters, or
conjunctions: a, an, the, and, but, or, for, in, of, on, to, etc.

**Sentence case** means capitalise only the first word and proper nouns.

## Letter Spacing for Labels

When using uppercase labels (overline variant), use consistent
letter-spacing.

- The theme overline variant uses `letterSpacing: '0.08em'`.
  New uppercase labels should prefer this value.
- In practice, many existing components use `0.05em` for
  uppercase labels. Both `0.05em` and `0.08em` are acceptable;
  prefer `0.08em` for new code and maintain consistency within
  each component.
- Avoid other values such as `0.1em` or `0.15em`.

## Prohibited Patterns

These patterns violate the typography standards and must not appear in
code.

- `fontSize: '0.75rem'` (12px) — below minimum.
- `fontSize: '0.625rem'` (10px) — below minimum.
- `fontSize: '0.6875rem'` (11px) — below minimum.
- `fontSize: '0.8125rem'` (13px) — not a standard size; use caption
  (14px).
- `fontSize: '0.9375rem'` (15px) — not a standard size; use
  subtitle2/button (16px).
- Arbitrary font sizes in new code that do not reference theme
  typography variants or address a documented layout constraint.
- Unusual letter-spacing values for uppercase labels such as
  0.1em or 0.15em. Use 0.08em or 0.05em.

## Theme Integration

The theme typography configuration in `pgedgeTheme.ts` must match these
guidelines. The canonical configuration is:

```typescript
const typography = {
    fontFamily: '"Inter", "SF Pro Display", -apple-system, '
        + 'BlinkMacSystemFont, "Segoe UI", Roboto, Oxygen, Ubuntu, '
        + 'sans-serif',
    fontSize: 16,
    htmlFontSize: 16,
    h1: {
        fontWeight: 700,
        fontSize: '2.75rem',
        lineHeight: 1.2,
        letterSpacing: '-0.02em',
    },
    h2: {
        fontWeight: 700,
        fontSize: '2.25rem',
        lineHeight: 1.3,
        letterSpacing: '-0.01em',
    },
    h3: {
        fontWeight: 600,
        fontSize: '1.875rem',
        lineHeight: 1.4,
    },
    h4: {
        fontWeight: 600,
        fontSize: '1.5rem',
        lineHeight: 1.4,
    },
    h5: {
        fontWeight: 600,
        fontSize: '1.375rem',
        lineHeight: 1.5,
    },
    h6: {
        fontWeight: 600,
        fontSize: '1.25rem',
        lineHeight: 1.5,
    },
    subtitle1: {
        fontWeight: 500,
        fontSize: '1.125rem',
        lineHeight: 1.5,
    },
    subtitle2: {
        fontWeight: 500,
        fontSize: '1rem',
        lineHeight: 1.5,
    },
    body1: {
        fontSize: '1rem',
        lineHeight: 1.6,
    },
    body2: {
        fontSize: '0.875rem',
        lineHeight: 1.6,
    },
    button: {
        fontWeight: 500,
        fontSize: '1rem',
        letterSpacing: '0.02em',
    },
    caption: {
        fontSize: '0.875rem',
        lineHeight: 1.5,
    },
    overline: {
        fontSize: '0.875rem',
        fontWeight: 600,
        letterSpacing: '0.08em',
        textTransform: 'uppercase',
    },
};
```

### Key Changes (2026 Cleanup)

The 2026 typography cleanup restored MUI's standard 16/14 sizes for
body text after user feedback that the previous 18/18 scale rendered
the interface uniformly oversized:

- body1 reduced from 18px to 16px (back to the MUI default).
- body2 reduced from 18px to 14px (separated from body1 again).
- subtitle1 stays at 18px and now exceeds body1, giving a real
  visual hierarchy between subtitle and body.
- subtitle2, button, h1-h6, caption, and overline are unchanged.
- The minimum-allowed size remains 14px (0.875rem) for new code.

When opening a file that still carries `fontSize: '1.125rem'`,
`fontSize: '1rem'`, or `fontSize: '0.875rem'` overrides on a
`<Typography>`, prefer the matching variant (`subtitle1`, `body1`,
`body2`) and delete the override. Keep explicit numeric
`fontSize` only on chart axis labels, Chips, MUI inputs, and
non-Typography elements where variant inheritance does not apply.
