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

func TestGetQuotedSelectCols(t *testing.T) {
	cols := GetQuotedSelectCols([]string{"col_a", "col_b"})
	if len(cols) != 2 {
		t.Fatalf("expected 2 cols, got %d", len(cols))
	}
	if cols[0] != `"col_a"` {
		t.Errorf("expected quoted col, got %s", cols[0])
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

func TestGetCoalescedSelectCols(t *testing.T) {
	t.Run("numeric columns", func(t *testing.T) {
		colTypes := map[string]string{
			"xact_commit": "bigint",
			"blks_hit":    "bigint",
		}
		cols := GetCoalescedSelectCols([]string{"xact_commit", "blks_hit"}, "data_buckets", colTypes)
		if len(cols) != 2 {
			t.Fatalf("expected 2 cols, got %d", len(cols))
		}
		expected0 := `COALESCE(data_buckets."xact_commit", 0) AS "xact_commit"`
		if cols[0] != expected0 {
			t.Errorf("cols[0] = %s, want %s", cols[0], expected0)
		}
		expected1 := `COALESCE(data_buckets."blks_hit", 0) AS "blks_hit"`
		if cols[1] != expected1 {
			t.Errorf("cols[1] = %s, want %s", cols[1], expected1)
		}
	})

	t.Run("interval columns", func(t *testing.T) {
		colTypes := map[string]string{
			"write_lag":  "interval",
			"replay_lag": "interval",
		}
		cols := GetCoalescedSelectCols([]string{"write_lag", "replay_lag"}, "data_buckets", colTypes)
		if len(cols) != 2 {
			t.Fatalf("expected 2 cols, got %d", len(cols))
		}
		expected0 := `COALESCE(data_buckets."write_lag", '0 seconds'::interval) AS "write_lag"`
		if cols[0] != expected0 {
			t.Errorf("cols[0] = %s, want %s", cols[0], expected0)
		}
		expected1 := `COALESCE(data_buckets."replay_lag", '0 seconds'::interval) AS "replay_lag"`
		if cols[1] != expected1 {
			t.Errorf("cols[1] = %s, want %s", cols[1], expected1)
		}
	})

	t.Run("mixed columns", func(t *testing.T) {
		colTypes := map[string]string{
			"sent_lsn":  "bigint",
			"write_lag": "interval",
		}
		cols := GetCoalescedSelectCols([]string{"sent_lsn", "write_lag"}, "data_buckets", colTypes)
		if len(cols) != 2 {
			t.Fatalf("expected 2 cols, got %d", len(cols))
		}
		if !strings.Contains(cols[0], ", 0)") {
			t.Errorf("numeric col should use 0 default, got %s", cols[0])
		}
		if !strings.Contains(cols[1], "'0 seconds'::interval") {
			t.Errorf("interval col should use interval default, got %s", cols[1])
		}
	})
}

func TestResolveMetricValue(t *testing.T) {
	t.Run("finite value is recorded as last known", func(t *testing.T) {
		lastKnown := map[string]float64{}
		val, ok := resolveMetricValue(float64(3.5), "1:cpu", lastKnown)
		if !ok || val != 3.5 {
			t.Fatalf("got (%v, %v), want (3.5, true)", val, ok)
		}
		if lastKnown["1:cpu"] != 3.5 {
			t.Errorf("lastKnown not updated: %v", lastKnown["1:cpu"])
		}
	})

	t.Run("null bucket with no prior value is skipped", func(t *testing.T) {
		lastKnown := map[string]float64{}
		_, ok := resolveMetricValue(nil, "1:cpu", lastKnown)
		if ok {
			t.Fatalf("expected skip for null with no prior value")
		}
		if _, exists := lastKnown["1:cpu"]; exists {
			t.Errorf("lastKnown should not be populated by a skipped bucket")
		}
	})

	t.Run("null bucket carries forward prior value", func(t *testing.T) {
		lastKnown := map[string]float64{"1:cpu": 7.25}
		val, ok := resolveMetricValue(nil, "1:cpu", lastKnown)
		if !ok || val != 7.25 {
			t.Fatalf("got (%v, %v), want (7.25, true)", val, ok)
		}
	})

	nonFinite := []struct {
		name string
		in   float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	}
	for _, nf := range nonFinite {
		t.Run(nf.name+" with no prior value is skipped", func(t *testing.T) {
			lastKnown := map[string]float64{}
			_, ok := resolveMetricValue(nf.in, "1:cpu", lastKnown)
			if ok {
				t.Fatalf("expected %s to be skipped when no prior value", nf.name)
			}
			if _, exists := lastKnown["1:cpu"]; exists {
				t.Errorf("%s must not poison lastKnown", nf.name)
			}
		})

		t.Run(nf.name+" carries forward prior value", func(t *testing.T) {
			lastKnown := map[string]float64{"1:cpu": 42}
			val, ok := resolveMetricValue(nf.in, "1:cpu", lastKnown)
			if !ok || val != 42 {
				t.Fatalf("got (%v, %v), want (42, true)", val, ok)
			}
			if lastKnown["1:cpu"] != 42 {
				t.Errorf("%s overwrote the last known good value: %v", nf.name, lastKnown["1:cpu"])
			}
		})
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

func TestBuildLatestRowsQuery(t *testing.T) {
	t.Run("connection filter and limit only", func(t *testing.T) {
		query, args := buildLatestRowsQuery(
			"pg_stat_all_tables",
			[]string{"relname", "n_live_tup"},
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
		if !strings.Contains(query, `ORDER BY "n_live_tup" desc, collected_at DESC`) {
			t.Errorf("query should order by quoted column then collected_at, got: %s", query)
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
		if !strings.Contains(query, `ORDER BY "idx_scan" asc, collected_at DESC`) {
			t.Errorf("query should order by idx_scan asc, got: %s", query)
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

	t.Run("database filter skipped without resolved column", func(t *testing.T) {
		query, args := buildLatestRowsQuery(
			"pg_sys_cpu_info",
			[]string{"cpu_user"},
			[]int{1},
			MetricFilters{DatabaseName: "northwind", DatabaseColumn: ""},
			"collected_at", "desc", 1,
		)

		if strings.Contains(query, "database_name") || strings.Contains(query, "datname") {
			t.Error("query should not filter by database when column unresolved")
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
