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

// Provider health tracking raises a system alert when the embedding or
// reasoning provider keeps failing. Anomaly detection fails safe when a
// provider call fails (Tier 2 passes the candidate on, Tier 3 raises the
// alert unclassified, re-evaluation keeps the alert), so without this a
// retired model or a revoked API key degrades detection with nothing
// but log lines to show for it. See GitHub issue #582.
//
// The tracking wraps the llm.EmbeddingProvider and llm.ReasoningProvider
// the engine holds, so the call sites in anomalies.go and reevaluation.go
// are untouched. Which caller an outcome belongs to is carried in the
// context: the re-evaluation worker's context is tagged when it starts,
// and an untagged Classify call is Tier 3 classification.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
	"github.com/pgedge/ai-workbench/alerter/internal/llm"
)

// providerTier names the caller of an LLM provider whose outcomes are
// tracked separately.
type providerTier string

const (
	providerTierEmbedding      providerTier = "tier2"
	providerTierClassification providerTier = "tier3"
	providerTierReevaluation   providerTier = "reevaluation"
)

// label is the tier's name as an operator reads it.
func (t providerTier) label() string {
	switch t {
	case providerTierEmbedding:
		return "Tier 2 embedding"
	case providerTierClassification:
		return "Tier 3 classification"
	default:
		return "Re-evaluation"
	}
}

// consequence says what anomaly detection does whilst the tier's
// provider is failing.
func (t providerTier) consequence() string {
	switch t {
	case providerTierEmbedding:
		return "Anomaly candidates are not compared with past anomalies, " +
			"so none is suppressed as a repeat and every one goes on to " +
			"Tier 3."
	case providerTierClassification:
		return "Anomaly candidates are raised as alerts without LLM " +
			"classification, so false positives are not filtered out."
	default:
		return "Acknowledged anomaly alerts are not re-evaluated, so none " +
			"is cleared on the strength of later evidence."
	}
}

// providerHealthKeyPrefix starts the metric_name of every provider
// health system alert.
const providerHealthKeyPrefix = "llm_provider_health."

// providerHealthKey is the metric_name that deduplicates the system alert
// for one tier and provider.
func providerHealthKey(tier providerTier, provider string) string {
	return providerHealthKeyPrefix + string(tier) + "." + provider
}

// providerHealthDBTimeout bounds each datastore call the tracker makes.
const providerHealthDBTimeout = 10 * time.Second

// providerHealthCheckTimeout bounds each startup health check call when
// the Tier 3 timeout is not configured.
const providerHealthCheckTimeout = 30 * time.Second

// providerHealthCheckText is the input of the startup embedding check.
const providerHealthCheckText = "pgEdge AI DBA Workbench alerter health check"

// providerHealthCheckPrompt is the prompt of the startup reasoning check;
// only whether the call succeeds matters, not what it answers.
const providerHealthCheckPrompt = "This is a connectivity check from the " +
	"pgEdge AI DBA Workbench alerter. Reply with the single word OK."

// providerTierContextKey is the context key for the tier tag.
type providerTierContextKey struct{}

// withProviderTier tags ctx so that provider calls made under it are
// counted against tier.
func withProviderTier(ctx context.Context, tier providerTier) context.Context {
	return context.WithValue(ctx, providerTierContextKey{}, tier)
}

// providerTierFromContext returns the tier ctx is tagged with, or
// fallback when it carries none.
func providerTierFromContext(ctx context.Context, fallback providerTier) providerTier {
	if tier, ok := ctx.Value(providerTierContextKey{}).(providerTier); ok {
		return tier
	}
	return fallback
}

// systemAlertStore is the part of the datastore the tracker uses.
type systemAlertStore interface {
	GetOpenSystemAlert(ctx context.Context, key string) (*database.Alert, error)
	GetOpenSystemAlerts(ctx context.Context, keyPrefix string) ([]*database.Alert, error)
	CreateSystemAlert(ctx context.Context, alert *database.Alert) (*database.Alert, bool, error)
	UpdateSystemAlert(ctx context.Context, alertID int64, description string, details *string) error
	ClearAlert(ctx context.Context, alertID int64) error
	GetAlert(ctx context.Context, alertID int64) (*database.Alert, error)
}

