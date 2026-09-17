/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package notifications

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

func TestNewTemplateRenderer(t *testing.T) {
	renderer := NewTemplateRenderer()
	if renderer == nil {
		t.Error("NewTemplateRenderer() returned nil")
	}
}

func TestTemplateRenderer_Render_BasicFields(t *testing.T) {
	renderer := NewTemplateRenderer()

	triggeredAt := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	payload := &database.NotificationPayload{
		AlertID:          123,
		AlertType:        "metric",
		AlertTitle:       "High CPU Usage",
		AlertDescription: "CPU usage exceeds 90%",
		Severity:         "warning",
		Status:           "active",
		TriggeredAt:      triggeredAt,
		ConnectionID:     1,
		ServerName:       "prod-db-1",
		ServerHost:       "192.168.1.100",
		ServerPort:       5432,
		NotificationType: "alert_fire",
		ReminderCount:    0,
		Timestamp:        time.Now(),
	}

	template := "Alert: {{.AlertTitle}} - Server: {{.ServerName}} - Severity: {{.Severity}}"
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	expected := "Alert: High CPU Usage - Server: prod-db-1 - Severity: warning"
	if result != expected {
		t.Errorf("Render() = %q, want %q", result, expected)
	}
}

func TestTemplateRenderer_Render_DefaultTemplate(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test Alert",
	}

	defaultTemplate := "Default: {{.AlertTitle}}"

	// Empty template string should use default
	result, err := renderer.Render("", payload, defaultTemplate)
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	if result != "Default: Test Alert" {
		t.Errorf("Render() = %q, want %q", result, "Default: Test Alert")
	}
}

func TestTemplateRenderer_Render_NoTemplateError(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test",
	}

	// Both template and default are empty
	_, err := renderer.Render("", payload, "")
	if err == nil {
		t.Error("Render() expected error for empty templates")
	}

	if err.Error() != "no template provided" {
		t.Errorf("Render() error = %q, want %q", err.Error(), "no template provided")
	}
}

func TestTemplateRenderer_Render_InvalidTemplate(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test",
	}

	// Invalid template syntax
	template := "{{.AlertTitle"
	_, err := renderer.Render(template, payload, "")
	if err == nil {
		t.Error("Render() expected error for invalid template syntax")
	}

	if !strings.Contains(err.Error(), "failed to compile template") {
		t.Errorf("Error should mention template compilation: %v", err)
	}
}

func TestTemplateRenderer_Render_OptionalFields(t *testing.T) {
	renderer := NewTemplateRenderer()

	metricName := "cpu_usage"
	metricValue := 95.5
	thresholdValue := 90.0
	operator := ">"
	databaseName := "mydb"

	payload := &database.NotificationPayload{
		AlertTitle:     "Test Alert",
		MetricName:     &metricName,
		MetricValue:    &metricValue,
		ThresholdValue: &thresholdValue,
		Operator:       &operator,
		DatabaseName:   &databaseName,
	}

	template := "Metric: {{.MetricName}} = {{.MetricValue}} ({{.Operator}} {{.ThresholdValue}}) in {{.DatabaseName}}"
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	expected := "Metric: cpu_usage = 95.5 (> 90) in mydb"
	if result != expected {
		t.Errorf("Render() = %q, want %q", result, expected)
	}
}

