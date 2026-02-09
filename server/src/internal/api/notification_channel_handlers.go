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
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// NotificationChannelHandler handles REST API requests for notification channel management
type NotificationChannelHandler struct {
	datastore       *database.Datastore
	authStore       *auth.AuthStore
	rbacChecker     *auth.RBACChecker
	hostValidator   *HostValidator
	checkPermission func(http.ResponseWriter, *http.Request) bool
}

// NewNotificationChannelHandler creates a new notification channel handler
func NewNotificationChannelHandler(datastore *database.Datastore, authStore *auth.AuthStore, rbacChecker *auth.RBACChecker) *NotificationChannelHandler {
	h := &NotificationChannelHandler{
		datastore:     datastore,
		authStore:     authStore,
		rbacChecker:   rbacChecker,
		hostValidator: DefaultHostValidator(),
	}
	if rbacChecker != nil {
		h.checkPermission = RequireAdminPermission(rbacChecker, auth.PermManageNotificationChannels, "manage notification channels")
	}
	return h
}

// NewNotificationChannelHandlerWithSecurity creates a new notification channel handler with custom security settings
func NewNotificationChannelHandlerWithSecurity(datastore *database.Datastore, authStore *auth.AuthStore,
	rbacChecker *auth.RBACChecker, allowInternal bool, allowedHosts, blockedHosts []string) *NotificationChannelHandler {
	h := &NotificationChannelHandler{
		datastore:     datastore,
		authStore:     authStore,
		rbacChecker:   rbacChecker,
		hostValidator: NewHostValidator(allowInternal, allowedHosts, blockedHosts),
	}
	if rbacChecker != nil {
		h.checkPermission = RequireAdminPermission(rbacChecker, auth.PermManageNotificationChannels, "manage notification channels")
	}
	return h
}

// RegisterRoutes registers notification channel management routes on the mux
func (h *NotificationChannelHandler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.HandlerFunc) http.HandlerFunc) {
	if h.datastore == nil {
		notConfigured := HandleNotConfigured("Notification channel management")
		mux.HandleFunc("/api/v1/notification-channels", authWrapper(notConfigured))
		mux.HandleFunc("/api/v1/notification-channels/", authWrapper(notConfigured))
		return
	}

	mux.HandleFunc("/api/v1/notification-channels", authWrapper(h.handleChannels))
	mux.HandleFunc("/api/v1/notification-channels/", authWrapper(h.handleChannelSubpath))
}

// NotificationChannelCreateRequest is the request body for creating a notification channel
type NotificationChannelCreateRequest struct {
	ChannelType     string  `json:"channel_type"`
	Name            string  `json:"name"`
	Description     *string `json:"description,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	IsEstateDefault *bool   `json:"is_estate_default,omitempty"`

	// Slack/Mattermost
	WebhookURL *string `json:"webhook_url,omitempty"`

	// Webhook specific
	EndpointURL     *string            `json:"endpoint_url,omitempty"`
	HTTPMethod      *string            `json:"http_method,omitempty"`
	Headers         *map[string]string `json:"headers,omitempty"`
	AuthType        *string            `json:"auth_type,omitempty"`
	AuthCredentials *string            `json:"auth_credentials,omitempty"`

	// Email fields
	SMTPHost     *string `json:"smtp_host,omitempty"`
	SMTPPort     *int    `json:"smtp_port,omitempty"`
	SMTPUsername *string `json:"smtp_username,omitempty"`
	SMTPPassword *string `json:"smtp_password,omitempty"`
	SMTPUseTLS   *bool   `json:"smtp_use_tls,omitempty"`
	FromAddress  *string `json:"from_address,omitempty"`
	FromName     *string `json:"from_name,omitempty"`

	// Templates
	TemplateAlertFire  *string `json:"template_alert_fire,omitempty"`
	TemplateAlertClear *string `json:"template_alert_clear,omitempty"`
	TemplateReminder   *string `json:"template_reminder,omitempty"`

	// Reminder
	ReminderEnabled       *bool `json:"reminder_enabled,omitempty"`
	ReminderIntervalHours *int  `json:"reminder_interval_hours,omitempty"`
}

// EmailRecipientRequest is the request body for creating or updating an email recipient
type EmailRecipientRequest struct {
	EmailAddress string  `json:"email_address"`
	DisplayName  *string `json:"display_name,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
}

