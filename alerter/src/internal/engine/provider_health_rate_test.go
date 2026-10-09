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
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// The tests in this file cover the failure rate that GitHub issue #593
// added alongside the consecutive failure count.

// countNotes counts the harness's notifications of typ.
func (h *trackerHarness) countNotes(typ database.NotificationType) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, note := range h.notes {
		if note.typ == typ {
			n++
		}
	}
	return n
}

// play records one call per character of pattern, 'F' a failure and
// 'S' a success, advancing the clock by step before each call.
func (h *trackerHarness) play(pattern string, now *time.Time, step time.Duration) {
	for _, c := range pattern {
		*now = now.Add(step)
		if c == 'F' {
			h.fail("401 Unauthorized")
		} else {
			h.succeed()
		}
	}
}

// withClock gives the harness a clock the test advances.
func (h *trackerHarness) withClock() *time.Time {
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	h.tracker.now = func() time.Time { return now }
	return &now
}

// TestProviderHealth_AlternatingFailuresRaiseOnRate is the case from
// the review that led to issue #593: alternating 401 and 200 responses
// never fail three times in a row, so the consecutive count alone
// raises nothing, whilst the failure rate raises the alert once and
// holds it until the provider recovers.
func TestProviderHealth_AlternatingFailuresRaiseOnRate(t *testing.T) {
	t.Run("consecutive count alone misses it", func(t *testing.T) {
		h := newTrackerHarness(3)
		h.settings.FailureRate = 1
		h.play(strings.Repeat("FS", 20), h.withClock(), time.Minute)
		if h.store.creates != 0 {
			t.Fatalf("alert raised %d times without the failure rate", h.store.creates)
		}
	})

	h := newTrackerHarness(3)
	now := h.withClock()

	// With the default rate (30% of 20 calls, so 6 failures) the sixth
	// failure, on the eleventh call, raises the alert.
	h.play(strings.Repeat("FS", 5), now, time.Minute)
	if h.open() != nil {
		t.Fatal("alert raised before six failures")
	}
	h.play("F", now, time.Minute)
	alert := h.open()
	if alert == nil {
		t.Fatal("alternating failures did not raise the alert")
	}
	for _, want := range []string{
		"6 of the last 11 tier 2 embedding calls to provider openai",
		"clears once fewer than 6 of the last 20 calls",
		"401 Unauthorized",
	} {
		if !strings.Contains(alert.Description, want) {
			t.Errorf("description %q lacks %q", alert.Description, want)
		}
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(*alert.AnomalyDetails), &details); err != nil {
		t.Fatalf("details: %v", err)
	}
	if details["recent_failures"] != float64(6) || details["recent_calls"] != float64(11) ||
		details["consecutive_failures"] != float64(1) {
		t.Errorf("details = %v", details)
	}

	// The provider keeps alternating for an hour: the alert stays open,
	// with one fire and no clear, and its text follows the counts.
	h.play(strings.Repeat("SF", 30), now, time.Minute)
	if h.open() == nil || h.countNotes(database.NotificationTypeAlertFire) != 1 ||
		h.countNotes(database.NotificationTypeAlertClear) != 0 {
		t.Fatalf("open %v, notes %v whilst alternating", h.open() != nil, h.notes)
	}
	if !strings.Contains(h.open().Description, "10 of the last 20") {
		t.Errorf("description not refreshed: %q", h.open().Description)
	}

	// The provider recovers: the alert clears once fewer than six of
	// the last 20 calls have failed, which takes ten successes.
	for i := 1; i <= 10; i++ {
		h.play("S", now, time.Minute)
		if open := h.open() != nil; open != (i < 10) {
			t.Fatalf("after %d successes open = %v", i, open)
		}
	}
	if h.countNotes(database.NotificationTypeAlertClear) != 1 ||
		!h.loggedContaining("a call succeeded, and 5 of the last 20 calls failed") {
		t.Errorf("notes %v, clear not logged with the counts", h.notes)
	}
}

