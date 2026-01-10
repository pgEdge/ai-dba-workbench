/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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
)

// Engine is the main alerter engine that coordinates all background processing
type Engine struct {
	config    *config.Config
	datastore *database.Datastore
	debug     bool

	// Synchronization
	mu       sync.RWMutex
	stopOnce sync.Once
}

// NewEngine creates a new alerter engine
func NewEngine(cfg *config.Config, datastore *database.Datastore, debug bool) *Engine {
	return &Engine{
		config:    cfg,
		datastore: datastore,
		debug:     debug,
	}
}

// Run starts the engine and runs until the context is cancelled
func (e *Engine) Run(ctx context.Context) error {
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
	}

	// Blackout scheduler
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.runBlackoutScheduler(ctx)
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

	e.log("All workers started")

	// Wait for shutdown
	<-ctx.Done()
	e.log("Shutdown signal received, stopping workers...")

	// Wait for all workers to finish
	wg.Wait()

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

// runThresholdEvaluator evaluates threshold-based alert rules
func (e *Engine) runThresholdEvaluator(ctx context.Context) {
	interval := time.Duration(e.config.Threshold.EvaluationIntervalSeconds) * time.Second
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
		}
	}
}

// runBaselineCalculator periodically recalculates metric baselines
func (e *Engine) runBaselineCalculator(ctx context.Context) {
	interval := time.Duration(e.config.Baselines.RefreshIntervalSeconds) * time.Second
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
		}
	}
}

// runAnomalyDetector runs the tiered anomaly detection system
func (e *Engine) runAnomalyDetector(ctx context.Context) {
	interval := time.Duration(e.config.Anomaly.Tier1.EvaluationIntervalSeconds) * time.Second
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
		}
	}
}

// runBlackoutScheduler checks and activates scheduled blackouts
func (e *Engine) runBlackoutScheduler(ctx context.Context) {
	// Check every minute for scheduled blackouts
	ticker := time.NewTicker(1 * time.Minute)
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
	// Check every 30 seconds for resolved alerts
	ticker := time.NewTicker(30 * time.Second)
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
	// Run retention cleanup daily
	ticker := time.NewTicker(24 * time.Hour)
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

// evaluateThresholds checks all threshold rules against current metrics
func (e *Engine) evaluateThresholds(ctx context.Context) {
	e.debug_log("Evaluating threshold rules...")

	// Get all enabled rules
	rules, err := e.datastore.GetEnabledAlertRules(ctx)
	if err != nil {
		e.log("ERROR: Failed to get alert rules: %v", err)
		return
	}

	e.debug_log("Found %d enabled rules", len(rules))

	for _, rule := range rules {
		if ctx.Err() != nil {
			return
		}
		e.evaluateRule(ctx, rule)
	}
}

// evaluateRule evaluates a single alert rule
func (e *Engine) evaluateRule(ctx context.Context, rule *database.AlertRule) {
	// Check if there's a blackout active for this rule
	if e.isBlackoutActive(ctx, rule) {
		e.debug_log("Skipping rule %s: blackout active", rule.Name)
		return
	}

	// Get current metric value from the latest collection
	value, connectionID, dbName, err := e.datastore.GetLatestMetricValue(ctx, rule.MetricName)
	if err != nil {
		e.debug_log("No data for metric %s: %v", rule.MetricName, err)
		return
	}

	// Get the effective threshold (global or per-connection override)
	threshold, operator, severity, enabled := e.datastore.GetEffectiveThreshold(ctx, rule.ID, connectionID, dbName)
	if !enabled {
		e.debug_log("Rule %s disabled for connection %d", rule.Name, connectionID)
		return
	}

	// Check if threshold is violated
	violated := e.checkThreshold(value, operator, threshold)

	if violated {
		e.triggerThresholdAlert(ctx, rule, value, threshold, operator, severity, connectionID, dbName)
	}
}

// checkThreshold checks if a value violates a threshold
func (e *Engine) checkThreshold(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return false
	}
}

// triggerThresholdAlert creates or updates an alert for a threshold violation
func (e *Engine) triggerThresholdAlert(ctx context.Context, rule *database.AlertRule, value, threshold float64, operator, severity string, connectionID int, dbName *string) {
	e.log("Threshold violated: %s (%.2f %s %.2f) on connection %d", rule.Name, value, operator, threshold, connectionID)

	// Check if there's already an active alert for this rule/connection
	existing, err := e.datastore.GetActiveThresholdAlert(ctx, rule.ID, connectionID, dbName)
	if err == nil && existing != nil {
		// Alert already exists, update last checked time
		e.debug_log("Alert already active for %s", rule.Name)
		return
	}

	// Create new alert
	alert := &database.Alert{
		AlertType:      "threshold",
		RuleID:         &rule.ID,
		ConnectionID:   connectionID,
		DatabaseName:   dbName,
		MetricName:     &rule.MetricName,
		MetricValue:    &value,
		ThresholdValue: &threshold,
		Operator:       &operator,
		Severity:       severity,
		Title:          rule.Name,
		Description:    rule.Description,
		Status:         "active",
		TriggeredAt:    time.Now(),
	}

	if err := e.datastore.CreateAlert(ctx, alert); err != nil {
		e.log("ERROR: Failed to create alert: %v", err)
	}
}

