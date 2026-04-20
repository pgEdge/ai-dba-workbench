/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Partitioning Conventions
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Metrics Partitioning Conventions

The collector stores probe output in weekly range-partitioned tables
under the `metrics` schema. Partition naming and range boundaries must
satisfy one invariant: both are computed in UTC from the same instant.

## The Invariant

For any `time.Time t`, the week partition is defined as:

- `utc := t.UTC()`
- `from := Monday 00:00:00 UTC of the week containing utc`
- `to := from + 7 days`
- `name suffix := from.Format("20060102")`

Never compute the week start by chaining weekday math in local time and
then `Truncate(24*time.Hour)`. `Truncate` operates in UTC regardless of
the value's location, so mixing the two produces a boundary that is
Monday-midnight in no consistent zone. The collector hit this as
issue #55 on a non-UTC host: two consecutive weekly partitions were
named six days apart and Postgres rejected the second with 42P17
"would overlap".

## Emitting Bound Literals

Partition bound literals in DDL must carry an explicit UTC offset so
the datastore session's `TimeZone` setting cannot reinterpret them.
Use the layout `"2006-01-02 15:04:05Z07:00"` with a UTC value; Go
emits `Z` for UTC which Postgres accepts as a timestamptz literal.

The shared helper lives at
`collector/src/probes/base.go`:

- `weeklyPartitionBounds(t time.Time) (nameSuffix string, from, to time.Time)`
- `const partitionBoundLayout = "2006-01-02 15:04:05Z07:00"`

Unit tests in `collector/src/probes/base_test.go` cover the known
off-by-one timezone cases (Sunday-in-UTC that looks like Monday in a
positive-offset zone, Monday-in-UTC that looks like Sunday in a
negative-offset zone).

## Expiry Comparisons

`DropExpiredPartitions` compares two `time.Time` instants
(`endTimestamp.Before(cutoff)`). `Before` is timezone-agnostic, so
that comparison is safe. The partition-bound parser accepts three
layouts; with the new UTC-offset literal, all newly created
partitions hit the `"2006-01-02 15:04:05-07:00"` or
`"2006-01-02 15:04:05-07"` branch.

## Anti-Patterns

- Do not use `timestamp.Truncate(24*time.Hour)` to find a day
  boundary unless you have already converted the value to UTC and
  that UTC day is exactly what you want.
- Do not mix `t.Weekday()` (local) with UTC-based truncation; pick
  one timezone and stay in it.
- Do not emit timestamp literals without an offset when storing into
  `timestamptz` columns or `timestamptz` range partitions; the
  session `TimeZone` will silently change the absolute instant.
