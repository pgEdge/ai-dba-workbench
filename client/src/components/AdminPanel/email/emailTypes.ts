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
 * Email channel as returned by the API.
 *
 * Note: The server intentionally redacts `smtp_username` and `smtp_password`
 * from the channel response (see issue #187). Clients receive boolean
 * `*_set` indicators instead so they can show whether a secret is
 * configured without exposing the value itself.
 */
export interface EmailChannel extends BaseChannel {
    smtp_host: string;
    smtp_port: number;
    /** True when the channel has an SMTP username stored on the server. */
    smtp_username_set: boolean;
    /** True when the channel has an SMTP password stored on the server. */
    smtp_password_set: boolean;
    use_tls: boolean;
    from_address: string;
    from_name: string;
    recipient_count: number;
}

/** Email recipient for a channel. */
export interface EmailRecipient {
    id: number;
    email: string;
    display_name: string;
    enabled: boolean;
}

/**
 * Form state for creating or editing an email channel.
 *
 * The `smtp_username` and `smtp_password` fields are local form state only;
 * they are never pre-populated from the API response. On edit, an empty
 * value means "preserve the existing server-side secret".
 */
export interface EmailFormState {
    name: string;
    description: string;
    enabled: boolean;
    is_estate_default: boolean;
    smtp_host: string;
    smtp_port: string;
    smtp_username: string;
    smtp_password: string;
    use_tls: boolean;
    from_address: string;
    from_name: string;
}

/** Default values for the email channel form. */
export const DEFAULT_EMAIL_FORM: EmailFormState = {
    name: '',
    description: '',
    enabled: true,
    is_estate_default: false,
    smtp_host: '',
    smtp_port: '587',
    smtp_username: '',
    smtp_password: '',
    use_tls: true,
    from_address: '',
    from_name: '',
};
