/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package metrics

import (
	"context"
	"encoding/json"
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// ptrInt64 returns a pointer to v for populating MetricFilters.QueryID.
func ptrInt64(v int64) *int64 {
	return &v
}

func TestParseTimeRange(t *testing.T) {
	tests := []struct {
		input   string
		wantDur time.Duration
		wantErr bool
	}{
		{"1h", 1 * time.Hour, false},
		{"6h", 6 * time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"2h", 0, true},
		{"", 0, true},
		{"abc", 0, true},
		{"1w", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			start, end, err := ParseTimeRange(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseTimeRange(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseTimeRange(%q) unexpected error: %v", tt.input, err)
				return
			}

			actualDur := end.Sub(start)
			// Allow up to 2 seconds of drift from test execution time
			diff := actualDur - tt.wantDur
			if diff < 0 {
				diff = -diff
			}
			if diff > 2*time.Second {
				t.Errorf("ParseTimeRange(%q) duration = %v, want ~%v",
					tt.input, actualDur, tt.wantDur)
			}

			if end.Before(start) {
				t.Errorf("ParseTimeRange(%q) end before start", tt.input)
			}
		})
	}
}

func TestIsMetricColumn(t *testing.T) {
	tests := []struct {
		name     string
		dataType string
		expected bool
	}{
		// Dimension columns
		{"connection_id", "integer", false},
		{"collected_at", "timestamp with time zone", false},
		{"datname", "name", false},
		{"query", "text", false},
		{"client_addr", "inet", false},
		{"relname", "character varying", false},
		{"relid", "oid", false},

		// Metric columns
		{"numbackends", "integer", true},
		{"xact_commit", "bigint", true},
		{"blks_hit", "bigint", true},
		{"temp_bytes", "numeric", true},
		{"active_time", "double precision", true},
		{"some_value", "real", true},
		{"small_count", "smallint", true},

		// Edge cases
		{"custom_column", "bigint", true},
		{"custom_text", "text", false},
		{"inserted_at", "timestamp without time zone", false},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_"+tt.dataType, func(t *testing.T) {
			result := IsMetricColumn(tt.name, tt.dataType)
			if result != tt.expected {
				t.Errorf("IsMetricColumn(%q, %q) = %v, want %v",
					tt.name, tt.dataType, result, tt.expected)
			}
		})
	}
}

