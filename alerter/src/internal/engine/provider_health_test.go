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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// fakeSystemAlertStore is an in-memory systemAlertStore. Each err field
// makes the matching method fail.
type fakeSystemAlertStore struct {
	mu     sync.Mutex
	nextID int64
	alerts map[int64]*database.Alert

	getOpenErr  error
	listErr     error
	createErr   error
	updateErr   error
	clearErr    error
	getAlertErr error

	// createConflict makes CreateSystemAlert behave as though another
	// process had raised the alert first.
	createConflict bool

	gets, creates, updates, clears int
}

func newFakeSystemAlertStore() *fakeSystemAlertStore {
	return &fakeSystemAlertStore{alerts: make(map[int64]*database.Alert)}
}

// put stores an open system alert for key and returns it.
func (f *fakeSystemAlertStore) put(key string) *database.Alert {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	k := key
	a := &database.Alert{ID: f.nextID, AlertType: database.AlertTypeSystem,
		MetricName: &k, Status: "active"}
	f.alerts[a.ID] = a
	return a
}

func (f *fakeSystemAlertStore) openFor(key string) *database.Alert {
	for _, a := range f.alerts {
		if a.MetricName != nil && *a.MetricName == key && a.Status != "cleared" {
			return a
		}
	}
	return nil
}

func (f *fakeSystemAlertStore) GetOpenSystemAlert(_ context.Context, key string) (*database.Alert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.getOpenErr != nil {
		return nil, f.getOpenErr
	}
	return f.openFor(key), nil
}

func (f *fakeSystemAlertStore) GetOpenSystemAlerts(_ context.Context, prefix string) ([]*database.Alert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*database.Alert
	for _, a := range f.alerts {
		if a.MetricName != nil && strings.HasPrefix(*a.MetricName, prefix) && a.Status != "cleared" {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeSystemAlertStore) CreateSystemAlert(_ context.Context, alert *database.Alert) (*database.Alert, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	if f.createErr != nil {
		return nil, false, f.createErr
	}
	if existing := f.openFor(*alert.MetricName); existing != nil {
		return existing, false, nil
	}
	f.nextID++
	alert.ID = f.nextID
	alert.AlertType = database.AlertTypeSystem
	alert.Status = "active"
	f.alerts[alert.ID] = alert
	if f.createConflict {
		return alert, false, nil
	}
	return alert, true, nil
}

func (f *fakeSystemAlertStore) UpdateSystemAlert(_ context.Context, id int64, description string, details *string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates++
	if f.updateErr != nil {
		return f.updateErr
	}
	a := f.alerts[id]
	if a == nil || a.Status == "cleared" {
		return database.ErrSystemAlertNotOpen
	}
	a.Description = description
	a.AnomalyDetails = details
	return nil
}

func (f *fakeSystemAlertStore) ClearAlert(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clears++
	if f.clearErr != nil {
		return f.clearErr
	}
	f.alerts[id].Status = "cleared"
	return nil
}

func (f *fakeSystemAlertStore) GetAlert(_ context.Context, id int64) (*database.Alert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getAlertErr != nil {
		return nil, f.getAlertErr
	}
	return f.alerts[id], nil
}

// notification is one queued notification.
type notification struct {
	id  int64
	typ database.NotificationType
}

// trackerHarness wires a tracker to a fake store and records what it
// notifies and logs.
type trackerHarness struct {
	store     *fakeSystemAlertStore
	tracker   *providerHealthTracker
	threshold int
	secrets   []string

	mu     sync.Mutex
	notes  []notification
	logged []string
}

func newTrackerHarness(threshold int) *trackerHarness {
	h := &trackerHarness{store: newFakeSystemAlertStore(), threshold: threshold}
	h.tracker = newProviderHealthTracker(h.store,
		func(a *database.Alert, typ database.NotificationType) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.notes = append(h.notes, notification{a.ID, typ})
		},
		func() int { return h.threshold },
		func() []string { return h.secrets },
		func(format string, args ...any) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.logged = append(h.logged, fmt.Sprintf(format, args...))
		})
	return h
}

func (h *trackerHarness) fail(msg string) {
	h.tracker.record(context.Background(), providerTierEmbedding, "openai",
		"text-embedding-3-small", errors.New(msg), false)
}

