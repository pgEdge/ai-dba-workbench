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
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/cron"
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
)

// notificationJob represents a notification to be sent by a worker
type notificationJob struct {
	alert    *database.Alert
	notifTyp database.NotificationType
}

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

	// Create and start notification worker pool using the generic WorkerPool abstraction.
	// The pool handles bounded concurrency with non-blocking submission and graceful shutdown.
	e.notificationPool = worker.NewWorkerPool(
		NotificationWorkerPoolSize,
		100, // Buffer for burst handling
		e.processNotificationJob,
	)
	e.notificationPool.Start()
	e.log("Started %d notification workers", NotificationWorkerPoolSize)

	return e
}

// processNotificationJob handles a single notification job
func (e *Engine) processNotificationJob(job notificationJob) {
	if e.notificationMgr == nil {
		return
	}

	notifCtx, cancel := context.WithTimeout(context.Background(), NotificationTimeout)
	defer cancel()

	if err := e.notificationMgr.SendAlertNotification(notifCtx, job.alert, job.notifTyp); err != nil {
		e.log("ERROR: Failed to send notification: %v", err)
	}

	// For clear notifications, also delete reminder states
	if job.notifTyp == database.NotificationTypeAlertClear {
		if err := e.datastore.DeleteReminderStatesForAlert(notifCtx, job.alert.ID); err != nil {
			e.log("ERROR: Failed to delete reminder states for alert %d: %v", job.alert.ID, err)
		}
	}
}

// queueNotification queues a notification job for async processing by the worker pool
func (e *Engine) queueNotification(alert *database.Alert, notifTyp database.NotificationType) {
	if !e.notificationPool.Submit(notificationJob{alert: alert, notifTyp: notifTyp}) {
		// Queue full or pool stopped - log warning but don't block
		e.log("WARNING: Notification queue full, dropping notification for alert %d", alert.ID)
	}
}

// StopNotificationWorkers gracefully shuts down the notification worker pool
func (e *Engine) StopNotificationWorkers() {
	e.notificationPool.Stop()
	e.log("Notification workers stopped")
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
}

// Run starts the engine and runs until the context is canceled
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

