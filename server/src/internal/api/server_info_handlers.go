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
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/chat"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/llmproxy"
)

// aiAnalysisCacheTTL is how long AI database analysis is cached.
const aiAnalysisCacheTTL = 5 * time.Minute

// llmAnalysisMaxTokens caps the AI analysis response length.
const llmAnalysisMaxTokens = 512

// llmAnalysisTemperature controls response creativity for analysis.
const llmAnalysisTemperature = 0.3

// keySettings lists the PostgreSQL configuration parameters included
// in the server info response.
var keySettings = []string{
	"shared_buffers",
	"work_mem",
	"effective_cache_size",
	"maintenance_work_mem",
	"max_worker_processes",
	"wal_level",
	"archive_mode",
	"max_wal_size",
	"min_wal_size",
	"checkpoint_completion_target",
	"random_page_cost",
	"effective_io_concurrency",
	"max_parallel_workers",
	"max_parallel_workers_per_gather",
	"autovacuum",
	"log_min_duration_statement",
}

// ServerInfoHandler handles GET /api/v1/server-info/{connection_id} requests.
type ServerInfoHandler struct {
	datastore   *database.Datastore
	authStore   *auth.AuthStore
	rbacChecker *auth.RBACChecker
	llmConfig   *llmproxy.Config

	cacheMu sync.RWMutex
	cache   map[int]*aiCacheEntry
}

// aiCacheEntry holds a cached AI analysis for a connection.
type aiCacheEntry struct {
	analysis    map[string]string
	generatedAt time.Time
	expiresAt   time.Time
}

// ServerInfoResponse is the JSON response for the server info endpoint.
type ServerInfoResponse struct {
	ConnectionID int             `json:"connection_id"`
	CollectedAt  *time.Time      `json:"collected_at"`
	System       *SystemInfo     `json:"system"`
	PostgreSQL   *PostgreSQLInfo `json:"postgresql"`
	Databases    []DatabaseInfo  `json:"databases"`
	Extensions   []ExtensionInfo `json:"extensions"`
	KeySettings  []SettingInfo   `json:"key_settings"`
	AIAnalysis   *AIAnalysisInfo `json:"ai_analysis"`
}

// SystemInfo holds operating system and hardware information.
type SystemInfo struct {
	OSName           *string    `json:"os_name"`
	OSVersion        *string    `json:"os_version"`
	Architecture     *string    `json:"architecture"`
	Hostname         *string    `json:"hostname"`
	CPUModel         *string    `json:"cpu_model"`
	CPUCores         *int       `json:"cpu_cores"`
	CPULogical       *int       `json:"cpu_logical"`
	CPUClockSpeed    *int64     `json:"cpu_clock_speed"`
	MemoryTotalBytes *int64     `json:"memory_total_bytes"`
	MemoryUsedBytes  *int64     `json:"memory_used_bytes"`
	MemoryFreeBytes  *int64     `json:"memory_free_bytes"`
	SwapTotalBytes   *int64     `json:"swap_total_bytes"`
	SwapUsedBytes    *int64     `json:"swap_used_bytes"`
	Disks            []DiskInfo `json:"disks"`
}

// DiskInfo holds information about a single disk mount point.
type DiskInfo struct {
	MountPoint     string `json:"mount_point"`
	FilesystemType string `json:"filesystem_type"`
	TotalBytes     int64  `json:"total_bytes"`
	UsedBytes      int64  `json:"used_bytes"`
	FreeBytes      int64  `json:"free_bytes"`
}

// PostgreSQLInfo holds PostgreSQL server configuration.
type PostgreSQLInfo struct {
	Version             *string `json:"version"`
	ClusterName         *string `json:"cluster_name"`
	DataDirectory       *string `json:"data_directory"`
	MaxConnections      *int    `json:"max_connections"`
	MaxWalSenders       *int    `json:"max_wal_senders"`
	MaxReplicationSlots *int    `json:"max_replication_slots"`
}

