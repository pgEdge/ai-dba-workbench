/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/overview"
)

// TestOverviewRateLimits checks that the AI Overview limits are read
// from llm.rate_limit.overview, and that a missing configuration falls
// back to the defaults rather than leaving the endpoints unlimited.
func TestOverviewRateLimits(t *testing.T) {
	defaults := overview.RateLimits{
		GenerationsPerHour: config.DefaultOverviewGenerationsPerHour,
		Burst:              config.DefaultOverviewGenerationBurst,
		RefreshInterval:    config.DefaultOverviewRefreshIntervalSeconds * time.Second,
	}
	if got := overviewRateLimits(nil); got != defaults {
		t.Errorf("overviewRateLimits(nil) = %+v, want %+v", got, defaults)
	}
	if got := overviewRateLimits(&config.Config{}); got != defaults {
		t.Errorf("overviewRateLimits(empty) = %+v, want %+v", got, defaults)
	}

	perHour, burst, refresh := 12, 3, 0
	cfg := &config.Config{}
	cfg.LLM.RateLimit.Overview = config.OverviewRateLimitConfig{
		GenerationsPerHourPtr:     &perHour,
		BurstPtr:                  &burst,
		RefreshIntervalSecondsPtr: &refresh,
	}
	want := overview.RateLimits{GenerationsPerHour: 12, Burst: 3, RefreshInterval: 0}
	if got := overviewRateLimits(cfg); got != want {
		t.Errorf("overviewRateLimits(cfg) = %+v, want %+v", got, want)
	}
}
