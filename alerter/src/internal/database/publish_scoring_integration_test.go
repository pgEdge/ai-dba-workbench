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
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/anomaly"
)

// TestPublishScoringSettings_RoundTrip publishes settings and reads the
// JSON back, then publishes again to prove the row is overwritten.
func TestPublishScoringSettings_RoundTrip(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()
	ctx := context.Background()

	var isNull bool
	if err := pool.QueryRow(ctx,
		`SELECT tier1_scoring IS NULL FROM alerter_settings WHERE id = 1`).Scan(&isNull); err != nil {
		t.Fatalf("failed to read the settings row: %v", err)
	}
	if !isNull {
		t.Fatal("tier1_scoring should start NULL")
	}

	want := anomaly.DefaultScoringSettings()
	want.MaxZScore = 7.5
	want.MetricFloors = map[string]anomaly.MetricFloor{
		"connection_count": {MinValue: 5, MinStdDev: 1.5},
	}
	for run := 0; run < 2; run++ {
		if err := ds.PublishScoringSettings(ctx, want); err != nil {
			t.Fatalf("PublishScoringSettings run %d failed: %v", run, err)
		}
		var raw []byte
		if err := pool.QueryRow(ctx,
			`SELECT tier1_scoring FROM alerter_settings WHERE id = 1`).Scan(&raw); err != nil {
			t.Fatalf("failed to read tier1_scoring: %v", err)
		}
		var got anomaly.ScoringSettings
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("stored JSON does not decode: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("round trip = %+v, want %+v", got, want)
		}
	}
}

// TestPublishScoringSettings_ReportsFailure drives the error path by
// removing the table.
func TestPublishScoringSettings_ReportsFailure(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE alerter_settings`); err != nil {
		t.Fatalf("failed to drop alerter_settings: %v", err)
	}
	err := ds.PublishScoringSettings(ctx, anomaly.DefaultScoringSettings())
	if err == nil || !strings.Contains(err.Error(), "failed to publish scoring settings") {
		t.Errorf("error = %v, want a publish failure", err)
	}
}

// TestPublishScoringSettings_RejectsUnencodableSettings verifies a value
// JSON cannot carry is reported rather than stored.
func TestPublishScoringSettings_RejectsUnencodableSettings(t *testing.T) {
	ds := &Datastore{}
	settings := anomaly.DefaultScoringSettings()
	settings.MaxZScore = math.NaN()
	err := ds.PublishScoringSettings(context.Background(), settings)
	if err == nil || !strings.Contains(err.Error(), "failed to encode scoring settings") {
		t.Errorf("error = %v, want an encoding failure", err)
	}
}
