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

/** Email channel with SMTP configuration and recipient count. */
export interface EmailChannel extends BaseChannel {
    smtp_host: string;
    smtp_port: number;
    smtp_username: string;
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

/** Form state for creating or editing an email channel. */
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