func (h *trackerHarness) succeed() {
	h.tracker.record(context.Background(), providerTierEmbedding, "openai",
		"text-embedding-3-small", nil, false)
}

func (h *trackerHarness) open() *database.Alert {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	return h.store.openFor(providerHealthKey(providerTierEmbedding, "openai"))
}

func (h *trackerHarness) loggedContaining(s string) bool {
	return h.countLogged(s) > 0
}

func (h *trackerHarness) countLogged(s string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, l := range h.logged {
		if strings.Contains(l, s) {
			n++
		}
	}
	return n
}

// disableCooldown lets a test raise an alert again straight after a
// clear.
func (h *trackerHarness) disableCooldown() {
	h.tracker.cooldown = 0
}

func TestProviderHealth_RaisesAtThresholdAndClearsOnSuccess(t *testing.T) {
	h := newTrackerHarness(3)

	h.fail("boom")
	h.fail("boom")
	if h.open() != nil {
		t.Fatal("alert raised before the threshold")
	}
	h.fail("model gpt-x does not exist")
	alert := h.open()
	if alert == nil {
		t.Fatal("no alert at the threshold")
	}
	if alert.Severity != "warning" || !strings.Contains(alert.Title, "Tier 2 embedding") ||
		!strings.Contains(alert.Title, "openai") {
		t.Errorf("unexpected alert: severity %q, title %q", alert.Severity, alert.Title)
	}
	if alert.ObjectName == nil || *alert.ObjectName != "openai/text-embedding-3-small" {
		t.Errorf("object name = %v", alert.ObjectName)
	}
	if !strings.Contains(alert.Description, "3 consecutive") ||
		!strings.Contains(alert.Description, "model gpt-x does not exist") {
		t.Errorf("description = %q", alert.Description)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(*alert.AnomalyDetails), &details); err != nil {
		t.Fatalf("details: %v", err)
	}
	if details["tier"] != "tier2" || details["source"] != "runtime" ||
		details["consecutive_failures"] != float64(3) || details["model"] != "text-embedding-3-small" {
		t.Errorf("details = %v", details)
	}
	if len(h.notes) != 1 || h.notes[0].typ != database.NotificationTypeAlertFire {
		t.Fatalf("notifications = %v, want one fire", h.notes)
	}

	// A further failure with the same error does not re-raise, but
	// rewrites the text with the new count, without logging the
	// unchanged error again.
	h.fail("model gpt-x does not exist")
	if h.store.creates != 1 || h.store.updates != 1 ||
		!strings.Contains(h.open().Description, "4 consecutive") {
		t.Errorf("creates %d, updates %d, description %q after a repeat failure",
			h.store.creates, h.store.updates, h.open().Description)
	}
	if n := h.countLogged("call failed"); n != 1 {
		t.Errorf("call error logged %d times, want once", n)
	}
	// A different error rewrites the text.
	h.fail("rate limited")
	if h.store.updates != 2 || !strings.Contains(h.open().Description, "rate limited") {
		t.Errorf("error change not recorded: updates %d", h.store.updates)
	}

	h.succeed()
	if h.open() != nil {
		t.Fatal("alert still open after a success")
	}
	if len(h.notes) != 2 || h.notes[1].typ != database.NotificationTypeAlertClear {
		t.Errorf("notifications = %v, want fire then clear", h.notes)
	}

	// The count restarted: two failures stay below the threshold.
	h.disableCooldown()
	h.fail("boom")
	h.fail("boom")
	if h.open() != nil {
		t.Error("count was not reset by the success")
	}
}

func TestProviderHealth_CooldownBoundsAFlakyProvider(t *testing.T) {
	h := newTrackerHarness(3)
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	h.tracker.now = func() time.Time { return now }

	h.fail("401")
	h.fail("401")
	h.fail("401")
	h.succeed()
	if len(h.notes) != 2 {
		t.Fatalf("notifications = %v, want fire then clear", h.notes)
	}

	// Inside the cooldown, runs of failures at or past the threshold
	// raise nothing.
	for i := 0; i < 10; i++ {
		now = now.Add(10 * time.Second)
		h.fail("401")
		h.fail("401")
		h.fail("401")
		h.succeed()
	}
	if h.store.creates != 1 || len(h.notes) != 2 {
		t.Fatalf("creates %d, notifications %v inside the cooldown", h.store.creates, h.notes)
	}

	// Once the cooldown has passed, the next run at the threshold
	// raises the alert again.
	now = now.Add(AlertCooldownPeriod)
	h.fail("401")
	h.fail("401")
	if h.open() != nil {
		t.Fatal("alert raised below the threshold after the cooldown")
	}
	h.fail("401")
	if h.open() == nil || h.store.creates != 2 {
		t.Errorf("alert not raised after the cooldown: creates %d", h.store.creates)
	}
}

