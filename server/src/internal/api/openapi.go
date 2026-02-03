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

// OpenAPI specification types for runtime generation.

// OpenAPISpec represents the root OpenAPI 3.0.3 specification document.
type OpenAPISpec struct {
	OpenAPI    string                     `json:"openapi"`
	Info       OpenAPIInfo                `json:"info"`
	Servers    []OpenAPIServer            `json:"servers"`
	Paths      map[string]OpenAPIPathItem `json:"paths"`
	Components OpenAPIComponents          `json:"components,omitempty"`
}

// OpenAPIInfo contains API metadata.
type OpenAPIInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// OpenAPIServer represents a server endpoint.
type OpenAPIServer struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// OpenAPIPathItem contains operations for a single path.
type OpenAPIPathItem struct {
	Get     *OpenAPIOperation `json:"get,omitempty"`
	Post    *OpenAPIOperation `json:"post,omitempty"`
	Put     *OpenAPIOperation `json:"put,omitempty"`
	Patch   *OpenAPIOperation `json:"patch,omitempty"`
	Delete  *OpenAPIOperation `json:"delete,omitempty"`
	Options *OpenAPIOperation `json:"options,omitempty"`
}

// OpenAPIOperation describes a single API operation.
type OpenAPIOperation struct {
	Summary     string                     `json:"summary,omitempty"`
	Description string                     `json:"description,omitempty"`
	OperationID string                     `json:"operationId,omitempty"`
	Tags        []string                   `json:"tags,omitempty"`
	Parameters  []OpenAPIParameter         `json:"parameters,omitempty"`
	RequestBody *OpenAPIRequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]OpenAPIResponse `json:"responses"`
	Security    []map[string][]string      `json:"security,omitempty"`
}

