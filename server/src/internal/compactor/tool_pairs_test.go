/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package compactor

import (
	"reflect"
	"strings"
	"testing"
)

// toolUseMessage is an assistant turn calling tool name with input.
func toolUseMessage(name string, input any) Message {
	return Message{Role: "assistant", Content: []any{
		map[string]any{"type": "text", "text": "Calling " + name},
		map[string]any{"type": "tool_use", "name": name, "input": input},
	}}
}

// toolResultMessage is a user turn carrying a tool's result.
func toolResultMessage(content any) Message {
	return Message{Role: "user", Content: []any{
		map[string]any{"type": "tool_result", "content": content},
	}}
}

// TestClassifier_ToolMessagesByTool checks how tool turns are ranked when
// tool results are not preserved wholesale: by tool name first, then by
// schema or error text in the turn, and otherwise as context.
func TestClassifier_ToolMessagesByTool(t *testing.T) {
	classifier := NewClassifier(false)
	long := strings.Repeat("row ", 200)

	tests := []struct {
		name       string
		msg        Message
		class      MessageClass
		importance float64
		reason     string
	}{
		{"schema tool", toolUseMessage("pg_dump_schema", nil),
			ClassAnchor, 1.0, "schema tool"},
		{"query analysis tool", toolUseMessage("analyze_query", map[string]any{"sql": "SELECT 1"}),
			ClassImportant, 0.85, "query analysis tool"},
		{"short query results", toolUseMessage("query_database", map[string]any{"sql": "SELECT 1"}),
			ClassContextual, 0.6, "query results"},
		{"substantial query results", toolUseMessage("query_database", map[string]any{"sql": long}),
			ClassImportant, 0.75, "substantial query results"},
		{"search tool", toolUseMessage("search_documentation", map[string]any{"q": "vacuum"}),
			ClassContextual, 0.65, "search tool"},
		{"schema in result", toolResultMessage("CREATE TABLE widgets (id int)"),
			ClassAnchor, 1.0, "schema in tool result"},
		{"error in result", toolResultMessage("permission denied for table widgets"),
			ClassImportant, 0.8, "error in tool result"},
		{"plain result", toolResultMessage("42 rows"),
			ClassContextual, 0.6, "tool message"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifier.Classify(tt.msg)
			if got.Class != tt.class || got.Importance != tt.importance {
				t.Errorf("class %s importance %v, want %s %v",
					got.Class, got.Importance, tt.class, tt.importance)
			}
			if !reflect.DeepEqual(got.Reasons, []string{tt.reason}) {
				t.Errorf("reasons %v, want [%s]", got.Reasons, tt.reason)
			}
		})
	}

	got := classifier.Classify(toolUseMessage("analyze_query", nil))
	if names, _ := got.Metadata["tools"].([]string); !reflect.DeepEqual(names, []string{"analyze_query"}) {
		t.Errorf("metadata tools = %v, want [analyze_query]", got.Metadata["tools"])
	}
}

