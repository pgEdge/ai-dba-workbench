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
	"fmt"
	"strings"
	"testing"
	"time"
)

// baselineKey renders a row's identity and mean for comparison.
func baselineKey(b MetricBaseline) string {
	db := "-"
	if b.DatabaseName != nil {
		db = *b.DatabaseName
	}
	key := fmt.Sprintf("%s|%s|%s", b.Metric, db, b.PeriodType)
	if b.HourOfDay != nil {
		key += fmt.Sprintf("|h%d", *b.HourOfDay)
	}
	if b.DayOfWeek != nil {
		key += fmt.Sprintf("|d%d", *b.DayOfWeek)
	}
	return fmt.Sprintf("%s=%g", key, b.Mean)
}

func baselineKeys(rows []MetricBaseline) string {
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = baselineKey(r)
	}
	return strings.Join(keys, " ")
}

// TestQueryBaselines_Integration seeds metric_baselines with all, hourly
// and daily rows over two databases and a server-level metric, and checks
// that each row comes back as stored: nothing is averaged across periods
// or databases, and each filter narrows the set as documented.
func TestQueryBaselines_Integration(t *testing.T) {
	pool := newCollectorSchemaPool(t)
	ctx := context.Background()

	conn := insertTestConnection(t, pool, "baselines-a")
	other := insertTestConnection(t, pool, "baselines-b")
	earliest := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	insert := func(connID int, db any, metric, period string, dow, hod any, mean float64) {
		t.Helper()
		execSchema(t, pool, `
            INSERT INTO metric_baselines
                (connection_id, database_name, metric_name, period_type,
                 day_of_week, hour_of_day, mean, stddev, min, max,
                 sample_count, earliest_sample_at)
            VALUES ($1, $2, $3, $4, $5, $6, $7, 2, 0, 100, 42, $8)`,
			connID, db, metric, period, dow, hod, mean, earliest)
	}
	const temp = "pg_stat_database.temp_files_delta"
	const count = "pg_stat_activity.count"
	insert(conn, "db1", temp, "all", nil, nil, 10)
	insert(conn, "db2", temp, "all", nil, nil, 20)
	insert(conn, "db1", temp, "hourly", nil, 3, 11)
	insert(conn, "db1", temp, "hourly", nil, 4, 12)
	insert(conn, "db2", temp, "daily", 0, nil, 21)
	insert(conn, nil, count, "all", nil, nil, 30)
	insert(conn, nil, count, "hourly", nil, 3, 31)
	insert(other, "db1", temp, "all", nil, nil, 99)

	db1 := "db1"
	db2 := "db2"
	tests := []struct {
		name    string
		probe   string
		metrics []string
		opts    BaselineQueryOptions
		want    string
	}{
		{
			name: "default is the all period for every database",
			want: "pg_stat_activity.count|-|all=30 " +
				"pg_stat_database.temp_files_delta|db1|all=10 " +
				"pg_stat_database.temp_files_delta|db2|all=20",
		},
		{
			name: "hourly rows are returned one per hour",
			opts: BaselineQueryOptions{PeriodType: "hourly"},
			want: "pg_stat_activity.count|-|hourly|h3=31 " +
				"pg_stat_database.temp_files_delta|db1|hourly|h3=11 " +
				"pg_stat_database.temp_files_delta|db1|hourly|h4=12",
		},
		{
			name: "daily rows for one database",
			opts: BaselineQueryOptions{PeriodType: "daily", DatabaseName: &db2},
			want: "pg_stat_database.temp_files_delta|db2|daily|d0=21",
		},
		{
			name: "a database drops server-level rows",
			opts: BaselineQueryOptions{DatabaseName: &db1},
			want: "pg_stat_database.temp_files_delta|db1|all=10",
		},
		{
			name:  "probe prefix",
			probe: "pg_stat_activity",
			want:  "pg_stat_activity.count|-|all=30",
		},
		{
			name:  "probe underscore is not a wildcard",
			probe: "pg_stat_activ_ty",
			want:  "",
		},
		{
			name:    "requested metrics",
			metrics: []string{temp},
			opts:    BaselineQueryOptions{PeriodType: "all"},
			want: "pg_stat_database.temp_files_delta|db1|all=10 " +
				"pg_stat_database.temp_files_delta|db2|all=20",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QueryBaselines(ctx, pool, conn, tt.probe, tt.metrics, tt.opts)
			if err != nil {
				t.Fatalf("QueryBaselines: %v", err)
			}
			if got == nil {
				t.Fatal("QueryBaselines returned nil, want an empty slice")
			}
			if keys := baselineKeys(got); keys != tt.want {
				t.Errorf("rows = %q\nwant   %q", keys, tt.want)
			}
		})
	}

	t.Run("row detail", func(t *testing.T) {
		got, err := QueryBaselines(ctx, pool, conn, "", []string{count}, BaselineQueryOptions{})
		if err != nil || len(got) != 1 {
			t.Fatalf("QueryBaselines = %v, %v; want one row", got, err)
		}
		b := got[0]
		if b.SampleCount != 42 || b.EarliestSampleAt == nil || !b.EarliestSampleAt.Equal(earliest) {
			t.Errorf("sample_count = %d, earliest_sample_at = %v", b.SampleCount, b.EarliestSampleAt)
		}
		if b.P50 != 30 || b.P95 != 34 || b.P99 != 36 || b.Stddev != 2 || b.Max != 100 {
			t.Errorf("approximations = %+v, want p50 30, p95 34, p99 36", b)
		}
	})

	t.Run("invalid period type", func(t *testing.T) {
		_, err := QueryBaselines(ctx, pool, conn, "", nil, BaselineQueryOptions{PeriodType: "overall"})
		if err == nil || !strings.Contains(err.Error(), "invalid period_type") {
			t.Fatalf("err = %v, want an invalid period_type error", err)
		}
	})

	t.Run("query failure", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := QueryBaselines(cctx, pool, conn, "", nil, BaselineQueryOptions{}); err == nil {
			t.Fatal("QueryBaselines with an already-done context succeeded")
		}
	})
}