func TestProviderHealth_StartupCheckIgnoresCooldown(t *testing.T) {
	h := newTrackerHarness(3)
	h.fail("401")
	h.fail("401")
	h.fail("401")
	h.succeed()
	h.tracker.record(context.Background(), providerTierEmbedding, "openai",
		"text-embedding-3-small", errors.New("401"), true)
	if h.open() == nil {
		t.Error("startup check failure did not raise within the cooldown")
	}
}

func TestProviderHealth_ReasoningSuccessClearsBothReasoningTiers(t *testing.T) {
	cls := providerHealthKey(providerTierClassification, "anthropic")
	reeval := providerHealthKey(providerTierReevaluation, "anthropic")
	succeed := func(h *trackerHarness, tier providerTier) {
		h.tracker.record(context.Background(), tier, "anthropic", "claude-x", nil, false)
	}

	t.Run("tier 3 success clears a re-evaluation alert", func(t *testing.T) {
		// No acknowledged alert is due, so no re-evaluation call will
		// ever clear the alert itself.
		h := newTrackerHarness(3)
		alert := h.store.put(reeval)
		succeed(h, providerTierClassification)
		if alert.Status != "cleared" {
			t.Error("re-evaluation alert outlived a successful Tier 3 call")
		}
	})
	t.Run("re-evaluation success clears a tier 3 alert", func(t *testing.T) {
		h := newTrackerHarness(3)
		alert := h.store.put(cls)
		succeed(h, providerTierReevaluation)
		if alert.Status != "cleared" {
			t.Error("Tier 3 alert outlived a successful re-evaluation call")
		}
	})
	t.Run("a reasoning success resets both counts", func(t *testing.T) {
		h := newTrackerHarness(3)
		fail := func() {
			h.tracker.record(withProviderTier(context.Background(), providerTierReevaluation),
				providerTierReevaluation, "anthropic", "claude-x", errors.New("down"), false)
		}
		fail()
		fail()
		succeed(h, providerTierClassification)
		fail()
		fail()
		if h.store.openFor(reeval) != nil {
			t.Error("re-evaluation count survived a Tier 3 success")
		}
	})
	t.Run("a reasoning success leaves the embedding alert alone", func(t *testing.T) {
		h := newTrackerHarness(3)
		alert := h.store.put(providerHealthKey(providerTierEmbedding, "anthropic"))
		succeed(h, providerTierClassification)
		if alert.Status == "cleared" {
			t.Error("embedding alert cleared by a reasoning success")
		}
	})
}

func TestProviderHealth_SuccessWithNothingOpen(t *testing.T) {
	h := newTrackerHarness(1)
	h.succeed()
	h.succeed()
	if h.store.gets != 1 {
		t.Errorf("open alert read %d times, want once", h.store.gets)
	}
	if h.store.clears != 0 || len(h.notes) != 0 {
		t.Error("a success with nothing open cleared or notified")
	}
}

func TestProviderHealth_ClearsAlertOpenBeforeRestart(t *testing.T) {
	h := newTrackerHarness(3)
	existing := h.store.put(providerHealthKey(providerTierEmbedding, "openai"))
	h.succeed()
	if existing.Status != "cleared" {
		t.Error("alert left open by a previous run was not cleared")
	}
}

func TestProviderHealth_FailureAdoptsAlertOpenBeforeRestart(t *testing.T) {
	h := newTrackerHarness(1)
	existing := h.store.put(providerHealthKey(providerTierEmbedding, "openai"))
	h.fail("boom")
	if h.store.creates != 0 || h.store.updates != 1 {
		t.Errorf("creates %d, updates %d, want the open alert updated", h.store.creates, h.store.updates)
	}
	if !strings.Contains(existing.Description, "boom") || len(h.notes) != 0 {
		t.Errorf("description %q, notes %v", existing.Description, h.notes)
	}
}

