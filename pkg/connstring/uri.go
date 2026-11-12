/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package connstring

import (
	"net/url"
	"strings"
)

// EncodeURIQuery encodes connection URI query parameters the way libpq,
// and so pgx since v5.12, decodes them. url.Values.Encode follows
// application/x-www-form-urlencoded rules and writes a space as '+', but
// libpq reads '+' in a URI as a literal plus, so a value such as
// "pgEdge AI DBA Workbench" would reach the server as
// "pgEdge+AI+DBA+Workbench". Encode escapes a literal '+' as "%2B", so
// every '+' left in its output stands for a space and can be rewritten
// as "%20" without ambiguity.
func EncodeURIQuery(query url.Values) string {
	return strings.ReplaceAll(query.Encode(), "+", "%20")
}
