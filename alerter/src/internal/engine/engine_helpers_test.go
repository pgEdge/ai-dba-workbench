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
	"sync"
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
)

// stderrCaptureMu serializes access to os.Stderr when tests replace it with a
// pipe. Without this guard, parallel tests that each mutate os.Stderr can
// interleave writes or race on the restore, leading to flaky output.
var stderrCaptureMu sync.Mutex

// captureStderr runs fn while redirecting os.Stderr to a pipe, and returns
// the captured output. It acquires a package-level mutex so concurrent
// tests cannot clobber each other's stderr redirection. Cleanup (closing
// both pipe ends, restoring os.Stderr, and releasing the mutex) is
// registered via t.Cleanup so the original state is always restored even
// if fn panics or the caller fails the test.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	stderrCaptureMu.Lock()
	mutexReleased := false
	releaseMutex := func() {
		if !mutexReleased {
			mutexReleased = true
			stderrCaptureMu.Unlock()
		}
	}

	r, w, err := os.Pipe()
	if err != nil {
		releaseMutex()
		t.Fatalf("os.Pipe failed: %v", err)
	}

	oldStderr := os.Stderr
	os.Stderr = w

	writerClosed := false
	closeWriter := func() {
		if !writerClosed {
			writerClosed = true
			if cerr := w.Close(); cerr != nil {
				t.Errorf("failed to close stderr pipe writer: %v", cerr)
			}
		}
	}

	readerClosed := false
	closeReader := func() {
		if !readerClosed {
			readerClosed = true
			if cerr := r.Close(); cerr != nil {
				t.Errorf("failed to close stderr pipe reader: %v", cerr)
			}
		}
	}

	t.Cleanup(func() {
		os.Stderr = oldStderr
		closeWriter()
		closeReader()
		releaseMutex()
	})

	fn()

	// Close the writer before reading so ReadFrom observes EOF.
	closeWriter()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("failed to read captured stderr: %v", err)
	}
	return buf.String()
}

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

	output := captureStderr(t, func() {
		engine.log("test message %d", 42)
	})

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

		output := captureStderr(t, func() {
			engine.debugLog("debug message %s", "test")
		})

		if !strings.Contains(output, "debug message test") {
			t.Error("debugLog should output message when debug is true")
		}
	})

	t.Run("debug disabled", func(t *testing.T) {
		engine := &Engine{debug: false}

		output := captureStderr(t, func() {
			engine.debugLog("debug message %s", "test")
		})

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