func TestProviderHealth_ImmediateIgnoresThreshold(t *testing.T) {
	h := newTrackerHarness(5)
	h.tracker.record(context.Background(), providerTierClassification, "anthropic",
		"claude-x", errors.New("model not found"), true)
	alert := h.store.openFor(providerHealthKey(providerTierClassification, "anthropic"))
	if alert == nil {
		t.Fatal("immediate failure did not raise the alert")
	}
	if !strings.Contains(alert.Description, "startup health check") ||
		!strings.Contains(alert.Description, "without LLM classification") {
		t.Errorf("description = %q", alert.Description)
	}
	if !strings.Contains(*alert.AnomalyDetails, `"source":"startup_check"`) {
		t.Errorf("details = %s", *alert.AnomalyDetails)
	}
}

func TestProviderHealth_ThresholdBelowOneUsesDefault(t *testing.T) {
	h := newTrackerHarness(0)
	for i := 1; i < config.DefaultProviderFailureThreshold; i++ {
		h.fail("boom")
	}
	if h.open() != nil {
		t.Fatal("alert raised below the default threshold")
	}
	h.fail("boom")
	if h.open() == nil {
		t.Error("alert not raised at the default threshold")
	}
}

func TestProviderHealth_CanceledCallIsIgnored(t *testing.T) {
	h := newTrackerHarness(1)
	h.tracker.record(context.Background(), providerTierEmbedding, "openai", "m",
		fmt.Errorf("request: %w", context.Canceled), false)
	if h.store.gets != 0 || h.open() != nil {
		t.Error("a canceled call was counted")
	}
}

