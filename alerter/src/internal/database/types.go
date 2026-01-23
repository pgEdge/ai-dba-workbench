/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import "time"

// AlerterSettings holds global alerter configuration
type AlerterSettings struct {
	ID                          int       `json:"id"`
	RetentionDays               int       `json:"retention_days"`
	DefaultAnomalyEnabled       bool      `json:"default_anomaly_enabled"`
	DefaultAnomalySensitivity   float64   `json:"default_anomaly_sensitivity"`
	BaselineRefreshIntervalMins int       `json:"baseline_refresh_interval_mins"`
	CorrelationWindowSeconds    int       `json:"correlation_window_seconds"`
	UpdatedAt                   time.Time `json:"updated_at"`
}

// ProbeAvailability tracks which probes have collected data for a connection
type ProbeAvailability struct {
	ID                int64      `json:"id"`
	ConnectionID      int        `json:"connection_id"`
	DatabaseName      string     `json:"database_name"`
	ProbeName         string     `json:"probe_name"`
	ExtensionName     *string    `json:"extension_name,omitempty"`
	IsAvailable       bool       `json:"is_available"`
	LastChecked       *time.Time `json:"last_checked,omitempty"`
	LastCollected     *time.Time `json:"last_collected,omitempty"`
	UnavailableReason *string    `json:"unavailable_reason,omitempty"`
}

// AlertRule represents a threshold-based alert rule
type AlertRule struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Category          string    `json:"category"`
	MetricName        string    `json:"metric_name"`
	DefaultOperator   string    `json:"default_operator"`
	DefaultThreshold  float64   `json:"default_threshold"`
	DefaultSeverity   string    `json:"default_severity"`
	DefaultEnabled    bool      `json:"default_enabled"`
	RequiredExtension *string   `json:"required_extension,omitempty"`
	IsBuiltIn         bool      `json:"is_built_in"`
	CreatedAt         time.Time `json:"created_at"`
}

// AlertThreshold represents a per-connection threshold override
type AlertThreshold struct {
	ID           int64     `json:"id"`
	RuleID       int64     `json:"rule_id"`
	ConnectionID *int      `json:"connection_id,omitempty"`
	DatabaseName *string   `json:"database_name,omitempty"`
	Operator     string    `json:"operator"`
	Threshold    float64   `json:"threshold"`
	Severity     string    `json:"severity"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Alert represents an active or historical alert
type Alert struct {
	ID             int64      `json:"id"`
	AlertType      string     `json:"alert_type"`
	RuleID         *int64     `json:"rule_id,omitempty"`
	ConnectionID   int        `json:"connection_id"`
	DatabaseName   *string    `json:"database_name,omitempty"`
	ProbeName      *string    `json:"probe_name,omitempty"`
	MetricName     *string    `json:"metric_name,omitempty"`
	MetricValue    *float64   `json:"metric_value,omitempty"`
	ThresholdValue *float64   `json:"threshold_value,omitempty"`
	Operator       *string    `json:"operator,omitempty"`
	Severity       string     `json:"severity"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	CorrelationID  *string    `json:"correlation_id,omitempty"`
	Status         string     `json:"status"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	ClearedAt      *time.Time `json:"cleared_at,omitempty"`
	LastUpdated    *time.Time `json:"last_updated,omitempty"`
	AnomalyScore   *float64   `json:"anomaly_score,omitempty"`
	AnomalyDetails *string    `json:"anomaly_details,omitempty"`
}

// AlertAcknowledgment represents user acknowledgment of an alert
type AlertAcknowledgment struct {
	ID              int64     `json:"id"`
	AlertID         int64     `json:"alert_id"`
	AcknowledgedBy  string    `json:"acknowledged_by"`
	AcknowledgedAt  time.Time `json:"acknowledged_at"`
	AcknowledgeType string    `json:"acknowledge_type"`
	Message         string    `json:"message"`
	FalsePositive   bool      `json:"false_positive"`
}

// Blackout represents a manual blackout period
type Blackout struct {
	ID           int64     `json:"id"`
	ConnectionID *int      `json:"connection_id,omitempty"`
	DatabaseName *string   `json:"database_name,omitempty"`
	Reason       string    `json:"reason"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
}

