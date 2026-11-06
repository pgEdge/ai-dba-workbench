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
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

// enableColdFront switches the package to ColdFront mode for the rest
// of the test.
func enableColdFront(t *testing.T) {
	t.Helper()
	SetColdFrontMetrics(true)
	t.Cleanup(func() { SetColdFrontMetrics(false) })
}

func TestColdFrontMetricsDefaultsOff(t *testing.T) {
	if ColdFrontMetricsEnabled() {
		t.Fatal("ColdFront mode is on by default")
	}
	enableColdFront(t)
	if !ColdFrontMetricsEnabled() {
		t.Fatal("SetColdFrontMetrics(true) did not take effect")
	}
}

func TestEnsurePartitionNoOpUnderColdFront(t *testing.T) {
	enableColdFront(t)
	q := &fakeQuerier{}
	if err := EnsurePartition(context.Background(), q, "pg_stat_activity", time.Now()); err != nil {
		t.Fatalf("EnsurePartition() error = %v", err)
	}
	if len(q.queries) != 0 || len(q.execs) != 0 {
		t.Errorf("EnsurePartition issued %d queries and %d statements under ColdFront", len(q.queries), len(q.execs))
	}
}

func TestDropExpiredPartitionsNoOpUnderColdFront(t *testing.T) {
	enableColdFront(t)
	q := &fakeQuerier{}
	dropped, err := DropExpiredPartitions(context.Background(), q, "pg_settings", 1)
	if err != nil || dropped != 0 {
		t.Fatalf("DropExpiredPartitions() = %d, %v; want 0, nil", dropped, err)
	}
	if len(q.queries) != 0 || len(q.execs) != 0 {
		t.Errorf("DropExpiredPartitions issued %d queries and %d statements under ColdFront", len(q.queries), len(q.execs))
	}
}

// TestPartitionHelpersRunInDefaultMode guards the default path: the
// helpers still query the catalog when ColdFront is off.
func TestPartitionHelpersRunInDefaultMode(t *testing.T) {
	q := &fakeQuerier{}
	if err := EnsurePartition(context.Background(), q, "pg_stat_activity", time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(q.queries) == 0 || len(q.execs) == 0 {
		t.Error("EnsurePartition did not check for and create the partition in the default mode")
	}
	q = &fakeQuerier{}
	if _, err := DropExpiredPartitions(context.Background(), q, "pg_stat_activity", 7); err != nil {
		t.Fatal(err)
	}
	if len(q.queries) == 0 {
		t.Error("DropExpiredPartitions issued nothing in the default mode")
	}
}

func TestConvertForColdFront(t *testing.T) {
	host := netip.MustParsePrefix("192.0.2.10/32")
	network := netip.MustParsePrefix("198.51.100.0/24")

	tests := []struct {
		name    string
		table   string
		columns []string
		values  [][]any
		want    [][]any
		same    bool
	}{
		{
			name:    "inet becomes text",
			table:   "pg_stat_activity",
			columns: []string{"connection_id", "client_addr", "datid"},
			values:  [][]any{{1, host, uint32(5)}, {1, network, nil}, {1, nil, nil}},
			want:    [][]any{{1, "192.0.2.10", uint32(5)}, {1, "198.51.100.0/24", nil}, {1, nil, nil}},
		},
		{
			name:    "char becomes text",
			table:   "pg_database",
			columns: []string{"datname", "datlocprovider", "datfrozenxid"},
			values:  [][]any{{"postgres", 'c', uint32(700)}, {"template0", rune(0), uint32(1)}},
			want:    [][]any{{"postgres", "c", uint32(700)}, {"template0", "", uint32(1)}},
		},
		{
			name:    "replication client_addr",
			table:   "pg_stat_replication",
			columns: []string{"client_addr"},
			values:  [][]any{{host}},
			want:    [][]any{{"192.0.2.10"}},
		},
		{
			name:    "no mapped column leaves rows alone",
			table:   "pg_stat_wal",
			columns: []string{"connection_id", "wal_bytes"},
			values:  [][]any{{1, "12"}},
			want:    [][]any{{1, "12"}},
			same:    true,
		},
		{
			name:    "short row is not indexed past its end",
			table:   "pg_stat_activity",
			columns: []string{"connection_id", "client_addr"},
			values:  [][]any{{1}},
			want:    [][]any{{1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := make([][]any, len(tt.values))
			for i, r := range tt.values {
				original[i] = append([]any(nil), r...)
			}

			got, err := convertForColdFront(tt.table, tt.columns, tt.values)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertForColdFront() = %#v, want %#v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.values, original) {
				t.Error("convertForColdFront modified the caller's rows")
			}
			if tt.same && len(got) > 0 && &got[0] != &tt.values[0] {
				t.Error("rows were copied although no column needed a conversion")
			}
		})
	}
}

func TestConvertForColdFrontUnknownTable(t *testing.T) {
	_, err := convertForColdFront("not_a_table", []string{"a"}, [][]any{{1}})
	if err == nil || !strings.Contains(err.Error(), "no ColdFront column specification") {
		t.Errorf("convertForColdFront() error = %v", err)
	}
}

// TestStoreMetricsColdFrontUnknownTable checks that StoreMetrics
// converts before it touches the connection: an unknown table fails
// without one.
func TestStoreMetricsColdFrontUnknownTable(t *testing.T) {
	enableColdFront(t)
	err := StoreMetrics(context.Background(), nil, "not_a_table", []string{"a"}, [][]any{{1}})
	if err == nil || !strings.Contains(err.Error(), "no ColdFront column specification") {
		t.Errorf("StoreMetrics() error = %v", err)
	}
}
