/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// LogLevel represents the logging verbosity level for database operations
type LogLevel int

const (
	// LogLevelNone disables all database logging
	LogLevelNone LogLevel = iota
	// LogLevelInfo logs basic information (connections, queries, errors)
	LogLevelInfo
	// LogLevelDebug logs detailed information (metadata loading, connection details, query details)
	LogLevelDebug
	// LogLevelTrace logs very detailed information (full queries, row counts, timings)
	LogLevelTrace
)

// Logger handles structured logging for database operations
type Logger struct {
	level  LogLevel
	logger *log.Logger
}

var globalLogger *Logger

func init() {
	// Initialize logger with level from environment variable
	// Default is LogLevelNone (no logging) when unset or set to "none"
	levelStr := strings.ToLower(strings.TrimSpace(os.Getenv("PGEDGE_DB_LOG_LEVEL")))

	var level LogLevel
	switch levelStr {
	case "none", "": // Explicitly handle "none" and empty string
		level = LogLevelNone
	case "info":
		level = LogLevelInfo
	case "debug":
		level = LogLevelDebug
	case "trace":
		level = LogLevelTrace
	default:
		// Invalid value, default to LogLevelNone
		level = LogLevelNone
	}

	globalLogger = &Logger{
		level:  level,
		logger: log.New(os.Stderr, "[DATABASE] ", log.LstdFlags),
	}
}

// SetLogLevel sets the global database log level
func SetLogLevel(level LogLevel) {
	globalLogger.level = level
}

// GetLogLevel returns the current log level
func GetLogLevel() LogLevel {
	return globalLogger.level
}

// Info logs an informational message (connections, basic queries, errors)
func (l *Logger) Info(format string, args ...any) {
	if l.level >= LogLevelInfo {
		l.logger.Printf("[INFO] "+format, args...)
	}
}

// Debug logs a debug message (metadata loading, connection pools, query details)
func (l *Logger) Debug(format string, args ...any) {
	if l.level >= LogLevelDebug {
		l.logger.Printf("[DEBUG] "+format, args...)
	}
}

// Trace logs a trace message (full queries, row counts, detailed timings)
func (l *Logger) Trace(format string, args ...any) {
	if l.level >= LogLevelTrace {
		l.logger.Printf("[TRACE] "+format, args...)
	}
}

// LogConnection logs a database connection attempt
func LogConnection(connStr string, duration time.Duration, err error) {
	// Sanitize connection string to hide password
	sanitized := SanitizeConnStr(connStr)
	if err != nil {
		globalLogger.Info("Connection failed: connection=%s, duration=%s, error=%v",
			sanitized, duration, err)
	} else {
		globalLogger.Info("Connection succeeded: connection=%s, duration=%s",
			sanitized, duration)
	}
}

// LogConnectionDetails logs detailed connection information
func LogConnectionDetails(connStr string, poolConfig map[string]any) {
	sanitized := SanitizeConnStr(connStr)
	configStr := ""
	for k, v := range poolConfig {
		configStr += fmt.Sprintf("%s=%v ", k, v)
	}
	globalLogger.Debug("Connection details: connection=%s, pool_config=%s",
		sanitized, strings.TrimSpace(configStr))
}

// LogMetadataLoad logs metadata loading operation
func LogMetadataLoad(connStr string, tableCount int, duration time.Duration, err error) {
	sanitized := SanitizeConnStr(connStr)
	if err != nil {
		globalLogger.Info("Metadata load failed: connection=%s, duration=%s, error=%v",
			sanitized, duration, err)
	} else {
		globalLogger.Info("Metadata loaded: connection=%s, table_count=%d, duration=%s",
			sanitized, tableCount, duration)
	}
}

// LogMetadataDetails logs detailed metadata loading information
func LogMetadataDetails(connStr string, schemaCount, tableCount, columnCount int) {
	sanitized := SanitizeConnStr(connStr)
	globalLogger.Debug("Metadata details: connection=%s, schema_count=%d, table_count=%d, column_count=%d",
		sanitized, schemaCount, tableCount, columnCount)
}

// connStrSpace lists the characters libpq's conninfo parser treats as
// white space (isspace in the C locale).
const connStrSpace = " \t\n\v\f\r"

// SanitizeConnStr sanitizes a PostgreSQL connection string by replacing the
// password with "***". This should be used when displaying connection strings
// to users or in error messages. It masks the userinfo password of a URI and
// any password or sslpassword parameter, in either the URI query or the
// keyword/value form.
func SanitizeConnStr(connStr string) string {
	if isURIConnStr(connStr) {
		return maskURIQueryPasswords(maskURIUserinfo(connStr))
	}
	return maskKeywordValuePasswords(connStr)
}

// maskURIQueryPasswords replaces the value of every password or
// sslpassword parameter in a URI query string with "***". A parameter
// starts after any '?' or '&' and its key runs to the next '='. pgx, like
// libpq, percent-decodes a query key before matching it, so pass%77ord
// sets the password; the key is therefore decoded the same way before it
// is compared, then trimmed of white space and compared without regard
// to case, which masks every spelling pgx accepts and some it does not.
// Only '&' ends a value, since a literal '&' in a value must be
// percent-encoded, so everything up to it is masked.
func maskURIQueryPasswords(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	last := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '?' && s[i] != '&' {
			continue
		}
		eq := strings.IndexAny(s[i+1:], "=?&")
		if eq < 0 {
			break
		}
		eq += i + 1
		if s[eq] != '=' || !isURIPasswordKey(s[i+1:eq]) {
			continue
		}
		end := strings.IndexByte(s[eq+1:], '&')
		if end < 0 {
			end = len(s)
		} else {
			end += eq + 1
		}
		b.WriteString(s[last : eq+1])
		b.WriteString("***")
		last = end
		i = end - 1
	}
	b.WriteString(s[last:])
	return b.String()
}

