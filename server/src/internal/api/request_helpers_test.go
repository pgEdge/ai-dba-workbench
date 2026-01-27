/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDecodeJSONBody(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		expectSuccess  bool
		expectedStatus int
	}{
		{
			name:          "valid JSON",
			body:          `{"name": "test", "value": 123}`,
			expectSuccess: true,
		},
		{
			name:           "invalid JSON",
			body:           `{invalid}`,
			expectSuccess:  false,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "empty body",
			body:           "",
			expectSuccess:  false,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()

			var dest map[string]interface{}
			result := DecodeJSONBody(rec, req, &dest)

			if result != tt.expectSuccess {
				t.Errorf("DecodeJSONBody returned %v, expected %v", result, tt.expectSuccess)
			}

			if !tt.expectSuccess && rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestParseQueryInt(t *testing.T) {
	tests := []struct {
		name           string
		queryParam     string
		queryValue     string
		expectValue    int
		expectOK       bool
		expectedStatus int
	}{
		{
			name:        "valid integer",
			queryParam:  "id",
			queryValue:  "123",
			expectValue: 123,
			expectOK:    true,
		},
		{
			name:        "negative integer",
			queryParam:  "offset",
			queryValue:  "-5",
			expectValue: -5,
			expectOK:    true,
		},
		{
			name:       "empty value",
			queryParam: "id",
			queryValue: "",
			expectOK:   false,
		},
		{
			name:           "invalid integer",
			queryParam:     "id",
			queryValue:     "abc",
			expectOK:       false,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?" + tt.queryParam + "=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()

			value, ok := ParseQueryInt(rec, req, tt.queryParam)

			if ok != tt.expectOK {
				t.Errorf("ParseQueryInt returned ok=%v, expected %v", ok, tt.expectOK)
			}

			if tt.expectOK && value != tt.expectValue {
				t.Errorf("ParseQueryInt returned value=%d, expected %d", value, tt.expectValue)
			}

			if !tt.expectOK && tt.expectedStatus != 0 && rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestParseQueryIntList(t *testing.T) {
	tests := []struct {
		name           string
		queryValue     string
		expectValues   []int
		expectOK       bool
		expectedStatus int
	}{
		{
			name:         "single value",
			queryValue:   "1",
			expectValues: []int{1},
			expectOK:     true,
		},
		{
			name:         "multiple values",
			queryValue:   "1,2,3",
			expectValues: []int{1, 2, 3},
			expectOK:     true,
		},
		{
			name:         "values with spaces URL encoded",
			queryValue:   "1,%202,%203",
			expectValues: []int{1, 2, 3},
			expectOK:     true,
		},
		{
			name:     "empty value",
			expectOK: false,
		},
		{
			name:           "invalid value in list",
			queryValue:     "1,abc,3",
			expectOK:       false,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?ids=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()

			values, ok := ParseQueryIntList(rec, req, "ids")

			if ok != tt.expectOK {
				t.Errorf("ParseQueryIntList returned ok=%v, expected %v", ok, tt.expectOK)
			}

			if tt.expectOK {
				if len(values) != len(tt.expectValues) {
					t.Errorf("ParseQueryIntList returned %d values, expected %d", len(values), len(tt.expectValues))
				} else {
					for i, v := range values {
						if v != tt.expectValues[i] {
							t.Errorf("ParseQueryIntList value[%d]=%d, expected %d", i, v, tt.expectValues[i])
						}
					}
				}
			}

			if !tt.expectOK && tt.expectedStatus != 0 && rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestParseQueryTime(t *testing.T) {
	validTime := "2024-01-15T10:30:00Z"
	expectedTime, _ := time.Parse(time.RFC3339, validTime)

	tests := []struct {
		name           string
		queryValue     string
		expectTime     time.Time
		expectOK       bool
		expectedStatus int
	}{
		{
			name:       "valid RFC3339 time",
			queryValue: validTime,
			expectTime: expectedTime,
			expectOK:   true,
		},
		{
			name:     "empty value",
			expectOK: false,
		},
		{
			name:           "invalid format",
			queryValue:     "2024-01-15",
			expectOK:       false,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?time=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()

			value, ok := ParseQueryTime(rec, req, "time")

			if ok != tt.expectOK {
				t.Errorf("ParseQueryTime returned ok=%v, expected %v", ok, tt.expectOK)
			}

			if tt.expectOK && !value.Equal(tt.expectTime) {
				t.Errorf("ParseQueryTime returned %v, expected %v", value, tt.expectTime)
			}

			if !tt.expectOK && tt.expectedStatus != 0 && rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestParseQueryBool(t *testing.T) {
	tests := []struct {
		name       string
		queryValue string
		expect     bool
	}{
		{name: "true", queryValue: "true", expect: true},
		{name: "1", queryValue: "1", expect: true},
		{name: "false", queryValue: "false", expect: false},
		{name: "0", queryValue: "0", expect: false},
		{name: "empty", queryValue: "", expect: false},
		{name: "invalid", queryValue: "yes", expect: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?flag=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)

			result := ParseQueryBool(req, "flag")

			if result != tt.expect {
				t.Errorf("ParseQueryBool returned %v, expected %v", result, tt.expect)
			}
		})
	}
}

func TestParseQueryStringList(t *testing.T) {
	tests := []struct {
		name         string
		queryValue   string
		expectValues []string
		expectOK     bool
	}{
		{
			name:         "single value",
			queryValue:   "type1",
			expectValues: []string{"type1"},
			expectOK:     true,
		},
		{
			name:         "multiple values",
			queryValue:   "type1,type2,type3",
			expectValues: []string{"type1", "type2", "type3"},
			expectOK:     true,
		},
		{
			name:         "values with spaces URL encoded",
			queryValue:   "type1,%20type2,%20type3",
			expectValues: []string{"type1", "type2", "type3"},
			expectOK:     true,
		},
		{
			name:     "empty value",
			expectOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?types=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)

			values, ok := ParseQueryStringList(req, "types")

			if ok != tt.expectOK {
				t.Errorf("ParseQueryStringList returned ok=%v, expected %v", ok, tt.expectOK)
			}

			if tt.expectOK {
				if len(values) != len(tt.expectValues) {
					t.Errorf("ParseQueryStringList returned %d values, expected %d", len(values), len(tt.expectValues))
				} else {
					for i, v := range values {
						if v != tt.expectValues[i] {
							t.Errorf("ParseQueryStringList value[%d]=%q, expected %q", i, v, tt.expectValues[i])
						}
					}
				}
			}
		})
	}
}

func TestRequireQueryTime(t *testing.T) {
	tests := []struct {
		name           string
		queryValue     string
		expectOK       bool
		expectedStatus int
	}{
		{
			name:       "valid time",
			queryValue: "2024-01-15T10:30:00Z",
			expectOK:   true,
		},
		{
			name:           "missing required",
			expectOK:       false,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid format",
			queryValue:     "invalid",
			expectOK:       false,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?time=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()

			_, ok := RequireQueryTime(rec, req, "time")

			if ok != tt.expectOK {
				t.Errorf("RequireQueryTime returned ok=%v, expected %v", ok, tt.expectOK)
			}

			if !tt.expectOK && rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestValidateTimeRange(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		expectOK bool
	}{
		{
			name:     "valid range",
			start:    now,
			end:      now.Add(time.Hour),
			expectOK: true,
		},
		{
			name:     "same time",
			start:    now,
			end:      now,
			expectOK: true,
		},
		{
			name:     "invalid range",
			start:    now.Add(time.Hour),
			end:      now,
			expectOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			result := ValidateTimeRange(rec, tt.start, tt.end)

			if result != tt.expectOK {
				t.Errorf("ValidateTimeRange returned %v, expected %v", result, tt.expectOK)
			}

			if !tt.expectOK && rec.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
			}
		})
	}
}

func TestValidateStringInSet(t *testing.T) {
	allowed := map[string]bool{"type1": true, "type2": true}

	tests := []struct {
		name     string
		value    string
		expectOK bool
	}{
		{name: "valid value", value: "type1", expectOK: true},
		{name: "another valid", value: "type2", expectOK: true},
		{name: "empty allowed", value: "", expectOK: true},
		{name: "invalid value", value: "type3", expectOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			result := ValidateStringInSet(rec, tt.value, "type", allowed)

			if result != tt.expectOK {
				t.Errorf("ValidateStringInSet returned %v, expected %v", result, tt.expectOK)
			}
		})
	}
}

func TestValidateStringsInSet(t *testing.T) {
	allowed := map[string]bool{"type1": true, "type2": true, "type3": true}

	tests := []struct {
		name     string
		values   []string
		expectOK bool
	}{
		{name: "all valid", values: []string{"type1", "type2"}, expectOK: true},
		{name: "empty list", values: []string{}, expectOK: true},
		{name: "one invalid", values: []string{"type1", "invalid"}, expectOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			result := ValidateStringsInSet(rec, tt.values, "types", allowed)

			if result != tt.expectOK {
				t.Errorf("ValidateStringsInSet returned %v, expected %v", result, tt.expectOK)
			}
		})
	}
}

func TestParseLimitWithDefaults(t *testing.T) {
	tests := []struct {
		name         string
		queryValue   string
		defaultLimit int
		maxLimit     int
		expect       int
	}{
		{name: "use default", queryValue: "", defaultLimit: 100, maxLimit: 1000, expect: 100},
		{name: "valid limit", queryValue: "50", defaultLimit: 100, maxLimit: 1000, expect: 50},
		{name: "exceeds max", queryValue: "2000", defaultLimit: 100, maxLimit: 1000, expect: 1000},
		{name: "invalid value", queryValue: "abc", defaultLimit: 100, maxLimit: 1000, expect: 100},
		{name: "zero value", queryValue: "0", defaultLimit: 100, maxLimit: 1000, expect: 100},
		{name: "negative", queryValue: "-5", defaultLimit: 100, maxLimit: 1000, expect: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?limit=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)

			result := ParseLimitWithDefaults(req, tt.defaultLimit, tt.maxLimit)

			if result != tt.expect {
				t.Errorf("ParseLimitWithDefaults returned %d, expected %d", result, tt.expect)
			}
		})
	}
}

func TestParseOffsetWithDefault(t *testing.T) {
	tests := []struct {
		name          string
		queryValue    string
		defaultOffset int
		expect        int
	}{
		{name: "use default", queryValue: "", defaultOffset: 0, expect: 0},
		{name: "valid offset", queryValue: "50", defaultOffset: 0, expect: 50},
		{name: "invalid value", queryValue: "abc", defaultOffset: 0, expect: 0},
		{name: "negative", queryValue: "-5", defaultOffset: 0, expect: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?offset=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)

			result := ParseOffsetWithDefault(req, tt.defaultOffset)

			if result != tt.expect {
				t.Errorf("ParseOffsetWithDefault returned %d, expected %d", result, tt.expect)
			}
		})
	}
}

func TestRequireMethod(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		allowed  []string
		expectOK bool
	}{
		{name: "GET allowed", method: http.MethodGet, allowed: []string{http.MethodGet}, expectOK: true},
		{name: "POST not allowed", method: http.MethodPost, allowed: []string{http.MethodGet}, expectOK: false},
		{name: "multiple allowed", method: http.MethodPost, allowed: []string{http.MethodGet, http.MethodPost}, expectOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/test", nil)
			rec := httptest.NewRecorder()

			result := RequireMethod(rec, req, tt.allowed...)

			if result != tt.expectOK {
				t.Errorf("RequireMethod returned %v, expected %v", result, tt.expectOK)
			}

			if !tt.expectOK && rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
			}
		})
	}
}

