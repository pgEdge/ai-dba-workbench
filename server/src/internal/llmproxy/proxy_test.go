/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package llmproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	"github.com/pgEdge/pgedge-go-llm-lib/llm/proxy"
	"golang.org/x/crypto/bcrypt"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/chat"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/memory"
	"github.com/pgedge/ai-workbench/server/internal/tracing"
)

// -----------------------------------------------------------------------
// Fake provider (registered globally, overriding "anthropic") for the
// end-to-end NewHandler round-trip test. It captures the dispatched
// ChatRequest so tests can assert the injected system prompt reached the
// provider layer.
// -----------------------------------------------------------------------

type fakeProvider struct {
	mu      sync.Mutex
	chatReq *pgllm.ChatRequest
}

var (
	fakeMu       sync.Mutex
	fakeInstance *fakeProvider
)

func setFake(f *fakeProvider) {
	fakeMu.Lock()
	defer fakeMu.Unlock()
	fakeInstance = f
}

func init() {
	// Override the "anthropic" provider with a test double so the proxy's
	// per-request constructor returns our capturing fake.
	pgllm.RegisterProvider("anthropic", func(_ pgllm.Options) (pgllm.Client, error) {
		fakeMu.Lock()
		defer fakeMu.Unlock()
		if fakeInstance == nil {
			return nil, errors.New("fake provider not initialized; call setFake() first")
		}
		return fakeInstance, nil
	})
}

func (f *fakeProvider) Chat(_ context.Context, req pgllm.ChatRequest) (*pgllm.ChatResponse, error) {
	f.mu.Lock()
	captured := req
	f.chatReq = &captured
	f.mu.Unlock()
	return &pgllm.ChatResponse{
		Content:    []pgllm.ContentBlock{{Type: pgllm.BlockText, Text: "hi"}},
		StopReason: pgllm.StopReasonEndTurn,
	}, nil
}

func (f *fakeProvider) capturedRequest() *pgllm.ChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chatReq
}

func (f *fakeProvider) ChatStream(context.Context, pgllm.ChatRequest) (*pgllm.Stream, error) {
	return nil, pgllm.ErrNotSupported
}
func (f *fakeProvider) Embed(context.Context, string) ([]float64, error) {
	return nil, pgllm.ErrNotSupported
}
func (f *fakeProvider) EmbedBatch(context.Context, []string) ([][]float64, error) {
	return nil, pgllm.ErrNotSupported
}
func (f *fakeProvider) Rerank(context.Context, pgllm.RerankRequest) (*pgllm.RerankResponse, error) {
	return nil, pgllm.ErrNotSupported
}
func (f *fakeProvider) EmbedMultimodal(context.Context, pgllm.MultimodalEmbedRequest) ([][]float64, error) {
	return nil, pgllm.ErrNotSupported
}
func (f *fakeProvider) ListModels(context.Context, ...pgllm.ListModelsOption) ([]string, error) {
	return []string{"claude-test"}, nil
}
func (f *fakeProvider) ListModelsWithMetadata(context.Context, ...pgllm.ListModelsOption) ([]pgllm.ModelInfo, error) {
	return []pgllm.ModelInfo{{ID: "claude-test"}}, nil
}
func (f *fakeProvider) Provider() string           { return "anthropic" }
func (f *fakeProvider) Model() string              { return "claude-test" }
func (f *fakeProvider) Usage() pgllm.TokenUsage    { return pgllm.TokenUsage{} }
func (f *fakeProvider) ResetUsage()                {}
func (f *fakeProvider) Ping(context.Context) error { return nil }

// -----------------------------------------------------------------------
// Auth store helper
// -----------------------------------------------------------------------

func newTestAuthStore(t *testing.T) *auth.AuthStore {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "llmproxy-authn-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create auth store: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)
	t.Cleanup(func() {
		store.Close()
		os.RemoveAll(tmpDir)
	})
	return store
}

// sessionTokenFor creates a user and returns a valid session token.
func sessionTokenFor(t *testing.T, store *auth.AuthStore, username string) string {
	t.Helper()
	if err := store.CreateUser(username, "Testpass1234", "test note", "Test User", ""); err != nil {
		t.Fatalf("failed to create user %q: %v", username, err)
	}
	token, _, err := store.AuthenticateUser(username, "Testpass1234")
	if err != nil {
		t.Fatalf("failed to authenticate %q: %v", username, err)
	}
	return token
}

// -----------------------------------------------------------------------
// buildProviders gating
// -----------------------------------------------------------------------

func TestBuildProviders_Gating(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want []string
	}{
		{"none", &Config{}, nil},
		{"anthropic key", &Config{AnthropicAPIKey: "k"}, []string{"anthropic"}},
		{"openai key", &Config{OpenAIAPIKey: "k"}, []string{"openai"}},
		{"openai baseurl only", &Config{OpenAIBaseURL: "http://x"}, []string{"openai"}},
		{"gemini key", &Config{GeminiAPIKey: "k"}, []string{"gemini"}},
		{"ollama url", &Config{OllamaURL: "http://x"}, []string{"ollama"}},
		{
			"all",
			&Config{AnthropicAPIKey: "k", OpenAIAPIKey: "k", GeminiAPIKey: "k", OllamaURL: "http://x"},
			[]string{"anthropic", "gemini", "ollama", "openai"},
		},
		{
			"anthropic key empty but baseurl set is not enough",
			&Config{AnthropicBaseURL: "http://x"},
			nil,
		},
		{
			"gemini baseurl alone is not enough",
			&Config{GeminiBaseURL: "http://x"},
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.buildProviders()
			if len(got) != len(tt.want) {
				t.Fatalf("expected providers %v, got keys %v", tt.want, keys(got))
			}
			for _, name := range tt.want {
				if _, ok := got[name]; !ok {
					t.Errorf("expected provider %q to be configured", name)
				}
			}
		})
	}
}

