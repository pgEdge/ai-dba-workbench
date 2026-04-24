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

/** Webhook notification channel returned by the API. */
export interface WebhookChannel extends BaseChannel {
    endpoint_url: string;
    http_method: string;
    headers: Record<string, string>;
    auth_type: string;
    auth_credentials: string;
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

/** Form state for creating or editing a webhook channel. */
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
