/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package engine provides the core alerter engine that runs background
// processing for threshold evaluation, anomaly detection, and baseline
// calculation.
package engine

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/alerter/internal/llm"
	"github.com/pgedge/ai-workbench/alerter/internal/notifications"
	"github.com/pgedge/ai-workbench/pkg/worker"
)

// NotificationWorkerPoolSize is the maximum number of concurrent notification goroutines.
// This bounds resource usage when many alerts fire simultaneously.
const NotificationWorkerPoolSize = 10

// Time interval constants for various background workers.
// These provide readable names for hardcoded durations and make tuning easier.
const (
	// NotificationTimeout is the maximum time allowed for sending a notification.
	NotificationTimeout = 30 * time.Second

	// BlackoutCheckInterval is how often the scheduler checks for scheduled blackouts.
	BlackoutCheckInterval = 1 * time.Minute

	// AlertCleanerInterval is how often resolved alerts are checked and cleared.
	AlertCleanerInterval = 30 * time.Second

	// RetentionCleanupInterval is how often old data is cleaned up.
	RetentionCleanupInterval = 24 * time.Hour

	// DefaultNotificationProcessInterval is the fallback interval for processing pending notifications.
	DefaultNotificationProcessInterval = 30 * time.Second

	// DefaultReminderCheckInterval is the fallback interval for checking reminder notifications.
	DefaultReminderCheckInterval = 60 * time.Minute

	// DefaultTier3Timeout is the fallback timeout for Tier 3 LLM classification.
	DefaultTier3Timeout = 30 * time.Second

	// AlertCooldownPeriod prevents flapping by suppressing new alerts for a
	// rule+connection+database combination that was recently cleared.
	AlertCooldownPeriod = 5 * time.Minute

	// ReevaluationSuppressionPeriod is how long to suppress anomaly alerts
	// after re-evaluation clears them based on user feedback. This is longer
	// than the standard cooldown to respect the user's assessment.
	ReevaluationSuppressionPeriod = 24 * time.Hour
)

// Engine is the main alerter engine that coordinates all background processing
type Engine struct {
	config          *config.Config
	datastore       *database.Datastore
	notificationMgr *notifications.Manager
	debug           bool

	// LLM providers for Tier 2/3 anomaly detection
	embeddingProvider llm.EmbeddingProvider
	reasoningProvider llm.ReasoningProvider

	// Notification worker pool using generic WorkerPool abstraction
	notificationPool *worker.WorkerPool[notificationJob]

	// ctx is the engine-wide context set during Run, used for
	// background operations such as notification job processing.
	ctx context.Context

	// Synchronization
	mu sync.RWMutex
}

// NewEngine creates a new alerter engine
func NewEngine(cfg *config.Config, datastore *database.Datastore, debug bool) *Engine {
	e := &Engine{
		config:    cfg,
		datastore: datastore,
		debug:     debug,
	}

	// Initialize notification manager (only if config and datastore are provided)
	if cfg != nil && datastore != nil {
		notificationMgr, err := notifications.NewManager(datastore, &cfg.Notifications, debug, e.log)
		if err != nil {
			// Log warning but don't fail - notifications are optional
			e.log("WARNING: Failed to initialize notification manager: %v", err)
		}
		e.notificationMgr = notificationMgr
	}

	// Initialize LLM providers for Tier 2/3 anomaly detection
	if cfg != nil {
		e.initLLMProviders()
	}

	// Create and start notification worker pool only when notifications are enabled.
	if e.notificationMgr != nil {
		e.notificationPool = worker.NewWorkerPool(
			NotificationWorkerPoolSize,
			100, // Buffer for burst handling
			e.processNotificationJob,
		)
		e.notificationPool.Start()
		e.log("Started %d notification workers", NotificationWorkerPoolSize)
	}

	return e
}

