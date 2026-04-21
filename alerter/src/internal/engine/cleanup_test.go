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

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// TestCheckAlertResolvedMissingFields tests checkAlertResolved with missing required fields
func TestCheckAlertResolvedMissingFields(t *testing.T) {
	engine := &Engine{}

	tests := []struct {
		name  string
		alert *database.Alert
	}{
		{
			name: "nil metric name",
			alert: &database.Alert{
				ID:             1,
				MetricName:     nil,
				ThresholdValue: float64Ptr(50.0),
				Operator:       strPtr(">"),
			},
		},
		{
			name: "nil threshold value",
			alert: &database.Alert{
				ID:             2,
				MetricName:     strPtr("test_metric"),
				ThresholdValue: nil,
				Operator:       strPtr(">"),
			},
		},
		{
			name: "nil operator",
			alert: &database.Alert{
				ID:             3,
				MetricName:     strPtr("test_metric"),
				ThresholdValue: float64Ptr(50.0),
				Operator:       nil,
			},
		},
		{
			name: "all nil",
			alert: &database.Alert{
				ID:             4,
				MetricName:     nil,
				ThresholdValue: nil,
				Operator:       nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should return early without panic when required fields are nil
			engine.checkAlertResolved(context.TODO(), tt.alert)
		})
	}
}