// DatabaseInfo holds information about a single database.
type DatabaseInfo struct {
	Name            string   `json:"name"`
	SizeBytes       *int64   `json:"size_bytes"`
	Encoding        *string  `json:"encoding"`
	ConnectionLimit *int     `json:"connection_limit"`
	Extensions      []string `json:"extensions"`
}

// ExtensionInfo holds information about a single installed extension.
type ExtensionInfo struct {
	Name     string  `json:"name"`
	Version  *string `json:"version"`
	Schema   *string `json:"schema"`
	Database string  `json:"database"`
}

// SettingInfo holds information about a single PostgreSQL setting.
type SettingInfo struct {
	Name     string  `json:"name"`
	Setting  *string `json:"setting"`
	Unit     *string `json:"unit,omitempty"`
	Category *string `json:"category"`
}

// AIAnalysisInfo holds the AI-generated analysis of databases.
type AIAnalysisInfo struct {
	Databases   map[string]string `json:"databases"`
	GeneratedAt time.Time         `json:"generated_at"`
}

// NewServerInfoHandler creates a new server info handler.
func NewServerInfoHandler(
	datastore *database.Datastore,
	authStore *auth.AuthStore,
	rbacChecker *auth.RBACChecker,
	llmConfig *llmproxy.Config,
) *ServerInfoHandler {
	return &ServerInfoHandler{
		datastore:   datastore,
		authStore:   authStore,
		rbacChecker: rbacChecker,
		llmConfig:   llmConfig,
		cache:       make(map[int]*aiCacheEntry),
	}
}

// RegisterRoutes registers the server info endpoint on the mux.
func (h *ServerInfoHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		notConfigured := HandleNotConfigured("Server info")
		mux.HandleFunc("/api/v1/server-info/", authWrapper(notConfigured))
		return
	}

	mux.HandleFunc("/api/v1/server-info/", authWrapper(h.handleServerInfoRouting))
}

// handleServerInfoRouting dispatches server info requests based on the
// URL path suffix.
func (h *ServerInfoHandler) handleServerInfoRouting(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/ai-analysis") {
		h.handleServerInfoAI(w, r)
		return
	}
	h.handleServerInfo(w, r)
}

// handleServerInfo handles GET /api/v1/server-info/{connection_id}
func (h *ServerInfoHandler) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	if !RequireGET(w, r) {
		return
	}

	// Parse connection ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/server-info/")
	if path == "" {
		RespondError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}

	connectionID, err := strconv.Atoi(path)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid connection ID")
		return
	}

	// Check RBAC access to this connection
	canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), connectionID)
	if !canAccess {
		RespondError(w, http.StatusForbidden,
			"Permission denied: you do not have access to this connection")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	pool := h.datastore.GetPool()

	response := ServerInfoResponse{
		ConnectionID: connectionID,
		System:       &SystemInfo{Disks: []DiskInfo{}},
		PostgreSQL:   &PostgreSQLInfo{},
		Databases:    []DatabaseInfo{},
		Extensions:   []ExtensionInfo{},
		KeySettings:  []SettingInfo{},
	}

	// Query OS info
	var collectedAt *time.Time
	h.queryOSInfo(ctx, pool, connectionID, response.System, &collectedAt)

	// Query CPU info
	h.queryCPUInfo(ctx, pool, connectionID, response.System)

	// Query memory info
	h.queryMemoryInfo(ctx, pool, connectionID, response.System)

	// Query disk info
	h.queryDiskInfo(ctx, pool, connectionID, response.System)

	// Query PostgreSQL server info
	h.queryServerInfo(ctx, pool, connectionID, response.PostgreSQL)

	// Query databases
	response.Databases = h.queryDatabases(ctx, pool, connectionID)

	// Query extensions
	response.Extensions = h.queryExtensions(ctx, pool, connectionID)

	// Attach extension names to their respective databases
	h.attachExtensionsToDatabases(response.Extensions, response.Databases)

	// Query key settings
	response.KeySettings = h.queryKeySettings(ctx, pool, connectionID)

	// Set the collected_at timestamp
	response.CollectedAt = collectedAt

	RespondJSON(w, http.StatusOK, response)
}