// initLLMProviders initializes the LLM providers based on configuration
func (e *Engine) initLLMProviders() {
	// Initialize embedding provider for Tier 2
	if e.config.Anomaly.Tier2.Enabled {
		provider, err := llm.NewEmbeddingProvider(e.config)
		if err != nil {
			e.log("WARNING: Failed to initialize embedding provider: %v", err)
			e.log("Tier 2 embedding similarity will be disabled")
		} else if provider != nil {
			e.embeddingProvider = provider
			e.log("Initialized embedding provider: %s", provider.ModelName())
		} else {
			e.log("No embedding provider configured, Tier 2 will be disabled")
		}
	}

	// Initialize reasoning provider for Tier 3
	if e.config.Anomaly.Tier3.Enabled {
		provider, err := llm.NewReasoningProvider(e.config)
		if err != nil {
			e.log("WARNING: Failed to initialize reasoning provider: %v", err)
			e.log("Tier 3 LLM classification will be disabled")
		} else if provider != nil {
			e.reasoningProvider = provider
			e.log("Initialized reasoning provider: %s", provider.ModelName())
		} else {
			e.log("No reasoning provider configured, Tier 3 will be disabled")
		}
	}

	// If anomaly detection is enabled but no LLM providers initialized
	// successfully, disable anomaly detection entirely. Without AI to
	// filter anomalies, raw statistical detection would generate too
	// much noise.
	if e.config.Anomaly.Enabled && e.embeddingProvider == nil && e.reasoningProvider == nil {
		e.config.Anomaly.Enabled = false
		e.log("Anomaly detection auto-disabled: no LLM providers available")
	}
}

// Run starts the engine and runs until the context is canceled
func (e *Engine) Run(ctx context.Context) error {
	e.ctx = ctx
	e.log("Engine starting...")

	// Start all background workers
	var wg sync.WaitGroup

	// Threshold evaluator
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.runThresholdEvaluator(ctx)
	}()

	// Baseline calculator
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.runBaselineCalculator(ctx)
	}()

	// Anomaly detector (if enabled)
	if e.config.Anomaly.Enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.runAnomalyDetector(ctx)
		}()

		// Re-evaluation worker (if enabled and reasoning provider available)
		if e.config.Anomaly.Reevaluation.Enabled && e.reasoningProvider != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				e.runReevaluationWorker(ctx)
			}()
		}
	}

	// Blackout scheduler
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.runBlackoutScheduler(ctx)
	}()

	// Connection error evaluator
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.runConnectionErrorEvaluator(ctx)
	}()

	// Alert cleaner (auto-clear resolved alerts)
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.runAlertCleaner(ctx)
	}()

	// Retention manager (cleanup old data)
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.runRetentionManager(ctx)
	}()

	// Start notification workers
	if e.notificationMgr != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.runNotificationWorker(ctx)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			e.runReminderWorker(ctx)
		}()
	}

	e.log("All workers started")

	// Wait for shutdown
	<-ctx.Done()
	e.log("Shutdown signal received, stopping workers...")

	// Wait for all workers to finish
	wg.Wait()

	// Stop notification worker pool
	e.StopNotificationWorkers()

	e.log("All workers stopped")
	return nil
}

// ReloadConfig updates the engine configuration
func (e *Engine) ReloadConfig(cfg *config.Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = cfg
	e.log("Configuration reloaded")
}

// getConfig returns the current configuration with proper read locking.
// All background workers must use this method instead of accessing
// e.config directly to avoid data races with ReloadConfig.
func (e *Engine) getConfig() *config.Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

// StopNotificationWorkers gracefully shuts down the notification worker pool
func (e *Engine) StopNotificationWorkers() {
	if e.notificationPool == nil {
		return
	}
	e.notificationPool.Stop()
	e.log("Notification workers stopped")
}

// runThresholdEvaluator evaluates threshold-based alert rules
func (e *Engine) runThresholdEvaluator(ctx context.Context) {
	cfg := e.getConfig()
	interval := time.Duration(cfg.Threshold.EvaluationIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.log("Threshold evaluator started (interval: %v)", interval)

	// Run immediately on start
	e.evaluateThresholds(ctx)

	for {
		select {
		case <-ctx.Done():
			e.log("Threshold evaluator stopping")
			return
		case <-ticker.C:
			e.evaluateThresholds(ctx)

			newCfg := e.getConfig()
			newInterval := time.Duration(newCfg.Threshold.EvaluationIntervalSeconds) * time.Second
			if newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				e.log("Threshold evaluator interval updated to %v", interval)
			}
		}
	}
}

