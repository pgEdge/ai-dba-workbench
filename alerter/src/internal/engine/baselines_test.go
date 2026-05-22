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
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// TestCalculateStatsBasic tests basic mean and stddev calculations
func TestCalculateStatsBasic(t *testing.T) {
	tests := []struct {
		name           string
		values         []float64
		expectedMean   float64
		expectedStdDev float64
		tolerance      float64
	}{
		{
			name:           "empty slice",
			values:         []float64{},
			expectedMean:   0,
			expectedStdDev: 0,
			tolerance:      0.0001,
		},
		{
			name:           "single value",
			values:         []float64{42.0},
			expectedMean:   42.0,
			expectedStdDev: 0,
			tolerance:      0.0001,
		},
		{
			name:           "two identical values",
			values:         []float64{10.0, 10.0},
			expectedMean:   10.0,
			expectedStdDev: 0,
			tolerance:      0.0001,
		},
		{
			name:           "symmetric distribution",
			values:         []float64{0.0, 10.0},
			expectedMean:   5.0,
			expectedStdDev: 5.0,
			tolerance:      0.01,
		},
		{
			name:           "standard test values",
			values:         []float64{2.0, 4.0, 4.0, 4.0, 5.0, 5.0, 7.0, 9.0},
			expectedMean:   5.0,
			expectedStdDev: 2.0,
			tolerance:      0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mean, stddev := calculateStats(tt.values)

			if math.Abs(mean-tt.expectedMean) > tt.tolerance {
				t.Errorf("mean = %v, expected %v (tolerance %v)", mean, tt.expectedMean, tt.tolerance)
			}

			if math.Abs(stddev-tt.expectedStdDev) > tt.tolerance {
				t.Errorf("stddev = %v, expected %v (tolerance %v)", stddev, tt.expectedStdDev, tt.tolerance)
			}
		})
	}
}

// TestCalculateStatsNegativeValues tests handling of negative values
func TestCalculateStatsNegativeValues(t *testing.T) {
	tests := []struct {
		name           string
		values         []float64
		expectedMean   float64
		expectedStdDev float64
		tolerance      float64
	}{
		{
			name:           "all negative",
			values:         []float64{-5.0, -10.0, -15.0},
			expectedMean:   -10.0,
			expectedStdDev: 4.082,
			tolerance:      0.01,
		},
		{
			name:           "mixed positive negative symmetric",
			values:         []float64{-5.0, -3.0, -1.0, 1.0, 3.0, 5.0},
			expectedMean:   0.0,
			expectedStdDev: 3.4156,
			tolerance:      0.1,
		},
		{
			name:           "mostly negative",
			values:         []float64{-100.0, -50.0, 10.0},
			expectedMean:   -46.666667,
			expectedStdDev: 44.97, // population stddev
			tolerance:      0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mean, stddev := calculateStats(tt.values)

			if math.Abs(mean-tt.expectedMean) > tt.tolerance {
				t.Errorf("mean = %v, expected %v (tolerance %v)", mean, tt.expectedMean, tt.tolerance)
			}

			if math.Abs(stddev-tt.expectedStdDev) > tt.tolerance {
				t.Errorf("stddev = %v, expected %v (tolerance %v)", stddev, tt.expectedStdDev, tt.tolerance)
			}
		})
	}
}

// TestCalculateStatsDatabaseMetrics tests with realistic database metric values
func TestCalculateStatsDatabaseMetrics(t *testing.T) {
	tests := []struct {
		name           string
		values         []float64
		expectedMean   float64
		expectedStdDev float64
		tolerance      float64
	}{
		{
			name:           "cpu usage percent",
			values:         []float64{50.0, 55.0, 48.0, 52.0, 49.0, 53.0, 51.0, 47.0},
			expectedMean:   50.625,
			expectedStdDev: 2.5495,
			tolerance:      0.1,
		},
		{
			name:           "connection counts",
			values:         []float64{100.0, 150.0, 120.0, 180.0, 130.0},
			expectedMean:   136.0,
			expectedStdDev: 27.276,
			tolerance:      0.5,
		},
		{
			name:           "replication lag seconds",
			values:         []float64{0.1, 0.2, 0.15, 0.3, 0.25},
			expectedMean:   0.2,
			expectedStdDev: 0.07071,
			tolerance:      0.01,
		},
		{
			name:           "cache hit ratio",
			values:         []float64{99.1, 99.5, 98.8, 99.3, 99.0},
			expectedMean:   99.14,
			expectedStdDev: 0.2417, // population stddev
			tolerance:      0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mean, stddev := calculateStats(tt.values)

			if math.Abs(mean-tt.expectedMean) > tt.tolerance {
				t.Errorf("mean = %v, expected %v (tolerance %v)", mean, tt.expectedMean, tt.tolerance)
			}

			if math.Abs(stddev-tt.expectedStdDev) > tt.tolerance {
				t.Errorf("stddev = %v, expected %v (tolerance %v)", stddev, tt.expectedStdDev, tt.tolerance)
			}
		})
	}
}

