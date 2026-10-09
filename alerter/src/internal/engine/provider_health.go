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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
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

// sharedTiers returns the tiers whose calls go to the same provider
// client as tier's: Tier 3 classification and re-evaluation share the
// reasoning provider, so a successful call from either shows that the
// provider answers again.
func (t providerTier) sharedTiers() []providerTier {
	if t == providerTierEmbedding {
		return []providerTier{providerTierEmbedding}
	}
	return []providerTier{providerTierClassification, providerTierReevaluation}
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

	// outcomes records whether each recent call failed, oldest first,
	// for the failure rate; it holds at most the failure rate window.
	// endRun may fold a run of consecutive failures into one entry.
	outcomes []bool

	// loaded is set once the datastore has been asked whether an alert
	// is already open for the key, which may have been raised before the
	// alerter restarted.
	loaded bool

	// alertID is the open system alert, or 0 when none is open.
	alertID int64

	// lastError is the redacted error the open alert last reported.
	lastError string

	// reportedDescription is the description the open alert last
	// reported, so that an unchanged one is not rewritten.
	reportedDescription string

	// clearedAt is when this process last cleared the alert, which
	// starts the re-raise cooldown.
	clearedAt time.Time
}

// push records the outcome of one call, dropping the oldest outcomes
// beyond window. The caller holds st.mu.
func (st *providerHealthState) push(failed bool, window int) {
	st.outcomes = append(st.outcomes, failed)
	if extra := len(st.outcomes) - window; extra > 0 {
		st.outcomes = append(st.outcomes[:0], st.outcomes[extra:]...)
	}
}

// recentFailures counts the failed calls among the recent outcomes. The
// caller holds st.mu.
func (st *providerHealthState) recentFailures() int {
	return countFailures(st.outcomes)
}

// countFailures counts the failed calls in outcomes.
func countFailures(outcomes []bool) int {
	n := 0
	for _, failed := range outcomes {
		if failed {
			n++
		}
	}
	return n
}

// endRun is called when a success ends a run of run consecutive
// failures. A run long enough to reach the failure threshold is an
// outage that the consecutive count reports by itself; when the
// provider's other recent calls would stay below the failure limit with
// the run counted once, the run is folded into a single failure. A
// provider that recovers from an outage therefore clears its alert on
// the first success, as it would without the failure rate, and a later
// failure does not raise the alert again on the strength of the outage.
// A provider already failing at the rate keeps the whole run. The caller
// holds st.mu.
func (st *providerHealthState) endRun(run, threshold, limit int) {
	if run < 2 || run < threshold {
		return
	}
	before := st.outcomes[:len(st.outcomes)-min(run, len(st.outcomes))]
	if countFailures(before)+1 >= limit {
		return
	}
	st.outcomes = append(before, true)
}

// providerHealthLimits is the provider health configuration in force,
// with the default in place of any value out of range.
type providerHealthLimits struct {
	// threshold is the consecutive failure count that opens the alert.
	threshold int

	// window is the number of recent calls the failure rate covers.
	window int

	// limit is the number of failures among the recent calls that
	// opens the alert and keeps it open: the failure rate applied to
	// the window, rounded up and at least one.
	limit int
}

// failureLimit is the number of failures among window calls that reaches
// rate, rounded up so that the share is at least rate, and at least one.
func failureLimit(rate float64, window int) int {
	// The tolerance stops a product such as 0.28 * 25, which floating
	// point puts a hair above 7, from rounding up to 8.
	limit := int(math.Ceil(rate*float64(window) - 1e-9))
	if limit < 1 {
		return 1
	}
	return limit
}

// providerHealthTracker counts provider failures per tier and provider,
// both consecutive failures and failures among the recent calls, and
// opens and clears the matching system alert.
type providerHealthTracker struct {
	store    systemAlertStore
	notify   func(*database.Alert, database.NotificationType)
	settings func() config.ProviderHealthConfig
	secrets  func() []string
	log      func(string, ...any)

	// now and cooldown time the re-raise cooldown; tests replace them.
	now      func() time.Time
	cooldown time.Duration

	mu     sync.Mutex
	states map[string]*providerHealthState
}