func keys(m map[string]pgllm.Options) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestProviderOptions_DefaultsAndHeaders(t *testing.T) {
	cfg := &Config{
		Model:       "claude-test",
		MaxTokens:   2048,
		Temperature: 0.5,
		LLMConfig:   &config.LLMConfig{TimeoutSeconds: 30},
	}
	opts := cfg.providerOptions("anthropic", "key", "http://base")
	if opts.APIKey != "key" {
		t.Errorf("expected APIKey 'key', got %q", opts.APIKey)
	}
	if opts.Model != "claude-test" {
		t.Errorf("expected Model 'claude-test', got %q", opts.Model)
	}
	if opts.BaseURL != "http://base" {
		t.Errorf("expected BaseURL 'http://base', got %q", opts.BaseURL)
	}
	if opts.MaxTokens == nil || *opts.MaxTokens != 2048 {
		t.Errorf("expected MaxTokens 2048, got %v", opts.MaxTokens)
	}
	if opts.Temperature == nil || *opts.Temperature != 0.5 {
		t.Errorf("expected Temperature 0.5, got %v", opts.Temperature)
	}
	if opts.RequestTimeout.Seconds() != 30 {
		t.Errorf("expected 30s timeout, got %v", opts.RequestTimeout)
	}
}

// -----------------------------------------------------------------------
// authorize
// -----------------------------------------------------------------------

func TestAuthorize_PublicPaths(t *testing.T) {
	cfg := &Config{} // no auth store needed for public paths
	for _, path := range []string{
		"/api/v1/llm/providers",
		"/api/v1/llm/models",
		"/api/v1/llm/health",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if err := cfg.authorize(req); err != nil {
			t.Errorf("expected public path %q to be allowed, got %v", path, err)
		}
	}
}

func TestAuthorize_MissingToken(t *testing.T) {
	store := newTestAuthStore(t)
	cfg := &Config{AuthStore: store}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	err := cfg.authorize(req)
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	var ae *proxy.AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *proxy.AuthError, got %T", err)
	}
	if ae.HTTPStatus() != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", ae.HTTPStatus())
	}
}

func TestAuthorize_InvalidToken(t *testing.T) {
	store := newTestAuthStore(t)
	cfg := &Config{AuthStore: store}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	req.Header.Set("Authorization", "Bearer bogus")
	err := cfg.authorize(req)
	var ae *proxy.AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *proxy.AuthError, got %T (%v)", err, err)
	}
	if ae.HTTPStatus() != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", ae.HTTPStatus())
	}
}

func TestAuthorize_ValidTokenAttachesContext(t *testing.T) {
	store := newTestAuthStore(t)
	token := sessionTokenFor(t, store, "alice")
	cfg := &Config{AuthStore: store}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if err := cfg.authorize(req); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	// The attached context must be readable on the same request object,
	// since the proxy reuses it for TransformRequest.
	if got := auth.GetUsernameFromContext(req.Context()); got != "alice" {
		t.Errorf("expected username 'alice' on request context, got %q", got)
	}
	if got := auth.GetUserIDFromContext(req.Context()); got <= 0 {
		t.Errorf("expected positive user ID on request context, got %d", got)
	}
}

// authorizeAttacksChatPath confirms that a /chat path is never matched by
// the public-suffix checks (no auth bypass).
func TestAuthorize_ChatRequiresAuth(t *testing.T) {
	store := newTestAuthStore(t)
	cfg := &Config{AuthStore: store}
	for _, path := range []string{
		"/api/v1/llm/chat",
		"/api/v1/llm/chat/stream",
		"/api/v1/llm/embed",
		"/api/v1/llm/rerank",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if err := cfg.authorize(req); err == nil {
			t.Errorf("expected auth required for %q, got nil error", path)
		}
	}
}

// -----------------------------------------------------------------------
// transformRequest
// -----------------------------------------------------------------------

// withIdentity returns a request whose context carries the given user
// identity, simulating a successful authorize.
func withIdentity(username string, userID int64) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	ctx := context.WithValue(req.Context(), auth.UsernameContextKey, username)
	ctx = context.WithValue(ctx, auth.UserIDContextKey, userID)
	return req.WithContext(ctx)
}

func TestTransformRequest_DefaultSystemPromptApplied(t *testing.T) {
	cfg := &Config{} // no stores
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	llmReq := &pgllm.ChatRequest{}
	if err := cfg.transformRequest(req, llmReq); err != nil {
		t.Fatalf("transformRequest error: %v", err)
	}
	if llmReq.SystemPrompt != chat.SystemPrompt {
		t.Errorf("expected default Ellie system prompt to be applied")
	}
}

