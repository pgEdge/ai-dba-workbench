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
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// UserInfo contains authenticated user information extracted from a request.
type UserInfo struct {
	Username    string
	IsSuperuser bool
}

// GetUserInfoFromRequest extracts username and superuser status from the
// Authorization header. Returns an error if authentication fails.
func GetUserInfoFromRequest(r *http.Request, authStore *auth.AuthStore) (*UserInfo, error) {
	var token string

	// Try Authorization header first (for API tokens and backwards compatibility)
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		token = strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			return nil, fmt.Errorf("invalid authorization header format")
		}
	} else {
		// Fall back to httpOnly session cookie (for browser sessions)
		cookie, err := r.Cookie("session_token")
		if err != nil || cookie.Value == "" {
			return nil, fmt.Errorf("missing authentication credentials")
		}
		token = cookie.Value
	}

	// Validate session token and get username
	username, err := authStore.ValidateSessionToken(token)
	if err != nil {
		return nil, err
	}

	// Look up user to get superuser status
	user, err := authStore.GetUser(username)
	if err != nil {
		// User exists but couldn't get details - return with superuser false
		return &UserInfo{Username: username, IsSuperuser: false}, nil
	}

	return &UserInfo{Username: username, IsSuperuser: user.IsSuperuser}, nil
}

// GetTokenHashFromRequest extracts and hashes the token from the Authorization
// header. Returns an empty string if the token cannot be extracted.
func GetTokenHashFromRequest(r *http.Request) string {
	var token string

	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		token = strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			return ""
		}
	} else {
		cookie, err := r.Cookie("session_token")
		if err != nil || cookie.Value == "" {
			return ""
		}
		token = cookie.Value
	}

	return auth.GetTokenHashByRawToken(token)
}

// DecodeJSONBody decodes a JSON request body into the provided destination.
// If decoding fails, it sends an error response and returns false.
// If decoding succeeds, it returns true and the caller should continue.
func DecodeJSONBody(w http.ResponseWriter, r *http.Request, dest interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	return true
}

// ParseQueryInt parses an integer query parameter. Returns the value and true
// if parsing succeeds, or zero and false if the parameter is empty or invalid.
// If the parameter is present but invalid, it sends an error response.
func ParseQueryInt(w http.ResponseWriter, r *http.Request, paramName string) (int, bool) {
	valueStr := r.URL.Query().Get(paramName)
	if valueStr == "" {
		return 0, false
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest,
			fmt.Sprintf("Invalid %s: %v", paramName, err))
		return 0, false
	}
	return value, true
}

// ParseQueryIntSilent parses an integer query parameter without sending an
// error response on failure. Returns the value and true if parsing succeeds,
// or zero and false if the parameter is empty or invalid.
func ParseQueryIntSilent(r *http.Request, paramName string) (int, bool) {
	valueStr := r.URL.Query().Get(paramName)
	if valueStr == "" {
		return 0, false
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return 0, false
	}
	return value, true
}

// ParseQueryInt64 parses an int64 query parameter. Returns the value and true
// if parsing succeeds, or zero and false if the parameter is empty or invalid.
// If the parameter is present but invalid, it sends an error response.
func ParseQueryInt64(w http.ResponseWriter, r *http.Request, paramName string) (int64, bool) {
	valueStr := r.URL.Query().Get(paramName)
	if valueStr == "" {
		return 0, false
	}

	value, err := strconv.ParseInt(valueStr, 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest,
			fmt.Sprintf("Invalid %s: %v", paramName, err))
		return 0, false
	}
	return value, true
}

// ParseQueryIntList parses a comma-separated list of integers from a query
// parameter. Returns the list and true if parsing succeeds, or nil and false
// if the parameter is empty. Sends an error response if any value is invalid.
func ParseQueryIntList(w http.ResponseWriter, r *http.Request, paramName string) ([]int, bool) {
	valueStr := r.URL.Query().Get(paramName)
	if valueStr == "" {
		return nil, false
	}

	parts := strings.Split(valueStr, ",")
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			RespondError(w, http.StatusBadRequest,
				fmt.Sprintf("Invalid %s: %v", paramName, err))
			return nil, false
		}
		result = append(result, value)
	}
	return result, true
}

// ParseQueryIntListSilent parses a comma-separated list of integers from a
// query parameter without sending an error response. Invalid values are
// silently skipped.
func ParseQueryIntListSilent(r *http.Request, paramName string) []int {
	valueStr := r.URL.Query().Get(paramName)
	if valueStr == "" {
		return nil
	}

	parts := strings.Split(valueStr, ",")
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		if value, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			result = append(result, value)
		}
	}
	return result
}

