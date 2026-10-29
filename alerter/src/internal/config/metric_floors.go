/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package config

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/pgedge/ai-workbench/pkg/anomaly"
)

// MetricFloorConfig is one anomaly.tier1.metric_floors entry. The fields
// are pointers so that an entry can override one floor and keep the
// built-in default for the other; 0 disables a floor.
type MetricFloorConfig struct {
	MinValue  *float64 `yaml:"min_value"`
	MinStdDev *float64 `yaml:"min_stddev"`
}

// MetricFloor holds the resolved absolute floors Tier 1 applies to one
// metric; see anomaly.MetricFloor. The built-in floors, and the reasoning
// behind each, live in pkg/anomaly.
type MetricFloor = anomaly.MetricFloor

// DefaultMetricFloor returns the built-in floors for a metric, or zero
// floors when it has none.
func DefaultMetricFloor(metric string) MetricFloor {
	return anomaly.DefaultMetricFloor(metric)
}

// DefaultMetricFloorNames returns, sorted, the metrics that have
// built-in floors.
func DefaultMetricFloorNames() []string {
	return anomaly.DefaultMetricFloorNames()
}

// MetricFloor returns the floors Tier 1 applies to a metric: the
// built-in default with any anomaly.tier1.metric_floors override laid
// over it field by field.
func (t *Tier1Config) MetricFloor(metric string) MetricFloor {
	floor := DefaultMetricFloor(metric)
	if override, ok := t.MetricFloors[metric]; ok {
		if override.MinValue != nil {
			floor.MinValue = *override.MinValue
		}
		if override.MinStdDev != nil {
			floor.MinStdDev = *override.MinStdDev
		}
	}
	return floor
}

// ScoringSettings returns the Tier 1 settings in the shared pkg/anomaly
// form. MetricFloors holds the effective floors: every built-in floor,
// plus every metric named under anomaly.tier1.metric_floors, each
// resolved through MetricFloor so overrides apply field by field.
func (t *Tier1Config) ScoringSettings() anomaly.ScoringSettings {
	names := DefaultMetricFloorNames()
	floors := make(map[string]MetricFloor, len(names)+len(t.MetricFloors))
	for _, name := range names {
		floors[name] = t.MetricFloor(name)
	}
	for name := range t.MetricFloors {
		floors[name] = t.MetricFloor(name)
	}
	return anomaly.ScoringSettings{
		Warmup:             t.Warmup,
		VarianceFloor:      t.VarianceFloor,
		MaxZScore:          t.MaxZScore,
		DefaultSensitivity: t.DefaultSensitivity,
		MetricFloors:       floors,
	}
}

// validateMetricFloors checks every anomaly.tier1.metric_floors entry,
// in metric-name order so that the error reported is deterministic.
func validateMetricFloors(floors map[string]MetricFloorConfig) error {
	names := make([]string, 0, len(floors))
	for name := range floors {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, metric := range names {
		if strings.TrimSpace(metric) == "" {
			return fmt.Errorf("anomaly.tier1.metric_floors keys must be non-empty metric names")
		}
		floor := floors[metric]
		if err := checkFloorValue(metric, "min_value", floor.MinValue); err != nil {
			return err
		}
		if err := checkFloorValue(metric, "min_stddev", floor.MinStdDev); err != nil {
			return err
		}
	}
	return nil
}

// checkFloorValue rejects a set floor that is negative, NaN or infinite.
func checkFloorValue(metric, field string, v *float64) error {
	if v == nil {
		return nil
	}
	if *v < 0 || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return fmt.Errorf(
			"anomaly.tier1.metric_floors.%s.%s must be a finite non-negative number",
			metric, field)
	}
	return nil
}