// handleChannels handles GET/POST /api/v1/notification-channels
func (h *NotificationChannelHandler) handleChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listChannels(w, r)
	case http.MethodPost:
		h.createChannel(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleChannelSubpath handles /api/v1/notification-channels/{id} and sub-resources
func (h *NotificationChannelHandler) handleChannelSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/notification-channels/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	channelID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid notification channel ID")
		return
	}

	// Handle /api/v1/notification-channels/{id}/test
	if len(parts) == 2 && parts[1] == "test" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.testChannel(w, r, channelID)
		return
	}

	// Handle /api/v1/notification-channels/{id}/recipients
	if len(parts) >= 2 && parts[1] == "recipients" {
		h.handleRecipientRoutes(w, r, channelID, parts[2:])
		return
	}

	// Handle /api/v1/notification-channels/{id}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.getChannel(w, r, channelID)
		case http.MethodPut:
			h.updateChannel(w, r, channelID)
		case http.MethodDelete:
			h.deleteChannel(w, r, channelID)
		default:
			w.Header().Set("Allow", "GET, PUT, DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.NotFound(w, r)
}

// handleRecipientRoutes handles /api/v1/notification-channels/{id}/recipients[/{recipientId}]
func (h *NotificationChannelHandler) handleRecipientRoutes(w http.ResponseWriter, r *http.Request, channelID int64, remainingParts []string) {
	// /api/v1/notification-channels/{id}/recipients
	if len(remainingParts) == 0 {
		switch r.Method {
		case http.MethodGet:
			h.listRecipients(w, r, channelID)
		case http.MethodPost:
			h.createRecipient(w, r, channelID)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/v1/notification-channels/{id}/recipients/{recipientId}
	if len(remainingParts) == 1 {
		recipientID, err := strconv.ParseInt(remainingParts[0], 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid recipient ID")
			return
		}

		switch r.Method {
		case http.MethodPut:
			h.updateRecipient(w, r, channelID, recipientID)
		case http.MethodDelete:
			h.deleteRecipient(w, r, recipientID)
		default:
			w.Header().Set("Allow", "PUT, DELETE")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.NotFound(w, r)
}

// listChannels handles GET /api/v1/notification-channels
func (h *NotificationChannelHandler) listChannels(w http.ResponseWriter, r *http.Request) {
	if !h.checkPermission(w, r) {
		return
	}

	channels, err := h.datastore.ListNotificationChannels(r.Context())
	if err != nil {
		log.Printf("[ERROR] Failed to fetch notification channels: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch notification channels")
		return
	}

	RespondJSON(w, http.StatusOK, channels)
}

// getChannel handles GET /api/v1/notification-channels/{id}
func (h *NotificationChannelHandler) getChannel(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.checkPermission(w, r) {
		return
	}

	channel, err := h.datastore.GetNotificationChannel(r.Context(), id)
	if err != nil {
		if errors.Is(err, database.ErrNotificationChannelNotFound) {
			RespondError(w, http.StatusNotFound, "Notification channel not found")
			return
		}
		log.Printf("[ERROR] Failed to fetch notification channel: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch notification channel")
		return
	}

	RespondJSON(w, http.StatusOK, channel)
}

// createChannel handles POST /api/v1/notification-channels
func (h *NotificationChannelHandler) createChannel(w http.ResponseWriter, r *http.Request) {
	if !h.checkPermission(w, r) {
		return
	}

	var req NotificationChannelCreateRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Validate channel type
	if !database.ValidChannelTypes[req.ChannelType] {
		RespondError(w, http.StatusBadRequest,
			"Invalid channel_type: must be one of email, slack, mattermost, webhook")
		return
	}

	// Validate name
	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	// Validate email-specific fields
	if req.ChannelType == string(database.ChannelTypeEmail) {
		if req.SMTPHost == nil || *req.SMTPHost == "" {
			RespondError(w, http.StatusBadRequest, "smtp_host is required for email channels")
			return
		}
		if req.FromAddress == nil || *req.FromAddress == "" {
			RespondError(w, http.StatusBadRequest, "from_address is required for email channels")
			return
		}
	}

	// Get owner_username from auth context
	username := auth.GetUsernameFromContext(r.Context())
	if username == "" {
		username = "unknown"
	}

	// Build the channel with defaults
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	smtpPort := 587
	if req.SMTPPort != nil {
		smtpPort = *req.SMTPPort
	}

	smtpUseTLS := true
	if req.SMTPUseTLS != nil {
		smtpUseTLS = *req.SMTPUseTLS
	}

	httpMethod := "POST"
	if req.HTTPMethod != nil {
		httpMethod = *req.HTTPMethod
	}

	reminderEnabled := false
	if req.ReminderEnabled != nil {
		reminderEnabled = *req.ReminderEnabled
	}

	reminderIntervalHours := 4
	if req.ReminderIntervalHours != nil {
		reminderIntervalHours = *req.ReminderIntervalHours
	}

	isEstateDefault := false
	if req.IsEstateDefault != nil {
		isEstateDefault = *req.IsEstateDefault
	}

	var headers map[string]string
	if req.Headers != nil {
		headers = *req.Headers
	}

	channel := &database.NotificationChannel{
		OwnerUsername:         &username,
		Enabled:               enabled,
		ChannelType:           database.NotificationChannelType(req.ChannelType),
		Name:                  req.Name,
		Description:           req.Description,
		WebhookURL:            req.WebhookURL,
		EndpointURL:           req.EndpointURL,
		HTTPMethod:            httpMethod,
		Headers:               headers,
		AuthType:              req.AuthType,
		AuthCredentials:       req.AuthCredentials,
		SMTPHost:              req.SMTPHost,
		SMTPPort:              smtpPort,
		SMTPUsername:          req.SMTPUsername,
		SMTPPassword:          req.SMTPPassword,
		SMTPUseTLS:            smtpUseTLS,
		FromAddress:           req.FromAddress,
		FromName:              req.FromName,
		TemplateAlertFire:     req.TemplateAlertFire,
		TemplateAlertClear:    req.TemplateAlertClear,
		TemplateReminder:      req.TemplateReminder,
		ReminderEnabled:       reminderEnabled,
		ReminderIntervalHours: reminderIntervalHours,
		IsEstateDefault:       isEstateDefault,
	}

	if err := h.datastore.CreateNotificationChannel(r.Context(), channel); err != nil {
		log.Printf("[ERROR] Failed to create notification channel: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to create notification channel")
		return
	}

	RespondJSON(w, http.StatusCreated, channel)
}

// updateChannel handles PUT /api/v1/notification-channels/{id}
func (h *NotificationChannelHandler) updateChannel(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.checkPermission(w, r) {
		return
	}

	var req NotificationChannelCreateRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Fetch existing channel
	existing, err := h.datastore.GetNotificationChannel(r.Context(), id)
	if err != nil {
		if errors.Is(err, database.ErrNotificationChannelNotFound) {
			RespondError(w, http.StatusNotFound, "Notification channel not found")
			return
		}
		log.Printf("[ERROR] Failed to fetch notification channel: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch notification channel")
		return
	}

	// Validate channel type if provided
	channelType := string(existing.ChannelType)
	if req.ChannelType != "" {
		if !database.ValidChannelTypes[req.ChannelType] {
			RespondError(w, http.StatusBadRequest,
				"Invalid channel_type: must be one of email, slack, mattermost, webhook")
			return
		}
		channelType = req.ChannelType
	}

	// Validate name
	name := existing.Name
	if req.Name != "" {
		name = req.Name
	}

	// Validate email-specific fields if this is an email channel
	if channelType == string(database.ChannelTypeEmail) {
		smtpHost := existing.SMTPHost
		if req.SMTPHost != nil {
			smtpHost = req.SMTPHost
		}
		if smtpHost == nil || *smtpHost == "" {
			RespondError(w, http.StatusBadRequest, "smtp_host is required for email channels")
			return
		}
		fromAddress := existing.FromAddress
		if req.FromAddress != nil {
			fromAddress = req.FromAddress
		}
		if fromAddress == nil || *fromAddress == "" {
			RespondError(w, http.StatusBadRequest, "from_address is required for email channels")
			return
		}
	}

	// Merge fields
	if req.Description != nil {
		existing.Description = req.Description
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.WebhookURL != nil {
		existing.WebhookURL = req.WebhookURL
	}
	if req.EndpointURL != nil {
		existing.EndpointURL = req.EndpointURL
	}
	if req.HTTPMethod != nil {
		existing.HTTPMethod = *req.HTTPMethod
	}
	if req.Headers != nil {
		existing.Headers = *req.Headers
	}
	if req.AuthType != nil {
		existing.AuthType = req.AuthType
	}
	if req.AuthCredentials != nil {
		existing.AuthCredentials = req.AuthCredentials
	}
	if req.SMTPHost != nil {
		existing.SMTPHost = req.SMTPHost
	}
	if req.SMTPPort != nil {
		existing.SMTPPort = *req.SMTPPort
	}
	if req.SMTPUsername != nil {
		existing.SMTPUsername = req.SMTPUsername
	}
	if req.SMTPPassword != nil {
		existing.SMTPPassword = req.SMTPPassword
	}
	if req.SMTPUseTLS != nil {
		existing.SMTPUseTLS = *req.SMTPUseTLS
	}
	if req.FromAddress != nil {
		existing.FromAddress = req.FromAddress
	}
	if req.FromName != nil {
		existing.FromName = req.FromName
	}
	if req.TemplateAlertFire != nil {
		existing.TemplateAlertFire = req.TemplateAlertFire
	}
	if req.TemplateAlertClear != nil {
		existing.TemplateAlertClear = req.TemplateAlertClear
	}
	if req.TemplateReminder != nil {
		existing.TemplateReminder = req.TemplateReminder
	}
	if req.ReminderEnabled != nil {
		existing.ReminderEnabled = *req.ReminderEnabled
	}
	if req.ReminderIntervalHours != nil {
		existing.ReminderIntervalHours = *req.ReminderIntervalHours
	}
	if req.IsEstateDefault != nil {
		existing.IsEstateDefault = *req.IsEstateDefault
	}

	existing.ChannelType = database.NotificationChannelType(channelType)
	existing.Name = name

	if err := h.datastore.UpdateNotificationChannel(r.Context(), existing); err != nil {
		if errors.Is(err, database.ErrNotificationChannelNotFound) {
			RespondError(w, http.StatusNotFound, "Notification channel not found")
			return
		}
		log.Printf("[ERROR] Failed to update notification channel: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to update notification channel")
		return
	}

	RespondJSON(w, http.StatusOK, existing)
}

// deleteChannel handles DELETE /api/v1/notification-channels/{id}
func (h *NotificationChannelHandler) deleteChannel(w http.ResponseWriter, r *http.Request, id int64) {
	if !h.checkPermission(w, r) {
		return
	}

	if err := h.datastore.DeleteNotificationChannel(r.Context(), id); err != nil {
		if errors.Is(err, database.ErrNotificationChannelNotFound) {
			RespondError(w, http.StatusNotFound, "Notification channel not found")
			return
		}
		log.Printf("[ERROR] Failed to delete notification channel: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to delete notification channel")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// listRecipients handles GET /api/v1/notification-channels/{id}/recipients
func (h *NotificationChannelHandler) listRecipients(w http.ResponseWriter, r *http.Request, channelID int64) {
	if !h.checkPermission(w, r) {
		return
	}

	// Verify channel exists
	_, err := h.datastore.GetNotificationChannel(r.Context(), channelID)
	if err != nil {
		if errors.Is(err, database.ErrNotificationChannelNotFound) {
			RespondError(w, http.StatusNotFound, "Notification channel not found")
			return
		}
		log.Printf("[ERROR] Failed to fetch notification channel: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch notification channel")
		return
	}

	recipients, err := h.datastore.ListEmailRecipients(r.Context(), channelID)
	if err != nil {
		log.Printf("[ERROR] Failed to fetch email recipients: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch email recipients")
		return
	}

	RespondJSON(w, http.StatusOK, recipients)
}

// createRecipient handles POST /api/v1/notification-channels/{id}/recipients
func (h *NotificationChannelHandler) createRecipient(w http.ResponseWriter, r *http.Request, channelID int64) {
	if !h.checkPermission(w, r) {
		return
	}

	// Verify channel exists
	_, err := h.datastore.GetNotificationChannel(r.Context(), channelID)
	if err != nil {
		if errors.Is(err, database.ErrNotificationChannelNotFound) {
			RespondError(w, http.StatusNotFound, "Notification channel not found")
			return
		}
		log.Printf("[ERROR] Failed to fetch notification channel: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch notification channel")
		return
	}

	var req EmailRecipientRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Validate email address
	if req.EmailAddress == "" || !strings.Contains(req.EmailAddress, "@") {
		RespondError(w, http.StatusBadRequest, "A valid email_address is required (must contain @)")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	recipient := &database.EmailRecipient{
		ChannelID:    channelID,
		EmailAddress: req.EmailAddress,
		DisplayName:  req.DisplayName,
		Enabled:      enabled,
	}

	if err := h.datastore.CreateEmailRecipient(r.Context(), recipient); err != nil {
		log.Printf("[ERROR] Failed to create email recipient: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to create email recipient")
		return
	}

	RespondJSON(w, http.StatusCreated, recipient)
}

// updateRecipient handles PUT /api/v1/notification-channels/{id}/recipients/{recipientId}
func (h *NotificationChannelHandler) updateRecipient(w http.ResponseWriter, r *http.Request, _ int64, recipientID int64) {
	if !h.checkPermission(w, r) {
		return
	}

	var req EmailRecipientRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Validate email address
	if req.EmailAddress == "" || !strings.Contains(req.EmailAddress, "@") {
		RespondError(w, http.StatusBadRequest, "A valid email_address is required (must contain @)")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	recipient := &database.EmailRecipient{
		ID:           recipientID,
		EmailAddress: req.EmailAddress,
		DisplayName:  req.DisplayName,
		Enabled:      enabled,
	}

	if err := h.datastore.UpdateEmailRecipient(r.Context(), recipient); err != nil {
		if errors.Is(err, database.ErrEmailRecipientNotFound) {
			RespondError(w, http.StatusNotFound, "Email recipient not found")
			return
		}
		log.Printf("[ERROR] Failed to update email recipient: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to update email recipient")
		return
	}

	RespondJSON(w, http.StatusOK, recipient)
}

// deleteRecipient handles DELETE /api/v1/notification-channels/{id}/recipients/{recipientId}
func (h *NotificationChannelHandler) deleteRecipient(w http.ResponseWriter, r *http.Request, recipientID int64) {
	if !h.checkPermission(w, r) {
		return
	}

	if err := h.datastore.DeleteEmailRecipient(r.Context(), recipientID); err != nil {
		if errors.Is(err, database.ErrEmailRecipientNotFound) {
			RespondError(w, http.StatusNotFound, "Email recipient not found")
			return
		}
		log.Printf("[ERROR] Failed to delete email recipient: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to delete email recipient")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// derefStr returns the dereferenced string or an empty string if the
// pointer is nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// TestChannelRequest is the optional request body for testing a channel
type TestChannelRequest struct {
	RecipientEmail *string `json:"recipient_email,omitempty"`
}

// testChannel handles POST /api/v1/notification-channels/{id}/test
func (h *NotificationChannelHandler) testChannel(w http.ResponseWriter, r *http.Request, channelID int64) {
	if !h.checkPermission(w, r) {
		return
	}

	channel, err := h.datastore.GetNotificationChannel(r.Context(), channelID)
	if err != nil {
		if errors.Is(err, database.ErrNotificationChannelNotFound) {
			RespondError(w, http.StatusNotFound, "Notification channel not found")
			return
		}
		log.Printf("[ERROR] Failed to fetch notification channel: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to fetch notification channel")
		return
	}

	switch channel.ChannelType {
	case database.ChannelTypeEmail:
		if channel.SMTPHost == nil || *channel.SMTPHost == "" {
			RespondError(w, http.StatusBadRequest, "SMTP host is not configured for this channel")
			return
		}
		if channel.FromAddress == nil || *channel.FromAddress == "" {
			RespondError(w, http.StatusBadRequest, "From address is not configured for this channel")
			return
		}

		// Optionally decode recipient_email from the request body
		var req TestChannelRequest
		if r.Body != nil {
			dec := json.NewDecoder(r.Body)
			if decErr := dec.Decode(&req); decErr != nil && !errors.Is(decErr, io.EOF) {
				log.Printf("[ERROR] Invalid request body: %v", decErr)
				RespondError(w, http.StatusBadRequest, "Invalid request body")
				return
			}
		}

		// Determine recipient list
		var toAddresses []string
		if req.RecipientEmail != nil && *req.RecipientEmail != "" {
			toAddresses = []string{*req.RecipientEmail}
		} else {
			for _, rcpt := range channel.Recipients {
				if rcpt.Enabled {
					toAddresses = append(toAddresses, rcpt.EmailAddress)
				}
			}
		}

		if len(toAddresses) == 0 {
			RespondError(w, http.StatusBadRequest,
				"No recipients available. Provide a recipient_email or add enabled recipients to the channel.")
			return
		}

		// Validate SMTP host to prevent SSRF attacks
		if err := h.hostValidator.ValidateHost(*channel.SMTPHost); err != nil {
			log.Printf("[ERROR] SMTP host validation failed: %v", err)
			RespondError(w, http.StatusBadRequest, "Invalid SMTP host")
			return
		}

		if err := sendTestEmail(
			*channel.SMTPHost,
			channel.SMTPPort,
			derefStr(channel.SMTPUsername),
			derefStr(channel.SMTPPassword),
			channel.SMTPUseTLS,
			*channel.FromAddress,
			derefStr(channel.FromName),
			toAddresses,
		); err != nil {
			log.Printf("[ERROR] Failed to send test email: %v", err)
			RespondError(w, http.StatusBadGateway, "Failed to send test email")
			return
		}

	case database.ChannelTypeSlack, database.ChannelTypeMattermost:
		if channel.WebhookURL == nil || *channel.WebhookURL == "" {
			RespondError(w, http.StatusBadRequest, "Webhook URL is not configured for this channel")
			return
		}
		displayType := "Slack"
		if channel.ChannelType == database.ChannelTypeMattermost {
			displayType = "Mattermost"
		}
		// Validate webhook URL host to prevent SSRF attacks
		webhookURL, parseErr := url.Parse(*channel.WebhookURL)
		if parseErr != nil {
			RespondError(w, http.StatusBadRequest, "Invalid webhook URL")
			return
		}
		if err := h.hostValidator.ValidateHost(webhookURL.Hostname()); err != nil {
			log.Printf("[ERROR] Webhook host validation failed: %v", err)
			RespondError(w, http.StatusBadRequest, "Invalid webhook host")
			return
		}

		if err := sendTestWebhook(*channel.WebhookURL, displayType); err != nil {
			log.Printf("[ERROR] Failed to send test webhook: %v", err)
			RespondError(w, http.StatusBadGateway, "Failed to send test webhook")
			return
		}

	case database.ChannelTypeWebhook:
		if channel.EndpointURL == nil || *channel.EndpointURL == "" {
			RespondError(w, http.StatusBadRequest, "Endpoint URL is not configured for this channel")
			return
		}
		// Validate endpoint URL host to prevent SSRF attacks
		endpointURL, parseErr := url.Parse(*channel.EndpointURL)
		if parseErr != nil {
			RespondError(w, http.StatusBadRequest, "Invalid endpoint URL")
			return
		}
		if err := h.hostValidator.ValidateHost(endpointURL.Hostname()); err != nil {
			log.Printf("[ERROR] Endpoint host validation failed: %v", err)
			RespondError(w, http.StatusBadRequest, "Invalid endpoint host")
			return
		}

		if err := sendTestGenericWebhook(
			*channel.EndpointURL,
			channel.HTTPMethod,
			channel.Headers,
			derefStr(channel.AuthType),
			derefStr(channel.AuthCredentials),
		); err != nil {
			log.Printf("[ERROR] Failed to send test webhook: %v", err)
			RespondError(w, http.StatusBadGateway, "Failed to send test webhook")
			return
		}

	default:
		RespondError(w, http.StatusBadRequest, "Test sending is not supported for this channel type")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}