// newProviderHealthTracker builds a tracker. settings is read on every
// call so a reloaded configuration takes effect, and secrets returns the
// configured API keys, which are removed from any error text.
func newProviderHealthTracker(
	store systemAlertStore,
	notify func(*database.Alert, database.NotificationType),
	settings func() config.ProviderHealthConfig,
	secrets func() []string,
	log func(string, ...any),
) *providerHealthTracker {
	return &providerHealthTracker{
		store:    store,
		notify:   notify,
		settings: settings,
		secrets:  secrets,
		log:      log,
		now:      time.Now,
		cooldown: AlertCooldownPeriod,
		states:   make(map[string]*providerHealthState),
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
// alert on this failure whatever the threshold and cooldown, for the
// startup check.
//
// A failure opens the alert when the consecutive failures reach the
// threshold, or when the failures among the recent calls reach the
// failure limit, which catches a provider that fails a steady share of
// its calls without failing many in a row (GitHub issue #593).
//
// A success resets the consecutive count and is recorded against every
// tier that shares the provider, clearing its alert unless the failure
// limit is still reached, so a re-evaluation alert does not outlive the
// fault just because no acknowledged alert is due for re-evaluation.
// Holding the alert open until the failure rate falls, and not raising
// it again within the cooldown once cleared (the same flapping guard
// the threshold alerts use), keeps a provider that fails intermittently
// from firing and clearing it on every short run of errors.
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

	// The call's own context may already have expired, which is often
	// why it failed; datastore writes must still happen.
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerHealthDBTimeout)
	defer cancel()

	lim := t.limits()
	if callErr == nil {
		for _, shared := range tier.sharedTiers() {
			t.recordSuccess(dbCtx, providerHealthKey(shared, provider), lim)
		}
		return
	}

	key := providerHealthKey(tier, provider)
	st := t.state(key)
	st.mu.Lock()
	defer st.mu.Unlock()

	st.failures++
	st.push(true, lim.window)
	tripped := st.failures >= lim.threshold || st.recentFailures() >= lim.limit
	if !immediate && (!tripped || t.coolingDown(st)) {
		return
	}
	if !t.load(dbCtx, key, st) {
		return
	}
	t.raise(dbCtx, key, st, tier, provider, model, callErr, immediate, lim)
}

// recordSuccess resets the consecutive failure count for key, records
// the success among the recent calls, and clears the alert if one is
// open and the failure limit is no longer reached.
func (t *providerHealthTracker) recordSuccess(dbCtx context.Context, key string, lim providerHealthLimits) {
	st := t.state(key)
	st.mu.Lock()
	defer st.mu.Unlock()

	st.endRun(st.failures, lim.threshold, lim.limit)
	st.failures = 0
	st.push(false, lim.window)
	if !t.load(dbCtx, key, st) || st.alertID == 0 {
		return
	}
	recent := st.recentFailures()
	if recent >= lim.limit {
		// The text is brought up to date by the next failure.
		return
	}
	t.clear(dbCtx, key, st, fmt.Sprintf("a call succeeded, and %d of the last %d calls failed",
		recent, len(st.outcomes)))
}

// coolingDown reports whether this process cleared the alert for st too
// recently to raise it again. The caller holds st.mu.
func (t *providerHealthTracker) coolingDown(st *providerHealthState) bool {
	return !st.clearedAt.IsZero() && t.now().Sub(st.clearedAt) < t.cooldown
}

// limits is the provider health configuration in force. Each value out
// of range, which validation normally rejects, falls back to its
// default.
func (t *providerHealthTracker) limits() providerHealthLimits {
	cfg := t.settings()
	threshold := cfg.FailureThreshold
	if threshold < 1 {
		threshold = config.DefaultProviderFailureThreshold
	}
	window := cfg.FailureRateWindow
	if window < 1 || window > config.MaxProviderFailureRateWindow {
		window = config.DefaultProviderFailureRateWindow
	}
	rate := cfg.FailureRate
	if math.IsNaN(rate) || rate <= 0 || rate > 1 {
		rate = config.DefaultProviderFailureRate
	}
	return providerHealthLimits{threshold: threshold, window: window, limit: failureLimit(rate, window)}
}

// raise opens the provider health alert for key, or refreshes the open
// one when the provider's error or the failure counts have changed. The
// caller holds st.mu and has loaded st.
func (t *providerHealthTracker) raise(dbCtx context.Context, key string, st *providerHealthState,
	tier providerTier, provider, model string, callErr error, immediate bool, lim providerHealthLimits) {
	secrets := t.secrets()
	lastError := redactProviderError(providerErrorText(callErr, provider), secrets)
	counts := providerHealthCounts{
		consecutive: st.failures,
		recent:      st.recentFailures(),
		calls:       len(st.outcomes),
		limits:      lim,
	}
	description := providerHealthDescription(tier, provider, model, counts, immediate, lastError)
	details := providerHealthDetails(tier, provider, model, counts, immediate, lastError)

	errorChanged := st.alertID == 0 || lastError != st.lastError
	if !errorChanged && description == st.reportedDescription {
		return
	}
	if errorChanged {
		t.logCallError(key, callErr, secrets)
	}
	// refresh reports false when another process cleared the alert this
	// one still held, in which case a new alert is raised below.
	if st.alertID != 0 && t.refresh(dbCtx, key, st, st.alertID, description, details, lastError) {
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
		t.refresh(dbCtx, key, st, opened.ID, description, details, lastError)
		return
	}
	st.lastError = lastError
	st.reportedDescription = description
	t.log("Provider health alert raised: %s (%s)", alert.Title, lastError)
	t.notify(opened, database.NotificationTypeAlertFire)
}