// OpenAPIParameter describes a single operation parameter.
type OpenAPIParameter struct {
	Name        string         `json:"name"`
	In          string         `json:"in"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Schema      *OpenAPISchema `json:"schema,omitempty"`
}

// OpenAPIRequestBody describes a request body.
type OpenAPIRequestBody struct {
	Description string                      `json:"description,omitempty"`
	Required    bool                        `json:"required,omitempty"`
	Content     map[string]OpenAPIMediaType `json:"content"`
}

// OpenAPIResponse describes a single response.
type OpenAPIResponse struct {
	Description string                      `json:"description"`
	Content     map[string]OpenAPIMediaType `json:"content,omitempty"`
}

// OpenAPIMediaType describes a media type.
type OpenAPIMediaType struct {
	Schema *OpenAPISchema `json:"schema,omitempty"`
}

// OpenAPISchema describes a data schema.
type OpenAPISchema struct {
	Type                 string                    `json:"type,omitempty"`
	Format               string                    `json:"format,omitempty"`
	Description          string                    `json:"description,omitempty"`
	Items                *OpenAPISchema            `json:"items,omitempty"`
	Properties           map[string]*OpenAPISchema `json:"properties,omitempty"`
	AdditionalProperties *OpenAPISchema            `json:"additionalProperties,omitempty"`
	Required             []string                  `json:"required,omitempty"`
	Ref                  string                    `json:"$ref,omitempty"`
	Enum                 []string                  `json:"enum,omitempty"`
	Default              interface{}               `json:"default,omitempty"`
	Nullable             bool                      `json:"nullable,omitempty"`
	Example              interface{}               `json:"example,omitempty"`
}

// OpenAPIComponents contains reusable components.
type OpenAPIComponents struct {
	Schemas         map[string]*OpenAPISchema         `json:"schemas,omitempty"`
	SecuritySchemes map[string]*OpenAPISecurityScheme `json:"securitySchemes,omitempty"`
}

// OpenAPISecurityScheme describes a security scheme.
type OpenAPISecurityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	Description  string `json:"description,omitempty"`
}

// BuildOpenAPISpec generates the OpenAPI specification at runtime.
func BuildOpenAPISpec() *OpenAPISpec {
	return &OpenAPISpec{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       "AI DBA Workbench API",
			Version:     "1.0.0",
			Description: "REST API for AI DBA Workbench - PostgreSQL monitoring and AI-assisted database administration",
		},
		Servers: []OpenAPIServer{
			{
				URL:         "/api/v1",
				Description: "AI DBA Workbench API v1",
			},
		},
		Paths:      buildPaths(),
		Components: buildComponents(),
	}
}

// buildComponents creates the reusable components section.
func buildComponents() OpenAPIComponents {
	return OpenAPIComponents{
		Schemas:         buildSchemas(),
		SecuritySchemes: buildSecuritySchemes(),
	}
}

// buildSecuritySchemes creates the security scheme definitions.
func buildSecuritySchemes() map[string]*OpenAPISecurityScheme {
	return map[string]*OpenAPISecurityScheme{
		"bearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "Session token obtained from /auth/login",
		},
	}
}

// buildSchemas creates the schema definitions.
func buildSchemas() map[string]*OpenAPISchema {
	return map[string]*OpenAPISchema{
		"ErrorResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"error": {Type: "string", Description: "Error message"},
			},
			Required: []string{"error"},
		},
		"LoginRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"username": {Type: "string", Description: "Username for authentication"},
				"password": {Type: "string", Description: "Password for authentication"},
			},
			Required: []string{"username", "password"},
		},
		"LoginResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"success":       {Type: "boolean", Description: "Whether login succeeded"},
				"session_token": {Type: "string", Description: "Session token for authenticated requests"},
				"expires_at":    {Type: "string", Format: "date-time", Description: "Token expiration time"},
				"message":       {Type: "string", Description: "Status message"},
			},
		},
		"UserInfoResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"authenticated": {Type: "boolean", Description: "Whether the user is authenticated"},
				"username":      {Type: "string", Description: "Username of the authenticated user"},
				"is_superuser":  {Type: "boolean", Description: "Whether the user has superuser privileges"},
				"error":         {Type: "string", Description: "Error message if authentication failed"},
			},
		},
		"Connection": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":             {Type: "integer", Description: "Connection ID"},
				"name":           {Type: "string", Description: "Display name"},
				"host":           {Type: "string", Description: "Database host"},
				"hostaddr":       {Type: "string", Description: "Database host IP address", Nullable: true},
				"port":           {Type: "integer", Description: "Database port"},
				"database_name":  {Type: "string", Description: "Default database name"},
				"username":       {Type: "string", Description: "Database username"},
				"sslmode":        {Type: "string", Description: "SSL mode", Nullable: true},
				"is_shared":      {Type: "boolean", Description: "Whether the connection is shared"},
				"is_monitored":   {Type: "boolean", Description: "Whether the connection is monitored"},
				"owner_username": {Type: "string", Description: "Username of the connection owner", Nullable: true},
				"created_at":     {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"updated_at":     {Type: "string", Format: "date-time", Description: "Last update timestamp"},
			},
		},
		"ConnectionCreateRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":          {Type: "string", Description: "Display name"},
				"host":          {Type: "string", Description: "Database host"},
				"hostaddr":      {Type: "string", Description: "Database host IP address"},
				"port":          {Type: "integer", Description: "Database port"},
				"database_name": {Type: "string", Description: "Default database name"},
				"username":      {Type: "string", Description: "Database username"},
				"password":      {Type: "string", Description: "Database password"},
				"sslmode":       {Type: "string", Description: "SSL mode"},
				"sslcert":       {Type: "string", Description: "SSL certificate path"},
				"sslkey":        {Type: "string", Description: "SSL key path"},
				"sslrootcert":   {Type: "string", Description: "SSL root certificate path"},
				"is_shared":     {Type: "boolean", Description: "Whether the connection is shared"},
				"is_monitored":  {Type: "boolean", Description: "Whether the connection is monitored"},
			},
			Required: []string{"name", "host", "port", "database_name", "username", "password"},
		},
		"ConnectionUpdateRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":          {Type: "string", Description: "Display name"},
				"host":          {Type: "string", Description: "Database host"},
				"hostaddr":      {Type: "string", Description: "Database host IP address"},
				"port":          {Type: "integer", Description: "Database port"},
				"database_name": {Type: "string", Description: "Default database name"},
				"username":      {Type: "string", Description: "Database username"},
				"password":      {Type: "string", Description: "Database password"},
				"sslmode":       {Type: "string", Description: "SSL mode"},
				"sslcert":       {Type: "string", Description: "SSL certificate path"},
				"sslkey":        {Type: "string", Description: "SSL key path"},
				"sslrootcert":   {Type: "string", Description: "SSL root certificate path"},
				"is_shared":     {Type: "boolean", Description: "Whether the connection is shared"},
				"is_monitored":  {Type: "boolean", Description: "Whether the connection is monitored"},
			},
		},
		"CurrentConnectionRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"connection_id": {Type: "integer", Description: "Connection ID to select"},
				"database_name": {Type: "string", Description: "Database name to use"},
			},
			Required: []string{"connection_id"},
		},
		"CurrentConnectionResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"connection_id": {Type: "integer", Description: "Selected connection ID"},
				"database_name": {Type: "string", Description: "Selected database name", Nullable: true},
				"host":          {Type: "string", Description: "Connection host"},
				"port":          {Type: "integer", Description: "Connection port"},
				"name":          {Type: "string", Description: "Connection display name"},
			},
		},
		"DatabaseInfo": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":        {Type: "string", Description: "Database name"},
				"owner":       {Type: "string", Description: "Database owner"},
				"encoding":    {Type: "string", Description: "Database encoding"},
				"size":        {Type: "string", Description: "Database size"},
				"tablespace":  {Type: "string", Description: "Default tablespace"},
				"description": {Type: "string", Description: "Database description", Nullable: true},
			},
		},
		"ClusterGroup": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":             {Type: "string", Description: "Group ID (numeric or auto-detected key)"},
				"name":           {Type: "string", Description: "Group name"},
				"description":    {Type: "string", Description: "Group description", Nullable: true},
				"owner_username": {Type: "string", Description: "Owner username", Nullable: true},
				"created_at":     {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"updated_at":     {Type: "string", Format: "date-time", Description: "Last update timestamp"},
			},
		},
		"ClusterGroupRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":        {Type: "string", Description: "Group name"},
				"description": {Type: "string", Description: "Group description"},
			},
			Required: []string{"name"},
		},
		"Cluster": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":          {Type: "string", Description: "Cluster ID"},
				"group_id":    {Type: "string", Description: "Parent group ID"},
				"name":        {Type: "string", Description: "Cluster name"},
				"description": {Type: "string", Description: "Cluster description", Nullable: true},
				"created_at":  {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"updated_at":  {Type: "string", Format: "date-time", Description: "Last update timestamp"},
			},
		},
		"ClusterRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":        {Type: "string", Description: "Cluster name"},
				"description": {Type: "string", Description: "Cluster description"},
				"group_id":    {Type: "integer", Description: "Group ID to assign cluster to"},
			},
		},
		"ServerInfo": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":           {Type: "integer", Description: "Server/connection ID"},
				"name":         {Type: "string", Description: "Server name"},
				"host":         {Type: "string", Description: "Server host"},
				"port":         {Type: "integer", Description: "Server port"},
				"role":         {Type: "string", Description: "Server role in cluster"},
				"cluster_id":   {Type: "string", Description: "Cluster ID", Nullable: true},
				"is_monitored": {Type: "boolean", Description: "Whether the server is monitored"},
			},
		},
		"ClusterTopology": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"groups": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/TopologyGroup"},
				},
			},
		},
		"TopologyGroup": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":          {Type: "string", Description: "Group ID"},
				"name":        {Type: "string", Description: "Group name"},
				"description": {Type: "string", Description: "Group description", Nullable: true},
				"clusters": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/TopologyCluster"},
				},
			},
		},
		"TopologyCluster": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":          {Type: "string", Description: "Cluster ID"},
				"name":        {Type: "string", Description: "Cluster name"},
				"description": {Type: "string", Description: "Cluster description", Nullable: true},
				"type":        {Type: "string", Description: "Cluster type (spock, streaming, standalone)"},
				"servers": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/TopologyServer"},
				},
			},
		},
		"TopologyServer": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":           {Type: "integer", Description: "Server/connection ID"},
				"name":         {Type: "string", Description: "Server name"},
				"host":         {Type: "string", Description: "Server host"},
				"port":         {Type: "integer", Description: "Server port"},
				"role":         {Type: "string", Description: "Server role"},
				"is_monitored": {Type: "boolean", Description: "Whether the server is monitored"},
				"status":       {Type: "string", Description: "Server status"},
			},
		},
		"Alert": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":              {Type: "integer", Format: "int64", Description: "Alert ID"},
				"connection_id":   {Type: "integer", Description: "Associated connection ID"},
				"server_name":     {Type: "string", Description: "Server name"},
				"alert_type":      {Type: "string", Description: "Type of alert"},
				"severity":        {Type: "string", Enum: []string{"critical", "warning", "info"}, Description: "Alert severity"},
				"status":          {Type: "string", Enum: []string{"active", "acknowledged", "cleared"}, Description: "Alert status"},
				"title":           {Type: "string", Description: "Alert title"},
				"message":         {Type: "string", Description: "Alert message"},
				"metric_name":     {Type: "string", Description: "Metric that triggered the alert", Nullable: true},
				"metric_value":    {Type: "number", Description: "Metric value when alert was triggered", Nullable: true},
				"threshold_value": {Type: "number", Description: "Threshold that was exceeded", Nullable: true},
				"fired_at":        {Type: "string", Format: "date-time", Description: "When the alert fired"},
				"cleared_at":      {Type: "string", Format: "date-time", Description: "When the alert was cleared", Nullable: true},
				"acknowledged_at": {Type: "string", Format: "date-time", Description: "When the alert was acknowledged", Nullable: true},
				"acknowledged_by": {Type: "string", Description: "Who acknowledged the alert", Nullable: true},
				"ack_message":     {Type: "string", Description: "Acknowledgement message", Nullable: true},
				"false_positive":  {Type: "boolean", Description: "Whether marked as false positive"},
			},
		},
		"AlertListResult": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"alerts": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/Alert"},
				},
				"total": {Type: "integer", Description: "Total count of matching alerts"},
			},
		},
		"AlertCountsResult": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"counts": {
					Type: "object",
					AdditionalProperties: &OpenAPISchema{
						Type: "object",
						Properties: map[string]*OpenAPISchema{
							"critical": {Type: "integer"},
							"warning":  {Type: "integer"},
							"info":     {Type: "integer"},
						},
					},
					Description: "Alert counts by server ID",
				},
			},
		},
		"AcknowledgeRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"alert_id":       {Type: "integer", Format: "int64", Description: "Alert ID to acknowledge"},
				"message":        {Type: "string", Description: "Acknowledgement message"},
				"false_positive": {Type: "boolean", Description: "Mark as false positive"},
			},
			Required: []string{"alert_id"},
		},
		"TimelineEvent": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":            {Type: "integer", Format: "int64", Description: "Event ID"},
				"connection_id": {Type: "integer", Description: "Associated connection ID"},
				"server_name":   {Type: "string", Description: "Server name"},
				"event_type":    {Type: "string", Description: "Type of event"},
				"severity":      {Type: "string", Description: "Event severity"},
				"title":         {Type: "string", Description: "Event title"},
				"message":       {Type: "string", Description: "Event message"},
				"details":       {Type: "object", Description: "Additional event details"},
				"event_time":    {Type: "string", Format: "date-time", Description: "When the event occurred"},
				"created_at":    {Type: "string", Format: "date-time", Description: "When the event was recorded"},
			},
		},
		"TimelineResult": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"events": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/TimelineEvent"},
				},
				"total": {Type: "integer", Description: "Total count of matching events"},
			},
		},
		"Conversation": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":         {Type: "string", Description: "Conversation ID"},
				"username":   {Type: "string", Description: "Owner username"},
				"title":      {Type: "string", Description: "Conversation title"},
				"provider":   {Type: "string", Description: "LLM provider"},
				"model":      {Type: "string", Description: "LLM model"},
				"connection": {Type: "string", Description: "Associated connection name"},
				"messages": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/Message"},
				},
				"created_at": {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"updated_at": {Type: "string", Format: "date-time", Description: "Last update timestamp"},
			},
		},
		"ConversationSummary": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":         {Type: "string", Description: "Conversation ID"},
				"title":      {Type: "string", Description: "Conversation title"},
				"connection": {Type: "string", Description: "Associated connection name"},
				"created_at": {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"updated_at": {Type: "string", Format: "date-time", Description: "Last update timestamp"},
				"preview":    {Type: "string", Description: "Preview of first message"},
			},
		},
		"ConversationListResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"conversations": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/ConversationSummary"},
				},
			},
		},
		"ConversationCreateRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"provider":   {Type: "string", Description: "LLM provider"},
				"model":      {Type: "string", Description: "LLM model"},
				"connection": {Type: "string", Description: "Associated connection name"},
				"messages": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/Message"},
				},
			},
			Required: []string{"messages"},
		},
		"ConversationUpdateRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"provider":   {Type: "string", Description: "LLM provider"},
				"model":      {Type: "string", Description: "LLM model"},
				"connection": {Type: "string", Description: "Associated connection name"},
				"messages": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/Message"},
				},
			},
		},
		"ConversationRenameRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"title": {Type: "string", Description: "New conversation title"},
			},
			Required: []string{"title"},
		},
		"Message": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"role":          {Type: "string", Description: "Message role (user, assistant, system)"},
				"content":       {Type: "object", Description: "Message content (string or structured)"},
				"timestamp":     {Type: "string", Description: "Message timestamp"},
				"provider":      {Type: "string", Description: "LLM provider used"},
				"model":         {Type: "string", Description: "LLM model used"},
				"cache_control": {Type: "object", Description: "Cache control settings"},
			},
			Required: []string{"role", "content"},
		},
		"ProvidersResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"providers": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/ProviderInfo"},
				},
				"defaultModel": {Type: "string", Description: "Default model name"},
			},
		},
		"ProviderInfo": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":      {Type: "string", Description: "Provider identifier"},
				"display":   {Type: "string", Description: "Display name"},
				"isDefault": {Type: "boolean", Description: "Whether this is the default provider"},
			},
		},
		"ModelsResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"models": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/ModelInfo"},
				},
			},
		},
		"ModelInfo": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":        {Type: "string", Description: "Model identifier"},
				"description": {Type: "string", Description: "Model description"},
			},
		},
		"ChatRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"messages": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/Message"},
				},
				"tools": {
					Type:        "array",
					Items:       &OpenAPISchema{Type: "object"},
					Description: "MCP tools available to the LLM",
				},
				"provider": {Type: "string", Description: "Override default provider"},
				"model":    {Type: "string", Description: "Override default model"},
				"debug":    {Type: "boolean", Description: "Enable debug mode for token usage"},
			},
			Required: []string{"messages"},
		},
		"ChatResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"content": {
					Type:        "array",
					Items:       &OpenAPISchema{Type: "object"},
					Description: "Response content blocks",
				},
				"stop_reason": {Type: "string", Description: "Reason for stopping generation"},
				"token_usage": {
					Type:        "object",
					Description: "Token usage statistics (when debug enabled)",
					Properties: map[string]*OpenAPISchema{
						"input_tokens":  {Type: "integer"},
						"output_tokens": {Type: "integer"},
					},
				},
			},
		},
		"CompactRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"messages": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/Message"},
				},
				"max_tokens":    {Type: "integer", Description: "Maximum tokens to target", Default: 100000},
				"recent_window": {Type: "integer", Description: "Number of recent messages to always keep", Default: 10},
				"keep_anchors":  {Type: "boolean", Description: "Keep anchor messages"},
				"options":       {Type: "object", Description: "Advanced compaction options"},
			},
			Required: []string{"messages"},
		},
		"CompactResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"messages": {
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/Message"},
				},
				"summary":        {Type: "object", Description: "Summary of dropped messages"},
				"token_estimate": {Type: "integer", Description: "Estimated token count"},
				"compaction_info": {
					Type:        "object",
					Description: "Compaction statistics",
					Properties: map[string]*OpenAPISchema{
						"original_count":    {Type: "integer"},
						"compacted_count":   {Type: "integer"},
						"dropped_count":     {Type: "integer"},
						"anchor_count":      {Type: "integer"},
						"tokens_saved":      {Type: "integer"},
						"compression_ratio": {Type: "number"},
					},
				},
			},
		},
		"SuccessResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"success": {Type: "boolean", Description: "Whether the operation succeeded"},
			},
		},
		"DeleteAllResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"success": {Type: "boolean", Description: "Whether the operation succeeded"},
				"deleted": {Type: "integer", Description: "Number of items deleted"},
			},
		},
	}
}

// buildPaths creates all API path definitions.
func buildPaths() map[string]OpenAPIPathItem {
	bearerAuth := []map[string][]string{{"bearerAuth": {}}}

	return map[string]OpenAPIPathItem{
		// Authentication
		"/auth/login": {
			Post: &OpenAPIOperation{
				Summary:     "User login",
				Description: "Authenticate a user and obtain a session token",
				OperationID: "login",
				Tags:        []string{"Authentication"},
				RequestBody: jsonRequestBody("LoginRequest", "User credentials", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("LoginResponse", "Successful authentication"),
					"400": jsonResponse("ErrorResponse", "Invalid request body"),
					"401": jsonResponse("ErrorResponse", "Invalid credentials"),
					"429": jsonResponse("ErrorResponse", "Too many failed attempts"),
					"503": jsonResponse("ErrorResponse", "Authentication not configured"),
				},
			},
		},

		// User Info
		"/user/info": {
			Get: &OpenAPIOperation{
				Summary:     "Get current user info",
				Description: "Returns authentication status and user information",
				OperationID: "getUserInfo",
				Tags:        []string{"User"},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("UserInfoResponse", "User information"),
				},
			},
		},

		// Connections
		"/connections": {
			Get: &OpenAPIOperation{
				Summary:     "List all connections",
				Description: "Returns all database connections available to the user",
				OperationID: "listConnections",
				Tags:        []string{"Connections"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("Connection", "List of connections"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"503": jsonResponse("ErrorResponse", "Datastore not configured"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Create a connection",
				Description: "Creates a new database connection",
				OperationID: "createConnection",
				Tags:        []string{"Connections"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("ConnectionCreateRequest", "Connection details", true),
				Responses: map[string]OpenAPIResponse{
					"201": jsonResponse("Connection", "Connection created"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Forbidden"),
					"503": jsonResponse("ErrorResponse", "Datastore not configured"),
				},
			},
		},

		"/connections/{id}": {
			Get: &OpenAPIOperation{
				Summary:     "Get connection by ID",
				Description: "Returns a specific connection by its ID",
				OperationID: "getConnection",
				Tags:        []string{"Connections"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Connection ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("Connection", "Connection details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Connection not found"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Update a connection",
				Description: "Updates an existing connection",
				OperationID: "updateConnection",
				Tags:        []string{"Connections"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Connection ID")},
				RequestBody: jsonRequestBody("ConnectionUpdateRequest", "Updated connection details", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("Connection", "Updated connection"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Forbidden"),
					"404": jsonResponse("ErrorResponse", "Connection not found"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete a connection",
				Description: "Deletes a connection",
				OperationID: "deleteConnection",
				Tags:        []string{"Connections"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Connection ID")},
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Connection deleted"},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Forbidden"),
					"404": jsonResponse("ErrorResponse", "Connection not found"),
				},
			},
		},

		"/connections/{id}/databases": {
			Get: &OpenAPIOperation{
				Summary:     "List databases for a connection",
				Description: "Returns all databases accessible via the specified connection",
				OperationID: "listConnectionDatabases",
				Tags:        []string{"Connections"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Connection ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("DatabaseInfo", "List of databases"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Connection not found"),
					"500": jsonResponse("ErrorResponse", "Failed to list databases"),
				},
			},
		},

		"/connections/current": {
			Get: &OpenAPIOperation{
				Summary:     "Get current connection",
				Description: "Returns the currently selected connection for the session",
				OperationID: "getCurrentConnection",
				Tags:        []string{"Connections"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("CurrentConnectionResponse", "Current connection"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "No connection selected"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Set current connection",
				Description: "Sets the connection for the current session",
				OperationID: "setCurrentConnection",
				Tags:        []string{"Connections"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("CurrentConnectionRequest", "Connection to select", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("CurrentConnectionResponse", "Connection selected"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Clear current connection",
				Description: "Clears the current connection selection",
				OperationID: "clearCurrentConnection",
				Tags:        []string{"Connections"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Connection cleared"},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		// Clusters
		"/clusters": {
			Get: &OpenAPIOperation{
				Summary:     "Get cluster topology",
				Description: "Returns the full cluster hierarchy including groups, clusters, and servers",
				OperationID: "getClusterTopology",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("ClusterTopology", "Cluster topology"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"503": jsonResponse("ErrorResponse", "Datastore not configured"),
				},
			},
		},

		"/clusters/{id}": {
			Get: &OpenAPIOperation{
				Summary:     "Get cluster by ID",
				Description: "Returns a specific cluster",
				OperationID: "getCluster",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamString("id", "Cluster ID (numeric or auto-detected key)")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("Cluster", "Cluster details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Cluster not found"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Update a cluster",
				Description: "Updates cluster name, description, or group assignment",
				OperationID: "updateCluster",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamString("id", "Cluster ID")},
				RequestBody: jsonRequestBody("ClusterRequest", "Updated cluster details", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("Cluster", "Updated cluster"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Forbidden"),
					"404": jsonResponse("ErrorResponse", "Cluster not found"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete a cluster",
				Description: "Deletes a manually created cluster",
				OperationID: "deleteCluster",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamString("id", "Cluster ID")},
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Cluster deleted"},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Forbidden"),
					"404": jsonResponse("ErrorResponse", "Cluster not found"),
				},
			},
		},

		"/clusters/{id}/servers": {
			Get: &OpenAPIOperation{
				Summary:     "List servers in a cluster",
				Description: "Returns all servers belonging to a cluster",
				OperationID: "listClusterServers",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Cluster ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("ServerInfo", "List of servers"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"500": jsonResponse("ErrorResponse", "Failed to list servers"),
				},
			},
		},

		// Cluster Groups
		"/cluster-groups": {
			Get: &OpenAPIOperation{
				Summary:     "List all cluster groups",
				Description: "Returns all cluster groups",
				OperationID: "listClusterGroups",
				Tags:        []string{"Cluster Groups"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("ClusterGroup", "List of cluster groups"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"503": jsonResponse("ErrorResponse", "Datastore not configured"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Create a cluster group",
				Description: "Creates a new cluster group",
				OperationID: "createClusterGroup",
				Tags:        []string{"Cluster Groups"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("ClusterGroupRequest", "Group details", true),
				Responses: map[string]OpenAPIResponse{
					"201": jsonResponse("ClusterGroup", "Group created"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		"/cluster-groups/{id}": {
			Get: &OpenAPIOperation{
				Summary:     "Get cluster group by ID",
				Description: "Returns a specific cluster group",
				OperationID: "getClusterGroup",
				Tags:        []string{"Cluster Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamString("id", "Group ID (numeric or auto-detected key)")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("ClusterGroup", "Group details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Group not found"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Update a cluster group",
				Description: "Updates a cluster group",
				OperationID: "updateClusterGroup",
				Tags:        []string{"Cluster Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamString("id", "Group ID")},
				RequestBody: jsonRequestBody("ClusterGroupRequest", "Updated group details", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("ClusterGroup", "Updated group"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Forbidden"),
					"404": jsonResponse("ErrorResponse", "Group not found"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete a cluster group",
				Description: "Deletes a cluster group",
				OperationID: "deleteClusterGroup",
				Tags:        []string{"Cluster Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamString("id", "Group ID")},
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Group deleted"},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Cannot delete default group"),
					"404": jsonResponse("ErrorResponse", "Group not found"),
				},
			},
		},

		"/cluster-groups/{id}/clusters": {
			Get: &OpenAPIOperation{
				Summary:     "List clusters in a group",
				Description: "Returns all clusters belonging to a group",
				OperationID: "listGroupClusters",
				Tags:        []string{"Cluster Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("Cluster", "List of clusters"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"500": jsonResponse("ErrorResponse", "Failed to list clusters"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Create a cluster in a group",
				Description: "Creates a new cluster within a group",
				OperationID: "createClusterInGroup",
				Tags:        []string{"Cluster Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				RequestBody: jsonRequestBody("ClusterRequest", "Cluster details", true),
				Responses: map[string]OpenAPIResponse{
					"201": jsonResponse("Cluster", "Cluster created"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		// Alerts
		"/alerts": {
			Get: &OpenAPIOperation{
				Summary:     "List alerts",
				Description: "Returns alerts with optional filtering",
				OperationID: "listAlerts",
				Tags:        []string{"Alerts"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamInt("connection_id", "Filter by connection ID"),
					queryParamString("connection_ids", "Filter by multiple connection IDs (comma-separated)"),
					queryParamString("status", "Filter by status (active, acknowledged, cleared)"),
					queryParamBool("exclude_cleared", "Exclude cleared alerts"),
					queryParamString("severity", "Filter by severity (critical, warning, info)"),
					queryParamString("alert_type", "Filter by alert type"),
					queryParamString("start_time", "Filter by start time (RFC3339)"),
					queryParamString("end_time", "Filter by end time (RFC3339)"),
					queryParamInt("limit", "Maximum number of results (default 100)"),
					queryParamInt("offset", "Offset for pagination"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("AlertListResult", "List of alerts"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"503": jsonResponse("ErrorResponse", "Datastore not configured"),
				},
			},
		},

		"/alerts/counts": {
			Get: &OpenAPIOperation{
				Summary:     "Get alert counts by server",
				Description: "Returns counts of active alerts grouped by server and severity",
				OperationID: "getAlertCounts",
				Tags:        []string{"Alerts"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("AlertCountsResult", "Alert counts"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"503": jsonResponse("ErrorResponse", "Datastore not configured"),
				},
			},
		},

		"/alerts/acknowledge": {
			Post: &OpenAPIOperation{
				Summary:     "Acknowledge an alert",
				Description: "Acknowledges an alert with an optional message",
				OperationID: "acknowledgeAlert",
				Tags:        []string{"Alerts"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("AcknowledgeRequest", "Acknowledgement details", true),
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Alert acknowledged",
						Content: map[string]OpenAPIMediaType{
							"application/json": {
								Schema: &OpenAPISchema{
									Type: "object",
									Properties: map[string]*OpenAPISchema{
										"status": {Type: "string", Example: "acknowledged"},
									},
								},
							},
						},
					},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"500": jsonResponse("ErrorResponse", "Failed to acknowledge"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Unacknowledge an alert",
				Description: "Removes acknowledgement from an alert",
				OperationID: "unacknowledgeAlert",
				Tags:        []string{"Alerts"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{queryParamIntRequired("alert_id", "Alert ID to unacknowledge")},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Alert unacknowledged",
						Content: map[string]OpenAPIMediaType{
							"application/json": {
								Schema: &OpenAPISchema{
									Type: "object",
									Properties: map[string]*OpenAPISchema{
										"status": {Type: "string", Example: "active"},
									},
								},
							},
						},
					},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"500": jsonResponse("ErrorResponse", "Failed to unacknowledge"),
				},
			},
		},

		// Timeline
		"/timeline/events": {
			Get: &OpenAPIOperation{
				Summary:     "List timeline events",
				Description: "Returns timeline events within a time range",
				OperationID: "listTimelineEvents",
				Tags:        []string{"Timeline"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamStringRequired("start_time", "Start time (RFC3339)"),
					queryParamStringRequired("end_time", "End time (RFC3339)"),
					queryParamInt("connection_id", "Filter by connection ID"),
					queryParamString("connection_ids", "Filter by multiple connection IDs (comma-separated)"),
					queryParamString("event_types", "Filter by event types (comma-separated)"),
					queryParamInt("limit", "Maximum number of results (default 500, max 1000)"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("TimelineResult", "List of timeline events"),
					"400": jsonResponse("ErrorResponse", "Invalid parameters"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"503": jsonResponse("ErrorResponse", "Datastore not configured"),
				},
			},
		},

		// Conversations
		"/conversations": {
			Get: &OpenAPIOperation{
				Summary:     "List conversations",
				Description: "Returns all conversations for the authenticated user",
				OperationID: "listConversations",
				Tags:        []string{"Conversations"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamInt("limit", "Maximum number of results (default 50)"),
					queryParamInt("offset", "Offset for pagination"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("ConversationListResponse", "List of conversations"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Create a conversation",
				Description: "Creates a new conversation",
				OperationID: "createConversation",
				Tags:        []string{"Conversations"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("ConversationCreateRequest", "Conversation details", true),
				Responses: map[string]OpenAPIResponse{
					"201": jsonResponse("Conversation", "Conversation created"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete all conversations",
				Description: "Deletes all conversations for the user (requires ?all=true)",
				OperationID: "deleteAllConversations",
				Tags:        []string{"Conversations"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{queryParamBoolRequired("all", "Must be true to confirm deletion")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("DeleteAllResponse", "Conversations deleted"),
					"400": jsonResponse("ErrorResponse", "Missing all=true parameter"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		"/conversations/{id}": {
			Get: &OpenAPIOperation{
				Summary:     "Get conversation by ID",
				Description: "Returns a specific conversation with all messages",
				OperationID: "getConversation",
				Tags:        []string{"Conversations"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamString("id", "Conversation ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("Conversation", "Conversation details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Conversation not found"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Update a conversation",
				Description: "Updates a conversation with new messages",
				OperationID: "updateConversation",
				Tags:        []string{"Conversations"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamString("id", "Conversation ID")},
				RequestBody: jsonRequestBody("ConversationUpdateRequest", "Updated conversation", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("Conversation", "Updated conversation"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Access denied"),
					"404": jsonResponse("ErrorResponse", "Conversation not found"),
				},
			},
			Patch: &OpenAPIOperation{
				Summary:     "Rename a conversation",
				Description: "Updates the conversation title",
				OperationID: "renameConversation",
				Tags:        []string{"Conversations"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamString("id", "Conversation ID")},
				RequestBody: jsonRequestBody("ConversationRenameRequest", "New title", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("SuccessResponse", "Conversation renamed"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Conversation not found"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete a conversation",
				Description: "Deletes a conversation",
				OperationID: "deleteConversation",
				Tags:        []string{"Conversations"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamString("id", "Conversation ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("SuccessResponse", "Conversation deleted"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Conversation not found"),
				},
			},
		},

		// LLM
		"/llm/providers": {
			Get: &OpenAPIOperation{
				Summary:     "List LLM providers",
				Description: "Returns available LLM providers and the default model",
				OperationID: "listLLMProviders",
				Tags:        []string{"LLM"},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("ProvidersResponse", "Available providers"),
				},
			},
		},

		"/llm/models": {
			Get: &OpenAPIOperation{
				Summary:     "List LLM models",
				Description: "Returns available models for a specific provider",
				OperationID: "listLLMModels",
				Tags:        []string{"LLM"},
				Parameters:  []OpenAPIParameter{queryParamStringRequired("provider", "Provider name (anthropic, openai, ollama)")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("ModelsResponse", "Available models"),
					"400": jsonResponse("ErrorResponse", "Invalid provider"),
				},
			},
		},

		"/llm/chat": {
			Post: &OpenAPIOperation{
				Summary:     "Send chat message to LLM",
				Description: "Sends messages to the configured LLM and returns the response",
				OperationID: "chatWithLLM",
				Tags:        []string{"LLM"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("ChatRequest", "Chat messages and tools", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("ChatResponse", "LLM response"),
					"400": jsonResponse("ErrorResponse", "Invalid request or provider not configured"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"500": jsonResponse("ErrorResponse", "LLM error"),
				},
			},
		},

		// Chat Compaction
		"/chat/compact": {
			Post: &OpenAPIOperation{
				Summary:     "Compact chat history",
				Description: "Compacts chat history to reduce token usage while preserving important context",
				OperationID: "compactChatHistory",
				Tags:        []string{"Chat"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("CompactRequest", "Messages to compact", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("CompactResponse", "Compacted messages"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},
	}
}

// Helper functions for building OpenAPI structures

func jsonRequestBody(schemaRef, description string, required bool) *OpenAPIRequestBody {
	return &OpenAPIRequestBody{
		Description: description,
		Required:    required,
		Content: map[string]OpenAPIMediaType{
			"application/json": {
				Schema: &OpenAPISchema{Ref: "#/components/schemas/" + schemaRef},
			},
		},
	}
}

func jsonResponse(schemaRef, description string) OpenAPIResponse {
	return OpenAPIResponse{
		Description: description,
		Content: map[string]OpenAPIMediaType{
			"application/json": {
				Schema: &OpenAPISchema{Ref: "#/components/schemas/" + schemaRef},
			},
		},
	}
}

func jsonArrayResponse(itemSchemaRef, description string) OpenAPIResponse {
	return OpenAPIResponse{
		Description: description,
		Content: map[string]OpenAPIMediaType{
			"application/json": {
				Schema: &OpenAPISchema{
					Type:  "array",
					Items: &OpenAPISchema{Ref: "#/components/schemas/" + itemSchemaRef},
				},
			},
		},
	}
}

func pathParamInt(name, description string) OpenAPIParameter {
	return OpenAPIParameter{
		Name:        name,
		In:          "path",
		Description: description,
		Required:    true,
		Schema:      &OpenAPISchema{Type: "integer"},
	}
}

func pathParamString(name, description string) OpenAPIParameter {
	return OpenAPIParameter{
		Name:        name,
		In:          "path",
		Description: description,
		Required:    true,
		Schema:      &OpenAPISchema{Type: "string"},
	}
}

func queryParamInt(name, description string) OpenAPIParameter {
	return OpenAPIParameter{
		Name:        name,
		In:          "query",
		Description: description,
		Required:    false,
		Schema:      &OpenAPISchema{Type: "integer"},
	}
}

func queryParamIntRequired(name, description string) OpenAPIParameter {
	return OpenAPIParameter{
		Name:        name,
		In:          "query",
		Description: description,
		Required:    true,
		Schema:      &OpenAPISchema{Type: "integer"},
	}
}

func queryParamString(name, description string) OpenAPIParameter {
	return OpenAPIParameter{
		Name:        name,
		In:          "query",
		Description: description,
		Required:    false,
		Schema:      &OpenAPISchema{Type: "string"},
	}
}

func queryParamStringRequired(name, description string) OpenAPIParameter {
	return OpenAPIParameter{
		Name:        name,
		In:          "query",
		Description: description,
		Required:    true,
		Schema:      &OpenAPISchema{Type: "string"},
	}
}

func queryParamBool(name, description string) OpenAPIParameter {
	return OpenAPIParameter{
		Name:        name,
		In:          "query",
		Description: description,
		Required:    false,
		Schema:      &OpenAPISchema{Type: "boolean"},
	}
}

func queryParamBoolRequired(name, description string) OpenAPIParameter {
	return OpenAPIParameter{
		Name:        name,
		In:          "query",
		Description: description,
		Required:    true,
		Schema:      &OpenAPISchema{Type: "boolean"},
	}
}