// TestMinValueBasic tests basic minValue functionality
func TestMinValueBasic(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{"empty", []float64{}, 0},
		{"single", []float64{42.0}, 42.0},
		{"ascending", []float64{1.0, 2.0, 3.0, 4.0, 5.0}, 1.0},
		{"descending", []float64{5.0, 4.0, 3.0, 2.0, 1.0}, 1.0},
		{"min at end", []float64{5.0, 3.0, 8.0, 1.0}, 1.0},
		{"min at start", []float64{1.0, 3.0, 8.0, 5.0}, 1.0},
		{"min in middle", []float64{5.0, 1.0, 8.0, 3.0}, 1.0},
		{"duplicates", []float64{5.0, 1.0, 8.0, 1.0, 9.0}, 1.0},
		{"all same", []float64{7.0, 7.0, 7.0}, 7.0},
		{"with zero", []float64{5.0, 0.0, 3.0, 8.0}, 0.0},
		{"negative", []float64{-5.0, -3.0, -8.0, -1.0}, -8.0},
		{"mixed", []float64{-5.0, 3.0, -8.0, 10.0}, -8.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := minValue(tt.values)
			if result != tt.expected {
				t.Errorf("minValue(%v) = %v, expected %v", tt.values, result, tt.expected)
			}
		})
	}
}

// TestMaxValueBasic tests basic maxValue functionality
func TestMaxValueBasic(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{"empty", []float64{}, 0},
		{"single", []float64{42.0}, 42.0},
		{"ascending", []float64{1.0, 2.0, 3.0, 4.0, 5.0}, 5.0},
		{"descending", []float64{5.0, 4.0, 3.0, 2.0, 1.0}, 5.0},
		{"max at end", []float64{1.0, 3.0, 5.0, 9.0}, 9.0},
		{"max at start", []float64{9.0, 3.0, 5.0, 1.0}, 9.0},
		{"max in middle", []float64{1.0, 9.0, 5.0, 3.0}, 9.0},
		{"duplicates", []float64{5.0, 9.0, 8.0, 9.0, 1.0}, 9.0},
		{"all same", []float64{7.0, 7.0, 7.0}, 7.0},
		{"with zero", []float64{-5.0, 0.0, -3.0, -8.0}, 0.0},
		{"negative", []float64{-5.0, -3.0, -8.0, -1.0}, -1.0},
		{"mixed", []float64{-5.0, 3.0, -8.0, 10.0}, 10.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maxValue(tt.values)
			if result != tt.expected {
				t.Errorf("maxValue(%v) = %v, expected %v", tt.values, result, tt.expected)
			}
		})
	}
}

// TestMinMaxValueLargeSlices tests performance with large slices
func TestMinMaxValueLargeSlices(t *testing.T) {
	size := 10000
	values := make([]float64, size)
	for i := 0; i < size; i++ {
		values[i] = float64(i)
	}

	min := minValue(values)
	max := maxValue(values)

	if min != 0.0 {
		t.Errorf("minValue of [0..%d] = %v, expected 0.0", size-1, min)
	}

	if max != float64(size-1) {
		t.Errorf("maxValue of [0..%d] = %v, expected %v", size-1, max, float64(size-1))
	}
}