func TestProviderHealth_DeadlineIsAFailure(t *testing.T) {
	h := newTrackerHarness(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The call's own context is already done, but the alert is still
	// written because the datastore calls detach from it.
	h.tracker.record(ctx, providerTierEmbedding, "openai", "m",
		context.DeadlineExceeded, false)
	if h.open() == nil {
		t.Error("a timed-out call did not raise the alert")
	}
}

func TestProviderHealth_StoreFailures(t *testing.T) {
	t.Run("read failure is retried", func(t *testing.T) {
		h := newTrackerHarness(1)
		h.store.getOpenErr = errors.New("db down")
		h.fail("boom")
		if h.open() != nil || !h.loggedContaining("Failed to read") {
			t.Fatal("alert raised although the open alert could not be read")
		}
		h.succeed()
		h.store.getOpenErr = nil
		h.fail("boom")
		if h.open() == nil {
			t.Error("alert not raised once the read recovered")
		}
	})
	t.Run("create failure", func(t *testing.T) {
		h := newTrackerHarness(1)
		h.store.createErr = errors.New("db down")
		h.fail("boom")
		if len(h.notes) != 0 || !h.loggedContaining("Failed to raise") {
			t.Error("create failure not handled")
		}
	})
	t.Run("update failure keeps the old text", func(t *testing.T) {
		h := newTrackerHarness(1)
		h.fail("first")
		h.store.updateErr = errors.New("db down")
		h.fail("second")
		if !h.loggedContaining("Failed to update") {
			t.Error("update failure not logged")
		}
		h.store.updateErr = nil
		h.fail("second")
		if !strings.Contains(h.open().Description, "second") {
			t.Error("text not rewritten once the update recovered")
		}
	})
	t.Run("alert cleared by another process is raised again", func(t *testing.T) {
		h := newTrackerHarness(1)
		h.fail("first")
		first := h.open()
		if first == nil {
			t.Fatal("alert not raised")
		}
		// Another process clears the alert behind this tracker's back.
		h.store.mu.Lock()
		first.Status = "cleared"
		h.store.mu.Unlock()
		h.fail("second")
		again := h.open()
		if again == nil || again.ID == first.ID || !strings.Contains(again.Description, "second") {
			t.Fatalf("alert after a stale clear = %+v, want a new alert", again)
		}
		if len(h.notes) != 2 {
			t.Errorf("notifications = %d, want 2", len(h.notes))
		}
	})
	t.Run("conflicting create refreshes the other alert", func(t *testing.T) {
		h := newTrackerHarness(1)
		h.store.createConflict = true
		h.fail("boom")
		if h.store.updates != 1 || len(h.notes) != 0 {
			t.Errorf("updates %d, notes %v", h.store.updates, h.notes)
		}
		h.fail("boom")
		if h.store.updates != 2 || !strings.Contains(h.open().Description, "2 consecutive") {
			t.Errorf("count not refreshed after a conflict: updates %d", h.store.updates)
		}
	})
	t.Run("conflicting create with a failed update", func(t *testing.T) {
		h := newTrackerHarness(1)
		h.store.createConflict = true
		h.store.updateErr = errors.New("db down")
		h.fail("boom")
		if !h.loggedContaining("Failed to update") {
			t.Error("update failure not logged")
		}
	})
	t.Run("clear failure is retried", func(t *testing.T) {
		h := newTrackerHarness(1)
		h.fail("boom")
		h.store.clearErr = errors.New("db down")
		h.succeed()
		if h.open() == nil || !h.loggedContaining("Failed to clear") {
			t.Fatal("alert cleared although the clear failed")
		}
		h.store.clearErr = nil
		h.succeed()
		if h.open() != nil {
			t.Error("alert not cleared on the next success")
		}
	})
	t.Run("cleared alert cannot be read back", func(t *testing.T) {
		h := newTrackerHarness(1)
		h.fail("boom")
		h.store.getAlertErr = errors.New("db down")
		h.succeed()
		if h.open() != nil || len(h.notes) != 1 || !h.loggedContaining("Failed to read cleared") {
			t.Errorf("notes %v", h.notes)
		}
	})
}

func TestProviderHealth_ClearStale(t *testing.T) {
	h := newTrackerHarness(1)
	keep := h.store.put(providerHealthKey(providerTierEmbedding, "openai"))
	stale := h.store.put(providerHealthKey(providerTierClassification, "ollama"))
	h.tracker.clearStale(context.Background(), map[string]bool{*keep.MetricName: true})
	if keep.Status == "cleared" || stale.Status != "cleared" {
		t.Errorf("keep %s, stale %s", keep.Status, stale.Status)
	}
	if len(h.notes) != 1 || h.notes[0].id != stale.ID {
		t.Errorf("notes = %v", h.notes)
	}

	h.store.listErr = errors.New("db down")
	h.tracker.clearStale(context.Background(), nil)
	if !h.loggedContaining("Failed to list") {
		t.Error("list failure not logged")
	}
}

func TestProviderTierContext(t *testing.T) {
	ctx := context.Background()
	if got := providerTierFromContext(ctx, providerTierClassification); got != providerTierClassification {
		t.Errorf("untagged = %s", got)
	}
	ctx = withProviderTier(ctx, providerTierReevaluation)
	if got := providerTierFromContext(ctx, providerTierClassification); got != providerTierReevaluation {
		t.Errorf("tagged = %s", got)
	}
}

func TestProviderTierText(t *testing.T) {
	for _, tier := range []providerTier{providerTierEmbedding, providerTierClassification, providerTierReevaluation} {
		if tier.label() == "" || tier.consequence() == "" {
			t.Errorf("%s has empty text", tier)
		}
	}
	if !strings.Contains(providerTierReevaluation.consequence(), "re-evaluated") {
		t.Error("re-evaluation consequence")
	}
}

func TestRedactProviderError(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		secrets []string
		absent  []string
		present []string
	}{
		{"configured key", "invalid key k3y-VALUE-123 supplied", []string{"", "k3y-VALUE-123"},
			[]string{"k3y-VALUE-123"}, []string{"[REDACTED]"}},
		{"bearer token", "Authorization: Bearer abc.def.ghi rejected", nil,
			[]string{"abc.def.ghi"}, []string{"[REDACTED]"}},
		{"query key", "GET https://api.example.com/v1?key=secretvalue&x=1 failed", nil,
			[]string{"secretvalue", "x=1"}, []string{"https://api.example.com/v1?[REDACTED] failed"}},
		{"json api_key", `{"api_key": "hunter2hunter2"}`, nil,
			[]string{"hunter2hunter2"}, nil},
		{"openai-shaped key", "Incorrect API key provided: sk-proj-abcdefghijkl", nil,
			[]string{"sk-proj-abcdefghijkl"}, []string{"Incorrect API key provided"}},
		{"google-shaped key", "bad AIzaSyA1234567890abcdefghijk", nil,
			[]string{"AIzaSyA1234567890abcdefghijk"}, nil},
		{"url userinfo", "dial https://user:pass@example.com/x", nil,
			[]string{"user:pass"}, []string{"https://[REDACTED]＠example.com/x"}},
		{"control characters", "line1\nline2\x1b[31m\u202e", nil,
			[]string{"\n", "\x1b", "\u202e"}, []string{"line1line2"}},
		{"plain message kept", "model gpt-x does not exist", nil,
			nil, []string{"model gpt-x does not exist"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactProviderError(tt.in, tt.secrets)
			for _, s := range tt.absent {
				if strings.Contains(got, s) {
					t.Errorf("%q still contains %q", got, s)
				}
			}
			for _, s := range tt.present {
				if !strings.Contains(got, s) {
					t.Errorf("%q lacks %q", got, s)
				}
			}
		})
	}

	long := strings.Repeat("é", maxProviderErrorBytes)
	got := redactProviderError(long, nil)
	if !utf8.ValidString(got) || len(got) > maxProviderErrorBytes+len("…") {
		t.Errorf("truncated to %d bytes, valid %v", len(got), utf8.ValidString(got))
	}
	if short := redactProviderError("ok", nil); short != "ok" {
		t.Errorf("short = %q", short)
	}
}