// TestProviderHealth_FailureRateDampsFlapping pins that a provider
// failing about half its calls, with occasional runs long enough to
// reach the threshold, fires and clears the alert far less often than
// the consecutive count alone lets it.
func TestProviderHealth_FailureRateDampsFlapping(t *testing.T) {
	// Half the calls fail, in runs of one to three; each cycle of 12
	// calls has one run that reaches the threshold of three.
	pattern := strings.Repeat("FFFSFSSFSFSS", 20)

	run := func(rate float64) *trackerHarness {
		h := newTrackerHarness(3)
		h.settings.FailureRate = rate
		h.play(pattern, h.withClock(), time.Minute)
		return h
	}

	// The consecutive count alone fires and clears once per cycle
	// whose run starts after the five-minute cooldown, every one.
	without := run(1)
	if fires := without.countNotes(database.NotificationTypeAlertFire); fires != 20 {
		t.Errorf("without the failure rate: %d fires, want 20", fires)
	}

	// With the failure rate, the run in each of the first two cycles
	// comes from a provider with fewer than six other recent failures,
	// so it folds into one failure and its success clears the alert as
	// before. By the third cycle the steady failures reach the limit,
	// and the alert stays open from then on.
	with := run(config.DefaultProviderFailureRate)
	if fires := with.countNotes(database.NotificationTypeAlertFire); fires != 3 {
		t.Errorf("with the failure rate: %d fires, want 3", fires)
	}
	if clears := with.countNotes(database.NotificationTypeAlertClear); clears != 2 {
		t.Errorf("with the failure rate: %d clears, want 2", clears)
	}
	if with.open() == nil {
		t.Error("alert not open at the end of the flapping")
	}
}

// TestProviderHealth_FailureRateHoldsAFlakyProviderOpen covers a
// provider that fails three calls in four: each run reaches the
// threshold, and once the provider's failures outside the run reach the
// limit, the success after a run no longer clears the alert.
func TestProviderHealth_FailureRateHoldsAFlakyProviderOpen(t *testing.T) {
	h := newTrackerHarness(3)
	now := h.withClock()

	// The first run raises and its success clears; the next runs fall
	// in the cooldown, and fold into single failures until five of
	// them are recorded.
	h.play(strings.Repeat("FFFS", 6), now, 10*time.Second)
	if h.store.creates != 1 || h.open() != nil {
		t.Fatalf("creates %d, open %v inside the cooldown", h.store.creates, h.open() != nil)
	}

	// After the cooldown the first failure raises on the rate, and the
	// following successes leave the alert open.
	*now = now.Add(AlertCooldownPeriod)
	h.play("F", now, time.Second)
	if h.open() == nil || !strings.Contains(h.open().Description, "of the last") {
		t.Fatalf("alert not raised on the rate after the cooldown: %+v", h.open())
	}
	h.play(strings.Repeat("FFSF", 10), now, time.Minute)
	if h.open() == nil || h.store.creates != 2 ||
		h.countNotes(database.NotificationTypeAlertClear) != 1 {
		t.Errorf("open %v, creates %d, notes %v", h.open() != nil, h.store.creates, h.notes)
	}
}

// TestProviderHealth_OutageRecoveryClearsAtOnce pins the behavior the
// failure rate keeps for a provider that fails consecutively: after an
// outage longer than the window, the first success clears the alert,
// and a later single failure does not raise it again.
func TestProviderHealth_OutageRecoveryClearsAtOnce(t *testing.T) {
	h := newTrackerHarness(3)
	now := h.withClock()
	h.play(strings.Repeat("S", 30)+strings.Repeat("F", 25), now, time.Minute)
	if alert := h.open(); alert == nil || !strings.Contains(alert.Description, "25 consecutive") ||
		!strings.Contains(alert.Description, "clears on the next successful call to the provider, "+
			"unless 6 or more of its last 20 calls are failing") {
		t.Fatalf("alert after the outage = %+v", alert)
	}

	h.play("S", now, time.Minute)
	if h.open() != nil {
		t.Fatal("alert not cleared by the first success after the outage")
	}
	if got := len(h.tracker.state(providerHealthKey(providerTierEmbedding, "openai")).outcomes); got != 2 {
		t.Errorf("outcomes after recovery = %d, want the outage folded into one", got)
	}

	// Long after the cooldown, isolated failures stay below the limit.
	*now = now.Add(time.Hour)
	h.play("FSSSFSSSFSSSFSSS", now, time.Minute)
	if h.store.creates != 1 {
		t.Errorf("alert raised again by isolated failures after recovery: creates %d", h.store.creates)
	}
}