// handleServerInfoAI handles GET /api/v1/server-info/{id}/ai-analysis
func (h *ServerInfoHandler) handleServerInfoAI(w http.ResponseWriter, r *http.Request) {
	if !RequireGET(w, r) {
		return
	}

	// Parse connection ID from URL path: strip prefix, take part before /ai-analysis
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/server-info/")
	path = strings.TrimSuffix(path, "/ai-analysis")
	if path == "" {
		RespondError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}

	connectionID, err := strconv.Atoi(path)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid connection ID")
		return
	}

	// Check RBAC access to this connection
	canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), connectionID)
	if !canAccess {
		RespondError(w, http.StatusForbidden,
			"Permission denied: you do not have access to this connection")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	pool := h.datastore.GetPool()

	// Query databases and extensions for AI analysis
	databases := h.queryDatabases(ctx, pool, connectionID)
	extensions := h.queryExtensions(ctx, pool, connectionID)
	h.attachExtensionsToDatabases(extensions, databases)

	// Generate AI analysis
	analysis := h.getAIAnalysis(ctx, connectionID, databases, extensions)
	if analysis == nil {
		RespondJSON(w, http.StatusOK, nil)
		return
	}

	RespondJSON(w, http.StatusOK, analysis)
}

// queryOSInfo queries the latest OS information for the connection.
func (h *ServerInfoHandler) queryOSInfo(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
	sys *SystemInfo,
	collectedAt **time.Time,
) {
	var ca time.Time
	err := pool.QueryRow(ctx, `
        SELECT name, version, architecture, host_name, collected_at
        FROM metrics.pg_sys_os_info
        WHERE connection_id = $1
        ORDER BY collected_at DESC
        LIMIT 1
    `, connectionID).Scan(
		&sys.OSName, &sys.OSVersion, &sys.Architecture,
		&sys.Hostname, &ca,
	)
	if err != nil {
		log.Printf("[DEBUG] No OS info for connection %d: %v",
			connectionID, err)
		return
	}
	*collectedAt = &ca
}

// queryCPUInfo queries the latest CPU information for the connection.
func (h *ServerInfoHandler) queryCPUInfo(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
	sys *SystemInfo,
) {
	err := pool.QueryRow(ctx, `
        SELECT model_name, no_of_cores, logical_processor, clock_speed_hz
        FROM metrics.pg_sys_cpu_info
        WHERE connection_id = $1
        ORDER BY collected_at DESC
        LIMIT 1
    `, connectionID).Scan(
		&sys.CPUModel, &sys.CPUCores, &sys.CPULogical, &sys.CPUClockSpeed,
	)
	if err != nil {
		log.Printf("[DEBUG] No CPU info for connection %d: %v",
			connectionID, err)
	}
}

// queryMemoryInfo queries the latest memory information for the connection.
func (h *ServerInfoHandler) queryMemoryInfo(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
	sys *SystemInfo,
) {
	err := pool.QueryRow(ctx, `
        SELECT total_memory, used_memory, free_memory,
               swap_total, swap_used
        FROM metrics.pg_sys_memory_info
        WHERE connection_id = $1
        ORDER BY collected_at DESC
        LIMIT 1
    `, connectionID).Scan(
		&sys.MemoryTotalBytes, &sys.MemoryUsedBytes, &sys.MemoryFreeBytes,
		&sys.SwapTotalBytes, &sys.SwapUsedBytes,
	)
	if err != nil {
		log.Printf("[DEBUG] No memory info for connection %d: %v",
			connectionID, err)
	}
}

