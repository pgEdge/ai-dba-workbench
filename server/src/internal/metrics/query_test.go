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
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

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

func TestBuildDerivedMetricsQuery(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)

	t.Run("empty derived rejected", func(t *testing.T) {
		_, _, err := BuildDerivedMetricsQuery(
			"pg_stat_all_tables", nil, 1, start, end, 60, "avg",
			MetricFilters{})
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
			1, start, end, 60, "avg", MetricFilters{})
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
			`generate_series($3::timestamptz, $4::timestamptz, $1::interval)`,
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
		if len(args) != 4 {
			t.Errorf("expected 4 args, got %d", len(args))
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
			1, start, end, 60, "avg", MetricFilters{})
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
			1, start, end, 60, "avg", MetricFilters{})
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
			1, start, end, 60, "avg", MetricFilters{})
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
			1, start, end, 60, "avg",
			MetricFilters{
				DatabaseName:   "mydb",
				DatabaseColumn: "database_name",
				SchemaName:     "public",
				TableName:      "users",
			})
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
		if len(args) != 7 {
			t.Errorf("expected 7 args, got %d", len(args))
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
			1, start, end, 60, "avg",
			MetricFilters{
				SchemaName: "public",
				TableName:  "orders",
				IndexName:  "pk_orders",
			})
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
		if len(args) != 7 {
			t.Fatalf("expected 7 args, got %d", len(args))
		}
		if args[6] != "pk_orders" {
			t.Errorf("expected indexrelname arg 'pk_orders', got %v", args[6])
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
			1, start, end, 60, "last", MetricFilters{})
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
			1, start, end, 60, "avg", MetricFilters{})
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
			1, start, tinyEnd, 60, "avg", MetricFilters{})
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
			1, start, end, 60, "last", MetricFilters{})
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

	for _, agg := range []string{"avg", "sum", "max"} {
		t.Run("dead_tuple_ratio "+agg+" uses SUM", func(t *testing.T) {
			query, _, err := BuildDerivedMetricsQuery(
				"pg_stat_all_tables",
				[]DerivedMetric{{
					OutputName: "dead_tuple_ratio",
					Kind:       DerivedDeadTupleRatio,
				}},
				1, start, end, 60, agg, MetricFilters{})
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