// BlackoutSchedule represents a recurring blackout schedule
type BlackoutSchedule struct {
	ID              int64     `json:"id"`
	ConnectionID    *int      `json:"connection_id,omitempty"`
	DatabaseName    *string   `json:"database_name,omitempty"`
	Name            string    `json:"name"`
	CronExpression  string    `json:"cron_expression"`
	DurationMinutes int       `json:"duration_minutes"`
	Timezone        string    `json:"timezone"`
	Reason          string    `json:"reason"`
	Enabled         bool      `json:"enabled"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// MetricDefinition describes a metric for anomaly detection
type MetricDefinition struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Category       string   `json:"category"`
	Description    string   `json:"description"`
	Unit           string   `json:"unit"`
	AnomalyEnabled bool     `json:"anomaly_enabled"`
	MinValue       *float64 `json:"min_value,omitempty"`
	MaxValue       *float64 `json:"max_value,omitempty"`
}

// MetricBaseline holds baseline statistics for a metric
type MetricBaseline struct {
	ID             int64     `json:"id"`
	ConnectionID   int       `json:"connection_id"`
	DatabaseName   *string   `json:"database_name,omitempty"`
	MetricName     string    `json:"metric_name"`
	PeriodType     string    `json:"period_type"`
	DayOfWeek      *int      `json:"day_of_week,omitempty"`
	HourOfDay      *int      `json:"hour_of_day,omitempty"`
	Mean           float64   `json:"mean"`
	StdDev         float64   `json:"stddev"`
	Min            float64   `json:"min"`
	Max            float64   `json:"max"`
	SampleCount    int64     `json:"sample_count"`
	LastCalculated time.Time `json:"last_calculated"`
}

// CorrelationGroup holds related anomalies
type CorrelationGroup struct {
	ID             int64      `json:"id"`
	ConnectionID   int        `json:"connection_id"`
	DatabaseName   *string    `json:"database_name,omitempty"`
	StartTime      time.Time  `json:"start_time"`
	EndTime        *time.Time `json:"end_time,omitempty"`
	AnomalyCount   int        `json:"anomaly_count"`
	RootCauseGuess *string    `json:"root_cause_guess,omitempty"`
}

// AnomalyCandidate represents a candidate anomaly for tier 2/3 processing
type AnomalyCandidate struct {
	ID            int64      `json:"id"`
	ConnectionID  int        `json:"connection_id"`
	DatabaseName  *string    `json:"database_name,omitempty"`
	MetricName    string     `json:"metric_name"`
	MetricValue   float64    `json:"metric_value"`
	ZScore        float64    `json:"z_score"`
	DetectedAt    time.Time  `json:"detected_at"`
	Context       string     `json:"context"`
	Tier1Pass     bool       `json:"tier1_pass"`
	Tier2Score    *float64   `json:"tier2_score,omitempty"`
	Tier2Pass     *bool      `json:"tier2_pass,omitempty"`
	Tier3Result   *string    `json:"tier3_result,omitempty"`
	Tier3Pass     *bool      `json:"tier3_pass,omitempty"`
	Tier3Error    *string    `json:"tier3_error,omitempty"`
	FinalDecision *string    `json:"final_decision,omitempty"`
	AlertID       *int64     `json:"alert_id,omitempty"`
	ProcessedAt   *time.Time `json:"processed_at,omitempty"`
}

// AlertListFilter holds filter options for listing alerts
type AlertListFilter struct {
	ConnectionID *int
	DatabaseName *string
	Status       *string
	Severity     *string
	AlertType    *string
	StartTime    *time.Time
	EndTime      *time.Time
	Limit        int
	Offset       int
}

// AlertListResult holds the result of listing alerts
type AlertListResult struct {
	Alerts []Alert `json:"alerts"`
	Total  int64   `json:"total"`
}

// HistoricalMetricValue represents a metric value with timestamp for baseline calculation
type HistoricalMetricValue struct {
	ConnectionID int
	DatabaseName *string
	Value        float64
	CollectedAt  time.Time
}

// AnomalyEmbedding represents a stored embedding for an anomaly candidate
type AnomalyEmbedding struct {
	ID          int64
	CandidateID int64
	Embedding   []float32
	ModelName   string
	CreatedAt   time.Time
}