// queryDiskInfo queries the latest disk information for the connection.
func (h *ServerInfoHandler) queryDiskInfo(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
	sys *SystemInfo,
) {
	rows, err := pool.Query(ctx, `
        SELECT mount_point, file_system_type,
               total_space, used_space, free_space
        FROM metrics.pg_sys_disk_info
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_sys_disk_info
              WHERE connection_id = $1
          )
        ORDER BY mount_point
    `, connectionID)
	if err != nil {
		log.Printf("[DEBUG] No disk info for connection %d: %v",
			connectionID, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var d DiskInfo
		var fsType *string
		if err := rows.Scan(
			&d.MountPoint, &fsType,
			&d.TotalBytes, &d.UsedBytes, &d.FreeBytes,
		); err != nil {
			log.Printf("[DEBUG] Error scanning disk info: %v", err)
			continue
		}
		if fsType != nil {
			d.FilesystemType = *fsType
		}
		sys.Disks = append(sys.Disks, d)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating disk info: %v", err)
	}
}

// queryServerInfo queries the latest PostgreSQL server information.
func (h *ServerInfoHandler) queryServerInfo(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
	pg *PostgreSQLInfo,
) {
	err := pool.QueryRow(ctx, `
        SELECT server_version, cluster_name, data_directory,
               max_connections, max_wal_senders, max_replication_slots
        FROM metrics.pg_server_info
        WHERE connection_id = $1
        ORDER BY collected_at DESC
        LIMIT 1
    `, connectionID).Scan(
		&pg.Version, &pg.ClusterName, &pg.DataDirectory,
		&pg.MaxConnections, &pg.MaxWalSenders, &pg.MaxReplicationSlots,
	)
	if err != nil {
		log.Printf("[DEBUG] No server info for connection %d: %v",
			connectionID, err)
	}
}

// queryDatabases queries the latest database information for the connection.
func (h *ServerInfoHandler) queryDatabases(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
) []DatabaseInfo {
	rows, err := pool.Query(ctx, `
        SELECT datname, database_size_bytes, encoding, datconnlimit
        FROM metrics.pg_database
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_database
              WHERE connection_id = $1
          )
          AND datname NOT IN ('template0', 'template1')
        ORDER BY datname
    `, connectionID)
	if err != nil {
		log.Printf("[DEBUG] No database info for connection %d: %v",
			connectionID, err)
		return []DatabaseInfo{}
	}
	defer rows.Close()

	var databases []DatabaseInfo
	for rows.Next() {
		var db DatabaseInfo
		var encoding *int
		if err := rows.Scan(
			&db.Name, &db.SizeBytes, &encoding, &db.ConnectionLimit,
		); err != nil {
			log.Printf("[DEBUG] Error scanning database info: %v", err)
			continue
		}
		if encoding != nil {
			encStr := pgEncodingName(*encoding)
			db.Encoding = &encStr
		}
		db.Extensions = []string{}
		databases = append(databases, db)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating database info: %v", err)
	}

	if databases == nil {
		databases = []DatabaseInfo{}
	}
	return databases
}

// queryExtensions queries the latest extension information for the connection.
func (h *ServerInfoHandler) queryExtensions(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
) []ExtensionInfo {
	rows, err := pool.Query(ctx, `
        SELECT extname, extversion, schema_name, database_name
        FROM metrics.pg_extension
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_extension
              WHERE connection_id = $1
          )
        ORDER BY database_name, extname
    `, connectionID)
	if err != nil {
		log.Printf("[DEBUG] No extension info for connection %d: %v",
			connectionID, err)
		return []ExtensionInfo{}
	}
	defer rows.Close()

	var extensions []ExtensionInfo
	for rows.Next() {
		var ext ExtensionInfo
		if err := rows.Scan(
			&ext.Name, &ext.Version, &ext.Schema, &ext.Database,
		); err != nil {
			log.Printf("[DEBUG] Error scanning extension info: %v", err)
			continue
		}
		extensions = append(extensions, ext)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating extension info: %v", err)
	}

	if extensions == nil {
		extensions = []ExtensionInfo{}
	}
	return extensions
}

// attachExtensionsToDatabases populates the Extensions field on each
// DatabaseInfo entry from the flat extension list.
func (h *ServerInfoHandler) attachExtensionsToDatabases(
	extensions []ExtensionInfo,
	databases []DatabaseInfo,
) {
	dbMap := make(map[string]int, len(databases))
	for i := range databases {
		dbMap[databases[i].Name] = i
	}
	for _, ext := range extensions {
		if idx, ok := dbMap[ext.Database]; ok {
			databases[idx].Extensions = append(
				databases[idx].Extensions, ext.Name,
			)
		}
	}
}

// queryKeySettings queries the curated list of key PostgreSQL settings.
func (h *ServerInfoHandler) queryKeySettings(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
) []SettingInfo {
	// Build the IN clause for key settings
	placeholders := make([]string, len(keySettings))
	args := make([]interface{}, len(keySettings)+1)
	args[0] = connectionID
	for i, name := range keySettings {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = name
	}

	query := fmt.Sprintf(`
        SELECT name, setting, unit, category
        FROM metrics.pg_settings
        WHERE connection_id = $1
          AND collected_at = (
              SELECT MAX(collected_at)
              FROM metrics.pg_settings
              WHERE connection_id = $1
          )
          AND name IN (%s)
        ORDER BY category, name
    `, strings.Join(placeholders, ", "))

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		log.Printf("[DEBUG] No settings info for connection %d: %v",
			connectionID, err)
		return []SettingInfo{}
	}
	defer rows.Close()

	var settings []SettingInfo
	for rows.Next() {
		var s SettingInfo
		if err := rows.Scan(
			&s.Name, &s.Setting, &s.Unit, &s.Category,
		); err != nil {
			log.Printf("[DEBUG] Error scanning setting info: %v", err)
			continue
		}
		settings = append(settings, s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[DEBUG] Error iterating settings: %v", err)
	}

	if settings == nil {
		settings = []SettingInfo{}
	}
	return settings
}

// getAIAnalysis returns a cached or freshly generated AI analysis of
// the databases on the given connection.
func (h *ServerInfoHandler) getAIAnalysis(
	ctx context.Context,
	connectionID int,
	databases []DatabaseInfo,
	extensions []ExtensionInfo,
) *AIAnalysisInfo {
	if h.llmConfig == nil || h.llmConfig.Provider == "" {
		return nil
	}

	// Check cache
	h.cacheMu.RLock()
	if entry, ok := h.cache[connectionID]; ok {
		if time.Now().UTC().Before(entry.expiresAt) {
			h.cacheMu.RUnlock()
			return &AIAnalysisInfo{
				Databases:   entry.analysis,
				GeneratedAt: entry.generatedAt,
			}
		}
	}
	h.cacheMu.RUnlock()

	// No databases to analyze
	if len(databases) == 0 {
		return nil
	}

	// Build prompt
	prompt := buildDatabaseAnalysisPrompt(databases)

	// Create LLM client
	client := h.createLLMClient()
	if client == nil {
		return nil
	}

	messages := []chat.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	resp, err := client.Chat(ctx, messages, nil)
	if err != nil {
		log.Printf("[ERROR] AI analysis failed for connection %d: %v",
			connectionID, err)
		return nil
	}

	// Parse the response into per-database analysis
	analysis := parseDatabaseAnalysisResponse(resp, databases)

	// Cache the result
	now := time.Now().UTC()
	h.cacheMu.Lock()
	h.cache[connectionID] = &aiCacheEntry{
		analysis:    analysis,
		generatedAt: now,
		expiresAt:   now.Add(aiAnalysisCacheTTL),
	}
	h.cacheMu.Unlock()

	return &AIAnalysisInfo{
		Databases:   analysis,
		GeneratedAt: now,
	}
}

// buildDatabaseAnalysisPrompt constructs the LLM prompt for database
// analysis.
func buildDatabaseAnalysisPrompt(databases []DatabaseInfo) string {
	var b strings.Builder

	b.WriteString(`You are a PostgreSQL expert. For each database listed `)
	b.WriteString(`below, provide a brief one-sentence description of what `)
	b.WriteString(`the database appears to be used for, based on its name, `)
	b.WriteString(`size, and installed extensions. Be specific and practical. `)
	b.WriteString(`If you cannot determine the purpose, say so briefly.`)
	b.WriteString("\n\n")
	b.WriteString("Respond with exactly one line per database in the format:\n")
	b.WriteString("DATABASE_NAME: description\n\n")
	b.WriteString("Do not include any other text, headers, or formatting.\n\n")
	b.WriteString("Databases:\n")

	for _, db := range databases {
		sizeStr := "unknown size"
		if db.SizeBytes != nil {
			sizeStr = formatBytes(*db.SizeBytes)
		}
		extStr := "none"
		if len(db.Extensions) > 0 {
			extStr = strings.Join(db.Extensions, ", ")
		}
		fmt.Fprintf(&b, "- %s (size: %s, extensions: %s)\n",
			db.Name, sizeStr, extStr)
	}

	return b.String()
}

// parseDatabaseAnalysisResponse extracts per-database descriptions from
// the LLM response text.
func parseDatabaseAnalysisResponse(
	resp chat.LLMResponse,
	databases []DatabaseInfo,
) map[string]string {
	// Extract text from response
	var text string
	for _, item := range resp.Content {
		switch v := item.(type) {
		case chat.TextContent:
			text += v.Text
		case map[string]interface{}:
			if t, ok := v["type"].(string); ok && t == "text" {
				if txt, ok := v["text"].(string); ok {
					text += txt
				}
			}
		}
	}

	result := make(map[string]string, len(databases))
	lines := strings.Split(text, "\n")

	// Build a set of known database names for validation
	dbNames := make(map[string]bool, len(databases))
	for _, db := range databases {
		dbNames[db.Name] = true
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Parse "DATABASE_NAME: description" format
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		// Strip common LLM formatting artifacts
		name = strings.TrimLeft(name, "- *")
		name = strings.Trim(name, "*`")
		// Strip numbered list prefixes like "1. " or "1) "
		if name != "" && name[0] >= '0' && name[0] <= '9' {
			for i, c := range name {
				if c == '.' || c == ')' {
					name = strings.TrimSpace(name[i+1:])
					break
				}
				if c < '0' || c > '9' {
					break
				}
			}
		}
		name = strings.TrimSpace(name)
		desc := strings.TrimSpace(parts[1])
		if dbNames[name] && desc != "" {
			result[name] = desc
		}
	}

	return result
}

// createLLMClient builds the appropriate chat client based on the
// configured LLM provider.
func (h *ServerInfoHandler) createLLMClient() chat.LLMClient {
	switch h.llmConfig.Provider {
	case "anthropic":
		return chat.NewAnthropicClient(
			h.llmConfig.AnthropicAPIKey,
			h.llmConfig.Model,
			llmAnalysisMaxTokens,
			llmAnalysisTemperature,
			false,
			h.llmConfig.AnthropicBaseURL,
		)
	case "openai":
		return chat.NewOpenAIClient(
			h.llmConfig.OpenAIAPIKey,
			h.llmConfig.Model,
			llmAnalysisMaxTokens,
			llmAnalysisTemperature,
			false,
			h.llmConfig.OpenAIBaseURL,
		)
	case "ollama":
		return chat.NewOllamaClient(
			h.llmConfig.OllamaURL,
			h.llmConfig.Model,
			false,
		)
	default:
		return nil
	}
}

// pgEncodingName converts a PostgreSQL encoding integer to its name.
func pgEncodingName(encoding int) string {
	encodings := map[int]string{
		0:  "SQL_ASCII",
		6:  "UTF8",
		8:  "LATIN1",
		16: "LATIN2",
		17: "LATIN3",
		18: "LATIN4",
		25: "ISO_8859_5",
		26: "ISO_8859_6",
		27: "ISO_8859_7",
		28: "ISO_8859_8",
		29: "LATIN5",
		30: "LATIN6",
		31: "LATIN7",
		32: "LATIN8",
		33: "LATIN9",
		34: "LATIN10",
		35: "WIN1256",
		36: "WIN1258",
		37: "WIN866",
		38: "WIN874",
		39: "KOI8R",
		40: "WIN1251",
		41: "WIN1252",
	}
	if name, ok := encodings[encoding]; ok {
		return name
	}
	return fmt.Sprintf("encoding_%d", encoding)
}

// formatBytes formats a byte count into a human-readable string.
func formatBytes(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
		tb = gb * 1024
	)

	switch {
	case bytes >= tb:
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(tb))
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
