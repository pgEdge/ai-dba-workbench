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

import "context"

// HostAddrParam is the libpq keyword naming the address to connect to
// whilst host stays the name used for TLS verification and .pgpass.
const HostAddrParam = "hostaddr"

// TakeHostAddr gives pgx libpq's hostaddr behavior. pgx has no hostaddr
// keyword, so it parses one into params (pgconn.Config.RuntimeParams)
// and would send it to the server as a startup parameter, which
// PostgreSQL refuses. TakeHostAddr removes the key from params and
// returns a LookupFunc that resolves every host to that address, for
// pgconn.Config.LookupFunc. pgx then dials the address while the host
// stays its TLS ServerName, so sslmode=verify-full checks the
// certificate against the host name, as libpq does.
//
// ok is false, and lookup nil, when params has no hostaddr. When the
// value is not a literal IP address the key is still removed, lookup is
// nil and err is ErrHostAddrNotIP, so a caller can fail closed.
func TakeHostAddr(params map[string]string) (lookup LookupFunc, ok bool, err error) {
	raw, ok := params[HostAddrParam]
	if !ok {
		return nil, false, nil
	}
	delete(params, HostAddrParam)
	ip, err := ParseHostAddr(raw)
	if err != nil {
		return nil, true, err
	}
	addr := ip.String()
	return func(context.Context, string) ([]string, error) {
		return []string{addr}, nil
	}, true, nil
}