func TestRequireGET(t *testing.T) {
	t.Run("GET allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		if !RequireGET(rec, req) {
			t.Error("RequireGET should return true for GET request")
		}
	})

	t.Run("POST not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		rec := httptest.NewRecorder()

		if RequireGET(rec, req) {
			t.Error("RequireGET should return false for POST request")
		}
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}
	})
}

func TestRequirePOST(t *testing.T) {
	t.Run("POST allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", nil)
		rec := httptest.NewRecorder()

		if !RequirePOST(rec, req) {
			t.Error("RequirePOST should return true for POST request")
		}
	})

	t.Run("GET not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		if RequirePOST(rec, req) {
			t.Error("RequirePOST should return false for GET request")
		}
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}
	})
}

func TestGetTokenHashFromRequest(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		expectHash bool
	}{
		{name: "valid bearer token", authHeader: "Bearer abc123", expectHash: true},
		{name: "missing header", authHeader: "", expectHash: false},
		{name: "invalid format", authHeader: "Basic abc123", expectHash: false},
		{name: "just Bearer", authHeader: "Bearer", expectHash: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			result := GetTokenHashFromRequest(req)

			if tt.expectHash && result == "" {
				t.Error("Expected non-empty token hash")
			}
			if !tt.expectHash && result != "" {
				t.Errorf("Expected empty token hash, got %q", result)
			}
		})
	}
}

