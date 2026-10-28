/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package probes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
	"github.com/pgedge/ai-workbench/pkg/logger"
	"github.com/pgedge/ai-workbench/pkg/rollback"
	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// coldFrontBatchRows caps the rows written by one statement under
// ColdFront. Each statement becomes one Iceberg commit, so a probe run
// should normally be a single statement: this is above what a probe
// produces per run on a typical server (pg_stat_statements keeps 5,000
// statements by default, pg_settings about 350 rows), while bounding the
// JSON document and the literal INSERT the helper builds from it to a
// few megabytes on the largest servers.
const coldFrontBatchRows = 5000

// coldFrontInsertStatement writes one batch through the datastore's
// public.workbench_coldfront_insert helper; see
// database.EnsureColdFrontInsertHelper. ColdFront rewrites a cold-tier
// INSERT with bound parameters into a function call, which caps it at 99
// parameters, so the rows travel as one jsonb parameter and the helper
// turns them into a literal multi-row INSERT on the server.
var coldFrontInsertStatement = sqlmarker.Tag(
	"SELECT public.workbench_coldfront_insert($1, $2::jsonb)")

// storeColdFrontMetrics is StoreMetrics under ColdFront.
func storeColdFrontMetrics(ctx context.Context, conn *pgxpool.Conn, tableName string, columns []string, values [][]any) error {
	// convertForColdFront refuses a table without a specification, so
	// the lookup after it always succeeds.
	values, err := convertForColdFront(tableName, columns, values)
	if err != nil {
		return err
	}
	spec, _ := coldfront.Lookup(tableName)

	txn, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback.Tx(ctx, txn) //nolint:errcheck // no-op after commit

	for i := 0; i < len(values); i += coldFrontBatchRows {
		end := min(i+coldFrontBatchRows, len(values))
		rows, err := coldFrontRowsJSON(spec, columns, values[i:end])
		if err != nil {
			return err
		}
		if _, err := txn.Exec(ctx, coldFrontInsertStatement, tableName, rows); err != nil {
			return fmt.Errorf("failed to execute INSERT: %w", err)
		}
	}

	if err := txn.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// coldFrontRowsJSON encodes a batch as the JSON array the insert helper
// takes: one object per row, keyed by column name. Every column must be
// in the table's specification, since the helper would otherwise drop
// an unknown key without a word. Values are made JSON-faithful by
// jsonValue; NaN and infinite floats, which JSON cannot carry, are
// stored as NULL and reported in one log line per batch.
func coldFrontRowsJSON(spec coldfront.Table, columns []string, batch [][]any) ([]byte, error) {
	types := make(map[string]string, len(spec.Columns))
	for _, c := range spec.Columns {
		types[c.Name] = c.PGType
	}
	for _, name := range columns {
		if _, ok := types[name]; !ok {
			return nil, fmt.Errorf("metrics.%s has no column %s in its ColdFront specification", spec.Name, name)
		}
	}

	nonFinite := map[string]int{}
	rows := make([]map[string]any, len(batch))
	for i, row := range batch {
		obj := make(map[string]any, len(columns))
		for j, name := range columns {
			if j >= len(row) {
				break
			}
			v, err := jsonValue(types[name], row[j])
			if errors.Is(err, errNonFinite) {
				nonFinite[name]++
				v = nil
			} else if err != nil {
				return nil, fmt.Errorf("metrics.%s.%s: %w", spec.Name, name, err)
			}
			obj[name] = v
		}
		rows[i] = obj
	}
	for name, n := range nonFinite {
		logger.Infof("Storing %d NaN or infinite value(s) of metrics.%s.%s as NULL: ColdFront rows travel as JSON, which cannot represent them",
			n, spec.Name, name)
	}

	b, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("metrics.%s: failed to encode rows: %w", spec.Name, err)
	}
	return b, nil
}

// errInvalidJSON reports a json or jsonb value that is not a JSON document.
var errInvalidJSON = errors.New("value is not valid JSON")

// errNonFinite marks a NaN or infinite float, which JSON cannot carry.
var errNonFinite = errors.New("non-finite float")

// jsonValue returns v in a form encoding/json renders faithfully for a
// column of the given PostgreSQL type, which jsonb_populate_recordset
// then reads back as the same value:
//
//   - time.Time marshals as RFC 3339 with nanoseconds, and integers and
//     pgtype.Numeric as exact JSON numbers, which jsonb keeps exactly.
//   - pgtype.Interval has no JSON form, so it becomes PostgreSQL interval
//     input text in months, days and microseconds.
//   - A string or []byte for a json or jsonb column is the document
//     itself, as pgx would send it, so it is embedded rather than quoted
//     as a JSON string; invalid JSON is an error, as it is in PostgreSQL.
//   - A NaN or infinite float returns errNonFinite.
func jsonValue(pgType string, v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, errNonFinite
		}
	case float32:
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return nil, errNonFinite
		}
	case time.Time:
		return x.Format(time.RFC3339Nano), nil
	case pgtype.Interval:
		if !x.Valid {
			return nil, nil
		}
		return fmt.Sprintf("%d months %d days %d microseconds", x.Months, x.Days, x.Microseconds), nil
	case string:
		if pgType == "jsonb" || pgType == "json" {
			if !json.Valid([]byte(x)) {
				return nil, errInvalidJSON
			}
			return json.RawMessage(x), nil
		}
	case []byte:
		if pgType == "jsonb" || pgType == "json" {
			if !json.Valid(x) {
				return nil, errInvalidJSON
			}
			return json.RawMessage(x), nil
		}
	}
	return v, nil
}