// TestCalculateStatsStdDevIsNotVariance verifies stddev is sqrt of variance
func TestCalculateStatsStdDevIsNotVariance(t *testing.T) {
	// For values [0, 10]: mean = 5, variance = 25, stddev = 5
	values := []float64{0.0, 10.0}
	mean, stddev := calculateStats(values)

	if mean != 5.0 {
		t.Errorf("mean = %v, expected 5.0", mean)
	}

	// stddev should be 5 (sqrt of 25), not 25 (variance)
	if stddev > 10.0 {
		t.Errorf("stddev = %v appears to be variance, not standard deviation", stddev)
	}

	if math.Abs(stddev-5.0) > 0.1 {
		t.Errorf("stddev = %v, expected approximately 5.0", stddev)
	}
}

// TestCalculateStatsRepeatedCallsConsistency ensures repeated calls produce same results
func TestCalculateStatsRepeatedCallsConsistency(t *testing.T) {
	values := []float64{10.0, 20.0, 30.0, 40.0, 50.0}

	mean1, stddev1 := calculateStats(values)
	mean2, stddev2 := calculateStats(values)

	if mean1 != mean2 {
		t.Errorf("calculateStats not consistent: mean %v != %v", mean1, mean2)
	}

	if stddev1 != stddev2 {
		t.Errorf("calculateStats not consistent: stddev %v != %v", stddev1, stddev2)
	}
}

// BenchmarkCalculateStatsLarge benchmarks with realistic dataset size
func BenchmarkCalculateStatsLarge(b *testing.B) {
	values := make([]float64, 1000)
	for i := range values {
		values[i] = float64(i%100) + 50.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculateStats(values)
	}
}

// BenchmarkMinValueLarge benchmarks minValue with large slice
func BenchmarkMinValueLarge(b *testing.B) {
	values := make([]float64, 1000)
	for i := range values {
		values[i] = float64(i%100) + 50.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		minValue(values)
	}
}

// BenchmarkMaxValueLarge benchmarks maxValue with large slice
func BenchmarkMaxValueLarge(b *testing.B) {
	values := make([]float64, 1000)
	for i := range values {
		values[i] = float64(i%100) + 50.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		maxValue(values)
	}
}

// TestEarliestTimestamp tests the earliestTimestamp helper directly.
// It verifies that the minimum CollectedAt value is returned regardless
// of slice ordering, and that an empty slice returns the zero time.
func TestEarliestTimestamp(t *testing.T) {
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		samples  []database.HistoricalMetricValue
		expected time.Time
	}{
		{
			name:     "empty slice returns zero time",
			samples:  nil,
			expected: time.Time{},
		},
		{
			name: "single sample",
			samples: []database.HistoricalMetricValue{
				{CollectedAt: base, Value: 1.0},
			},
			expected: base,
		},
		{
			name: "ordered ascending picks first",
			samples: []database.HistoricalMetricValue{
				{CollectedAt: base, Value: 1.0},
				{CollectedAt: base.Add(1 * time.Hour), Value: 2.0},
				{CollectedAt: base.Add(2 * time.Hour), Value: 3.0},
			},
			expected: base,
		},
		{
			name: "ordered descending picks last",
			samples: []database.HistoricalMetricValue{
				{CollectedAt: base.Add(2 * time.Hour), Value: 3.0},
				{CollectedAt: base.Add(1 * time.Hour), Value: 2.0},
				{CollectedAt: base, Value: 1.0},
			},
			expected: base,
		},
		{
			name: "unordered picks earliest",
			samples: []database.HistoricalMetricValue{
				{CollectedAt: base.Add(1 * time.Hour), Value: 2.0},
				{CollectedAt: base, Value: 1.0},
				{CollectedAt: base.Add(2 * time.Hour), Value: 3.0},
			},
			expected: base,
		},
		{
			name: "duplicate earliest timestamps",
			samples: []database.HistoricalMetricValue{
				{CollectedAt: base, Value: 1.0},
				{CollectedAt: base, Value: 2.0},
				{CollectedAt: base.Add(1 * time.Hour), Value: 3.0},
			},
			expected: base,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := earliestTimestamp(tt.samples)
			if !got.Equal(tt.expected) {
				t.Errorf("earliestTimestamp = %v, expected %v", got, tt.expected)
			}
		})
	}
}

