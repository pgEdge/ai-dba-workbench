/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package overview

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/ratelimit"
)

// RateLimits configures the per-user limits on the LLM generations the
// overview endpoints may trigger. They guard the operator's LLM
// account against accidental runaway cost, such as a script in a loop
// or a browser tab stuck reconnecting, rather than against hostile
// users. A zero GenerationsPerHour or RefreshInterval disables that
// limit.
type RateLimits struct {
	// GenerationsPerHour is the sustained number of summary
	// generations each user may trigger per hour.
	GenerationsPerHour int

	// Burst is how many generations a user may trigger in quick
	// succession before the hourly rate applies.
	Burst int

	// RefreshInterval is the minimum time between one user's forced
	// refreshes (refresh=true).
	RefreshInterval time.Duration
}

// generationLimits holds the limiters built from RateLimits. The zero
// value, with nil limiters, limits nothing.
type generationLimits struct {
	generate *ratelimit.Limiter
	refresh  *ratelimit.Limiter
}

// SetRateLimits applies per-user generation limits to the handler. It
// must be called before the handler serves requests.
func (h *Handler) SetRateLimits(limits RateLimits) {
	h.limits = generationLimits{
		generate: ratelimit.PerHour(limits.GenerationsPerHour, limits.Burst),
		refresh:  ratelimit.New(limits.RefreshInterval, 1),
	}
}

// admission is the outcome of checking a request against the caller's
// generation limits.
type admission struct {
	// force reports that the caller may bypass a fresh cached summary.
	force bool

	// cached, when set, is a summary to serve in place of generating
	// one, because the caller has used up their generations.
	cached *Overview
}

// admitGeneration checks a request for the summary cached under key
// against the caller's limits. A refresh beyond the refresh throttle
// is treated as an ordinary request. A request that a fresh cached
// summary satisfies costs nothing. Otherwise the request spends one of
// the caller's generations; once those are used up the cached summary
// is served even if stale, and only when there is none does the
// function write a 429 response and return false.
func (h *Handler) admitGeneration(w http.ResponseWriter, r *http.Request, key string, refresh bool) (admission, bool) {
	user := auth.GetUsernameFromContext(r.Context())
	cached, fresh := h.generator.cachedSummary(key)

	if refresh {
		if ok, _ := h.limits.refresh.Allow(user); !ok {
			refresh = false
		}
	}
	if !refresh && fresh {
		return admission{}, true
	}

	ok, wait := h.limits.generate.Allow(user)
	if ok {
		return admission{force: refresh}, true
	}
	if cached != nil {
		return admission{cached: cached}, true
	}
	writeRateLimited(w, wait)
	return admission{}, false
}

// allowEstateRefresh reports whether the caller may force a
// regeneration of the estate-wide overview. When they may not, the
// caller serves the current estate overview instead.
func (h *Handler) allowEstateRefresh(r *http.Request) bool {
	user := auth.GetUsernameFromContext(r.Context())
	if ok, _ := h.limits.refresh.Allow(user); !ok {
		return false
	}
	ok, _ := h.limits.generate.Allow(user)
	return ok
}

// connectionsSummary returns the summary for an explicit connection
// list, subject to the caller's generation limits. When ok is false a
// 429 response has been written and the caller must return.
func (h *Handler) connectionsSummary(w http.ResponseWriter, r *http.Request, ids []int, scopeName string, refresh bool) (*Overview, bool, error) {
	adm, ok := h.admitGeneration(w, r, connectionsCacheKey(ids), refresh)
	if !ok {
		return nil, false, nil
	}
	if adm.cached != nil {
		return adm.cached, true, nil
	}
	overview, err := h.generator.GetConnectionsSummary(ids, scopeName, adm.force)
	return overview, true, err
}

// scopedSummary returns the summary for a server, cluster or group
// scope, subject to the caller's generation limits. When ok is false a
// 429 response has been written and the caller must return.
func (h *Handler) scopedSummary(w http.ResponseWriter, r *http.Request, scopeType string, scopeID int, refresh bool) (*Overview, bool, error) {
	adm, ok := h.admitGeneration(w, r, scopedCacheKey(scopeType, scopeID), refresh)
	if !ok {
		return nil, false, nil
	}
	if adm.cached != nil {
		return adm.cached, true, nil
	}
	overview, err := h.generator.GetScopedSummary(scopeType, scopeID, adm.force)
	return overview, true, err
}

// admitStream checks an SSE subscription against the caller's
// generation limits before the stream starts. Streams that generate
// nothing (the estate feed, and a scope in which the caller can see no
// connections) always pass. The returned overview, when set, is a
// stale cached summary to send in place of generating one. When ok is
// false a 429 response has been written and the caller must return.
func (h *Handler) admitStream(w http.ResponseWriter, r *http.Request, scopeKey string) (*Overview, bool) {
	if scopeKey == "" || scopeKey == connectionsCacheKey(nil) {
		return nil, true
	}
	adm, ok := h.admitGeneration(w, r, scopeKey, false)
	return adm.cached, ok
}

// writeRateLimited writes a 429 response telling the client how many
// seconds to wait before trying again.
func writeRateLimited(w http.ResponseWriter, wait time.Duration) {
	seconds := int(wait / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	http.Error(w, "Too many AI overview requests; try again later", http.StatusTooManyRequests)
}

// connectionsCacheKey returns the summary cache key, which is also the
// SSE scope key, for a list of connection IDs. The IDs are sorted so
// that the same set always maps to the same key. It must match the key
// GetConnectionsSummary builds; TestCacheKeysMatchGenerator checks it.
func connectionsCacheKey(ids []int) string {
	sorted := make([]int, len(ids))
	copy(sorted, ids)
	sortInts(sorted)

	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = strconv.Itoa(id)
	}
	return "connections:" + strings.Join(parts, ",")
}

// scopedCacheKey returns the summary cache key, which is also the SSE
// scope key, for a server, cluster or group scope. It must match the
// key GetScopedSummary builds; TestCacheKeysMatchGenerator checks it.
func scopedCacheKey(scopeType string, scopeID int) string {
	return fmt.Sprintf("%s:%d", scopeType, scopeID)
}

// cachedSummary returns the summary cached under key, whether or not
// it is stale, and reports whether it is still fresh. It does not
// count as an access for eviction purposes.
func (g *Generator) cachedSummary(key string) (*Overview, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	entry, ok := g.scopedCache[key]
	if !ok {
		return nil, false
	}
	return entry.overview, time.Now().UTC().Before(entry.overview.StaleAt)
}
