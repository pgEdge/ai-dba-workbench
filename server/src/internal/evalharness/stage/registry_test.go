/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package stage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/wbclient"
)

func TestAPIRegistry(t *testing.T) {
	t.Parallel()
	var got createConnectionRequest
	var deleted string
	createResponse := `{"id": 42, "name": "eval-x"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == connectionsPath:
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(createResponse))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, connectionsPath+"/"):
			deleted = strings.TrimPrefix(r.URL.Path, connectionsPath+"/")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	reg := &APIRegistry{Client: wbclient.New(srv.URL, "tok", 5*time.Second)}
	spec := ConnectionSpec{Name: "eval-x", Host: "127.0.0.1", Port: 5432, Database: "eval_x",
		Username: "postgres", Password: "pw", SSLMode: "disable"}
	id, err := reg.Create(ctx, spec)
	if err != nil || id != 42 {
		t.Fatalf("Create = %d, %v", id, err)
	}
	if got.Name != "eval-x" || got.DatabaseName != "eval_x" || got.Password != "pw" ||
		got.SSLMode != "disable" || got.IsShared || got.IsMonitored || got.Description == "" {
		t.Errorf("request body %+v", got)
	}
	if err := reg.Delete(ctx, 42); err != nil || deleted != "42" {
		t.Errorf("Delete: %v, path id %q", err, deleted)
	}

	createResponse = `{}`
	if _, err := reg.Create(ctx, spec); err == nil || !strings.Contains(err.Error(), "did not return a connection id") {
		t.Errorf("Create without id: %v", err)
	}
	reg.Client.BaseURL = srv.URL + "/nowhere"
	if _, err := reg.Create(ctx, spec); err == nil {
		t.Error("Create succeeded against a 404")
	}
}
