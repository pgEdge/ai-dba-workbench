# Remaining Code Review Items

These are lower-priority refactoring tasks that don't affect security or
functionality. They can be addressed incrementally over time.

## Code Duplication

### Issue 8: Chat Client Duplication

Extract shared chat client code between different chat implementations in
the server package to reduce duplication.

### Issue 9: Duplicate File Reading Functions

Consolidate file reading utilities that are repeated across different
packages.

### Issue 10: Repeated Worker Pattern

Create a generic worker pool abstraction to replace repeated goroutine +
channel patterns used for background processing.

## Component/Function Size

### Issue 11: Large React Components

Split oversized React components (e.g., ClusterNavigator at ~1800 lines)
into smaller, focused sub-components.

### Issue 12: ClusterContext God Object

Split ClusterContext into separate focused contexts:

- ClusterDataContext
- ClusterSelectionContext
- ClusterActionsContext

### Issue 13: Long API Handler Functions

Extract query parsing and validation helpers from large API handler
functions to improve readability.

## Frontend Improvements

### Issue 19: TypeScript Migration

Migrate React client from JavaScript to TypeScript for improved type
safety.

### Issue 22: Hardcoded Colors

Move hardcoded color values (e.g., `#22C55E`, `#EF4444`) to MUI theme
configuration.

### Issue 23: Inline Style Objects

Extract inline style objects created in render methods to constants or
styled components to prevent unnecessary re-renders.