func TestProviderName(t *testing.T) {
	if got := providerName("  OpenAI "); got != "openai" {
		t.Errorf("got %q", got)
	}
	if got := providerName(""); got != "unknown" {
		t.Errorf("got %q", got)
	}
}

func TestConfiguredSecrets(t *testing.T) {
	if got := configuredSecrets(config.NewConfig()); len(got) != 4 {
		t.Errorf("got %d secrets, want one per provider", len(got))
	}
}

// fakeEmbedder and fakeReasoner are providers whose outcome a test sets.
type fakeEmbedder struct{ err error }

func (f *fakeEmbedder) GenerateEmbedding(context.Context, string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []float32{1}, nil
}

func (f *fakeEmbedder) ModelName() string { return "emb-model" }

type fakeReasoner struct{ err error }

func (f *fakeReasoner) Classify(context.Context, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "OK", nil
}

func (f *fakeReasoner) ModelName() string    { return "reason-model" }
func (f *fakeReasoner) ProviderName() string { return "reason-provider" }
func (f *fakeReasoner) SystemPrompt() string { return "reason system prompt" }

func TestHealthTrackingWrappers(t *testing.T) {
	h := newTrackerHarness(1)
	emb := &healthTrackingEmbedding{inner: &fakeEmbedder{err: errors.New("down")},
		provider: "openai", tracker: h.tracker}
	rsn := &healthTrackingReasoning{inner: &fakeReasoner{err: errors.New("down")},
		provider: "anthropic", tracker: h.tracker}

	if emb.ModelName() != "emb-model" || rsn.ModelName() != "reason-model" {
		t.Error("model names not passed through")
	}
	if _, err := emb.GenerateEmbedding(context.Background(), "x"); err == nil {
		t.Error("embedding error swallowed")
	}
	if _, err := rsn.Classify(context.Background(), "x"); err == nil {
		t.Error("classify error swallowed")
	}
	if _, err := rsn.Classify(withProviderTier(context.Background(), providerTierReevaluation), "x"); err == nil {
		t.Error("classify error swallowed")
	}
	for _, key := range []string{
		providerHealthKey(providerTierEmbedding, "openai"),
		providerHealthKey(providerTierClassification, "anthropic"),
		providerHealthKey(providerTierReevaluation, "anthropic"),
	} {
		if h.store.openFor(key) == nil {
			t.Errorf("no alert for %s", key)
		}
	}

	emb.inner = &fakeEmbedder{}
	if v, err := emb.GenerateEmbedding(context.Background(), "x"); err != nil || len(v) != 1 {
		t.Errorf("embedding = %v, %v", v, err)
	}
	if h.store.openFor(providerHealthKey(providerTierEmbedding, "openai")) != nil {
		t.Error("embedding alert not cleared by success")
	}
}

