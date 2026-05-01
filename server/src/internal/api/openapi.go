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
	Default              any                       `json:"default,omitempty"`
	Nullable             bool                      `json:"nullable,omitempty"`
	Example              any                       `json:"example,omitempty"`
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
			Version:     "1.0.0-beta1",
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
			Type:        "object",
			Description: "The session token is transmitted via httpOnly cookie, not in the response body",
			Properties: map[string]*OpenAPISchema{
				"success":    {Type: "boolean", Description: "Whether login succeeded"},
				"expires_at": {Type: "string", Format: "date-time", Description: "Token expiration time"},
				"message":    {Type: "string", Description: "Status message"},
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
				"id":                {Type: "integer", Description: "Connection ID"},
				"name":              {Type: "string", Description: "Display name"},
				"host":              {Type: "string", Description: "Database host"},
				"hostaddr":          {Type: "string", Description: "Database host IP address", Nullable: true},
				"port":              {Type: "integer", Description: "Database port"},
				"database_name":     {Type: "string", Description: "Default database name"},
				"username":          {Type: "string", Description: "Database username"},
				"ssl_mode":          {Type: "string", Description: "SSL mode", Nullable: true},
				"is_shared":         {Type: "boolean", Description: "Whether the connection is shared"},
				"is_monitored":      {Type: "boolean", Description: "Whether the connection is monitored"},
				"owner_username":    {Type: "string", Description: "Username of the connection owner", Nullable: true},
				"membership_source": {Type: "string", Description: "How the connection was assigned to its cluster: auto (by auto-detection) or manual (by a user)"},
				"created_at":        {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"updated_at":        {Type: "string", Format: "date-time", Description: "Last update timestamp"},
			},
		},
		"ConnectionCreateRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":               {Type: "string", Description: "Display name"},
				"host":               {Type: "string", Description: "Database host"},
				"hostaddr":           {Type: "string", Description: "Database host IP address"},
				"port":               {Type: "integer", Description: "Database port"},
				"database_name":      {Type: "string", Description: "Default database name"},
				"username":           {Type: "string", Description: "Database username"},
				"password":           {Type: "string", Description: "Database password"},
				"ssl_mode":           {Type: "string", Description: "SSL mode"},
				"ssl_cert_path":      {Type: "string", Description: "SSL certificate path"},
				"ssl_key_path":       {Type: "string", Description: "SSL key path"},
				"ssl_root_cert_path": {Type: "string", Description: "SSL root certificate path"},
				"is_shared":          {Type: "boolean", Description: "Whether the connection is shared"},
				"is_monitored":       {Type: "boolean", Description: "Whether the connection is monitored"},
			},
			Required: []string{"name", "host", "port", "database_name", "username", "password"},
		},
		"ConnectionUpdateRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":               {Type: "string", Description: "Display name"},
				"host":               {Type: "string", Description: "Database host"},
				"hostaddr":           {Type: "string", Description: "Database host IP address"},
				"port":               {Type: "integer", Description: "Database port"},
				"database_name":      {Type: "string", Description: "Default database name"},
				"username":           {Type: "string", Description: "Database username"},
				"password":           {Type: "string", Description: "Database password"},
				"ssl_mode":           {Type: "string", Description: "SSL mode"},
				"ssl_cert_path":      {Type: "string", Description: "SSL certificate path"},
				"ssl_key_path":       {Type: "string", Description: "SSL key path"},
				"ssl_root_cert_path": {Type: "string", Description: "SSL root certificate path"},
				"is_shared":          {Type: "boolean", Description: "Whether the connection is shared"},
				"is_monitored":       {Type: "boolean", Description: "Whether the connection is monitored"},
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
				"id":               {Type: "string", Description: "Cluster ID"},
				"group_id":         {Type: "string", Description: "Parent group ID"},
				"name":             {Type: "string", Description: "Cluster name"},
				"description":      {Type: "string", Description: "Cluster description", Nullable: true},
				"replication_type": {Type: "string", Description: "Replication type for the cluster", Nullable: true},
				"created_at":       {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"updated_at":       {Type: "string", Format: "date-time", Description: "Last update timestamp"},
			},
		},
		"ClusterRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":             {Type: "string", Description: "Cluster name"},
				"description":      {Type: "string", Description: "Cluster description"},
				"group_id":         {Type: "integer", Description: "Group ID to assign cluster to"},
				"replication_type": {Type: "string", Description: "Replication type for the cluster"},
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
				"id":               {Type: "string", Description: "Cluster ID"},
				"name":             {Type: "string", Description: "Cluster name"},
				"description":      {Type: "string", Description: "Cluster description", Nullable: true},
				"cluster_type":     {Type: "string", Description: "Auto-detected cluster type (spock, spock_ha, binary, logical, server, manual)"},
				"replication_type": {Type: "string", Description: "User-set replication type from the database", Nullable: true},
				"auto_cluster_key": {Type: "string", Description: "Auto-detection key for the cluster", Nullable: true},
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
				"last_updated":    {Type: "string", Format: "date-time", Description: "When the alert state was last updated by the alerter (severity changes, metric value updates, reactivation)", Nullable: true},
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
		"StatusResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"status": {Type: "string", Description: "Status message", Example: "ok"},
			},
		},
		"AlertRule": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":                 {Type: "integer", Format: "int64", Description: "Alert rule ID"},
				"name":               {Type: "string", Description: "Rule name"},
				"description":        {Type: "string", Description: "Rule description"},
				"category":           {Type: "string", Description: "Rule category"},
				"metric_name":        {Type: "string", Description: "Metric name"},
				"metric_unit":        {Type: "string", Description: "Metric unit", Nullable: true},
				"default_operator":   {Type: "string", Description: "Default comparison operator"},
				"default_threshold":  {Type: "number", Description: "Default threshold value"},
				"default_severity":   {Type: "string", Description: "Default severity level", Enum: []string{"info", "warning", "critical"}},
				"default_enabled":    {Type: "boolean", Description: "Default enabled state"},
				"required_extension": {Type: "string", Description: "Required PostgreSQL extension", Nullable: true},
				"is_built_in":        {Type: "boolean", Description: "Whether the rule is built-in"},
				"created_at":         {Type: "string", Format: "date-time", Description: "Creation timestamp"},
			},
		},
		"AlertOverride": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"rule_id":            {Type: "integer", Format: "int64", Description: "Alert rule ID"},
				"name":               {Type: "string", Description: "Rule name"},
				"description":        {Type: "string", Description: "Rule description"},
				"category":           {Type: "string", Description: "Rule category"},
				"metric_name":        {Type: "string", Description: "Metric name"},
				"metric_unit":        {Type: "string", Description: "Metric unit", Nullable: true},
				"default_operator":   {Type: "string", Description: "Default comparison operator"},
				"default_threshold":  {Type: "number", Description: "Default threshold value"},
				"default_severity":   {Type: "string", Description: "Default severity level", Enum: []string{"info", "warning", "critical"}},
				"default_enabled":    {Type: "boolean", Description: "Default enabled state"},
				"has_override":       {Type: "boolean", Description: "Whether an override exists at this scope"},
				"override_operator":  {Type: "string", Description: "Override comparison operator", Nullable: true},
				"override_threshold": {Type: "number", Description: "Override threshold value", Nullable: true},
				"override_severity":  {Type: "string", Description: "Override severity level", Nullable: true},
				"override_enabled":   {Type: "boolean", Description: "Override enabled state", Nullable: true},
			},
		},
		"AlertOverrideUpdate": {
			Type:     "object",
			Required: []string{"operator", "threshold", "severity", "enabled"},
			Properties: map[string]*OpenAPISchema{
				"operator":  {Type: "string", Description: "Comparison operator", Enum: []string{">", ">=", "<", "<=", "==", "!="}},
				"threshold": {Type: "number", Description: "Threshold value"},
				"severity":  {Type: "string", Description: "Severity level", Enum: []string{"info", "warning", "critical"}},
				"enabled":   {Type: "boolean", Description: "Whether the rule is enabled"},
			},
		},
		"OverrideDetail": {
			Type:     "object",
			Nullable: true,
			Properties: map[string]*OpenAPISchema{
				"operator":  {Type: "string", Description: "Comparison operator"},
				"threshold": {Type: "number", Description: "Threshold value"},
				"severity":  {Type: "string", Description: "Severity level"},
				"enabled":   {Type: "boolean", Description: "Whether the rule is enabled"},
			},
		},
		"OverrideContextResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"hierarchy": {
					Type: "object",
					Properties: map[string]*OpenAPISchema{
						"connection_id": {Type: "integer", Description: "Server connection ID"},
						"cluster_id":    {Type: "integer", Description: "Cluster ID", Nullable: true},
						"group_id":      {Type: "integer", Description: "Group ID", Nullable: true},
						"server_name":   {Type: "string", Description: "Server name"},
						"cluster_name":  {Type: "string", Description: "Cluster name", Nullable: true},
						"group_name":    {Type: "string", Description: "Group name", Nullable: true},
					},
				},
				"rule": {Ref: "#/components/schemas/AlertRule"},
				"overrides": {
					Type: "object",
					Properties: map[string]*OpenAPISchema{
						"server":  {Ref: "#/components/schemas/OverrideDetail"},
						"cluster": {Ref: "#/components/schemas/OverrideDetail"},
						"group":   {Ref: "#/components/schemas/OverrideDetail"},
					},
				},
			},
		},

		// --- New schemas for missing endpoints ---

		"CapabilitiesResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"ai_enabled":     {Type: "boolean", Description: "Whether AI features are enabled"},
				"max_iterations": {Type: "integer", Description: "Maximum LLM tool-use iterations"},
			},
		},
		"AlertRuleUpdate": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"default_operator":  {Type: "string", Description: "Default comparison operator"},
				"default_threshold": {Type: "number", Description: "Default threshold value"},
				"default_severity":  {Type: "string", Description: "Default severity level", Enum: []string{"info", "warning", "critical"}},
				"default_enabled":   {Type: "boolean", Description: "Default enabled state"},
			},
		},
		"SaveAnalysisRequest": {
			Type:     "object",
			Required: []string{"alert_id", "analysis"},
			Properties: map[string]*OpenAPISchema{
				"alert_id":     {Type: "integer", Format: "int64", Description: "Alert ID"},
				"analysis":     {Type: "string", Description: "AI analysis text"},
				"metric_value": {Type: "number", Description: "Metric value at time of analysis"},
			},
		},
		"AddServerToClusterRequest": {
			Type:     "object",
			Required: []string{"connection_id"},
			Properties: map[string]*OpenAPISchema{
				"connection_id": {Type: "integer", Description: "Connection ID of the server to add"},
				"role":          {Type: "string", Description: "Server role in the cluster"},
			},
		},
		"Blackout": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":            {Type: "integer", Format: "int64", Description: "Blackout ID"},
				"scope":         {Type: "string", Description: "Blackout scope (estate, group, cluster, server)"},
				"group_id":      {Type: "integer", Description: "Group ID", Nullable: true},
				"cluster_id":    {Type: "integer", Description: "Cluster ID", Nullable: true},
				"connection_id": {Type: "integer", Description: "Connection ID", Nullable: true},
				"database_name": {Type: "string", Description: "Database name", Nullable: true},
				"reason":        {Type: "string", Description: "Reason for blackout"},
				"start_time":    {Type: "string", Format: "date-time", Description: "Blackout start time"},
				"end_time":      {Type: "string", Format: "date-time", Description: "Blackout end time"},
				"created_by":    {Type: "string", Description: "Username who created the blackout"},
				"created_at":    {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"is_active":     {Type: "boolean", Description: "Whether the blackout is currently active"},
			},
		},
		"BlackoutCreateRequest": {
			Type:     "object",
			Required: []string{"scope", "reason", "start_time", "end_time"},
			Properties: map[string]*OpenAPISchema{
				"scope":         {Type: "string", Description: "Blackout scope (estate, group, cluster, server)"},
				"group_id":      {Type: "integer", Description: "Group ID (required for group scope)", Nullable: true},
				"cluster_id":    {Type: "integer", Description: "Cluster ID (required for cluster scope)", Nullable: true},
				"connection_id": {Type: "integer", Description: "Connection ID (required for server scope)", Nullable: true},
				"database_name": {Type: "string", Description: "Database name", Nullable: true},
				"reason":        {Type: "string", Description: "Reason for blackout"},
				"start_time":    {Type: "string", Format: "date-time", Description: "Blackout start time (RFC3339)"},
				"end_time":      {Type: "string", Format: "date-time", Description: "Blackout end time (RFC3339)"},
			},
		},
		"BlackoutUpdateRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"reason":   {Type: "string", Description: "Updated reason"},
				"end_time": {Type: "string", Format: "date-time", Description: "Updated end time (RFC3339)"},
			},
		},
		"BlackoutSchedule": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":               {Type: "integer", Format: "int64", Description: "Schedule ID"},
				"scope":            {Type: "string", Description: "Schedule scope"},
				"group_id":         {Type: "integer", Description: "Group ID", Nullable: true},
				"cluster_id":       {Type: "integer", Description: "Cluster ID", Nullable: true},
				"connection_id":    {Type: "integer", Description: "Connection ID", Nullable: true},
				"database_name":    {Type: "string", Description: "Database name", Nullable: true},
				"name":             {Type: "string", Description: "Schedule name"},
				"cron_expression":  {Type: "string", Description: "Cron expression for scheduling"},
				"duration_minutes": {Type: "integer", Description: "Duration in minutes"},
				"timezone":         {Type: "string", Description: "Timezone for the schedule"},
				"reason":           {Type: "string", Description: "Reason for blackout"},
				"enabled":          {Type: "boolean", Description: "Whether the schedule is enabled"},
				"created_by":       {Type: "string", Description: "Username who created the schedule"},
				"created_at":       {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"updated_at":       {Type: "string", Format: "date-time", Description: "Last update timestamp"},
			},
		},
		"BlackoutScheduleRequest": {
			Type:     "object",
			Required: []string{"scope", "name", "cron_expression", "duration_minutes", "reason"},
			Properties: map[string]*OpenAPISchema{
				"scope":            {Type: "string", Description: "Schedule scope (estate, group, cluster, server)"},
				"group_id":         {Type: "integer", Description: "Group ID", Nullable: true},
				"cluster_id":       {Type: "integer", Description: "Cluster ID", Nullable: true},
				"connection_id":    {Type: "integer", Description: "Connection ID", Nullable: true},
				"database_name":    {Type: "string", Description: "Database name", Nullable: true},
				"name":             {Type: "string", Description: "Schedule name"},
				"cron_expression":  {Type: "string", Description: "Cron expression for scheduling"},
				"duration_minutes": {Type: "integer", Description: "Duration in minutes"},
				"timezone":         {Type: "string", Description: "Timezone (defaults to UTC)"},
				"reason":           {Type: "string", Description: "Reason for blackout"},
				"enabled":          {Type: "boolean", Description: "Whether the schedule is enabled"},
			},
		},
		"ProbeConfig": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":                          {Type: "integer", Description: "Probe config ID"},
				"connection_id":               {Type: "integer", Description: "Connection ID", Nullable: true},
				"is_enabled":                  {Type: "boolean", Description: "Whether the probe is enabled"},
				"name":                        {Type: "string", Description: "Probe name"},
				"description":                 {Type: "string", Description: "Probe description"},
				"collection_interval_seconds": {Type: "integer", Description: "Collection interval in seconds"},
				"retention_days":              {Type: "integer", Description: "Data retention in days"},
				"created_at":                  {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"updated_at":                  {Type: "string", Format: "date-time", Description: "Last update timestamp"},
			},
		},
		"ProbeConfigUpdate": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"is_enabled":                  {Type: "boolean", Description: "Whether the probe is enabled"},
				"collection_interval_seconds": {Type: "integer", Description: "Collection interval in seconds"},
				"retention_days":              {Type: "integer", Description: "Data retention in days"},
			},
		},
		"ProbeOverride": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":                      {Type: "string", Description: "Probe name"},
				"description":               {Type: "string", Description: "Probe description"},
				"default_enabled":           {Type: "boolean", Description: "Default enabled state"},
				"default_interval_seconds":  {Type: "integer", Description: "Default collection interval"},
				"default_retention_days":    {Type: "integer", Description: "Default retention days"},
				"has_override":              {Type: "boolean", Description: "Whether an override exists at this scope"},
				"override_enabled":          {Type: "boolean", Description: "Override enabled state", Nullable: true},
				"override_interval_seconds": {Type: "integer", Description: "Override collection interval", Nullable: true},
				"override_retention_days":   {Type: "integer", Description: "Override retention days", Nullable: true},
			},
		},
		"ProbeOverrideUpdate": {
			Type:     "object",
			Required: []string{"is_enabled", "collection_interval_seconds", "retention_days"},
			Properties: map[string]*OpenAPISchema{
				"is_enabled":                  {Type: "boolean", Description: "Whether the probe is enabled"},
				"collection_interval_seconds": {Type: "integer", Description: "Collection interval in seconds"},
				"retention_days":              {Type: "integer", Description: "Data retention in days"},
			},
		},
		"NotificationChannel": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":                      {Type: "integer", Format: "int64", Description: "Channel ID"},
				"owner_username":          {Type: "string", Description: "Owner username", Nullable: true},
				"enabled":                 {Type: "boolean", Description: "Whether the channel is enabled"},
				"channel_type":            {Type: "string", Description: "Channel type (email, slack, mattermost, webhook)"},
				"name":                    {Type: "string", Description: "Channel name"},
				"description":             {Type: "string", Description: "Channel description", Nullable: true},
				"endpoint_url":            {Type: "string", Description: "Endpoint URL (webhook)", Nullable: true},
				"http_method":             {Type: "string", Description: "HTTP method for webhook"},
				"smtp_host":               {Type: "string", Description: "SMTP host (email)", Nullable: true},
				"smtp_port":               {Type: "integer", Description: "SMTP port (email)"},
				"from_address":            {Type: "string", Description: "From address (email)", Nullable: true},
				"from_name":               {Type: "string", Description: "From name (email)", Nullable: true},
				"is_estate_default":       {Type: "boolean", Description: "Whether this is an estate default channel"},
				"reminder_enabled":        {Type: "boolean", Description: "Whether reminders are enabled"},
				"reminder_interval_hours": {Type: "integer", Description: "Reminder interval in hours"},
				// Boolean indicators that flag whether each secret is
				// configured. The actual secret values (webhook_url,
				// auth_credentials, smtp_username, smtp_password) are
				// never returned by the API; clients use these flags
				// to render a "configured" badge or to decide whether
				// to show a "leave unchanged" affordance on edit.
				"webhook_url_set":      {Type: "boolean", Description: "Whether a webhook URL is configured"},
				"auth_credentials_set": {Type: "boolean", Description: "Whether webhook auth credentials are configured"},
				"smtp_username_set":    {Type: "boolean", Description: "Whether an SMTP username is configured"},
				"smtp_password_set":    {Type: "boolean", Description: "Whether an SMTP password is configured"},
				"created_at":           {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"updated_at":           {Type: "string", Format: "date-time", Description: "Last update timestamp"},
			},
		},
		"NotificationChannelCreateRequest": {
			Type:     "object",
			Required: []string{"channel_type", "name"},
			Properties: map[string]*OpenAPISchema{
				"channel_type":      {Type: "string", Description: "Channel type (email, slack, mattermost, webhook)"},
				"name":              {Type: "string", Description: "Channel name"},
				"description":       {Type: "string", Description: "Channel description"},
				"enabled":           {Type: "boolean", Description: "Whether the channel is enabled"},
				"is_estate_default": {Type: "boolean", Description: "Whether this is an estate default channel"},
				// Secret fields: on PUT, omit the field to keep the
				// existing value, send "" to clear, or send a non-empty
				// string to replace. The corresponding GET response
				// never echoes these values; use the *_set indicators
				// on the response to detect whether they are configured.
				"webhook_url":             {Type: "string", Description: "Webhook URL (Slack/Mattermost). On PUT: omit to keep existing, empty string to clear, value to replace."},
				"endpoint_url":            {Type: "string", Description: "Endpoint URL (webhook)"},
				"http_method":             {Type: "string", Description: "HTTP method for webhook"},
				"headers":                 {Type: "object", Description: "HTTP headers for webhook", AdditionalProperties: &OpenAPISchema{Type: "string"}},
				"auth_type":               {Type: "string", Description: "Webhook auth type (e.g. bearer, basic)"},
				"auth_credentials":        {Type: "string", Description: "Webhook auth credentials. On PUT: omit to keep existing, empty string to clear, value to replace."},
				"smtp_host":               {Type: "string", Description: "SMTP host (email)"},
				"smtp_port":               {Type: "integer", Description: "SMTP port (email)"},
				"smtp_username":           {Type: "string", Description: "SMTP username (email). On PUT: omit to keep existing, empty string to clear, value to replace."},
				"smtp_password":           {Type: "string", Description: "SMTP password (email). On PUT: omit to keep existing, empty string to clear, value to replace."},
				"smtp_use_tls":            {Type: "boolean", Description: "Use TLS for SMTP (email)"},
				"from_address":            {Type: "string", Description: "From address (email)"},
				"from_name":               {Type: "string", Description: "From name (email)"},
				"reminder_enabled":        {Type: "boolean", Description: "Whether reminders are enabled"},
				"reminder_interval_hours": {Type: "integer", Description: "Reminder interval in hours"},
			},
		},
		"EmailRecipient": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":            {Type: "integer", Format: "int64", Description: "Recipient ID"},
				"channel_id":    {Type: "integer", Format: "int64", Description: "Parent channel ID"},
				"email_address": {Type: "string", Description: "Email address"},
				"display_name":  {Type: "string", Description: "Display name", Nullable: true},
				"enabled":       {Type: "boolean", Description: "Whether the recipient is enabled"},
				"created_at":    {Type: "string", Format: "date-time", Description: "Creation timestamp"},
			},
		},
		"EmailRecipientRequest": {
			Type:     "object",
			Required: []string{"email_address"},
			Properties: map[string]*OpenAPISchema{
				"email_address": {Type: "string", Description: "Email address"},
				"display_name":  {Type: "string", Description: "Display name"},
				"enabled":       {Type: "boolean", Description: "Whether the recipient is enabled"},
			},
		},
		"TestChannelRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"recipient_email": {Type: "string", Description: "Optional recipient email for testing"},
			},
		},
		"ChannelOverride": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"channel_id":        {Type: "integer", Format: "int64", Description: "Channel ID"},
				"channel_name":      {Type: "string", Description: "Channel name"},
				"channel_type":      {Type: "string", Description: "Channel type"},
				"description":       {Type: "string", Description: "Channel description", Nullable: true},
				"is_estate_default": {Type: "boolean", Description: "Whether this is an estate default"},
				"has_override":      {Type: "boolean", Description: "Whether an override exists at this scope"},
				"override_enabled":  {Type: "boolean", Description: "Override enabled state", Nullable: true},
			},
		},
		"ChannelOverrideUpdate": {
			Type:     "object",
			Required: []string{"enabled"},
			Properties: map[string]*OpenAPISchema{
				"enabled": {Type: "boolean", Description: "Whether the channel is enabled at this scope"},
			},
		},
		"ServerInfoResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"connection_id": {Type: "integer", Description: "Connection ID"},
				"collected_at":  {Type: "string", Format: "date-time", Description: "Data collection timestamp", Nullable: true},
				"system":        {Type: "object", Description: "System information (OS, CPU, memory, disks)"},
				"postgresql":    {Type: "object", Description: "PostgreSQL server configuration"},
				"databases":     {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "Database information"},
				"extensions":    {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "Installed extensions"},
				"key_settings":  {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "Key PostgreSQL settings"},
				"ai_analysis":   {Type: "object", Description: "AI-generated database analysis", Nullable: true},
			},
		},
		"AIAnalysisResponse": {
			Type:     "object",
			Nullable: true,
			Properties: map[string]*OpenAPISchema{
				"databases":    {Type: "object", Description: "Per-database AI analysis", AdditionalProperties: &OpenAPISchema{Type: "string"}},
				"generated_at": {Type: "string", Format: "date-time", Description: "When the analysis was generated"},
			},
		},
		"MetricsQueryResult": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"probe_name":     {Type: "string", Description: "Probe name"},
				"connection_ids": {Type: "array", Items: &OpenAPISchema{Type: "integer"}, Description: "Connection IDs queried"},
				"time_range":     {Type: "string", Description: "Time range"},
				"buckets":        {Type: "integer", Description: "Number of time buckets"},
				"aggregation":    {Type: "string", Description: "Aggregation method"},
				"series":         {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "Time series data"},
			},
		},
		"BaselinesResult": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"connection_id": {Type: "integer", Description: "Connection ID"},
				"probe_name":    {Type: "string", Description: "Probe name"},
				"baselines":     {Type: "object", Description: "Baseline values per metric", AdditionalProperties: &OpenAPISchema{Type: "object"}},
			},
		},
		"PerfSummaryResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"time_range":  {Type: "string", Description: "Time range used"},
				"connections": {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "Per-connection performance data"},
				"aggregate":   {Type: "object", Description: "Aggregate performance metrics", Nullable: true},
			},
		},
		"DatabaseSummaryResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"connection_id": {Type: "integer", Description: "Connection ID"},
				"time_range":    {Type: "string", Description: "Time range used"},
				"databases":     {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "Per-database summary data"},
			},
		},
		"TopQueryRow": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"queryid":          {Type: "integer", Format: "int64", Description: "Query ID"},
				"database_name":    {Type: "string", Description: "Database name where the query was executed"},
				"query":            {Type: "string", Description: "Query text"},
				"calls":            {Type: "integer", Format: "int64", Description: "Total call count"},
				"total_exec_time":  {Type: "number", Description: "Total execution time in ms"},
				"mean_exec_time":   {Type: "number", Description: "Mean execution time in ms"},
				"rows":             {Type: "integer", Format: "int64", Description: "Total rows returned"},
				"shared_blks_hit":  {Type: "integer", Format: "int64", Description: "Shared blocks hit"},
				"shared_blks_read": {Type: "integer", Format: "int64", Description: "Shared blocks read"},
			},
		},
		"LatestSnapshotResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"rows": {
					Type:        "array",
					Description: "Latest snapshot rows, one per entity",
					Items: &OpenAPISchema{
						Type:                 "object",
						AdditionalProperties: &OpenAPISchema{},
						Description:          "Row with column names as keys",
					},
				},
				"total_count": {
					Type:        "integer",
					Description: "Total number of distinct entities before pagination",
				},
			},
			Required: []string{"rows", "total_count"},
		},
		"OverviewResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"status":       {Type: "string", Description: "Overview status (ready, generating)"},
				"summary":      {Type: "object", Description: "AI-generated summary", Nullable: true},
				"generated_at": {Type: "string", Format: "date-time", Description: "When the overview was generated"},
			},
		},
		"Memory": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":         {Type: "integer", Format: "int64", Description: "Memory ID"},
				"username":   {Type: "string", Description: "Owner username"},
				"scope":      {Type: "string", Description: "Memory scope (user, system)"},
				"category":   {Type: "string", Description: "Memory category"},
				"content":    {Type: "string", Description: "Memory content"},
				"pinned":     {Type: "boolean", Description: "Whether the memory is pinned"},
				"model_name": {Type: "string", Description: "Model that created the memory"},
				"created_at": {Type: "string", Format: "date-time", Description: "Creation timestamp"},
				"updated_at": {Type: "string", Format: "date-time", Description: "Last update timestamp"},
			},
		},
		"MemoryListResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"memories": {Type: "array", Items: &OpenAPISchema{Ref: "#/components/schemas/Memory"}},
			},
		},
		"MCPTool": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":        {Type: "string", Description: "Tool name"},
				"description": {Type: "string", Description: "Tool description"},
				"inputSchema": {Type: "object", Description: "JSON Schema for tool input"},
			},
		},
		"MCPToolListResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"tools": {Type: "array", Items: &OpenAPISchema{Ref: "#/components/schemas/MCPTool"}},
			},
		},
		"MCPToolCallRequest": {
			Type:     "object",
			Required: []string{"name"},
			Properties: map[string]*OpenAPISchema{
				"name":      {Type: "string", Description: "Tool name to execute"},
				"arguments": {Type: "object", Description: "Tool arguments"},
			},
		},
		"MCPToolCallResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"content": {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "Tool response content"},
				"isError": {Type: "boolean", Description: "Whether the tool call resulted in an error"},
			},
		},
		"RBACUser": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":                 {Type: "integer", Format: "int64", Description: "User ID"},
				"username":           {Type: "string", Description: "Username"},
				"display_name":       {Type: "string", Description: "Display name"},
				"email":              {Type: "string", Description: "Email address"},
				"enabled":            {Type: "boolean", Description: "Whether the user is enabled"},
				"is_superuser":       {Type: "boolean", Description: "Whether the user is a superuser"},
				"is_service_account": {Type: "boolean", Description: "Whether the user is a service account"},
				"annotation":         {Type: "string", Description: "User annotation"},
			},
		},
		"UserCreateRequest": {
			Type:     "object",
			Required: []string{"username"},
			Properties: map[string]*OpenAPISchema{
				"username":           {Type: "string", Description: "Username"},
				"password":           {Type: "string", Description: "Password (required for non-service accounts)"},
				"display_name":       {Type: "string", Description: "Display name"},
				"email":              {Type: "string", Description: "Email address"},
				"annotation":         {Type: "string", Description: "User annotation"},
				"enabled":            {Type: "boolean", Description: "Whether the user is enabled"},
				"is_superuser":       {Type: "boolean", Description: "Whether the user is a superuser"},
				"is_service_account": {Type: "boolean", Description: "Whether the user is a service account"},
			},
		},
		"UserUpdateRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"password":     {Type: "string", Description: "New password"},
				"display_name": {Type: "string", Description: "Display name"},
				"email":        {Type: "string", Description: "Email address"},
				"annotation":   {Type: "string", Description: "User annotation"},
				"enabled":      {Type: "boolean", Description: "Whether the user is enabled"},
				"is_superuser": {Type: "boolean", Description: "Whether the user is a superuser"},
			},
		},
		"UserPrivilegesResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"username":              {Type: "string", Description: "Username"},
				"is_superuser":          {Type: "boolean", Description: "Whether the user is a superuser"},
				"groups":                {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "Group names"},
				"mcp_privileges":        {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "MCP privilege identifiers"},
				"connection_privileges": {Type: "object", Description: "Connection ID to access level mapping", AdditionalProperties: &OpenAPISchema{Type: "string"}},
				"admin_permissions":     {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "Admin permission names"},
			},
		},
		"RBACGroup": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":           {Type: "integer", Format: "int64", Description: "Group ID"},
				"name":         {Type: "string", Description: "Group name"},
				"description":  {Type: "string", Description: "Group description"},
				"member_count": {Type: "integer", Description: "Number of members"},
			},
		},
		"RBACGroupDetail": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":                    {Type: "integer", Format: "int64", Description: "Group ID"},
				"name":                  {Type: "string", Description: "Group name"},
				"description":           {Type: "string", Description: "Group description"},
				"user_members":          {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "User member names"},
				"group_members":         {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "Group member names"},
				"mcp_privileges":        {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "MCP privileges"},
				"connection_privileges": {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "Connection privileges"},
				"admin_permissions":     {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "Admin permissions"},
			},
		},
		"GroupCreateRequest": {
			Type:     "object",
			Required: []string{"name"},
			Properties: map[string]*OpenAPISchema{
				"name":        {Type: "string", Description: "Group name"},
				"description": {Type: "string", Description: "Group description"},
			},
		},
		"GroupUpdateRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"name":        {Type: "string", Description: "Group name"},
				"description": {Type: "string", Description: "Group description"},
			},
		},
		"GroupMemberRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"user_id":  {Type: "integer", Format: "int64", Description: "User ID to add"},
				"group_id": {Type: "integer", Format: "int64", Description: "Group ID to add (nested group)"},
			},
		},
		"GroupEffectivePrivileges": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"group_name":            {Type: "string", Description: "Group name"},
				"mcp_privileges":        {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "Effective MCP privilege names"},
				"connection_privileges": {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "Effective connection privileges"},
				"admin_permissions":     {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "Effective admin permissions"},
			},
		},
		"GroupPermissionsResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"group_id":    {Type: "integer", Format: "int64", Description: "Group ID"},
				"permissions": {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "Admin permissions"},
			},
		},
		"RBACToken": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"id":                 {Type: "integer", Format: "int64", Description: "Token ID"},
				"name":               {Type: "string", Description: "Token annotation/name"},
				"token_prefix":       {Type: "string", Description: "Token hash prefix for identification"},
				"user_id":            {Type: "integer", Format: "int64", Description: "Owner user ID"},
				"username":           {Type: "string", Description: "Owner username"},
				"is_service_account": {Type: "boolean", Description: "Whether the owner is a service account"},
				"is_superuser":       {Type: "boolean", Description: "Whether the owner is a superuser"},
				"expires_at":         {Type: "string", Format: "date-time", Description: "Token expiration time", Nullable: true},
				"scope":              {Type: "object", Description: "Token scope restrictions", Nullable: true},
			},
		},
		"TokenCreateRequest": {
			Type:     "object",
			Required: []string{"owner_username", "annotation"},
			Properties: map[string]*OpenAPISchema{
				"owner_username": {Type: "string", Description: "Username of the token owner"},
				"annotation":     {Type: "string", Description: "Token description/name"},
				"expires_in":     {Type: "string", Description: "Expiry duration (e.g., 24h, 30d, 1y, never)"},
			},
		},
		"TokenCreateResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"token":      {Type: "string", Description: "Raw token value (shown only once)"},
				"id":         {Type: "integer", Format: "int64", Description: "Token ID"},
				"owner":      {Type: "string", Description: "Owner username"},
				"annotation": {Type: "string", Description: "Token annotation"},
				"expires_at": {Type: "string", Format: "date-time", Description: "Token expiration time", Nullable: true},
				"message":    {Type: "string", Description: "Status message"},
			},
		},
		"TokenScopeResponse": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"token_id":          {Type: "integer", Format: "int64", Description: "Token ID"},
				"scoped":            {Type: "boolean", Description: "Whether the token has scope restrictions"},
				"connections":       {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "Connection scope entries"},
				"mcp_privileges":    {Type: "array", Items: &OpenAPISchema{Type: "integer", Format: "int64"}, Description: "MCP privilege IDs"},
				"admin_permissions": {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "Admin permission names"},
			},
		},
		"TokenScopeRequest": {
			Type: "object",
			Properties: map[string]*OpenAPISchema{
				"connections":       {Type: "array", Items: &OpenAPISchema{Type: "object"}, Description: "Connection scope entries"},
				"mcp_privileges":    {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "MCP privilege names"},
				"admin_permissions": {Type: "array", Items: &OpenAPISchema{Type: "string"}, Description: "Admin permission names"},
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
					"403": jsonResponse("ErrorResponse", "Access denied"),
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
					"403": jsonResponse("ErrorResponse", "Access denied"),
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
					"403": jsonResponse("ErrorResponse", "Access denied"),
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
			Post: &OpenAPIOperation{
				Summary:     "Create a cluster",
				Description: "Creates a new cluster, optionally assigned to a group",
				OperationID: "createCluster",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("ClusterRequest", "Cluster details", true),
				Responses: map[string]OpenAPIResponse{
					"201": jsonResponse("Cluster", "Cluster created"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Forbidden"),
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
				Description: "Deletes a cluster. Manually created clusters are hard-deleted. Auto-detected clusters are soft-deleted by setting dismissed=true so they do not reappear in topology. This applies to persisted auto-detected clusters addressed by numeric ID and to transient auto-detected IDs (server-{id} or cluster-spock-{prefix}).",
				OperationID: "deleteCluster",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamString("id", "Cluster ID. Accepts a numeric cluster ID, server-{id}, or cluster-spock-{prefix}.")},
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
			Post: &OpenAPIOperation{
				Summary:     "Add server to cluster",
				Description: "Assigns a server connection to a cluster with an optional role",
				OperationID: "addServerToCluster",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Cluster ID")},
				RequestBody: jsonRequestBody("AddServerToClusterRequest", "Server assignment details", true),
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Server added to cluster"},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Forbidden"),
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
					"403": jsonResponse("ErrorResponse", "Access denied"),
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
					"403": jsonResponse("ErrorResponse", "Access denied"),
					"500": jsonResponse("ErrorResponse", "Failed to unacknowledge"),
				},
			},
		},

		// Alert Overrides
		"/alert-overrides/{scope}/{scopeId}": {
			Get: &OpenAPIOperation{
				Summary:     "List alert overrides",
				Description: "Returns all alert rules with their override values at the specified scope",
				OperationID: "listAlertOverrides",
				Tags:        []string{"Alert Overrides"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamString("scope", "Override scope (server, cluster, or group)"),
					pathParamInt("scopeId", "Scope entity ID"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("AlertOverride", "List of alert rules with overrides"),
					"400": jsonResponse("ErrorResponse", "Invalid scope"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"503": jsonResponse("ErrorResponse", "Datastore not configured"),
				},
			},
		},
		"/alert-overrides/{scope}/{scopeId}/{ruleId}": {
			Put: &OpenAPIOperation{
				Summary:     "Create or update alert override",
				Description: "Creates or updates an alert threshold override at the specified scope for a rule",
				OperationID: "upsertAlertOverride",
				Tags:        []string{"Alert Overrides"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamString("scope", "Override scope (server, cluster, or group)"),
					pathParamInt("scopeId", "Scope entity ID"),
					pathParamInt("ruleId", "Alert rule ID"),
				},
				RequestBody: jsonRequestBody("AlertOverrideUpdate", "Override values", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("StatusResponse", "Override saved"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Permission denied"),
					"503": jsonResponse("ErrorResponse", "Datastore not configured"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete alert override",
				Description: "Removes an alert threshold override, reverting to the inherited or default value",
				OperationID: "deleteAlertOverride",
				Tags:        []string{"Alert Overrides"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamString("scope", "Override scope (server, cluster, or group)"),
					pathParamInt("scopeId", "Scope entity ID"),
					pathParamInt("ruleId", "Alert rule ID"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("StatusResponse", "Override deleted"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Permission denied"),
					"503": jsonResponse("ErrorResponse", "Datastore not configured"),
				},
			},
		},
		"/alert-overrides/context/{connectionId}/{ruleId}": {
			Get: &OpenAPIOperation{
				Summary:     "Get override context for editing",
				Description: "Returns the connection hierarchy, rule defaults, and existing overrides at all applicable scopes for a given connection and rule. Used by the alert override edit dialog.",
				OperationID: "getAlertOverrideContext",
				Tags:        []string{"Alert Overrides"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamInt("connectionId", "Server connection ID"),
					pathParamInt("ruleId", "Alert rule ID"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("OverrideContextResponse", "Override context with hierarchy and existing overrides"),
					"400": jsonResponse("ErrorResponse", "Invalid parameters"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Permission denied"),
					"500": jsonResponse("ErrorResponse", "Internal server error"),
					"503": jsonResponse("ErrorResponse", "Datastore not configured"),
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

		// Auth - Logout
		"/auth/logout": {
			Post: &OpenAPIOperation{
				Summary:     "User logout",
				Description: "Clears the session cookie to log the user out",
				OperationID: "logout",
				Tags:        []string{"Authentication"},
				Responses: map[string]OpenAPIResponse{
					"200": {Description: "Logout successful"},
					"405": {Description: "Method not allowed"},
				},
			},
		},

		// Capabilities
		"/capabilities": {
			Get: &OpenAPIOperation{
				Summary:     "Get server capabilities",
				Description: "Returns server capability flags including AI feature availability",
				OperationID: "getCapabilities",
				Tags:        []string{"Server"},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("CapabilitiesResponse", "Server capabilities"),
				},
			},
		},

		// Clusters - flat list
		"/clusters/list": {
			Get: &OpenAPIOperation{
				Summary:     "List all clusters (flat)",
				Description: "Returns a flat list of all clusters for autocomplete and selection UIs",
				OperationID: "listClustersFlat",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("Cluster", "Flat list of clusters"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		// Clusters - remove server
		"/clusters/{id}/servers/{connectionId}": {
			Delete: &OpenAPIOperation{
				Summary:     "Remove server from cluster",
				Description: "Removes a server connection from a cluster",
				OperationID: "removeServerFromCluster",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamInt("id", "Cluster ID"),
					pathParamInt("connectionId", "Connection ID of server to remove"),
				},
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Server removed from cluster"},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Forbidden"),
				},
			},
		},

		// Clusters - relationships
		"/clusters/{id}/relationships": {
			Get: &OpenAPIOperation{
				Summary:     "List cluster relationships",
				Description: "Returns replication relationships for a cluster",
				OperationID: "listClusterRelationships",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Cluster ID")},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "List of relationships",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "array", Items: &OpenAPISchema{Type: "object"}}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		"/clusters/{id}/relationships/{relationshipId}": {
			Delete: &OpenAPIOperation{
				Summary:     "Delete cluster relationship",
				Description: "Deletes a specific cluster relationship",
				OperationID: "deleteClusterRelationship",
				Tags:        []string{"Clusters"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamInt("id", "Cluster ID"),
					pathParamInt("relationshipId", "Relationship ID"),
				},
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Relationship deleted"},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		// Alerts - save analysis
		"/alerts/analysis": {
			Put: &OpenAPIOperation{
				Summary:     "Save AI analysis for alert",
				Description: "Saves an AI-generated analysis for a specific alert",
				OperationID: "saveAlertAnalysis",
				Tags:        []string{"Alerts"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("SaveAnalysisRequest", "Analysis data", true),
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Analysis saved",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object", Properties: map[string]*OpenAPISchema{"status": {Type: "string"}}}},
						},
					},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Access denied"),
				},
			},
		},

		// Alert Rules
		"/alert-rules": {
			Get: &OpenAPIOperation{
				Summary:     "List all alert rules",
				Description: "Returns all configured alert rules",
				OperationID: "listAlertRules",
				Tags:        []string{"Alert Rules"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("AlertRule", "List of alert rules"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		"/alert-rules/{id}": {
			Get: &OpenAPIOperation{
				Summary:     "Get alert rule",
				Description: "Returns a specific alert rule by ID",
				OperationID: "getAlertRule",
				Tags:        []string{"Alert Rules"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Alert rule ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("AlertRule", "Alert rule details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Alert rule not found"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Update alert rule",
				Description: "Updates an alert rule. Requires manage_alert_rules permission.",
				OperationID: "updateAlertRule",
				Tags:        []string{"Alert Rules"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Alert rule ID")},
				RequestBody: jsonRequestBody("AlertRuleUpdate", "Updated rule fields", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("AlertRule", "Updated alert rule"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_alert_rules permission"),
					"404": jsonResponse("ErrorResponse", "Alert rule not found"),
				},
			},
		},

		// Blackouts
		"/blackouts": {
			Get: &OpenAPIOperation{
				Summary:     "List blackouts",
				Description: "Returns blackouts, optionally filtered by scope and status",
				OperationID: "listBlackouts",
				Tags:        []string{"Blackouts"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamString("scope", "Filter by scope (estate, group, cluster, server)"),
					queryParamInt("group_id", "Filter by group ID"),
					queryParamInt("cluster_id", "Filter by cluster ID"),
					queryParamInt("connection_id", "Filter by connection ID"),
					queryParamBool("active", "Filter by active status"),
					queryParamInt("limit", "Maximum number of results"),
					queryParamInt("offset", "Offset for pagination"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("Blackout", "List of blackouts"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Create a blackout",
				Description: "Creates a new alert blackout window",
				OperationID: "createBlackout",
				Tags:        []string{"Blackouts"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("BlackoutCreateRequest", "Blackout details", true),
				Responses: map[string]OpenAPIResponse{
					"201": jsonResponse("Blackout", "Blackout created"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_blackouts permission"),
				},
			},
		},

		"/blackouts/{id}": {
			Get: &OpenAPIOperation{
				Summary:     "Get blackout",
				Description: "Returns a specific blackout by ID",
				OperationID: "getBlackout",
				Tags:        []string{"Blackouts"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Blackout ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("Blackout", "Blackout details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Blackout not found"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Update blackout",
				Description: "Updates a blackout's reason or end time",
				OperationID: "updateBlackout",
				Tags:        []string{"Blackouts"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Blackout ID")},
				RequestBody: jsonRequestBody("BlackoutUpdateRequest", "Updated blackout fields", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("Blackout", "Updated blackout"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_blackouts permission"),
					"404": jsonResponse("ErrorResponse", "Blackout not found"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete blackout",
				Description: "Deletes a blackout",
				OperationID: "deleteBlackout",
				Tags:        []string{"Blackouts"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Blackout ID")},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Blackout deleted",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object", Properties: map[string]*OpenAPISchema{"status": {Type: "string"}}}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_blackouts permission"),
					"404": jsonResponse("ErrorResponse", "Blackout not found"),
				},
			},
		},

		"/blackouts/{id}/stop": {
			Post: &OpenAPIOperation{
				Summary:     "Stop active blackout",
				Description: "Stops an active blackout early by setting its end time to now",
				OperationID: "stopBlackout",
				Tags:        []string{"Blackouts"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Blackout ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("Blackout", "Stopped blackout"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_blackouts permission"),
					"404": jsonResponse("ErrorResponse", "Blackout not found"),
				},
			},
		},

		// Blackout Schedules
		"/blackout-schedules": {
			Get: &OpenAPIOperation{
				Summary:     "List blackout schedules",
				Description: "Returns blackout schedules, optionally filtered by scope",
				OperationID: "listBlackoutSchedules",
				Tags:        []string{"Blackouts"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamString("scope", "Filter by scope (estate, group, cluster, server)"),
					queryParamInt("group_id", "Filter by group ID"),
					queryParamInt("cluster_id", "Filter by cluster ID"),
					queryParamInt("connection_id", "Filter by connection ID"),
					queryParamBool("enabled", "Filter by enabled status"),
					queryParamInt("limit", "Maximum number of results"),
					queryParamInt("offset", "Offset for pagination"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("BlackoutSchedule", "List of blackout schedules"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Create blackout schedule",
				Description: "Creates a new recurring blackout schedule",
				OperationID: "createBlackoutSchedule",
				Tags:        []string{"Blackouts"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("BlackoutScheduleRequest", "Schedule details", true),
				Responses: map[string]OpenAPIResponse{
					"201": jsonResponse("BlackoutSchedule", "Schedule created"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_blackouts permission"),
				},
			},
		},

		"/blackout-schedules/{id}": {
			Get: &OpenAPIOperation{
				Summary:     "Get blackout schedule",
				Description: "Returns a specific blackout schedule by ID",
				OperationID: "getBlackoutSchedule",
				Tags:        []string{"Blackouts"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Schedule ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("BlackoutSchedule", "Schedule details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Schedule not found"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Update blackout schedule",
				Description: "Updates a blackout schedule",
				OperationID: "updateBlackoutSchedule",
				Tags:        []string{"Blackouts"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Schedule ID")},
				RequestBody: jsonRequestBody("BlackoutScheduleRequest", "Updated schedule details", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("BlackoutSchedule", "Updated schedule"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_blackouts permission"),
					"404": jsonResponse("ErrorResponse", "Schedule not found"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete blackout schedule",
				Description: "Deletes a blackout schedule",
				OperationID: "deleteBlackoutSchedule",
				Tags:        []string{"Blackouts"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Schedule ID")},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Schedule deleted",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object", Properties: map[string]*OpenAPISchema{"status": {Type: "string"}}}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_blackouts permission"),
					"404": jsonResponse("ErrorResponse", "Schedule not found"),
				},
			},
		},

		// Probe Configuration
		"/probe-configs": {
			Get: &OpenAPIOperation{
				Summary:     "List probe configurations",
				Description: "Returns probe configurations, optionally filtered by connection ID",
				OperationID: "listProbeConfigs",
				Tags:        []string{"Probe Configuration"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{queryParamInt("connection_id", "Filter by connection ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("ProbeConfig", "List of probe configurations"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		"/probe-configs/{id}": {
			Get: &OpenAPIOperation{
				Summary:     "Get probe configuration",
				Description: "Returns a specific probe configuration by ID",
				OperationID: "getProbeConfig",
				Tags:        []string{"Probe Configuration"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Probe config ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("ProbeConfig", "Probe configuration details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Probe config not found"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Update probe configuration",
				Description: "Updates a probe configuration. Requires manage_probes permission.",
				OperationID: "updateProbeConfig",
				Tags:        []string{"Probe Configuration"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Probe config ID")},
				RequestBody: jsonRequestBody("ProbeConfigUpdate", "Updated probe config fields", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("ProbeConfig", "Updated probe configuration"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_probes permission"),
					"404": jsonResponse("ErrorResponse", "Probe config not found"),
				},
			},
		},

		// Probe Overrides
		"/probe-overrides/{scope}/{scopeId}": {
			Get: &OpenAPIOperation{
				Summary:     "List probe overrides",
				Description: "Returns probe overrides for a specific scope (server, cluster, or group)",
				OperationID: "listProbeOverrides",
				Tags:        []string{"Probe Configuration"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamString("scope", "Override scope (server, cluster, or group)"),
					pathParamInt("scopeId", "Scope entity ID"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("ProbeOverride", "List of probe overrides"),
					"400": jsonResponse("ErrorResponse", "Invalid scope"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		"/probe-overrides/{scope}/{scopeId}/{probeName}": {
			Put: &OpenAPIOperation{
				Summary:     "Upsert probe override",
				Description: "Creates or updates a probe override at the specified scope",
				OperationID: "upsertProbeOverride",
				Tags:        []string{"Probe Configuration"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamString("scope", "Override scope (server, cluster, or group)"),
					pathParamInt("scopeId", "Scope entity ID"),
					pathParamString("probeName", "Probe name"),
				},
				RequestBody: jsonRequestBody("ProbeOverrideUpdate", "Override settings", true),
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Override saved",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object", Properties: map[string]*OpenAPISchema{"status": {Type: "string"}}}},
						},
					},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_probes permission"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete probe override",
				Description: "Deletes a probe override at the specified scope",
				OperationID: "deleteProbeOverride",
				Tags:        []string{"Probe Configuration"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamString("scope", "Override scope (server, cluster, or group)"),
					pathParamInt("scopeId", "Scope entity ID"),
					pathParamString("probeName", "Probe name"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Override deleted",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object", Properties: map[string]*OpenAPISchema{"status": {Type: "string"}}}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_probes permission"),
				},
			},
		},

		// Notification Channels
		"/notification-channels": {
			Get: &OpenAPIOperation{
				Summary:     "List notification channels",
				Description: "Returns all notification channels",
				OperationID: "listNotificationChannels",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("NotificationChannel", "List of notification channels"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Create notification channel",
				Description: "Creates a new notification channel (email, Slack, Mattermost, or webhook)",
				OperationID: "createNotificationChannel",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("NotificationChannelCreateRequest", "Channel details", true),
				Responses: map[string]OpenAPIResponse{
					"201": jsonResponse("NotificationChannel", "Channel created"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_notification_channels permission"),
				},
			},
		},

		"/notification-channels/{id}": {
			Get: &OpenAPIOperation{
				Summary:     "Get notification channel",
				Description: "Returns a specific notification channel by ID",
				OperationID: "getNotificationChannel",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Channel ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("NotificationChannel", "Channel details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Channel not found"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Update notification channel",
				Description: "Updates a notification channel",
				OperationID: "updateNotificationChannel",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Channel ID")},
				RequestBody: jsonRequestBody("NotificationChannelCreateRequest", "Updated channel details", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("NotificationChannel", "Updated channel"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_notification_channels permission"),
					"404": jsonResponse("ErrorResponse", "Channel not found"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete notification channel",
				Description: "Deletes a notification channel",
				OperationID: "deleteNotificationChannel",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Channel ID")},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Channel deleted",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object", Properties: map[string]*OpenAPISchema{"status": {Type: "string"}}}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_notification_channels permission"),
					"404": jsonResponse("ErrorResponse", "Channel not found"),
				},
			},
		},

		"/notification-channels/{id}/test": {
			Post: &OpenAPIOperation{
				Summary:     "Test notification channel",
				Description: "Sends a test notification through the channel",
				OperationID: "testNotificationChannel",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Channel ID")},
				RequestBody: jsonRequestBody("TestChannelRequest", "Optional test parameters", false),
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Test notification sent",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object", Properties: map[string]*OpenAPISchema{"status": {Type: "string"}}}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_notification_channels permission"),
					"404": jsonResponse("ErrorResponse", "Channel not found"),
				},
			},
		},

		"/notification-channels/{id}/recipients": {
			Get: &OpenAPIOperation{
				Summary:     "List email recipients",
				Description: "Returns all email recipients for a notification channel",
				OperationID: "listEmailRecipients",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Channel ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("EmailRecipient", "List of email recipients"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Channel not found"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Add email recipient",
				Description: "Adds an email recipient to a notification channel",
				OperationID: "addEmailRecipient",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Channel ID")},
				RequestBody: jsonRequestBody("EmailRecipientRequest", "Recipient details", true),
				Responses: map[string]OpenAPIResponse{
					"201": jsonResponse("EmailRecipient", "Recipient added"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_notification_channels permission"),
				},
			},
		},

		"/notification-channels/{id}/recipients/{recipientId}": {
			Put: &OpenAPIOperation{
				Summary:     "Update email recipient",
				Description: "Updates an email recipient",
				OperationID: "updateEmailRecipient",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamInt("id", "Channel ID"),
					pathParamInt("recipientId", "Recipient ID"),
				},
				RequestBody: jsonRequestBody("EmailRecipientRequest", "Updated recipient details", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("EmailRecipient", "Updated recipient"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_notification_channels permission"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete email recipient",
				Description: "Removes an email recipient from a notification channel",
				OperationID: "deleteEmailRecipient",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamInt("id", "Channel ID"),
					pathParamInt("recipientId", "Recipient ID"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Recipient deleted",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object", Properties: map[string]*OpenAPISchema{"status": {Type: "string"}}}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_notification_channels permission"),
				},
			},
		},

		// Channel Overrides
		"/channel-overrides/{scope}/{scopeId}": {
			Get: &OpenAPIOperation{
				Summary:     "List channel overrides",
				Description: "Returns notification channel overrides for a specific scope",
				OperationID: "listChannelOverrides",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamString("scope", "Override scope (server, cluster, or group)"),
					pathParamInt("scopeId", "Scope entity ID"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("ChannelOverride", "List of channel overrides"),
					"400": jsonResponse("ErrorResponse", "Invalid scope"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		"/channel-overrides/{scope}/{scopeId}/{channelId}": {
			Put: &OpenAPIOperation{
				Summary:     "Upsert channel override",
				Description: "Creates or updates a channel override at the specified scope",
				OperationID: "upsertChannelOverride",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamString("scope", "Override scope (server, cluster, or group)"),
					pathParamInt("scopeId", "Scope entity ID"),
					pathParamInt("channelId", "Channel ID"),
				},
				RequestBody: jsonRequestBody("ChannelOverrideUpdate", "Override settings", true),
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Override saved",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object", Properties: map[string]*OpenAPISchema{"status": {Type: "string"}}}},
						},
					},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_notification_channels permission"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete channel override",
				Description: "Deletes a channel override at the specified scope",
				OperationID: "deleteChannelOverride",
				Tags:        []string{"Notification Channels"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamString("scope", "Override scope (server, cluster, or group)"),
					pathParamInt("scopeId", "Scope entity ID"),
					pathParamInt("channelId", "Channel ID"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Override deleted",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object", Properties: map[string]*OpenAPISchema{"status": {Type: "string"}}}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_notification_channels permission"),
				},
			},
		},

		// Server Info
		"/server-info/{connection_id}": {
			Get: &OpenAPIOperation{
				Summary:     "Get server information",
				Description: "Returns comprehensive server information including OS, PostgreSQL, databases, extensions, and settings",
				OperationID: "getServerInfo",
				Tags:        []string{"Server Info"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("connection_id", "Connection ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("ServerInfoResponse", "Server information"),
					"400": jsonResponse("ErrorResponse", "Invalid connection ID"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Permission denied"),
				},
			},
		},

		"/server-info/{id}/ai-analysis": {
			Get: &OpenAPIOperation{
				Summary:     "Get AI database analysis",
				Description: "Returns AI-generated analysis of databases on the specified server",
				OperationID: "getAIAnalysis",
				Tags:        []string{"Server Info"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Connection ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("AIAnalysisResponse", "AI analysis of databases"),
					"400": jsonResponse("ErrorResponse", "Invalid connection ID"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Permission denied"),
				},
			},
		},

		// Metrics
		"/metrics/query": {
			Get: &OpenAPIOperation{
				Summary:     "Query metrics",
				Description: "Queries time-series metrics for one or more connections",
				OperationID: "queryMetrics",
				Tags:        []string{"Metrics"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamString("connection_ids", "Comma-separated connection IDs"),
					queryParamInt("connection_id", "Single connection ID"),
					queryParamStringRequired("probe_name", "Probe name to query"),
					queryParamString("time_range", "Time range (1h, 6h, 24h, 7d, 30d)"),
					queryParamString("database_name", "Filter by database name"),
					queryParamString("schema_name", "Filter by schema name"),
					queryParamString("table_name", "Filter by table name"),
					queryParamInt("buckets", "Number of time buckets"),
					queryParamString("aggregation", "Aggregation method"),
					queryParamString("metrics", "Comma-separated metric names"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("MetricsQueryResult", "Metrics query results"),
					"400": jsonResponse("ErrorResponse", "Invalid query parameters"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Permission denied"),
				},
			},
		},

		"/metrics/baselines": {
			Get: &OpenAPIOperation{
				Summary:     "Get metric baselines",
				Description: "Returns baseline values for metrics on a specific connection",
				OperationID: "getMetricBaselines",
				Tags:        []string{"Metrics"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamIntRequired("connection_id", "Connection ID"),
					queryParamStringRequired("probe_name", "Probe name"),
					queryParamString("metrics", "Comma-separated metric names"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("BaselinesResult", "Baseline values"),
					"400": jsonResponse("ErrorResponse", "Invalid query parameters"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		"/metrics/performance-summary": {
			Get: &OpenAPIOperation{
				Summary:     "Get performance summary",
				Description: "Returns a performance summary for one or more connections",
				OperationID: "getPerformanceSummary",
				Tags:        []string{"Metrics"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamString("connection_ids", "Comma-separated connection IDs"),
					queryParamInt("connection_id", "Single connection ID"),
					queryParamString("time_range", "Time range (1h, 6h, 24h, 7d, 30d)"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("PerfSummaryResponse", "Performance summary"),
					"400": jsonResponse("ErrorResponse", "Invalid parameters"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Permission denied"),
				},
			},
		},

		"/metrics/database-summaries": {
			Get: &OpenAPIOperation{
				Summary:     "Get database summaries",
				Description: "Returns per-database summaries for a connection",
				OperationID: "getDatabaseSummaries",
				Tags:        []string{"Metrics"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamIntRequired("connection_id", "Connection ID"),
					queryParamString("time_range", "Time range (1h, 6h, 24h, 7d, 30d)"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("DatabaseSummaryResponse", "Database summaries"),
					"400": jsonResponse("ErrorResponse", "Invalid parameters"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Permission denied"),
				},
			},
		},

		"/metrics/top-queries": {
			Get: &OpenAPIOperation{
				Summary:     "Get top queries",
				Description: "Returns the top queries by execution time, calls, or other metrics",
				OperationID: "getTopQueries",
				Tags:        []string{"Metrics"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamIntRequired("connection_id", "Connection ID"),
					queryParamInt("limit", "Maximum number of queries to return"),
					queryParamString("order_by", "Column to sort by"),
					queryParamString("order", "Sort order (asc or desc)"),
					queryParamString("queryid", "Filter by specific query ID"),
					queryParamBool("exclude_collector", "Exclude collector queries"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonArrayResponse("TopQueryRow", "Top queries"),
					"400": jsonResponse("ErrorResponse", "Invalid parameters"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Permission denied"),
				},
			},
		},

		"/metrics/latest": {
			Get: &OpenAPIOperation{
				Summary:     "Get latest probe snapshot",
				Description: "Returns the most recent collected row for each unique entity in a probe table, with optional filtering, sorting, and pagination",
				OperationID: "getLatestSnapshot",
				Tags:        []string{"Metrics"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamIntRequired("connection_id", "Connection ID"),
					queryParamStringRequired("probe_name", "Probe table name (e.g. pg_stat_all_tables)"),
					queryParamString("database_name", "Filter by database name"),
					queryParamString("order_by", "Column to sort by (default: collected_at)"),
					queryParamString("order", "Sort order: asc or desc (default: desc)"),
					queryParamInt("limit", "Maximum rows to return (default 20, max 100)"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("LatestSnapshotResponse", "Latest snapshot rows"),
					"400": jsonResponse("ErrorResponse", "Invalid parameters"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Permission denied"),
				},
			},
		},

		// Overview
		"/overview": {
			Get: &OpenAPIOperation{
				Summary:     "Get AI-generated estate overview",
				Description: "Returns an AI-generated overview of the monitored estate, optionally scoped",
				OperationID: "getOverview",
				Tags:        []string{"Overview"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamBool("refresh", "Force regeneration of the overview"),
					queryParamString("connection_ids", "Comma-separated connection IDs"),
					queryParamString("scope_type", "Scope type (estate, group, cluster, server)"),
					queryParamString("scope_id", "Scope entity ID"),
					queryParamString("scope_name", "Scope display name"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("OverviewResponse", "Estate overview"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		"/overview/stream": {
			Get: &OpenAPIOperation{
				Summary:     "Stream overview generation",
				Description: "Streams overview generation progress via Server-Sent Events (SSE)",
				OperationID: "streamOverview",
				Tags:        []string{"Overview"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamString("connection_ids", "Comma-separated connection IDs"),
					queryParamString("scope_type", "Scope type (estate, group, cluster, server)"),
					queryParamString("scope_id", "Scope entity ID"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "SSE event stream",
						Content: map[string]OpenAPIMediaType{
							"text/event-stream": {Schema: &OpenAPISchema{Type: "string", Description: "Server-Sent Events stream"}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		// Memories
		"/memories": {
			Get: &OpenAPIOperation{
				Summary:     "List pinned memories",
				Description: "Returns chat memories for the current user",
				OperationID: "listMemories",
				Tags:        []string{"Memory"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					queryParamString("category", "Filter by memory category"),
					queryParamInt("limit", "Maximum number of results (default 100, max 1000)"),
				},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("MemoryListResponse", "List of memories"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		"/memories/{id}": {
			Delete: &OpenAPIOperation{
				Summary:     "Delete memory",
				Description: "Deletes a specific memory by ID",
				OperationID: "deleteMemory",
				Tags:        []string{"Memory"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Memory ID")},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Memory deleted",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object", Properties: map[string]*OpenAPISchema{"status": {Type: "string"}}}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Permission denied"),
					"404": jsonResponse("ErrorResponse", "Memory not found"),
				},
			},
			Patch: &OpenAPIOperation{
				Summary:     "Update memory pin status",
				Description: "Updates the pinned status of a memory",
				OperationID: "updateMemoryPinned",
				Tags:        []string{"Memory"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Memory ID")},
				RequestBody: &OpenAPIRequestBody{
					Description: "Pin status update",
					Required:    true,
					Content: map[string]OpenAPIMediaType{
						"application/json": {
							Schema: &OpenAPISchema{
								Type:     "object",
								Required: []string{"pinned"},
								Properties: map[string]*OpenAPISchema{
									"pinned": {Type: "boolean", Description: "Whether the memory should be pinned"},
								},
							},
						},
					},
				},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Pin status updated",
						Content: map[string]OpenAPIMediaType{
							"application/json": {
								Schema: &OpenAPISchema{
									Type: "object",
									Properties: map[string]*OpenAPISchema{
										"id":     {Type: "integer", Format: "int64"},
										"pinned": {Type: "boolean"},
									},
								},
							},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"404": jsonResponse("ErrorResponse", "Memory not found"),
				},
			},
		},

		// MCP Tools
		"/mcp/tools": {
			Get: &OpenAPIOperation{
				Summary:     "List available MCP tools",
				Description: "Returns the MCP tools available to the current user based on RBAC filtering",
				OperationID: "listMCPTools",
				Tags:        []string{"MCP Tools"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("MCPToolListResponse", "List of available MCP tools"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
				},
			},
		},

		"/mcp/tools/call": {
			Post: &OpenAPIOperation{
				Summary:     "Execute MCP tool",
				Description: "Executes a named MCP tool with the provided arguments",
				OperationID: "callMCPTool",
				Tags:        []string{"MCP Tools"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("MCPToolCallRequest", "Tool name and arguments", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("MCPToolCallResponse", "Tool execution result"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"500": jsonResponse("ErrorResponse", "Tool execution failed"),
				},
			},
		},

		// RBAC Users
		"/rbac/users": {
			Get: &OpenAPIOperation{
				Summary:     "List all users",
				Description: "Returns all users. Requires manage_users permission.",
				OperationID: "listUsers",
				Tags:        []string{"RBAC Users"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "User list",
						Content: map[string]OpenAPIMediaType{
							"application/json": {
								Schema: &OpenAPISchema{
									Type: "object",
									Properties: map[string]*OpenAPISchema{
										"users": {Type: "array", Items: &OpenAPISchema{Ref: "#/components/schemas/RBACUser"}},
									},
								},
							},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_users permission"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Create user",
				Description: "Creates a new user or service account. Requires manage_users permission.",
				OperationID: "createUser",
				Tags:        []string{"RBAC Users"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("UserCreateRequest", "User details", true),
				Responses: map[string]OpenAPIResponse{
					"201": {
						Description: "User created",
						Content: map[string]OpenAPIMediaType{
							"application/json": {
								Schema: &OpenAPISchema{
									Type:       "object",
									Properties: map[string]*OpenAPISchema{"message": {Type: "string"}},
								},
							},
						},
					},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_users permission"),
				},
			},
		},

		"/rbac/users/{id}": {
			Put: &OpenAPIOperation{
				Summary:     "Update user",
				Description: "Updates a user's details. Requires manage_users permission.",
				OperationID: "updateUser",
				Tags:        []string{"RBAC Users"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "User ID")},
				RequestBody: jsonRequestBody("UserUpdateRequest", "Updated user fields", true),
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "User updated",
						Content: map[string]OpenAPIMediaType{
							"application/json": {
								Schema: &OpenAPISchema{
									Type:       "object",
									Properties: map[string]*OpenAPISchema{"message": {Type: "string"}},
								},
							},
						},
					},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_users permission"),
					"404": jsonResponse("ErrorResponse", "User not found"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete user",
				Description: "Deletes a user. Requires manage_users permission.",
				OperationID: "deleteUser",
				Tags:        []string{"RBAC Users"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "User ID")},
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "User deleted"},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_users permission"),
					"404": jsonResponse("ErrorResponse", "User not found"),
				},
			},
		},

		"/rbac/users/{id}/privileges": {
			Get: &OpenAPIOperation{
				Summary:     "Get user effective privileges",
				Description: "Returns the effective privileges for a user, including inherited group privileges",
				OperationID: "getUserPrivileges",
				Tags:        []string{"RBAC Users"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "User ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("UserPrivilegesResponse", "User privilege details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_users permission"),
					"404": jsonResponse("ErrorResponse", "User not found"),
				},
			},
		},

		// RBAC Groups
		"/rbac/groups": {
			Get: &OpenAPIOperation{
				Summary:     "List all groups",
				Description: "Returns all RBAC groups with member counts",
				OperationID: "listGroups",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Group list",
						Content: map[string]OpenAPIMediaType{
							"application/json": {
								Schema: &OpenAPISchema{
									Type: "object",
									Properties: map[string]*OpenAPISchema{
										"groups": {Type: "array", Items: &OpenAPISchema{Ref: "#/components/schemas/RBACGroup"}},
									},
								},
							},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_groups permission"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Create group",
				Description: "Creates a new RBAC group",
				OperationID: "createGroup",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("GroupCreateRequest", "Group details", true),
				Responses: map[string]OpenAPIResponse{
					"201": {
						Description: "Group created",
						Content: map[string]OpenAPIMediaType{
							"application/json": {
								Schema: &OpenAPISchema{
									Type: "object",
									Properties: map[string]*OpenAPISchema{
										"id":   {Type: "integer", Format: "int64"},
										"name": {Type: "string"},
									},
								},
							},
						},
					},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_groups permission"),
				},
			},
		},

		"/rbac/groups/{id}": {
			Get: &OpenAPIOperation{
				Summary:     "Get group details",
				Description: "Returns a group with its members, privileges, and permissions",
				OperationID: "getGroup",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("RBACGroupDetail", "Group details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_groups permission"),
					"404": jsonResponse("ErrorResponse", "Group not found"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Update group",
				Description: "Updates a group's name or description",
				OperationID: "updateGroup",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				RequestBody: jsonRequestBody("GroupUpdateRequest", "Updated group fields", true),
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("RBACGroup", "Updated group"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_groups permission"),
					"404": jsonResponse("ErrorResponse", "Group not found"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Delete group",
				Description: "Deletes an RBAC group",
				OperationID: "deleteGroup",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Group deleted"},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_groups permission"),
				},
			},
		},

		"/rbac/groups/{id}/members": {
			Post: &OpenAPIOperation{
				Summary:     "Add group member",
				Description: "Adds a user or nested group as a member. Provide either user_id or group_id.",
				OperationID: "addGroupMember",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				RequestBody: jsonRequestBody("GroupMemberRequest", "Member to add", true),
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Member added"},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_groups permission"),
				},
			},
		},

		"/rbac/groups/{id}/members/{type}/{memberId}": {
			Delete: &OpenAPIOperation{
				Summary:     "Remove group member",
				Description: "Removes a user or group member from the group",
				OperationID: "removeGroupMember",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters: []OpenAPIParameter{
					pathParamInt("id", "Group ID"),
					pathParamString("type", "Member type (user or group)"),
					pathParamInt("memberId", "Member ID"),
				},
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Member removed"},
					"400": jsonResponse("ErrorResponse", "Invalid member type"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_groups permission"),
				},
			},
		},

		"/rbac/groups/{id}/effective-privileges": {
			Get: &OpenAPIOperation{
				Summary:     "Get group effective privileges",
				Description: "Returns the effective privileges for a group, including inherited privileges",
				OperationID: "getGroupEffectivePrivileges",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("GroupEffectivePrivileges", "Effective privilege details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_permissions permission"),
					"404": jsonResponse("ErrorResponse", "Group not found"),
				},
			},
		},

		"/rbac/groups/{id}/privileges/mcp": {
			Get: &OpenAPIOperation{
				Summary:     "Get group MCP privileges",
				Description: "Returns the MCP tool privileges assigned to a group",
				OperationID: "getGroupMCPPrivileges",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "MCP privileges",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object"}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_permissions permission"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Set group MCP privileges",
				Description: "Sets the MCP tool privileges for a group",
				OperationID: "setGroupMCPPrivileges",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				RequestBody: &OpenAPIRequestBody{
					Description: "MCP privileges to set",
					Required:    true,
					Content: map[string]OpenAPIMediaType{
						"application/json": {Schema: &OpenAPISchema{Type: "object"}},
					},
				},
				Responses: map[string]OpenAPIResponse{
					"200": {Description: "Privileges updated"},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_permissions permission"),
				},
			},
		},

		"/rbac/groups/{id}/privileges/connections": {
			Get: &OpenAPIOperation{
				Summary:     "Get group connection privileges",
				Description: "Returns the connection-level privileges assigned to a group",
				OperationID: "getGroupConnectionPrivileges",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Connection privileges",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object"}},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_permissions permission"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Set group connection privileges",
				Description: "Sets the connection-level privileges for a group",
				OperationID: "setGroupConnectionPrivileges",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				RequestBody: &OpenAPIRequestBody{
					Description: "Connection privileges to set",
					Required:    true,
					Content: map[string]OpenAPIMediaType{
						"application/json": {Schema: &OpenAPISchema{Type: "object"}},
					},
				},
				Responses: map[string]OpenAPIResponse{
					"200": {Description: "Privileges updated"},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_permissions permission"),
				},
			},
		},

		"/rbac/groups/{id}/permissions": {
			Get: &OpenAPIOperation{
				Summary:     "Get group admin permissions",
				Description: "Returns the admin permissions assigned to a group",
				OperationID: "getGroupPermissions",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("GroupPermissionsResponse", "Group admin permissions"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_permissions permission"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Set group admin permissions",
				Description: "Sets the admin permissions for a group",
				OperationID: "setGroupPermissions",
				Tags:        []string{"RBAC Groups"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Group ID")},
				RequestBody: &OpenAPIRequestBody{
					Description: "Permissions to set",
					Required:    true,
					Content: map[string]OpenAPIMediaType{
						"application/json": {Schema: &OpenAPISchema{Type: "object"}},
					},
				},
				Responses: map[string]OpenAPIResponse{
					"200": {Description: "Permissions updated"},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_permissions permission"),
				},
			},
		},

		// RBAC Tokens
		"/rbac/tokens": {
			Get: &OpenAPIOperation{
				Summary:     "List all tokens",
				Description: "Returns all API tokens with scope information. Requires manage_token_scopes permission.",
				OperationID: "listTokens",
				Tags:        []string{"RBAC Tokens"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "Token list",
						Content: map[string]OpenAPIMediaType{
							"application/json": {
								Schema: &OpenAPISchema{
									Type: "object",
									Properties: map[string]*OpenAPISchema{
										"tokens": {Type: "array", Items: &OpenAPISchema{Ref: "#/components/schemas/RBACToken"}},
									},
								},
							},
						},
					},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_token_scopes permission"),
				},
			},
			Post: &OpenAPIOperation{
				Summary:     "Create token",
				Description: "Creates a new API token for the specified user. The raw token is returned only once.",
				OperationID: "createToken",
				Tags:        []string{"RBAC Tokens"},
				Security:    bearerAuth,
				RequestBody: jsonRequestBody("TokenCreateRequest", "Token details", true),
				Responses: map[string]OpenAPIResponse{
					"201": jsonResponse("TokenCreateResponse", "Token created with raw value"),
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_token_scopes permission"),
				},
			},
		},

		"/rbac/tokens/{id}": {
			Delete: &OpenAPIOperation{
				Summary:     "Delete token",
				Description: "Deletes an API token",
				OperationID: "deleteToken",
				Tags:        []string{"RBAC Tokens"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Token ID")},
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Token deleted"},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_token_scopes permission"),
				},
			},
		},

		"/rbac/tokens/{id}/scope": {
			Get: &OpenAPIOperation{
				Summary:     "Get token scope",
				Description: "Returns the scope restrictions for a token",
				OperationID: "getTokenScope",
				Tags:        []string{"RBAC Tokens"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Token ID")},
				Responses: map[string]OpenAPIResponse{
					"200": jsonResponse("TokenScopeResponse", "Token scope details"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_token_scopes permission"),
				},
			},
			Put: &OpenAPIOperation{
				Summary:     "Set token scope",
				Description: "Sets scope restrictions for a token including connections, MCP privileges, and admin permissions",
				OperationID: "setTokenScope",
				Tags:        []string{"RBAC Tokens"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Token ID")},
				RequestBody: jsonRequestBody("TokenScopeRequest", "Scope configuration", true),
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Scope updated"},
					"400": jsonResponse("ErrorResponse", "Invalid request"),
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_token_scopes permission"),
				},
			},
			Delete: &OpenAPIOperation{
				Summary:     "Clear token scope",
				Description: "Removes all scope restrictions from a token",
				OperationID: "clearTokenScope",
				Tags:        []string{"RBAC Tokens"},
				Security:    bearerAuth,
				Parameters:  []OpenAPIParameter{pathParamInt("id", "Token ID")},
				Responses: map[string]OpenAPIResponse{
					"204": {Description: "Scope cleared"},
					"401": jsonResponse("ErrorResponse", "Unauthorized"),
					"403": jsonResponse("ErrorResponse", "Requires manage_token_scopes permission"),
				},
			},
		},

		// RBAC Privileges
		"/rbac/privileges/mcp": {
			Get: &OpenAPIOperation{
				Summary:     "List MCP privilege identifiers",
				Description: "Returns all available MCP privilege identifiers",
				OperationID: "listMCPPrivileges",
				Tags:        []string{"RBAC Privileges"},
				Security:    bearerAuth,
				Responses: map[string]OpenAPIResponse{
					"200": {
						Description: "MCP privilege identifiers",
						Content: map[string]OpenAPIMediaType{
							"application/json": {Schema: &OpenAPISchema{Type: "object"}},
						},
					},
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
