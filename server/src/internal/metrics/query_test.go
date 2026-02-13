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
	"strings"
	"testing"
	"time"
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
		if !strings.Contains(query, `COALESCE(data_buckets."xact_commit", 0) AS "xact_commit"`) {
			t.Error("query should COALESCE metric columns to fill NULL gaps")
		}

		// Check args
		if len(args) != 4 {
			t.Errorf("expected 4 args, got %d", len(args))
		}
		if args[1] != 1 {
			t.Errorf("expected connection_id=1, got %v", args[1])
		}
	})

	t.Run("with filters", func(t *testing.T) {
		query, args, err := BuildMetricsQuery(
			"pg_stat_database",
			[]string{"xact_commit"},
			map[string]string{"xact_commit": "bigint"},
			1, start, end, 60, "sum",
			MetricFilters{
				DatabaseName: "mydb",
				SchemaName:   "public",
				TableName:    "users",
			},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(query, "datname = $5") {
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

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name       string
		input      interface{}
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
