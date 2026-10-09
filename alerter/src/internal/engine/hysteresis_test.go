/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package engine

import (
	"context"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// sampleAt returns a fixed instant offset by the given number of minutes,
// so the tests can name distinct samples without depending on the clock.
func sampleAt(minute int) time.Time {
	return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC).Add(
		time.Duration(minute) * time.Minute)
}

// TestSampleStreaks_Observe covers the counting rules: a new sample
// advances the count, the same or an older sample leaves it alone, and a
// judgement that the condition does not hold resets it, even on a sample
// already counted.
func TestSampleStreaks_Observe(t *testing.T) {
	steps := []struct {
		label  string
		sample int
		holds  bool
		want   int
	}{
		{"first sample", 0, true, 1},
		{"same sample read again", 0, true, 1},
		{"next sample", 1, true, 2},
		{"older sample", 0, true, 2},
		{"another new sample", 2, true, 3},
		{"condition stops holding on the same sample", 2, false, 0},
		{"condition holds again", 3, true, 1},
	}

	var streaks sampleStreaks[string]
	for _, step := range steps {
		got := streaks.observe("k", sampleAt(step.sample), step.holds)
		if got != step.want {
			t.Errorf("%s: observe = %d, want %d", step.label, got, step.want)
		}
		if c := streaks.count("k"); c != step.want {
			t.Errorf("%s: count = %d, want %d", step.label, c, step.want)
		}
	}
}

// TestSampleStreaks_ObserveFalseOnZeroValue proves the zero value is
// usable for a reset before anything has been counted.
func TestSampleStreaks_ObserveFalseOnZeroValue(t *testing.T) {
	var streaks sampleStreaks[int64]
	if got := streaks.observe(1, sampleAt(0), false); got != 0 {
		t.Errorf("observe(false) on an empty set = %d, want 0", got)
	}
	if streaks.size() != 0 {
		t.Errorf("size = %d, want 0", streaks.size())
	}
	if c := streaks.count(1); c != 0 {
		t.Errorf("count of an unknown key = %d, want 0", c)
	}
}

// TestSampleStreaks_Sweep covers the pass-based pruning the evaluator
// uses: a key the current pass did not observe is dropped unless the
// keep function asks for it, whilst observed keys always survive.
func TestSampleStreaks_Sweep(t *testing.T) {
	var streaks sampleStreaks[string]

	streaks.startPass()
	streaks.observe("seen", sampleAt(0), true)
	streaks.observe("unseen", sampleAt(0), true)
	streaks.observe("kept", sampleAt(0), true)

	streaks.startPass()
	streaks.observe("seen", sampleAt(1), true)
	streaks.sweep(func(key string) bool { return key == "kept" })

	if c := streaks.count("seen"); c != 2 {
		t.Errorf("observed key count = %d, want 2", c)
	}
	if c := streaks.count("unseen"); c != 0 {
		t.Errorf("unobserved key count = %d, want 0 (swept)", c)
	}
	if c := streaks.count("kept"); c != 1 {
		t.Errorf("kept key count = %d, want 1", c)
	}

	// A nil keep function sweeps every unobserved key.
	streaks.startPass()
	streaks.sweep(nil)
	if streaks.size() != 0 {
		t.Errorf("size after sweep(nil) = %d, want 0", streaks.size())
	}
}

// TestSampleStreaks_Retain covers the cleaner's pruning, which ignores
// passes and keeps exactly the keys the predicate accepts.
func TestSampleStreaks_Retain(t *testing.T) {
	var streaks sampleStreaks[int64]
	streaks.observe(1, sampleAt(0), true)
	streaks.observe(2, sampleAt(0), true)
	streaks.observe(2, sampleAt(1), true)

	streaks.retain(func(id int64) bool { return id == 2 })

	if c := streaks.count(1); c != 0 {
		t.Errorf("dropped key count = %d, want 0", c)
	}
	if c := streaks.count(2); c != 2 {
		t.Errorf("retained key count = %d, want 2", c)
	}
	if streaks.size() != 1 {
		t.Errorf("size = %d, want 1", streaks.size())
	}
}

