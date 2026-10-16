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
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func newPgSysMemoryInfoProbeForTest() *PgSysMemoryInfoProbe {
	return NewPgSysMemoryInfoProbe(&ProbeConfig{
		Name:                      ProbeNamePgSysMemoryInfo,
		CollectionIntervalSeconds: 600,
		RetentionDays:             7,
		IsEnabled:                 true,
	})
}

func TestPgSysMemoryInfoProbe_Surface(t *testing.T) {
	p := newPgSysMemoryInfoProbeForTest()
	if p.GetName() != ProbeNamePgSysMemoryInfo {
		t.Errorf("GetName() = %q, want %q", p.GetName(),
			ProbeNamePgSysMemoryInfo)
	}
	if p.GetExtensionName() != "system_stats" {
		t.Errorf("GetExtensionName() = %q, want system_stats",
			p.GetExtensionName())
	}
	if p.GetQuery() == "" {
		t.Error("GetQuery() must not be empty")
	}
}

func TestPgSysMemoryInfoProbe_StoreEmpty(t *testing.T) {
	p := newPgSysMemoryInfoProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v, want nil", err)
	}
}

// TestMetricInt64 covers the three signed widths the helper accepts and
// the rejections. The accepted set is exactly what pgx v5 decodes
// PostgreSQL's integer types to: BIGINT to int64, INTEGER to int32 and
// SMALLINT to int16. Everything else, unsigned widths included, must be
// rejected so the caller stores NULL rather than a coerced number.
func TestMetricInt64(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int64
		ok    bool
	}{
		{"int64", int64(42), 42, true},
		{"int32", int32(42), 42, true},
		{"int16", int16(42), 42, true},
		{"negative int64", int64(-1), -1, true},
		{"int8 rejected", int8(42), 0, false},
		{"int rejected", 42, 0, false},
		{"uint64 rejected", uint64(42), 0, false},
		{"uint64 above MaxInt64 rejected", uint64(math.MaxUint64), 0, false},
		{"uint32 rejected", uint32(42), 0, false},
		{"uint rejected", uint(42), 0, false},
		{"float64 rejected", float64(42), 0, false},
		{"float32 rejected", float32(42), 0, false},
		{"numeric rejected", pgtype.Numeric{Int: big.NewInt(42), Valid: true}, 0, false},
		{"string rejected", "42", 0, false},
		{"bool rejected", true, 0, false},
		{"nil rejected", nil, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := metricInt64(tc.input)
			if ok != tc.ok {
				t.Fatalf("metricInt64(%v) ok = %v, want %v",
					tc.input, ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("metricInt64(%v) = %d, want %d",
					tc.input, got, tc.want)
			}
		})
	}
}

// TestEstimateAvailableMemory checks that the estimate is the sum of the
// two inputs when both are present, and NULL (nil) rather than a
// silently wrong zero whenever either is missing or unusable.
func TestEstimateAvailableMemory(t *testing.T) {
	tests := []struct {
		name   string
		metric map[string]any
		want   any
	}{
		{
			name: "both present",
			metric: map[string]any{
				"free_memory": int64(1024),
				"cache_total": int64(2048),
			},
			want: int64(3072),
		},
		{
			name: "both zero",
			metric: map[string]any{
				"free_memory": int64(0),
				"cache_total": int64(0),
			},
			want: int64(0),
		},
		{
			name: "mixed signed integer widths",
			metric: map[string]any{
				"free_memory": int32(10),
				"cache_total": int16(5),
			},
			want: int64(15),
		},
		{
			name: "unsigned input rejected",
			metric: map[string]any{
				"free_memory": uint64(10),
				"cache_total": int64(5),
			},
			want: nil,
		},
		{
			name:   "both missing",
			metric: map[string]any{},
			want:   nil,
		},
		{
			name: "free_memory missing",
			metric: map[string]any{
				"cache_total": int64(2048),
			},
			want: nil,
		},
		{
			name: "cache_total missing",
			metric: map[string]any{
				"free_memory": int64(1024),
			},
			want: nil,
		},
		{
			name: "free_memory NULL",
			metric: map[string]any{
				"free_memory": nil,
				"cache_total": int64(2048),
			},
			want: nil,
		},
		{
			name: "cache_total NULL",
			metric: map[string]any{
				"free_memory": int64(1024),
				"cache_total": nil,
			},
			want: nil,
		},
		{
			name: "free_memory not an integer",
			metric: map[string]any{
				"free_memory": "1024",
				"cache_total": int64(2048),
			},
			want: nil,
		},
		{
			name: "cache_total not an integer",
			metric: map[string]any{
				"free_memory": int64(1024),
				"cache_total": 2048.5,
			},
			want: nil,
		},
		{
			name: "positive overflow",
			metric: map[string]any{
				"free_memory": int64(math.MaxInt64),
				"cache_total": int64(1),
			},
			want: nil,
		},
		{
			name: "negative overflow",
			metric: map[string]any{
				"free_memory": int64(math.MinInt64),
				"cache_total": int64(-1),
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := estimateAvailableMemory(tc.metric)
			if got != tc.want {
				t.Errorf("estimateAvailableMemory(%v) = %v (%T), want %v (%T)",
					tc.metric, got, got, tc.want, tc.want)
			}
		})
	}
}

// TestPgSysMemoryInfoProbe_ExecuteAndStore drives the probe end to end
// against the test double for pg_sys_memory_info() and confirms that the
// estimate is written to the new column.
func TestPgSysMemoryInfoProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgSysMemoryInfoProbeForTest()
	ctx := context.Background()

	metrics, err := p.Execute(ctx, "mem-conn", conn, 16)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("Execute returned %d rows, want 1", len(metrics))
	}

	// The test double reports free_memory 0 and cache_total 0, so
	// override them to a pair that proves the sum is stored rather than
	// a constant.
	metrics[0]["free_memory"] = int64(1024)
	metrics[0]["cache_total"] = int64(2048)

	now := time.Now().UTC()
	if err := p.Store(ctx, conn, 1, now, metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}

	var available *int64
	if err := pool.QueryRow(ctx, `
		SELECT available_memory
		FROM metrics.pg_sys_memory_info
		WHERE connection_id = 1 AND collected_at = $1
	`, now).Scan(&available); err != nil {
		t.Fatalf("read back available_memory: %v", err)
	}
	if available == nil {
		t.Fatal("available_memory stored as NULL, want 3072")
	}
	if *available != 3072 {
		t.Errorf("available_memory = %d, want 3072", *available)
	}
}

// TestPgSysMemoryInfoProbe_StoreNullAvailableMemory confirms that a row
// missing one of the inputs stores SQL NULL rather than zero.
func TestPgSysMemoryInfoProbe_StoreNullAvailableMemory(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgSysMemoryInfoProbeForTest()
	ctx := context.Background()

	now := time.Now().UTC()
	metrics := []map[string]any{{
		"total_memory": int64(8192),
		"free_memory":  int64(1024),
		// cache_total deliberately absent.
	}}
	if err := p.Store(ctx, conn, 2, now, metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}

	var available *int64
	if err := pool.QueryRow(ctx, `
		SELECT available_memory
		FROM metrics.pg_sys_memory_info
		WHERE connection_id = 2 AND collected_at = $1
	`, now).Scan(&available); err != nil {
		t.Fatalf("read back available_memory: %v", err)
	}
	if available != nil {
		t.Errorf("available_memory = %d, want NULL", *available)
	}
}
