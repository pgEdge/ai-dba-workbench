/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
)

// testSendTimeout bounds each test-send connection attempt and, for the
// HTTP channels, the whole request.
const testSendTimeout = 30 * time.Second

// testSendPolicy is the address policy every "send test notification"
// connection is held to: the always-blocked floor (unspecified,
// link-local and cloud metadata addresses), which the alerter refuses
// whatever its settings, and then the connection_security rules v
// applies to a channel's host when it is tested (CheckDialAddr). The
// floor comes first because an allowed_hosts name permits every address
// it resolves to, and allow_internal_networks permits link-local ones,
// and neither should let a test send reach an address no real delivery
// ever could.
func testSendPolicy(v *HostValidator) hostvalidation.AddrPolicy {
	return func(host string, ip net.IP) error {
		if err := hostvalidation.CheckAlwaysBlocked(host, ip); err != nil {
			return err
		}
		return v.CheckDialAddr(host, ip)
	}
}

// notificationTestSender opens every connection the "send test
// notification" endpoint makes, HTTP and SMTP alike, through one
// guarded dial function: it resolves the channel's host itself and
// connects only to the addresses the policy permits, so a name that
// passed hostValidator.ValidateHost a moment earlier cannot be
// re-pointed at an internal or metadata address before the connection
// is made (DNS rebinding).
type notificationTestSender struct {
	dial   hostvalidation.DialContextFunc
	client *http.Client
	// rootCAs verifies the SMTP server's certificate; nil is the system
	// pool. Only tests set it.
	rootCAs *x509.CertPool
}

// newNotificationTestSender builds a sender whose connections are held
// to policy. A nil resolve is the system resolver; tests pass their own.
func newNotificationTestSender(policy hostvalidation.AddrPolicy, resolve hostvalidation.LookupFunc) *notificationTestSender {
	dialer := &net.Dialer{Timeout: testSendTimeout, KeepAlive: testSendTimeout}
	dial := hostvalidation.GuardedDialContext(policy, resolve, dialer.DialContext)
	return &notificationTestSender{
		dial: dial,
		client: &http.Client{
			Timeout: testSendTimeout,
			Transport: &http.Transport{
				// No proxy, deliberately, and not even one named in
				// HTTP_PROXY or HTTPS_PROXY: through a proxy the only
				// address connected to is the proxy's, so the guard
				// would check that and never the channel's, and the
				// proxy would be free to reach anything. The alerter's
				// delivery client is built the same way.
				Proxy:               nil,
				DialContext:         dial,
				TLSHandshakeTimeout: testSendTimeout,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  true,
			},
			// Never follow a redirect. A Location header could name any
			// host, and net/http would copy the full URL, which for
			// Slack, Mattermost and Telegram is the credential, into the
			// Referer header of the redirected request. A redirect
			// target would still be reached through the guard, but not
			// following one keeps the credential away from it too.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// isRefusedTarget reports whether err means the guard refused every
// address the channel's host resolved to. The handler answers such a
// refusal as it does a failed host check, without naming the host or
// any address.
func isRefusedTarget(err error) bool {
	return errors.Is(err, hostvalidation.ErrNoPermittedAddress)
}
