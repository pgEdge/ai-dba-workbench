/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - SQL Read/Write Classification
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Classifying User SQL as a Read or a Write

Several server paths run SQL that a user or an LLM supplies, and each
must decide whether a statement only reads. A first-keyword check is
not enough: `SELECT pg_terminate_backend(...)`, `SELECT set_config(...)`
and `SELECT nextval(...)` start with `SELECT` but change server state,
and most of them work inside a read-only transaction (issue #646).

## The Shared Package

`server/src/internal/sqllex` holds the one lexer and side-effect check;
do not copy either into another package.

- `lexer.go`: string, quoted-identifier, dollar-quote and comment
  skipping (`SkipNonCode`, `MaskNonCode`, `SkipQuoted`), identifier
  helpers, and `StringReading` for the two ways a server may read
  `'...\'...'` (`standard_conforming_strings` on or off).
- `side_effects.go`: `CallsSideEffectingFunction(sql, reading)` and
  `CallsSideEffectingFunctionUnderAnyReading(sql)`. Matching ignores
  case, `pg_catalog.` or other schema qualification, double-quoted and
  `U&"..."` names, and whitespace or comments before `(`; names inside
  literals and comments do not match. The function lists come from the
  PostgreSQL, Spock and pg_cron documentation; a database test checks
  the "core" family against `pg_proc`.

Use `CallsSideEffectingFunctionUnderAnyReading` unless the caller
already knows the session's string reading; it fails closed.

## Callers

- `api.isReadOnlyStatement` (`query_handlers.go`): a side-effecting
  call makes the statement a write, so it needs write access and the
  confirmation prompt.
- `tools.isSelectQuery` (`sql_helpers.go`): returns false for such a
  statement, so `query_database` sends it down its write path
  (explicit `connection_id` and `read_write` access required) and
  injects no `LIMIT`.
- `execute_explain`: refuses `analyze` for such a query.
- `test_query`: refuses when the statement, or any statement in it,
  does not start with a keyword EXPLAIN only plans (`explainOnlyPlans`),
  because the tool prefixes `EXPLAIN ` and a leading `ANALYZE` would
  run it.
- `count_rows` (`validateWhereClause`) and `query_datastore`: refuse.

Any new path that classifies user SQL by its first keyword must also
call the side-effect check.

## Limits

A user-defined function, view, operator or cast that calls one of
these functions internally is not detected; only the database knows
what it does.
