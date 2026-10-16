/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// insertDiskRow writes one metrics.pg_sys_disk_info sample. The
// filesystem type is a pointer so a test can distinguish a recorded type
// from the NULL the probe leaves when it could not determine one.
func insertDiskRow(t *testing.T, pool *pgxpool.Pool, connID int,
	mount string, fsType *string, used, total int64, at time.Time) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO metrics.pg_sys_disk_info
		    (connection_id, mount_point, file_system_type,
		     used_space, total_space, collected_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, connID, mount, fsType, used, total, at); err != nil {
		t.Fatalf("failed to insert pg_sys_disk_info row for %s: %v", mount, err)
	}
}

// diskFsType returns a pointer to the given filesystem type, for the
// insertDiskRow calls that record one.
func diskFsType(name string) *string { return &name }

// diskLatestValues returns the latest pg_sys_disk_info.used_percent rows
// for the given connection.
func diskLatestValues(t *testing.T, ds *Datastore, connID int) []MetricValue {
	t.Helper()

	values, err := ds.GetLatestMetricValues(context.Background(),
		"pg_sys_disk_info.used_percent")
	if err != nil {
		t.Fatalf("GetLatestMetricValues failed: %v", err)
	}
	var found []MetricValue
	for _, v := range values {
		if v.ConnectionID == connID {
			found = append(found, v)
		}
	}
	return found
}

// diskHistoricalValues returns the historical
// pg_sys_disk_info.used_percent rows for the given connection over a one
// day lookback.
func diskHistoricalValues(t *testing.T, ds *Datastore, connID int) []HistoricalMetricValue {
	t.Helper()

	values, err := ds.GetHistoricalMetricValues(context.Background(),
		"pg_sys_disk_info.used_percent", 1)
	if err != nil {
		t.Fatalf("GetHistoricalMetricValues failed: %v", err)
	}
	var found []HistoricalMetricValue
	for _, v := range values {
		if v.ConnectionID == connID {
			found = append(found, v)
		}
	}
	return found
}

// TestDiskUsedPercent_ExcludesPseudoFilesystems is the regression test
// for GitHub issue #428. A squashfs image is always 100% used, so before
// the exclusion the MAX across mounts reported 100% on any host with a
// snap installed and the disk alert fired permanently. Both the latest
// and the historical query must report the real ext4 mount's 50%
// instead, and a connection whose only mounts are pseudo filesystems
// must drop out of the result set entirely rather than report a
// fabricated figure.
func TestDiskUsedPercent_ExcludesPseudoFilesystems(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	at := time.Now().UTC().Add(-2 * time.Minute)

	mixed := insertConnection(t, pool, "disk-mixed-mounts")
	insertDiskRow(t, pool, mixed, "/", diskFsType("ext4"), 50, 100, at)
	insertDiskRow(t, pool, mixed, "/snap/core22/1", diskFsType("squashfs"), 100, 100, at)
	insertDiskRow(t, pool, mixed, "/dev/shm", diskFsType("tmpfs"), 90, 100, at)
	insertDiskRow(t, pool, mixed, "/sys/fs/cgroup", diskFsType("cgroup2"), 100, 100, at)

	pseudoOnly := insertConnection(t, pool, "disk-pseudo-only")
	insertDiskRow(t, pool, pseudoOnly, "/run", diskFsType("tmpfs"), 100, 100, at)
	insertDiskRow(t, pool, pseudoOnly, "/proc", diskFsType("proc"), 100, 100, at)

	latest := diskLatestValues(t, ds, mixed)
	if len(latest) != 1 {
		t.Fatalf("latest: expected one row for the mixed connection, got %d: %+v",
			len(latest), latest)
	}
	if latest[0].Value != 50 {
		t.Errorf("latest value = %v, want 50 (the ext4 mount, not the squashfs image)",
			latest[0].Value)
	}
	if rows := diskLatestValues(t, ds, pseudoOnly); len(rows) != 0 {
		t.Errorf("latest: expected no rows for the pseudo-only connection, got %+v", rows)
	}

	historical := diskHistoricalValues(t, ds, mixed)
	if len(historical) != 1 {
		t.Fatalf("historical: expected one row for the mixed connection, got %d: %+v",
			len(historical), historical)
	}
	if historical[0].Value != 50 {
		t.Errorf("historical value = %v, want 50", historical[0].Value)
	}
	if rows := diskHistoricalValues(t, ds, pseudoOnly); len(rows) != 0 {
		t.Errorf("historical: expected no rows for the pseudo-only connection, got %+v", rows)
	}
}

