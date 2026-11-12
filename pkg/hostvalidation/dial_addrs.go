/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package hostvalidation

import (
	"context"
	"net"
	"time"
)

// fallbackDelay is how long the addresses of the first family are tried
// alone before those of the other family are tried alongside them. It is
// net.Dialer's default FallbackDelay, the value RFC 8305 recommends, and
// a variable only so that tests can shorten it.
var fallbackDelay = 300 * time.Millisecond

// minAttemptTimeout is the least time one address is given when the
// context's deadline is shared among several, as net.Dialer does. It
// is a variable only so that tests can shorten it.
var minAttemptTimeout = 2 * time.Second

// dialResult is the outcome of one dialSerial run within dialAddrs.
type dialResult struct {
	conn    net.Conn
	err     error
	primary bool
}

// dialAddrs connects to port at one of ips, permitted IP literals in the
// order the resolver returned them, in the way net.Dialer connects to a
// host name ("Happy Eyeballs", RFC 8305). GuardedDialContext cannot
// simply hand net.Dialer the host name and check each address in its
// Control hook, because the dial would then resolve the name through
// net.Dialer.Resolver rather than the injected LookupFunc, and the
// LookupFunc is what lets the callers' tests point a name at several
// addresses, some refused and some silent.
//
// The addresses are split by family, the first address's family being
// the primary one. With one family, they are tried in turn. With two,
// the primary family is tried first and the other alongside it once
// fallbackDelay has passed or the primary family has failed, whichever
// comes first. The first connection made wins; the attempt still
// running is stopped, and a connection it makes anyway is closed. When
// every address fails, the primary family's error is returned, as
// net.Dialer does. An empty ips is ErrNoPermittedAddress.
func dialAddrs(ctx context.Context, dial DialContextFunc, network, port string, ips []string) (net.Conn, error) {
	if len(ips) == 0 {
		return nil, ErrNoPermittedAddress
	}
	primaries, fallbacks := splitByFamily(ips)
	if len(fallbacks) == 0 {
		return dialSerial(ctx, dial, network, port, primaries)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Buffered for both attempts, so neither goroutine blocks on its
	// send once this function has returned.
	results := make(chan dialResult, 2)
	start := func(primary bool, list []string) {
		go func() {
			conn, err := dialSerial(ctx, dial, network, port, list)
			results <- dialResult{conn: conn, err: err, primary: primary}
		}()
	}

	start(true, primaries)
	running := 1
	timer := time.NewTimer(fallbackDelay)
	defer timer.Stop()
	fallbackStarted := false
	startFallback := func() {
		if fallbackStarted {
			return
		}
		fallbackStarted = true
		timer.Stop()
		running++
		start(false, fallbacks)
	}

	var primaryErr error
	for {
		select {
		case <-timer.C:
			// The timer can fire as the context ends; a fallback
			// started then could only fail.
			if ctx.Err() == nil {
				startFallback()
			}
		case res := <-results:
			running--
			if res.err == nil {
				if running > 0 {
					go closeLateConns(results, running)
				}
				return res.conn, nil
			}
			if res.primary {
				primaryErr = res.err
			}
			if ctx.Err() == nil {
				startFallback()
			}
			// The primary attempt always reports before running can
			// reach zero, so primaryErr is set here.
			if running == 0 {
				return nil, primaryErr
			}
		}
	}
}

// closeLateConns receives the n results still to come from dialAddrs's
// attempts after it has returned another connection, and closes any
// connection among them.
func closeLateConns(results <-chan dialResult, n int) {
	for i := 0; i < n; i++ {
		if r := <-results; r.conn != nil {
			_ = r.conn.Close()
		}
	}
}

// splitByFamily splits ips, keeping their order, into those of the
// first address's family and those of the other.
func splitByFamily(ips []string) (primaries, fallbacks []string) {
	if len(ips) == 0 {
		return nil, nil
	}
	firstIsV4 := isIPv4Literal(ips[0])
	for _, ip := range ips {
		if isIPv4Literal(ip) == firstIsV4 {
			primaries = append(primaries, ip)
		} else {
			fallbacks = append(fallbacks, ip)
		}
	}
	return primaries, fallbacks
}

// isIPv4Literal reports whether s is an IPv4 address, or an IPv4-mapped
// IPv6 one, which a dial treats as IPv4.
func isIPv4Literal(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil
}

// dialSerial tries ips in turn until one connects. When ctx has a
// deadline, each attempt gets an equal share of the time left (but at
// least minAttemptTimeout, or all of it if less is left), as net.Dialer
// does, so one silent address cannot use up the time the others need.
// It stops early once ctx is done, and returns the last attempt's error;
// when no address is tried, the error is ErrNoPermittedAddress.
//
// Each address is dialled in the canonical text form of the IP the
// policy checked, not as the resolver wrote it, so the dialler cannot
// read the text differently from net.ParseIP. An entry that does not
// parse is skipped; GuardedDialContext never passes one.
func dialSerial(ctx context.Context, dial DialContextFunc, network, port string, ips []string) (net.Conn, error) {
	lastErr := ErrNoPermittedAddress
	for i, ipText := range ips {
		ip := net.ParseIP(ipText)
		if ip == nil {
			continue
		}
		attemptCtx, cancel := attemptContext(ctx, len(ips)-i)
		conn, err := dial(attemptCtx, network, net.JoinHostPort(ip.String(), port))
		cancel()
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

// attemptContext returns the context for one of remaining attempts
// sharing ctx's deadline.
func attemptContext(ctx context.Context, remaining int) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok || remaining <= 1 {
		return ctx, func() {}
	}
	left := time.Until(deadline)
	timeout := left / time.Duration(remaining)
	if timeout < minAttemptTimeout {
		timeout = min(minAttemptTimeout, left)
	}
	return context.WithTimeout(ctx, timeout)
}