func TestTransformRequest_PreservesProvidedSystemPrompt(t *testing.T) {
	cfg := &Config{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	llmReq := &pgllm.ChatRequest{SystemPrompt: "custom prompt"}
	if err := cfg.transformRequest(req, llmReq); err != nil {
		t.Fatalf("transformRequest error: %v", err)
	}
	if !strings.HasPrefix(llmReq.SystemPrompt, "custom prompt") {
		t.Errorf("expected provided prompt to be preserved as the base, got %q", llmReq.SystemPrompt)
	}
}

func TestTransformRequest_CompactDescriptions(t *testing.T) {
	cfg := &Config{
		UseCompactDescriptions: true,
		CompactDescriptions:    map[string]string{"query": "compact desc"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	llmReq := &pgllm.ChatRequest{
		Tools: []pgllm.Tool{
			{Name: "query", Description: "full desc"},
			{Name: "other", Description: "other desc"},
		},
	}
	if err := cfg.transformRequest(req, llmReq); err != nil {
		t.Fatalf("transformRequest error: %v", err)
	}
	if llmReq.Tools[0].CompactDescription != "compact desc" {
		t.Errorf("expected compact description set on 'query', got %q", llmReq.Tools[0].CompactDescription)
	}
	if llmReq.Tools[1].CompactDescription != "" {
		t.Errorf("expected no compact description for unmapped tool, got %q", llmReq.Tools[1].CompactDescription)
	}
	if llmReq.ToolDescriptions != pgllm.ToolDescriptionCompact {
		t.Errorf("expected ToolDescriptionCompact mode, got %q", llmReq.ToolDescriptions)
	}
}

func TestTransformRequest_UserContextInjection(t *testing.T) {
	store := newTestAuthStore(t)
	// Create user and resolve its ID.
	if err := store.CreateUser("bob", "Testpass1234", "bob note", "Bob", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	user, err := store.GetUser("bob")
	if err != nil || user == nil {
		t.Fatalf("failed to fetch user: %v", err)
	}

	cfg := &Config{AuthStore: store}
	req := withIdentity("bob", user.ID)
	llmReq := &pgllm.ChatRequest{}
	if err := cfg.transformRequest(req, llmReq); err != nil {
		t.Fatalf("transformRequest error: %v", err)
	}
	if !strings.Contains(llmReq.SystemPrompt, "<current-user>") {
		t.Errorf("expected user-context block in system prompt, got %q", llmReq.SystemPrompt)
	}
	if !strings.Contains(llmReq.SystemPrompt, "Username: bob") {
		t.Errorf("expected username in user-context block, got %q", llmReq.SystemPrompt)
	}
	// Default Ellie prompt should be the base.
	if !strings.HasPrefix(llmReq.SystemPrompt, chat.SystemPrompt) {
		t.Errorf("expected Ellie prompt as base before user context")
	}
}

// TestTransformRequest_MemoryThenUserContextOrder verifies that pinned
// memory wraps the base BEFORE the user-context block, matching the old
// HandleChat ordering. Requires a local Postgres via
// TEST_AI_WORKBENCH_SERVER; skipped when unset.
func TestTransformRequest_MemoryThenUserContextOrder(t *testing.T) {
	dsn := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if dsn == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set; skipping memory-injection DB test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to test DB: %v", err)
	}
	defer pool.Close()

	// Minimal chat_memories table scoped to this test.
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS chat_memories`); err != nil {
		t.Fatalf("failed to drop table: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		CREATE TABLE chat_memories (
			id          BIGSERIAL PRIMARY KEY,
			username    TEXT NOT NULL,
			scope       TEXT NOT NULL,
			category    TEXT NOT NULL,
			content     TEXT NOT NULL,
			pinned      BOOLEAN NOT NULL DEFAULT FALSE,
			model_name  TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS chat_memories`)
	})
	if _, err := pool.Exec(ctx,
		`INSERT INTO chat_memories (username, scope, category, content, pinned)
		 VALUES ($1, 'user', 'pref', 'prefers concise answers', TRUE)`,
		"carol"); err != nil {
		t.Fatalf("failed to insert memory: %v", err)
	}

	store := newTestAuthStore(t)
	if err := store.CreateUser("carol", "Testpass1234", "", "Carol", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	user, err := store.GetUser("carol")
	if err != nil || user == nil {
		t.Fatalf("failed to fetch user: %v", err)
	}

	cfg := &Config{
		AuthStore:   store,
		MemoryStore: memory.NewStore(pool),
	}
	req := withIdentity("carol", user.ID)
	llmReq := &pgllm.ChatRequest{}
	if err := cfg.transformRequest(req, llmReq); err != nil {
		t.Fatalf("transformRequest error: %v", err)
	}

	sp := llmReq.SystemPrompt
	memIdx := strings.Index(sp, "<user-stored-memories>")
	userIdx := strings.Index(sp, "<current-user>")
	if memIdx < 0 {
		t.Fatalf("expected memory block in system prompt, got %q", sp)
	}
	if userIdx < 0 {
		t.Fatalf("expected user-context block in system prompt, got %q", sp)
	}
	if memIdx > userIdx {
		t.Errorf("expected memory block BEFORE user-context block (memIdx=%d userIdx=%d)", memIdx, userIdx)
	}
	if !strings.Contains(sp, "prefers concise answers") {
		t.Errorf("expected memory content in prompt, got %q", sp)
	}
}

// -----------------------------------------------------------------------
// applyMemoryContext / applyUserContext guard branches
//
// These exercise the early-return guards in the two injection helpers that
// the happy-path transformRequest tests above do not reach, keeping each
// helper at or above the project's 90% line-coverage floor.
// -----------------------------------------------------------------------

func TestApplyMemoryContext_NilStore(t *testing.T) {
	cfg := &Config{} // MemoryStore is nil
	out := cfg.applyMemoryContext(context.Background(), "base")
	if out != "base" {
		t.Errorf("expected base unchanged when MemoryStore is nil, got %q", out)
	}
}

func TestApplyMemoryContext_NoUsername(t *testing.T) {
	// A non-nil store but no username in context must return base without
	// touching the store (GetPinned is never called).
	cfg := &Config{MemoryStore: memory.NewStore(nil)}
	out := cfg.applyMemoryContext(context.Background(), "base")
	if out != "base" {
		t.Errorf("expected base unchanged when context carries no username, got %q", out)
	}
}

// TestApplyMemoryContext_DBPaths covers the GetPinned error path and the
// empty-result path. It needs a live Postgres via TEST_AI_WORKBENCH_SERVER
// and is skipped when that is unset.
func TestApplyMemoryContext_DBPaths(t *testing.T) {
	dsn := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if dsn == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set; skipping memory-context DB test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to test DB: %v", err)
	}
	defer pool.Close()

	cfg := &Config{MemoryStore: memory.NewStore(pool)}
	userCtx := context.WithValue(ctx, auth.UsernameContextKey, "nobody")

	// Error path: with no chat_memories table present, GetPinned fails and
	// the helper logs a warning and returns base unchanged.
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS chat_memories`); err != nil {
		t.Fatalf("failed to drop table: %v", err)
	}
	if out := cfg.applyMemoryContext(userCtx, "base"); out != "base" {
		t.Errorf("expected base unchanged on GetPinned error, got %q", out)
	}

	// Empty-result path: table exists but the user has no pinned memories.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE chat_memories (
			id          BIGSERIAL PRIMARY KEY,
			username    TEXT NOT NULL,
			scope       TEXT NOT NULL,
			category    TEXT NOT NULL,
			content     TEXT NOT NULL,
			pinned      BOOLEAN NOT NULL DEFAULT FALSE,
			model_name  TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS chat_memories`)
	})
	if out := cfg.applyMemoryContext(userCtx, "base"); out != "base" {
		t.Errorf("expected base unchanged when no pinned memories, got %q", out)
	}
}