// TestProviderHealth_OutageOfAFlakyProviderStaysOpen pins that a run of
// failures from a provider already failing at the rate is kept whole,
// so the success that ends it does not clear the alert.
func TestProviderHealth_OutageOfAFlakyProviderStaysOpen(t *testing.T) {
	h := newTrackerHarness(3)
	now := h.withClock()
	h.play(strings.Repeat("FS", 10)+"FFFF", now, time.Minute)
	if h.open() == nil {
		t.Fatal("alert not raised")
	}
	h.play("S", now, time.Minute)
	if h.open() == nil {
		t.Fatal("success after a run cleared the alert of a provider failing at the rate")
	}
	if !strings.Contains(h.store.alerts[1].Description, "4 consecutive") {
		t.Errorf("description = %q", h.store.alerts[1].Description)
	}
	// The next failure reports the rate again.
	h.play("F", now, time.Minute)
	if !strings.Contains(h.open().Description, "of the last 20") {
		t.Errorf("description after the run = %q", h.open().Description)
	}
}

// TestProviderHealth_StartupFailureClearsOnSuccess pins that a failed
// startup check, which raises at once, still clears on the next success.
func TestProviderHealth_StartupFailureClearsOnSuccess(t *testing.T) {
	h := newTrackerHarness(3)
	h.tracker.record(t.Context(), providerTierEmbedding, "openai",
		"text-embedding-3-small", errStartup, true)
	alert := h.open()
	if alert == nil || !strings.Contains(alert.Description, "unless 6 or more of its last 20 calls") {
		t.Fatalf("startup alert = %+v", alert)
	}
	h.succeed()
	if h.open() != nil {
		t.Error("startup alert not cleared by a success")
	}
}

// errStartup is a provider failure for the tests that record calls
// directly.
var errStartup = errors.New("model retired")

// TestProviderHealth_SharedSuccessCountsTowardsTheRate pins that a
// re-evaluation success is recorded against Tier 3's recent calls too,
// as it shares the reasoning provider.
func TestProviderHealth_SharedSuccessCountsTowardsTheRate(t *testing.T) {
	h := newTrackerHarness(3)
	h.settings.FailureRateWindow = 4
	h.settings.FailureRate = 0.5
	cls := providerHealthKey(providerTierClassification, "anthropic")
	record := func(tier providerTier, err error) {
		h.tracker.record(withProviderTier(t.Context(), tier), tier, "anthropic", "claude-x", err, false)
	}
	record(providerTierClassification, errStartup)
	record(providerTierClassification, nil)
	record(providerTierClassification, errStartup)
	if h.store.openFor(cls) == nil {
		t.Fatal("two of three Tier 3 calls failing did not raise at a rate of 0.5")
	}
	record(providerTierReevaluation, nil)
	if h.store.openFor(cls) == nil {
		t.Fatal("re-evaluation success cleared Tier 3 with 2 of 4 calls failing")
	}
	record(providerTierReevaluation, nil)
	if h.store.openFor(cls) != nil {
		t.Error("Tier 3 alert not cleared once its failures fell below the limit")
	}
}