// refresh rewrites an open provider health alert's text, recording
// lastError and the description only once the write has succeeded so
// that a failed write is retried on the next failure. It reports false,
// and forgets the alert,
// when the alert is no longer open, as happens when another alerter
// process cleared it; any other outcome reports true.
func (t *providerHealthTracker) refresh(dbCtx context.Context, key string, st *providerHealthState,
	alertID int64, description string, details *string, lastError string) bool {
	err := t.store.UpdateSystemAlert(dbCtx, alertID, description, details)
	switch {
	case errors.Is(err, database.ErrSystemAlertNotOpen):
		st.alertID = 0
		st.lastError = ""
		st.reportedDescription = ""
		return false
	case err != nil:
		t.log("ERROR: Failed to update provider health alert %s: %v", key, err)
	default:
		st.lastError = lastError
		st.reportedDescription = description
	}
	return true
}

// logCallError writes the provider error to the alerter's own log with
// the endpoint details the alert leaves out, so an operator can see
// which host refused the call. Credentials are still removed.
func (t *providerHealthTracker) logCallError(key string, callErr error, secrets []string) {
	t.log("Provider health %s: call failed: %s", key, redactProviderLogError(callErr.Error(), secrets))
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
	st.reportedDescription = ""
	st.clearedAt = t.now()
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

// providerHealthCounts is what an alert reports about the failures.
type providerHealthCounts struct {
	// consecutive is the number of consecutive failed calls.
	consecutive int

	// recent is the number of failed calls among the calls recorded.
	recent int

	// calls is the number of recent calls recorded.
	calls int

	limits providerHealthLimits
}

// byRate reports whether the alert is open on the failure rate rather
// than on a run of consecutive failures.
func (c providerHealthCounts) byRate() bool {
	return c.consecutive < c.limits.threshold
}

// providerHealthDescription is the alert's description.
func providerHealthDescription(tier providerTier, provider, model string,
	counts providerHealthCounts, startup bool, lastError string) string {
	var what string
	switch {
	case startup:
		what = fmt.Sprintf("The startup health check of the %s provider %s (model %s) failed.",
			strings.ToLower(tier.label()), provider, model)
	case counts.byRate():
		what = fmt.Sprintf("%d of the last %d %s calls to provider %s (model %s) have failed.",
			counts.recent, counts.calls, strings.ToLower(tier.label()), provider, model)
	default:
		what = fmt.Sprintf("%d consecutive %s calls to provider %s (model %s) have failed.",
			counts.consecutive, strings.ToLower(tier.label()), provider, model)
	}
	var clears string
	if counts.byRate() && !startup {
		clears = fmt.Sprintf("The alert clears once fewer than %d of the last %d calls to the provider have failed.",
			counts.limits.limit, counts.limits.window)
	} else {
		clears = fmt.Sprintf("The alert clears on the next successful call to the provider, unless "+
			"%d or more of its last %d calls are failing.", counts.limits.limit, counts.limits.window)
	}
	return fmt.Sprintf("%s %s %s Last error: %s", what, tier.consequence(), clears, lastError)
}

// providerHealthDetails is the alert's anomaly_details JSON, which gives
// a client the parts of the description separately.
func providerHealthDetails(tier providerTier, provider, model string,
	counts providerHealthCounts, startup bool, lastError string) *string {
	source := "runtime"
	if startup {
		source = "startup_check"
	}
	raw, err := json.Marshal(map[string]any{
		"tier":                 string(tier),
		"tier_label":           tier.label(),
		"provider":             provider,
		"model":                model,
		"consecutive_failures": counts.consecutive,
		"recent_failures":      counts.recent,
		"recent_calls":         counts.calls,
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
// a bearer or basic credential, the query string of a URL, a key or
// token parameter or header, a key-shaped value from a known provider,
// and the userinfo of a URL.
var providerErrorPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\b((?:bearer|basic)\s+)[A-Za-z0-9._~+/=-]+`), "${1}[REDACTED]"},
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://[^\s?#"'<>]*)\?[^\s"'<>#]*`), "${1}?[REDACTED]"},
	{regexp.MustCompile(`(?i)((?:api[_-]?key|x-api-key|x-goog-api-key|access[_-]?token|token|key|secret|password|authorization)["']?\s*[:=]\s*["']?)[^\s"'&,;}]+`), "${1}[REDACTED]"},
	{regexp.MustCompile(`\b(?:sk|pa|pk)-[A-Za-z0-9_\-]{8,}`), "[REDACTED]"},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{20,}`), "[REDACTED]"},
	{regexp.MustCompile(`(://)[^/@\s]+@`), "${1}[REDACTED]@"},
}

// redactedMarker replaces each credential removed from provider text.
const redactedMarker = "[REDACTED]"

// providerMarkup replaces the characters Slack mrkdwn and Mattermost
// Markdown treat as links or mentions (<url|text>, <!channel>,
// [text](url), @channel). The Slack and Mattermost notifiers escape
// their own markup, but the provider's text reaches the web client,
// email and webhook payloads too, so it is kept free of markup here as
// well.
var providerMarkup = strings.NewReplacer(
	"<", "(", ">", ")", "[", "(", "]", ")", "@", "＠",
)

// providerErrorText is the text of a failed provider call that an alert
// may repeat. A transport failure names the endpoint's host, address,
// port and path, which not every user with alert access should learn,
// so it is reduced to the operation and a category of failure.
func providerErrorText(err error, provider string) string {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err.Error()
	}
	op := strings.ToUpper(urlErr.Op)
	if op == "" || strings.ContainsFunc(op, func(r rune) bool { return r < 'A' || r > 'Z' }) {
		op = "Request"
	}
	return fmt.Sprintf("%s to %s endpoint failed: %s", op, provider, transportFailure(urlErr))
}

// transportFailure names the category of a transport error without
// repeating any of its detail.
func transportFailure(urlErr *url.Error) string {
	err := urlErr.Err
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var authorityErr x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var invalidErr x509.CertificateInvalidError
	var recordErr tls.RecordHeaderError
	switch {
	case urlErr.Timeout() || errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.As(err, &dnsErr):
		return "host name lookup failed"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection reset"
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return "host unreachable"
	case errors.As(err, &certErr), errors.As(err, &authorityErr),
		errors.As(err, &hostnameErr), errors.As(err, &invalidErr):
		return "TLS certificate verification failed"
	case errors.As(err, &recordErr):
		return "TLS handshake failed"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection closed by the server"
	default:
		return "network error"
	}
}

// redactProviderError prepares a provider error for an alert that every
// user with alert access, and every notification channel, will see. It
// removes credentials as redactProviderLogError does, replaces markup
// characters, and caps the length.
func redactProviderError(msg string, secrets []string) string {
	// Replace markup between the redaction markers, which stay intact.
	parts := strings.Split(redactProviderLogError(msg, secrets), redactedMarker)
	for i, part := range parts {
		parts[i] = providerMarkup.Replace(part)
	}
	msg = strings.Join(parts, redactedMarker)
	if len(msg) <= maxProviderErrorBytes {
		return msg
	}
	cut := maxProviderErrorBytes
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}
	return msg[:cut] + "…"
}