// ParseQueryTime parses a time query parameter in RFC3339 format. Returns the
// time and true if parsing succeeds, or zero time and false if the parameter
// is empty or invalid. Sends an error response if the format is invalid.
func ParseQueryTime(w http.ResponseWriter, r *http.Request, paramName string) (time.Time, bool) {
	valueStr := r.URL.Query().Get(paramName)
	if valueStr == "" {
		return time.Time{}, false
	}

	value, err := time.Parse(time.RFC3339, valueStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest,
			fmt.Sprintf("Invalid %s format, expected RFC3339: %v", paramName, err))
		return time.Time{}, false
	}
	return value, true
}

// ParseQueryTimeSilent parses a time query parameter in RFC3339 format without
// sending an error response. Returns the time and true if parsing succeeds,
// or zero time and false if the parameter is empty or invalid.
func ParseQueryTimeSilent(r *http.Request, paramName string) (time.Time, bool) {
	valueStr := r.URL.Query().Get(paramName)
	if valueStr == "" {
		return time.Time{}, false
	}

	value, err := time.Parse(time.RFC3339, valueStr)
	if err != nil {
		return time.Time{}, false
	}
	return value, true
}

// ParseQueryString returns the query parameter value or empty string if not
// present. This is a convenience wrapper that is clearer in intent than
// calling r.URL.Query().Get() directly.
func ParseQueryString(r *http.Request, paramName string) string {
	return r.URL.Query().Get(paramName)
}

// ParseQueryStringList parses a comma-separated list of strings from a query
// parameter. Returns the list and true if the parameter is present, or nil
// and false if empty.
func ParseQueryStringList(r *http.Request, paramName string) ([]string, bool) {
	valueStr := r.URL.Query().Get(paramName)
	if valueStr == "" {
		return nil, false
	}

	parts := strings.Split(valueStr, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result, len(result) > 0
}

// ParseQueryBool parses a boolean query parameter. Returns true if the value
// is "true" or "1", false otherwise.
func ParseQueryBool(r *http.Request, paramName string) bool {
	value := r.URL.Query().Get(paramName)
	return value == "true" || value == "1"
}

// RequireQueryTime validates that a time query parameter is present and valid.
// Returns the time and true if valid, or sends an error response and returns
// false if missing or invalid.
func RequireQueryTime(w http.ResponseWriter, r *http.Request, paramName string) (time.Time, bool) {
	valueStr := r.URL.Query().Get(paramName)
	if valueStr == "" {
		RespondError(w, http.StatusBadRequest, paramName+" is required")
		return time.Time{}, false
	}

	value, err := time.Parse(time.RFC3339, valueStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest,
			fmt.Sprintf("Invalid %s format, expected RFC3339: %v", paramName, err))
		return time.Time{}, false
	}
	return value, true
}

// ValidateTimeRange validates that endTime is after startTime. Sends an error
// response and returns false if invalid.
func ValidateTimeRange(w http.ResponseWriter, startTime, endTime time.Time) bool {
	if endTime.Before(startTime) {
		RespondError(w, http.StatusBadRequest, "end_time must be after start_time")
		return false
	}
	return true
}

// ValidateStringInSet validates that a string value is in the allowed set.
// Returns true if valid or if value is empty (optional). Sends an error
// response and returns false if the value is not in the set.
func ValidateStringInSet(w http.ResponseWriter, value, paramName string, allowed map[string]bool) bool {
	if value == "" {
		return true
	}
	if !allowed[value] {
		RespondError(w, http.StatusBadRequest,
			fmt.Sprintf("Invalid %s: %s", paramName, value))
		return false
	}
	return true
}

// ValidateStringsInSet validates that all strings in a list are in the allowed
// set. Returns true if all are valid or if list is empty. Sends an error
// response and returns false if any value is not in the set.
func ValidateStringsInSet(w http.ResponseWriter, values []string, paramName string, allowed map[string]bool) bool {
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if !allowed[trimmed] {
			RespondError(w, http.StatusBadRequest,
				fmt.Sprintf("Invalid %s: %s", paramName, trimmed))
			return false
		}
	}
	return true
}

// ParseLimitWithDefaults parses a limit query parameter with default and
// maximum values. Returns the parsed limit, clamped to max if necessary.
func ParseLimitWithDefaults(r *http.Request, defaultLimit, maxLimit int) int {
	limitStr := r.URL.Query().Get("limit")
	if limitStr == "" {
		return defaultLimit
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

// ParseOffsetWithDefault parses an offset query parameter with a default value.
func ParseOffsetWithDefault(r *http.Request, defaultOffset int) int {
	offsetStr := r.URL.Query().Get("offset")
	if offsetStr == "" {
		return defaultOffset
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		return defaultOffset
	}
	return offset
}

// RequireMethod checks that the request method matches the expected method.
// Sends a Method Not Allowed response with proper Allow header if not.
// Returns true if method matches, false otherwise.
func RequireMethod(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, method := range allowed {
		if r.Method == method {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	return false
}

// RequireGET is a convenience method for RequireMethod(w, r, http.MethodGet).
func RequireGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// RequirePOST is a convenience method for RequireMethod(w, r, http.MethodPost).
func RequirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}