func TestApplyUserContext_NilStore(t *testing.T) {
	cfg := &Config{} // AuthStore is nil
	out := cfg.applyUserContext(context.Background(), "base")
	if out != "base" {
		t.Errorf("expected base unchanged when AuthStore is nil, got %q", out)
	}
}

func TestApplyUserContext_NoIdentity(t *testing.T) {
	// AuthStore present but the context carries no identity: userID is 0,
	// so the helper returns base without a lookup.
	cfg := &Config{AuthStore: newTestAuthStore(t)}
	out := cfg.applyUserContext(context.Background(), "base")
	if out != "base" {
		t.Errorf("expected base unchanged when context carries no identity, got %q", out)
	}
}

func TestApplyUserContext_UserNotFound(t *testing.T) {
	// A positive userID and a username that does not exist in the store:
	// buildUserInfo returns nil, so the helper returns base unchanged.
	cfg := &Config{AuthStore: newTestAuthStore(t)}
	out := cfg.applyUserContext(withIdentity("ghost", 999).Context(), "base")
	if out != "base" {
		t.Errorf("expected base unchanged when user cannot be looked up, got %q", out)
	}
}

// -----------------------------------------------------------------------
// NewHandler end-to-end
// -----------------------------------------------------------------------

func TestNewHandler_ProvidersEndpointPublic(t *testing.T) {
	cfg := &Config{
		Provider:        "anthropic",
		Model:           "claude-test",
		AnthropicAPIKey: "test-key",
		OpenAIAPIKey:    "test-key",
	}
	h, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/llm/providers", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from public providers endpoint, got %d (%s)", w.Code, w.Body.String())
	}
	var resp proxy.ProvidersResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode providers response: %v", err)
	}
	if len(resp.Providers) != 2 {
		t.Errorf("expected 2 configured providers, got %d", len(resp.Providers))
	}
}

func TestNewHandler_ChatRequiresAuth(t *testing.T) {
	store := newTestAuthStore(t)
	cfg := &Config{
		Provider:        "anthropic",
		Model:           "claude-test",
		AnthropicAPIKey: "test-key",
		AuthStore:       store,
	}
	h, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler error: %v", err)
	}

	body, _ := json.Marshal(proxy.ChatRequest{
		Messages: []pgllm.Message{pgllm.UserText("hello")},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated chat, got %d", w.Code)
	}
}

func TestNewHandler_ChatInjectsSystemPrompt(t *testing.T) {
	fake := &fakeProvider{}
	setFake(fake)
	t.Cleanup(func() { setFake(nil) })

	store := newTestAuthStore(t)
	token := sessionTokenFor(t, store, "dave")

	cfg := &Config{
		Provider:        "anthropic",
		Model:           "claude-test",
		AnthropicAPIKey: "test-key",
		AuthStore:       store,
	}
	h, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler error: %v", err)
	}

	body, _ := json.Marshal(proxy.ChatRequest{
		Messages: []pgllm.Message{pgllm.UserText("hello")},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from authed chat, got %d (%s)", w.Code, w.Body.String())
	}

	captured := fake.capturedRequest()
	if captured == nil {
		t.Fatal("provider never received a chat request")
	}
	// The injected Ellie prompt plus user-context must have reached the
	// provider, proving the authorize -> transformRequest context attach
	// propagated to the dispatched request.
	if !strings.HasPrefix(captured.SystemPrompt, chat.SystemPrompt) {
		t.Errorf("expected Ellie system prompt to reach provider, got %q", captured.SystemPrompt)
	}
	if !strings.Contains(captured.SystemPrompt, "Username: dave") {
		t.Errorf("expected user-context (Username: dave) to reach provider, got %q", captured.SystemPrompt)
	}

	// The response should carry the library wire contract.
	var resp proxy.ChatResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode chat response: %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "hi" {
		t.Errorf("unexpected response content: %+v", resp.Content)
	}
}

// -----------------------------------------------------------------------
// Tracing hooks
// -----------------------------------------------------------------------

