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
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
)

// TestNewEngineWithNilParams tests NewEngine with nil config and datastore
func TestNewEngineWithNilParams(t *testing.T) {
	tests := []struct {
		name  string
		debug bool
	}{
		{"debug off", false},
		{"debug on", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(nil, nil, tt.debug)

			if engine == nil {
				t.Fatal("NewEngine returned nil")
			}

			if engine.debug != tt.debug {
				t.Errorf("engine.debug = %v, expected %v", engine.debug, tt.debug)
			}

			if engine.config != nil {
				t.Error("engine.config should be nil")
			}

			if engine.datastore != nil {
				t.Error("engine.datastore should be nil")
			}

			if engine.notificationMgr != nil {
				t.Error("engine.notificationMgr should be nil without config")
			}

			if engine.notificationPool != nil {
				t.Error("engine.notificationPool should be nil without notification manager")
			}
		})
	}
}

// TestNewEngineWithConfig tests NewEngine with a minimal config but no datastore
func TestNewEngineWithConfig(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Anomaly.Enabled = false // Disable anomaly detection
	cfg.Anomaly.Tier2.Enabled = false
	cfg.Anomaly.Tier3.Enabled = false

	engine := NewEngine(cfg, nil, false)

	if engine == nil {
		t.Fatal("NewEngine returned nil")
	}

	if engine.config != cfg {
		t.Error("engine.config should match provided config")
	}

	// Notification manager should still be nil because datastore is nil
	if engine.notificationMgr != nil {
		t.Error("engine.notificationMgr should be nil without datastore")
	}
}

// TestReloadConfigMethod tests the ReloadConfig method
func TestReloadConfigMethod(t *testing.T) {
	engine := NewEngine(nil, nil, false)

	if engine.config != nil {
		t.Error("initial config should be nil")
	}

	// Reload with a new config
	newCfg := config.NewConfig()
	engine.ReloadConfig(newCfg)

	if engine.config != newCfg {
		t.Error("config should be updated after ReloadConfig")
	}

	// Reload with nil should set config to nil
	engine.ReloadConfig(nil)

	if engine.config != nil {
		t.Error("config should be nil after ReloadConfig(nil)")
	}
}

// TestGetConfigMethod tests the getConfig method returns correct config
func TestGetConfigMethod(t *testing.T) {
	cfg := config.NewConfig()
	engine := NewEngine(cfg, nil, false)

	retrievedCfg := engine.getConfig()

	if retrievedCfg != cfg {
		t.Error("getConfig should return the current config")
	}

	// Reload and verify getConfig returns new config
	newCfg := config.NewConfig()
	newCfg.Threshold.EvaluationIntervalSeconds = 120
	engine.ReloadConfig(newCfg)

	retrievedCfg = engine.getConfig()
	if retrievedCfg != newCfg {
		t.Error("getConfig should return updated config after ReloadConfig")
	}
}

// TestStopNotificationWorkersNilPool tests StopNotificationWorkers with nil pool
func TestStopNotificationWorkersNilPool(t *testing.T) {
	engine := &Engine{
		notificationPool: nil,
	}

	// Should not panic
	engine.StopNotificationWorkers()
}

// TestLogMethod tests the log method outputs to stderr
func TestLogMethod(t *testing.T) {
	engine := &Engine{}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	engine.log("test message %d", 42)

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "[alerter]") {
		t.Error("log output should contain [alerter] prefix")
	}

	if !strings.Contains(output, "test message 42") {
		t.Error("log output should contain formatted message")
	}
}

// TestDebugLogMethod tests the debugLog method respects debug flag
func TestDebugLogMethod(t *testing.T) {
	t.Run("debug enabled", func(t *testing.T) {
		engine := &Engine{debug: true}

		// Capture stderr
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		engine.debugLog("debug message %s", "test")

		w.Close()
		os.Stderr = oldStderr

		var buf bytes.Buffer
		buf.ReadFrom(r)
		output := buf.String()

		if !strings.Contains(output, "debug message test") {
			t.Error("debugLog should output message when debug is true")
		}
	})

	t.Run("debug disabled", func(t *testing.T) {
		engine := &Engine{debug: false}

		// Capture stderr
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		engine.debugLog("debug message %s", "test")

		w.Close()
		os.Stderr = oldStderr

		var buf bytes.Buffer
		buf.ReadFrom(r)
		output := buf.String()

		if strings.Contains(output, "debug message test") {
			t.Error("debugLog should not output message when debug is false")
		}
	})
}

// TestEngineConstants tests that engine constants have expected values
func TestEngineConstants(t *testing.T) {
	// Verify constants are set to reasonable values
	if NotificationWorkerPoolSize <= 0 {
		t.Error("NotificationWorkerPoolSize should be positive")
	}

	if NotificationTimeout <= 0 {
		t.Error("NotificationTimeout should be positive")
	}

	if BlackoutCheckInterval <= 0 {
		t.Error("BlackoutCheckInterval should be positive")
	}

	if AlertCleanerInterval <= 0 {
		t.Error("AlertCleanerInterval should be positive")
	}

	if RetentionCleanupInterval <= 0 {
		t.Error("RetentionCleanupInterval should be positive")
	}

	if DefaultNotificationProcessInterval <= 0 {
		t.Error("DefaultNotificationProcessInterval should be positive")
	}

	if DefaultReminderCheckInterval <= 0 {
		t.Error("DefaultReminderCheckInterval should be positive")
	}

	if DefaultTier3Timeout <= 0 {
		t.Error("DefaultTier3Timeout should be positive")
	}

	if AlertCooldownPeriod <= 0 {
		t.Error("AlertCooldownPeriod should be positive")
	}

	if ReevaluationSuppressionPeriod <= 0 {
		t.Error("ReevaluationSuppressionPeriod should be positive")
	}
}

// TestEngineFieldsAfterConstruction verifies engine fields are properly initialized
func TestEngineFieldsAfterConstruction(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
		debug  bool
	}{
		{"nil config debug off", nil, false},
		{"nil config debug on", nil, true},
		{"with config debug off", config.NewConfig(), false},
		{"with config debug on", config.NewConfig(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(tt.config, nil, tt.debug)

			if engine.debug != tt.debug {
				t.Errorf("debug = %v, expected %v", engine.debug, tt.debug)
			}

			// Verify config is set correctly
			if tt.config == nil && engine.config != nil {
				t.Error("config should be nil when passed nil")
			}
			if tt.config != nil && engine.config != tt.config {
				t.Error("config should match passed config")
			}
		})
	}
}
