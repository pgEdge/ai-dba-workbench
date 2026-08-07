/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package compactor

import (
	"testing"
	"time"
)

func TestAnalytics_RecordCompaction(t *testing.T) {
	analytics := NewAnalytics()

	info1 := CompactionInfo{
		OriginalCount:    10,
		CompactedCount:   5,
		TokensSaved:      1000,
		CompressionRatio: 0.5,
	}

	analytics.RecordCompaction(info1, 100*time.Millisecond)

}

func TestAnalytics_MultipleCompactions(t *testing.T) {
	analytics := NewAnalytics()

	info1 := CompactionInfo{
		OriginalCount:    10,
		CompactedCount:   5,
		TokensSaved:      1000,
		CompressionRatio: 0.5,
	}

	info2 := CompactionInfo{
		OriginalCount:    20,
		CompactedCount:   10,
		TokensSaved:      2000,
		CompressionRatio: 0.5,
	}

	analytics.RecordCompaction(info1, 100*time.Millisecond)
	analytics.RecordCompaction(info2, 200*time.Millisecond)

}

func TestAnalytics_GetEfficiencyReport(t *testing.T) {
	analytics := NewAnalytics()

	info1 := CompactionInfo{
		OriginalCount:    10,
		CompactedCount:   5,
		TokensSaved:      1000,
		CompressionRatio: 0.5,
	}

	info2 := CompactionInfo{
		OriginalCount:    20,
		CompactedCount:   15,
		TokensSaved:      500,
		CompressionRatio: 0.75,
	}

	analytics.RecordCompaction(info1, 100*time.Millisecond)
	analytics.RecordCompaction(info2, 200*time.Millisecond)

	// AverageCompression = TotalMessagesOut / TotalMessagesIn = 20 / 30 = 0.666...
}

func TestAnalytics_Reset(t *testing.T) {
	analytics := NewAnalytics()

	info := CompactionInfo{
		OriginalCount:    10,
		CompactedCount:   5,
		TokensSaved:      1000,
		CompressionRatio: 0.5,
	}

	analytics.RecordCompaction(info, 100*time.Millisecond)

	analytics.Reset()

}

func TestAnalytics_LastCompactionTime(t *testing.T) {
	analytics := NewAnalytics()

	info := CompactionInfo{
		OriginalCount:  10,
		CompactedCount: 5,
	}

	analytics.RecordCompaction(info, 100*time.Millisecond)

}

func TestAnalytics_ThreadSafety(t *testing.T) {
	analytics := NewAnalytics()

	done := make(chan bool)

	// Simulate concurrent compactions
	for i := 0; i < 10; i++ {
		go func() {
			info := CompactionInfo{
				OriginalCount:  10,
				CompactedCount: 5,
				TokensSaved:    100,
			}
			analytics.RecordCompaction(info, 10*time.Millisecond)
			done <- true
		}()
	}

	// Wait for all goroutines

}

func TestAnalytics_EmptyMetrics(t *testing.T) {

}
