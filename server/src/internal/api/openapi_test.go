/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildOpenAPISpec(t *testing.T) {
	spec := BuildOpenAPISpec()

	// Verify basic structure
	if spec.OpenAPI != "3.0.3" {
		t.Errorf("Expected OpenAPI version 3.0.3, got %s", spec.OpenAPI)
	}

	if spec.Info.Title != "AI DBA Workbench API" {
		t.Errorf("Expected title 'AI DBA Workbench API', got %s", spec.Info.Title)
	}

	if spec.Info.Version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", spec.Info.Version)
	}

	// Verify we have servers
	if len(spec.Servers) == 0 {
		t.Error("Expected at least one server defined")
	}

	// Verify we have paths
	if len(spec.Paths) == 0 {
		t.Error("Expected at least one path defined")
	}

	// Verify key paths exist
	keyPaths := []string{
		"/auth/login",
		"/user/info",
		"/connections",
		"/connections/{id}",
		"/connections/current",
		"/clusters",
		"/cluster-groups",
		"/alerts",
		"/alerts/counts",
		"/timeline/events",
		"/conversations",
		"/llm/providers",
		"/llm/models",
		"/llm/chat",
		"/chat/compact",
	}

	for _, path := range keyPaths {
		if _, ok := spec.Paths[path]; !ok {
			t.Errorf("Expected path %s to be defined", path)
		}
	}

	// Verify components exist
	if len(spec.Components.Schemas) == 0 {
		t.Error("Expected schemas to be defined")
	}

	if len(spec.Components.SecuritySchemes) == 0 {
		t.Error("Expected security schemes to be defined")
	}

	// Verify bearerAuth security scheme
	if _, ok := spec.Components.SecuritySchemes["bearerAuth"]; !ok {
		t.Error("Expected bearerAuth security scheme to be defined")
	}

	// Verify key schemas exist
	keySchemas := []string{
		"ErrorResponse",
		"LoginRequest",
		"LoginResponse",
		"Connection",
		"Cluster",
		"ClusterGroup",
		"Alert",
		"TimelineEvent",
		"Conversation",
		"Message",
	}

	for _, schema := range keySchemas {
		if _, ok := spec.Components.Schemas[schema]; !ok {
			t.Errorf("Expected schema %s to be defined", schema)
		}
	}
}

func TestBuildOpenAPISpec_JSONSerialization(t *testing.T) {
	spec := BuildOpenAPISpec()

	// Verify it can be serialized to JSON
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("Failed to marshal OpenAPI spec to JSON: %v", err)
	}

	// Verify it's valid JSON by unmarshaling it back
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal OpenAPI spec JSON: %v", err)
	}

	// Verify key fields are present
	if _, ok := result["openapi"]; !ok {
		t.Error("Expected 'openapi' field in JSON output")
	}
	if _, ok := result["info"]; !ok {
		t.Error("Expected 'info' field in JSON output")
	}
	if _, ok := result["paths"]; !ok {
		t.Error("Expected 'paths' field in JSON output")
	}
	if _, ok := result["components"]; !ok {
		t.Error("Expected 'components' field in JSON output")
	}
}

func TestBuildOpenAPISpec_PathOperations(t *testing.T) {
	spec := BuildOpenAPISpec()

	// Test /auth/login has POST operation
	loginPath := spec.Paths["/auth/login"]
	if loginPath.Post == nil {
		t.Error("Expected POST operation on /auth/login")
	}
	if loginPath.Post.OperationID != "login" {
		t.Errorf("Expected operationId 'login', got %s", loginPath.Post.OperationID)
	}

	// Test /connections has both GET and POST
	connPath := spec.Paths["/connections"]
	if connPath.Get == nil {
		t.Error("Expected GET operation on /connections")
	}
	if connPath.Post == nil {
		t.Error("Expected POST operation on /connections")
	}

	// Test /connections/{id} has GET, PUT, DELETE
	connIDPath := spec.Paths["/connections/{id}"]
	if connIDPath.Get == nil {
		t.Error("Expected GET operation on /connections/{id}")
	}
	if connIDPath.Put == nil {
		t.Error("Expected PUT operation on /connections/{id}")
	}
	if connIDPath.Delete == nil {
		t.Error("Expected DELETE operation on /connections/{id}")
	}

	// Test /conversations/{id} has GET, PUT, PATCH, DELETE
	convIDPath := spec.Paths["/conversations/{id}"]
	if convIDPath.Get == nil {
		t.Error("Expected GET operation on /conversations/{id}")
	}
	if convIDPath.Put == nil {
		t.Error("Expected PUT operation on /conversations/{id}")
	}
	if convIDPath.Patch == nil {
		t.Error("Expected PATCH operation on /conversations/{id}")
	}
	if convIDPath.Delete == nil {
		t.Error("Expected DELETE operation on /conversations/{id}")
	}
}