func TestParseQueryIntSilent(t *testing.T) {
	tests := []struct {
		name        string
		queryValue  string
		expectValue int
		expectOK    bool
	}{
		{name: "valid", queryValue: "42", expectValue: 42, expectOK: true},
		{name: "empty", queryValue: "", expectValue: 0, expectOK: false},
		{name: "invalid", queryValue: "abc", expectValue: 0, expectOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?id=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)

			value, ok := ParseQueryIntSilent(req, "id")

			if ok != tt.expectOK || value != tt.expectValue {
				t.Errorf("ParseQueryIntSilent returned (%d, %v), expected (%d, %v)",
					value, ok, tt.expectValue, tt.expectOK)
			}
		})
	}
}

func TestParseQueryInt64(t *testing.T) {
	tests := []struct {
		name           string
		queryValue     string
		expectValue    int64
		expectOK       bool
		expectedStatus int
	}{
		{name: "valid", queryValue: "9223372036854775807", expectValue: 9223372036854775807, expectOK: true},
		{name: "empty", queryValue: "", expectValue: 0, expectOK: false},
		{name: "invalid", queryValue: "abc", expectValue: 0, expectOK: false, expectedStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?id=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			rec := httptest.NewRecorder()

			value, ok := ParseQueryInt64(rec, req, "id")

			if ok != tt.expectOK || value != tt.expectValue {
				t.Errorf("ParseQueryInt64 returned (%d, %v), expected (%d, %v)",
					value, ok, tt.expectValue, tt.expectOK)
			}

			if !tt.expectOK && tt.expectedStatus != 0 && rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestParseQueryIntListSilent(t *testing.T) {
	tests := []struct {
		name         string
		queryValue   string
		expectValues []int
	}{
		{name: "valid list", queryValue: "1,2,3", expectValues: []int{1, 2, 3}},
		{name: "empty", queryValue: "", expectValues: nil},
		{name: "skip invalid", queryValue: "1,abc,3", expectValues: []int{1, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?ids=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)

			values := ParseQueryIntListSilent(req, "ids")

			if len(values) != len(tt.expectValues) {
				t.Errorf("ParseQueryIntListSilent returned %d values, expected %d",
					len(values), len(tt.expectValues))
			} else {
				for i, v := range values {
					if v != tt.expectValues[i] {
						t.Errorf("value[%d]=%d, expected %d", i, v, tt.expectValues[i])
					}
				}
			}
		})
	}
}

func TestParseQueryTimeSilent(t *testing.T) {
	validTime := "2024-01-15T10:30:00Z"
	expectedTime, _ := time.Parse(time.RFC3339, validTime)

	tests := []struct {
		name       string
		queryValue string
		expectTime time.Time
		expectOK   bool
	}{
		{name: "valid", queryValue: validTime, expectTime: expectedTime, expectOK: true},
		{name: "empty", queryValue: "", expectOK: false},
		{name: "invalid", queryValue: "not-a-time", expectOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryValue != "" {
				url = "/test?time=" + tt.queryValue
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)

			value, ok := ParseQueryTimeSilent(req, "time")

			if ok != tt.expectOK {
				t.Errorf("ParseQueryTimeSilent returned ok=%v, expected %v", ok, tt.expectOK)
			}

			if tt.expectOK && !value.Equal(tt.expectTime) {
				t.Errorf("ParseQueryTimeSilent returned %v, expected %v", value, tt.expectTime)
			}
		})
	}
}

func TestParseQueryString(t *testing.T) {
	t.Run("with value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test?name=test", nil)
		if result := ParseQueryString(req, "name"); result != "test" {
			t.Errorf("Expected 'test', got %q", result)
		}
	})

	t.Run("without value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		if result := ParseQueryString(req, "name"); result != "" {
			t.Errorf("Expected empty string, got %q", result)
		}
	})
}

func TestUserInfo(t *testing.T) {
	// Test UserInfo struct
	info := &UserInfo{
		Username:    "testuser",
		IsSuperuser: true,
	}

	if info.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got %q", info.Username)
	}

	if !info.IsSuperuser {
		t.Error("Expected IsSuperuser to be true")
	}

	// Test with JSON serialization
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Failed to marshal UserInfo: %v", err)
	}

	var decoded UserInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal UserInfo: %v", err)
	}

	if decoded.Username != info.Username || decoded.IsSuperuser != info.IsSuperuser {
		t.Error("UserInfo mismatch after JSON round-trip")
	}
}