// redactProviderLogError deletes control and formatting characters,
// removes the configured secrets verbatim and anything that looks like
// a credential. It keeps endpoint details, for the alerter's own log.
func redactProviderLogError(msg string, secrets []string) string {
	msg = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) ||
			r == ' ' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToValidUTF8(msg, "�"))
	for _, secret := range secrets {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, redactedMarker)
		}
	}
	for _, p := range providerErrorPatterns {
		msg = p.re.ReplaceAllString(msg, p.repl)
	}
	return msg
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

// ProviderName implements llm.ReasoningProvider. It is the wrapped
// provider's own name, so the re-evaluation fingerprint (GitHub issue
// #575) is the same with or without health tracking.
func (h *healthTrackingReasoning) ProviderName() string {
	return h.inner.ProviderName()
}

// SystemPrompt implements llm.ReasoningProvider by delegating to the
// wrapped provider.
func (h *healthTrackingReasoning) SystemPrompt() string {
	return h.inner.SystemPrompt()
}

// healthCheck makes one cheap call and raises the alert for tier at
// once if it fails.
func (h *healthTrackingReasoning) healthCheck(ctx context.Context, tier providerTier) error {
	_, err := h.inner.Classify(ctx, providerHealthCheckPrompt)
	h.tracker.record(ctx, tier, h.provider, h.inner.ModelName(), err, true)
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

// minDerivedSecretLen is the shortest base_url userinfo or query value
// treated as a secret; removing a shorter one verbatim would mangle
// ordinary words in the error text.
const minDerivedSecretLen = 4

// configuredSecrets returns the API keys cfg holds, and the userinfo and
// query values of each provider's base_url.
func configuredSecrets(cfg *config.Config) []string {
	secrets := []string{
		cfg.GetOpenAIAPIKey(),
		cfg.GetAnthropicAPIKey(),
		cfg.GetVoyageAPIKey(),
		cfg.GetGeminiAPIKey(),
	}
	for _, base := range []string{
		cfg.LLM.Ollama.BaseURL,
		cfg.LLM.OpenAI.BaseURL,
		cfg.LLM.Anthropic.BaseURL,
		cfg.LLM.Voyage.BaseURL,
		cfg.LLM.Gemini.BaseURL,
	} {
		secrets = append(secrets, baseURLSecrets(base)...)
	}
	return secrets
}

// baseURLSecrets returns the userinfo and query values of a configured
// base URL, in decoded and escaped forms, longest first so that a value
// is removed before any shorter value inside it.
func baseURLSecrets(base string) []string {
	u, err := url.Parse(base)
	if err != nil {
		return nil
	}
	var values []string
	if u.User != nil {
		values = append(values, u.User.String(), u.User.Username())
		if password, ok := u.User.Password(); ok {
			values = append(values, password)
		}
	}
	// Query keeps the pairs that parse and drops malformed ones.
	for _, vs := range u.Query() {
		values = append(values, vs...)
	}
	seen := map[string]bool{}
	var secrets []string
	for _, v := range values {
		for _, form := range []string{v, url.QueryEscape(v), url.PathEscape(v)} {
			if len(form) >= minDerivedSecretLen && !seen[form] {
				seen[form] = true
				secrets = append(secrets, form)
			}
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return secrets
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
		func() config.ProviderHealthConfig { return e.getConfig().Anomaly.ProviderHealth },
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
// alerts that the running configuration can still raise and clear. A
// tier counts only while the current configuration enables it, since a
// configuration reload can disable a tier whose provider was built at
// startup.
func (e *Engine) activeProviderHealthKeys() map[string]bool {
	active := make(map[string]bool)
	cfg := e.getConfig()
	if !cfg.Anomaly.Enabled {
		return active
	}
	if emb, ok := e.embeddingProvider.(*healthTrackingEmbedding); ok && cfg.Anomaly.Tier2.Enabled {
		active[providerHealthKey(providerTierEmbedding, emb.provider)] = true
	}
	if rsn, ok := e.reasoningProvider.(*healthTrackingReasoning); ok {
		if cfg.Anomaly.Tier3.Enabled {
			active[providerHealthKey(providerTierClassification, rsn.provider)] = true
		}
		if cfg.Anomaly.Reevaluation.Enabled {
			active[providerHealthKey(providerTierReevaluation, rsn.provider)] = true
		}
	}
	return active
}

// reasoningHealthTier is the tier a failed reasoning provider health
// check raises its alert for: Tier 3 classification while it is enabled,
// otherwise re-evaluation while that is enabled. The reasoning provider
// is built only when Tier 3 is enabled at startup, so re-evaluation is
// chosen only when a configuration reload has disabled Tier 3 since. A
// successful check clears the alerts of both tiers either way. It
// reports false when neither uses the reasoning provider.
func reasoningHealthTier(cfg *config.Config) (providerTier, bool) {
	switch {
	case cfg.Anomaly.Tier3.Enabled:
		return providerTierClassification, true
	case cfg.Anomaly.Reevaluation.Enabled:
		return providerTierReevaluation, true
	default:
		return providerTierClassification, false
	}
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

	if emb, ok := e.embeddingProvider.(*healthTrackingEmbedding); ok && cfg.Anomaly.Tier2.Enabled {
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		if err := emb.healthCheck(checkCtx); err != nil {
			e.log("WARNING: Embedding provider %s failed its startup health check", emb.provider)
		}
		cancel()
	}
	if rsn, ok := e.reasoningProvider.(*healthTrackingReasoning); ok {
		tier, used := reasoningHealthTier(cfg)
		if !used {
			return
		}
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		if err := rsn.healthCheck(checkCtx, tier); err != nil {
			e.log("WARNING: Reasoning provider %s failed its startup health check", rsn.provider)
		}
		cancel()
	}
}
