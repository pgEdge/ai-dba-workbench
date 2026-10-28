/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package coldfront holds the single source of truth for storing the
// collector's metrics.* tables in pgEdge ColdFront instead of in
// partitioned PostgreSQL tables: the column specification of every
// metrics table, the mapping from each column's PostgreSQL type to a type
// ColdFront accepts, and the value conversions the collector applies
// before inserting into a column whose type was mapped.
//
// The migrations (collector/src/database) build the
// coldfront.create_iceberg_table calls from this package, and the metrics
// writer (collector/src/probes) converts values from it, so the two
// cannot disagree about a column's type.
package coldfront

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MinimumBuildProcedure is the regprocedure whose presence identifies a
// ColdFront build new enough for the Workbench. coldfront._partition_clause
// arrived in pgEdge/coldfront#92, which also contains #85, the read-dialect
// support the server relies on. The extension reports extversion 1.0 in
// every build, so the version string cannot be used for this check.
const MinimumBuildProcedure = "coldfront._partition_clause(jsonb,text[])"

// CreateTableProcedure creates an Iceberg table and its wrapper view.
// Its presence is how the collector tells that the coldfront extension
// is installed.
const CreateTableProcedure = "coldfront.create_iceberg_table(text,text,jsonb,text[])"

// AttachProcedure attaches the Lakekeeper catalog as the DuckDB catalog
// "ice" from the coldfront.warehouse and coldfront.lakekeeper_endpoint
// settings. The collector calls it before creating the Iceberg namespace
// for the metrics tables, so a build without it is refused as well.
const AttachProcedure = "coldfront.ensure_attached()"

// IcebergNamespace is the Iceberg namespace the metrics tables are
// created in. create_iceberg_table names its Iceberg table
// ice.<p_schema>.<p_table>, and p_schema is always metrics.
const IcebergNamespace = "metrics"

// MinimumBuildDescription names the minimum ColdFront build in operator-
// facing error messages.
const MinimumBuildDescription = "a pgEdge ColdFront build that includes " +
	"pgEdge/coldfront#92 (the coldfront._partition_clause function)"

// PartitionColumns is the Iceberg partition specification every metrics
// table is created with: one partition per UTC day of collected_at, which
// every metrics table carries as a timestamptz column.
var PartitionColumns = []string{"day(collected_at)"}

// mappedTypes lists the PostgreSQL types the metrics tables use that
// ColdFront rejects, with the type each is declared as instead. The keys
// are the names format_type() reports.
var mappedTypes = map[string]string{
	// oid and xid are unsigned 32-bit integers; bigint holds every value.
	"oid": "bigint",
	"xid": "bigint",
	// pg_duckdb cannot plan any query over inet, so addresses are text.
	"inet": "text",
	// The single-byte "char" and the 63-byte name are both short text.
	`"char"`: "text",
	"name":   "text",
	// Iceberg decimals need a bounded precision. The unbounded numeric
	// columns hold byte counts, which fit easily in 38 digits.
	"numeric": "numeric(38,0)",
}

// nativeTypes lists the format_type() names ColdFront stores as they are.
var nativeTypes = map[string]bool{
	"bigint":                      true,
	"integer":                     true,
	"smallint":                    true,
	"real":                        true,
	"double precision":            true,
	"boolean":                     true,
	"timestamp with time zone":    true,
	"timestamp without time zone": true,
	"date":                        true,
	"time without time zone":      true,
	"uuid":                        true,
	"text":                        true,
	"bytea":                       true,
	"jsonb":                       true,
	"json":                        true,
	"interval":                    true,
}

// nonArrayElementTypes are native types ColdFront accepts as scalars but
// not as array elements.
var nonArrayElementTypes = map[string]bool{
	"timestamp with time zone": true,
	"jsonb":                    true,
	"json":                     true,
	"interval":                 true,
}

var (
	boundedStringType  = regexp.MustCompile(`^character( varying)?\(\d+\)$`)
	boundedNumericType = regexp.MustCompile(`^numeric\((\d+),\s*\d+\)$`)
)

// maxNumericPrecision is the largest decimal precision Iceberg stores.
const maxNumericPrecision = 38

// StorageType returns the type a column declared as pgType (a
// format_type() name) is created with through
// coldfront.create_iceberg_table. A type ColdFront stores natively is
// returned unchanged, a type it rejects but the Workbench knows how to
// widen is mapped, and anything else is an error, so an unsupported type
// fails the migration rather than reaching ColdFront.
func StorageType(pgType string) (string, error) {
	t := strings.TrimSpace(pgType)
	if mapped, ok := mappedTypes[t]; ok {
		return mapped, nil
	}
	if isNativeScalar(t) {
		return t, nil
	}
	if elem, ok := strings.CutSuffix(t, "[]"); ok {
		if isNativeScalar(elem) && !nonArrayElementTypes[elem] {
			return t, nil
		}
		return "", fmt.Errorf("ColdFront cannot store an array of %s", elem)
	}
	return "", fmt.Errorf("ColdFront has no mapping for PostgreSQL type %s", t)
}

// isNativeScalar reports whether ColdFront stores the scalar type t as
// it is.
func isNativeScalar(t string) bool {
	if nativeTypes[t] || boundedStringType.MatchString(t) {
		return true
	}
	if m := boundedNumericType.FindStringSubmatch(t); m != nil {
		precision, err := strconv.Atoi(m[1])
		return err == nil && precision <= maxNumericPrecision
	}
	return false
}