// providerHealthState is the tracker's view of one tier and provider.
type providerHealthState struct {
	mu sync.Mutex

	// failures counts consecutive failed calls since the last success.
	failures int

	// loaded is set once the datastore has been asked whether an alert
	// is already open for the key, which may have been raised before the
	// alerter restarted.
	loaded bool

	// alertID is the open system alert, or 0 when none is open.
	alertID int64

	// lastError is the redacted error the open alert last reported.
	lastError string
}

// providerHealthTracker counts consecutive provider failures per tier
// and provider, and opens and clears the matching system alert.
type providerHealthTracker struct {
	store     systemAlertStore
	notify    func(*database.Alert, database.NotificationType)
	threshold func() int
	secrets   func() []string
	log       func(string, ...any)

	mu     sync.Mutex
	states map[string]*providerHealthState
}

// newProviderHealthTracker builds a tracker. threshold is read on every
// failure so a reloaded configuration takes effect, and secrets returns
// the configured API keys, which are removed from any error text.
func newProviderHealthTracker(
	store systemAlertStore,
	notify func(*database.Alert, database.NotificationType),
	threshold func() int,
	secrets func() []string,
	log func(string, ...any),
) *providerHealthTracker {
	return &providerHealthTracker{
		store:     store,
		notify:    notify,
		threshold: threshold,
		secrets:   secrets,
		log:       log,
		states:    make(map[string]*providerHealthState),
	}
}

// state returns the state for key, creating it on first use.
func (t *providerHealthTracker) state(key string) *providerHealthState {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.states[key]
	if !ok {
		st = &providerHealthState{}
		t.states[key] = st
	}
	return st
}

// record notes the outcome of one provider call. immediate opens the
// alert on this failure whatever the threshold, for the startup check.
//
// A call abandoned because its context was canceled says nothing about
// the provider, so it is neither a failure nor a success. A deadline is
// a failure: a provider too slow to answer degrades detection as surely
// as one that errors.
func (t *providerHealthTracker) record(ctx context.Context, tier providerTier,
	provider, model string, callErr error, immediate bool) {
	if errors.Is(callErr, context.Canceled) {
		return
	}

	key := providerHealthKey(tier, provider)
	st := t.state(key)
	st.mu.Lock()
	defer st.mu.Unlock()

	// The call's own context may already have expired, which is often
	// why it failed; datastore writes must still happen.
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerHealthDBTimeout)
	defer cancel()

	if callErr == nil {
		st.failures = 0
		if !t.load(dbCtx, key, st) || st.alertID == 0 {
			return
		}
		t.clear(dbCtx, key, st, "the next call succeeded")
		return
	}

	st.failures++
	threshold := t.threshold()
	if threshold < 1 {
		threshold = config.DefaultProviderFailureThreshold
	}
	if !immediate && st.failures < threshold {
		return
	}
	if !t.load(dbCtx, key, st) {
		return
	}

	lastError := redactProviderError(callErr.Error(), t.secrets())
	description := providerHealthDescription(tier, provider, model, st.failures, immediate, lastError)
	details := providerHealthDetails(tier, provider, model, st.failures, immediate, lastError)

	if st.alertID != 0 {
		if lastError == st.lastError {
			return
		}
		if err := t.store.UpdateSystemAlert(dbCtx, st.alertID, description, details); err != nil {
			t.log("ERROR: Failed to update provider health alert %s: %v", key, err)
			return
		}
		st.lastError = lastError
		return
	}

	objectName := provider + "/" + model
	alert := &database.Alert{
		ObjectName:     &objectName,
		MetricName:     &key,
		Severity:       "warning",
		Title:          fmt.Sprintf("Anomaly detection degraded: %s provider %s failing", tier.label(), provider),
		Description:    description,
		AnomalyDetails: details,
		TriggeredAt:    time.Now(),
	}
	opened, created, err := t.store.CreateSystemAlert(dbCtx, alert)
	if err != nil {
		t.log("ERROR: Failed to raise provider health alert %s: %v", key, err)
		return
	}
	st.alertID = opened.ID
	if !created {
		// Another alerter process raised it first; refresh its text.
		if err := t.store.UpdateSystemAlert(dbCtx, opened.ID, description, details); err != nil {
			t.log("ERROR: Failed to update provider health alert %s: %v", key, err)
			return
		}
		st.lastError = lastError
		return
	}
	st.lastError = lastError
	t.log("Provider health alert raised: %s (%s)", alert.Title, lastError)
	t.notify(opened, database.NotificationTypeAlertFire)
}

