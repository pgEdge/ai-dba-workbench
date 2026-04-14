/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const maxBodySize = 65536

var (
	secretDir  string
	serverURL  string
	apiKeyFile string
	tokenFile  string
)

func init() {
	secretDir = envOrDefault("SECRET_DIR", "/etc/pgedge/secret")
	serverURL = envOrDefault("SERVER_URL", "http://server:8080")
	apiKeyFile = filepath.Join(secretDir, "anthropic-api-key")
	tokenFile = filepath.Join(secretDir, "helper-token")
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func readToken() string {
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(data))
}

func apiRequest(method, path string, body any) (json.RawMessage, error) {
	token := readToken()
	url := serverURL + "/api/v1" + path

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return json.RawMessage(respBody), nil
}

func respondJSON(w http.ResponseWriter, code int, data any) {
	body, err := json.Marshal(data)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(body) //nolint:errcheck // Best-effort write to HTTP response
}

func readBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	if r.ContentLength > maxBodySize {
		respondJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": "Request body too large",
		})
		return nil, false
	}

	limited := io.LimitReader(r.Body, maxBodySize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Failed to read request body",
		})
		return nil, false
	}
	if len(data) > maxBodySize {
		respondJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": "Request body too large",
		})
		return nil, false
	}

	var result map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &result); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{
				"error": "Invalid JSON",
			})
			return nil, false
		}
	}
	if result == nil {
		result = make(map[string]any)
	}
	return result, true
}

func writeAPIKey(key string) error {
	if err := os.WriteFile(apiKeyFile, []byte(key), 0600); err != nil {
		return fmt.Errorf("write API key file: %w", err)
	}
	return nil
}

func restartServer() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "restart", "wt-server")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker restart: %w: %s", err, string(out))
	}
	return nil
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	keyConfigured := false
	data, err := os.ReadFile(apiKeyFile)
	if err == nil {
		keyConfigured = len(bytes.TrimSpace(data)) > 0
	}

	demoPresent := false
	raw, err := apiRequest("GET", "/connections", nil)
	if err == nil {
		var conns []map[string]any
		if json.Unmarshal(raw, &conns) == nil {
			for _, c := range conns {
				if name, ok := c["name"].(string); ok && name == "demo-ecommerce" {
					demoPresent = true
					break
				}
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"api_key_configured": keyConfigured,
		"demo_data_present":  demoPresent,
	})
}

func handleSetAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	body, ok := readBody(w, r)
	if !ok {
		return
	}

	key := ""
	if v, exists := body["api_key"]; exists {
		if s, ok := v.(string); ok {
			key = bytes.NewBufferString(s).String()
			key = string(bytes.TrimSpace([]byte(key)))
		}
	}
	if key == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "api_key required",
		})
		return
	}

	if err := writeAPIKey(key); err != nil {
		log.Printf("Error writing API key: %v", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to write API key",
		})
		return
	}

	// Restart the server container so the AI Overview generator
	// picks up the new API key. SIGHUP reloads config but does
	// not restart subsystems that check credentials at boot.
	if err := restartServer(); err != nil {
		log.Printf("Error restarting server: %v", err)
	}

	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func handleAddConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	body, ok := readBody(w, r)
	if !ok {
		return
	}

	// Find and delete demo connection.
	raw, err := apiRequest("GET", "/connections", nil)
	if err == nil {
		var conns []map[string]any
		if json.Unmarshal(raw, &conns) == nil {
			for _, c := range conns {
				if name, ok := c["name"].(string); ok && name == "demo-ecommerce" {
					if id, ok := c["id"]; ok {
						apiRequest("DELETE", fmt.Sprintf("/connections/%v", id), nil) //nolint:errcheck // Best-effort delete
					}
					break
				}
			}
		}
	}

	// Build connection payload.
	name := "my-database"
	if v, ok := body["name"].(string); ok && v != "" {
		name = v
	}
	port := float64(5432)
	if v, ok := body["port"].(float64); ok {
		port = v
	}
	sslMode := "prefer"
	if v, ok := body["ssl_mode"].(string); ok && v != "" {
		sslMode = v
	}

	connPayload := map[string]any{
		"name":          name,
		"host":          body["host"],
		"port":          port,
		"database_name": body["database_name"],
		"username":      body["username"],
		"password":      body["password"],
		"ssl_mode":      sslMode,
		"is_shared":     true,
		"is_monitored":  true,
		"description":   "Connected via walkthrough",
	}

	result, err := apiRequest("POST", "/connections", connPayload)
	if err != nil {
		log.Printf("Error creating connection: %v", err)
		respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to create connection",
		})
		return
	}

	// Write API key if provided.
	if v, ok := body["api_key"].(string); ok {
		key := string(bytes.TrimSpace([]byte(v)))
		if key != "" {
			if err := writeAPIKey(key); err != nil {
				log.Printf("Error writing API key: %v", err)
			} else {
				// Restart the server container so the AI Overview
				// generator picks up the new API key.
				if err := restartServer(); err != nil {
					log.Printf("Error restarting server: %v", err)
				}
			}
		}
	}

	connID := "unknown"
	var resultMap map[string]any
	if json.Unmarshal(result, &resultMap) == nil {
		if id, ok := resultMap["id"]; ok {
			connID = fmt.Sprintf("%v", id)
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"connection_id": connID,
	})
}

func main() {
	port := envOrDefault("PORT", "8090")

	mux := http.NewServeMux()
	mux.HandleFunc("/status", handleStatus)
	mux.HandleFunc("/set-api-key", handleSetAPIKey)
	mux.HandleFunc("/add-connection", handleAddConnection)

	log.Printf("Walkthrough helper listening on port %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