// TestTracingHooks_DisabledNoPanic exercises the early-return guard in
// each hook when tracing is disabled. This is the default process state
// unless a prior test initialized the global tracer.
func TestTracingHooks_DisabledNoPanic(t *testing.T) {
	cfg := &Config{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)

	cfg.onRequest(req, proxy.RequestInfo{
		RequestID: "rid",
		Request: &pgllm.ChatRequest{
			Messages: []pgllm.Message{pgllm.UserText("hi")},
		},
	})
	cfg.onResponse(req, proxy.ResponseInfo{
		RequestID: "rid",
		Response: &pgllm.ChatResponse{
			Content: []pgllm.ContentBlock{{Type: pgllm.BlockText, Text: "ok"}},
		},
	})
	cfg.onError(req, proxy.ErrorInfo{RequestID: "rid", Err: errors.New("boom")})

	// Nil-info branches must also be safe.
	cfg.onRequest(req, proxy.RequestInfo{})
	cfg.onResponse(req, proxy.ResponseInfo{})
	cfg.onError(req, proxy.ErrorInfo{})
}

// TestTracingHooks_Enabled drives the logging bodies of the hooks with
// tracing turned on. tracing.Initialize is sync.Once-gated, so if a
// prior test already initialized the tracer this still exercises the
// hook bodies (guarded by IsEnabled) without asserting file output.
func TestTracingHooks_Enabled(t *testing.T) {
	traceFile := t.TempDir() + "/trace.jsonl"
	_ = tracing.Initialize(traceFile)
	if !tracing.IsEnabled() {
		t.Skip("tracer already initialized disabled in this process; logging body covered elsewhere")
	}

	cfg := &Config{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	ctx := context.WithValue(req.Context(), auth.TokenHashContextKey, "hash123")
	req = req.WithContext(ctx)

	cfg.onRequest(req, proxy.RequestInfo{
		RequestID: "rid",
		Request: &pgllm.ChatRequest{
			Messages: []pgllm.Message{
				pgllm.UserText("a user prompt"),
				pgllm.AssistantText("ignored assistant text"),
			},
		},
	})
	// A non-zero upstream Duration must be threaded from ResponseInfo
	// into the tracer. The tracer records it as duration_ms, so a
	// 1500ms upstream call is expected to appear as "duration_ms":1500.
	cfg.onResponse(req, proxy.ResponseInfo{
		RequestID: "rid",
		Response: &pgllm.ChatResponse{
			Content: []pgllm.ContentBlock{{Type: pgllm.BlockText, Text: "the answer"}},
		},
		Duration: 1500 * time.Millisecond,
	})
	cfg.onError(req, proxy.ErrorInfo{RequestID: "rid", Err: errors.New("kaboom")})

	if err := tracing.Close(); err != nil {
		t.Fatalf("failed to close tracer: %v", err)
	}
	data, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("failed to read trace file: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "a user prompt") {
		t.Errorf("expected user prompt in trace output")
	}
	if !strings.Contains(out, "the answer") {
		t.Errorf("expected LLM response in trace output")
	}
	if !strings.Contains(out, "kaboom") {
		t.Errorf("expected error in trace output")
	}
	// Prove the LLM-response duration was forwarded rather than the old
	// hardcoded zero: the response entry must carry the 1500ms value.
	if !strings.Contains(out, `"duration_ms":1500`) {
		t.Errorf("expected forwarded response duration_ms:1500 in trace output, got: %s", out)
	}
}

// -----------------------------------------------------------------------
// buildUserInfo with groups and admin permissions
// -----------------------------------------------------------------------

func TestBuildUserInfo_GroupsAndPermissions(t *testing.T) {
	store := newTestAuthStore(t)
	if err := store.CreateUser("erin", "Testpass1234", "erin note", "Erin", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	user, err := store.GetUser("erin")
	if err != nil || user == nil {
		t.Fatalf("failed to fetch user: %v", err)
	}

	groupID, err := store.CreateGroup("dbas", "Database admins")
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}
	if err := store.AddUserToGroup(groupID, user.ID); err != nil {
		t.Fatalf("failed to add user to group: %v", err)
	}
	if err := store.GrantAdminPermission(groupID, "manage_users"); err != nil {
		t.Fatalf("failed to grant admin permission: %v", err)
	}

	info := buildUserInfo(store, user.ID, "erin")
	if info == nil {
		t.Fatal("expected non-nil UserInfo")
	}
	if info.Username != "erin" || info.DisplayName != "Erin" || info.Notes != "erin note" {
		t.Errorf("unexpected user fields: %+v", info)
	}
	if len(info.Groups) != 1 || info.Groups[0] != "dbas" {
		t.Errorf("expected group 'dbas', got %v", info.Groups)
	}
	if len(info.AdminPerms) != 1 || info.AdminPerms[0] != "manage_users" {
		t.Errorf("expected admin perm 'manage_users', got %v", info.AdminPerms)
	}
}

func TestBuildUserInfo_UnknownUser(t *testing.T) {
	store := newTestAuthStore(t)
	if info := buildUserInfo(store, 999, "nobody"); info != nil {
		t.Errorf("expected nil UserInfo for unknown user, got %+v", info)
	}
}

// -----------------------------------------------------------------------
// Model-name validation
// -----------------------------------------------------------------------

func TestIsValidModelName(t *testing.T) {
	longName := strings.Repeat("a", 257)
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		// Valid model names: charset and length within bounds, no ".." substring.
		{"typical gemini name", "gemini-2.5-flash", true},
		{"typical anthropic name", "claude-3-7-sonnet-20250219", true},
		{"colon and slash allowed", "library/model:tag", true},
		{"single slash vendor/model", "vendor/model", true},
		{"single dot voyage", "voyage-3.5", true},
		{"single dot text-embedding", "text-embedding-3-small", true},
		{"single char", "a", true},
		{"exactly 256 chars", strings.Repeat("a", 256), true},

		// Invalid: empty or too long.
		{"empty is rejected by helper", "", false},
		{"exactly 257 chars", longName, false},

		// SECURITY: percent-encoded traversal is rejected because '%' is
		// outside the allowed charset; this is the vector that would decode
		// to a path separator at the upstream provider.
		{"percent-encoded traversal", "..%2f..%2fadmin", false},

		// SECURITY: literal ".." traversal is now rejected (VULN-001). The
		// characters '.' and '/' individually pass the charset check, but
		// any name containing the substring ".." is rejected to prevent
		// path-manipulation when the value is interpolated into a provider
		// URL (e.g. "/v1beta/models/{model}:generateContent").
		{"literal path traversal", "../../../../v1/admin", false},
		{"dotdot at start", "..%2f..%2fadmin", false},
		{"dotdot interior", "a..b", false},

		// Invalid: characters outside the allowed set.
		{"space", "gemini pro", false},
		{"control char", "gemini\n", false},
		{"backslash", "gemini\\pro", false},
		{"question mark", "model?foo=bar", false},
		{"unicode", "modèle", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidModelName(tt.model); got != tt.want {
				t.Errorf("isValidModelName(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

// TestValidateModelMiddleware_RejectsUnsafeModel drives the middleware
// directly: an unsafe per-request model on a model-bearing endpoint is
// rejected with 400 before the wrapped handler ever runs.
func TestValidateModelMiddleware_RejectsUnsafeModel(t *testing.T) {
	var reached bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	h := validateModelMiddleware(next)

	body, _ := json.Marshal(proxy.ChatRequest{
		Model:    "..%2f..%2fadmin",
		Messages: []pgllm.Message{pgllm.UserText("hi")},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if reached {
		t.Fatal("expected middleware to reject before reaching the wrapped handler")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsafe model, got %d (%s)", w.Code, w.Body.String())
	}
	var resp proxy.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if !strings.Contains(resp.Error, "invalid model name") {
		t.Errorf("expected invalid-model-name error, got %q", resp.Error)
	}
}

// TestValidateModelMiddleware_RejectsOversizedBody verifies that a request
// body larger than maxChatBodySize is rejected with 413 rather than being
// silently truncated to the cap and processed. A LimitReader at exactly the
// cap would have truncated the body and let the wrapped handler run on the
// prefix; reading cap+1 bytes detects the overflow and rejects it.
func TestValidateModelMiddleware_RejectsOversizedBody(t *testing.T) {
	var reached bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	h := validateModelMiddleware(next)

	// Build a body that is comfortably larger than the cap. The model field
	// is valid; the rejection must be driven purely by the body size, not by
	// model validation.
	padding := strings.Repeat("a", maxChatBodySize+1024)
	body, _ := json.Marshal(proxy.ChatRequest{
		Model:    "gemini-1.5-pro",
		Messages: []pgllm.Message{pgllm.UserText(padding)},
	})
	if int64(len(body)) <= maxChatBodySize {
		t.Fatalf("test setup: body %d bytes is not over the cap %d", len(body), maxChatBodySize)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if reached {
		t.Fatal("expected oversized body to be rejected before reaching the wrapped handler")
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d (%s)", w.Code, w.Body.String())
	}
	var resp proxy.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if !strings.Contains(resp.Error, "maximum allowed size") {
		t.Errorf("expected size-limit error, got %q", resp.Error)
	}
}

// TestValidateModelMiddleware_AcceptsBodyAtCap verifies that a body whose
// size is just under the cap is NOT rejected: the cap+1 read must not flag a
// within-limit body as oversized.
func TestValidateModelMiddleware_AcceptsBodyAtCap(t *testing.T) {
	var reached bool
	var gotBody []byte
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	h := validateModelMiddleware(next)

	body, _ := json.Marshal(proxy.ChatRequest{
		Model:    "gemini-1.5-pro",
		Messages: []pgllm.Message{pgllm.UserText("hello")},
	})
	if int64(len(body)) > maxChatBodySize {
		t.Fatalf("test setup: body %d bytes unexpectedly exceeds cap %d", len(body), maxChatBodySize)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if !reached {
		t.Fatal("expected within-cap body to reach the wrapped handler")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for within-cap body, got %d (%s)", w.Code, w.Body.String())
	}
	if !bytes.Equal(gotBody, body) {
		t.Errorf("body not restored intact for downstream handler")
	}
}

// TestValidateModelMiddleware_PassesValidAndEmptyModel confirms that a
// valid model and an omitted model (the operator default) both flow
// through to the wrapped handler with the body intact and re-readable.
func TestValidateModelMiddleware_PassesValidAndEmptyModel(t *testing.T) {
	cases := []struct {
		name  string
		model string
	}{
		{"valid model", "gemini-1.5-pro"},
		{"empty model uses default", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody []byte
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				gotBody = b
				w.WriteHeader(http.StatusOK)
			})
			h := validateModelMiddleware(next)

			body, _ := json.Marshal(proxy.ChatRequest{
				Model:    tc.model,
				Messages: []pgllm.Message{pgllm.UserText("hi")},
			})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", bytes.NewReader(body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
			}
			// The wrapped handler must still see the original, intact body.
			if !bytes.Equal(gotBody, body) {
				t.Errorf("body not restored for downstream handler:\n got %s\nwant %s", gotBody, body)
			}
		})
	}
}

// TestValidateModelMiddleware_NonModelRoutesPassThrough verifies that GET
// discovery endpoints and unknown paths bypass model validation entirely,
// body untouched.
func TestValidateModelMiddleware_NonModelRoutesPassThrough(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"GET providers", http.MethodGet, "/api/v1/llm/providers"},
		{"GET models", http.MethodGet, "/api/v1/llm/models"},
		{"POST unknown path", http.MethodPost, "/api/v1/llm/unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reached bool
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			})
			h := validateModelMiddleware(next)

			// A body with an unsafe model must NOT be inspected on these
			// routes, so the request still reaches the wrapped handler.
			body, _ := json.Marshal(modelEnvelope{Model: "..%2f..%2fadmin"})
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if !reached {
				t.Fatal("expected non-model route to pass through to wrapped handler")
			}
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
		})
	}
}

