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

- Business logic and hooks: 90%.
- API handlers and utilities: 80%.
- UI components: 70%.