// load reads whether an alert is already open for key, once per key, and
// reports whether the state is now known. A failed read is retried on
// the next call.
func (t *providerHealthTracker) load(ctx context.Context, key string, st *providerHealthState) bool {
	if st.loaded {
		return true
	}
	open, err := t.store.GetOpenSystemAlert(ctx, key)
	if err != nil {
		t.log("ERROR: Failed to read provider health alert %s: %v", key, err)
		return false
	}
	if open != nil {
		st.alertID = open.ID
	}
	st.loaded = true
	return true
}

// clear closes the open alert for key and queues the clear notification.
// A failed clear leaves the alert recorded as open, so the next success
// tries again.
func (t *providerHealthTracker) clear(ctx context.Context, key string, st *providerHealthState, why string) {
	id := st.alertID
	if err := t.store.ClearAlert(ctx, id); err != nil {
		t.log("ERROR: Failed to clear provider health alert %s: %v", key, err)
		return
	}
	st.alertID = 0
	st.lastError = ""
	t.log("Provider health alert cleared: %s (%s)", key, why)

	cleared, err := t.store.GetAlert(ctx, id)
	if err != nil {
		t.log("WARNING: Failed to read cleared provider health alert %d: %v", id, err)
		return
	}
	t.notify(cleared, database.NotificationTypeAlertClear)
}

// clearStale clears every open provider health alert whose key is not in
// active, which happens when the configured provider changes or a tier is
// turned off: nothing would ever call that provider again to clear it.
func (t *providerHealthTracker) clearStale(ctx context.Context, active map[string]bool) {
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerHealthDBTimeout)
	defer cancel()

	open, err := t.store.GetOpenSystemAlerts(dbCtx, providerHealthKeyPrefix)
	if err != nil {
		t.log("ERROR: Failed to list provider health alerts: %v", err)
		return
	}
	for _, alert := range open {
		if alert.MetricName == nil || active[*alert.MetricName] {
			continue
		}
		key := *alert.MetricName
		st := t.state(key)
		st.mu.Lock()
		st.loaded = true
		st.alertID = alert.ID
		t.clear(dbCtx, key, st, "the provider is no longer in use")
		st.mu.Unlock()
	}
}

// providerHealthDescription is the alert's description.
func providerHealthDescription(tier providerTier, provider, model string,
	failures int, startup bool, lastError string) string {
	var what string
	if startup {
		what = fmt.Sprintf("The startup health check of the %s provider %s (model %s) failed.",
			strings.ToLower(tier.label()), provider, model)
	} else {
		what = fmt.Sprintf("%d consecutive %s calls to provider %s (model %s) have failed.",
			failures, strings.ToLower(tier.label()), provider, model)
	}
	return fmt.Sprintf("%s %s The alert clears on the next successful call. Last error: %s",
		what, tier.consequence(), lastError)
}

