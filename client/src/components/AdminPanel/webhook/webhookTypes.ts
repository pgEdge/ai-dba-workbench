/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { BaseChannel } from '../channels/channelTypes';

/**
 * Webhook notification channel as returned by the API.
 *
 * Note: The server intentionally redacts `auth_credentials` AND the
 * custom `headers` map from the channel response (see issue #187).
 * Header VALUES often carry bearer tokens or API keys
 * (`Authorization`, `X-API-Key`, ...) so they are never returned to
 * the client. Instead the response carries:
 *
 * - `auth_credentials_set`: boolean — whether auth credentials are
 *   stored.
 * - `header_names`: string[] — the configured header NAMES, sorted
 *   alphabetically. Names are non-secret and may be displayed.
 */
export interface WebhookChannel extends BaseChannel {
    endpoint_url: string;
    http_method: string;
    /**
     * Names of the custom HTTP headers configured for this channel,
     * sorted alphabetically. Header values are redacted by the
     * server and are not present in the API response.
     */
    header_names: string[];
    auth_type: string;
    /** True when the channel has auth credentials stored on the server. */
    auth_credentials_set: boolean;
    template_alert_fire: string;
    template_alert_clear: string;
    template_reminder: string;
}

/** A single HTTP header entry with a stable ID for React keys. */
export interface HeaderEntry {
    id: string;
    key: string;
    value: string;
}

/**
 * Form state for creating or editing a webhook channel.
 *
 * `auth_credentials` is the assembled credential string built from the
 * per-auth-type form fields. It is local form state only and is never
 * pre-populated from the API response. On edit, an empty value means
 * "preserve the existing server-side credentials".
 */
export interface WebhookFormState {
    name: string;
    description: string;
    endpoint_url: string;
    http_method: string;
    headers: HeaderEntry[];
    auth_type: string;
    auth_credentials: string;
    enabled: boolean;
    is_estate_default: boolean;
    template_alert_fire: string;
    template_alert_clear: string;
    template_reminder: string;
}

/** Default form state for creating a new webhook channel. */
export const DEFAULT_WEBHOOK_FORM: WebhookFormState = {
    name: '',
    description: '',
    endpoint_url: '',
    http_method: 'POST',
    headers: [],
    auth_type: 'none',
    auth_credentials: '',
    enabled: true,
    is_estate_default: false,
    template_alert_fire: '',
    template_alert_clear: '',
    template_reminder: '',
};