func TestTemplateRenderer_Render_ComputedFields(t *testing.T) {
	renderer := NewTemplateRenderer()

	tests := []struct {
		name     string
		severity string
		expected string
	}{
		{"critical", "critical", SeverityColorCritical},
		{"warning", "warning", SeverityColorWarning},
		{"info", "info", SeverityColorInfo},
		{"unknown defaults to info", "unknown", SeverityColorInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := &database.NotificationPayload{
				Severity: tt.severity,
			}

			template := "{{.SeverityColor}}"
			result, err := renderer.Render(template, payload, "")
			if err != nil {
				t.Errorf("Render() unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("SeverityColor for %s = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestTemplateRenderer_Render_SeverityEmoji(t *testing.T) {
	renderer := NewTemplateRenderer()

	tests := []struct {
		name     string
		severity string
		expected string
	}{
		{"critical", "critical", SeverityEmojiCritical},
		{"warning", "warning", SeverityEmojiWarning},
		{"info", "info", SeverityEmojiInfo},
		{"unknown defaults to info", "other", SeverityEmojiInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := &database.NotificationPayload{
				Severity: tt.severity,
			}

			template := "{{.SeverityEmoji}}"
			result, err := renderer.Render(template, payload, "")
			if err != nil {
				t.Errorf("Render() unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("SeverityEmoji for %s = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestTemplateRenderer_Render_Duration(t *testing.T) {
	renderer := NewTemplateRenderer()

	triggeredAt := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	clearedAt := time.Date(2025, 1, 15, 12, 30, 45, 0, time.UTC)

	payload := &database.NotificationPayload{
		TriggeredAt: triggeredAt,
		ClearedAt:   &clearedAt,
	}

	template := "{{.Duration}}"
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	// Duration should be 2h 30m
	if result != "2h 30m" {
		t.Errorf("Duration = %q, want %q", result, "2h 30m")
	}
}

func TestTemplateRenderer_Render_DurationFromField(t *testing.T) {
	renderer := NewTemplateRenderer()

	duration := "1h 15m"
	payload := &database.NotificationPayload{
		TriggeredAt: time.Now(),
		ClearedAt:   nil,
		Duration:    &duration,
	}

	template := "{{.Duration}}"
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	if result != "1h 15m" {
		t.Errorf("Duration = %q, want %q", result, "1h 15m")
	}
}

func TestTemplateRenderer_Render_EmptyDuration(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		TriggeredAt: time.Now(),
		ClearedAt:   nil,
		Duration:    nil,
	}

	template := "Duration: [{{.Duration}}]"
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	if result != "Duration: []" {
		t.Errorf("Duration = %q, want %q", result, "Duration: []")
	}
}

func TestTemplateRenderer_Render_TemplateCaching(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test",
	}

	template := "Cached: {{.AlertTitle}}"

	// Render the same template multiple times
	for i := 0; i < 5; i++ {
		result, err := renderer.Render(template, payload, "")
		if err != nil {
			t.Errorf("Render() iteration %d unexpected error: %v", i, err)
		}

		if result != "Cached: Test" {
			t.Errorf("Render() iteration %d = %q, want %q", i, result, "Cached: Test")
		}
	}
}

func TestTemplateRenderer_RenderJSON_ValidJSON(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertID:    123,
		AlertTitle: "Test Alert",
		Severity:   "warning",
	}

	template := `{"alert_id": {{.AlertID}}, "title": "{{.AlertTitle}}", "severity": "{{.Severity}}"}`
	result, err := renderer.RenderJSON(template, payload, "")
	if err != nil {
		t.Errorf("RenderJSON() unexpected error: %v", err)
	}

	expected := `{"alert_id": 123, "title": "Test Alert", "severity": "warning"}`
	if result != expected {
		t.Errorf("RenderJSON() = %q, want %q", result, expected)
	}
}

func TestTemplateRenderer_RenderJSON_InvalidJSON(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test",
	}

	// Template produces invalid JSON (missing closing brace)
	template := `{"title": "{{.AlertTitle}}"`
	_, err := renderer.RenderJSON(template, payload, "")
	if err == nil {
		t.Error("RenderJSON() expected error for invalid JSON")
	}

	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("Error should mention invalid JSON: %v", err)
	}
}

func TestTemplateRenderer_RenderJSON_TemplateError(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test",
	}

	// Invalid template syntax
	template := `{"title": "{{.AlertTitle"`
	_, err := renderer.RenderJSON(template, payload, "")
	if err == nil {
		t.Error("RenderJSON() expected error for template syntax error")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"seconds only", 45 * time.Second, "45s"},
		{"minutes and seconds", 5*time.Minute + 30*time.Second, "5m 30s"},
		{"hours and minutes", 3*time.Hour + 15*time.Minute, "3h 15m"},
		{"days and hours", 2*24*time.Hour + 6*time.Hour, "2d 6h"},
		{"zero", 0, "0s"},
		{"negative duration", -5 * time.Minute, "5m 0s"},
		{"exactly one hour", 1 * time.Hour, "1h 0m"},
		{"exactly one day", 24 * time.Hour, "1d 0h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestGetSeverityColor(t *testing.T) {
	tests := []struct {
		severity string
		expected string
	}{
		{"critical", SeverityColorCritical},
		{"warning", SeverityColorWarning},
		{"info", SeverityColorInfo},
		{"unknown", SeverityColorInfo},
		{"", SeverityColorInfo},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			result := getSeverityColor(tt.severity)
			if result != tt.expected {
				t.Errorf("getSeverityColor(%q) = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestGetSeverityEmoji(t *testing.T) {
	tests := []struct {
		severity string
		expected string
	}{
		{"critical", SeverityEmojiCritical},
		{"warning", SeverityEmojiWarning},
		{"info", SeverityEmojiInfo},
		{"unknown", SeverityEmojiInfo},
		{"", SeverityEmojiInfo},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			result := getSeverityEmoji(tt.severity)
			if result != tt.expected {
				t.Errorf("getSeverityEmoji(%q) = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestTemplateRenderer_Render_DateFormatting(t *testing.T) {
	renderer := NewTemplateRenderer()

	triggeredAt := time.Date(2025, 6, 15, 14, 30, 45, 0, time.UTC)

	payload := &database.NotificationPayload{
		TriggeredAt: triggeredAt,
	}

	template := `{{.TriggeredAt.Format "2006-01-02 15:04:05"}}`
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	expected := "2025-06-15 14:30:45"
	if result != expected {
		t.Errorf("Render() = %q, want %q", result, expected)
	}
}

func TestTemplateRenderer_Render_ConditionalFields(t *testing.T) {
	renderer := NewTemplateRenderer()

	// With database name
	dbName := "production"
	payloadWithDB := &database.NotificationPayload{
		AlertTitle:   "Test",
		DatabaseName: &dbName,
	}

	template := `Alert: {{.AlertTitle}}{{if .DatabaseName}} in {{.DatabaseName}}{{end}}`
	result, err := renderer.Render(template, payloadWithDB, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	if result != "Alert: Test in production" {
		t.Errorf("Render() = %q, want %q", result, "Alert: Test in production")
	}

	// Without database name
	payloadWithoutDB := &database.NotificationPayload{
		AlertTitle:   "Test",
		DatabaseName: nil,
	}

	result2, err := renderer.Render(template, payloadWithoutDB, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	if result2 != "Alert: Test" {
		t.Errorf("Render() = %q, want %q", result2, "Alert: Test")
	}
}

func TestDefaultTemplatesAreValid(t *testing.T) {
	renderer := NewTemplateRenderer()

	triggeredAt := time.Now()
	clearedAt := triggeredAt.Add(1 * time.Hour)
	metricName := "cpu_usage"
	metricValue := 95.0
	thresholdValue := 90.0
	operator := ">"
	dbName := "testdb"

	payload := &database.NotificationPayload{
		AlertID:          1,
		AlertType:        "metric",
		AlertTitle:       "High CPU Usage",
		AlertDescription: "CPU usage exceeds threshold",
		Severity:         "warning",
		Status:           "active",
		TriggeredAt:      triggeredAt,
		ClearedAt:        &clearedAt,
		MetricName:       &metricName,
		MetricValue:      &metricValue,
		ThresholdValue:   &thresholdValue,
		Operator:         &operator,
		ConnectionID:     1,
		ServerName:       "prod-db",
		ServerHost:       "10.0.0.1",
		ServerPort:       5432,
		DatabaseName:     &dbName,
		NotificationType: "alert_fire",
		ReminderCount:    2,
		Timestamp:        time.Now(),
	}

	templates := []struct {
		name     string
		template string
		isJSON   bool
	}{
		{"Slack Alert Fire", DefaultSlackAlertFireTemplate, true},
		{"Slack Alert Clear", DefaultSlackAlertClearTemplate, true},
		{"Slack Reminder", DefaultSlackReminderTemplate, true},
		{"Email Alert Fire", DefaultEmailAlertFireTemplate, false},
		{"Email Alert Clear", DefaultEmailAlertClearTemplate, false},
		{"Email Reminder", DefaultEmailReminderTemplate, false},
		{"Webhook Alert Fire", DefaultWebhookAlertFireTemplate, true},
		{"Webhook Alert Clear", DefaultWebhookAlertClearTemplate, true},
		{"Webhook Reminder", DefaultWebhookReminderTemplate, true},
	}

	for _, tt := range templates {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			var err error

			if tt.isJSON {
				result, err = renderer.RenderJSON(tt.template, payload, "")
			} else {
				result, err = renderer.Render(tt.template, payload, "")
			}

			if err != nil {
				t.Errorf("Template %s failed to render: %v", tt.name, err)
			}

			if result == "" {
				t.Errorf("Template %s produced empty result", tt.name)
			}
		})
	}
}

func TestTemplateRenderer_RenderJSON_EscapesSpecialCharacters(t *testing.T) {
	renderer := NewTemplateRenderer()

	tests := []struct {
		name        string
		description string
		wantSubstr  string
	}{
		{
			"double quotes",
			`password authentication failed for user "postgres"`,
			`password authentication failed for user \"postgres\"`,
		},
		{
			"backticks and quotes",
			"connection error: failed to connect to `user=postgres database=postgres`: failed SASL auth: FATAL: password authentication failed for user \"postgres\"",
			`failed for user \"postgres\"`,
		},
		{
			"newlines",
			"line one\nline two\nline three",
			`line one\nline two\nline three`,
		},
		{
			"backslashes",
			`path is C:\Users\admin`,
			`path is C:\\Users\\admin`,
		},
		{
			"tabs",
			"col1\tcol2\tcol3",
			`col1\tcol2\tcol3`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := &database.NotificationPayload{
				AlertID:          1,
				AlertTitle:       "Test Alert",
				AlertDescription: tt.description,
				Severity:         "warning",
			}

			tmpl := `{"title":"{{.AlertTitle}}","description":"{{.AlertDescription}}"}`
			result, err := renderer.RenderJSON(tmpl, payload, "")
			if err != nil {
				t.Fatalf("RenderJSON() unexpected error: %v", err)
			}

			if !strings.Contains(result, tt.wantSubstr) {
				t.Errorf("RenderJSON() result should contain %q, got %q", tt.wantSubstr, result)
			}
		})
	}
}

func TestTemplateRenderer_RenderJSON_DoesNotDoubleEscapeSafeStrings(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertID:          123,
		AlertTitle:       "High CPU Usage",
		AlertDescription: "CPU usage exceeds 90%",
		Severity:         "warning",
	}

	tmpl := `{"alert_id": {{.AlertID}}, "title": "{{.AlertTitle}}", "description": "{{.AlertDescription}}"}`
	result, err := renderer.RenderJSON(tmpl, payload, "")
	if err != nil {
		t.Fatalf("RenderJSON() unexpected error: %v", err)
	}

	expected := `{"alert_id": 123, "title": "High CPU Usage", "description": "CPU usage exceeds 90%"}`
	if result != expected {
		t.Errorf("RenderJSON() = %q, want %q", result, expected)
	}
}

func TestTemplateRenderer_RenderJSON_DefaultTemplatesWithSpecialChars(t *testing.T) {
	renderer := NewTemplateRenderer()

	triggeredAt := time.Now()
	clearedAt := triggeredAt.Add(1 * time.Hour)
	metricName := "cpu_usage"
	metricValue := 95.0
	thresholdValue := 90.0
	operator := ">"
	dbName := "testdb"

	// Use a description that contains characters problematic for JSON
	payload := &database.NotificationPayload{
		AlertID:          1,
		AlertType:        "metric",
		AlertTitle:       "Connection Failed",
		AlertDescription: "connection error: failed to connect to `user=postgres database=postgres`: FATAL: password authentication failed for user \"postgres\"",
		Severity:         "critical",
		Status:           "active",
		TriggeredAt:      triggeredAt,
		ClearedAt:        &clearedAt,
		MetricName:       &metricName,
		MetricValue:      &metricValue,
		ThresholdValue:   &thresholdValue,
		Operator:         &operator,
		ConnectionID:     1,
		ServerName:       "prod-db",
		ServerHost:       "10.0.0.1",
		ServerPort:       5432,
		DatabaseName:     &dbName,
		NotificationType: "alert_fire",
		ReminderCount:    2,
		Timestamp:        time.Now(),
	}

	jsonTemplates := []struct {
		name     string
		template string
	}{
		{"Slack Alert Fire", DefaultSlackAlertFireTemplate},
		{"Slack Alert Clear", DefaultSlackAlertClearTemplate},
		{"Slack Reminder", DefaultSlackReminderTemplate},
		{"Webhook Alert Fire", DefaultWebhookAlertFireTemplate},
		{"Webhook Alert Clear", DefaultWebhookAlertClearTemplate},
		{"Webhook Reminder", DefaultWebhookReminderTemplate},
	}

	for _, tt := range jsonTemplates {
		t.Run(tt.name, func(t *testing.T) {
			result, err := renderer.RenderJSON(tt.template, payload, "")
			if err != nil {
				t.Errorf("RenderJSON(%s) with special chars failed: %v", tt.name, err)
			}
			if result == "" {
				t.Errorf("RenderJSON(%s) produced empty result", tt.name)
			}
		})
	}
}

func TestJsonEscapeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain text", "hello world", "hello world"},
		{"double quotes", `say "hello"`, `say \"hello\"`},
		{"backslash", `C:\path`, `C:\\path`},
		{"newline", "line1\nline2", `line1\nline2`},
		{"tab", "col1\tcol2", `col1\tcol2`},
		{"backtick", "`code`", "`code`"},
		{"mixed special", "error: \"fail\"\nnext", `error: \"fail\"\nnext`},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jsonEscapeString(tt.input)
			if result != tt.expected {
				t.Errorf("jsonEscapeString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTemplateRenderer_Render_AllPayloadFields(t *testing.T) {
	renderer := NewTemplateRenderer()

	metricName := "connections"
	metricValue := 150.0
	thresholdValue := 100.0
	operator := ">="
	dbName := "maindb"
	duration := "2h 30m"
	clearedAt := time.Now()

	payload := &database.NotificationPayload{
		AlertID:          42,
		AlertType:        "threshold",
		AlertTitle:       "Connection Limit",
		AlertDescription: "Too many connections",
		Severity:         "critical",
		Status:           "cleared",
		TriggeredAt:      time.Now().Add(-3 * time.Hour),
		ClearedAt:        &clearedAt,
		MetricName:       &metricName,
		MetricValue:      &metricValue,
		ThresholdValue:   &thresholdValue,
		Operator:         &operator,
		ConnectionID:     5,
		ServerName:       "primary",
		ServerHost:       "db.example.com",
		ServerPort:       5433,
		DatabaseName:     &dbName,
		NotificationType: "alert_clear",
		ReminderCount:    0,
		Duration:         &duration,
		Timestamp:        time.Now(),
	}

	template := `ID:{{.AlertID}} Type:{{.AlertType}} Title:{{.AlertTitle}} Desc:{{.AlertDescription}} ` +
		`Sev:{{.Severity}} Status:{{.Status}} Conn:{{.ConnectionID}} Server:{{.ServerName}} ` +
		`Host:{{.ServerHost}} Port:{{.ServerPort}} NotifType:{{.NotificationType}} ` +
		`Remind:{{.ReminderCount}} Metric:{{.MetricName}} Val:{{.MetricValue}} ` +
		`Thresh:{{.ThresholdValue}} Op:{{.Operator}} DB:{{.DatabaseName}}`

	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	// Verify key fields are present
	if !strings.Contains(result, "ID:42") {
		t.Error("Result should contain AlertID")
	}
	if !strings.Contains(result, "Title:Connection Limit") {
		t.Error("Result should contain AlertTitle")
	}
	if !strings.Contains(result, "Sev:critical") {
		t.Error("Result should contain Severity")
	}
	if !strings.Contains(result, "DB:maindb") {
		t.Error("Result should contain DatabaseName")
	}
}

// telegramRenderPayload returns a payload with every optional field
// populated, for exercising the Telegram default templates.
func telegramRenderPayload() *database.NotificationPayload {
	metric := "connections_used"
	metricValue := 195.0
	threshold := 180.0
	operator := ">"
	dbName := "orders"
	triggeredAt := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

	return &database.NotificationPayload{
		AlertID:          42,
		AlertType:        "metric",
		AlertTitle:       "Connection limit",
		AlertDescription: "Too many connections",
		Severity:         "critical",
		Status:           "active",
		TriggeredAt:      triggeredAt,
		MetricName:       &metric,
		MetricValue:      &metricValue,
		ThresholdValue:   &threshold,
		Operator:         &operator,
		ConnectionID:     1,
		ServerName:       "prod-db-1",
		ServerHost:       "db1.example.com",
		ServerPort:       5432,
		DatabaseName:     &dbName,
		NotificationType: string(database.NotificationTypeAlertFire),
		ReminderCount:    3,
		Timestamp:        triggeredAt,
	}
}

func TestTemplateRenderer_RenderHTML_EscapesPayloadButNotTemplate(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := telegramRenderPayload()
	payload.AlertTitle = `idx_a<b & "c"`
	payload.AlertDescription = "5 > 3 && true"

	result, err := renderer.RenderHTML(
		`<b>{{.AlertTitle}}</b>: <i>{{.AlertDescription}}</i>`, payload, "")
	if err != nil {
		t.Fatalf("RenderHTML() unexpected error: %v", err)
	}

	want := `<b>idx_a&lt;b &amp; &#34;c&#34;</b>: <i>5 &gt; 3 &amp;&amp; true</i>`
	if result != want {
		t.Errorf("RenderHTML() = %q, want %q", result, want)
	}
}

func TestTemplateRenderer_RenderHTML_UsesDefaultWhenTemplateEmpty(t *testing.T) {
	renderer := NewTemplateRenderer()

	result, err := renderer.RenderHTML("", telegramRenderPayload(),
		`<b>{{.AlertTitle}}</b>`)
	if err != nil {
		t.Fatalf("RenderHTML() unexpected error: %v", err)
	}
	if result != "<b>Connection limit</b>" {
		t.Errorf("RenderHTML() = %q, want the default template's output", result)
	}
}

func TestTemplateRenderer_RenderHTML_NoTemplate(t *testing.T) {
	renderer := NewTemplateRenderer()

	_, err := renderer.RenderHTML("", telegramRenderPayload(), "")
	if err == nil {
		t.Fatal("RenderHTML() expected an error when no template is provided")
	}
	if !strings.Contains(err.Error(), "no template provided") {
		t.Errorf("RenderHTML() error = %q, want it to mention the missing template",
			err.Error())
	}
}

func TestTemplateRenderer_RenderHTML_CompileError(t *testing.T) {
	renderer := NewTemplateRenderer()

	_, err := renderer.RenderHTML("{{.AlertTitle", telegramRenderPayload(), "")
	if err == nil {
		t.Fatal("RenderHTML() expected a compile error")
	}
	if !strings.Contains(err.Error(), "failed to compile template") {
		t.Errorf("RenderHTML() error = %q, want a compile failure", err.Error())
	}
}

func TestTemplateRenderer_RenderHTML_ExecuteError(t *testing.T) {
	renderer := NewTemplateRenderer()

	// Calling a method that does not exist on a string compiles but
	// fails at execution time.
	_, err := renderer.RenderHTML("{{.AlertTitle.NoSuchMethod}}",
		telegramRenderPayload(), "")
	if err == nil {
		t.Fatal("RenderHTML() expected an execute error")
	}
	if !strings.Contains(err.Error(), "failed to execute template") {
		t.Errorf("RenderHTML() error = %q, want an execute failure", err.Error())
	}
}

// TestTemplateRenderer_RenderHTML_LeavesNonStringsAlone confirms numbers
// and times pass through unescaped, so arithmetic and Format calls in a
// template still work.
func TestTemplateRenderer_RenderHTML_LeavesNonStringsAlone(t *testing.T) {
	renderer := NewTemplateRenderer()

	result, err := renderer.RenderHTML(
		`{{.AlertID}}|{{.ServerPort}}|{{.MetricValue}}|{{.TriggeredAt.Format "2006-01-02"}}`,
		telegramRenderPayload(), "")
	if err != nil {
		t.Fatalf("RenderHTML() unexpected error: %v", err)
	}
	if result != "42|5432|195|2026-03-01" {
		t.Errorf("RenderHTML() = %q, want %q", result, "42|5432|195|2026-03-01")
	}
}

func TestHTMLEscapeStringValues(t *testing.T) {
	data := map[string]any{
		"plain":   "nothing special",
		"markup":  `a<b>c&d"e'f`,
		"number":  42,
		"float":   1.5,
		"boolean": true,
		"time":    time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC),
	}

	htmlEscapeStringValues(data)

	if data["plain"] != "nothing special" {
		t.Errorf("plain = %v, want it unchanged", data["plain"])
	}
	if data["markup"] != "a&lt;b&gt;c&amp;d&#34;e&#39;f" {
		t.Errorf("markup = %v, want the escaped form", data["markup"])
	}
	if data["number"] != 42 {
		t.Errorf("number = %v, want 42", data["number"])
	}
	if data["float"] != 1.5 {
		t.Errorf("float = %v, want 1.5", data["float"])
	}
	if data["boolean"] != true {
		t.Errorf("boolean = %v, want true", data["boolean"])
	}
	if _, ok := data["time"].(time.Time); !ok {
		t.Errorf("time = %T, want it left as a time.Time", data["time"])
	}
}

// TestHTMLEscapeStringValues_RecursesIntoContainers is the regression
// test for H-003. The helper used to skip every non-string value, which
// was safe only by accident: nothing the payload produces today is a
// container. A field of affected relations, a map of labels or a slice
// of LLM-generated detail would have gone into a parse_mode: HTML
// message unescaped.
func TestHTMLEscapeStringValues_RecursesIntoContainers(t *testing.T) {
	const raw = `a<b>c&d"e'f`
	const escaped = "a&lt;b&gt;c&amp;d&#34;e&#39;f"

	strSlice := []string{raw}
	strMap := map[string]string{"k": raw}
	anySlice := []any{raw, 7, []string{raw}}
	anyMap := map[string]any{"inner": raw, "deeper": map[string]any{"k": raw}}

	data := map[string]any{
		"strings": strSlice,
		"strmap":  strMap,
		"anys":    anySlice,
		"anymap":  anyMap,
	}

	htmlEscapeStringValues(data)

	got, ok := data["strings"].([]string)
	if !ok || len(got) != 1 || got[0] != escaped {
		t.Errorf("[]string = %v, want %q escaped", data["strings"], raw)
	}
	gotMap, ok := data["strmap"].(map[string]string)
	if !ok || gotMap["k"] != escaped {
		t.Errorf("map[string]string = %v, want %q escaped", data["strmap"], raw)
	}
	gotAny, ok := data["anys"].([]any)
	if !ok || len(gotAny) != 3 {
		t.Fatalf("[]any = %v, want three elements", data["anys"])
	}
	if gotAny[0] != escaped {
		t.Errorf("[]any[0] = %v, want it escaped", gotAny[0])
	}
	if gotAny[1] != 7 {
		t.Errorf("[]any[1] = %v, want the number untouched", gotAny[1])
	}
	if nested, ok := gotAny[2].([]string); !ok || nested[0] != escaped {
		t.Errorf("[]any[2] = %v, want the nested slice escaped", gotAny[2])
	}
	gotAnyMap, ok := data["anymap"].(map[string]any)
	if !ok || gotAnyMap["inner"] != escaped {
		t.Errorf("map[string]any = %v, want it escaped", data["anymap"])
	}
	if deeper, ok := gotAnyMap["deeper"].(map[string]any); !ok || deeper["k"] != escaped {
		t.Errorf("nested map = %v, want it escaped", gotAnyMap["deeper"])
	}

	// The caller's containers are shared with the payload, which the
	// same alert fans out to every other channel, so escaping must
	// copy rather than mutate them.
	if strSlice[0] != raw {
		t.Error("the caller's []string was escaped in place")
	}
	if strMap["k"] != raw {
		t.Error("the caller's map[string]string was escaped in place")
	}
	if anySlice[0] != raw {
		t.Error("the caller's []any was escaped in place")
	}
	if anyMap["inner"] != raw {
		t.Error("the caller's map[string]any was escaped in place")
	}
}

// TestNotificationPayloadFieldTypesAreEscapable is a tripwire rather
// than a behavior test, and it is the other half of the H-003 fix.
// htmlEscapeStringValues now handles the container types a template
// data map can hold, but enhancePayload copies every payload field
// across by hand, so a field of some type nobody considered could be
// added without anyone revisiting escaping at all. This fails as soon
// as NotificationPayload grows an exported field outside the set of
// types that provably cannot carry markup, which forces whoever adds it
// to come and check.
func TestNotificationPayloadFieldTypesAreEscapable(t *testing.T) {
	// Strings are escaped; numbers, booleans and times render through
	// fmt and cannot produce markup.
	escapable := map[string]bool{
		"string":     true,
		"*string":    true,
		"bool":       true,
		"*bool":      true,
		"int":        true,
		"*int":       true,
		"int64":      true,
		"*int64":     true,
		"float64":    true,
		"*float64":   true,
		"time.Time":  true,
		"*time.Time": true,
	}

	typ := reflect.TypeOf(database.NotificationPayload{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		if !escapable[field.Type.String()] {
			t.Errorf("NotificationPayload.%s is a %s, which htmlEscapeStringValues "+
				"has not been shown to escape. Add it to htmlEscapeValue (and to "+
				"this list) before sending it through a parse_mode: HTML message.",
				field.Name, field.Type)
		}
	}
}

// TestTelegramDefaultTemplatesAreNotMatchedByIsHTMLTemplate guards the
// trap the Telegram design avoids. isHTMLTemplate only recognizes
// templates that start with an HTML document or block prefix, and the
// Telegram defaults start with an emoji or an action. If one ever began
// with <div>, <html> or <body>, Render would route it through
// html/template and escape the literal markup, and someone might then be
// tempted to use Render in place of RenderHTML.
func TestTelegramDefaultTemplatesAreNotMatchedByIsHTMLTemplate(t *testing.T) {
	templates := map[string]string{
		"alert fire":  DefaultTelegramAlertFireTemplate,
		"alert clear": DefaultTelegramAlertClearTemplate,
		"reminder":    DefaultTelegramReminderTemplate,
	}

	for name, tmpl := range templates {
		t.Run(name, func(t *testing.T) {
			if isHTMLTemplate(tmpl) {
				t.Errorf("the Telegram %s template is matched by isHTMLTemplate; "+
					"it must not start with an HTML block prefix", name)
			}
		})
	}
}

// TestTelegramDefaultTemplates_RenderWithinLimits renders each default
// with the real renderer and checks the output is usable Telegram HTML:
// under the length limit, non-empty, and carrying the fields the Slack
// defaults carry.
func TestTelegramDefaultTemplates_RenderWithinLimits(t *testing.T) {
	renderer := NewTemplateRenderer()
	cleared := time.Date(2026, 3, 1, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name           string
		template       string
		clearedAt      *time.Time
		wantSubstrings []string
	}{
		{
			name:     "alert fire",
			template: DefaultTelegramAlertFireTemplate,
			wantSubstrings: []string{
				"🔴 <b>Alert: Connection limit</b>",
				"<b>Server:</b> prod-db-1 (<code>db1.example.com:5432</code>)",
				"<b>Severity:</b> critical",
				"<code>orders</code>",
				"<code>connections_used</code>",
				"threshold &gt; 180",
				"Too many connections",
			},
		},
		{
			name:      "alert clear",
			template:  DefaultTelegramAlertClearTemplate,
			clearedAt: &cleared,
			wantSubstrings: []string{
				"✅ <b>Resolved: Connection limit</b>",
				"<b>Duration:</b> 1h 30m",
				"<b>Cleared:</b> 2026-03-01 10:30:00 UTC",
			},
		},
		{
			name:     "reminder",
			template: DefaultTelegramReminderTemplate,
			wantSubstrings: []string{
				"⏰ <b>Reminder: Connection limit</b> is still active",
				"<b>Reminder:</b> #3",
				"<b>Active since:</b> 2026-03-01 09:00:00 UTC",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := telegramRenderPayload()
			payload.ClearedAt = tt.clearedAt

			result, err := renderer.RenderHTML("", payload, tt.template)
			if err != nil {
				t.Fatalf("RenderHTML() unexpected error: %v", err)
			}
			if result == "" {
				t.Fatal("RenderHTML() produced empty output")
			}
			if n := len([]rune(result)); n > telegramMaxMessageRunes {
				t.Errorf("rendered %d runes, over Telegram's %d limit", n,
					telegramMaxMessageRunes)
			}
			for _, want := range tt.wantSubstrings {
				if !strings.Contains(result, want) {
					t.Errorf("rendered output is missing %q\ngot:\n%s", want, result)
				}
			}
		})
	}
}

// TestTelegramDefaultTemplates_OmitOptionalFields checks the conditional
// blocks so a payload with no database or metric renders cleanly.
func TestTelegramDefaultTemplates_OmitOptionalFields(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertID:          7,
		AlertTitle:       "Replication lag",
		AlertDescription: "Lag is high",
		Severity:         "warning",
		Status:           "active",
		TriggeredAt:      time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC),
		ConnectionID:     1,
		ServerName:       "prod-db-1",
		ServerHost:       "db1.example.com",
		ServerPort:       5432,
		NotificationType: string(database.NotificationTypeAlertFire),
		ReminderCount:    1,
	}

	for name, tmpl := range map[string]string{
		"alert fire":  DefaultTelegramAlertFireTemplate,
		"alert clear": DefaultTelegramAlertClearTemplate,
		"reminder":    DefaultTelegramReminderTemplate,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := renderer.RenderHTML("", payload, tmpl)
			if err != nil {
				t.Fatalf("RenderHTML() unexpected error: %v", err)
			}
			if strings.Contains(result, "<b>Database:</b>") {
				t.Errorf("database block rendered for a payload with no database:\n%s",
					result)
			}
			if strings.Contains(result, "<b>Metric:</b>") {
				t.Errorf("metric block rendered for a payload with no metric:\n%s",
					result)
			}
			if !strings.Contains(result, "Replication lag") {
				t.Errorf("rendered output is missing the alert title:\n%s", result)
			}
		})
	}
}
