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
	"net"
	"net/http"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
)

// deliveryPolicy is the address policy every notification delivery is
// held to. By default it refuses the internal ranges as well as the
// always-blocked ones (hostvalidation.CheckNotInternal), the rule the
// generic webhook was checked against before the check moved to the
// dial; notifications.allow_internal_networks relaxes it to the
// always-blocked floor (unspecified, link-local and cloud metadata
// addresses), which nothing may reach.
func deliveryPolicy(allowInternal bool) hostvalidation.AddrPolicy {
	if allowInternal {
		return hostvalidation.CheckAlwaysBlocked
	}
	return hostvalidation.CheckNotInternal
}

// deliveryDialer returns the dial function every notification
// connection is opened with, HTTP and SMTP alike: it resolves the host
// itself and connects only to addresses policy permits, so a name that
// passed a check when the channel was saved or tested cannot be
// re-pointed at an internal or metadata address before delivery (DNS
// rebinding). A nil resolve is the system resolver; tests pass their
// own.
func deliveryDialer(policy hostvalidation.AddrPolicy, resolve hostvalidation.LookupFunc) hostvalidation.DialContextFunc {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return hostvalidation.GuardedDialContext(policy, resolve, dialer.DialContext)
}

// newDeliveryHTTPClient builds the one HTTP client the webhook, Slack,
// Mattermost and Telegram notifiers share, connecting through dial.
func newDeliveryHTTPClient(cfg *config.NotificationsConfig, dial hostvalidation.DialContextFunc) *http.Client {
	client := &http.Client{
		Timeout: time.Duration(cfg.HTTPTimeoutSeconds) * time.Second,
		Transport: &http.Transport{
			// No proxy, deliberately, and not even one named in
			// HTTP_PROXY or HTTPS_PROXY. Through a proxy the only
			// address connected to is the proxy's, so the guard would check
			// that and never the destination, and the proxy would be
			// free to reach anything. Every delivery therefore goes
			// direct, as it always has (a nil Proxy is what an
			// http.Transport literal has always meant here).
			Proxy:              nil,
			DialContext:        dial,
			MaxIdleConns:       cfg.HTTPMaxIdleConns,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: true,
		},
		// Never follow a redirect. This is set on the one client every
		// notifier shares, because each channel type it serves has its
		// own reason to need it:
		//
		//   - Telegram: net/http copies the previous request's full
		//     URL into the Referer header of the redirected request,
		//     and that URL is
		//     https://api.telegram.org/bot<token>/sendMessage. A 3xx
		//     would hand the live bot token to whatever host the
		//     Location header names.
		//   - Slack and Mattermost: the whole webhook URL is the
		//     secret, and it would travel in Referer in exactly the
		//     same way.
		//   - The generic webhook: a Location header could name any
		//     host the operator never configured.
		//
		// A redirect target would still be connected through dial, and so
		// refused if its address is, but not following one at all keeps
		// the credential in the URL away from it too. The cost is that
		// a provider answering a delivery with a 3xx fails that
		// delivery instead of following it. That is the intended trade,
		// and it makes delivery behave like the test-send path in
		// server/src/internal/api/webhook_test_sender.go, which has
		// always refused redirects.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if client.Timeout == 0 {
		client.Timeout = 30 * time.Second
	}
	return client
}