// TestValidateModelMiddleware_MalformedBodyDeferredToProxy confirms a body
// that is not valid JSON is passed through (the proxy reports its own
// parse error) rather than being rejected as an invalid model.
func TestValidateModelMiddleware_MalformedBodyDeferredToProxy(t *testing.T) {
	var reached bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		// The proxy would re-read and reject; emulate a pass-through read.
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	h := validateModelMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat",
		bytes.NewReader([]byte("{not json")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if !reached {
		t.Fatal("expected malformed body to be deferred to the wrapped handler")
	}
}

// errReadCloser is a request body whose Read always fails, used to drive
// the middleware's body-read error path.
type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("boom") }
func (errReadCloser) Close() error             { return nil }

// TestValidateModelMiddleware_BodyReadError confirms a body that fails to
// read is rejected with 400 and never reaches the wrapped handler.
func TestValidateModelMiddleware_BodyReadError(t *testing.T) {
	var reached bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	h := validateModelMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", nil)
	req.Body = errReadCloser{}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if reached {
		t.Fatal("expected body-read failure to reject before the wrapped handler")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on body-read error, got %d", w.Code)
	}
}

// TestNewHandler_ChatRejectsUnsafeModel exercises the full round-trip: an
// authenticated chat request carrying a path-manipulating model is
// rejected with 400 by the wrapped handler before any provider call.
func TestNewHandler_ChatRejectsUnsafeModel(t *testing.T) {
	fake := &fakeProvider{}
	setFake(fake)
	t.Cleanup(func() { setFake(nil) })

	store := newTestAuthStore(t)
	token := sessionTokenFor(t, store, "frank")

	cfg := &Config{
		Provider:        "anthropic",
		Model:           "claude-test",
		AnthropicAPIKey: "test-key",
		AuthStore:       store,
	}
	h, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler error: %v", err)
	}

	body, _ := json.Marshal(proxy.ChatRequest{
		Model:    "..%2f..%2fadmin",
		Messages: []pgllm.Message{pgllm.UserText("hello")},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/llm/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsafe model, got %d (%s)", w.Code, w.Body.String())
	}
	if fake.capturedRequest() != nil {
		t.Fatal("provider must not be reached for an unsafe model")
	}
}

