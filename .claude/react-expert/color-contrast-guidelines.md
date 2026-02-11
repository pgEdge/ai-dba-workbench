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

# Color Contrast Guidelines

These guidelines supplement the existing typography guidelines.
They ensure all UI elements meet WCAG AA contrast requirements
for typical vision.

## Current Palette Reference

The project theme defines these base values.

**Light mode:**

- `background.default`: #F9FAFB
- `background.paper`: #FFFFFF
- `text.primary`: #1F2937
- `text.secondary`: #6B7280
- `divider`: #E5E7EB

**Dark mode:**

- `background.default`: #0F172A
- `background.paper`: #1E293B
- `text.primary`: #F1F5F9
- `text.secondary`: #CBD5E1
- `divider`: #334155

## Minimum Contrast Ratios (WCAG AA)

- Normal text (under 18px): 4.5:1 against its background.
- Large text (18px+ bold or 24px+): 3:1 against its background.
- UI components and graphical objects: 3:1 against adjacent
  colors.

## Light Mode Panel and Card Backgrounds

Panels placed on white (#FFFFFF) must be clearly distinguishable
from the page surface.

- Grey fills require a minimum alpha of 0.12. Values between
  0.05 and 0.08 produce invisible panels.
- Colored fills (info, warning, success, error) require a
  minimum alpha of 0.10.
- Border colors must achieve at least 3:1 contrast against the
  panel background.

## Dark Mode Text

- `text.secondary` must achieve a minimum 4.5:1 contrast ratio
  against `background.paper` (#1E293B).
- `text.disabled` must achieve a minimum 3:1 contrast ratio
  against `background.paper`.
- Avoid `grey.500` (#64748B) or darker values for text on dark
  backgrounds; these fail contrast requirements.

## Alpha Value Minimums for Backgrounds

| Context                         | Minimum Alpha |
|---------------------------------|---------------|
| Grey panel fills in light mode  | 0.12          |
| Colored status fills in light   | 0.10          |
| Grey panel fills in dark mode   | 0.30          |
| Any visible background element  | 0.05          |

## Borders

**Light mode:**

- Use `grey.300` (#D1D5DB) or darker for visible borders.
- Do not use `grey.200` (#E5E7EB) as a border color; the
  contrast against white is insufficient.

**Dark mode:**

- Use `grey.600` (#475569) as the minimum for visible borders.

**Dashed borders:**

- Dashed borders need slightly more contrast than solid borders
  because less ink covers the edge.

## Prohibited Patterns

The following patterns produce invisible or unreadable results.

- `alpha(grey, 0.05)` or `alpha(grey, 0.06)` for visible
  panels. These are invisible on white backgrounds.
- `alpha(color, 0.04)` for hover states. Use 0.08 as the
  minimum hover alpha.
- `text.disabled` for content that users need to read. Reserve
  `text.disabled` exclusively for truly disabled elements.