// providerHealthDetails is the alert's anomaly_details JSON, which gives
// a client the parts of the description separately.
func providerHealthDetails(tier providerTier, provider, model string,
	failures int, startup bool, lastError string) *string {
	source := "runtime"
	if startup {
		source = "startup_check"
	}
	raw, err := json.Marshal(map[string]any{
		"tier":                 string(tier),
		"tier_label":           tier.label(),
		"provider":             provider,
		"model":                model,
		"consecutive_failures": failures,
		"source":               source,
		"last_error":           lastError,
	})
	if err != nil {
		return nil
	}
	s := string(raw)
	return &s
}

// maxProviderErrorBytes caps the error text an alert repeats.
const maxProviderErrorBytes = 512

// providerErrorPatterns match credentials a provider error may repeat:
// a bearer token, a key or token parameter or header, a key-shaped value
// from a known provider, and the userinfo of a URL.
var providerErrorPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)(bearer\s+)[^\s"',;]+`), "${1}[REDACTED]"},
	{regexp.MustCompile(`(?i)((?:api[_-]?key|x-api-key|x-goog-api-key|access[_-]?token|token|key|secret|password|authorization)["']?\s*[:=]\s*["']?)[^\s"'&,;}]+`), "${1}[REDACTED]"},
	{regexp.MustCompile(`\b(?:sk|pa|pk)-[A-Za-z0-9_\-]{8,}`), "[REDACTED]"},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{20,}`), "[REDACTED]"},
	{regexp.MustCompile(`(://)[^/@\s]+@`), "${1}[REDACTED]@"},
}

// redactProviderError prepares a provider error for an alert that every
// user with alert access, and every notification channel, will see. It
// deletes control and formatting characters, removes the configured API
// keys verbatim and anything that looks like a credential, and caps the
// length.
func redactProviderError(msg string, secrets []string) string {
	msg = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
			r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, strings.ToValidUTF8(msg, "\uFFFD"))
	for _, secret := range secrets {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "[REDACTED]")
		}
	}
	for _, p := range providerErrorPatterns {
		msg = p.re.ReplaceAllString(msg, p.repl)
	}
	if len(msg) <= maxProviderErrorBytes {
		return msg
	}
	cut := maxProviderErrorBytes
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut] + "…"
}

// healthTrackingEmbedding wraps an embedding provider and records the
// outcome of every call against Tier 2.
type healthTrackingEmbedding struct {
	inner    llm.EmbeddingProvider
	provider string
	tracker  *providerHealthTracker
}

// GenerateEmbedding implements llm.EmbeddingProvider.
func (h *healthTrackingEmbedding) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	embedding, err := h.inner.GenerateEmbedding(ctx, text)
	h.tracker.record(ctx, providerTierEmbedding, h.provider, h.inner.ModelName(), err, false)
	return embedding, err
}

// ModelName implements llm.EmbeddingProvider.
func (h *healthTrackingEmbedding) ModelName() string {
	return h.inner.ModelName()
}

// healthCheck makes one cheap call and raises the alert at once if it
// fails.
func (h *healthTrackingEmbedding) healthCheck(ctx context.Context) error {
	_, err := h.inner.GenerateEmbedding(ctx, providerHealthCheckText)
	h.tracker.record(ctx, providerTierEmbedding, h.provider, h.inner.ModelName(), err, true)
	return err
}

// healthTrackingReasoning wraps a reasoning provider and records the
// outcome of every call against the tier the context is tagged with,
// Tier 3 classification when it carries none.
type healthTrackingReasoning struct {
	inner    llm.ReasoningProvider
	provider string
	tracker  *providerHealthTracker
}

// Classify implements llm.ReasoningProvider.
func (h *healthTrackingReasoning) Classify(ctx context.Context, prompt string) (string, error) {
	response, err := h.inner.Classify(ctx, prompt)
	tier := providerTierFromContext(ctx, providerTierClassification)
	h.tracker.record(ctx, tier, h.provider, h.inner.ModelName(), err, false)
	return response, err
}

// ModelName implements llm.ReasoningProvider.
func (h *healthTrackingReasoning) ModelName() string {
	return h.inner.ModelName()
}

