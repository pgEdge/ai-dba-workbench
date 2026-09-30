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
	"strings"
)

// ChatMarkup identifies the markup a chat service parses in the text of
// an incoming-webhook message, and so which escaping
// TemplateRenderer.RenderChatJSON applies to the template values.
type ChatMarkup int

const (
	// ChatMarkupSlack is Slack's mrkdwn.
	ChatMarkupSlack ChatMarkup = iota + 1

	// ChatMarkupMattermost is Mattermost's Markdown, together with the
	// Slack compatibility translation Mattermost applies to incoming
	// webhooks.
	ChatMarkupMattermost
)

// slackEscaper replaces the three characters Slack treats as control
// characters with the HTML entities its formatting documentation
// prescribes. Slack decodes only these three entities for display, so
// nothing else may be encoded. Escaping '<' is what stops a value from
// forming a <!channel> or <!here> announcement, a <@user> mention or a
// <https://example.com|click here> link. Plain "@channel" is only
// parsed when the message sets link_names, which the default templates
// do not. Slack documents no escape for its emphasis markers ('*', '_',
// '~' and '`'), so those are left alone; they can change how a value
// looks but cannot notify anyone or disguise a link.
var slackEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// escapeSlackText escapes s for Slack mrkdwn.
func escapeSlackText(s string) string {
	return slackEscaper.Replace(s)
}

// mentionBreaker is inserted after every '@' in a Mattermost value. It
// is U+200B ZERO WIDTH SPACE, which renders as nothing.
const mentionBreaker = "\u200b"

// isMattermostInlineMarkup reports whether c opens or delimits a
// Markdown construct wherever it appears on a line: escapes, code
// spans, emphasis and strikethrough, links and images, autolinks,
// block quotes, tables and character references. The last matters
// for mentions too, since Mattermost decodes "&#64;channel" to
// "@channel" before looking for mentions.
func isMattermostInlineMarkup(c rune) bool {
	switch c {
	case '\\', '`', '*', '_', '~', '[', ']', '<', '>', '|', '&':
		return true
	}
	return false
}

// isMattermostLineStartMarkup reports whether c begins a Markdown block
// construct when it is the first non-blank character of a line:
// headings, list items, thematic breaks and setext heading underlines.
func isMattermostLineStartMarkup(c rune) bool {
	switch c {
	case '#', '-', '+', '=':
		return true
	}
	return false
}

// escapeMattermostText escapes s so that Mattermost shows it as literal
// text rather than Markdown, and so that it cannot notify anyone.
//
// Markdown characters are neutralized with a backslash, which
// CommonMark (and so Mattermost's web, mobile and server-side parsers)
// accepts before any ASCII punctuation and removes when rendering, so
// an escaped value looks the same as the original. The characters that
// only mean something at the start of a line are escaped only there,
// as is the '.' or ')' after a leading run of digits (an ordered list
// item), which keeps backslashes out of host names and addresses where
// they would serve no purpose.
//
// Mentions need more than that. Mattermost looks for "@channel",
// "@here", "@all" and "@username" in the text the Markdown parser
// produces, which is after backslash escapes have been removed, so
// "\@channel" still notifies the channel. A zero-width space after
// each '@' does stop it: Mattermost splits words at any character that
// is not a letter, a digit or one of ":.-_@", so the '@' and the name
// fall into separate words, and neither matches a mention.
//
// Escaping '<' and '>' also defeats the Slack compatibility layer,
// which rewrites the literal strings "<!channel>", "<!here>", "<!all>"
// and "<@userid>" into mentions before the text is stored.
func escapeMattermostText(s string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/8)

	lineStart := true
	leadingDigits := false
	for _, c := range s {
		switch {
		case c == '\n' || c == '\r':
			b.WriteRune(c)
			lineStart = true
			leadingDigits = false
			continue
		case lineStart && (c == ' ' || c == '\t'):
			b.WriteRune(c)
			continue
		}

		switch {
		case lineStart && isMattermostLineStartMarkup(c):
			b.WriteByte('\\')
			b.WriteRune(c)
		case lineStart && c >= '0' && c <= '9':
			b.WriteRune(c)
			leadingDigits = true
			lineStart = false
			continue
		case leadingDigits && c >= '0' && c <= '9':
			b.WriteRune(c)
			continue
		case leadingDigits && (c == '.' || c == ')'):
			b.WriteByte('\\')
			b.WriteRune(c)
		case isMattermostInlineMarkup(c):
			b.WriteByte('\\')
			b.WriteRune(c)
		case c == '@':
			b.WriteRune(c)
			b.WriteString(mentionBreaker)
		default:
			b.WriteRune(c)
		}
		lineStart = false
		leadingDigits = false
	}
	return b.String()
}
