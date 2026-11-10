# Per-User Rate Limiting

`server/src/internal/ratelimit` holds a keyed token-bucket limiter for
capping LLM spend per user (issue #822).

- `ratelimit.New(interval, burst)` refills one token per `interval`;
  `ratelimit.PerHour(count, burst)` is the hourly form. Both return nil
  for a non-positive rate, and a nil `*Limiter` allows everything, so
  "0 disables" needs no special casing at call sites.
- `Allow(key)` returns `(ok, wait)`; `wait` is rounded up to whole
  seconds, ready for a `Retry-After` header. Full buckets are swept at
  most once a minute, so memory tracks active users only.
- Only the AI Overview endpoints use it, wired in
  `server/src/internal/overview/ratelimit.go` from
  `llm.rate_limit.overview` (`config.OverviewRateLimitConfig`, pointer
  fields with accessor defaults). The settings are read at start-up via
  `overviewRateLimits` in `cmd/mcp-server/handlers.go`, not on SIGHUP.
- Overview admission prefers a cached summary to a 429: a fresh cache
  hit is free, and a user out of tokens gets the stale entry if any.
  Connection lists are looked up with `connectionsKey(ids,
  sanitizeScopeName(name))`, the key `GetConnectionsSummary` builds, so
  a named or restricted-scope summary is found; `scopedCacheKey` must
  match the key `GetScopedSummary` builds inline.
  `TestCacheKeysMatchGenerator` and `TestRateLimit_PartialScopeCacheHit`
  fail if they drift.
- To limit another LLM feature (chat, server analysis), add a sibling
  under `llm.rate_limit.<feature>` and build a separate limiter, keyed
  by username, rather than sharing the overview buckets.