// healthCheck makes one cheap call and raises the Tier 3 alert at once
// if it fails.
func (h *healthTrackingReasoning) healthCheck(ctx context.Context) error {
	_, err := h.inner.Classify(ctx, providerHealthCheckPrompt)
	h.tracker.record(ctx, providerTierClassification, h.provider, h.inner.ModelName(), err, true)
	return err
}

// providerName normalises a configured provider name for use in a key.
func providerName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "unknown"
	}
	return name
}

// configuredSecrets returns the API keys cfg holds.
func configuredSecrets(cfg *config.Config) []string {
	return []string{
		cfg.GetOpenAIAPIKey(),
		cfg.GetAnthropicAPIKey(),
		cfg.GetVoyageAPIKey(),
		cfg.GetGeminiAPIKey(),
	}
}

// initProviderHealth installs the tracking wrappers around the engine's
// providers. It needs a datastore to hold the alerts, so it does nothing
// without one.
func (e *Engine) initProviderHealth() {
	if e.datastore == nil {
		return
	}
	e.providerHealth = newProviderHealthTracker(
		e.datastore,
		e.queueNotification,
		func() int { return e.getConfig().Anomaly.ProviderHealth.FailureThreshold },
		func() []string { return configuredSecrets(e.getConfig()) },
		e.log,
	)
	if e.embeddingProvider != nil {
		e.embeddingProvider = &healthTrackingEmbedding{
			inner:    e.embeddingProvider,
			provider: providerName(e.config.LLM.EmbeddingProvider),
			tracker:  e.providerHealth,
		}
	}
	if e.reasoningProvider != nil {
		e.reasoningProvider = &healthTrackingReasoning{
			inner:    e.reasoningProvider,
			provider: providerName(e.config.LLM.ReasoningProvider),
			tracker:  e.providerHealth,
		}
	}
}

// activeProviderHealthKeys returns the keys of the provider health
// alerts that the running configuration can still raise and clear.
func (e *Engine) activeProviderHealthKeys() map[string]bool {
	active := make(map[string]bool)
	cfg := e.getConfig()
	if !cfg.Anomaly.Enabled {
		return active
	}
	if emb, ok := e.embeddingProvider.(*healthTrackingEmbedding); ok {
		active[providerHealthKey(providerTierEmbedding, emb.provider)] = true
	}
	if rsn, ok := e.reasoningProvider.(*healthTrackingReasoning); ok {
		active[providerHealthKey(providerTierClassification, rsn.provider)] = true
		if cfg.Anomaly.Reevaluation.Enabled {
			active[providerHealthKey(providerTierReevaluation, rsn.provider)] = true
		}
	}
	return active
}

// checkProviderHealth runs at startup. It clears provider health alerts
// the configuration can no longer resolve, then makes one cheap call to
// each configured provider, raising the alert at once for any that fails
// (a retired model, say) and clearing any left open by a previous run
// for one that answers.
func (e *Engine) checkProviderHealth(ctx context.Context) {
	if e.providerHealth == nil {
		return
	}
	e.providerHealth.clearStale(ctx, e.activeProviderHealthKeys())

	cfg := e.getConfig()
	if !cfg.Anomaly.Enabled {
		return
	}
	timeout := time.Duration(cfg.Anomaly.Tier3.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = providerHealthCheckTimeout
	}

	if emb, ok := e.embeddingProvider.(*healthTrackingEmbedding); ok {
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		if err := emb.healthCheck(checkCtx); err != nil {
			e.log("WARNING: Embedding provider %s failed its startup health check", emb.provider)
		}
		cancel()
	}
	if rsn, ok := e.reasoningProvider.(*healthTrackingReasoning); ok {
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		if err := rsn.healthCheck(checkCtx); err != nil {
			e.log("WARNING: Reasoning provider %s failed its startup health check", rsn.provider)
		}
		cancel()
	}
}