// runNotificationWorker processes pending and retry notifications
func (e *Engine) runNotificationWorker(ctx context.Context) {
	interval := time.Duration(e.config.Notifications.ProcessIntervalSeconds) * time.Second
	if interval == 0 {
		interval = DefaultNotificationProcessInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.log("Notification worker started (interval: %v)", interval)

	for {
		select {
		case <-ctx.Done():
			e.log("Notification worker stopping")
			return
		case <-ticker.C:
			if e.notificationMgr != nil {
				if err := e.notificationMgr.ProcessPendingNotifications(ctx); err != nil {
					e.log("ERROR: Failed to process pending notifications: %v", err)
				}
			}
		}
	}
}

// runReminderWorker sends periodic reminder notifications for active alerts
func (e *Engine) runReminderWorker(ctx context.Context) {
	interval := time.Duration(e.config.Notifications.ReminderCheckIntervalMinutes) * time.Minute
	if interval == 0 {
		interval = DefaultReminderCheckInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.log("Reminder worker started (interval: %v)", interval)

	// Process reminders immediately on start
	if e.notificationMgr != nil {
		if err := e.notificationMgr.ProcessReminders(ctx); err != nil {
			e.log("ERROR: Failed to process reminders: %v", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			e.log("Reminder worker stopping")
			return
		case <-ticker.C:
			if e.notificationMgr != nil {
				if err := e.notificationMgr.ProcessReminders(ctx); err != nil {
					e.log("ERROR: Failed to process reminders: %v", err)
				}
			}
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

// evaluateConnectionErrors checks monitored connections for error states
func (e *Engine) evaluateConnectionErrors(ctx context.Context) {
	e.debug_log("Evaluating connection errors...")

	connections, err := e.datastore.GetMonitoredConnectionErrors(ctx)
	if err != nil {
		e.log("ERROR: Failed to get monitored connection errors: %v", err)
		return
	}

	for _, conn := range connections {
		if ctx.Err() != nil {
			return
		}

		if conn.ConnectionError != nil {
			// Connection has an error - create or update alert
			alertID, desc, found, err := e.datastore.GetActiveConnectionAlert(ctx, conn.ConnectionID)
			if err != nil {
				e.log("ERROR: Failed to check active connection alert for connection %d: %v", conn.ConnectionID, err)
				continue
			}

			if !found {
				alert, err := e.datastore.CreateConnectionAlert(ctx, conn.ConnectionID, conn.Name, *conn.ConnectionError)
				if err != nil {
					e.log("ERROR: Failed to create connection alert for connection %d: %v", conn.ConnectionID, err)
					continue
				}
				e.log("Connection error alert created for %s: %s", conn.Name, *conn.ConnectionError)

				// Queue notification for async processing
				e.queueNotification(alert, database.NotificationTypeAlertFire)
			} else if desc != *conn.ConnectionError {
				if err := e.datastore.UpdateConnectionAlertDescription(ctx, alertID, *conn.ConnectionError); err != nil {
					e.log("ERROR: Failed to update connection alert description for connection %d: %v", conn.ConnectionID, err)
				} else {
					e.debug_log("Updated connection error description for %s", conn.Name)
				}
			}
		} else {
			// No error - clear any active connection alert
			alertID, _, found, err := e.datastore.GetActiveConnectionAlert(ctx, conn.ConnectionID)
			if err != nil {
				e.log("ERROR: Failed to check active connection alert for connection %d: %v", conn.ConnectionID, err)
				continue
			}

			if found {
				if err := e.datastore.ClearAlert(ctx, alertID); err != nil {
					e.log("ERROR: Failed to clear connection alert for connection %d: %v", conn.ConnectionID, err)
					continue
				}
				e.log("Connection error alert cleared for %s", conn.Name)

				// Queue clear notification
				alert, err := e.datastore.GetAlert(ctx, alertID)
				if err == nil {
					e.queueNotification(alert, database.NotificationTypeAlertClear)
				}
			}
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
		e.evaluateRuleForAllConnections(ctx, rule)
	}
}

// evaluateRuleForAllConnections evaluates a rule across all connections with data
func (e *Engine) evaluateRuleForAllConnections(ctx context.Context, rule *database.AlertRule) {
	// Get all metric values for this rule's metric
	values, err := e.datastore.GetLatestMetricValues(ctx, rule.MetricName)
	if err != nil {
		e.debug_log("No data for metric %s: %v", rule.MetricName, err)
		return
	}

	for _, mv := range values {
		if ctx.Err() != nil {
			return
		}

		// Check if there's a blackout active for this connection
		connID := mv.ConnectionID
		active, err := e.datastore.IsBlackoutActive(ctx, &connID, mv.DatabaseName)
		if err != nil {
			e.debug_log("Error checking blackout for connection %d: %v", connID, err)
		}
		if active {
			e.debug_log("Skipping rule %s for connection %d: blackout active", rule.Name, connID)
			continue
		}

		// Get the effective threshold (global or per-connection override)
		threshold, operator, severity, enabled := e.datastore.GetEffectiveThreshold(
			ctx, rule.ID, mv.ConnectionID, mv.DatabaseName)
		if !enabled {
			e.debug_log("Rule %s disabled for connection %d", rule.Name, mv.ConnectionID)
			continue
		}

		// Check if threshold is violated
		violated := e.checkThreshold(mv.Value, operator, threshold)

		if violated {
			e.triggerThresholdAlert(ctx, rule, mv.Value, threshold, operator,
				severity, mv.ConnectionID, mv.DatabaseName, mv.ObjectName)
		}
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
func (e *Engine) triggerThresholdAlert(ctx context.Context, rule *database.AlertRule, value, threshold float64, operator, severity string, connectionID int, dbName *string, objectName *string) {
	e.log("Threshold violated: %s (%.2f %s %.2f) on connection %d", rule.Name, value, operator, threshold, connectionID)

	// Check if there's already an active alert for this rule/connection
	existing, err := e.datastore.GetActiveThresholdAlert(ctx, rule.ID, connectionID, dbName)
	if err == nil && existing != nil {
		// Alert already exists - update metric_value and last_updated timestamp
		if err := e.datastore.UpdateAlertMetricValue(ctx, existing.ID, value); err != nil {
			e.log("ERROR: Failed to update alert metric value: %v", err)
		} else {
			e.debug_log("Updated metric value for active alert %s: %.2f -> %.2f", rule.Name, *existing.MetricValue, value)
		}
		return
	}

	// Create new alert
	alert := &database.Alert{
		AlertType:      "threshold",
		RuleID:         &rule.ID,
		ConnectionID:   connectionID,
		DatabaseName:   dbName,
		ObjectName:     objectName,
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
		return
	}

	// Queue alert notification for async processing by worker pool
	e.queueNotification(alert, database.NotificationTypeAlertFire)
}

// calculateBaselines recalculates metric baselines for anomaly detection.
// It generates three types of baselines:
//   - 'all': Global aggregate baseline across all historical data
//   - 'hourly': Baselines per hour of day (0-23) for time-aware anomaly detection
//   - 'daily': Baselines per day of week (0=Sunday to 6=Saturday)
func (e *Engine) calculateBaselines(ctx context.Context) {
	e.debug_log("Calculating baselines...")

	// Get all active connections
	connections, err := e.datastore.GetActiveConnections(ctx)
	if err != nil {
		e.log("ERROR: Failed to get active connections: %v", err)
		return
	}

	// Get all enabled alert rules to determine which metrics need baselines
	rules, err := e.datastore.GetEnabledAlertRules(ctx)
	if err != nil {
		e.log("ERROR: Failed to get alert rules: %v", err)
		return
	}

	// Get lookback days from config (default to 7 if not set)
	lookbackDays := e.config.Baselines.LookbackDays
	if lookbackDays <= 0 {
		lookbackDays = 7
	}

	// Minimum samples required to create a time-period baseline
	const minSamplesForTimePeriod = 3

	e.log("Calculating baselines for %d connections, %d rules (lookback: %d days)",
		len(connections), len(rules), lookbackDays)

	// For each metric, fetch historical data and calculate baselines
	for _, rule := range rules {
		if ctx.Err() != nil {
			return
		}

		// Get historical metric values for all connections
		histValues, err := e.datastore.GetHistoricalMetricValues(ctx, rule.MetricName, lookbackDays)
		if err != nil {
			e.debug_log("No historical data for metric %s: %v", rule.MetricName, err)
			// Fall back to current values for 'all' baseline only
			e.calculateGlobalBaselinesFallback(ctx, connections, rule.MetricName)
			continue
		}

		if len(histValues) == 0 {
			continue
		}

		e.debug_log("Processing %d historical values for metric %s", len(histValues), rule.MetricName)

		// Group values by connection ID and optionally database name
		type groupKey struct {
			connectionID int
			databaseName string
		}
		groupedValues := make(map[groupKey][]database.HistoricalMetricValue)

		for _, hv := range histValues {
			dbName := ""
			if hv.DatabaseName != nil {
				dbName = *hv.DatabaseName
			}
			key := groupKey{connectionID: hv.ConnectionID, databaseName: dbName}
			groupedValues[key] = append(groupedValues[key], hv)
		}

		// Process each connection/database group
		for key, values := range groupedValues {
			if ctx.Err() != nil {
				return
			}

			var dbNamePtr *string
			if key.databaseName != "" {
				dbNamePtr = &key.databaseName
			}

			// Calculate 'all' baseline (global aggregate)
			e.calculateAllBaseline(ctx, key.connectionID, dbNamePtr, rule.MetricName, values)

			// Calculate hourly baselines (by hour of day)
			e.calculateHourlyBaselines(ctx, key.connectionID, dbNamePtr, rule.MetricName, values, minSamplesForTimePeriod)

			// Calculate daily baselines (by day of week)
			e.calculateDailyBaselines(ctx, key.connectionID, dbNamePtr, rule.MetricName, values, minSamplesForTimePeriod)
		}
	}

	e.log("Baseline calculation complete")
}

// calculateAllBaseline calculates the global 'all' baseline for a metric
func (e *Engine) calculateAllBaseline(ctx context.Context, connID int, dbName *string, metricName string, values []database.HistoricalMetricValue) {
	if len(values) == 0 {
		return
	}

	// Extract float values
	floatValues := make([]float64, len(values))
	for i, v := range values {
		floatValues[i] = v.Value
	}

	mean, stddev := calculateStats(floatValues)

	baseline := &database.MetricBaseline{
		ConnectionID:   connID,
		DatabaseName:   dbName,
		MetricName:     metricName,
		PeriodType:     "all",
		Mean:           mean,
		StdDev:         stddev,
		Min:            minValue(floatValues),
		Max:            maxValue(floatValues),
		SampleCount:    int64(len(floatValues)),
		LastCalculated: time.Now(),
	}

	if err := e.datastore.UpsertMetricBaseline(ctx, baseline); err != nil {
		e.log("ERROR: Failed to upsert 'all' baseline for %s on connection %d: %v",
			metricName, connID, err)
	}
}

// calculateHourlyBaselines calculates baselines for each hour of the day (0-23)
func (e *Engine) calculateHourlyBaselines(ctx context.Context, connID int, dbName *string, metricName string, values []database.HistoricalMetricValue, minSamples int) {
	// Group values by hour of day
	hourlyValues := make(map[int][]float64)
	for _, v := range values {
		hour := v.CollectedAt.Hour()
		hourlyValues[hour] = append(hourlyValues[hour], v.Value)
	}

	// Calculate baseline for each hour that has enough samples
	for hour, vals := range hourlyValues {
		if len(vals) < minSamples {
			continue
		}

		mean, stddev := calculateStats(vals)
		hourVal := hour

		baseline := &database.MetricBaseline{
			ConnectionID:   connID,
			DatabaseName:   dbName,
			MetricName:     metricName,
			PeriodType:     "hourly",
			HourOfDay:      &hourVal,
			Mean:           mean,
			StdDev:         stddev,
			Min:            minValue(vals),
			Max:            maxValue(vals),
			SampleCount:    int64(len(vals)),
			LastCalculated: time.Now(),
		}

		if err := e.datastore.UpsertMetricBaseline(ctx, baseline); err != nil {
			e.log("ERROR: Failed to upsert hourly baseline for %s hour %d on connection %d: %v",
				metricName, hour, connID, err)
		}
	}
}

// calculateDailyBaselines calculates baselines for each day of the week (0=Sunday to 6=Saturday)
func (e *Engine) calculateDailyBaselines(ctx context.Context, connID int, dbName *string, metricName string, values []database.HistoricalMetricValue, minSamples int) {
	// Group values by day of week (0=Sunday, 1=Monday, ..., 6=Saturday)
	dailyValues := make(map[int][]float64)
	for _, v := range values {
		// Go's time.Weekday() returns 0=Sunday, 1=Monday, etc.
		dayOfWeek := int(v.CollectedAt.Weekday())
		dailyValues[dayOfWeek] = append(dailyValues[dayOfWeek], v.Value)
	}

	// Calculate baseline for each day that has enough samples
	for day, vals := range dailyValues {
		if len(vals) < minSamples {
			continue
		}

		mean, stddev := calculateStats(vals)
		dayVal := day

		baseline := &database.MetricBaseline{
			ConnectionID:   connID,
			DatabaseName:   dbName,
			MetricName:     metricName,
			PeriodType:     "daily",
			DayOfWeek:      &dayVal,
			Mean:           mean,
			StdDev:         stddev,
			Min:            minValue(vals),
			Max:            maxValue(vals),
			SampleCount:    int64(len(vals)),
			LastCalculated: time.Now(),
		}

		if err := e.datastore.UpsertMetricBaseline(ctx, baseline); err != nil {
			e.log("ERROR: Failed to upsert daily baseline for %s day %d on connection %d: %v",
				metricName, day, connID, err)
		}
	}
}

// calculateGlobalBaselinesFallback calculates only 'all' baselines when historical data
// is not available. This uses the current metric values as a fallback.
func (e *Engine) calculateGlobalBaselinesFallback(ctx context.Context, connections []int, metricName string) {
	// Get current metric values
	values, err := e.datastore.GetLatestMetricValues(ctx, metricName)
	if err != nil {
		return
	}

	// Group by connection
	for _, connID := range connections {
		var connValues []float64
		for _, v := range values {
			if v.ConnectionID == connID {
				connValues = append(connValues, v.Value)
			}
		}

		if len(connValues) == 0 {
			continue
		}

		mean, stddev := calculateStats(connValues)

		baseline := &database.MetricBaseline{
			ConnectionID:   connID,
			MetricName:     metricName,
			PeriodType:     "all",
			Mean:           mean,
			StdDev:         stddev,
			Min:            minValue(connValues),
			Max:            maxValue(connValues),
			SampleCount:    int64(len(connValues)),
			LastCalculated: time.Now(),
		}

		if err := e.datastore.UpsertMetricBaseline(ctx, baseline); err != nil {
			e.log("ERROR: Failed to upsert fallback baseline for %s on connection %d: %v",
				metricName, connID, err)
		}
	}
}

// detectAnomalies runs the tiered anomaly detection
func (e *Engine) detectAnomalies(ctx context.Context) {
	e.debug_log("Running anomaly detection...")

	if !e.config.Anomaly.Tier1.Enabled {
		return
	}

	// Get all active connections
	connections, err := e.datastore.GetActiveConnections(ctx)
	if err != nil {
		e.log("ERROR: Failed to get active connections: %v", err)
		return
	}

	// Get all enabled alert rules
	rules, err := e.datastore.GetEnabledAlertRules(ctx)
	if err != nil {
		e.log("ERROR: Failed to get alert rules: %v", err)
		return
	}

	sensitivity := e.config.Anomaly.Tier1.DefaultSensitivity

	// For each connection and metric, check for anomalies
	for _, connID := range connections {
		if ctx.Err() != nil {
			return
		}

		for _, rule := range rules {
			// Get current metric value
			values, err := e.datastore.GetLatestMetricValues(ctx, rule.MetricName)
			if err != nil {
				continue
			}

			// Find value for this connection
			var currentValue *database.MetricValue
			for i := range values {
				if values[i].ConnectionID == connID {
					currentValue = &values[i]
					break
				}
			}
			if currentValue == nil {
				continue
			}

			// Get baseline for this metric/connection
			baselines, err := e.datastore.GetMetricBaselines(ctx, connID, rule.MetricName)
			if err != nil || len(baselines) == 0 {
				continue
			}

			baseline := baselines[0]
			if baseline.StdDev == 0 {
				continue
			}

			// Calculate z-score
			zScore := (currentValue.Value - baseline.Mean) / baseline.StdDev

			// Check if z-score exceeds threshold
			if zScore > sensitivity || zScore < -sensitivity {
				e.debug_log("Tier 1 anomaly detected: %s on connection %d (z-score: %.2f)",
					rule.MetricName, connID, zScore)

				// Create anomaly candidate for further processing
				candidate := &database.AnomalyCandidate{
					ConnectionID: connID,
					MetricName:   rule.MetricName,
					MetricValue:  currentValue.Value,
					ZScore:       zScore,
					DetectedAt:   time.Now(),
					Context:      fmt.Sprintf(`{"baseline_mean": %.2f, "baseline_stddev": %.2f, "period_type": "%s"}`, baseline.Mean, baseline.StdDev, baseline.PeriodType),
					Tier1Pass:    true,
				}

				if err := e.datastore.CreateAnomalyCandidate(ctx, candidate); err != nil {
					e.log("ERROR: Failed to create anomaly candidate: %v", err)
				}
			}
		}
	}

	// Process tier 2 and tier 3 if enabled
	if e.config.Anomaly.Tier2.Enabled || e.config.Anomaly.Tier3.Enabled {
		e.processTier2And3(ctx)
	}
}

// processTier2And3 processes anomaly candidates through tier 2 and tier 3
func (e *Engine) processTier2And3(ctx context.Context) {
	candidates, err := e.datastore.GetUnprocessedAnomalyCandidates(ctx, 100)
	if err != nil {
		e.log("ERROR: Failed to get anomaly candidates: %v", err)
		return
	}

	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return
		}

		var similarAnomalies []*database.SimilarAnomaly
		var embedding []float32

		// Tier 2: Embedding similarity
		if e.config.Anomaly.Tier2.Enabled && e.embeddingProvider != nil {
			embedding, similarAnomalies = e.processTier2(ctx, candidate)
		} else {
			// Skip Tier 2, pass through to Tier 3
			tier2Pass := true
			candidate.Tier2Pass = &tier2Pass
		}

		// Tier 3: LLM classification (only if Tier 2 passed or was skipped)
		if e.config.Anomaly.Tier3.Enabled && e.reasoningProvider != nil &&
			(candidate.Tier2Pass == nil || *candidate.Tier2Pass) {
			e.processTier3(ctx, candidate, similarAnomalies)
		}

		// Determine final decision
		e.determineFinalDecision(candidate)

		// Store embedding if we have one
		if len(embedding) > 0 {
			if err := e.datastore.StoreAnomalyEmbedding(ctx, candidate.ID, embedding, e.embeddingProvider.ModelName()); err != nil {
				e.debug_log("Failed to store embedding for candidate %d: %v", candidate.ID, err)
			}
		}

		// Mark as processed
		now := time.Now()
		candidate.ProcessedAt = &now

		if err := e.datastore.UpdateAnomalyCandidate(ctx, candidate); err != nil {
			e.log("ERROR: Failed to update anomaly candidate: %v", err)
		}
	}
}

// processTier2 handles Tier 2 embedding similarity processing
func (e *Engine) processTier2(ctx context.Context, candidate *database.AnomalyCandidate) ([]float32, []*database.SimilarAnomaly) {
	e.debug_log("Tier 2: Processing candidate %d for metric %s", candidate.ID, candidate.MetricName)

	// Build the context text for embedding
	contextText := e.buildContextText(candidate)

	// Generate embedding
	embedding, err := e.embeddingProvider.GenerateEmbedding(ctx, contextText)
	if err != nil {
		e.log("ERROR: Failed to generate embedding for candidate %d: %v", candidate.ID, err)
		// On embedding failure, pass through to Tier 3
		tier2Pass := true
		candidate.Tier2Pass = &tier2Pass
		return nil, nil
	}

	// Search for similar past anomalies
	threshold := e.config.Anomaly.Tier2.SimilarityThreshold
	if threshold <= 0 {
		threshold = 0.3 // Default minimum similarity
	}

	similarAnomalies, err := e.datastore.FindSimilarAnomalies(ctx, embedding, candidate.ID, threshold, 10)
	if err != nil {
		e.debug_log("Failed to find similar anomalies: %v", err)
		// On search failure, pass through to Tier 3
		tier2Pass := true
		candidate.Tier2Pass = &tier2Pass
		return embedding, nil
	}

	// Analyze similar anomalies
	if len(similarAnomalies) > 0 {
		// Find the highest similarity score
		var maxSimilarity float64
		var suppressCount, alertCount int

		for _, sa := range similarAnomalies {
			if sa.Similarity > maxSimilarity {
				maxSimilarity = sa.Similarity
			}
			if sa.FinalDecision != nil {
				switch *sa.FinalDecision {
				case "suppress", "suppressed", "false_positive":
					suppressCount++
				case "alert", "anomaly":
					alertCount++
				}
			}
		}

		candidate.Tier2Score = &maxSimilarity

		// Apply suppression logic based on similar anomalies
		suppressionThreshold := e.config.Anomaly.Tier2.SuppressionThreshold
		if suppressionThreshold <= 0 {
			suppressionThreshold = 0.85 // Default high similarity threshold for suppression
		}

		if maxSimilarity >= suppressionThreshold && suppressCount > alertCount {
			// High similarity to suppressed anomalies -> suppress this one too
			tier2Pass := false
			candidate.Tier2Pass = &tier2Pass
			e.debug_log("Tier 2: Suppressing candidate %d (similarity %.2f to %d suppressed anomalies)",
				candidate.ID, maxSimilarity, suppressCount)
		} else if maxSimilarity >= suppressionThreshold && alertCount > suppressCount {
			// High similarity to real anomalies -> this is likely a real issue
			tier2Pass := true
			candidate.Tier2Pass = &tier2Pass
			e.debug_log("Tier 2: Passing candidate %d (similarity %.2f to %d alerted anomalies)",
				candidate.ID, maxSimilarity, alertCount)
		} else {
			// Low similarity or mixed results -> needs LLM review
			tier2Pass := true
			candidate.Tier2Pass = &tier2Pass
			e.debug_log("Tier 2: Passing candidate %d to Tier 3 for review (similarity %.2f)",
				candidate.ID, maxSimilarity)
		}
	} else {
		// No similar anomalies found -> needs LLM review
		tier2Pass := true
		candidate.Tier2Pass = &tier2Pass
		score := 0.0
		candidate.Tier2Score = &score
		e.debug_log("Tier 2: No similar anomalies found for candidate %d, passing to Tier 3",
			candidate.ID)
	}

	return embedding, similarAnomalies
}

// processTier3 handles Tier 3 LLM classification
func (e *Engine) processTier3(ctx context.Context, candidate *database.AnomalyCandidate, similarAnomalies []*database.SimilarAnomaly) {
	e.debug_log("Tier 3: Processing candidate %d with LLM", candidate.ID)

	// Build the classification prompt
	prompt := e.buildClassificationPrompt(candidate, similarAnomalies)

	// Create a timeout context for Tier 3
	timeout := time.Duration(e.config.Anomaly.Tier3.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = DefaultTier3Timeout
	}
	tier3Ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Call the LLM for classification
	response, err := e.reasoningProvider.Classify(tier3Ctx, prompt)
	if err != nil {
		e.log("ERROR: Tier 3 LLM classification failed for candidate %d: %v", candidate.ID, err)
		errStr := err.Error()
		candidate.Tier3Error = &errStr
		// On LLM failure, default to alert (fail safe)
		tier3Pass := true
		candidate.Tier3Pass = &tier3Pass
		result := "LLM classification failed, defaulting to alert"
		candidate.Tier3Result = &result
		return
	}

	// Store the raw response
	candidate.Tier3Result = &response

	// Parse the LLM response
	decision, _ := e.parseLLMResponse(response)

	switch strings.ToLower(decision) {
	case "alert", "anomaly":
		tier3Pass := true
		candidate.Tier3Pass = &tier3Pass
		e.debug_log("Tier 3: LLM classified candidate %d as ALERT", candidate.ID)
	case "suppress", "suppressed", "false_positive":
		tier3Pass := false
		candidate.Tier3Pass = &tier3Pass
		e.debug_log("Tier 3: LLM classified candidate %d as SUPPRESS", candidate.ID)
	default:
		// Unknown response, default to alert
		tier3Pass := true
		candidate.Tier3Pass = &tier3Pass
		e.debug_log("Tier 3: Unknown LLM response for candidate %d, defaulting to alert", candidate.ID)
	}
}

// determineFinalDecision sets the final decision based on tier results
func (e *Engine) determineFinalDecision(candidate *database.AnomalyCandidate) {
	// If Tier 2 explicitly suppressed, suppress the anomaly
	if candidate.Tier2Pass != nil && !*candidate.Tier2Pass {
		decision := "suppress"
		candidate.FinalDecision = &decision
		return
	}

	// If Tier 3 was run, use its decision
	if candidate.Tier3Pass != nil {
		if *candidate.Tier3Pass {
			decision := "alert"
			candidate.FinalDecision = &decision
		} else {
			decision := "suppress"
			candidate.FinalDecision = &decision
		}
		return
	}

	// If only Tier 2 passed, treat as anomaly
	if candidate.Tier2Pass != nil && *candidate.Tier2Pass {
		decision := "alert"
		candidate.FinalDecision = &decision
		return
	}

	// Default to anomaly (Tier 1 already passed)
	decision := "alert"
	candidate.FinalDecision = &decision
}

// buildContextText builds a text representation of the anomaly for embedding
func (e *Engine) buildContextText(candidate *database.AnomalyCandidate) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Metric: %s\n", candidate.MetricName))
	sb.WriteString(fmt.Sprintf("Value: %.4f\n", candidate.MetricValue))
	sb.WriteString(fmt.Sprintf("Z-Score: %.2f\n", candidate.ZScore))
	sb.WriteString(fmt.Sprintf("Connection ID: %d\n", candidate.ConnectionID))

	if candidate.DatabaseName != nil {
		sb.WriteString(fmt.Sprintf("Database: %s\n", *candidate.DatabaseName))
	}

	sb.WriteString(fmt.Sprintf("Detected at: %s\n", candidate.DetectedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Context: %s\n", candidate.Context))

	return sb.String()
}

// buildClassificationPrompt builds the prompt for LLM classification
func (e *Engine) buildClassificationPrompt(candidate *database.AnomalyCandidate, similarAnomalies []*database.SimilarAnomaly) string {
	var sb strings.Builder

	sb.WriteString("Analyze the following anomaly candidate and determine if it is a real issue that requires attention (alert) or a false positive that should be suppressed.\n\n")

	sb.WriteString("## Current Anomaly\n")
	sb.WriteString(fmt.Sprintf("- Metric: %s\n", candidate.MetricName))
	sb.WriteString(fmt.Sprintf("- Value: %.4f\n", candidate.MetricValue))
	sb.WriteString(fmt.Sprintf("- Z-Score: %.2f (standard deviations from mean)\n", candidate.ZScore))
	sb.WriteString(fmt.Sprintf("- Connection ID: %d\n", candidate.ConnectionID))

	if candidate.DatabaseName != nil {
		sb.WriteString(fmt.Sprintf("- Database: %s\n", *candidate.DatabaseName))
	}

	sb.WriteString(fmt.Sprintf("- Detected at: %s\n", candidate.DetectedAt.Format(time.RFC3339)))

	// Parse and include baseline info from context
	var contextData map[string]interface{}
	if err := json.Unmarshal([]byte(candidate.Context), &contextData); err == nil {
		if mean, ok := contextData["baseline_mean"].(float64); ok {
			sb.WriteString(fmt.Sprintf("- Baseline mean: %.4f\n", mean))
		}
		if stddev, ok := contextData["baseline_stddev"].(float64); ok {
			sb.WriteString(fmt.Sprintf("- Baseline stddev: %.4f\n", stddev))
		}
		if periodType, ok := contextData["period_type"].(string); ok {
			sb.WriteString(fmt.Sprintf("- Baseline period: %s\n", periodType))
		}
	}

	// Include similar past anomalies if available
	if len(similarAnomalies) > 0 {
		sb.WriteString("\n## Similar Past Anomalies\n")
		for i, sa := range similarAnomalies {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("... and %d more similar anomalies\n", len(similarAnomalies)-5))
				break
			}
			decision := "unknown"
			if sa.FinalDecision != nil {
				decision = *sa.FinalDecision
			}
			sb.WriteString(fmt.Sprintf("- Similarity: %.2f%%, Decision: %s, Metric: %s\n",
				sa.Similarity*100, decision, sa.MetricName))
		}
	} else {
		sb.WriteString("\n## Similar Past Anomalies\nNo similar past anomalies found.\n")
	}

	sb.WriteString("\n## Instructions\n")
	sb.WriteString("Based on the above information, respond with a JSON object containing:\n")
	sb.WriteString("- \"decision\": either \"alert\" (real issue) or \"suppress\" (false positive)\n")
	sb.WriteString("- \"confidence\": a number from 0 to 1\n")
	sb.WriteString("- \"reasoning\": a brief explanation\n")

	return sb.String()
}

// parseLLMResponse parses the LLM response to extract the decision
func (e *Engine) parseLLMResponse(response string) (string, float64) {
	// Try to parse as JSON first
	var result struct {
		Decision   string  `json:"decision"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(response), &result); err == nil {
		return result.Decision, result.Confidence
	}

	// Fall back to text parsing
	lowerResponse := strings.ToLower(response)

	// Look for explicit decision keywords
	if strings.Contains(lowerResponse, "\"decision\"") || strings.Contains(lowerResponse, "'decision'") {
		// Try to find decision value
		if strings.Contains(lowerResponse, "\"alert\"") || strings.Contains(lowerResponse, "'alert'") {
			return "alert", 0.5
		}
		if strings.Contains(lowerResponse, "\"suppress\"") || strings.Contains(lowerResponse, "'suppress'") {
			return "suppress", 0.5
		}
	}

	// Look for decision keywords in natural language
	if strings.Contains(lowerResponse, "should be suppressed") ||
		strings.Contains(lowerResponse, "false positive") ||
		strings.Contains(lowerResponse, "not a real issue") ||
		strings.Contains(lowerResponse, "normal behavior") {
		return "suppress", 0.5
	}

	if strings.Contains(lowerResponse, "real issue") ||
		strings.Contains(lowerResponse, "should alert") ||
		strings.Contains(lowerResponse, "requires attention") ||
		strings.Contains(lowerResponse, "genuine anomaly") {
		return "alert", 0.5
	}

	// Default to alert if we can't parse the response
	return "alert", 0.3
}

// checkScheduledBlackouts activates scheduled blackouts based on cron expressions
func (e *Engine) checkScheduledBlackouts(ctx context.Context) {
	e.debug_log("Checking scheduled blackouts...")

	// Get all enabled blackout schedules
	schedules, err := e.datastore.GetEnabledBlackoutSchedules(ctx)
	if err != nil {
		e.log("ERROR: Failed to get blackout schedules: %v", err)
		return
	}

	now := time.Now()

	for _, schedule := range schedules {
		if ctx.Err() != nil {
			return
		}

		// Check if current time matches the cron expression
		// This is a simplified check - a full implementation would use a cron parser
		if e.cronMatches(schedule.CronExpression, now, schedule.Timezone) {
			// Check if there's already an active blackout for this schedule
			connID := schedule.ConnectionID
			active, err := e.datastore.IsBlackoutActive(ctx, connID, schedule.DatabaseName)
			if err != nil {
				e.debug_log("Error checking blackout for schedule %s: %v", schedule.Name, err)
			}
			if active {
				continue
			}

			// Create a new blackout entry, inheriting scope from the schedule
			endTime := now.Add(time.Duration(schedule.DurationMinutes) * time.Minute)
			blackout := &database.Blackout{
				Scope:        schedule.Scope,
				ConnectionID: schedule.ConnectionID,
				GroupID:      schedule.GroupID,
				ClusterID:    schedule.ClusterID,
				DatabaseName: schedule.DatabaseName,
				Reason:       fmt.Sprintf("Scheduled: %s", schedule.Name),
				StartTime:    now,
				EndTime:      endTime,
				CreatedBy:    "scheduler",
				CreatedAt:    now,
			}

			if err := e.datastore.CreateBlackout(ctx, blackout); err != nil {
				e.log("ERROR: Failed to create blackout: %v", err)
			} else {
				e.log("Created scheduled blackout: %s (until %s)", schedule.Name, endTime.Format(time.RFC3339))
			}
		}
	}
}

// cronMatches checks if the current time matches a cron expression.
// It supports standard 5-field cron expressions with wildcards, ranges,
// lists, and steps (e.g., "*/15 9-17 * * 1-5" for every 15 minutes
// from 9am-5pm on weekdays).
func (e *Engine) cronMatches(cronExpr string, now time.Time, timezone string) bool {
	matches, err := cron.Matches(cronExpr, now, timezone)
	if err != nil {
		e.debug_log("Invalid cron expression %q: %v", cronExpr, err)
		return false
	}
	return matches
}

// calculateStats calculates mean and standard deviation for a slice of values
func calculateStats(values []float64) (mean, stddev float64) {
	if len(values) == 0 {
		return 0, 0
	}

	// Calculate mean
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean = sum / float64(len(values))

	// Calculate standard deviation
	var variance float64
	for _, v := range values {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(values))
	stddev = math.Sqrt(variance)

	return mean, stddev
}

// minValue returns the minimum value in a slice
func minValue(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

// maxValue returns the maximum value in a slice
func maxValue(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
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
			return
		}

		// Queue clear notification for async processing by worker pool
		e.queueNotification(alert, database.NotificationTypeAlertClear)
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
