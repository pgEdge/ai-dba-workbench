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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// hostileTitle and friends are alert text of the kind that reaches a
// notification from outside the Workbench: a provider error, a database
// or server name, or an LLM-written description.
const (
	hostileTitle       = "<!channel> *urgent* <https://evil.example/login|Open runbook>"
	hostileDescription = "@here see [the fix](https://evil.example/x) & &#64;all\n" +
		"# Big heading\n- bullet\n1. numbered\n> quoted <@U012AB3CD> `code` ~gone~ | a | b |\n" +
		`"quoted" \ backslash`
	hostileServer   = "db<1>&@channel"
	hostileDatabase = "app_db-1.example"
	hostileMetric   = "pg_stat_statements"
)

func hostileChatPayload(notifType database.NotificationType) *database.NotificationPayload {
	triggered := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	cleared := triggered.Add(90 * time.Minute)
	metric := hostileMetric
	dbName := hostileDatabase
	p := &database.NotificationPayload{
		AlertID:          7,
		AlertTitle:       hostileTitle,
		AlertDescription: hostileDescription,
		Severity:         "critical",
		ServerName:       hostileServer,
		ServerHost:       "db1.example.com",
		ServerPort:       5432,
		DatabaseName:     &dbName,
		MetricName:       &metric,
		NotificationType: string(notifType),
		TriggeredAt:      triggered,
		ReminderCount:    3,
	}
	if notifType == database.NotificationTypeAlertClear {
		p.ClearedAt = &cleared
	}
	return p
}

