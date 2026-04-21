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
	"math"
	"testing"
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
