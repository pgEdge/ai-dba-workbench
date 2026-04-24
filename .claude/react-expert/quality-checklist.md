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

This document provides quality standards, anti-patterns, and review
checklists for React and TypeScript code.

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

When configuring chart axis labels, prefer `fontSize: 14` over
`fontSize: 10` or `fontSize: 12`.

Note: many existing components use 12px and smaller sizes. These
are known technical debt. Do not introduce new violations, but
existing instances do not need to be fixed in unrelated changes.

## Common React Anti-Patterns

### Hook Issues

- Missing `useEffect` dependencies cause stale closures.
- Object literals in dependency arrays trigger infinite loops;
  use `useMemo` for stable references.
- Calling hooks conditionally or inside loops violates the
  Rules of Hooks.

### State Management

- Mutating state directly (e.g., `items.push()`) instead of
  creating new references with the spread operator.
- Running expensive computations on every render instead of
  wrapping in `useMemo`.
- Storing derived values in state when they can be computed
  from existing state or props.

### Performance

- Inline object or function literals in JSX props cause child
  re-renders; use `useMemo` and `useCallback`.
- Using array index as `key` in lists that reorder or filter.
- Missing `React.memo` on pure child components that receive
  stable props.
- Large components that should be split for independent
  re-rendering.

### Error Handling

- Unhandled promise rejections in async event handlers.
- Catching errors as `unknown` without type narrowing via
  `instanceof Error`.
- Missing error boundaries around component subtrees.

## TypeScript Standards

### Naming Conventions

- Components: `PascalCase` (e.g., `UserProfile`).
- Hooks: `camelCase` with `use` prefix (e.g., `useAuth`).
- Functions: `camelCase` (e.g., `fetchData`).
- Constants: `SCREAMING_SNAKE_CASE` (e.g., `MAX_RETRIES`).
- Types and interfaces: `PascalCase` (e.g., `UserData`).

### Type Safety

- Avoid `any`; use `unknown` with type guards when needed.
- Define explicit return types for exported functions.
- Use discriminated unions over optional properties for
  mutually exclusive states.

## Code Structure Standards

### Function Length

- Under 20 lines: ideal.
- 20 to 50 lines: acceptable.
- 50 to 100 lines: consider splitting.
- Over 100 lines: must refactor.

### Nesting Depth

- Maximum four levels; prefer early returns and extracted
  helper functions.

### Code Duplication

- Three or more identical blocks must be extracted.
- Repeated business logic must be shared.
- Test setup duplication is acceptable for clarity.

## UI Component Review Checklist

### Functionality

- [ ] Component works as expected.
- [ ] State management is correct.
- [ ] Props interface is clear and typed.
- [ ] Events are handled properly.

### Accessibility

- [ ] ARIA labels are present on interactive elements.
- [ ] Keyboard navigation works.
- [ ] Focus management is correct.
- [ ] Screen reader compatible.

### Styling

- [ ] Uses MUI theme system (no raw CSS values).
- [ ] Responsive design across breakpoints.
- [ ] Consistent with project design system.
- [ ] No inline styles unless necessary.

### Performance

- [ ] Unnecessary re-renders prevented.
- [ ] Heavy computations memoized.
- [ ] Lazy loading used where appropriate.
- [ ] Bundle size impact is acceptable.

### Testing

- [ ] Component tests added.
- [ ] User interactions tested.
- [ ] Edge cases covered (empty, error, loading).
- [ ] Accessibility tested.

## New Feature Review Checklist

### Code Quality

- [ ] Follows naming conventions.
- [ ] Functions are under 50 lines.
- [ ] No deep nesting (under 4 levels).
- [ ] No unused code or dead code paths.
- [ ] Exported functions are documented.

### Error Handling

- [ ] Errors are caught and displayed to the user.
- [ ] Error types are narrowed properly.
- [ ] No sensitive data in error messages.

### Security

- [ ] User inputs are validated and sanitized.
- [ ] No XSS vulnerabilities (no `dangerouslySetInnerHTML`).
- [ ] Credentials and tokens handled properly.

### Documentation

- [ ] Public APIs and complex logic documented.
- [ ] Changelog updated for user-facing changes.

## Coverage Requirements

New and modified client code must reach at least 90% line coverage;
this is a non-negotiable floor, not an aspirational target.

The 90% floor applies to modified code as well as new code; if you
touch a module whose coverage sits below 90%, raise the touched
units to 90% as part of the same change.

Measure coverage with `cd client && make coverage`, which runs
`npm run test:coverage` (Vitest with `@vitest/coverage-v8`); review
the text reporter output to confirm the changed files report at
least 90% line coverage.

If `vitest.config.js` does not yet configure coverage gates, add
them as a follow-up task with an owner and due date.

## Codacy Auto-Fix Passes

