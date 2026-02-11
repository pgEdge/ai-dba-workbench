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
| Default body text    | 1.125rem (18px)  | body1       | Standard for all readable text|
| Secondary body text  | 1.125rem (18px)  | body2       | Same size, different weight   |
| Subtitle             | 1.125rem (18px)  | subtitle1   |                               |
| Small subtitle       | 1rem (16px)      | subtitle2   |                               |
| Button text          | 1rem (16px)      | button      |                               |
| Caption/label        | 0.875rem (14px)  | caption     | Smallest allowed size         |
| Overline label       | 0.875rem (14px)  | overline    | Uppercase with letter-spacing |

### Strict Rules

1. **No text smaller than 14px (0.875rem).** This is an absolute minimum.
   Any existing text below 14px must be increased.

2. **Default readable text must be 18px (1.125rem).** Do not use smaller
   sizes for primary content.

3. **14px (0.875rem) is reserved for captions, labels, and decorative
   text only.** Never use 14px for content the user needs to read as
   primary information.

4. **16px (1rem) may be used for subtitle2 and buttons.** This suits
   secondary information that is less prominent than body text but more
   prominent than captions.

5. **Always use MUI Typography variants from the theme.** Do not use
   arbitrary `fontSize` values in `sx` props or inline styles. If a new
   size is needed, add it to the theme first.

6. **Custom `fontSize` in sx props is prohibited** unless the value
   exactly matches a theme variant. This prevents the proliferation of
   arbitrary sizes (10px, 11px, 13px, 15px, etc.).

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

- `letterSpacing: '0.08em'` is the single standard for all uppercase
  labels.
- Do not use varying letter-spacing values (0.05em, 0.1em, etc.).

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
- Arbitrary font sizes in component styles that do not reference theme
  typography variants.
- Different letter-spacing values for uppercase labels; always use
  0.08em.

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
        fontSize: '1.125rem',
        lineHeight: 1.6,
    },
    body2: {
        fontSize: '1.125rem',
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

### Key Changes (2px Scale Bump)

These changes bump most sizes up by 2px for improved readability:

- Base minimum increased from 12px to 14px.
- body1/body2 increased from 16px to 18px.
- subtitle1 increased from 16px to 18px.
- subtitle2 increased from 14px to 16px.
- caption/overline increased from 12px to 14px.
- h5 increased from 20px to 22px.
- h6 increased from 18px to 20px.
- h1-h4 and button unchanged.