func TestFailureLimit(t *testing.T) {
	tests := []struct {
		rate   float64
		window int
		want   int
	}{
		{0.3, 20, 6},
		{0.5, 20, 10},
		{0.51, 20, 11},
		{1, 20, 20},
		{0.01, 20, 1},
		{0.3, 1, 1},
		{1.0 / 3, 3, 1},
		{0.7, 10, 7},
		// Products that floating point puts a hair above a whole
		// number, which round up one too far without the tolerance.
		{0.28, 25, 7},
		{0.56, 25, 14},
		// A share so small that the tolerance takes the product
		// below zero still needs one failure.
		{1e-12, 1, 1},
	}
	for _, tt := range tests {
		if got := failureLimit(tt.rate, tt.window); got != tt.want {
			t.Errorf("failureLimit(%v, %d) = %d, want %d", tt.rate, tt.window, got, tt.want)
		}
	}
}

func TestProviderHealthLimitsFallBackToDefaults(t *testing.T) {
	def := providerHealthLimits{
		threshold: config.DefaultProviderFailureThreshold,
		window:    config.DefaultProviderFailureRateWindow,
		limit: failureLimit(config.DefaultProviderFailureRate,
			config.DefaultProviderFailureRateWindow),
	}
	tests := []struct {
		name string
		cfg  config.ProviderHealthConfig
		want providerHealthLimits
	}{
		{"zero values", config.ProviderHealthConfig{}, def},
		{"window too large", config.ProviderHealthConfig{
			FailureRateWindow: config.MaxProviderFailureRateWindow + 1, FailureRate: 0.3}, def},
		{"rate NaN", config.ProviderHealthConfig{FailureRateWindow: 20, FailureRate: math.NaN()}, def},
		{"rate above 1", config.ProviderHealthConfig{FailureRateWindow: 20, FailureRate: 1.5}, def},
		{"valid", config.ProviderHealthConfig{FailureThreshold: 5, FailureRateWindow: 10, FailureRate: 0.5},
			providerHealthLimits{threshold: 5, window: 10, limit: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newProviderHealthTracker(nil, nil,
				func() config.ProviderHealthConfig { return tt.cfg }, nil, nil)
			if got := tracker.limits(); got != tt.want {
				t.Errorf("limits() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestProviderHealthStateWindow(t *testing.T) {
	t.Run("push keeps the most recent outcomes", func(t *testing.T) {
		st := &providerHealthState{}
		for i := 0; i < 25; i++ {
			st.push(i%5 == 0, 20)
		}
		if len(st.outcomes) != 20 || st.recentFailures() != 4 {
			t.Errorf("outcomes %d, failures %d", len(st.outcomes), st.recentFailures())
		}
		// A reload that shrinks the window drops the oldest outcomes.
		st.push(true, 5)
		if len(st.outcomes) != 5 || st.recentFailures() != 1 || !st.outcomes[4] {
			t.Errorf("after shrinking: outcomes %v", st.outcomes)
		}
	})
	t.Run("endRun", func(t *testing.T) {
		tests := []struct {
			name     string
			outcomes string
			run      int
			want     string
		}{
			{"short run kept", "SSFF", 2, "SSFF"},
			{"run of one kept", "SSF", 1, "SSF"},
			{"run folded", "SSFFF", 3, "SSF"},
			{"run longer than the window folded", "FFFF", 9, "F"},
			{"run of a failing provider kept", "FFFFFSFFF", 3, "FFFFFSFFF"},
			{"run folded just below the limit", "FFFFSFFF", 3, "FFFFSF"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				st := &providerHealthState{}
				for _, c := range tt.outcomes {
					st.push(c == 'F', 20)
				}
				st.endRun(tt.run, 3, 6)
				var got strings.Builder
				for _, failed := range st.outcomes {
					if failed {
						got.WriteByte('F')
					} else {
						got.WriteByte('S')
					}
				}
				if got.String() != tt.want {
					t.Errorf("outcomes = %s, want %s", got.String(), tt.want)
				}
			})
		}
	})
}
