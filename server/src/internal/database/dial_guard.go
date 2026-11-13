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
	"context"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
)

// DialGuard restricts the addresses a pgx connection may dial. It is
// applied to the parsed configuration of every connection to a
// monitored database, and never to the server's own datastore, which
// may legitimately be on loopback or a private network.
type DialGuard func(cfg *pgconn.Config)

// SetDialPolicy sets the policy that GuardMonitoredConnConfig applies to
// every address a monitored connection resolves to when it connects.
// The server sets it once at start-up from connection_security, so the
// address actually connected to passes the same allow-list, block-list and
// allow_internal_networks rules as the host did when it was saved. A nil
// policy selects hostvalidation.CheckAlwaysBlocked.
func (d *Datastore) SetDialPolicy(policy hostvalidation.AddrPolicy) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dialPolicy = policy
}

// dialPolicyOrFloor returns the policy set by SetDialPolicy, or
// hostvalidation.CheckAlwaysBlocked when there is none or d is nil.
func (d *Datastore) dialPolicyOrFloor() hostvalidation.AddrPolicy {
	if d == nil {
		return hostvalidation.CheckAlwaysBlocked
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.dialPolicy == nil {
		return hostvalidation.CheckAlwaysBlocked
	}
	return d.dialPolicy
}

// GuardMonitoredConnConfig wraps cfg's LookupFunc so that pgx dials only
// the resolved addresses the dial policy permits. Save-time validation
// checks the addresses a host name resolved to then; this checks the
// ones it resolves to now, which is what defeats DNS rebinding. It is a
// DialGuard, and is safe to call on a nil Datastore.
func (d *Datastore) GuardMonitoredConnConfig(cfg *pgconn.Config) {
	GuardConnConfig(cfg, d.dialPolicyOrFloor())
}

// GuardConnConfig wraps cfg's LookupFunc with hostvalidation.GuardedLookup
// under policy (CheckAlwaysBlocked when nil).
//
// It first applies any hostaddr BuildConnectionString put in the
// connection string (hostvalidation.TakeHostAddr), so that pgx dials
// that address but verifies TLS against the host name, and never sends
// hostaddr to the server as a startup parameter. Every monitored
// connection string passes through here, which is why the hostaddr is
// handled here rather than at each place one is parsed. A hostaddr that
// is not an IP address fails every connection attempt rather than
// falling back to resolving the host.
func GuardConnConfig(cfg *pgconn.Config, policy hostvalidation.AddrPolicy) {
	if lookup, ok, err := hostvalidation.TakeHostAddr(cfg.RuntimeParams); ok {
		if err != nil {
			lookup = func(context.Context, string) ([]string, error) {
				return nil, err
			}
		}
		cfg.LookupFunc = lookup
	}
	cfg.LookupFunc = hostvalidation.GuardedLookup(policy, cfg.LookupFunc)
}