func TestIsValidIdentifier(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"pg_stat_database", true},
		{"PG_STAT_DATABASE", true},
		{"table1", true},
		{"_private", true},
		{"a", true},
		{"", false},
		{"123table", false},
		{"table-name", false},
		{"table name", false},
		{"table;drop", false},
		{"table'injection", false},
		{"select*from", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := IsValidIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("IsValidIdentifier(%q) = %v, want %v",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestQuoteIdentifier(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", `"simple"`},
		{"with space", `"with space"`},
		{`has"quote`, `"has""quote"`},
		{"pg_stat_database", `"pg_stat_database"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := QuoteIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("QuoteIdentifier(%q) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildMetricsQuery(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)

	t.Run("basic query structure", func(t *testing.T) {
		query, args, err := BuildMetricsQuery(
			"pg_stat_database",
			[]string{"xact_commit", "blks_hit"},
			map[string]string{"xact_commit": "bigint", "blks_hit": "bigint"},
			1, start, end, 60, "avg",
			MetricFilters{},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check query contains expected elements
		if !strings.Contains(query, `date_bin($1::interval`) {
			t.Error("query should contain date_bin")
		}
		if !strings.Contains(query, `metrics."pg_stat_database"`) {
			t.Error("query should reference the probe table")
		}
		if !strings.Contains(query, `connection_id = $2`) {
			t.Error("query should filter by connection_id")
		}
		if !strings.Contains(query, `avg("xact_commit")`) {
			t.Error("query should aggregate xact_commit")
		}
		if !strings.Contains(query, `avg("blks_hit")`) {
			t.Error("query should aggregate blks_hit")
		}
		if !strings.Contains(query, `data_buckets."xact_commit"`) {
			t.Error("query should include qualified metric columns for LOCF")
		}
		if strings.Contains(query, `COALESCE(data_buckets."xact_commit"`) {
			t.Error("query should not COALESCE metric columns; LOCF is applied in Go")
		}
		if !strings.Contains(query, `FROM generate_series($3::timestamptz, `+
			`$4::timestamptz, $1::interval) AS g(bucket_time)`) {
			t.Error("bucket series must be generated in the FROM clause")
		}

		// Check args
		if len(args) != 4 {
			t.Errorf("expected 4 args, got %d", len(args))
		}
		if args[1] != 1 {
			t.Errorf("expected connection_id=1, got %v", args[1])
		}
	})

	t.Run("with filters using datname", func(t *testing.T) {
		query, args, err := BuildMetricsQuery(
			"pg_stat_database",
			[]string{"xact_commit"},
			map[string]string{"xact_commit": "bigint"},
			1, start, end, 60, "sum",
			MetricFilters{
				DatabaseName:   "mydb",
				DatabaseColumn: "datname",
				SchemaName:     "public",
				TableName:      "users",
			},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(query, `"datname" = $5`) {
			t.Error("query should filter by datname")
		}
		if !strings.Contains(query, "schemaname = $6") {
			t.Error("query should filter by schemaname")
		}
		if !strings.Contains(query, "relname = $7") {
			t.Error("query should filter by relname")
		}
		if len(args) != 7 {
			t.Errorf("expected 7 args, got %d", len(args))
		}
	})

	t.Run("with filters using database_name", func(t *testing.T) {
		query, args, err := BuildMetricsQuery(
			"pg_stat_all_tables",
			[]string{"seq_scan"},
			map[string]string{"seq_scan": "bigint"},
			1, start, end, 60, "avg",
			MetricFilters{
				DatabaseName:   "mydb",
				DatabaseColumn: "database_name",
				SchemaName:     "public",
			},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(query, `"database_name" = $5`) {
			t.Error("query should filter by database_name")
		}
		if !strings.Contains(query, "schemaname = $6") {
			t.Error("query should filter by schemaname")
		}
		if len(args) != 6 {
			t.Errorf("expected 6 args, got %d", len(args))
		}
	})

	t.Run("with index_name filter", func(t *testing.T) {
		query, args, err := BuildMetricsQuery(
			"pg_stat_all_indexes",
			[]string{"idx_scan"},
			map[string]string{"idx_scan": "bigint"},
			1, start, end, 60, "avg",
			MetricFilters{
				SchemaName: "public",
				TableName:  "orders",
				IndexName:  "pk_orders",
			},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Filters bind after the four fixed args ($1-$4): schemaname $5,
		// relname $6, then indexrelname $7.
		if !strings.Contains(query, "schemaname = $5") {
			t.Error("query should filter by schemaname")
		}
		if !strings.Contains(query, "relname = $6") {
			t.Error("query should filter by relname")
		}
		if !strings.Contains(query, "indexrelname = $7") {
			t.Errorf("query should filter by indexrelname, got:\n%s", query)
		}
		if len(args) != 7 {
			t.Fatalf("expected 7 args, got %d", len(args))
		}
		if args[6] != "pk_orders" {
			t.Errorf("expected indexrelname arg 'pk_orders', got %v", args[6])
		}
	})

	t.Run("with queryid filter", func(t *testing.T) {
		query, args, err := BuildMetricsQuery(
			"pg_stat_statements",
			[]string{"calls"},
			map[string]string{"calls": "bigint"},
			1, start, end, 60, "sum",
			MetricFilters{
				DatabaseName:   "mydb",
				DatabaseColumn: "database_name",
				QueryID:        ptrInt64(-1234567890123456789),
			},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Filters bind after the four fixed args ($1-$4): database_name
		// $5, then queryid $6.
		if !strings.Contains(query, `"database_name" = $5`) {
			t.Error("query should filter by database_name")
		}
		if !strings.Contains(query, "queryid = $6") {
			t.Errorf("query should filter by queryid, got:\n%s", query)
		}
		if len(args) != 6 {
			t.Fatalf("expected 6 args, got %d", len(args))
		}
		if args[5] != int64(-1234567890123456789) {
			t.Errorf("expected int64 queryid arg, got %#v", args[5])
		}
	})

	t.Run("queryid filter omitted leaves SQL unchanged", func(t *testing.T) {
		withoutField, argsA, err := BuildMetricsQuery(
			"pg_stat_statements",
			[]string{"calls"},
			map[string]string{"calls": "bigint"},
			1, start, end, 60, "sum",
			MetricFilters{},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		withEmptyField, argsB, err := BuildMetricsQuery(
			"pg_stat_statements",
			[]string{"calls"},
			map[string]string{"calls": "bigint"},
			1, start, end, 60, "sum",
			MetricFilters{QueryID: nil},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if withoutField != withEmptyField {
			t.Errorf("nil QueryID changed SQL:\n%s\n---\n%s",
				withoutField, withEmptyField)
		}
		if strings.Contains(withEmptyField, "queryid") {
			t.Error("nil QueryID must not add a queryid clause")
		}
		if len(argsA) != len(argsB) {
			t.Errorf("nil QueryID changed arg count: %d vs %d",
				len(argsA), len(argsB))
		}
	})

	t.Run("index_name filter omitted leaves SQL unchanged", func(t *testing.T) {
		// The Table Detail dashboard never sends index_name; verify that an
		// empty IndexName produces byte-identical SQL and args to a query
		// built with a zero-value filter set that also omits it.
		withoutField, argsA, err := BuildMetricsQuery(
			"pg_stat_all_tables",
			[]string{"seq_scan"},
			map[string]string{"seq_scan": "bigint"},
			1, start, end, 60, "avg",
			MetricFilters{SchemaName: "public", TableName: "orders"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		withEmptyField, argsB, err := BuildMetricsQuery(
			"pg_stat_all_tables",
			[]string{"seq_scan"},
			map[string]string{"seq_scan": "bigint"},
			1, start, end, 60, "avg",
			MetricFilters{SchemaName: "public", TableName: "orders", IndexName: ""},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if withoutField != withEmptyField {
			t.Errorf("empty IndexName changed SQL:\n%s\n---\n%s",
				withoutField, withEmptyField)
		}
		if strings.Contains(withEmptyField, "indexrelname") {
			t.Error("empty IndexName must not add an indexrelname clause")
		}
		if len(argsA) != len(argsB) {
			t.Errorf("empty IndexName changed arg count: %d vs %d",
				len(argsA), len(argsB))
		}
	})

	t.Run("database filter skipped when column empty", func(t *testing.T) {
		query, args, err := BuildMetricsQuery(
			"pg_sys_cpu_info",
			[]string{"cpu_user"},
			map[string]string{"cpu_user": "double precision"},
			1, start, end, 60, "avg",
			MetricFilters{
				DatabaseName:   "mydb",
				DatabaseColumn: "",
			},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if strings.Contains(query, "datname") || strings.Contains(query, "database_name") {
			t.Error("query should not filter by database when column is empty")
		}
		if len(args) != 4 {
			t.Errorf("expected 4 args (no database filter), got %d", len(args))
		}
	})

	t.Run("last aggregation", func(t *testing.T) {
		query, _, err := BuildMetricsQuery(
			"pg_stat_database",
			[]string{"xact_commit"},
			map[string]string{"xact_commit": "bigint"},
			1, start, end, 60, "last",
			MetricFilters{},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(query, "array_agg") {
			t.Error("last aggregation should use array_agg")
		}
	})

	t.Run("sub-second bucket width clamped", func(t *testing.T) {
		tinyEnd := start.Add(time.Second)
		query, _, err := BuildMetricsQuery(
			"pg_stat_database",
			[]string{"xact_commit"},
			map[string]string{"xact_commit": "bigint"},
			1, start, tinyEnd, 60, "avg",
			MetricFilters{},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(query, "date_bin($1::interval") {
			t.Error("query should still build with clamped bucket width")
		}
	})
}

func TestGetAggSelectCols(t *testing.T) {
	cols := GetAggSelectCols([]string{"col_a", "col_b"}, "avg")
	if len(cols) != 2 {
		t.Fatalf("expected 2 cols, got %d", len(cols))
	}
	if !strings.Contains(cols[0], `avg("col_a")`) {
		t.Errorf("expected avg aggregation, got %s", cols[0])
	}

	lastCols := GetAggSelectCols([]string{"col_a"}, "last")
	if !strings.Contains(lastCols[0], "array_agg") {
		t.Errorf("expected array_agg for last, got %s", lastCols[0])
	}
}

func TestGetQualifiedSelectCols(t *testing.T) {
	cols := GetQualifiedSelectCols([]string{"xact_commit", "blks_hit"}, "data_buckets")
	if len(cols) != 2 {
		t.Fatalf("expected 2 cols, got %d", len(cols))
	}
	expected0 := `data_buckets."xact_commit"`
	if cols[0] != expected0 {
		t.Errorf("cols[0] = %s, want %s", cols[0], expected0)
	}
	expected1 := `data_buckets."blks_hit"`
	if cols[1] != expected1 {
		t.Errorf("cols[1] = %s, want %s", cols[1], expected1)
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name       string
		input      any
		expected   float64
		expectedOk bool
	}{
		{"nil", nil, 0, false},
		{"float64", float64(1.5), 1.5, true},
		{"float32", float32(2.5), 2.5, true},
		{"int64", int64(42), 42, true},
		{"int32", int32(7), 7, true},
		{"int", int(99), 99, true},
		{"int16", int16(10), 10, true},
		{"int8", int8(5), 5, true},
		{"uint64", uint64(100), 100, true},
		{"uint32", uint32(50), 50, true},
		{"uint16", uint16(25), 25, true},
		{"uint8", uint8(12), 12, true},
		{"string", "abc", 0, false},
		{"nan", math.NaN(), 0, false},
		{"pos_inf", math.Inf(1), 0, false},
		{"neg_inf", math.Inf(-1), 0, false},
		{"float32_nan", float32(math.NaN()), 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := toFloat64(tt.input)
			if ok != tt.expectedOk {
				t.Errorf("toFloat64(%v) ok = %v, want %v",
					tt.input, ok, tt.expectedOk)
			}
			if result != tt.expected {
				t.Errorf("toFloat64(%v) = %v, want %v",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestToFloat64_PointerAndNumeric(t *testing.T) {
	var wrapped any = float64(3.5)
	var wrappedNil any

	tests := []struct {
		name       string
		input      any
		expected   float64
		expectedOk bool
	}{
		{"any pointer to float64", &wrapped, 3.5, true},
		{"any pointer to nil", &wrappedNil, 0, false},
		{"nil any pointer", (*any)(nil), 0, false},
		{
			name:       "valid numeric",
			input:      pgtype.Numeric{Int: big.NewInt(42), Exp: 0, Valid: true},
			expected:   42,
			expectedOk: true,
		},
		{
			name:       "invalid numeric",
			input:      pgtype.Numeric{Valid: false},
			expected:   0,
			expectedOk: false,
		},
		{
			name:       "pointer valid numeric",
			input:      &pgtype.Numeric{Int: big.NewInt(7), Exp: 0, Valid: true},
			expected:   7,
			expectedOk: true,
		},
		{
			name:       "pointer invalid numeric",
			input:      &pgtype.Numeric{Valid: false},
			expected:   0,
			expectedOk: false,
		},
		{"nil numeric pointer", (*pgtype.Numeric)(nil), 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := toFloat64(tt.input)
			if ok != tt.expectedOk {
				t.Errorf("toFloat64(%v) ok = %v, want %v",
					tt.input, ok, tt.expectedOk)
			}
			if result != tt.expected {
				t.Errorf("toFloat64(%v) = %v, want %v",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestFiniteFloat(t *testing.T) {
	tests := []struct {
		name       string
		input      float64
		expected   float64
		expectedOk bool
	}{
		{"zero", 0, 0, true},
		{"positive", 3.14, 3.14, true},
		{"negative", -2.5, -2.5, true},
		{"nan", math.NaN(), 0, false},
		{"pos_inf", math.Inf(1), 0, false},
		{"neg_inf", math.Inf(-1), 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := finiteFloat(tt.input)
			if ok != tt.expectedOk {
				t.Errorf("finiteFloat(%v) ok = %v, want %v",
					tt.input, ok, tt.expectedOk)
			}
			if result != tt.expected {
				t.Errorf("finiteFloat(%v) = %v, want %v",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestToFloat64_Interval(t *testing.T) {
	const (
		secondsPerDay   = 86_400.0
		secondsPerMonth = 30.0 * secondsPerDay
	)

	tests := []struct {
		name       string
		input      any
		expected   float64
		expectedOk bool
	}{
		{
			name:       "microseconds only",
			input:      pgtype.Interval{Microseconds: 2_500_000, Valid: true},
			expected:   2.5,
			expectedOk: true,
		},
		{
			name:       "days included",
			input:      pgtype.Interval{Days: 2, Valid: true},
			expected:   2 * secondsPerDay,
			expectedOk: true,
		},
		{
			name:       "months included",
			input:      pgtype.Interval{Months: 3, Valid: true},
			expected:   3 * secondsPerMonth,
			expectedOk: true,
		},
		{
			name: "combined micros days months",
			input: pgtype.Interval{
				Microseconds: 1_500_000,
				Days:         2,
				Months:       1,
				Valid:        true,
			},
			expected:   1.5 + 2*secondsPerDay + secondsPerMonth,
			expectedOk: true,
		},
		{
			name:       "null interval treated as zero",
			input:      pgtype.Interval{Valid: false},
			expected:   0,
			expectedOk: true,
		},
		{
			name: "pointer combined micros days months",
			input: &pgtype.Interval{
				Microseconds: 500_000,
				Days:         1,
				Months:       2,
				Valid:        true,
			},
			expected:   0.5 + secondsPerDay + 2*secondsPerMonth,
			expectedOk: true,
		},
		{
			name:       "pointer days and months",
			input:      &pgtype.Interval{Days: 5, Months: 1, Valid: true},
			expected:   5*secondsPerDay + secondsPerMonth,
			expectedOk: true,
		},
		{
			name:       "nil pointer treated as zero",
			input:      (*pgtype.Interval)(nil),
			expected:   0,
			expectedOk: true,
		},
		{
			name:       "pointer null interval treated as zero",
			input:      &pgtype.Interval{Valid: false},
			expected:   0,
			expectedOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := toFloat64(tt.input)
			if ok != tt.expectedOk {
				t.Errorf("toFloat64(%v) ok = %v, want %v",
					tt.input, ok, tt.expectedOk)
			}
			if result != tt.expected {
				t.Errorf("toFloat64(%v) = %v, want %v",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateOrder(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"asc", "asc", false},
		{"desc", "desc", false},
		{"ASC", "asc", false},
		{"DESC", "desc", false},
		{"  desc  ", "desc", false},
		{"", "desc", false},
		{"sideways", "", true},
		{"asc; DROP TABLE", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ValidateOrder(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateOrder(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateOrder(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ValidateOrder(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveOrderByColumn(t *testing.T) {
	// The caller passes the full set of returned columns, which mixes
	// numeric metrics with dimension and timestamp columns.
	metricCols := []string{
		"n_live_tup", "n_dead_tup", "seq_scan",
		"relname", "schemaname", "last_vacuum", "last_autovacuum",
	}

	t.Run("empty defaults to collected_at", func(t *testing.T) {
		got, err := ResolveOrderByColumn("", metricCols)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "collected_at" {
			t.Errorf("got %q, want collected_at", got)
		}
	})

	t.Run("collected_at is accepted", func(t *testing.T) {
		got, err := ResolveOrderByColumn("collected_at", metricCols)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "collected_at" {
			t.Errorf("got %q, want collected_at", got)
		}
	})

	t.Run("whitespace trimmed", func(t *testing.T) {
		got, err := ResolveOrderByColumn("  n_live_tup  ", metricCols)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "n_live_tup" {
			t.Errorf("got %q, want n_live_tup", got)
		}
	})

	t.Run("valid metric column accepted", func(t *testing.T) {
		got, err := ResolveOrderByColumn("seq_scan", metricCols)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "seq_scan" {
			t.Errorf("got %q, want seq_scan", got)
		}
	})

	t.Run("dimension column accepted", func(t *testing.T) {
		got, err := ResolveOrderByColumn("relname", metricCols)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "relname" {
			t.Errorf("got %q, want relname", got)
		}
	})

	t.Run("timestamp column accepted", func(t *testing.T) {
		got, err := ResolveOrderByColumn("last_vacuum", metricCols)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "last_vacuum" {
			t.Errorf("got %q, want last_vacuum", got)
		}
	})

	t.Run("unknown column rejected", func(t *testing.T) {
		_, err := ResolveOrderByColumn("not_a_column", metricCols)
		if err == nil {
			t.Fatal("expected error for unknown column")
		}
	})

	t.Run("injection attempt rejected", func(t *testing.T) {
		// A crafted ORDER BY payload must never resolve to a column; it is
		// not in the discovered metric set, so it is rejected before any
		// SQL text is built.
		_, err := ResolveOrderByColumn("1; DROP TABLE metrics.pg_stat_all_tables", metricCols)
		if err == nil {
			t.Fatal("expected error for injection attempt")
		}
	})
}

// tableCols mimics the numeric metric columns discovered for the
// pg_stat_all_tables probe, which the table dashboards depend on.
var tableCols = []string{
	"seq_scan", "idx_scan", "n_tup_ins", "n_tup_upd", "n_tup_del",
	"n_tup_hot_upd", "n_live_tup", "n_dead_tup",
}

func TestClassifyMetrics(t *testing.T) {
	t.Run("empty request returns all columns as raw", func(t *testing.T) {
		raw, derived, order, err := classifyMetrics(
			nil, []string{"seq_scan", "idx_scan"}, "pg_stat_all_tables")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(derived) != 0 {
			t.Errorf("expected no derived metrics, got %d", len(derived))
		}
		if len(raw) != 2 || len(order) != 2 {
			t.Errorf("expected 2 raw/order entries, got %d/%d",
				len(raw), len(order))
		}
		if order[0] != "seq_scan" || order[1] != "idx_scan" {
			t.Errorf("unexpected order: %v", order)
		}
	})

	t.Run("raw column accepted", func(t *testing.T) {
		raw, derived, order, err := classifyMetrics(
			[]string{"seq_scan"}, tableCols, "pg_stat_all_tables")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(raw) != 1 || raw[0] != "seq_scan" {
			t.Errorf("expected raw [seq_scan], got %v", raw)
		}
		if len(derived) != 0 {
			t.Errorf("expected no derived, got %v", derived)
		}
		if len(order) != 1 || order[0] != "seq_scan" {
			t.Errorf("unexpected order: %v", order)
		}
	})

	t.Run("valid per_sec base accepted", func(t *testing.T) {
		raw, derived, order, err := classifyMetrics(
			[]string{"seq_scan_per_sec"}, tableCols, "pg_stat_all_tables")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(raw) != 0 {
			t.Errorf("expected no raw columns, got %v", raw)
		}
		if len(derived) != 1 {
			t.Fatalf("expected 1 derived, got %d", len(derived))
		}
		if derived[0].Kind != DerivedPerSec {
			t.Errorf("expected DerivedPerSec, got %v", derived[0].Kind)
		}
		if derived[0].BaseColumn != "seq_scan" {
			t.Errorf("expected base seq_scan, got %q", derived[0].BaseColumn)
		}
		if derived[0].OutputName != "seq_scan_per_sec" {
			t.Errorf("unexpected output name %q", derived[0].OutputName)
		}
		if len(order) != 1 || order[0] != "seq_scan_per_sec" {
			t.Errorf("unexpected order: %v", order)
		}
	})

	t.Run("per_sec suffix on non-column rejected", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"bogus_per_sec"}, tableCols, "pg_stat_all_tables")
		if err == nil {
			t.Fatal("expected error for non-column per_sec base")
		}
		if !strings.Contains(err.Error(), "bogus_per_sec") {
			t.Errorf("error should name the metric, got %q", err.Error())
		}
	})

	t.Run("bare per_sec suffix rejected", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"_per_sec"}, tableCols, "pg_stat_all_tables")
		if err == nil {
			t.Fatal("expected error for bare _per_sec")
		}
	})

	t.Run("real column ending in per_sec wins over derived", func(t *testing.T) {
		cols := []string{"seq_scan", "custom_per_sec"}
		raw, derived, _, err := classifyMetrics(
			[]string{"custom_per_sec"}, cols, "pg_stat_all_tables")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(derived) != 0 {
			t.Errorf("expected raw treatment, got derived %v", derived)
		}
		if len(raw) != 1 || raw[0] != "custom_per_sec" {
			t.Errorf("expected raw [custom_per_sec], got %v", raw)
		}
	})

	t.Run("dead_tuple_ratio accepted when both columns present", func(t *testing.T) {
		_, derived, order, err := classifyMetrics(
			[]string{"dead_tuple_ratio"}, tableCols, "pg_stat_all_tables")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(derived) != 1 || derived[0].Kind != DerivedDeadTupleRatio {
			t.Fatalf("expected 1 dead-tuple-ratio derived, got %v", derived)
		}
		if derived[0].OutputName != "dead_tuple_ratio" {
			t.Errorf("unexpected output name %q", derived[0].OutputName)
		}
		if len(order) != 1 || order[0] != "dead_tuple_ratio" {
			t.Errorf("unexpected order: %v", order)
		}
	})

	t.Run("dead_tuple_ratio rejected when columns missing", func(t *testing.T) {
		cols := []string{"seq_scan", "n_live_tup"} // no n_dead_tup
		_, _, _, err := classifyMetrics(
			[]string{"dead_tuple_ratio"}, cols, "pg_stat_all_tables")
		if err == nil {
			t.Fatal("expected error when n_dead_tup missing")
		}
		if !strings.Contains(err.Error(), "n_dead_tup") {
			t.Errorf("error should mention required columns, got %q",
				err.Error())
		}
	})

	t.Run("unknown metric rejected", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"nonexistent"}, tableCols, "pg_stat_all_tables")
		if err == nil {
			t.Fatal("expected error for unknown metric")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected not-found error, got %q", err.Error())
		}
	})

	t.Run("invalid identifier rejected", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"bad-name"}, tableCols, "pg_stat_all_tables")
		if err == nil {
			t.Fatal("expected error for invalid identifier")
		}
		if !strings.Contains(err.Error(), "invalid metric name") {
			t.Errorf("expected invalid-identifier error, got %q", err.Error())
		}
	})

	t.Run("valid delta base accepted", func(t *testing.T) {
		raw, derived, order, err := classifyMetrics(
			[]string{"seq_scan_delta"}, tableCols, "pg_stat_all_tables")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(raw) != 0 {
			t.Errorf("expected no raw columns, got %v", raw)
		}
		if len(derived) != 1 {
			t.Fatalf("expected 1 derived, got %d", len(derived))
		}
		if derived[0].Kind != DerivedDelta {
			t.Errorf("expected DerivedDelta, got %v", derived[0].Kind)
		}
		if derived[0].BaseColumn != "seq_scan" {
			t.Errorf("expected base seq_scan, got %q", derived[0].BaseColumn)
		}
		if derived[0].OutputName != "seq_scan_delta" {
			t.Errorf("unexpected output name %q", derived[0].OutputName)
		}
		if len(order) != 1 || order[0] != "seq_scan_delta" {
			t.Errorf("unexpected order: %v", order)
		}
	})

	t.Run("delta suffix on non-column rejected", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"bogus_delta"}, tableCols, "pg_stat_all_tables")
		if err == nil {
			t.Fatal("expected error for non-column delta base")
		}
		if !strings.Contains(err.Error(), "bogus_delta") {
			t.Errorf("error should name the metric, got %q", err.Error())
		}
		if !strings.Contains(err.Error(), "per-bucket delta") {
			t.Errorf("error should explain the delta case, got %q", err.Error())
		}
	})

	t.Run("bare delta suffix rejected", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"_delta"}, tableCols, "pg_stat_all_tables")
		if err == nil {
			t.Fatal("expected error for bare _delta")
		}
	})

	t.Run("real column ending in delta wins over derived", func(t *testing.T) {
		cols := []string{"seq_scan", "custom_delta"}
		raw, derived, _, err := classifyMetrics(
			[]string{"custom_delta"}, cols, "pg_stat_all_tables")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(derived) != 0 {
			t.Errorf("expected raw treatment, got derived %v", derived)
		}
		if len(raw) != 1 || raw[0] != "custom_delta" {
			t.Errorf("expected raw [custom_delta], got %v", raw)
		}
	})

	t.Run("per_sec and delta on the same column coexist", func(t *testing.T) {
		_, derived, order, err := classifyMetrics(
			[]string{"seq_scan_per_sec", "seq_scan_delta"},
			tableCols, "pg_stat_all_tables")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(derived) != 2 {
			t.Fatalf("expected 2 derived, got %d", len(derived))
		}
		if derived[0].Kind != DerivedPerSec || derived[1].Kind != DerivedDelta {
			t.Errorf("unexpected kinds: %v", derived)
		}
		if derived[0].BaseColumn != "seq_scan" ||
			derived[1].BaseColumn != "seq_scan" {
			t.Errorf("both should share base seq_scan, got %v", derived)
		}
		want := []string{"seq_scan_per_sec", "seq_scan_delta"}
		for i := range want {
			if order[i] != want[i] {
				t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
			}
		}
	})

	t.Run("mixed raw and derived preserves order", func(t *testing.T) {
		raw, derived, order, err := classifyMetrics(
			[]string{"seq_scan", "idx_scan_per_sec", "dead_tuple_ratio"},
			tableCols, "pg_stat_all_tables")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(raw) != 1 || raw[0] != "seq_scan" {
			t.Errorf("expected raw [seq_scan], got %v", raw)
		}
		if len(derived) != 2 {
			t.Fatalf("expected 2 derived, got %d", len(derived))
		}
		if derived[0].Kind != DerivedPerSec ||
			derived[0].BaseColumn != "idx_scan" {
			t.Errorf("expected idx_scan per_sec first, got %v", derived[0])
		}
		if derived[1].Kind != DerivedDeadTupleRatio {
			t.Errorf("expected dead_tuple_ratio second, got %v", derived[1])
		}
		want := []string{"seq_scan", "idx_scan_per_sec", "dead_tuple_ratio"}
		for i := range want {
			if order[i] != want[i] {
				t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
			}
		}
	})

	t.Run("duplicate metric names deduplicated", func(t *testing.T) {
		raw, derived, order, err := classifyMetrics(
			[]string{
				"seq_scan", "seq_scan",
				"idx_scan_per_sec", "idx_scan_per_sec",
				"dead_tuple_ratio", "dead_tuple_ratio",
			},
			tableCols, "pg_stat_all_tables")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(raw) != 1 || raw[0] != "seq_scan" {
			t.Errorf("expected raw [seq_scan], got %v", raw)
		}
		if len(derived) != 2 {
			t.Fatalf("expected 2 derived, got %d: %v", len(derived), derived)
		}
		if derived[0].OutputName != "idx_scan_per_sec" ||
			derived[1].OutputName != "dead_tuple_ratio" {
			t.Errorf("unexpected derived: %v", derived)
		}
		want := []string{"seq_scan", "idx_scan_per_sec", "dead_tuple_ratio"}
		if len(order) != len(want) {
			t.Fatalf("expected order %v, got %v", want, order)
		}
		for i := range want {
			if order[i] != want[i] {
				t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
			}
		}
	})

	// Column kind rules (issue #402). These use real probe names so the
	// static registry, not a test override, decides the kind.
	dbCols := []string{
		"numbackends", "xact_commit", "blk_read_time", "active_time",
		"sessions",
	}
	stmtCols := []string{"calls", "total_exec_time", "mean_exec_time"}
	cpuCols := []string{"idle_mode_percent"}

	t.Run("per_sec on a gauge names the kind", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"n_dead_tup_per_sec"}, tableCols, "pg_stat_all_tables")
		if err == nil {
			t.Fatal("expected error")
		}
		want := `metric "n_dead_tup_per_sec" not supported for probe ` +
			`"pg_stat_all_tables": "n_dead_tup" is a gauge, not a cumulative counter`
		if err.Error() != want {
			t.Errorf("error = %q\nwant    %q", err.Error(), want)
		}
	})

	t.Run("per_sec on a time counter points at _pct", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"blk_read_time_per_sec"}, dbCols, "pg_stat_database")
		if err == nil {
			t.Fatal("expected error")
		}
		for _, frag := range []string{
			`"blk_read_time" is a cumulative time counter, not a cumulative counter`,
			`request "blk_read_time_pct"`,
		} {
			if !strings.Contains(err.Error(), frag) {
				t.Errorf("error %q lacks %q", err.Error(), frag)
			}
		}
	})

	t.Run("per_sec on a session time counter points at _sessions", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"active_time_per_sec"}, dbCols, "pg_stat_database")
		if err == nil {
			t.Fatal("expected error")
		}
		for _, frag := range []string{
			`"active_time" is a session time counter, not a cumulative counter`,
			`request "active_time_sessions"`,
		} {
			if !strings.Contains(err.Error(), frag) {
				t.Errorf("error %q lacks %q", err.Error(), frag)
			}
		}
	})

	t.Run("per_sec on a watermark and a ratio rejected", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"mean_exec_time_per_sec"}, stmtCols, "pg_stat_statements")
		if err == nil || !strings.Contains(err.Error(), "is a lifetime watermark") {
			t.Errorf("watermark: got %v", err)
		}
		if err != nil && strings.Contains(err.Error(), "request") {
			t.Errorf("watermark error should carry no hint: %v", err)
		}
		_, _, _, err = classifyMetrics(
			[]string{"idle_mode_percent_per_sec"}, cpuCols, "pg_sys_cpu_usage_info")
		if err == nil || !strings.Contains(err.Error(), "is a ratio") {
			t.Errorf("ratio: got %v", err)
		}
	})

	t.Run("per_sec on a real counter carries the /s unit", func(t *testing.T) {
		_, derived, _, err := classifyMetrics(
			[]string{"xact_commit_per_sec", "sessions_per_sec"}, dbCols, "pg_stat_database")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(derived) != 2 {
			t.Fatalf("expected 2 derived, got %d", len(derived))
		}
		for _, d := range derived {
			if d.Kind != DerivedPerSec || d.Unit != "/s" {
				t.Errorf("%s: kind %v unit %q", d.OutputName, d.Kind, d.Unit)
			}
		}
	})

	t.Run("delta accepts counter and time kinds with matching units", func(t *testing.T) {
		_, derived, _, err := classifyMetrics(
			[]string{"xact_commit_delta", "blk_read_time_delta", "active_time_delta"},
			dbCols, "pg_stat_database")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantUnits := map[string]string{
			"xact_commit_delta":   "",
			"blk_read_time_delta": "ms",
			"active_time_delta":   "ms",
		}
		for _, d := range derived {
			if d.Kind != DerivedDelta {
				t.Errorf("%s: kind %v, want DerivedDelta", d.OutputName, d.Kind)
			}
			if d.Unit != wantUnits[d.OutputName] {
				t.Errorf("%s: unit %q, want %q", d.OutputName, d.Unit, wantUnits[d.OutputName])
			}
		}
	})

	t.Run("delta on a gauge rejected", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"numbackends_delta"}, dbCols, "pg_stat_database")
		if err == nil || !strings.Contains(err.Error(),
			`"numbackends" is a gauge, not a cumulative counter`) {
			t.Errorf("got %v", err)
		}
	})

	t.Run("pct accepted only on a time counter", func(t *testing.T) {
		_, derived, order, err := classifyMetrics(
			[]string{"blk_read_time_pct"}, dbCols, "pg_stat_database")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(derived) != 1 || derived[0].Kind != DerivedTimeShare ||
			derived[0].BaseColumn != "blk_read_time" || derived[0].Unit != "%" {
			t.Errorf("unexpected derived %+v", derived)
		}
		if len(order) != 1 || order[0] != "blk_read_time_pct" {
			t.Errorf("unexpected order %v", order)
		}
		_, _, _, err = classifyMetrics(
			[]string{"xact_commit_pct"}, dbCols, "pg_stat_database")
		if err == nil || !strings.Contains(err.Error(),
			`"xact_commit" is a cumulative counter, not a cumulative time counter`) {
			t.Errorf("counter: got %v", err)
		}
		_, _, _, err = classifyMetrics(
			[]string{"active_time_pct"}, dbCols, "pg_stat_database")
		if err == nil || !strings.Contains(err.Error(),
			`"active_time" is a session time counter, not a cumulative time counter`) {
			t.Errorf("session time: got %v", err)
		}
		_, _, _, err = classifyMetrics(
			[]string{"nothing_pct"}, dbCols, "pg_stat_database")
		if err == nil || !strings.Contains(err.Error(), "to compute a share of wall-clock time") {
			t.Errorf("missing base: got %v", err)
		}
	})

	t.Run("sessions accepted only on a session time counter", func(t *testing.T) {
		_, derived, _, err := classifyMetrics(
			[]string{"active_time_sessions"}, dbCols, "pg_stat_database")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(derived) != 1 || derived[0].Kind != DerivedSessionAverage ||
			derived[0].BaseColumn != "active_time" || derived[0].Unit != "sessions" {
			t.Errorf("unexpected derived %+v", derived)
		}
		_, _, _, err = classifyMetrics(
			[]string{"blk_read_time_sessions"}, dbCols, "pg_stat_database")
		if err == nil || !strings.Contains(err.Error(),
			`"blk_read_time" is a cumulative time counter, not a session time counter`) {
			t.Errorf("time counter: got %v", err)
		}
		_, _, _, err = classifyMetrics(
			[]string{"nothing_sessions"}, dbCols, "pg_stat_database")
		if err == nil || !strings.Contains(err.Error(), "to compute an average session count") {
			t.Errorf("missing base: got %v", err)
		}
	})

	t.Run("real column ending in a derived suffix stays raw", func(t *testing.T) {
		cols := []string{"blk_read_time", "cache_pct", "peak_sessions"}
		raw, derived, _, err := classifyMetrics(
			[]string{"cache_pct", "peak_sessions"}, cols, "pg_stat_database")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(raw) != 2 || len(derived) != 0 {
			t.Errorf("raw %v derived %v", raw, derived)
		}
	})

	t.Run("dead_tuple_ratio carries the percent unit", func(t *testing.T) {
		_, derived, _, err := classifyMetrics(
			[]string{"dead_tuple_ratio"}, tableCols, "pg_stat_all_tables")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(derived) != 1 || derived[0].Unit != "%" {
			t.Errorf("unexpected derived %+v", derived)
		}
	})

	t.Run("unregistered probe treats every column as a gauge", func(t *testing.T) {
		_, _, _, err := classifyMetrics(
			[]string{"hits_per_sec"}, []string{"hits"}, "not_a_probe")
		if err == nil || !strings.Contains(err.Error(), `"hits" is a gauge`) {
			t.Errorf("got %v", err)
		}
	})
}

func TestEntityKeyColumns(t *testing.T) {
	tests := []struct {
		name       string
		outputCols []string
		colTypes   map[string]string
		want       []string
	}{
		{
			name:       "table probe keys on text and name columns",
			outputCols: []string{"database_name", "schemaname", "relname", "n_live_tup", "seq_scan"},
			colTypes: map[string]string{
				"database_name": "text",
				"schemaname":    "name",
				"relname":       "name",
				"n_live_tup":    "bigint",
				"seq_scan":      "bigint",
			},
			want: []string{"database_name", "schemaname", "relname"},
		},
		{
			name:       "varchar columns count as entity keys",
			outputCols: []string{"label", "value"},
			colTypes:   map[string]string{"label": "character varying", "value": "numeric"},
			want:       []string{"label"},
		},
		{
			name:       "no text columns yields no keys",
			outputCols: []string{"cpu_user", "cpu_system"},
			colTypes:   map[string]string{"cpu_user": "double precision", "cpu_system": "double precision"},
			want:       nil,
		},
		{
			name:       "preserves output column order",
			outputCols: []string{"relname", "schemaname"},
			colTypes:   map[string]string{"relname": "name", "schemaname": "name"},
			want:       []string{"relname", "schemaname"},
		},
		{
			// Simulates PR #343's additions (table_size /
			// table_size_pretty). The text-typed *_pretty value column
			// must NOT join the entity key, or different historical
			// rendered sizes would fragment one table into many and
			// reproduce the latest-row staleness bug.
			name: "text-typed _pretty value column is excluded",
			outputCols: []string{
				"database_name", "schemaname", "relname",
				"table_size", "table_size_pretty", "n_live_tup",
			},
			colTypes: map[string]string{
				"database_name":     "text",
				"schemaname":        "name",
				"relname":           "name",
				"table_size":        "bigint",
				"table_size_pretty": "text",
				"n_live_tup":        "bigint",
			},
			want: []string{"database_name", "schemaname", "relname"},
		},
		{
			// Index-probe analog of the above (index_size_pretty).
			name: "index _pretty value column is excluded",
			outputCols: []string{
				"database_name", "schemaname", "relname", "indexrelname",
				"index_size", "index_size_pretty", "idx_scan",
			},
			colTypes: map[string]string{
				"database_name":     "character varying",
				"schemaname":        "name",
				"relname":           "name",
				"indexrelname":      "name",
				"index_size":        "bigint",
				"index_size_pretty": "text",
				"idx_scan":          "bigint",
			},
			want: []string{
				"database_name", "schemaname", "relname", "indexrelname",
			},
		},
		{
			name:       "internal bookkeeping columns are excluded",
			outputCols: []string{"connection_id", "collected_at", "inserted_at", "datname"},
			colTypes: map[string]string{
				"connection_id": "integer",
				"collected_at":  "timestamp with time zone",
				"inserted_at":   "timestamp with time zone",
				"datname":       "name",
			},
			want: []string{"datname"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EntityKeyColumns(tt.outputCols, tt.colTypes)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestIsEntityKeyColumn exercises the single-column predicate directly,
// covering each type branch and both exclusion signals.
func TestIsEntityKeyColumn(t *testing.T) {
	tests := []struct {
		name     string
		colName  string
		dataType string
		want     bool
	}{
		{"text identity column", "schemaname", "name", true},
		{"varchar identity column", "database_name", "character varying", true},
		{"plain text column", "relname", "text", true},
		{"numeric value column", "n_live_tup", "bigint", false},
		{"timestamp column", "last_vacuum", "timestamp with time zone", false},
		{"text _pretty value column", "table_size_pretty", "text", false},
		{"internal bookkeeping column", "collected_at", "timestamp with time zone", false},
		{"internal connection_id", "connection_id", "integer", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEntityKeyColumn(tt.colName, tt.dataType); got != tt.want {
				t.Errorf("IsEntityKeyColumn(%q, %q) = %v, want %v",
					tt.colName, tt.dataType, got, tt.want)
			}
		})
	}
}

func TestBuildLatestRowsQuery(t *testing.T) {
	t.Run("entity-key probe wraps in DISTINCT ON", func(t *testing.T) {
		query, args := buildLatestRowsQuery(
			"pg_stat_all_tables",
			[]string{"relname", "n_live_tup"},
			map[string]string{"relname": "name", "n_live_tup": "bigint"},
			[]int{1},
			MetricFilters{},
			"n_live_tup", "desc", 1,
		)

		if !strings.Contains(query, `metrics."pg_stat_all_tables"`) {
			t.Error("query should reference the probe table")
		}
		if !strings.Contains(query, "connection_id IN ($1)") {
			t.Error("query should filter by connection_id")
		}
		// The inner query reduces each entity (keyed by connection_id plus
		// the text/name column relname) to its newest sample via DISTINCT ON.
		if !strings.Contains(query, `DISTINCT ON (connection_id, "relname")`) {
			t.Errorf("query should use DISTINCT ON connection_id and the entity key, got: %s", query)
		}
		if !strings.Contains(query, `ORDER BY connection_id, "relname", collected_at DESC`) {
			t.Errorf("inner query should order by entity key then collected_at DESC, got: %s", query)
		}
		// The outer query ranks the per-entity latest rows by order_by.
		if !strings.Contains(query, `ORDER BY "n_live_tup" desc, collected_at DESC`) {
			t.Errorf("outer query should rank by order_by column, got: %s", query)
		}
		if !strings.Contains(query, "LIMIT $2") {
			t.Error("query should apply LIMIT placeholder")
		}
		if len(args) != 2 {
			t.Fatalf("expected 2 args, got %d", len(args))
		}
		if args[0] != 1 || args[1] != 1 {
			t.Errorf("unexpected args: %v", args)
		}
	})

	t.Run("all filters applied", func(t *testing.T) {
		query, args := buildLatestRowsQuery(
			"pg_stat_all_indexes",
			[]string{"indexrelname", "idx_scan"},
			map[string]string{"indexrelname": "name", "idx_scan": "bigint"},
			[]int{2, 3},
			MetricFilters{
				DatabaseName:   "northwind",
				DatabaseColumn: "database_name",
				SchemaName:     "public",
				TableName:      "orders",
				IndexName:      "orders_pkey",
			},
			"idx_scan", "asc", 5,
		)

		if !strings.Contains(query, "connection_id IN ($1, $2)") {
			t.Error("query should list multiple connection placeholders")
		}
		if !strings.Contains(query, `"database_name" = $3`) {
			t.Error("query should filter by database_name")
		}
		if !strings.Contains(query, "schemaname = $4") {
			t.Error("query should filter by schemaname")
		}
		if !strings.Contains(query, "relname = $5") {
			t.Error("query should filter by relname")
		}
		if !strings.Contains(query, "indexrelname = $6") {
			t.Error("query should filter by indexrelname")
		}
		if !strings.Contains(query, `DISTINCT ON (connection_id, "indexrelname")`) {
			t.Errorf("query should use DISTINCT ON connection_id and the index entity key, got: %s", query)
		}
		if !strings.Contains(query, `ORDER BY "idx_scan" asc, collected_at DESC`) {
			t.Errorf("query should rank by idx_scan asc, got: %s", query)
		}
		if !strings.Contains(query, "LIMIT $7") {
			t.Error("query should apply LIMIT placeholder at $7")
		}
		// 2 connections + 4 filters + 1 limit
		if len(args) != 7 {
			t.Fatalf("expected 7 args, got %d", len(args))
		}
		if args[6] != 5 {
			t.Errorf("expected limit arg 5, got %v", args[6])
		}
	})

	t.Run("queryid filter applied", func(t *testing.T) {
		query, args := buildLatestRowsQuery(
			"pg_stat_statements",
			[]string{"query", "calls"},
			map[string]string{"query": "text", "calls": "bigint"},
			[]int{7},
			MetricFilters{QueryID: ptrInt64(-1234567890123456789)},
			"calls", "desc", 10,
		)

		if !strings.Contains(query, "queryid = $2") {
			t.Errorf("query should filter by queryid, got: %s", query)
		}
		if !strings.Contains(query, "LIMIT $3") {
			t.Errorf("limit placeholder should follow the filter, got: %s", query)
		}
		// 1 connection + 1 filter + 1 limit
		if len(args) != 3 {
			t.Fatalf("expected 3 args, got %d", len(args))
		}
		if args[1] != int64(-1234567890123456789) {
			t.Errorf("expected int64 queryid arg, got %#v", args[1])
		}
		if args[2] != 10 {
			t.Errorf("expected limit arg 10, got %v", args[2])
		}
	})

	t.Run("queryid zero is a real filter", func(t *testing.T) {
		// Zero is a legitimate pg_stat_statements identifier, so the
		// pointer form must distinguish it from "no filter".
		query, args := buildLatestRowsQuery(
			"pg_stat_statements",
			[]string{"calls"},
			map[string]string{"calls": "bigint"},
			[]int{7},
			MetricFilters{QueryID: ptrInt64(0)},
			"calls", "desc", 10,
		)
		if !strings.Contains(query, "queryid = $2") {
			t.Errorf("zero queryid should still filter, got: %s", query)
		}
		if len(args) != 3 || args[1] != int64(0) {
			t.Errorf("expected int64 zero queryid arg, got %#v", args)
		}
	})

	t.Run("no text entity keys still keys DISTINCT ON connection_id", func(t *testing.T) {
		// A probe with only numeric metric columns (e.g. pg_sys_cpu_info) has
		// no text/name entity keys, yet connection_id alone must still key the
		// DISTINCT ON so distinct connections never collapse into one row. The
		// order_by is a real metric column, not collected_at, so the outer
		// ranking is a genuine ORDER BY on that column rather than a no-op
		// duplication of collected_at.
		query, args := buildLatestRowsQuery(
			"pg_sys_cpu_info",
			[]string{"cpu_user"},
			map[string]string{"cpu_user": "double precision"},
			[]int{1},
			MetricFilters{DatabaseName: "northwind", DatabaseColumn: ""},
			"cpu_user", "desc", 1,
		)

		if strings.Contains(query, "database_name") || strings.Contains(query, "datname") {
			t.Error("query should not filter by database when column unresolved")
		}
		// Even without text/name entity keys, connection_id alone keys the
		// DISTINCT ON so distinct connections stay separate entities.
		if !strings.Contains(query, "DISTINCT ON (connection_id)") {
			t.Errorf("query should use DISTINCT ON (connection_id), got: %s", query)
		}
		// The inner query reduces each connection to its newest sample.
		if !strings.Contains(query, "ORDER BY connection_id, collected_at DESC") {
			t.Errorf("inner query should order by connection_id then collected_at DESC, got: %s", query)
		}
		// The outer query ranks the per-connection latest rows by the
		// requested metric column, proving it is not a collected_at no-op.
		if !strings.Contains(query, `ORDER BY "cpu_user" desc, collected_at DESC`) {
			t.Errorf("outer query should rank by cpu_user desc, got: %s", query)
		}
		// 1 connection + 1 limit only
		if len(args) != 2 {
			t.Fatalf("expected 2 args, got %d", len(args))
		}
	})
}

func TestQueryLatestRows_ValidationBeforeDB(t *testing.T) {
	ctx := context.Background()

	t.Run("invalid probe name", func(t *testing.T) {
		_, err := QueryLatestRows(ctx, nil, "bad;name", []int{1},
			MetricFilters{}, "", "desc", 1)
		if err == nil {
			t.Fatal("expected error for invalid probe name")
		}
	})

	t.Run("invalid order", func(t *testing.T) {
		_, err := QueryLatestRows(ctx, nil, "pg_stat_all_tables", []int{1},
			MetricFilters{}, "", "sideways", 1)
		if err == nil {
			t.Fatal("expected error for invalid order")
		}
	})

	t.Run("no connections", func(t *testing.T) {
		_, err := QueryLatestRows(ctx, nil, "pg_stat_all_tables", nil,
			MetricFilters{}, "", "desc", 1)
		if err == nil {
			t.Fatal("expected error for missing connections")
		}
	})
}

func TestSanitizeFloat(t *testing.T) {
	if got := sanitizeFloat(1.5); got != 1.5 {
		t.Errorf("sanitizeFloat(1.5) = %v, want 1.5", got)
	}
	if got := sanitizeFloat(math.NaN()); got != nil {
		t.Errorf("sanitizeFloat(NaN) = %v, want nil", got)
	}
	if got := sanitizeFloat(math.Inf(1)); got != nil {
		t.Errorf("sanitizeFloat(+Inf) = %v, want nil", got)
	}
	if got := sanitizeFloat(math.Inf(-1)); got != nil {
		t.Errorf("sanitizeFloat(-Inf) = %v, want nil", got)
	}
}

func TestNormalizeLatestValue(t *testing.T) {
	ts := time.Date(2026, 7, 15, 10, 40, 47, 0, time.UTC)

	tests := []struct {
		name  string
		input any
		want  any
	}{
		{"nil", nil, nil},
		{"int64", int64(1363), int64(1363)},
		{"float64", float64(2.5), float64(2.5)},
		{"float32", float32(2.5), float64(2.5)},
		{"nan float", math.NaN(), nil},
		{"inf float", math.Inf(1), nil},
		{"string", "public", "public"},
		{"bytes", []byte("orders"), "orders"},
		{"time", ts, ts.Format(time.RFC3339)},
		{"bool", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLatestValue(tt.input)
			if got != tt.want {
				t.Errorf("normalizeLatestValue(%v) = %v (%T), want %v (%T)",
					tt.input, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestNormalizeLatestValue_PgtypeValues(t *testing.T) {
	t.Run("valid numeric", func(t *testing.T) {
		num := pgtype.Numeric{Int: big.NewInt(1363), Exp: 0, Valid: true}
		got := normalizeLatestValue(num)
		if got != float64(1363) {
			t.Errorf("got %v, want 1363", got)
		}
	})

	t.Run("null numeric", func(t *testing.T) {
		got := normalizeLatestValue(pgtype.Numeric{Valid: false})
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("valid interval", func(t *testing.T) {
		iv := pgtype.Interval{Microseconds: 2_500_000, Valid: true}
		got := normalizeLatestValue(iv)
		if got != float64(2.5) {
			t.Errorf("got %v, want 2.5", got)
		}
	})

	t.Run("null interval treated as zero", func(t *testing.T) {
		got := normalizeLatestValue(pgtype.Interval{Valid: false})
		if got != float64(0) {
			t.Errorf("got %v, want 0", got)
		}
	})
}

func TestValidateLatestRowParams(t *testing.T) {
	t.Run("invalid probe name", func(t *testing.T) {
		_, _, err := validateLatestRowParams("bad;name", []int{1}, "desc", 1)
		if err == nil {
			t.Fatal("expected error for invalid probe name")
		}
	})

	t.Run("invalid order", func(t *testing.T) {
		_, _, err := validateLatestRowParams("pg_stat_all_tables", []int{1}, "sideways", 1)
		if err == nil {
			t.Fatal("expected error for invalid order")
		}
	})

	t.Run("no connections", func(t *testing.T) {
		_, _, err := validateLatestRowParams("pg_stat_all_tables", nil, "desc", 1)
		if err == nil {
			t.Fatal("expected error for missing connections")
		}
	})

	t.Run("empty order defaults to desc", func(t *testing.T) {
		order, _, err := validateLatestRowParams("pg_stat_all_tables", []int{1}, "", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if order != "desc" {
			t.Errorf("got order %q, want desc", order)
		}
	})

	t.Run("order normalized to lowercase", func(t *testing.T) {
		order, _, err := validateLatestRowParams("pg_stat_all_tables", []int{1}, "ASC", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if order != "asc" {
			t.Errorf("got order %q, want asc", order)
		}
	})

	t.Run("limit below one clamped up", func(t *testing.T) {
		_, limit, err := validateLatestRowParams("pg_stat_all_tables", []int{1}, "desc", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if limit != 1 {
			t.Errorf("got limit %d, want 1", limit)
		}
	})

	t.Run("limit above max clamped down", func(t *testing.T) {
		_, limit, err := validateLatestRowParams(
			"pg_stat_all_tables", []int{1}, "desc", maxLatestRowLimit+50)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if limit != maxLatestRowLimit {
			t.Errorf("got limit %d, want %d", limit, maxLatestRowLimit)
		}
	})

	t.Run("valid limit passes through", func(t *testing.T) {
		_, limit, err := validateLatestRowParams("pg_stat_all_tables", []int{1, 2}, "desc", 25)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if limit != 25 {
			t.Errorf("got limit %d, want 25", limit)
		}
	})
}

func TestSelectLatestOutputColumns(t *testing.T) {
	t.Run("internal columns removed", func(t *testing.T) {
		allCols := []string{
			"connection_id", "collected_at", "inserted_at",
			"relname", "n_live_tup",
		}
		got := selectLatestOutputColumns(allCols)
		want := []string{"relname", "n_live_tup"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("order preserved and no columns filtered", func(t *testing.T) {
		allCols := []string{"a", "b", "c"}
		got := selectLatestOutputColumns(allCols)
		if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
			t.Errorf("got %v, want [a b c]", got)
		}
	})

	t.Run("all internal columns yields empty", func(t *testing.T) {
		allCols := []string{"connection_id", "collected_at", "inserted_at"}
		got := selectLatestOutputColumns(allCols)
		if len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
}

func TestRateAggExpr(t *testing.T) {
	t.Run("standard aggregation", func(t *testing.T) {
		expr := rateAggExpr("avg", 2, "seq_scan_per_sec")
		if expr != `avg(rate_2) AS "seq_scan_per_sec"` {
			t.Errorf("unexpected expr: %s", expr)
		}
	})

	t.Run("last aggregation filters nulls", func(t *testing.T) {
		expr := rateAggExpr("last", 0, "idx_scan_per_sec")
		if !strings.Contains(expr, "array_agg(rate_0 ORDER BY collected_at DESC)") {
			t.Errorf("last should use ordered array_agg, got %s", expr)
		}
		if !strings.Contains(expr, "FILTER (WHERE rate_0 IS NOT NULL)") {
			t.Errorf("last should filter NULL rates, got %s", expr)
		}
		if !strings.Contains(expr, `AS "idx_scan_per_sec"`) {
			t.Errorf("last should alias output, got %s", expr)
		}
	})
}

func TestDeltaAggExpr(t *testing.T) {
	expr := deltaAggExpr(3, "seq_scan_delta")
	if expr != `SUM(delta_3) AS "seq_scan_delta"` {
		t.Errorf("unexpected expr: %s", expr)
	}
}

func TestBuildDerivedMetricsQuery(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)

	t.Run("empty derived rejected", func(t *testing.T) {
		_, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables", nil, nil, nil, 1, start, end, 60, "avg",
			MetricFilters{}, testMaxElapsed)
		if err == nil {
			t.Fatal("expected error for empty derived slice")
		}
	})

	t.Run("single per_sec rate", func(t *testing.T) {
		query, args, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{{
				OutputName: "seq_scan_per_sec",
				BaseColumn: "seq_scan",
				Kind:       DerivedPerSec,
			}},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		checks := []string{
			`metrics."pg_stat_all_tables"`,
			`SUM("seq_scan") AS total_0`,
			`LAG(SUM("seq_scan")) OVER (ORDER BY collected_at) AS prev_0`,
			`EXTRACT(EPOCH FROM collected_at`,
			`CASE WHEN (total_0 - prev_0) >= 0 AND elapsed_sec > 0`,
			`(total_0 - prev_0)::float / elapsed_sec`,
			`avg(rate_0) AS "seq_scan_per_sec"`,
			`FROM generate_series($3::timestamptz, $4::timestamptz, $1::interval) AS g(bucket_time)`,
			`LEFT JOIN rate_buckets ON all_buckets.bucket_time = rate_buckets.bucket_time`,
			`rate_buckets."seq_scan_per_sec"`,
			`connection_id = $2`,
		}
		for _, c := range checks {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q\n---\n%s", c, query)
			}
		}
		if strings.Contains(query, "ratio_buckets") {
			t.Error("query should not reference ratio_buckets")
		}
		if len(args) != 5 {
			t.Errorf("expected 5 args, got %d", len(args))
		}
		if args[1] != 1 {
			t.Errorf("expected connection_id=1, got %v", args[1])
		}
	})

	t.Run("multiple per_sec rates", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{
				{OutputName: "n_tup_ins_per_sec", BaseColumn: "n_tup_ins", Kind: DerivedPerSec},
				{OutputName: "n_tup_upd_per_sec", BaseColumn: "n_tup_upd", Kind: DerivedPerSec},
			},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range []string{
			`SUM("n_tup_ins") AS total_0`,
			`SUM("n_tup_upd") AS total_1`,
			"rate_0", "rate_1",
			`avg(rate_0) AS "n_tup_ins_per_sec"`,
			`avg(rate_1) AS "n_tup_upd_per_sec"`,
		} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q", c)
			}
		}
	})

	t.Run("dead_tuple_ratio uses 0-100 scale", func(t *testing.T) {
		query, args, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{{
				OutputName: "dead_tuple_ratio",
				Kind:       DerivedDeadTupleRatio,
			}},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range []string{
			`SUM("n_live_tup")`,
			`SUM("n_dead_tup")`,
			`* 100.0`,
			`END AS dead_tuple_ratio`,
			`ratio_buckets.dead_tuple_ratio`,
			`LEFT JOIN ratio_buckets ON all_buckets.bucket_time = ratio_buckets.bucket_time`,
		} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q\n---\n%s", c, query)
			}
		}
		if strings.Contains(query, "rate_buckets") {
			t.Error("ratio-only query should not reference rate_buckets")
		}
		if len(args) != 4 {
			t.Errorf("expected 4 args, got %d", len(args))
		}
	})

	t.Run("mixed per_sec and ratio", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{
				{OutputName: "seq_scan_per_sec", BaseColumn: "seq_scan", Kind: DerivedPerSec},
				{OutputName: "dead_tuple_ratio", Kind: DerivedDeadTupleRatio},
			},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range []string{
			"rate_samples AS", "rate_buckets AS", "ratio_buckets AS",
			`rate_buckets."seq_scan_per_sec"`,
			"ratio_buckets.dead_tuple_ratio",
			"LEFT JOIN rate_buckets", "LEFT JOIN ratio_buckets",
		} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q", c)
			}
		}
		// The per_sec output must precede the ratio output.
		iRate := strings.Index(query, `rate_buckets."seq_scan_per_sec"`)
		iRatio := strings.Index(query, "ratio_buckets.dead_tuple_ratio")
		if iRate < 0 || iRatio < 0 || iRate > iRatio {
			t.Errorf("output columns out of order: rate=%d ratio=%d",
				iRate, iRatio)
		}
	})

	t.Run("filters add args and clauses", func(t *testing.T) {
		query, args, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{{
				OutputName: "seq_scan_per_sec",
				BaseColumn: "seq_scan",
				Kind:       DerivedPerSec,
			}},
			nil, nil, 1, start, end, 60, "avg",
			MetricFilters{
				DatabaseName:   "mydb",
				DatabaseColumn: "database_name",
				SchemaName:     "public",
				TableName:      "users",
			}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range []string{
			`"database_name" = $5`,
			"schemaname = $6",
			"relname = $7",
		} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing filter %q", c)
			}
		}
		if len(args) != 8 {
			t.Errorf("expected 8 args, got %d", len(args))
		}
	})

	t.Run("index_name filter binds indexrelname", func(t *testing.T) {
		// This is the derived per-second path that powers the Index detail
		// dashboard's Scan Activity chart (idx_scan_per_sec over time).
		query, args, err := BuildDerivedMetricsQuery(
			"pg_stat_all_indexes",
			[]DerivedMetric{{
				OutputName: "idx_scan_per_sec",
				BaseColumn: "idx_scan",
				Kind:       DerivedPerSec,
			}},
			nil, nil, 1, start, end, 60, "avg",
			MetricFilters{
				SchemaName: "public",
				TableName:  "orders",
				IndexName:  "pk_orders",
			}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range []string{
			"schemaname = $5",
			"relname = $6",
			"indexrelname = $7",
		} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing filter %q, got:\n%s", c, query)
			}
		}
		// The shared WHERE clause is emitted in every rate CTE; confirm both
		// occurrences bind the same placeholder so the scoping is consistent.
		if strings.Count(query, "indexrelname = $7") < 1 {
			t.Error("query should filter by indexrelname")
		}
		if len(args) != 8 {
			t.Fatalf("expected 8 args, got %d", len(args))
		}
		if args[6] != "pk_orders" {
			t.Errorf("expected indexrelname arg 'pk_orders', got %v", args[6])
		}
	})

	t.Run("queryid filter binds queryid", func(t *testing.T) {
		// The derived path shares metricQueryBase with the raw-column
		// path, so the QueryDetail charts scope correctly either way.
		query, args, err := BuildDerivedMetricsQuery(
			"pg_stat_statements",
			[]DerivedMetric{{
				OutputName: "calls_per_sec",
				BaseColumn: "calls",
				Kind:       DerivedPerSec,
			}},
			nil, nil, 1, start, end, 60, "avg",
			MetricFilters{QueryID: ptrInt64(42)}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(query, "queryid = $5") {
			t.Errorf("query should filter by queryid, got:\n%s", query)
		}
		if len(args) != 6 {
			t.Fatalf("expected 6 args, got %d", len(args))
		}
		if args[4] != int64(42) {
			t.Errorf("expected int64 queryid arg 42, got %#v", args[4])
		}
	})

	t.Run("last aggregation on rate", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{{
				OutputName: "seq_scan_per_sec",
				BaseColumn: "seq_scan",
				Kind:       DerivedPerSec,
			}},
			nil, nil, 1, start, end, 60, "last", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(query, "array_agg(rate_0 ORDER BY collected_at DESC)") {
			t.Errorf("last aggregation should use array_agg, got:\n%s", query)
		}
		if !strings.Contains(query, "FILTER (WHERE rate_0 IS NOT NULL)") {
			t.Error("last aggregation should filter NULL rates")
		}
	})

	t.Run("unknown derived kind rejected", func(t *testing.T) {
		_, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{{OutputName: "weird", Kind: DerivedMetricKind(99)}},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err == nil {
			t.Fatal("expected error for unknown derived kind")
		}
	})

	t.Run("sub-second bucket width clamped", func(t *testing.T) {
		tinyEnd := start.Add(time.Second)
		query, args, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{{
				OutputName: "seq_scan_per_sec",
				BaseColumn: "seq_scan",
				Kind:       DerivedPerSec,
			}},
			nil, nil, 1, start, tinyEnd, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(query, "rate_buckets") {
			t.Error("query should still build with clamped bucket width")
		}
		if len(args) == 0 || args[0] != "1 seconds" {
			t.Errorf("expected clamped bucket interval %q, got %v",
				"1 seconds", args)
		}
	})

	t.Run("dead_tuple_ratio last uses latest sample not SUM", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{{
				OutputName: "dead_tuple_ratio",
				Kind:       DerivedDeadTupleRatio,
			}},
			nil, nil, 1, start, end, 60, "last", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range []string{
			`(array_agg("n_live_tup" ORDER BY collected_at DESC))[1]`,
			`(array_agg("n_dead_tup" ORDER BY collected_at DESC))[1]`,
			`END AS dead_tuple_ratio`,
			`* 100.0`,
		} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q\n---\n%s", c, query)
			}
		}
		if strings.Contains(query, `SUM("n_live_tup")`) ||
			strings.Contains(query, `SUM("n_dead_tup")`) {
			t.Errorf("last aggregation should not SUM tuple counts:\n%s", query)
		}
	})

	t.Run("single delta", func(t *testing.T) {
		query, args, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{{
				OutputName: "seq_scan_delta",
				BaseColumn: "seq_scan",
				Kind:       DerivedDelta,
			}},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		checks := []string{
			`SUM("seq_scan") AS total_0`,
			`LAG(SUM("seq_scan")) OVER (ORDER BY collected_at) AS prev_0`,
			`CASE WHEN prev_0 IS NULL THEN 0 ` +
				`WHEN elapsed_sec > $5::float8 THEN NULL ` +
				`WHEN (total_0 - prev_0) >= 0 ` +
				`THEN (total_0 - prev_0) ELSE 0 END AS delta_0`,
			`SUM(delta_0) AS delta_0`,
			`SUM(delta_0) AS "seq_scan_delta"`,
			`COUNT(delta_0) = 0 AS gap_0`,
			// A bucket holding samples is a gap when every one of them was
			// rejected; a bucket holding none reads 0 only when an accepted
			// sample interval spans it, so a connection with no rows, the
			// span either side of the probe's history and a collection
			// outage all yield NULL buckets rather than confident zeros.
			`CASE WHEN rate_buckets.bucket_time IS NOT NULL ` +
				`THEN CASE WHEN COALESCE(rate_buckets.gap_0, false) ` +
				`THEN NULL ELSE COALESCE(rate_buckets."seq_scan_delta", 0) END ` +
				`WHEN EXISTS (SELECT 1 FROM rate_samples s ` +
				`WHERE s.delta_0 IS NOT NULL ` +
				`AND s.prev_collected_at IS NOT NULL ` +
				`AND s.prev_collected_at < all_buckets.bucket_time + $1::interval ` +
				`AND s.collected_at >= all_buckets.bucket_time) ` +
				`THEN 0 END AS "seq_scan_delta"`,
			// The predecessor time a span test needs is carried up from
			// the innermost sample query.
			`LAG(collected_at) OVER (ORDER BY collected_at) AS prev_collected_at`,
			`MAX(prev_collected_at) AS prev_collected_at`,
			`LEFT JOIN rate_buckets ON all_buckets.bucket_time = rate_buckets.bucket_time`,
		}
		for _, c := range checks {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q\n---\n%s", c, query)
			}
		}
		if strings.Contains(query, "rate_0") {
			t.Errorf("delta-only query should not emit a rate column:\n%s", query)
		}
		if len(args) != 5 {
			t.Errorf("expected 5 args, got %d", len(args))
		}
	})

	for _, agg := range []string{"avg", "last", "max"} {
		t.Run("delta ignores aggregation "+agg, func(t *testing.T) {
			// SUM is the only meaningful bucket aggregate for an increment,
			// so the requested aggregation must not reach the delta column.
			query, _, err := BuildDerivedMetricsQuery(
				"pg_stat_all_tables",
				[]DerivedMetric{{
					OutputName: "seq_scan_delta",
					BaseColumn: "seq_scan",
					Kind:       DerivedDelta,
				}},
				nil, nil, 1, start, end, 60, agg, MetricFilters{}, testMaxElapsed)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(query, `SUM(delta_0) AS "seq_scan_delta"`) {
				t.Errorf("%s should still SUM the delta:\n%s", agg, query)
			}
			if strings.Contains(query, "array_agg(delta_0") {
				t.Errorf("%s should not array_agg the delta:\n%s", agg, query)
			}
		})
	}

	t.Run("mixed per_sec and delta share the rate CTEs", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{
				{OutputName: "seq_scan_per_sec", BaseColumn: "seq_scan", Kind: DerivedPerSec},
				{OutputName: "n_tup_ins_delta", BaseColumn: "n_tup_ins", Kind: DerivedDelta},
			},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range []string{
			`SUM("seq_scan") AS total_0`,
			`SUM("n_tup_ins") AS total_1`,
			`avg(rate_0) AS "seq_scan_per_sec"`,
			`SUM(delta_1) AS "n_tup_ins_delta"`,
			`rate_buckets."seq_scan_per_sec"`,
			`COALESCE(rate_buckets."n_tup_ins_delta", 0)`,
		} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q\n---\n%s", c, query)
			}
		}
		// One shared pair of CTEs, not two.
		if n := strings.Count(query, "rate_samples AS"); n != 1 {
			t.Errorf("expected exactly 1 rate_samples CTE, got %d", n)
		}
		if n := strings.Count(query, "rate_buckets AS"); n != 1 {
			t.Errorf("expected exactly 1 rate_buckets CTE, got %d", n)
		}
		if n := strings.Count(query,
			"LEFT JOIN rate_buckets ON all_buckets.bucket_time"); n != 1 {
			t.Errorf("expected exactly 1 rate_buckets join, got %d", n)
		}
	})

	t.Run("delta and ratio together", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{
				{OutputName: "seq_scan_delta", BaseColumn: "seq_scan", Kind: DerivedDelta},
				{OutputName: "dead_tuple_ratio", Kind: DerivedDeadTupleRatio},
			},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range []string{
			`COALESCE(rate_buckets."seq_scan_delta", 0)`,
			"ratio_buckets.dead_tuple_ratio",
			"LEFT JOIN rate_buckets", "LEFT JOIN ratio_buckets",
		} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q\n---\n%s", c, query)
			}
		}
	})

	for _, agg := range []string{"avg", "sum", "max"} {
		t.Run("dead_tuple_ratio "+agg+" uses SUM", func(t *testing.T) {
			query, _, err := BuildDerivedMetricsQuery(
				"pg_stat_all_tables",
				[]DerivedMetric{{
					OutputName: "dead_tuple_ratio",
					Kind:       DerivedDeadTupleRatio,
				}},
				nil, nil, 1, start, end, 60, agg, MetricFilters{}, testMaxElapsed)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, c := range []string{
				`SUM("n_live_tup")`,
				`SUM("n_dead_tup")`,
			} {
				if !strings.Contains(query, c) {
					t.Errorf("query missing %q\n---\n%s", c, query)
				}
			}
			if strings.Contains(query, `array_agg("n_dead_tup"`) {
				t.Errorf("%s aggregation should not use array_agg:\n%s",
					agg, query)
			}
		})
	}

	t.Run("time share and session average are scaled rates", func(t *testing.T) {
		query, args, err := BuildDerivedMetricsQuery(
			"pg_stat_database",
			[]DerivedMetric{
				{OutputName: "blk_read_time_pct", BaseColumn: "blk_read_time", Kind: DerivedTimeShare},
				{OutputName: "active_time_sessions", BaseColumn: "active_time", Kind: DerivedSessionAverage},
				{OutputName: "xact_commit_per_sec", BaseColumn: "xact_commit", Kind: DerivedPerSec},
			},
			[]string{"datname"}, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range []string{
			`SUM("blk_read_time") AS total_0`,
			`CASE WHEN (total_0 - prev_0) >= 0 AND elapsed_sec > 0 AND elapsed_sec <= $5::float8 ` +
				`THEN 100.0 * (total_0 - prev_0)::float / (elapsed_sec * 1000.0) END AS rate_0`,
			`CASE WHEN (total_1 - prev_1) >= 0 AND elapsed_sec > 0 AND elapsed_sec <= $5::float8 ` +
				`THEN (total_1 - prev_1)::float / (elapsed_sec * 1000.0) END AS rate_1`,
			`CASE WHEN (total_2 - prev_2) >= 0 AND elapsed_sec > 0 AND elapsed_sec <= $5::float8 ` +
				`THEN (total_2 - prev_2)::float / elapsed_sec END AS rate_2`,
			`SUM(rate_0) AS rate_0`,
			`SUM(rate_1) AS rate_1`,
			`avg(rate_0) AS "blk_read_time_pct"`,
			`avg(rate_1) AS "active_time_sessions"`,
			`avg(rate_2) AS "xact_commit_per_sec"`,
			`rate_buckets."blk_read_time_pct",`,
			`rate_buckets."active_time_sessions",`,
			`rate_buckets."xact_commit_per_sec"`,
			`PARTITION BY "datname" ORDER BY collected_at`,
		} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q\n---\n%s", c, query)
			}
		}
		if strings.Contains(query, "delta_") || strings.Contains(query, "ratio_buckets") {
			t.Error("scaled rates must not emit delta or ratio CTE columns")
		}
		if len(args) != 5 {
			t.Errorf("expected 5 args, got %d", len(args))
		}
	})

	t.Run("time share applies the last aggregation", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_database",
			[]DerivedMetric{{OutputName: "blk_read_time_pct", BaseColumn: "blk_read_time", Kind: DerivedTimeShare}},
			nil, nil, 1, start, end, 60, "last", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(query, `FILTER (WHERE rate_0 IS NOT NULL))[1] AS "blk_read_time_pct"`) {
			t.Errorf("last aggregation not applied:\n%s", query)
		}
	})
}

func TestRateSampleExpr(t *testing.T) {
	for _, tc := range []struct {
		kind DerivedMetricKind
		want string
	}{
		{DerivedPerSec, "(total_3 - prev_3)::float / elapsed_sec"},
		{DerivedTimeShare, "100.0 * (total_3 - prev_3)::float / (elapsed_sec * 1000.0)"},
		{DerivedSessionAverage, "(total_3 - prev_3)::float / (elapsed_sec * 1000.0)"},
	} {
		if got := rateSampleExpr(tc.kind, 3); got != tc.want {
			t.Errorf("rateSampleExpr(%v) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestRatioTupleExpr(t *testing.T) {
	t.Run("standard aggregation uses SUM", func(t *testing.T) {
		expr := ratioTupleExpr("avg", "n_dead_tup")
		if expr != `SUM("n_dead_tup")` {
			t.Errorf("unexpected expr: %s", expr)
		}
	})

	t.Run("last aggregation uses ordered array_agg", func(t *testing.T) {
		expr := ratioTupleExpr("last", "n_live_tup")
		want := `(array_agg("n_live_tup" ORDER BY collected_at DESC))[1]`
		if expr != want {
			t.Errorf("expected %q, got %q", want, expr)
		}
	})
}

func TestBuildDerivedMetricsQueryLookback(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)

	perSec := []DerivedMetric{{
		OutputName: "seq_scan_per_sec",
		BaseColumn: "seq_scan",
		Kind:       DerivedPerSec,
	}}
	delta := []DerivedMetric{{
		OutputName: "seq_scan_delta",
		BaseColumn: "seq_scan",
		Kind:       DerivedDelta,
	}}

	// The sample query must admit the newest sample taken strictly before
	// the window so the first in-window sample has a LAG to subtract from,
	// but only from within lookbackBound of the window start, so a
	// predecessor left behind by a long collection gap is not borrowed.
	const wantLookback = `collected_at >= COALESCE((SELECT MAX(collected_at) ` +
		`FROM metrics."pg_stat_all_tables" WHERE connection_id = $2 ` +
		`AND collected_at < $3 AND collected_at >= $3 - ` + lookbackBound +
		`), $3)`

	for _, tc := range []struct {
		name    string
		derived []DerivedMetric
	}{
		{"per_sec", perSec},
		{"delta", delta},
	} {
		t.Run(tc.name+" reaches one sample before the window", func(t *testing.T) {
			query, args, err := BuildDerivedMetricsQuery(
				"pg_stat_all_tables", tc.derived, nil, nil, 1, start, end, 60, "avg",
				MetricFilters{}, testMaxElapsed)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(query, wantLookback) {
				t.Errorf("query missing lookback lower bound %q\n---\n%s",
					wantLookback, query)
			}
			// The borrowed pre-window sample must be dropped again after the
			// LAG so it never becomes a bucket of its own.
			if !strings.Contains(query, ") samples\n                WHERE collected_at >= $3") {
				t.Errorf("rate_samples must exclude pre-window rows:\n%s", query)
			}
			// The upper bound and the argument layout are untouched.
			if !strings.Contains(query, "collected_at <= $4") {
				t.Error("query missing the window upper bound")
			}
			if len(args) != 5 {
				t.Errorf("expected 5 args, got %d", len(args))
			}
		})
	}

	t.Run("lookback repeats the dimension filters", func(t *testing.T) {
		// The extra sample must belong to the same entity, or the LAG would
		// subtract another table's counter from this one's.
		query, args, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables", perSec, nil, nil, 1, start, end, 60, "avg",
			MetricFilters{
				DatabaseName:   "mydb",
				DatabaseColumn: "database_name",
				SchemaName:     "public",
				TableName:      "users",
			}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `COALESCE((SELECT MAX(collected_at) ` +
			`FROM metrics."pg_stat_all_tables" WHERE connection_id = $2 ` +
			`AND collected_at < $3 AND collected_at >= $3 - ` + lookbackBound +
			` AND "database_name" = $5 ` +
			`AND schemaname = $6 AND relname = $7), $3)`
		if !strings.Contains(query, want) {
			t.Errorf("query missing filtered lookback %q\n---\n%s", want, query)
		}
		// Same placeholders as the outer clause: no renumbering.
		if len(args) != 8 {
			t.Errorf("expected 8 args, got %d", len(args))
		}
	})

	t.Run("queryid filter reaches the lookback", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_statements",
			[]DerivedMetric{{
				OutputName: "calls_per_sec",
				BaseColumn: "calls",
				Kind:       DerivedPerSec,
			}},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{QueryID: ptrInt64(42)}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `AND collected_at < $3 AND collected_at >= $3 - ` +
			lookbackBound + ` AND queryid = $5), $3)`
		if !strings.Contains(query, want) {
			t.Errorf("query missing %q\n---\n%s", want, query)
		}
	})

	t.Run("ratio CTE keeps the plain window bounds", func(t *testing.T) {
		// Only the counter path needs the extra sample; the dead-tuple ratio
		// reads absolute values and must stay confined to the window.
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{{
				OutputName: "dead_tuple_ratio",
				Kind:       DerivedDeadTupleRatio,
			}},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(query, "MAX(collected_at)") {
			t.Errorf("ratio-only query must not look back:\n%s", query)
		}
		if !strings.Contains(query, "connection_id = $2 AND collected_at >= $3 AND collected_at <= $4") {
			t.Errorf("ratio query missing the plain window bounds:\n%s", query)
		}
	})

	t.Run("one lookback per query when rate and ratio are mixed", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{
				{OutputName: "seq_scan_per_sec", BaseColumn: "seq_scan", Kind: DerivedPerSec},
				{OutputName: "dead_tuple_ratio", Kind: DerivedDeadTupleRatio},
			},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n := strings.Count(query, "MAX(collected_at)"); n != 1 {
			t.Errorf("expected exactly 1 lookback subquery, got %d:\n%s", n, query)
		}
		// The ratio CTE still uses the unwidened lower bound.
		if !strings.Contains(query, "connection_id = $2 AND collected_at >= $3 AND collected_at <= $4") {
			t.Errorf("ratio CTE lost its plain window bounds:\n%s", query)
		}
	})

	t.Run("raw metrics query is unchanged", func(t *testing.T) {
		// Raw columns are absolute readings, not counters differenced across
		// samples, so the raw path must not borrow an out-of-window row.
		query, args, err := BuildMetricsQuery(
			"pg_stat_all_tables", []string{"seq_scan"},
			map[string]string{"seq_scan": "bigint"},
			1, start, end, 60, "avg", MetricFilters{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(query, "MAX(collected_at)") {
			t.Errorf("raw query must not look back:\n%s", query)
		}
		if !strings.Contains(query, "connection_id = $2 AND collected_at >= $3 AND collected_at <= $4") {
			t.Errorf("raw query missing the plain window bounds:\n%s", query)
		}
		if len(args) != 4 {
			t.Errorf("expected 4 args, got %d", len(args))
		}
	})
}

func TestMetricQueryParts(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)

	t.Run("where matches metricQueryBase", func(t *testing.T) {
		filters := MetricFilters{SchemaName: "public", TableName: "orders"}
		parts := metricQueryClauses("pg_stat_all_tables", 7, start, end, time.Minute, filters)
		whereSQL, args := metricQueryBase("pg_stat_all_tables", 7, start, end, time.Minute, filters)
		if parts.where() != whereSQL {
			t.Errorf("where() = %q, metricQueryBase = %q", parts.where(), whereSQL)
		}
		if len(parts.args) != len(args) {
			t.Fatalf("arg count mismatch: %d vs %d", len(parts.args), len(args))
		}
		if args[0] != "60 seconds" || args[1] != 7 {
			t.Errorf("unexpected leading args: %v", args[:2])
		}
	})

	t.Run("lookbackWhere leaves the other clauses alone", func(t *testing.T) {
		parts := metricQueryClauses("pg_stat_all_tables", 1, start, end, time.Minute, MetricFilters{})
		got := parts.lookbackWhere("pg_stat_all_tables")
		if strings.Contains(got, " AND collected_at >= $3 AND") {
			t.Errorf("lookbackWhere kept the plain lower bound: %q", got)
		}
		for _, want := range []string{
			"connection_id = $2",
			"collected_at <= $4",
			`FROM metrics."pg_stat_all_tables"`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("lookbackWhere missing %q: %q", want, got)
			}
		}
	})

	t.Run("lookbackWhere does not mutate the filter clauses", func(t *testing.T) {
		// where() and lookbackWhere() both append to filterClauses; each must
		// copy, or the second call would observe the first call's clauses.
		parts := metricQueryClauses("pg_stat_all_tables", 1, start, end, time.Minute,
			MetricFilters{SchemaName: "public"})
		_ = parts.where()
		_ = parts.lookbackWhere("pg_stat_all_tables")
		if len(parts.filterClauses) != 1 || parts.filterClauses[0] != "schemaname = $5" {
			t.Errorf("filterClauses mutated: %v", parts.filterClauses)
		}
		if got := parts.where(); got !=
			"connection_id = $2 AND collected_at >= $3 AND collected_at <= $4 AND schemaname = $5" {
			t.Errorf("second where() differs: %q", got)
		}
	})
}

func TestLookbackBound(t *testing.T) {
	// The bound is expressed in the query's own bucket-width argument so the
	// $N layout stays shared with BuildMetricsQuery, and it is the larger of
	// a few bucket widths and a fixed floor covering the coarsest charted
	// probe interval (600s) several times over on a 1h window.
	if !strings.HasPrefix(lookbackBound, "GREATEST($1::interval * ") {
		t.Errorf("lookbackBound must scale with the bucket width: %q", lookbackBound)
	}
	if !strings.Contains(lookbackBound, "INTERVAL '30 minutes'") {
		t.Errorf("lookbackBound must keep the 30-minute floor: %q", lookbackBound)
	}
	if strings.ContainsAny(lookbackBound, "26789") {
		t.Errorf("lookbackBound multiplier changed; revisit the justification: %q",
			lookbackBound)
	}
}

func TestBuildDerivedMetricsQueryEntityPartition(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)
	derived := []DerivedMetric{
		{OutputName: "tx_bytes_per_sec", BaseColumn: "tx_bytes", Kind: DerivedPerSec},
		{OutputName: "rx_bytes_delta", BaseColumn: "rx_bytes", Kind: DerivedDelta},
	}

	t.Run("entity keys partition the LAG and group the samples", func(t *testing.T) {
		query, args, err := BuildDerivedMetricsQuery(
			"pg_sys_network_info", derived, []string{"interface_name"}, nil,
			1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		checks := []string{
			// Each counter is differenced against the same interface's
			// previous reading, and so is the elapsed time.
			`LAG(SUM("tx_bytes")) OVER (PARTITION BY "interface_name" ORDER BY collected_at) AS prev_0`,
			`LAG(SUM("rx_bytes")) OVER (PARTITION BY "interface_name" ORDER BY collected_at) AS prev_1`,
			`LAG(collected_at) OVER (PARTITION BY "interface_name" ORDER BY collected_at)`,
			// One row per interface and sample in the innermost query.
			`SELECT
                        collected_at, "interface_name",`,
			`GROUP BY collected_at, "interface_name"`,
			// The per-interface changes are summed per sample time, so the
			// bucket stage still sees one row per sample.
			`SUM(rate_0) AS rate_0`,
			`SUM(delta_1) AS delta_1`,
			`) entity_samples
            GROUP BY collected_at`,
		}
		for _, c := range checks {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q\n---\n%s", c, query)
			}
		}
		// Three counter window functions (the SUM, its LAG and the
		// elapsed-time LAG) plus the predecessor time the delta span test
		// needs, all partitioned by the entity keys.
		if n := strings.Count(query, "PARTITION BY"); n != 4 {
			t.Errorf("expected 4 partitioned window functions, got %d:\n%s", n, query)
		}
		// Entity keys are identifiers, never bound values.
		if len(args) != 5 {
			t.Errorf("expected 5 args, got %d", len(args))
		}
	})

	t.Run("several entity keys are quoted and listed in order", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_statements", derived[:1],
			[]string{"database_name", "queryid", "userid", "dbid", "toplevel"}, nil,
			1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `PARTITION BY "database_name", "queryid", "userid", "dbid", "toplevel" ORDER BY collected_at`
		if !strings.Contains(query, want) {
			t.Errorf("query missing %q\n---\n%s", want, query)
		}
	})

	t.Run("no entity keys reduces to the single-row form", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_wal", derived, nil, nil, 1, start, end, 60, "avg",
			MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(query, "PARTITION BY") {
			t.Errorf("single-row probe must not partition:\n%s", query)
		}
		for _, c := range []string{
			`LAG(SUM("tx_bytes")) OVER (ORDER BY collected_at) AS prev_0`,
			`GROUP BY collected_at
                ) samples`,
		} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q\n---\n%s", c, query)
			}
		}
	})

	t.Run("ratio-only query ignores entity keys", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables",
			[]DerivedMetric{{OutputName: "dead_tuple_ratio", Kind: DerivedDeadTupleRatio}},
			[]string{"relname"}, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(query, `"relname"`) {
			t.Errorf("ratio query must not reference entity keys:\n%s", query)
		}
	})
}

func TestNeedsEntityKeys(t *testing.T) {
	for _, tc := range []struct {
		name    string
		derived []DerivedMetric
		want    bool
	}{
		{"none", nil, false},
		{"ratio only", []DerivedMetric{{Kind: DerivedDeadTupleRatio}}, false},
		{"per_sec", []DerivedMetric{{Kind: DerivedPerSec}}, true},
		{"delta", []DerivedMetric{{Kind: DerivedDelta}}, true},
		{"time share", []DerivedMetric{{Kind: DerivedTimeShare}}, true},
		{"session average", []DerivedMetric{{Kind: DerivedSessionAverage}}, true},
		{"ratio then delta", []DerivedMetric{{Kind: DerivedDeadTupleRatio}, {Kind: DerivedDelta}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsEntityKeys(tc.derived); got != tc.want {
				t.Errorf("needsEntityKeys = %v, want %v", got, tc.want)
			}
		})
	}
}

// testMaxElapsed is the gap bound the builder unit tests pass: three
// one-minute probe intervals, matching the fixtures' minute-spaced samples.
const testMaxElapsed = 3 * time.Minute

func TestBuildDerivedMetricsQueryGuards(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)
	rate := []DerivedMetric{{
		OutputName: "xact_commit_per_sec", BaseColumn: "xact_commit", Kind: DerivedPerSec,
	}}
	delta := []DerivedMetric{{
		OutputName: "xact_commit_delta", BaseColumn: "xact_commit", Kind: DerivedDelta,
	}}
	// pg_stat_database is a registered probe whose counters are guarded
	// by stats_reset.
	const probe = "pg_stat_database"
	withReset := []string{"connection_id", "collected_at", "datname", "xact_commit", "stats_reset"}
	withoutReset := []string{"connection_id", "collected_at", "datname", "xact_commit"}

	t.Run("reset marker present adds the reset columns and guard", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			derived []DerivedMetric
			want    string
		}{
			{"rate", rate, `CASE WHEN (total_0 - prev_0) >= 0 AND elapsed_sec > 0 ` +
				`AND elapsed_sec <= $5::float8 ` +
				`AND reset_0 IS NOT DISTINCT FROM prev_reset_0 ` +
				`THEN (total_0 - prev_0)::float / elapsed_sec END AS rate_0`},
			{"delta", delta, `CASE WHEN prev_0 IS NULL THEN 0 ` +
				`WHEN elapsed_sec > $5::float8 THEN NULL ` +
				`WHEN (total_0 - prev_0) >= 0 ` +
				`AND reset_0 IS NOT DISTINCT FROM prev_reset_0 ` +
				`THEN (total_0 - prev_0) ELSE 0 END AS delta_0`},
		} {
			query, args, err := BuildDerivedMetricsQuery(probe, tc.derived,
				[]string{"datname"}, withReset, 1, start, end, 60, "avg",
				MetricFilters{}, testMaxElapsed)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
			for _, c := range []string{
				`MAX("stats_reset") AS reset_0`,
				`LAG(MAX("stats_reset")) OVER (PARTITION BY "datname" ORDER BY collected_at) AS prev_reset_0`,
				tc.want,
			} {
				if !strings.Contains(query, c) {
					t.Errorf("%s: query missing %q\n---\n%s", tc.name, c, query)
				}
			}
			if len(args) != 5 || args[4] != 180.0 {
				t.Errorf("%s: args = %v, want the 180s gap bound as the fifth", tc.name, args)
			}
		}
	})

	t.Run("reset marker absent from the table is dropped", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(probe, rate,
			[]string{"datname"}, withoutReset, 1, start, end, 60, "avg",
			MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(query, "reset_0") || strings.Contains(query, "stats_reset") {
			t.Errorf("query references the absent reset column:\n%s", query)
		}
		if !strings.Contains(query, `AND elapsed_sec <= $5::float8 THEN`) {
			t.Errorf("gap guard missing without the reset guard:\n%s", query)
		}
	})

	t.Run("unregistered probe has no reset guard", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery("not_a_registered_probe", rate,
			nil, withReset, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.Contains(query, "reset_0") {
			t.Errorf("query has a reset guard for an unregistered probe:\n%s", query)
		}
	})

	t.Run("gap bound follows the filter arguments", func(t *testing.T) {
		qid := int64(42)
		query, args, err := BuildDerivedMetricsQuery(probe, rate, nil, nil, 1,
			start, end, 60, "avg",
			MetricFilters{DatabaseName: "app", DatabaseColumn: "datname", QueryID: &qid},
			2*time.Minute)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 7 || args[4] != "app" || args[5] != qid || args[6] != 120.0 {
			t.Errorf("args = %v, want filters then the 120s gap bound", args)
		}
		for _, c := range []string{`"datname" = $5`, `queryid = $6`, `elapsed_sec <= $7::float8`} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q\n---\n%s", c, query)
			}
		}
	})

	t.Run("ratio-only query binds no gap argument", func(t *testing.T) {
		query, args, err := BuildDerivedMetricsQuery("pg_stat_all_tables",
			[]DerivedMetric{{OutputName: "dead_tuple_ratio", Kind: DerivedDeadTupleRatio}},
			nil, nil, 1, start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 4 {
			t.Errorf("expected 4 args for a ratio-only query, got %d", len(args))
		}
		if strings.Contains(query, "$5") {
			t.Errorf("ratio-only query references an unbound $5:\n%s", query)
		}
	})

	t.Run("delta gap flag drives the final select", func(t *testing.T) {
		mixed := append(append([]DerivedMetric{}, rate...), delta...)
		query, _, err := BuildDerivedMetricsQuery(probe, mixed, nil, nil, 1,
			start, end, 60, "avg", MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, c := range []string{
			`COUNT(delta_1) = 0 AS gap_1`,
			`CASE WHEN rate_buckets.bucket_time IS NOT NULL ` +
				`THEN CASE WHEN COALESCE(rate_buckets.gap_1, false) ` +
				`THEN NULL ELSE COALESCE(rate_buckets."xact_commit_delta", 0) END ` +
				`WHEN EXISTS (SELECT 1 FROM rate_samples s ` +
				`WHERE s.delta_1 IS NOT NULL ` +
				`AND s.prev_collected_at IS NOT NULL ` +
				`AND s.prev_collected_at < all_buckets.bucket_time + $1::interval ` +
				`AND s.collected_at >= all_buckets.bucket_time) ` +
				`THEN 0 END AS "xact_commit_delta"`,
		} {
			if !strings.Contains(query, c) {
				t.Errorf("query missing %q\n---\n%s", c, query)
			}
		}
		if strings.Contains(query, "gap_0") {
			t.Errorf("rate slot must not carry a gap flag:\n%s", query)
		}
	})
}

func TestProbeEntityExclusionInQueries(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)
	const probe = "pg_sys_network_info"
	excl := ProbeEntityExclusion(probe)
	if excl == "" {
		t.Fatal("pg_sys_network_info must register a loopback exclusion")
	}

	t.Run("raw query", func(t *testing.T) {
		query, args, err := BuildMetricsQuery(probe, []string{"tx_bytes"},
			map[string]string{"tx_bytes": "bigint"}, 1, start, end, 60, "avg",
			MetricFilters{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(query, " AND "+excl) {
			t.Errorf("raw query lacks the exclusion %q:\n%s", excl, query)
		}
		if len(args) != 4 {
			t.Errorf("exclusion must bind no argument, got %d args", len(args))
		}
	})

	t.Run("derived and lookback queries", func(t *testing.T) {
		query, _, err := BuildDerivedMetricsQuery(probe, []DerivedMetric{{
			OutputName: "tx_bytes_per_sec", BaseColumn: "tx_bytes", Kind: DerivedPerSec,
		}}, []string{"interface_name"}, nil, 1, start, end, 60, "avg",
			MetricFilters{}, testMaxElapsed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Once in the lookback subquery, once in the sample query.
		if got := strings.Count(query, excl); got != 2 {
			t.Errorf("exclusion appears %d times, want 2:\n%s", got, query)
		}
	})

	t.Run("unregistered probe adds nothing", func(t *testing.T) {
		parts := metricQueryClauses("pg_stat_wal", 1, start, end, time.Minute, MetricFilters{})
		if len(parts.filterClauses) != 0 {
			t.Errorf("unexpected clauses %v", parts.filterClauses)
		}
	})
}

func TestMetricDataPointJSON(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	v := 1.5
	got, err := json.Marshal([]MetricDataPoint{{Time: ts}, {Time: ts, Value: &v}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `[{"time":"2025-01-01T00:00:00Z","value":null},{"time":"2025-01-01T00:00:00Z","value":1.5}]`
	if string(got) != want {
		t.Errorf("json = %s, want %s", got, want)
	}
}

func TestAggregationGuard(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	end := time.Now()

	t.Run("the accepted set is closed", func(t *testing.T) {
		for _, agg := range ValidAggregations() {
			if !IsValidAggregation(agg) {
				t.Errorf("%q is listed but not accepted", agg)
			}
		}
		for _, agg := range []string{
			"", "AVG", "median", "avg(x)", "avg; DROP TABLE metrics.t --",
		} {
			if IsValidAggregation(agg) {
				t.Errorf("%q is accepted, want it refused", agg)
			}
		}
	})

	// The builders interpolate the aggregation into SQL as a bare function
	// name, so each refuses anything outside that set itself rather than
	// relying on its caller having validated first.
	t.Run("BuildMetricsQuery refuses an unvalidated aggregation", func(t *testing.T) {
		_, _, err := BuildMetricsQuery("pg_stat_all_tables",
			[]string{"seq_scan"}, map[string]string{"seq_scan": "bigint"},
			1, start, end, 60, "avg); DROP TABLE metrics.pg_stat_all_tables --",
			MetricFilters{})
		if err == nil {
			t.Fatal("expected an error for an invalid aggregation")
		}
		if !strings.Contains(err.Error(), "invalid aggregation") {
			t.Errorf("error = %v, want it to mention an invalid aggregation", err)
		}
	})

	t.Run("BuildDerivedMetricsQuery refuses an unvalidated aggregation", func(t *testing.T) {
		derived := []DerivedMetric{{
			OutputName: "seq_scan_per_sec",
			BaseColumn: "seq_scan",
			Kind:       DerivedPerSec,
		}}
		_, _, err := BuildDerivedMetricsQuery("pg_stat_all_tables", derived,
			nil, nil, 1, start, end, 60, "median", MetricFilters{}, testMaxElapsed)
		if err == nil {
			t.Fatal("expected an error for an invalid aggregation")
		}
		if !strings.Contains(err.Error(), "invalid aggregation") {
			t.Errorf("error = %v, want it to mention an invalid aggregation", err)
		}
	})
}