// runBaselineCalculator periodically recalculates metric baselines
func (e *Engine) runBaselineCalculator(ctx context.Context) {
	cfg := e.getConfig()
	interval := time.Duration(cfg.Baselines.RefreshIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.log("Baseline calculator started (interval: %v)", interval)

	// Run immediately on start
	e.calculateBaselines(ctx)

	for {
		select {
		case <-ctx.Done():
			e.log("Baseline calculator stopping")
			return
		case <-ticker.C:
			e.calculateBaselines(ctx)

			newCfg := e.getConfig()
			newInterval := time.Duration(newCfg.Baselines.RefreshIntervalSeconds) * time.Second
			if newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				e.log("Baseline calculator interval updated to %v", interval)
			}
		}
	}
}

// runAnomalyDetector runs the tiered anomaly detection system
func (e *Engine) runAnomalyDetector(ctx context.Context) {
	cfg := e.getConfig()
	interval := time.Duration(cfg.Anomaly.Tier1.EvaluationIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.log("Anomaly detector started (interval: %v)", interval)

	for {
		select {
		case <-ctx.Done():
			e.log("Anomaly detector stopping")
			return
		case <-ticker.C:
			e.detectAnomalies(ctx)

			newCfg := e.getConfig()
			newInterval := time.Duration(newCfg.Anomaly.Tier1.EvaluationIntervalSeconds) * time.Second
			if newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				e.log("Anomaly detector interval updated to %v", interval)
			}
		}
	}
}

// runBlackoutScheduler checks and activates scheduled blackouts
func (e *Engine) runBlackoutScheduler(ctx context.Context) {
	// Check for scheduled blackouts periodically
	ticker := time.NewTicker(BlackoutCheckInterval)
	defer ticker.Stop()

	e.log("Blackout scheduler started")

	for {
		select {
		case <-ctx.Done():
			e.log("Blackout scheduler stopping")
			return
		case <-ticker.C:
			e.checkScheduledBlackouts(ctx)
		}
	}
}

// runAlertCleaner checks for resolved conditions and clears alerts
func (e *Engine) runAlertCleaner(ctx context.Context) {
	// Check for resolved alerts periodically
	ticker := time.NewTicker(AlertCleanerInterval)
	defer ticker.Stop()

	e.log("Alert cleaner started")

	for {
		select {
		case <-ctx.Done():
			e.log("Alert cleaner stopping")
			return
		case <-ticker.C:
			e.cleanResolvedAlerts(ctx)
		}
	}
}

// runRetentionManager cleans up old data based on retention policy
func (e *Engine) runRetentionManager(ctx context.Context) {
	// Run retention cleanup periodically
	ticker := time.NewTicker(RetentionCleanupInterval)
	defer ticker.Stop()

	e.log("Retention manager started")

	// Run immediately on start
	e.cleanupOldData(ctx)

	for {
		select {
		case <-ctx.Done():
			e.log("Retention manager stopping")
			return
		case <-ticker.C:
			e.cleanupOldData(ctx)
		}
	}
}

// runConnectionErrorEvaluator monitors connections for error states
func (e *Engine) runConnectionErrorEvaluator(ctx context.Context) {
	ticker := time.NewTicker(AlertCleanerInterval)
	defer ticker.Stop()

	e.log("Connection error evaluator started (interval: %v)", AlertCleanerInterval)

	// Run immediately on start
	e.evaluateConnectionErrors(ctx)

	for {
		select {
		case <-ctx.Done():
			e.log("Connection error evaluator stopping")
			return
		case <-ticker.C:
			e.evaluateConnectionErrors(ctx)
		}
	}
}

// log outputs a message to stderr
func (e *Engine) log(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "[alerter] %s\n", msg)
}

// debugLog outputs a message only if debug is enabled
func (e *Engine) debugLog(format string, args ...any) {
	if e.debug {
		e.log(format, args...)
	}
}