// TestClassifier_ExtractTextFromBlock covers each block type the
// classifier reads text from, including content it cannot marshal.
func TestClassifier_ExtractTextFromBlock(t *testing.T) {
	c := NewClassifier(false)
	unmarshalable := make(chan int)

	tests := []struct {
		name  string
		block map[string]any
		want  string
	}{
		{"no type", map[string]any{"text": "ignored"}, ""},
		{"unknown type", map[string]any{"type": "image"}, ""},
		{"text", map[string]any{"type": "text", "text": "hello"}, "hello"},
		{"text without string", map[string]any{"type": "text", "text": 7}, ""},
		{"tool use", map[string]any{"type": "tool_use", "name": "query_database",
			"input": map[string]any{"sql": "SELECT 1"}}, `query_database {"sql":"SELECT 1"}`},
		{"tool use with unmarshalable input", map[string]any{"type": "tool_use",
			"name": "query_database", "input": unmarshalable}, "query_database"},
		{"tool result string", map[string]any{"type": "tool_result", "content": "done"}, "done"},
		{"tool result items", map[string]any{"type": "tool_result", "content": []any{
			map[string]any{"type": "text", "text": "first"},
			"not a block",
			map[string]any{"type": "text", "text": "second"},
		}}, "first second"},
		{"tool result object", map[string]any{"type": "tool_result",
			"content": map[string]any{"rows": 2}}, `{"rows":2}`},
		{"tool result unmarshalable", map[string]any{"type": "tool_result",
			"content": unmarshalable}, "(error marshaling content: "},
		{"tool result without content", map[string]any{"type": "tool_result"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.extractTextFromBlock(tt.block)
			if strings.HasSuffix(tt.want, ": ") {
				if !strings.HasPrefix(got, tt.want) {
					t.Errorf("got %q, want prefix %q", got, tt.want)
				}
				return
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClassifier_GetContentTextOtherTypes checks that content which is
// neither a string nor a block list is read as JSON.
func TestClassifier_GetContentTextOtherTypes(t *testing.T) {
	c := NewClassifier(false)
	if got := c.getContentText(Message{Content: map[string]any{"a": 1}}); got != `{"a":1}` {
		t.Errorf("map content read as %q", got)
	}
	if got := c.getContentText(Message{Content: make(chan int)}); !strings.HasPrefix(got, "(error marshaling content: ") {
		t.Errorf("unmarshalable content read as %q", got)
	}
}

func TestCompactor_MessagesEqual(t *testing.T) {
	c := NewCompactor(CompactRequest{})
	a := createMessage("user", "hello")
	if !c.messagesEqual(a, createMessage("user", "hello")) {
		t.Error("identical messages compared unequal")
	}
	if c.messagesEqual(a, createMessage("assistant", "hello")) {
		t.Error("messages with different roles compared equal")
	}
	if c.messagesEqual(a, createMessage("user", "goodbye")) {
		t.Error("messages with different content compared equal")
	}
}

func TestCompactor_HasToolUseAndResults(t *testing.T) {
	c := NewCompactor(CompactRequest{})
	use := toolUseMessage("query_database", nil)
	result := toolResultMessage("1 row")
	plain := createMessage("user", "tool_use")
	malformed := Message{Role: "user", Content: []any{"tool_result", map[string]any{"type": 3}}}

	tests := []struct {
		name            string
		msg             Message
		wantUse, wantRe bool
	}{
		{"tool use", use, true, false},
		{"tool result", result, false, true},
		{"string content", plain, false, false},
		{"malformed blocks", malformed, false, false},
	}
	for _, tt := range tests {
		if got := c.hasToolUse(tt.msg); got != tt.wantUse {
			t.Errorf("%s: hasToolUse = %v, want %v", tt.name, got, tt.wantUse)
		}
		if got := c.hasToolResults(tt.msg); got != tt.wantRe {
			t.Errorf("%s: hasToolResults = %v, want %v", tt.name, got, tt.wantRe)
		}
	}
}

// TestCompactor_AdjustStartForToolPairs checks that the recent window
// never starts on a tool result whose tool call it would leave behind.
func TestCompactor_AdjustStartForToolPairs(t *testing.T) {
	c := NewCompactor(CompactRequest{})
	messages := []Message{
		createMessage("user", "first question"),
		createMessage("assistant", "first answer"),
		toolUseMessage("query_database", nil),
		toolResultMessage("1 row"),
		createMessage("user", "follow up"),
	}
	tests := []struct {
		name  string
		start int
		want  int
	}{
		{"start at the first message pair", 1, 1},
		{"start past the end", len(messages), len(messages)},
		{"start on an assistant turn", 2, 2},
		{"start on a tool result", 3, 2},
		{"start on a plain user turn", 4, 4},
	}
	for _, tt := range tests {
		if got := c.adjustStartForToolPairs(messages, tt.start); got != tt.want {
			t.Errorf("%s: start %d adjusted to %d, want %d", tt.name, tt.start, got, tt.want)
		}
	}
}

// TestCompactor_EnsureToolPairsInSlice checks that keeping either half of
// a tool call and its result keeps the other half, in original order.
func TestCompactor_EnsureToolPairsInSlice(t *testing.T) {
	c := NewCompactor(CompactRequest{})
	intro := createMessage("user", "show me the widgets")
	use := toolUseMessage("query_database", map[string]any{"sql": "SELECT * FROM widgets"})
	result := toolResultMessage("3 rows")
	outro := createMessage("assistant", "There are three widgets.")
	middle := []Message{intro, use, result, outro}

	tests := []struct {
		name string
		kept []Message
		want []Message
	}{
		{"nothing kept", nil, nil},
		{"result kept", []Message{result}, []Message{use, result}},
		{"call kept", []Message{outro, use}, []Message{use, result, outro}},
		{"pair already kept", []Message{use, result}, []Message{use, result}},
		{"no tool turns kept", []Message{intro}, []Message{intro}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.ensureToolPairsInSlice(middle, tt.kept, 0)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("kept %v, want %v", got, tt.want)
			}
		})
	}

	if got := c.ensureToolPairsInSlice(nil, []Message{result}, 0); !reflect.DeepEqual(got, []Message{result}) {
		t.Errorf("empty middle slice changed the kept set to %v", got)
	}
}

// TestCompactor_ClassifyAndKeepImportant checks that anchors and
// important turns are kept, and that the kept set is topped up to
// MinImportantMessages in conversation order, skipping transient turns.
func TestCompactor_ClassifyAndKeepImportant(t *testing.T) {
	anchor := createMessage("user", "Actually, the table is called gadgets")
	ack := createMessage("user", "ok")
	contextual := createMessage("user", "list the indexes")
	routine := createMessage("assistant", "Done.")
	messages := []Message{ack, contextual, anchor, routine}

	c := NewCompactor(CompactRequest{KeepAnchors: true,
		Options: &CompactionOptions{MinImportantMessages: 3}})
	got := c.classifyAndKeepImportant(messages)
	want := []Message{anchor, contextual, routine}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kept %v, want %v", got, want)
	}

	c = NewCompactor(CompactRequest{Options: &CompactionOptions{MinImportantMessages: 1}})
	if got := c.classifyAndKeepImportant(messages); !reflect.DeepEqual(got, []Message{anchor}) {
		t.Errorf("with a minimum already met, kept %v, want only the anchor", got)
	}
}
