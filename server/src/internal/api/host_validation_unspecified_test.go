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

import "testing"

// TestHostValidator_UnspecifiedAndNAT64 checks that the IPv6
// unspecified address, which a dialler turns into localhost, and the
// IPv6 forms that embed an IPv4 address are internal addresses: refused
// by default, permitted by allow_internal_networks or an allow-list
// entry, and refused by a block-list entry whatever else is set.
func TestHostValidator_UnspecifiedAndNAT64(t *testing.T) {
	addrs := []string{"::", "::ffff:0.0.0.0", "64:ff9b::a9fe:a9fe",
		"2002:a9fe:a9fe::1", "fec0::1"}

	strict := NewHostValidator(false, nil, nil)
	for _, a := range addrs {
		if err := strict.ValidateHost(a); err == nil {
			t.Errorf("ValidateHost(%q) = nil, want an error", a)
		}
		if err := strict.ValidateHostAddr(a); err == nil {
			t.Errorf("ValidateHostAddr(%q) = nil, want an error", a)
		}
		if err := strict.ValidateHostAddr("[" + a + "]"); err == nil {
			t.Errorf("ValidateHostAddr(%q) = nil, want an error", "["+a+"]")
		}
	}
	// A bracketed host is connection-string structure, so ValidateHost
	// refuses it on syntax whatever the address inside.
	if err := strict.ValidateHost("[::]"); err == nil {
		t.Error("ValidateHost(\"[::]\") = nil, want an error")
	}

	permissive := NewHostValidator(true, nil, nil)
	for _, a := range addrs {
		if err := permissive.ValidateHost(a); err != nil {
			t.Errorf("allow_internal_networks: ValidateHost(%q) = %v, want nil", a, err)
		}
	}

	allowed := NewHostValidator(false, []string{"::/128", "64:ff9b::/96"}, nil)
	for _, a := range []string{"::", "64:ff9b::a9fe:a9fe"} {
		if err := allowed.ValidateHostAddr(a); err != nil {
			t.Errorf("allowed_hosts: ValidateHostAddr(%q) = %v, want nil", a, err)
		}
	}
	if err := allowed.ValidateHost("::ffff:0.0.0.0"); err == nil {
		t.Error("allowed_hosts: ::ffff:0.0.0.0 is outside the allow-list and must stay refused")
	}

	blocked := NewHostValidator(true, nil, []string{"::"})
	if err := blocked.ValidateHost("::"); err == nil {
		t.Error("blocked_hosts: ValidateHost(\"::\") = nil, want an error")
	}
}