// TestNewThresholdSampleKey checks that the key distinguishes every part
// of a metric value's identity, including a nil name from an empty one.
func TestNewThresholdSampleKey(t *testing.T) {
	empty := ""
	db := "appdb"
	obj := "public.orders"

	base := newThresholdSampleKey(7, database.MetricValue{ConnectionID: 3})
	if base != (thresholdSampleKey{ruleID: 7, connectionID: 3}) {
		t.Errorf("key with no names = %+v", base)
	}

	full := newThresholdSampleKey(7, database.MetricValue{
		ConnectionID: 3, DatabaseName: &db, ObjectName: &obj,
	})
	want := thresholdSampleKey{
		ruleID: 7, connectionID: 3,
		databaseName: db, hasDatabase: true,
		objectName: obj, hasObject: true,
	}
	if full != want {
		t.Errorf("key with names = %+v, want %+v", full, want)
	}

	emptyNames := newThresholdSampleKey(7, database.MetricValue{
		ConnectionID: 3, DatabaseName: &empty, ObjectName: &empty,
	})
	if emptyNames == base {
		t.Error("empty names must not collide with nil names")
	}
	if newThresholdSampleKey(8, database.MetricValue{ConnectionID: 3}) == base {
		t.Error("different rules must not share a key")
	}
	if newThresholdSampleKey(7, database.MetricValue{ConnectionID: 4}) == base {
		t.Error("different connections must not share a key")
	}
}

// TestThresholdCounts covers the configuration lookups, including the
// fallback to acting on a single sample with no configuration or a value
// that Validate would have rejected.
func TestThresholdCounts(t *testing.T) {
	withCounts := func(trigger, clear int) *config.Config {
		cfg := config.NewConfig()
		cfg.Threshold.TriggerCount = trigger
		cfg.Threshold.ClearCount = clear
		return cfg
	}

	tests := []struct {
		name        string
		cfg         *config.Config
		wantTrigger int
		wantClear   int
	}{
		{"no configuration", nil, 1, 1},
		{"defaults", config.NewConfig(),
			config.DefaultThresholdTriggerCount, config.DefaultThresholdClearCount},
		{"explicit counts", withCounts(4, 5), 4, 5},
		{"single sample", withCounts(1, 1), 1, 1},
		{"zero falls back", withCounts(0, 0), 1, 1},
		{"negative falls back", withCounts(-2, -3), 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Engine{config: tt.cfg}
			if got := e.thresholdTriggerCount(); got != tt.wantTrigger {
				t.Errorf("thresholdTriggerCount = %d, want %d", got, tt.wantTrigger)
			}
			if got := e.thresholdClearCount(); got != tt.wantClear {
				t.Errorf("thresholdClearCount = %d, want %d", got, tt.wantClear)
			}
		})
	}
}

// TestRecordResolvedSample_BelowClearCount covers the path that leaves
// the alert active: with clear_count 3, two distinct resolved samples are
// counted and nothing is cleared, which the nil datastore would turn into
// a panic. A violating sample then resets the count.
func TestRecordResolvedSample_BelowClearCount(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Threshold.ClearCount = 3
	e := &Engine{config: cfg}
	alert := &database.Alert{ID: 42}
	ctx := context.Background()

	e.recordResolvedSample(ctx, alert, sampleAt(0), 10)
	e.recordResolvedSample(ctx, alert, sampleAt(0), 10)
	e.recordResolvedSample(ctx, alert, sampleAt(1), 10)
	if c := e.clearStreaks.count(alert.ID); c != 2 {
		t.Errorf("clear count = %d, want 2", c)
	}

	e.recordViolatingSample(alert, sampleAt(2))
	if c := e.clearStreaks.count(alert.ID); c != 0 {
		t.Errorf("clear count after a violating sample = %d, want 0", c)
	}
}
