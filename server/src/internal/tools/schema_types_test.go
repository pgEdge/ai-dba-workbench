/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/resources"
)

// untypedSchemaProperties returns the path of every property in props,
// at any depth (array items and nested object properties included),
// that does not declare a type. Some providers, Gemini among them,
// reject a whole request when any parameter schema lacks a type.
func untypedSchemaProperties(path string, props map[string]any) []string {
	var bad []string
	for name, raw := range props {
		p := path + "." + name
		node, ok := raw.(map[string]any)
		if !ok {
			bad = append(bad, p+" (not an object)")
			continue
		}
		bad = append(bad, untypedSchemaNode(p, node)...)
	}
	return bad
}

// untypedSchemaNode checks one schema node and recurses into its items
// and properties.
func untypedSchemaNode(path string, node map[string]any) []string {
	var bad []string
	if t, ok := node["type"].(string); !ok || t == "" {
		bad = append(bad, path)
	}
	if items, ok := node["items"]; ok {
		itemNode, ok := items.(map[string]any)
		if !ok {
			bad = append(bad, path+"[] (not an object)")
		} else {
			bad = append(bad, untypedSchemaNode(path+"[]", itemNode)...)
		}
	}
	if nested, ok := node["properties"].(map[string]any); ok {
		bad = append(bad, untypedSchemaProperties(path, nested)...)
	}
	return bad
}

// TestToolSchemasDeclareTypes walks every tool's input schema and fails
// on any property without a type.
func TestToolSchemasDeclareTypes(t *testing.T) {
	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()
	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)
	provider := NewContextAwareProvider(clientManager, resourceReg, database.NewClient(nil), cfg, nil, nil, nil)
	if err := provider.RegisterTools(context.TODO()); err != nil {
		t.Fatalf("RegisterTools failed: %v", err)
	}

	defs := provider.List()
	// These tools are registered only with a memory store or a
	// knowledgebase file, so they are built directly here.
	for _, tool := range []Tool{
		StoreMemoryTool(nil, cfg, nil),
		RecallMemoriesTool(nil, cfg),
		DeleteMemoryTool(nil),
		SearchKnowledgebaseTool("", cfg),
	} {
		defs = append(defs, tool.Definition)
	}
	if len(defs) < 20 {
		t.Fatalf("expected at least 20 tools, got %d", len(defs))
	}

	for _, def := range defs {
		if def.InputSchema.Type != "object" {
			t.Errorf("%s: input schema type = %q, want object", def.Name, def.InputSchema.Type)
		}
		if bad := untypedSchemaProperties(def.Name, def.InputSchema.Properties); len(bad) > 0 {
			t.Errorf("%s: properties without a type: %s", def.Name, strings.Join(bad, ", "))
		}
	}
}

// TestUntypedSchemaPropertiesDetects proves the walker finds a missing
// type at the top level, inside array items and in nested properties.
func TestUntypedSchemaPropertiesDetects(t *testing.T) {
	props := map[string]any{
		"ok":    map[string]any{"type": "string"},
		"top":   map[string]any{"description": "no type"},
		"weird": "not a map",
		"list": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"value": map[string]any{"description": "polymorphic"},
					"fine":  map[string]any{"type": "integer"},
				},
			},
		},
		"untypeditems": map[string]any{"type": "array", "items": map[string]any{}},
		"baditems":     map[string]any{"type": "array", "items": "string"},
	}
	found := map[string]bool{}
	for _, p := range untypedSchemaProperties("t", props) {
		found[p] = true
	}
	want := []string{
		"t.top", "t.weird (not an object)", "t.list[].value",
		"t.untypeditems[]", "t.baditems[] (not an object)",
	}
	for _, w := range want {
		if !found[w] {
			t.Errorf("walker missed %q; got %v", w, found)
		}
	}
	if len(found) != len(want) {
		t.Errorf("walker flagged %d paths, want %d: %v", len(found), len(want), found)
	}
}