// baselinesIntegrationSchema is the minimal schema required to exercise
// the baseline-build path end-to-end. It includes connections, alert_rules
// (so GetEnabledAlertRules returns something), metric_baselines, and the
// metrics.pg_stat_activity table used by the pg_stat_activity.count
// historical SQL branch.
const baselinesIntegrationSchema = `
DROP TABLE IF EXISTS metric_baselines CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_monitored BOOLEAN NOT NULL DEFAULT TRUE,
    connection_error TEXT
);

CREATE TABLE alert_rules (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    category VARCHAR(100) NOT NULL DEFAULT 'general',
    metric_name VARCHAR(255) NOT NULL,
    default_operator VARCHAR(10) NOT NULL DEFAULT '>',
    default_threshold REAL NOT NULL DEFAULT 0,
    default_severity VARCHAR(20) NOT NULL DEFAULT 'warning',
    default_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    required_extension VARCHAR(100),
    is_built_in BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE metric_baselines (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    metric_name TEXT NOT NULL,
    period_type TEXT NOT NULL,
    day_of_week INTEGER,
    hour_of_day INTEGER,
    mean REAL NOT NULL,
    stddev REAL NOT NULL,
    min REAL NOT NULL,
    max REAL NOT NULL,
    sample_count BIGINT NOT NULL DEFAULT 0,
    last_calculated TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    earliest_sample_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_metric_baselines_unique
    ON metric_baselines(
        connection_id,
        COALESCE(database_name, ''),
        metric_name,
        period_type,
        COALESCE(day_of_week, -1),
        COALESCE(hour_of_day, -1)
    );

CREATE SCHEMA metrics;

CREATE TABLE metrics.pg_stat_activity (
    connection_id INTEGER NOT NULL,
    backend_type TEXT,
    state TEXT,
    wait_event_type TEXT,
    xact_start TIMESTAMPTZ,
    query_start TIMESTAMPTZ,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const baselinesIntegrationTeardown = `
DROP SCHEMA IF EXISTS metrics CASCADE;
DROP TABLE IF EXISTS metric_baselines CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// newBaselinesIntegrationEnv creates the integration-test environment for
// the baseline-build path. The test is skipped if TEST_AI_WORKBENCH_SERVER
// is not set or the database is unreachable.
func newBaselinesIntegrationEnv(t *testing.T) (*Engine, *database.Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping baselines integration test")
	}

	// Safety guard: refuse to run destructive DDL on anything other
	// than the local test database. CLAUDE.local.md is explicit that
	// regression tests must target 127.0.0.1/ai_workbench only;
	// accidentally pointing TEST_AI_WORKBENCH_SERVER at any shared
	// instance would have these tests wipe its schema.
	assertLocalTestDSN(t, connStr)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, baselinesIntegrationSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create baselines integration schema: %v", err)
	}

	ds := database.NewTestDatastore(pool)

	cfg := config.NewConfig()
	cfg.Anomaly.Enabled = false
	cfg.Anomaly.Tier2.Enabled = false
	cfg.Anomaly.Tier3.Enabled = false
	cfg.Baselines.LookbackDays = 7

	engine := NewEngine(cfg, ds, false)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), baselinesIntegrationTeardown); err != nil {
			t.Logf("baselines integration teardown failed: %v", err)
		}
		pool.Close()
	}

	return engine, ds, pool, cleanup
}

// insertBaselinesTestConnection inserts a connection row that is
// monitored and enabled, returning its id.
func insertBaselinesTestConnection(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name, enabled, is_monitored) VALUES ($1, TRUE, TRUE) RETURNING id`,
		name).Scan(&id)
	if err != nil {
		t.Fatalf("Failed to insert connection %q: %v", name, err)
	}
	return id
}

// insertBaselinesTestAlertRule inserts an alert rule so GetEnabledAlertRules
// will return it during calculateBaselines.
func insertBaselinesTestAlertRule(t *testing.T, pool *pgxpool.Pool, name, metricName string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO alert_rules (name, description, category, metric_name, default_operator,
		    default_threshold, default_severity, default_enabled, is_built_in)
		VALUES ($1, 'Test rule', 'test', $2, '>', 0, 'warning', TRUE, FALSE)
	`, name, metricName)
	if err != nil {
		t.Fatalf("Failed to insert alert rule %q: %v", name, err)
	}
}