func TestBuildOpenAPISpec_SecurityRequirements(t *testing.T) {
	spec := BuildOpenAPISpec()

	// Endpoints that should require auth
	authRequiredPaths := []string{
		"/connections",
		"/clusters",
		"/alerts",
		"/timeline/events",
		"/conversations",
		"/llm/chat",
		"/chat/compact",
	}

	for _, path := range authRequiredPaths {
		pathItem, ok := spec.Paths[path]
		if !ok {
			continue
		}

		// Check GET if exists
		if pathItem.Get != nil && len(pathItem.Get.Security) == 0 {
			// /llm/providers and /llm/models don't require auth
			if path != "/llm/providers" && path != "/llm/models" {
				t.Errorf("Expected security requirement on GET %s", path)
			}
		}

		// Check POST if exists
		if pathItem.Post != nil && len(pathItem.Post.Security) == 0 {
			// /auth/login doesn't require auth
			if path != "/auth/login" {
				t.Errorf("Expected security requirement on POST %s", path)
			}
		}
	}

	// Endpoints that should NOT require auth
	noAuthPaths := map[string]string{
		"/auth/login":    "Post",
		"/user/info":     "Get",
		"/llm/providers": "Get",
		"/llm/models":    "Get",
	}

	for path, method := range noAuthPaths {
		pathItem, ok := spec.Paths[path]
		if !ok {
			t.Errorf("Expected path %s to exist", path)
			continue
		}

		var op *OpenAPIOperation
		switch method {
		case "Get":
			op = pathItem.Get
		case "Post":
			op = pathItem.Post
		}

		if op != nil && len(op.Security) > 0 {
			t.Errorf("Expected no security requirement on %s %s", method, path)
		}
	}
}

func TestBuildOpenAPISpec_MetricsQueryCustomWindowParams(t *testing.T) {
	// The custom time-range feature is only discoverable through the spec,
	// so /metrics/query must document both timestamps and advertise the
	// custom value on time_range. The perf-summary paths keep the preset
	// list, since they do not accept a custom window.
	spec := BuildOpenAPISpec()

	op := spec.Paths["/metrics/query"].Get
	if op == nil {
		t.Fatal("Expected GET operation on /metrics/query")
	}

	params := make(map[string]string, len(op.Parameters))
	for _, p := range op.Parameters {
		params[p.Name] = p.Description
	}

	for _, name := range []string{"time_start", "time_end"} {
		desc, ok := params[name]
		if !ok {
			t.Errorf("Expected %s query parameter on /metrics/query", name)
			continue
		}
		if !strings.Contains(desc, "RFC 3339") {
			t.Errorf("Expected %s description to mention RFC 3339, got %q",
				name, desc)
		}
	}
	if desc := params["time_range"]; !strings.Contains(desc, "custom") {
		t.Errorf("Expected time_range description to mention custom, got %q",
			desc)
	}

	perfOp := spec.Paths["/metrics/performance-summary"].Get
	if perfOp == nil {
		t.Fatal("Expected GET operation on /metrics/performance-summary")
	}
	for _, p := range perfOp.Parameters {
		if p.Name == "time_start" || p.Name == "time_end" {
			t.Errorf("Unexpected %s parameter on /metrics/performance-summary",
				p.Name)
		}
	}
}
