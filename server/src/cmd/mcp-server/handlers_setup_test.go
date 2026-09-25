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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/conversations"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/overview"
)

// stubToolProvider is the smallest ContextAwareToolProvider the server
// will accept: SetupHandlers only needs List for the compact-description
// lookup and the REST bridge only needs the interface satisfied.
type stubToolProvider struct {
	tools     []mcp.Tool
	listCalls int
}

func (p *stubToolProvider) List() []mcp.Tool {
	p.listCalls++
	return p.tools
}

func (p *stubToolProvider) ListForContext(context.Context) []mcp.Tool {
	return p.tools
}

func (p *stubToolProvider) Execute(context.Context, string,
	map[string]any) (mcp.ToolResponse, error) {

	return mcp.ToolResponse{}, nil
}

// routeRegistered reports whether the mux has a handler for the given
// path, which is how these tests tell a registered endpoint from one the
// mux would answer with 404.
func routeRegistered(t *testing.T, mux *http.ServeMux, path string) bool {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	_, pattern := mux.Handler(req)
	return pattern != ""
}

// TestHandleOpenAPISpecServesSpec checks the unauthenticated schema
// endpoint: a GET returns the built specification, and any other method
// is refused rather than being treated as a read.
func TestHandleOpenAPISpecServesSpec(t *testing.T) {
	rec := httptest.NewRecorder()
	handleOpenAPISpec(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var spec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("Failed to decode spec: %v", err)
	}
	if spec["openapi"] == nil {
		t.Error("Expected an openapi version in the served specification")
	}
	if spec["paths"] == nil {
		t.Error("Expected paths in the served specification")
	}

	rec = httptest.NewRecorder()
	handleOpenAPISpec(rec, httptest.NewRequest(http.MethodPost, "/api/v1/openapi.json", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for POST, got %d", rec.Code)
	}
}

// TestRegisterDatastoreHandlerReportsStatus checks the status line the
// helper writes, since that line is how an operator reading the start-up
// output learns whether an endpoint has a datastore behind it.
func TestRegisterDatastoreHandlerReportsStatus(t *testing.T) {
	tests := []struct {
		name      string
		datastore any
		want      string
	}{
		{name: "with datastore", datastore: struct{}{}, want: "Timeline events: ENABLED"},
		{name: "without datastore", datastore: nil, want: "Timeline events: DISABLED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			handler := &stubRouteRegistrar{}

			output := captureStderr(t, func() {
				registerDatastoreHandler(mux, handler, nil,
					"Timeline events", tt.datastore)
			})

			if !handler.registered {
				t.Error("Expected the handler's routes to be registered")
			}
			if !strings.Contains(output, tt.want) {
				t.Errorf("Expected %q in the status output, got %q", tt.want, output)
			}
		})
	}
}

// stubRouteRegistrar records that RegisterRoutes was called.
type stubRouteRegistrar struct {
	registered bool
}

func (s *stubRouteRegistrar) RegisterRoutes(*http.ServeMux,
	func(http.HandlerFunc) http.HandlerFunc) {

	s.registered = true
}

// TestSetupHandlersRegistersOptionalEndpoints wires every optional
// dependency and checks that the endpoints each one guards are actually
// registered, since a missing route is served as a 404 that the web
// client reports as a broken feature rather than a disabled one.
func TestSetupHandlersRegistersOptionalEndpoints(t *testing.T) {
	store, _ := newWrapperTestStore(t)

	provider := &stubToolProvider{tools: []mcp.Tool{
		{Name: "list_tables", CompactDescription: "List tables"},
		{Name: "run_query"},
	}}

	cfg := &config.Config{}
	cfg.LLM.MaxIterations = 7

	deps := &HandlerDependencies{
		AuthStore: store,
		// A datastore with no pool is enough: these handlers are only
		// registered here, never invoked, and the pool is what a call
		// would reach for.
		Datastore:    database.NewTestDatastore(nil),
		ConvStore:    conversations.NewStore(nil),
		ToolProvider: provider,
		OverviewGen:  &overview.Generator{},
		Config:       cfg,
	}

	mux := http.NewServeMux()
	output := captureStderr(t, func() {
		if err := SetupHandlers(deps)(mux); err != nil {
			t.Fatalf("SetupHandlers returned an error: %v", err)
		}
	})

	for _, path := range []string{
		"/api/v1/mcp/tools",
		"/api/v1/conversations",
		"/api/v1/overview",
		"/api/v1/memories",
		"/api/v1/rbac/groups",
		"/api/v1/llm/providers",
	} {
		if !routeRegistered(t, mux, path) {
			t.Errorf("Expected %s to be registered", path)
		}
	}

	if provider.listCalls == 0 {
		t.Error("Expected the tool provider to be consulted for compact descriptions")
	}
	if !strings.Contains(output, "Memory management: ENABLED") {
		t.Errorf("Expected memory management to be reported as enabled, got %q", output)
	}

	// The capabilities endpoint is public and reports the configured
	// iteration cap, which the client uses to bound an agent run.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 from capabilities, got %d", rec.Code)
	}
	var caps map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Fatalf("Failed to decode capabilities: %v", err)
	}
	if caps["max_iterations"] != float64(7) {
		t.Errorf("Expected max_iterations 7, got %v", caps["max_iterations"])
	}
}