// providerHealthEngine builds an engine holding wrapped fake providers
// and a tracker on a fake store.
func providerHealthEngine(embErr, rsnErr error) (*Engine, *trackerHarness) {
	cfg := config.NewConfig()
	cfg.Anomaly.Enabled = true
	cfg.Anomaly.Reevaluation.Enabled = true
	h := newTrackerHarness(3)
	e := &Engine{config: cfg, providerHealth: h.tracker}
	e.embeddingProvider = &healthTrackingEmbedding{inner: &fakeEmbedder{err: embErr},
		provider: "openai", tracker: h.tracker}
	e.reasoningProvider = &healthTrackingReasoning{inner: &fakeReasoner{err: rsnErr},
		provider: "anthropic", tracker: h.tracker}
	return e, h
}

func TestCheckProviderHealth(t *testing.T) {
	t.Run("failures raise at once and stale alerts clear", func(t *testing.T) {
		e, h := providerHealthEngine(errors.New("no such model"), errors.New("401 access denied"))
		stale := h.store.put(providerHealthKey(providerTierEmbedding, "voyage"))
		e.checkProviderHealth(context.Background())

		if stale.Status != "cleared" {
			t.Error("stale alert for an unconfigured provider not cleared")
		}
		for _, key := range []string{
			providerHealthKey(providerTierEmbedding, "openai"),
			providerHealthKey(providerTierClassification, "anthropic"),
		} {
			if h.store.openFor(key) == nil {
				t.Errorf("no alert for %s", key)
			}
		}
		if !h.loggedContaining("Provider health alert raised") {
			t.Error("raised alert not logged")
		}
	})
	t.Run("healthy providers clear alerts from a previous run", func(t *testing.T) {
		e, h := providerHealthEngine(nil, nil)
		e.config.Anomaly.Tier3.TimeoutSeconds = 0
		old := h.store.put(providerHealthKey(providerTierClassification, "anthropic"))
		reeval := h.store.put(providerHealthKey(providerTierReevaluation, "anthropic"))
		e.checkProviderHealth(context.Background())
		if old.Status != "cleared" {
			t.Error("alert from a previous run not cleared by a healthy check")
		}
		// With no acknowledged alert due, nothing else would ever call
		// the provider for re-evaluation, so the check clears it too.
		if reeval.Status != "cleared" {
			t.Error("re-evaluation alert not cleared by a healthy reasoning check")
		}
	})
	t.Run("anomaly detection off clears everything and checks nothing", func(t *testing.T) {
		e, h := providerHealthEngine(errors.New("down"), errors.New("down"))
		e.config.Anomaly.Enabled = false
		old := h.store.put(providerHealthKey(providerTierEmbedding, "openai"))
		e.checkProviderHealth(context.Background())
		if old.Status != "cleared" || h.store.creates != 0 {
			t.Errorf("status %s, creates %d", old.Status, h.store.creates)
		}
	})
	t.Run("disabled tiers are neither checked nor kept", func(t *testing.T) {
		e, h := providerHealthEngine(errors.New("down"), errors.New("down"))
		e.config.Anomaly.Tier2.Enabled = false
		e.config.Anomaly.Tier3.Enabled = false
		e.config.Anomaly.Reevaluation.Enabled = false
		emb := h.store.put(providerHealthKey(providerTierEmbedding, "openai"))
		cls := h.store.put(providerHealthKey(providerTierClassification, "anthropic"))
		e.checkProviderHealth(context.Background())
		if emb.Status != "cleared" || cls.Status != "cleared" || h.store.creates != 0 {
			t.Errorf("embedding %s, classification %s, creates %d",
				emb.Status, cls.Status, h.store.creates)
		}
	})
	t.Run("reasoning check falls back to re-evaluation", func(t *testing.T) {
		e, h := providerHealthEngine(nil, errors.New("401 access denied"))
		e.config.Anomaly.Tier3.Enabled = false
		e.checkProviderHealth(context.Background())
		if h.store.openFor(providerHealthKey(providerTierReevaluation, "anthropic")) == nil {
			t.Error("no re-evaluation alert")
		}
		if h.store.openFor(providerHealthKey(providerTierClassification, "anthropic")) != nil {
			t.Error("classification alert raised although Tier 3 is disabled")
		}
	})
	t.Run("no tracker", func(t *testing.T) {
		(&Engine{config: config.NewConfig()}).checkProviderHealth(context.Background())
	})
}