// seedStatActivitySample writes one client-backend row at the given
// collected_at timestamp. The pg_stat_activity.count historical SQL
// groups by (connection_id, collected_at), so one row per timestamp
// produces one sample with value=1.
func seedStatActivitySample(t *testing.T, pool *pgxpool.Pool, connID int, collectedAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO metrics.pg_stat_activity (connection_id, backend_type, collected_at)
		VALUES ($1, 'client backend', $2)
	`, connID, collectedAt)
	if err != nil {
		t.Fatalf("Failed to seed pg_stat_activity sample at %v: %v", collectedAt, err)
	}
}

// TestBaselineBuildPersistsEarliestSampleAt verifies that calculateBaselines
// captures the minimum sample timestamp once per (connection, metric) and
// writes the same value onto every period_type row (all, hourly, daily).
func TestBaselineBuildPersistsEarliestSampleAt(t *testing.T) {
	engine, _, pool, cleanup := newBaselinesIntegrationEnv(t)
	defer cleanup()

	ctx := context.Background()

	connID := insertBaselinesTestConnection(t, pool, "baseline-earliest-conn")
	insertBaselinesTestAlertRule(t, pool, "baseline_earliest_rule", "pg_stat_activity.count")

	// Three samples spaced 10 hours apart so the earliest is well inside
	// the 7-day lookback window. Truncate to the second so the read-back
	// comparison is exact.
	earliest := time.Now().UTC().Add(-30 * time.Hour).Truncate(time.Second)
	timestamps := []time.Time{
		earliest,
		earliest.Add(10 * time.Hour),
		earliest.Add(20 * time.Hour),
	}
	for _, ts := range timestamps {
		seedStatActivitySample(t, pool, connID, ts)
	}

	engine.calculateBaselines(ctx)

	baselines, err := engine.datastore.GetMetricBaselines(ctx, connID, "pg_stat_activity.count")
	if err != nil {
		t.Fatalf("GetMetricBaselines failed: %v", err)
	}
	if len(baselines) == 0 {
		t.Fatal("Expected at least one baseline row, got 0")
	}

	// Ensure we exercised all three period_type code paths. Hourly and
	// daily baselines require minSamplesForTimePeriod=3 samples falling
	// in the same hour/day bucket; the three samples above are 10h apart
	// so they land in three distinct hours but, depending on wall-clock
	// time, may land in fewer than three distinct weekdays. Track which
	// period types we saw and assert 'all' is always present.
	periodSeen := make(map[string]bool)
	for _, b := range baselines {
		periodSeen[b.PeriodType] = true
		if b.EarliestSampleAt.IsZero() {
			t.Errorf("baseline period_type=%s has zero EarliestSampleAt; want %v",
				b.PeriodType, earliest)
			continue
		}
		if !b.EarliestSampleAt.UTC().Equal(earliest) {
			t.Errorf("baseline period_type=%s EarliestSampleAt = %v, want %v",
				b.PeriodType, b.EarliestSampleAt.UTC(), earliest)
		}
	}
	if !periodSeen["all"] {
		t.Error("Expected an 'all' period_type baseline row, got none")
	}
}

// TestBaselineBuildSharesEarliestAcrossPeriodTypes seeds enough samples
// to exercise the hourly and daily code paths (which require at least
// three samples in the same hour-of-day or day-of-week bucket) and
// verifies that every period_type row carries the same EarliestSampleAt
// value derived from the minimum collected_at across the raw samples.
func TestBaselineBuildSharesEarliestAcrossPeriodTypes(t *testing.T) {
	engine, _, pool, cleanup := newBaselinesIntegrationEnv(t)
	defer cleanup()

	// Widen the lookback so we can space samples a week apart and have
	// at least three land on the same weekday inside the window.
	cfg := engine.getConfig()
	cfg.Baselines.LookbackDays = 30
	engine.ReloadConfig(cfg)

	ctx := context.Background()

	connID := insertBaselinesTestConnection(t, pool, "baseline-shared-conn")
	insertBaselinesTestAlertRule(t, pool, "baseline_shared_rule", "pg_stat_activity.count")

	// Three samples spaced exactly 7 days apart so every sample lands in
	// the same hour-of-day and the same day-of-week bucket. That
	// guarantees minSamplesForTimePeriod=3 is met for one hour and one
	// weekday, exercising both the hourly and daily upsert branches.
	earliest := time.Now().UTC().Add(-21 * 24 * time.Hour).Truncate(time.Second)
	for i := 0; i < 3; i++ {
		seedStatActivitySample(t, pool, connID, earliest.Add(time.Duration(i)*7*24*time.Hour))
	}

	engine.calculateBaselines(ctx)

	baselines, err := engine.datastore.GetMetricBaselines(ctx, connID, "pg_stat_activity.count")
	if err != nil {
		t.Fatalf("GetMetricBaselines failed: %v", err)
	}

	periodSeen := make(map[string]bool)
	for _, b := range baselines {
		periodSeen[b.PeriodType] = true
		if b.EarliestSampleAt.IsZero() {
			t.Errorf("baseline period_type=%s has zero EarliestSampleAt; want %v",
				b.PeriodType, earliest)
			continue
		}
		if !b.EarliestSampleAt.UTC().Equal(earliest) {
			t.Errorf("baseline period_type=%s EarliestSampleAt = %v, want %v",
				b.PeriodType, b.EarliestSampleAt.UTC(), earliest)
		}
	}

	for _, period := range []string{"all", "hourly", "daily"} {
		if !periodSeen[period] {
			t.Errorf("Expected a %q period_type baseline row, got none", period)
		}
	}
}

// TestBaselineBuildNullSafeForEmptyMetric verifies that calculateBaselines
// does not persist any baseline row when no samples exist for the metric.
func TestBaselineBuildNullSafeForEmptyMetric(t *testing.T) {
	engine, _, pool, cleanup := newBaselinesIntegrationEnv(t)
	defer cleanup()

	ctx := context.Background()

	connID := insertBaselinesTestConnection(t, pool, "baseline-empty-conn")
	insertBaselinesTestAlertRule(t, pool, "baseline_empty_rule", "pg_stat_activity.count")

	// Deliberately seed no samples.
	engine.calculateBaselines(ctx)

	baselines, err := engine.datastore.GetMetricBaselines(ctx, connID, "pg_stat_activity.count")
	if err != nil {
		t.Fatalf("GetMetricBaselines failed: %v", err)
	}
	if len(baselines) != 0 {
		t.Errorf("Expected 0 baselines for empty metric, got %d", len(baselines))
	}
}

// assertLocalTestDSN fails the test fast if the supplied DSN points
// at anything other than a local loopback host and one of the known
// safe test database names. CLAUDE.local.md is explicit that the
// alerter integration tests must only target a local loopback
// Postgres; the destructive DDL embedded in the integration schemas
// would wipe any other instance the env var resolved to. The
// loopback-only host check is the primary safety net. The database
// allowlist is intentionally tiny: "ai_workbench" for local dev and
// "postgres" for CI (the default database created by the postgres
// Docker image). Shared across integration helpers in the engine
// package.
func assertLocalTestDSN(t *testing.T, dsn string) {
	t.Helper()
	allowedHosts := map[string]struct{}{
		"127.0.0.1": {},
		"localhost": {},
		"":          {}, // unix socket; only reachable on this host
	}
	allowedDBs := map[string]struct{}{
		"ai_workbench": {},
		"postgres":     {},
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	host := cfg.ConnConfig.Host
	if _, ok := allowedHosts[host]; !ok {
		t.Fatalf("refusing to run destructive integration tests "+
			"against non-loopback host %q; set "+
			"TEST_AI_WORKBENCH_SERVER to a "+
			"postgresql://...@127.0.0.1 DSN", host)
	}
	if _, ok := allowedDBs[cfg.ConnConfig.Database]; !ok {
		t.Fatalf("refusing to run destructive integration tests "+
			"against database %q; expected one of: "+
			"ai_workbench, postgres",
			cfg.ConnConfig.Database)
	}
}