// isBlackoutActive checks if any blackout is active for the given rule
func (e *Engine) isBlackoutActive(ctx context.Context, rule *database.AlertRule) bool {
	// Check global blackouts and connection-specific blackouts
	active, err := e.datastore.IsBlackoutActive(ctx, nil, nil)
	if err != nil {
		e.log("ERROR: Failed to check blackout: %v", err)
		return false
	}
	return active
}

// calculateBaselines recalculates metric baselines for anomaly detection
func (e *Engine) calculateBaselines(ctx context.Context) {
	e.debug_log("Calculating baselines...")
	// TODO: Implement baseline calculation
	// - Get all metric definitions with anomaly_enabled
	// - For each connection/database, calculate statistics
	// - Update metric_baselines table
}

// detectAnomalies runs the tiered anomaly detection
func (e *Engine) detectAnomalies(ctx context.Context) {
	e.debug_log("Running anomaly detection...")
	// TODO: Implement tiered anomaly detection
	// - Tier 1: Statistical z-score detection
	// - Tier 2: Embedding similarity (if enabled)
	// - Tier 3: LLM classification (if enabled)
}

// checkScheduledBlackouts activates scheduled blackouts
func (e *Engine) checkScheduledBlackouts(ctx context.Context) {
	e.debug_log("Checking scheduled blackouts...")
	// TODO: Implement scheduled blackout activation
	// - Get all enabled schedules
	// - Check if current time matches cron expression
	// - Create blackout entries as needed
}

// cleanResolvedAlerts clears alerts where the condition has resolved
func (e *Engine) cleanResolvedAlerts(ctx context.Context) {
	e.debug_log("Checking for resolved alerts...")

	// Get all active threshold alerts
	alerts, err := e.datastore.GetActiveAlerts(ctx)
	if err != nil {
		e.log("ERROR: Failed to get active alerts: %v", err)
		return
	}

	for _, alert := range alerts {
		if ctx.Err() != nil {
			return
		}

		if alert.AlertType == "threshold" && alert.RuleID != nil {
			e.checkAlertResolved(ctx, alert)
		}
	}
}

// checkAlertResolved checks if a threshold alert's condition has resolved
func (e *Engine) checkAlertResolved(ctx context.Context, alert *database.Alert) {
	if alert.MetricName == nil || alert.ThresholdValue == nil || alert.Operator == nil {
		return
	}

	// Get current metric value
	value, connectionID, _, err := e.datastore.GetLatestMetricValue(ctx, *alert.MetricName)
	if err != nil {
		return
	}

	// Only check if it's the same connection
	if connectionID != alert.ConnectionID {
		return
	}

	// Check if threshold is still violated
	stillViolated := e.checkThreshold(value, *alert.Operator, *alert.ThresholdValue)

	if !stillViolated {
		e.log("Alert resolved: %s (%.2f no longer %s %.2f)", alert.Title, value, *alert.Operator, *alert.ThresholdValue)
		if err := e.datastore.ClearAlert(ctx, alert.ID); err != nil {
			e.log("ERROR: Failed to clear alert: %v", err)
		}
	}
}

// cleanupOldData removes data older than retention period
func (e *Engine) cleanupOldData(ctx context.Context) {
	e.debug_log("Running retention cleanup...")

	// Get retention settings
	settings, err := e.datastore.GetAlerterSettings(ctx)
	if err != nil {
		e.log("ERROR: Failed to get settings: %v", err)
		return
	}

	retentionDays := settings.RetentionDays
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	// Delete old cleared/acknowledged alerts
	deleted, err := e.datastore.DeleteOldAlerts(ctx, cutoff)
	if err != nil {
		e.log("ERROR: Failed to delete old alerts: %v", err)
	} else if deleted > 0 {
		e.log("Deleted %d old alerts", deleted)
	}

	// Delete old anomaly candidates
	deleted, err = e.datastore.DeleteOldAnomalyCandidates(ctx, cutoff)
	if err != nil {
		e.log("ERROR: Failed to delete old anomaly candidates: %v", err)
	} else if deleted > 0 {
		e.log("Deleted %d old anomaly candidates", deleted)
	}
}

// log outputs a message to stderr
func (e *Engine) log(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "[alerter] %s\n", msg)
}

// debug_log outputs a message only if debug is enabled
func (e *Engine) debug_log(format string, args ...interface{}) {
	if e.debug {
		e.log(format, args...)
	}
}