func TestProviderOptions_NoLLMConfig(t *testing.T) {
	cfg := &Config{Model: "m"} // no LLMConfig -> no headers, no timeout
	opts := cfg.providerOptions("anthropic", "k", "")
	if opts.CustomHeaders != nil {
		t.Errorf("expected nil headers with no LLMConfig, got %v", opts.CustomHeaders)
	}
	if opts.RequestTimeout != 0 {
		t.Errorf("expected zero timeout with no LLMConfig, got %v", opts.RequestTimeout)
	}
	if opts.MaxTokens != nil {
		t.Errorf("expected nil MaxTokens when unset, got %v", opts.MaxTokens)
	}
}

// -----------------------------------------------------------------------
// BuildClientOptions — shared credential/options builder for the direct
// (non-proxy) analysis clients used by overview and server-info.
// -----------------------------------------------------------------------

// TestBuildClientOptions_ProviderCredentialSelection verifies that each
// provider branch selects the matching API key and base URL, that unknown
// providers select neither, and that the caller-supplied max-tokens and
// temperature are always applied along with the shared model.
func TestBuildClientOptions_ProviderCredentialSelection(t *testing.T) {
	cfg := &Config{
		Model:            "shared-model",
		AnthropicAPIKey:  "anthropic-key",
		AnthropicBaseURL: "http://anthropic",
		OpenAIAPIKey:     "openai-key",
		OpenAIBaseURL:    "http://openai",
		GeminiAPIKey:     "gemini-key",
		GeminiBaseURL:    "http://gemini",
		OllamaURL:        "http://ollama",
	}

	tests := []struct {
		provider    string
		wantAPIKey  string
		wantBaseURL string
	}{
		{"anthropic", "anthropic-key", "http://anthropic"},
		{"openai", "openai-key", "http://openai"},
		{"gemini", "gemini-key", "http://gemini"},
		{"ollama", "", "http://ollama"},
		{"unknown", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			cfg.Provider = tc.provider
			opts := cfg.BuildClientOptions(1234, 0.25)
			if opts.APIKey != tc.wantAPIKey {
				t.Errorf("APIKey: got %q, want %q", opts.APIKey, tc.wantAPIKey)
			}
			if opts.BaseURL != tc.wantBaseURL {
				t.Errorf("BaseURL: got %q, want %q", opts.BaseURL, tc.wantBaseURL)
			}
			if opts.Model != "shared-model" {
				t.Errorf("Model: got %q, want %q", opts.Model, "shared-model")
			}
			if opts.MaxTokens == nil || *opts.MaxTokens != 1234 {
				t.Errorf("MaxTokens: got %v, want 1234", opts.MaxTokens)
			}
			if opts.Temperature == nil || *opts.Temperature != 0.25 {
				t.Errorf("Temperature: got %v, want 0.25", opts.Temperature)
			}
		})
	}
}