func TestEscapeSlackText(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain text", "plain text"},
		{"<!channel>", "&lt;!channel&gt;"},
		{"<https://evil.example|click>", "&lt;https://evil.example|click&gt;"},
		{"<@U012AB3CD>", "&lt;@U012AB3CD&gt;"},
		{"a & b", "a &amp; b"},
		{"&lt; already", "&amp;lt; already"},
		{"*bold* _it_ @channel", "*bold* _it_ @channel"},
	}
	for _, tt := range tests {
		if got := escapeSlackText(tt.in); got != tt.want {
			t.Errorf("escapeSlackText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEscapeMattermostText(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain", "plain text", "plain text"},
		{"empty", "", ""},
		{"emphasis", "*bold* _it_ ~s~", `\*bold\* \_it\_ \~s\~`},
		{"link", "[x](https://evil.example)", `\[x\](https://evil.example)`},
		{"image", "![x](https://evil.example/a.png)", `!\[x\](https://evil.example/a.png)`},
		{"slack link", "<https://evil.example|x>", `\<https://evil.example\|x\>`},
		{"slack announcement", "<!channel>", `\<!channel\>`},
		{"slack user", "<@U1>", "\\<@\u200bU1\\>"},
		{"code", "`x`", "\\`x\\`"},
		{"backslash", `a\b`, `a\\b`},
		{"entity", "&#64;all", `\&#64;all`},
		{"table", "| a |", `\| a \|`},
		{"mention", "@channel", "@\u200bchannel"},
		{"mention mid word", "a.@here", "a.@\u200bhere"},
		{"email", "ops@example.com", "ops@\u200bexample.com"},
		{"heading", "# big", `\# big`},
		{"hash mid line", "issue #5", "issue #5"},
		{"bullet", "- item", `\- item`},
		{"plus bullet", "+ item", `\+ item`},
		{"setext", "title\n===", "title\n\\==="},
		{"indented bullet", "  - item", `  \- item`},
		{"hyphen mid line", "db-1.example", "db-1.example"},
		{"ordered list", "12. item", `12\. item`},
		{"ordered paren", "3) item", `3\) item`},
		{"leading number", "10.0.0.1", `10\.0.0.1`},
		{"number mid line", "port 5432.", "port 5432."},
		{"digits then text", "5 rows", "5 rows"},
		{"quote", "> q", `\> q`},
		{"crlf lines", "a\r\n- b\n1. c", "a\r\n\\- b\n1\\. c"},
		{"unicode", "héllo — ✓", "héllo — ✓"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeMattermostText(tt.in); got != tt.want {
				t.Errorf("escapeMattermostText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// mattermostMentionWords splits text into words the way Mattermost's
// server-side mention parser does (StandardMentionParser.ProcessText):
// at every character that is not a letter, a digit or one of ":.-_@",
// then trimming leading ":.-_", then splitting again at ".-:". Any
// resulting word equal to "@channel", "@here" or "@all", or starting
// with '@' and followed by a name, would be treated as a mention.
func mattermostMentionWords(text string) []string {
	var out []string
	for _, word := range strings.FieldsFunc(text, func(c rune) bool {
		return c != ':' && c != '.' && c != '-' && c != '_' && c != '@' &&
			!unicode.IsLetter(c) && !unicode.IsNumber(c)
	}) {
		word = strings.TrimLeft(word, ":.-_")
		out = append(out, word)
		out = append(out, strings.FieldsFunc(word, func(c rune) bool {
			return c == '.' || c == '-' || c == ':'
		})...)
	}
	return out
}

func assertNoMattermostMention(t *testing.T, where, text string) {
	t.Helper()
	for _, w := range mattermostMentionWords(text) {
		if len(w) > 1 && strings.HasPrefix(w, "@") {
			t.Errorf("%s: %q would be parsed by Mattermost as the mention %q", where, text, w)
		}
	}
}

func TestEscapeMattermostText_DefeatsMentionParser(t *testing.T) {
	for _, in := range []string{
		"@channel", "@here", "@all", "@alice", "_@channel", ".@here", "a.@all",
		"x-@alice:", "(@channel)", hostileDescription, hostileServer,
	} {
		assertNoMattermostMention(t, "escaped", escapeMattermostText(in))
	}
}

// slackUserMention is the pattern Mattermost's Slack compatibility
// layer (replaceUserIds) rewrites into an @username mention.
var slackUserMention = regexp.MustCompile("<@([a-zA-Z0-9]+)>")

// chatMessage is the part of a Slack or Mattermost webhook body the
// default templates fill in.
type chatMessage struct {
	Text        string `json:"text"`
	Attachments []struct {
		Color  string `json:"color"`
		Text   string `json:"text"`
		Fields []struct {
			Title string `json:"title"`
			Value string `json:"value"`
		} `json:"fields"`
	} `json:"attachments"`
}

// chatStrings returns every string a chat service would parse for
// markup in the message.
func (m chatMessage) chatStrings() []string {
	out := []string{m.Text}
	for _, a := range m.Attachments {
		out = append(out, a.Text)
		for _, f := range a.Fields {
			out = append(out, f.Title, f.Value)
		}
	}
	return out
}

func decodeChatMessage(t *testing.T, body string) chatMessage {
	t.Helper()
	var m chatMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("rendered body is not a chat message: %v\n%s", err, body)
	}
	return m
}

var chatDefaultTemplates = []struct {
	name      string
	notifType database.NotificationType
	template  string
}{
	{"alert fire", database.NotificationTypeAlertFire, DefaultSlackAlertFireTemplate},
	{"alert clear", database.NotificationTypeAlertClear, DefaultSlackAlertClearTemplate},
	{"reminder", database.NotificationTypeReminder, DefaultSlackReminderTemplate},
}

func TestRenderChatJSON_Slack_EscapesHostileValues(t *testing.T) {
	renderer := NewTemplateRenderer()
	for _, tt := range chatDefaultTemplates {
		t.Run(tt.name, func(t *testing.T) {
			body, err := renderer.RenderChatJSON(ChatMarkupSlack, "",
				hostileChatPayload(tt.notifType), tt.template)
			if err != nil {
				t.Fatalf("RenderChatJSON() unexpected error: %v", err)
			}
			msg := decodeChatMessage(t, body)
			for _, s := range msg.chatStrings() {
				if strings.ContainsAny(s, "<>") {
					t.Errorf("Slack text %q contains a raw '<' or '>'", s)
				}
				if strings.Contains(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(
					s, "&amp;", ""), "&lt;", ""), "&gt;", ""), "&") {
					t.Errorf("Slack text %q contains an unescaped '&'", s)
				}
			}
			if !strings.Contains(msg.Text, "&lt;!channel&gt;") {
				t.Errorf("Slack text = %q, want the title's <!channel> escaped", msg.Text)
			}
			if !strings.Contains(msg.Text, "&lt;https://evil.example/login|Open runbook&gt;") {
				t.Errorf("Slack text = %q, want the title's link escaped", msg.Text)
			}
			if got := msg.Attachments[0].Fields[0].Value; !strings.HasPrefix(got, "db&lt;1&gt;&amp;@channel") {
				t.Errorf("Slack server field = %q, want the server name escaped", got)
			}
		})
	}
}

func TestRenderChatJSON_Slack_FireDescriptionAndColor(t *testing.T) {
	body, err := NewTemplateRenderer().RenderChatJSON(ChatMarkupSlack, "",
		hostileChatPayload(database.NotificationTypeAlertFire), DefaultSlackAlertFireTemplate)
	if err != nil {
		t.Fatalf("RenderChatJSON() unexpected error: %v", err)
	}
	msg := decodeChatMessage(t, body)
	a := msg.Attachments[0]
	if a.Color != SeverityColorCritical {
		t.Errorf("color = %q, want %q", a.Color, SeverityColorCritical)
	}
	// The JSON escaping still ran after the chat escaping: the quotes,
	// backslash and newlines survive the round trip as literal text.
	if !strings.Contains(a.Text, `"quoted" \ backslash`) || !strings.Contains(a.Text, "\n# Big heading") {
		t.Errorf("description = %q, want quotes, backslash and newlines intact", a.Text)
	}
	if !strings.Contains(a.Text, "&lt;@U012AB3CD&gt;") || !strings.Contains(a.Text, "&amp; &amp;#64;all") {
		t.Errorf("description = %q, want the user mention and ampersands escaped", a.Text)
	}
}

func TestRenderChatJSON_Mattermost_EscapesHostileValues(t *testing.T) {
	renderer := NewTemplateRenderer()
	for _, tt := range chatDefaultTemplates {
		t.Run(tt.name, func(t *testing.T) {
			body, err := renderer.RenderChatJSON(ChatMarkupMattermost, "",
				hostileChatPayload(tt.notifType), tt.template)
			if err != nil {
				t.Fatalf("RenderChatJSON() unexpected error: %v", err)
			}
			msg := decodeChatMessage(t, body)
			for _, s := range msg.chatStrings() {
				assertNoMattermostMention(t, tt.name, s)
				// Mattermost's Slack compatibility layer rewrites
				// these literal strings into mentions.
				for _, lit := range []string{"<!channel>", "<!here>", "<!all>"} {
					if strings.Contains(s, lit) {
						t.Errorf("Mattermost text %q contains %q", s, lit)
					}
				}
				if slackUserMention.MatchString(s) {
					t.Errorf("Mattermost text %q contains a <@userid> mention", s)
				}
			}
			want := `\<!channel\> \*urgent\* \<https://evil.example/login\|Open runbook\>`
			if !strings.Contains(msg.Text, want) {
				t.Errorf("Mattermost text = %q, want it to contain %q", msg.Text, want)
			}
			if got := msg.Attachments[0].Fields[0].Value; !strings.HasPrefix(got, "db\\<1\\>\\&@\u200bchannel") {
				t.Errorf("Mattermost server field = %q, want the server name escaped", got)
			}
		})
	}
}

func TestRenderChatJSON_Mattermost_FireDescriptionAndColor(t *testing.T) {
	body, err := NewTemplateRenderer().RenderChatJSON(ChatMarkupMattermost, "",
		hostileChatPayload(database.NotificationTypeAlertFire), DefaultSlackAlertFireTemplate)
	if err != nil {
		t.Fatalf("RenderChatJSON() unexpected error: %v", err)
	}
	msg := decodeChatMessage(t, body)
	a := msg.Attachments[0]
	if a.Color != SeverityColorCritical {
		t.Errorf("color = %q, want %q unescaped", a.Color, SeverityColorCritical)
	}
	want := "@\u200bhere see \\[the fix\\](https://evil.example/x) \\& \\&#64;all\n" +
		"\\# Big heading\n\\- bullet\n1\\. numbered\n" +
		"\\> quoted \\<@\u200bU012AB3CD\\> \\`code\\` \\~gone\\~ \\| a \\| b \\|\n" +
		`"quoted" \\ backslash`
	if a.Text != want {
		t.Errorf("description = %q, want %q", a.Text, want)
	}
	var metric, database string
	for _, f := range a.Fields {
		switch f.Title {
		case "Metric":
			metric = f.Value
		case "Database":
			database = f.Value
		}
	}
	if metric != `pg\_stat\_statements` {
		t.Errorf("metric field = %q, want underscores escaped", metric)
	}
	if database != `app\_db-1.example` {
		t.Errorf("database field = %q, want only the underscore escaped", database)
	}
}

// TestRenderChatJSON_CustomTemplateMarkupSurvives confirms that what an
// administrator writes into a custom template is not escaped: its
// links, mentions and emphasis reach the service as written, and only
// the values interpolated into it are escaped.
func TestRenderChatJSON_CustomTemplateMarkupSurvives(t *testing.T) {
	tests := []struct {
		name     string
		markup   ChatMarkup
		template string
		want     string
	}{
		{
			name:     "slack",
			markup:   ChatMarkupSlack,
			template: `{"text":"<!here> *{{.AlertTitle}}* <https://wiki.example.com/runbook|Runbook> & more"}`,
			want:     "<!here> *&lt;!channel&gt; *urgent* &lt;https://evil.example/login|Open runbook&gt;* <https://wiki.example.com/runbook|Runbook> & more",
		},
		{
			name:     "mattermost",
			markup:   ChatMarkupMattermost,
			template: `{"text":"@channel **{{.AlertTitle}}** [Runbook](https://wiki.example.com/runbook) <!here>"}`,
			want:     `@channel **\<!channel\> \*urgent\* \<https://evil.example/login\|Open runbook\>** [Runbook](https://wiki.example.com/runbook) <!here>`,
		},
	}
	renderer := NewTemplateRenderer()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := renderer.RenderChatJSON(tt.markup, tt.template,
				hostileChatPayload(database.NotificationTypeAlertFire), DefaultSlackAlertFireTemplate)
			if err != nil {
				t.Fatalf("RenderChatJSON() unexpected error: %v", err)
			}
			if got := decodeChatMessage(t, body).Text; got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderChatJSON_DoesNotMutatePayload(t *testing.T) {
	payload := hostileChatPayload(database.NotificationTypeAlertFire)
	for _, markup := range []ChatMarkup{ChatMarkupSlack, ChatMarkupMattermost} {
		if _, err := NewTemplateRenderer().RenderChatJSON(markup, "", payload,
			DefaultSlackAlertFireTemplate); err != nil {
			t.Fatalf("RenderChatJSON() unexpected error: %v", err)
		}
	}
	if payload.AlertTitle != hostileTitle || *payload.DatabaseName != hostileDatabase {
		t.Errorf("payload was modified: title %q, database %q",
			payload.AlertTitle, *payload.DatabaseName)
	}
}

func TestRenderChatJSON_Errors(t *testing.T) {
	renderer := NewTemplateRenderer()
	payload := hostileChatPayload(database.NotificationTypeAlertFire)
	tests := []struct {
		name     string
		markup   ChatMarkup
		template string
		want     string
	}{
		{"unknown markup", ChatMarkup(0), `{"text":"x"}`, "unknown chat markup"},
		{"no template", ChatMarkupSlack, "", "no template provided"},
		{"compile error", ChatMarkupMattermost, `{{.AlertTitle`, "failed to compile template"},
		{"execute error", ChatMarkupSlack, `{{.AlertTitle.Nope}}`, "failed to execute template"},
		{"invalid JSON", ChatMarkupSlack, `{"text":{{.AlertTitle}}}`, "not valid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := renderer.RenderChatJSON(tt.markup, tt.template, payload, "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("RenderChatJSON() error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

// TestChatNotifiers_SendEscapedBody drives the Slack and Mattermost
// notifiers end to end with the real renderer, confirming each posts
// the body escaped for its own service.
func TestChatNotifiers_SendEscapedBody(t *testing.T) {
	tests := []struct {
		name  string
		build func(*http.Client, TemplateRenderer) Notifier
		want  string
	}{
		{"slack", NewSlackNotifier, "🚨 Alert: &lt;!channel&gt; *urgent* &lt;https://evil.example/login|Open runbook&gt;"},
		{"mattermost", NewMattermostNotifier, `🚨 Alert: \<!channel\> \*urgent\* \<https://evil.example/login\|Open runbook\>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var received string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("failed to read request body: %v", err)
				}
				received = string(b)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			notifier := tt.build(server.Client(), NewTemplateRenderer())
			channel := &database.NotificationChannel{WebhookURL: strPtr(server.URL)}
			if err := notifier.Send(context.Background(), channel,
				hostileChatPayload(database.NotificationTypeAlertFire)); err != nil {
				t.Fatalf("Send() unexpected error: %v", err)
			}
			if got := decodeChatMessage(t, received).Text; got != tt.want {
				t.Errorf("posted text = %q, want %q", got, tt.want)
			}
		})
	}
}
