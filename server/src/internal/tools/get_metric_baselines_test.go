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
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// get_metric_baselines tool definition tests
// ---------------------------------------------------------------------------

func TestGetMetricBaselinesToolDefinition(t *testing.T) {
	tool := GetMetricBaselinesTool(nil, nil, nil, nil)

	if tool.Definition.Name != "get_metric_baselines" {
		t.Errorf("expected tool name 'get_metric_baselines', got %q", tool.Definition.Name)
	}

	if tool.Definition.Description == "" {
		t.Error("expected non-empty description")
	}

	if tool.Definition.CompactDescription == "" {
		t.Error("expected non-empty compact description")
	}

	// Verify no required parameters
	if len(tool.Definition.InputSchema.Required) != 0 {
		t.Errorf("expected 0 required parameters, got %d",
			len(tool.Definition.InputSchema.Required))
	}

	// Verify all expected properties exist
	for _, prop := range []string{"connection_id", "metric_name"} {
		if _, ok := tool.Definition.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected property %q in input schema", prop)
		}
	}
}

// ---------------------------------------------------------------------------
// get_metric_baselines nil pool test
// ---------------------------------------------------------------------------

func TestGetMetricBaselinesNilPool(t *testing.T) {
	tool := GetMetricBaselinesTool(nil, nil, nil, nil)

	response, err := tool.Handler(map[string]any{})
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !response.IsError {
		t.Error("expected error response when pool is nil")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "Datastore not configured") {
		t.Errorf("expected 'Datastore not configured' error, got: %s",
			response.Content[0].Text)
	}
}

// ---------------------------------------------------------------------------
// get_metric_baselines parameter validation tests
// ---------------------------------------------------------------------------

func TestGetMetricBaselinesInvalidConnectionID(t *testing.T) {
	tool := GetMetricBaselinesTool(nil, nil, nil, nil)

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "connection_id is string",
			args: map[string]any{
				"connection_id": "abc",
			},
		},
		{
			name: "connection_id is boolean",
			args: map[string]any{
				"connection_id": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := tool.Handler(tt.args)
			if err != nil {
				t.Fatalf("handler returned unexpected error: %v", err)
			}
			// Should fail on nil pool first
			if !response.IsError {
				t.Error("expected error response")
			}
		})
	}
}

func TestGetMetricBaselinesWithMetricName(t *testing.T) {
	tool := GetMetricBaselinesTool(nil, nil, nil, nil)

	args := map[string]any{
		"metric_name": "cpu_usage",
	}

	response, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	// Should fail on nil pool
	if !response.IsError {
		t.Error("expected error response")
	}
	if len(response.Content) == 0 {
		t.Fatal("expected error message in response content")
	}
	if !strings.Contains(response.Content[0].Text, "Datastore not configured") {
		t.Errorf("expected 'Datastore not configured' error, got: %s",
			response.Content[0].Text)
	}
}

// ---------------------------------------------------------------------------
// escapeLikePattern helper tests
// ---------------------------------------------------------------------------

