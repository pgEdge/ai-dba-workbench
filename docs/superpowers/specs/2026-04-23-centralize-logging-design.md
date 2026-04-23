# Centralize Console Logging Behind a Utility

Addresses GitHub issue #165. The React client has 38
`console.error` calls and 4 `console.warn` calls scattered
across ~30 production files. This design replaces all direct
console calls with a centralized logging utility.

## Logger Module

A new file at `client/src/utils/logger.ts` exports a `logger`
object with four methods: `error`, `warn`, `info`, and `debug`.
Each method delegates to the corresponding `console` method.

```typescript
export const logger = {
  error(...args: unknown[]): void { console.error(...args); },
  warn(...args: unknown[]): void { console.warn(...args); },
  info(...args: unknown[]): void { console.info(...args); },
  debug(...args: unknown[]): void { console.debug(...args); },
};
```

The signatures accept variadic `unknown[]` arguments, matching
the `console` API. Existing call sites change only the import
and object prefix; no argument changes are required.

This module is the sole authorized location for direct `console`
calls in production code.

## Call Site Migration

Every `console.error(...)` becomes `logger.error(...)`. Every
`console.warn(...)` becomes `logger.warn(...)`. Each affected
file gains one import:

```typescript
import { logger } from '../utils/logger';
```

The migration covers 38 `console.error` calls across ~30 files
and 4 `console.warn` calls across 2 files. No behavioral
changes result from this migration.

## ESLint Enforcement

The existing ESLint rule in `client/eslint.config.js`:

```javascript
'no-console': ['warn', { allow: ['warn', 'error'] }],
```

Changes to:

```javascript
'no-console': ['error'],
```

This promotes the rule from `warn` to `error` and removes all
exemptions. A targeted override for `logger.ts` re-allows
`console` usage in that single file. This prevents future direct
`console` calls from entering the codebase.

## Test Strategy

The logger module gets a dedicated test file at
`client/src/utils/__tests__/logger.test.ts`. The tests verify
that each method delegates to the correct `console` method with
the correct arguments.

Existing test files that mock `console.error` directly (such as
`AuthContext.test.tsx` and other context tests) will be updated
to mock the `logger` module instead. This approach is cleaner
and decouples tests from the `console` implementation detail.

The logger module achieves 100% line coverage from its unit
tests. All touched files maintain the 90% coverage floor.

## Files Changed

| Category | Files | Change |
|----------|-------|--------|
| New | `client/src/utils/logger.ts` | Logger module |
| New | `client/src/utils/__tests__/logger.test.ts` | Logger tests |
| Modified | ~30 production files | Replace console calls |
| Modified | `client/eslint.config.js` | Tighten no-console rule |
| Modified | ~10 test files | Mock logger instead of console |

## Acceptance Criteria

- No direct `console.error` or `console.warn` calls remain in
  production code.
- The centralized logger is used consistently across all files.
- ESLint enforces the rule as an error, not a warning.
- All existing tests pass with no regressions.
- New and modified code meets the 90% coverage floor.
