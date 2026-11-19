/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package mcp

import (
	"context"
	"testing"
)

func TestCallMetaRoundTrip(t *testing.T) {
	if got := CallMetaFromContext(context.Background()); got != (CallMeta{}) {
		t.Errorf("empty context returned %+v", got)
	}
	want := CallMeta{ConversationID: "c", Model: "m", Provider: "p"}
	if got := CallMetaFromContext(WithCallMeta(context.Background(), want)); got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}
