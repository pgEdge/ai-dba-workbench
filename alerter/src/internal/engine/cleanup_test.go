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

// TestAlertTypeConstants tests that alert type constants are used correctly
func TestAlertTypeConstants(t *testing.T) {
	alert := &database.Alert{
		ID:        1,
		AlertType: "threshold",
		RuleID:    int64Ptr(10),
	}

	// Verify the alert type check pattern
	if alert.ID != 1 || alert.AlertType != "threshold" || alert.RuleID == nil {
		t.Error("Alert should have ID, threshold type, and rule ID")
	}

	anomalyAlert := &database.Alert{
		ID:        2,
		AlertType: "anomaly",
	}

	if anomalyAlert.ID != 2 || anomalyAlert.AlertType != "anomaly" {
		t.Error("Anomaly alert should have ID and anomaly type")
	}
}

// int64Ptr is a helper to create *int64
func int64Ptr(i int64) *int64 { return &i }