// isURIPasswordKey reports whether rawKey, a URI query key as written,
// names the password or sslpassword parameter once percent-decoded.
func isURIPasswordKey(rawKey string) bool {
	key := strings.Trim(percentDecodeLenient(rawKey), connStrSpace)
	return strings.EqualFold(key, "password") || strings.EqualFold(key, "sslpassword")
}

// percentDecodeLenient decodes every valid %XX sequence in s and copies
// anything else, including a malformed sequence, unchanged. pgx refuses
// a malformed sequence, so no key it accepts is decoded differently.
func percentDecodeLenient(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, okHi := hexValue(s[i+1])
			lo, okLo := hexValue(s[i+2])
			if okHi && okLo {
				b.WriteByte(hi<<4 | lo)
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// hexValue returns the value of the hexadecimal digit c.
func hexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// isURIConnStr reports whether connStr is in the URI form: a scheme
// followed by "://" before any '=' or white space, which a keyword/value
// string always has first.
func isURIConnStr(connStr string) bool {
	s := strings.TrimLeft(connStr, connStrSpace)
	i := strings.Index(s, "://")
	return i > 0 && !strings.ContainsAny(s[:i], "="+connStrSpace)
}

// maskKeywordValuePasswords replaces the value of every password and
// sslpassword keyword in a keyword/value connection string with "***",
// copying everything else as it is. It reads the string as libpq's
// conninfo_parse does: a keyword ends at '=' or white space, white space
// may surround the '=', and a value is either quoted with ' (where \'
// and \\ are escapes) or runs to the next white space that a backslash
// does not escape, so a value such as ab&cd or correct\ horse is masked
// whole. An unterminated quoted value runs to the end of the string. A
// word with no '=' after it, which libpq would refuse, is copied and
// scanning carries on after it.
func maskKeywordValuePasswords(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		start := i
		i = skipConnStrSpace(s, i)
		kwStart := i
		for i < len(s) && s[i] != '=' && !isConnStrSpace(s[i]) {
			i++
		}
		keyword := s[kwStart:i]
		i = skipConnStrSpace(s, i)
		if i >= len(s) || s[i] != '=' {
			b.WriteString(s[start:i])
			continue
		}
		i = skipConnStrSpace(s, i+1)
		valStart := i
		i = scanConnStrValue(s, i)
		b.WriteString(s[start:valStart])
		if strings.EqualFold(keyword, "password") || strings.EqualFold(keyword, "sslpassword") {
			b.WriteString("***")
		} else {
			b.WriteString(s[valStart:i])
		}
	}
	return b.String()
}

// isConnStrSpace reports whether c is white space to libpq's parser.
func isConnStrSpace(c byte) bool {
	return strings.IndexByte(connStrSpace, c) >= 0
}

// skipConnStrSpace returns the index of the first byte at or after i
// that is not white space.
func skipConnStrSpace(s string, i int) int {
	for i < len(s) && isConnStrSpace(s[i]) {
		i++
	}
	return i
}

// scanConnStrValue returns the index just past the keyword/value
// connection string value that starts at i, following libpq's quoting
// and backslash escaping.
func scanConnStrValue(s string, i int) int {
	if i < len(s) && s[i] == '\'' {
		for i++; i < len(s); i++ {
			switch s[i] {
			case '\\':
				i++ // the escaped byte, skipped by the loop's i++
			case '\'':
				return i + 1
			}
		}
		return len(s)
	}
	for i < len(s) && !isConnStrSpace(s[i]) {
		if s[i] == '\\' {
			i++
		}
		i++
	}
	return min(i, len(s))
}

// maskURIUserinfo replaces the password in a URI's userinfo with "***",
// returning any other string unchanged.
func maskURIUserinfo(connStr string) string {
	// Handle postgres://user:password@host:port/database?params format
	// Find the scheme (postgres://)
	schemeIdx := strings.Index(connStr, "://")
	if schemeIdx == -1 {
		// No scheme found, return as is
		return connStr
	}

	scheme := connStr[:schemeIdx+3] // Include "://"
	rest := connStr[schemeIdx+3:]

	// Find the @ that separates credentials from host
	// We need to find the LAST @ before any / or ? to handle passwords with @
	hostSepIdx := -1
	for i := len(rest) - 1; i >= 0; i-- {
		if rest[i] == '@' {
			// Check if this @ is before any / or ?
			slashIdx := strings.Index(rest[i:], "/")
			questionIdx := strings.Index(rest[i:], "?")
			if (slashIdx == -1 || slashIdx > 0) && (questionIdx == -1 || questionIdx > 0) {
				hostSepIdx = i
				break
			}
		}
	}

	if hostSepIdx == -1 {
		// No @ found, no credentials
		return connStr
	}

	credentials := rest[:hostSepIdx]
	hostAndRest := rest[hostSepIdx+1:]

	// Find the : that separates user from password
	colonIdx := strings.Index(credentials, ":")
	if colonIdx == -1 {
		// No password, return as is
		return connStr
	}

	user := credentials[:colonIdx]
	// Password is everything after the first :
	return scheme + user + ":***@" + hostAndRest
}