Codacy runs Biome and type-aware ESLint rules that are not
configured in the local `client/eslint.config.js`. When closing
a batch of auto-fixable Codacy findings at scale, follow this
approach:

- Run Biome via `npx @biomejs/biome check --write --unsafe src`
  with a temporary `client/biome.json` that sets
  `formatter.enabled = false`, `assist.enabled = false`, and
  `linter.rules.recommended = false`; only enable the specific
  rules being closed. This avoids re-formatting the whole tree
  (Prettier owns formatting here).
- Run type-aware ESLint via a temporary
  `client/eslint.autofix.config.js` that extends
  `tseslint.configs.recommendedTypeChecked`, disables every
  rule except the targeted ones, and enables `projectService`.
- Delete both temporary configs after the pass. Neither file
  belongs in commits.

### Rules to avoid in auto-fix passes

- `@typescript-eslint/non-nullable-type-assertion-style` rewrites
  `(x as T)` to `x!`, which trips the project's
  `@typescript-eslint/no-non-null-assertion` rule. Leave these
  sites alone; they need per-case judgement.
- `@typescript-eslint/prefer-nullish-coalescing` emits
  **suggestions**, not fixes, in typescript-eslint v8, so
  `eslint --fix` does nothing with it. Do not attempt to
  bulk-apply the suggestions — each `||` → `??` swap needs a
  review of the left-hand type (swapping on `string | ""` or
  `number | 0` changes semantics).
- `@typescript-eslint/no-confusing-void-expression` has a mix of
  safe rewrites (wrapping `() => foo()` in a block) and risky
  ones (adding `void` prefix); the latter are suggestion-only,
  and the fix count is small enough to handle manually.
- Biome's `useImportType` is safe but can remove adjacent
  `eslint-disable-next-line` comments when the import it tags
  lives next to disabled code. Re-check
  `@typescript-eslint/no-non-null-assertion` warning counts
  after the pass and restore any comments that vanished.
- `@typescript-eslint/no-confusing-void-expression --fix` also
  strips adjacent `eslint-disable-next-line` comments when it
  rewrites lines that sit directly above them (for example
  `// eslint-disable-next-line @typescript-eslint/no-explicit-any`
  on the line before a `MockInstance<any[], any>` declaration
  inside a `describe` block). The block-level
  `/* eslint-disable @typescript-eslint/no-explicit-any */`
  pragma at the top of test files is also at risk. After every
  pass, diff for `eslint-disable` removals and restore them by
  hand; a spike in `no-explicit-any` or `no-non-null-assertion`
  warnings in test files is the tell-tale sign.
- Remaining manual sites after `--fix` almost always follow the
  pattern `() => cond && sideEffectReturningVoid()`. The short-
  circuit form returns `false | void`, which is why the auto-
  fixer refuses to wrap it in a block (the block body would
  still return `void` and the original `false` branch is
  discarded silently, but the rule's heuristic is conservative
  here). Prefer the `void` operator form:
  `() => void (cond && sideEffect())`. It keeps the original
  one-line shape and, crucially, does **not** add a statement or
  `if` branch to the enclosing component body — important when
  the enclosing function is already near the Codacy/Lizard
  CCN-8 or nloc-medium threshold. Only fall back to
  `() => { if (cond) { sideEffect(); } }` when the extra branch
  demonstrably does not push any surrounding function over
  threshold. Never wrap an expression that already returns a
  non-void value in `void (...)` — that would silently discard a
  real return, which is a semantic change rather than a
  stylistic one.
- If the rule fires on an inner call like
  `prev.find(a => a.id === id)` where the flagged position is
  the outer `find` call (not the callback), the root cause is
  usually `useState([])` without an explicit type argument — TS
  infers `never[]`, `.find()` on `never[]` resolves to `never`,
  and the rule treats `never` the same as `void`. The minimal
  semantic-preserving fix is to widen the array through a local
  cast (for example
  `const list = prev as unknown as { id: unknown }[]`), then
  call `.find()` on the widened reference. Avoid
  `eslint-disable-next-line` for this rule: the local ESLint
  config does not enable `no-confusing-void-expression`, so the
  disable comment itself becomes an "unused directive" warning.
- After a `no-confusing-void-expression` auto-fix pass, run
  `npm run format:check` and compare the file count against the
  baseline. ESLint rewrites single-line arrow handlers like
  `onChange={(e) => onChange(e.target.value)}` into a one-line
  block that exceeds Prettier's print width; Prettier will then
  flag the file as newly unformatted. Apply
  `npx prettier --write <file>` only to the affected file(s)
  after the ESLint pass so Prettier's diff stays scoped to the
  lines the rule already touched.

### Verify before and after

Always measure the project ESLint warning count with
`npm run lint` before and after the pass. The post-pass warning
count must match the baseline; an increase means an auto-fix
introduced a pattern flagged by another rule, and that specific
rule should be disabled and the change reverted.