func TestEscapeLikePattern(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no special characters",
			input: "cache_hit",
			// underscore is escaped so it matches literally
			want: `cache\_hit`,
		},
		{
			name:  "percent sign",
			input: "100%cpu",
			want:  `100\%cpu`,
		},
		{
			name:  "underscore",
			input: "cache_hit_ratio",
			want:  `cache\_hit\_ratio`,
		},
		{
			name:  "backslash escaped first",
			input: `a\b`,
			want:  `a\\b`,
		},
		{
			name:  "all specials combined",
			input: `a_b%c\d`,
			want:  `a\_b\%c\\d`,
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeLikePattern(tt.input)
			if got != tt.want {
				t.Errorf("escapeLikePattern(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// scanMetricNames helper tests
// ---------------------------------------------------------------------------

// fakeRows implements the minimal Next/Scan interface used by
// scanMetricNames, allowing the Scan-error branch to be exercised without a
// real database.
type fakeRows struct {
	values  []string
	idx     int
	failAt  int // index at which Scan returns an error; -1 to never fail
	scanErr error
}

func (f *fakeRows) Next() bool {
	return f.idx < len(f.values)
}

func (f *fakeRows) Scan(dest ...any) error {
	if f.idx == f.failAt {
		return f.scanErr
	}
	if len(dest) > 0 {
		if p, ok := dest[0].(*string); ok {
			*p = f.values[f.idx]
		}
	}
	f.idx++
	return nil
}

func TestScanMetricNames(t *testing.T) {
	t.Run("collects all rows", func(t *testing.T) {
		rows := &fakeRows{values: []string{"a", "b", "c"}, failAt: -1}
		got := scanMetricNames(rows)
		if strings.Join(got, ",") != "a,b,c" {
			t.Errorf("expected a,b,c got %v", got)
		}
	})

	t.Run("stops on scan error", func(t *testing.T) {
		rows := &fakeRows{
			values:  []string{"a", "b", "c"},
			failAt:  1,
			scanErr: fmt.Errorf("boom"),
		}
		got := scanMetricNames(rows)
		// Should have collected only the first value before the error.
		if strings.Join(got, ",") != "a" {
			t.Errorf("expected only [a] before error, got %v", got)
		}
	})

	t.Run("empty result", func(t *testing.T) {
		rows := &fakeRows{values: nil, failAt: -1}
		got := scanMetricNames(rows)
		if got != nil {
			t.Errorf("expected nil for empty result, got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// formatOptionalInt helper tests
// ---------------------------------------------------------------------------

func TestFormatOptionalInt(t *testing.T) {
	tests := []struct {
		name  string
		input *int
		want  string
	}{
		{
			name:  "nil",
			input: nil,
			want:  "",
		},
		{
			name:  "zero",
			input: ptrInt(0),
			want:  "0",
		},
		{
			name:  "positive",
			input: ptrInt(42),
			want:  "42",
		},
		{
			name:  "negative",
			input: ptrInt(-10),
			want:  "-10",
		},
		{
			name:  "day of week",
			input: ptrInt(3),
			want:  "3",
		},
		{
			name:  "hour of day",
			input: ptrInt(14),
			want:  "14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatOptionalInt(tt.input)
			if got != tt.want {
				t.Errorf("formatOptionalInt() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// get_metric_baselines paging tests
// ---------------------------------------------------------------------------

func TestGetMetricBaselinesPagingSchema(t *testing.T) {
	tool := GetMetricBaselinesTool(nil, nil, nil, nil)
	props := tool.Definition.InputSchema.Properties

	limit, ok := props["limit"].(map[string]any)
	if !ok {
		t.Fatal("expected limit property in input schema")
	}
	if limit["minimum"] != 1 || limit["maximum"] != baselinesMaxLimit {
		t.Errorf("limit bounds = %v..%v; want 1..%d", limit["minimum"], limit["maximum"], baselinesMaxLimit)
	}
	if _, ok := props["offset"].(map[string]any); !ok {
		t.Fatal("expected offset property in input schema")
	}

	// The prose states the limits as literals; keep it in step with the
	// constants.
	want := fmt.Sprintf("default: %d, max: %d", baselinesDefaultLimit, baselinesMaxLimit)
	for name, text := range map[string]string{
		"description":       tool.Definition.Description,
		"limit description": limit["description"].(string),
	} {
		if !strings.Contains(strings.ToLower(text), want) {
			t.Errorf("%s does not state %q", name, want)
		}
	}
	compact := fmt.Sprintf("Returns %d rows by default (limit, max %d)", baselinesDefaultLimit, baselinesMaxLimit)
	if !strings.Contains(tool.Definition.CompactDescription, compact) {
		t.Errorf("compact description does not state %q", compact)
	}
}

func TestParseBaselinesPage(t *testing.T) {
	limitErr := fmt.Sprintf("Invalid 'limit' parameter: must be an integer between 1 and %d", baselinesMaxLimit)
	offsetErr := fmt.Sprintf("Invalid 'offset' parameter: must be an integer of 0 or more (at most %d)", math.MaxInt32)

	cases := []struct {
		name    string
		args    map[string]any
		want    baselinesPage
		wantErr string
	}{
		{"defaults", map[string]any{}, baselinesPage{limit: baselinesDefaultLimit}, ""},
		{"nil values are absent", map[string]any{"limit": nil, "offset": nil}, baselinesPage{limit: baselinesDefaultLimit}, ""},
		{"float values", map[string]any{"limit": float64(25), "offset": float64(50)}, baselinesPage{limit: 25, offset: 50}, ""},
		{"int values", map[string]any{"limit": 1, "offset": 0}, baselinesPage{limit: 1}, ""},
		{"json number", map[string]any{"limit": json.Number("300")}, baselinesPage{limit: baselinesMaxLimit}, ""},
		{"largest offset", map[string]any{"offset": float64(math.MaxInt32)}, baselinesPage{limit: baselinesDefaultLimit, offset: math.MaxInt32}, ""},
		{"zero limit", map[string]any{"limit": float64(0)}, baselinesPage{}, limitErr},
		{"negative limit", map[string]any{"limit": -1}, baselinesPage{}, limitErr},
		{"limit above max", map[string]any{"limit": float64(baselinesMaxLimit + 1)}, baselinesPage{}, limitErr},
		{"fractional limit", map[string]any{"limit": 2.5}, baselinesPage{}, limitErr},
		{"string limit", map[string]any{"limit": "10"}, baselinesPage{}, limitErr},
		{"bool limit", map[string]any{"limit": true}, baselinesPage{}, limitErr},
		{"negative offset", map[string]any{"offset": float64(-1)}, baselinesPage{}, offsetErr},
		{"offset too large", map[string]any{"offset": float64(math.MaxInt32) + 1}, baselinesPage{}, offsetErr},
		{"fractional offset", map[string]any{"offset": 0.5}, baselinesPage{}, offsetErr},
		{"string offset", map[string]any{"offset": "5"}, baselinesPage{}, offsetErr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, errMsg := parseBaselinesPage(tc.args)
			if errMsg != tc.wantErr {
				t.Fatalf("error = %q; want %q", errMsg, tc.wantErr)
			}
			if tc.wantErr == "" && got != tc.want {
				t.Errorf("page = %+v; want %+v", got, tc.want)
			}
		})
	}
}

func TestBaselinesCountLine(t *testing.T) {
	cases := []struct {
		name  string
		total int
		shown int
		page  baselinesPage
		want  string
	}{
		{"all on first page", 32, 32, baselinesPage{limit: 100}, "Showing all 32 baselines"},
		{"first of several", 640, 100, baselinesPage{limit: 100}, "Showing rows 1-100 of 640 baselines (more available - use offset=100 for next page)"},
		{"middle page", 640, 100, baselinesPage{limit: 100, offset: 100}, "Showing rows 101-200 of 640 baselines (more available - use offset=200 for next page)"},
		{"last page", 640, 40, baselinesPage{limit: 100, offset: 600}, "Showing rows 601-640 of 640 baselines (last page)"},
		{"exact last page", 200, 100, baselinesPage{limit: 100, offset: 100}, "Showing rows 101-200 of 200 baselines (last page)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := baselinesCountLine(tc.total, tc.shown, tc.page); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestBaselineMetricPattern(t *testing.T) {
	if got := baselineMetricPattern(nil); got != nil {
		t.Errorf("nil metric name gave pattern %q; want nil", *got)
	}
	name := "cache_hit"
	got := baselineMetricPattern(&name)
	if got == nil || *got != `%cache\_hit%` {
		t.Errorf("pattern = %v; want %q", got, `%cache\_hit%`)
	}
}

// ---------------------------------------------------------------------------
// helper functions for tests
// ---------------------------------------------------------------------------

func ptrInt(i int) *int {
	return &i
}