// TestBuildClientOptions_TimeoutAndHeaders verifies that a positive
// configured timeout is applied and that provider-specific custom headers
// are wired into the returned Options.
func TestBuildClientOptions_TimeoutAndHeaders(t *testing.T) {
	cfg := &Config{
		Provider:        "anthropic",
		Model:           "m",
		AnthropicAPIKey: "k",
		LLMConfig: &config.LLMConfig{
			TimeoutSeconds:         45,
			AnthropicCustomHeaders: map[string]string{"X-Test": "value"},
		},
	}
	opts := cfg.BuildClientOptions(100, 0.5)
	if opts.RequestTimeout.Seconds() != 45 {
		t.Errorf("RequestTimeout: got %v, want 45s", opts.RequestTimeout)
	}
	if opts.CustomHeaders["X-Test"] != "value" {
		t.Errorf("CustomHeaders: got %v, want X-Test=value", opts.CustomHeaders)
	}
}

// TestBuildClientOptions_NoLLMConfig verifies the nil-LLMConfig guard: no
// headers are loaded and no timeout is applied, while the caller-supplied
// max-tokens and temperature are still set.
func TestBuildClientOptions_NoLLMConfig(t *testing.T) {
	cfg := &Config{Provider: "openai", Model: "m", OpenAIAPIKey: "k"}
	opts := cfg.BuildClientOptions(50, 0.1)
	if opts.CustomHeaders != nil {
		t.Errorf("expected nil headers with no LLMConfig, got %v", opts.CustomHeaders)
	}
	if opts.RequestTimeout != 0 {
		t.Errorf("expected zero timeout with no LLMConfig, got %v", opts.RequestTimeout)
	}
	if opts.MaxTokens == nil || *opts.MaxTokens != 50 {
		t.Errorf("MaxTokens: got %v, want 50", opts.MaxTokens)
	}
	if opts.Temperature == nil || *opts.Temperature != 0.1 {
		t.Errorf("Temperature: got %v, want 0.1", opts.Temperature)
	}
}

// TestBuildClientOptions_HeaderLoadErrorTreatedAsNoHeaders verifies that a
// header-loading failure (here, a header sourced from an unreadable file)
// is logged and treated as no headers so it never blocks analysis.
func TestBuildClientOptions_HeaderLoadErrorTreatedAsNoHeaders(t *testing.T) {
	cfg := &Config{
		Provider:        "anthropic",
		Model:           "m",
		AnthropicAPIKey: "k",
		LLMConfig: &config.LLMConfig{
			CustomHeadersFiles: map[string]string{
				"X-From-File": "/nonexistent/does/not/exist/header",
			},
		},
	}
	opts := cfg.BuildClientOptions(10, 0.2)
	if opts.CustomHeaders != nil {
		t.Errorf("expected nil headers when header loading fails, got %v", opts.CustomHeaders)
	}
	if opts.APIKey != "k" {
		t.Errorf("APIKey: got %q, want %q", opts.APIKey, "k")
	}
}

// TestBuildClientOptions_ZeroTimeoutNotApplied verifies that a zero or
// unset TimeoutSeconds leaves RequestTimeout at the library default (0),
// matching the timeout-only-when-positive rule.
func TestBuildClientOptions_ZeroTimeoutNotApplied(t *testing.T) {
	cfg := &Config{
		Provider:        "anthropic",
		Model:           "m",
		AnthropicAPIKey: "k",
		LLMConfig:       &config.LLMConfig{TimeoutSeconds: 0},
	}
	opts := cfg.BuildClientOptions(10, 0.2)
	if opts.RequestTimeout != 0 {
		t.Errorf("expected zero timeout when TimeoutSeconds=0, got %v", opts.RequestTimeout)
	}
}

// TestAnalysisMaxTokens verifies that the analysis paths pick up the
// operator-configured llm.max_tokens and fall back to
// DefaultAnalysisMaxTokens whenever the setting is absent or non-positive.
// A nil receiver is included because the overview generator holds a nil
// *Config when AI is disabled.
func TestAnalysisMaxTokens(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{name: "nil config", cfg: nil, want: DefaultAnalysisMaxTokens},
		{name: "unset", cfg: &Config{}, want: DefaultAnalysisMaxTokens},
		{name: "zero", cfg: &Config{MaxTokens: 0}, want: DefaultAnalysisMaxTokens},
		{name: "negative", cfg: &Config{MaxTokens: -1}, want: DefaultAnalysisMaxTokens},
		{name: "configured", cfg: &Config{MaxTokens: 8192}, want: 8192},
		{name: "small but positive", cfg: &Config{MaxTokens: 64}, want: 64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.AnalysisMaxTokens(); got != tc.want {
				t.Errorf("AnalysisMaxTokens() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestBuildClientOptions_AnalysisMaxTokensFlowsThrough verifies that the
// budget AnalysisMaxTokens reports reaches the library Options the analysis
// call sites construct.
func TestBuildClientOptions_AnalysisMaxTokensFlowsThrough(t *testing.T) {
	cfg := &Config{
		Provider:        "anthropic",
		Model:           "m",
		AnthropicAPIKey: "k",
		MaxTokens:       12288,
	}
	opts := cfg.BuildClientOptions(cfg.AnalysisMaxTokens(), 0.3)
	if opts.MaxTokens == nil || *opts.MaxTokens != 12288 {
		t.Errorf("opts.MaxTokens = %v, want 12288", opts.MaxTokens)
	}
}