// TestDiskUsedPercent_RetainsNullFilesystemType pins the COALESCE in the
// exclusion predicate. file_system_type is nullable, and a bare NOT IN
// against NULL yields NULL rather than true, which would have silently
// dropped every mount whose type the probe did not record; the mount
// here is the fullest real filesystem, so losing it would understate the
// metric rather than merely lose a row.
func TestDiskUsedPercent_RetainsNullFilesystemType(t *testing.T) {
	ds, pool, cleanup := newHistoricalMetricsTestDatastore(t)
	defer cleanup()

	at := time.Now().UTC().Add(-2 * time.Minute)

	connID := insertConnection(t, pool, "disk-null-fs-type")
	insertDiskRow(t, pool, connID, "/data", nil, 90, 100, at)
	insertDiskRow(t, pool, connID, "/", diskFsType("xfs"), 10, 100, at)
	insertDiskRow(t, pool, connID, "/snap/core22/1", diskFsType("squashfs"), 100, 100, at)

	latest := diskLatestValues(t, ds, connID)
	if len(latest) != 1 {
		t.Fatalf("latest: expected one row, got %d: %+v", len(latest), latest)
	}
	if latest[0].Value != 90 {
		t.Errorf("latest value = %v, want 90 (the untyped mount is retained)",
			latest[0].Value)
	}

	historical := diskHistoricalValues(t, ds, connID)
	if len(historical) != 1 {
		t.Fatalf("historical: expected one row, got %d: %+v", len(historical), historical)
	}
	if historical[0].Value != 90 {
		t.Errorf("historical value = %v, want 90", historical[0].Value)
	}
}

// TestPseudoFilesystemExclusion_Fragment checks the shape of the shared
// fragment and that both disk queries embed it, so neither query can
// lose the predicate or drift to a different list of types without the
// test failing.
func TestPseudoFilesystemExclusion_Fragment(t *testing.T) {
	if !strings.HasPrefix(pseudoFilesystemExclusion,
		"COALESCE(file_system_type, '') NOT IN (") {
		t.Fatalf("fragment does not COALESCE the nullable column: %s",
			pseudoFilesystemExclusion)
	}

	want := []string{
		"tmpfs", "devtmpfs", "devfs", "proc", "sysfs", "cgroup", "cgroup2",
		"overlay", "squashfs", "ramfs", "debugfs", "tracefs", "securityfs",
		"pstore", "autofs", "mqueue", "hugetlbfs", "configfs", "fusectl",
		"binfmt_misc", "efivarfs", "nsfs", "bpf",
	}
	for _, fs := range want {
		if !strings.Contains(pseudoFilesystemExclusion, "'"+fs+"'") {
			t.Errorf("fragment omits filesystem type %q", fs)
		}
	}

	cfg, ok := metricRegistry["pg_sys_disk_info.used_percent"]
	if !ok {
		t.Fatal("pg_sys_disk_info.used_percent missing from the registry")
	}
	if !strings.Contains(cfg.latestSQL, pseudoFilesystemExclusion) {
		t.Error("latestSQL does not embed the pseudo-filesystem exclusion")
	}
	if !strings.Contains(cfg.historicalSQL, pseudoFilesystemExclusion) {
		t.Error("historicalSQL does not embed the pseudo-filesystem exclusion")
	}
}