func TestReasoningHealthTier(t *testing.T) {
	tests := []struct {
		name          string
		tier3, reeval bool
		want          providerTier
		wantUsed      bool
	}{
		{"tier 3 wins", true, true, providerTierClassification, true},
		{"tier 3 only", true, false, providerTierClassification, true},
		{"re-evaluation only", false, true, providerTierReevaluation, true},
		{"neither", false, false, providerTierClassification, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.Anomaly.Tier3.Enabled = tt.tier3
			cfg.Anomaly.Reevaluation.Enabled = tt.reeval
			got, used := reasoningHealthTier(cfg)
			if got != tt.want || used != tt.wantUsed {
				t.Errorf("reasoningHealthTier = %v, %v; want %v, %v", got, used, tt.want, tt.wantUsed)
			}
		})
	}
}

func TestActiveProviderHealthKeys(t *testing.T) {
	e, _ := providerHealthEngine(nil, nil)
	if got := e.activeProviderHealthKeys(); len(got) != 3 {
		t.Errorf("active = %v, want three keys", got)
	}
	e.config.Anomaly.Reevaluation.Enabled = false
	if got := e.activeProviderHealthKeys(); len(got) != 2 ||
		got[providerHealthKey(providerTierReevaluation, "anthropic")] {
		t.Errorf("active = %v", got)
	}
	// A reload that disables a tier drops its key even though the
	// provider built at startup is still wrapped.
	e.config.Anomaly.Tier2.Enabled = false
	e.config.Anomaly.Tier3.Enabled = false
	e.config.Anomaly.Reevaluation.Enabled = true
	if got := e.activeProviderHealthKeys(); len(got) != 1 ||
		!got[providerHealthKey(providerTierReevaluation, "anthropic")] {
		t.Errorf("active after disabling tiers = %v", got)
	}
}

func TestInitProviderHealth(t *testing.T) {
	t.Run("no datastore leaves providers unwrapped", func(t *testing.T) {
		emb := &fakeEmbedder{}
		e := &Engine{config: config.NewConfig(), embeddingProvider: emb}
		e.initProviderHealth()
		if e.providerHealth != nil || e.embeddingProvider != emb {
			t.Error("providers wrapped without a datastore")
		}
	})
	t.Run("wraps both providers", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.LLM.EmbeddingProvider = "OpenAI"
		cfg.LLM.ReasoningProvider = "anthropic"
		e := &Engine{config: cfg, datastore: database.NewTestDatastore(nil),
			embeddingProvider: &fakeEmbedder{}, reasoningProvider: &fakeReasoner{}}
		e.initProviderHealth()
		emb, ok := e.embeddingProvider.(*healthTrackingEmbedding)
		if !ok || emb.provider != "openai" {
			t.Fatalf("embedding provider = %#v", e.embeddingProvider)
		}
		if rsn, ok := e.reasoningProvider.(*healthTrackingReasoning); !ok || rsn.provider != "anthropic" {
			t.Fatalf("reasoning provider = %#v", e.reasoningProvider)
		}
		if got := e.providerHealth.threshold(); got != config.DefaultProviderFailureThreshold {
			t.Errorf("threshold = %d", got)
		}
		if got := e.providerHealth.secrets(); len(got) != 4 {
			t.Errorf("secrets = %d", len(got))
		}
	})
	t.Run("nil providers stay nil", func(t *testing.T) {
		e := &Engine{config: config.NewConfig(), datastore: database.NewTestDatastore(nil)}
		e.initProviderHealth()
		if e.embeddingProvider != nil || e.reasoningProvider != nil || e.providerHealth == nil {
			t.Error("unexpected wrapping")
		}
	})
}

// TestHealthTrackingReasoningDelegatesIdentity pins that the wrapper
// reports the wrapped provider's name and system prompt, which feed the
// re-evaluation fingerprint (GitHub issue #575).
func TestHealthTrackingReasoningDelegatesIdentity(t *testing.T) {
	rsn := &healthTrackingReasoning{inner: &fakeReasoner{}, provider: "anthropic"}
	if got := rsn.ProviderName(); got != "reason-provider" {
		t.Errorf("ProviderName() = %q, want the wrapped provider's", got)
	}
	if got := rsn.SystemPrompt(); got != "reason system prompt" {
		t.Errorf("SystemPrompt() = %q, want the wrapped provider's", got)
	}
}
