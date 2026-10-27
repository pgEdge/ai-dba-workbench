/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/pgedge/ai-workbench/collector/src/probes"
	"github.com/pgedge/ai-workbench/pkg/flagutil"
)

func TestUseColdFrontForMetricsDefaultsOff(t *testing.T) {
	c := NewConfig()
	if c.UseColdFrontForMetrics || c.GetUseColdFrontForMetrics() {
		t.Fatal("use_coldfront_for_metrics is on by default")
	}
}

func TestUseColdFrontForMetricsFromYAML(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want bool
	}{
		{"absent", "datastore:\n  host: h\n", false},
		{"false", "use_coldfront_for_metrics: false\n", false},
		{"true", "use_coldfront_for_metrics: true\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cfg.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			c := NewConfig()
			if err := c.LoadFromFile(path); err != nil {
				t.Fatal(err)
			}
			if c.GetUseColdFrontForMetrics() != tt.want {
				t.Errorf("GetUseColdFrontForMetrics() = %v, want %v", c.GetUseColdFrontForMetrics(), tt.want)
			}
		})
	}
}

// TestUseColdFrontForMetricsFlag checks that the flag overrides the file
// only when it was passed, in both directions.
func TestUseColdFrontForMetricsFlag(t *testing.T) {
	orig := *useColdFrontForMetrics
	t.Cleanup(func() { *useColdFrontForMetrics = orig })

	tests := []struct {
		name     string
		fromFile bool
		flagVal  bool
		passed   bool
		want     bool
	}{
		{"not passed keeps file true", true, false, false, true},
		{"not passed keeps file false", false, true, false, false},
		{"passed true overrides file false", false, true, true, true},
		{"passed false overrides file true", true, false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*useColdFrontForMetrics = tt.flagVal
			c := NewConfig()
			c.UseColdFrontForMetrics = tt.fromFile
			passed := flagutil.Set{}
			if tt.passed {
				passed[flagUseColdFrontForMetrics] = true
			}
			c.ApplyFlags(passed)
			if c.UseColdFrontForMetrics != tt.want {
				t.Errorf("UseColdFrontForMetrics = %v, want %v", c.UseColdFrontForMetrics, tt.want)
			}
		})
	}
}

func TestUseColdFrontForMetricsFlagRegistered(t *testing.T) {
	if f := flag.Lookup(flagUseColdFrontForMetrics); f == nil || f.DefValue != "false" {
		t.Fatalf("flag -%s is not registered with default false: %+v", flagUseColdFrontForMetrics, f)
	}
}

func TestApplyMetricsStorageMode(t *testing.T) {
	t.Cleanup(func() { probes.SetColdFrontMetrics(false) })

	applyMetricsStorageMode(true)
	if !probes.ColdFrontMetricsEnabled() {
		t.Error("applyMetricsStorageMode(true) did not enable ColdFront in the probes")
	}
	applyMetricsStorageMode(false)
	if probes.ColdFrontMetricsEnabled() {
		t.Error("applyMetricsStorageMode(false) left ColdFront enabled")
	}
}

func TestStartGarbageCollector(t *testing.T) {
	ctx := context.Background()

	gc, err := startGarbageCollector(ctx, nil, true)
	if err != nil || gc != nil {
		t.Fatalf("startGarbageCollector under ColdFront = %v, %v; want nil, nil", gc, err)
	}
	// Stopping the collector that was never started is a no-op.
	stopGarbageCollector(gc)

	gc, err = startGarbageCollector(ctx, nil, false)
	if err != nil || gc == nil {
		t.Fatalf("startGarbageCollector in the default mode = %v, %v", gc, err)
	}
	stopGarbageCollector(gc)
}
