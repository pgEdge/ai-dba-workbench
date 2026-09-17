/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Credential-field descriptors for the shared messaging-channel panel.
 *
 * Messaging platforms differ in what they need to reach a destination:
 * Slack and Mattermost take a single incoming-webhook URL, Telegram
 * takes a bot token plus a chat ID. A platform describes its
 * credentials as a list of these descriptors and
 * `AdminMessagingChannels` renders, validates and submits them
 * generically.
 *
 * The pure helpers live here rather than in the component file so the
 * validation rules can be unit-tested directly (and so the component
 * module keeps exporting only its component).
 */

/** One credential field of a messaging channel. */
export interface MessagingChannelField {
    /** API field name used in create/update request bodies. */
    key: string;
    /** Label for the form field and for its table column. */
    label: string;
    /**
     * Response boolean reporting whether a secret is configured, e.g.
     * `webhook_url_set`. Secrets are never echoed back by the server
     * (issue #187), so this flag is the only way to tell.
     */
    setFlag?: string;
    /**
     * Response field echoing the value back, for non-secret fields,
     * e.g. `telegram_chat_id`.
     */
    valueKey?: string;
    /**
     * Whether a value is mandatory. Required secrets are mandatory on
     * create and optional on edit once one is stored (blank means
     * "leave unchanged"); required non-secret fields are mandatory in
     * both cases.
     */
    required?: boolean;
    /**
     * Secret fields are masked, are never populated from the API on
     * edit, and are left blank to keep the stored value.
     */
    secret?: boolean;
    helperText?: string;
    placeholder?: string;
    /**
     * Show a column for this field in the channel table. A field with
     * a `setFlag` renders a configured / not-configured indicator; a
     * field with a `valueKey` renders the value itself.
     */
    showInTable?: boolean;
}

/**
 * Configuration that varies between messaging platforms (Slack,
 * Mattermost, Telegram, etc.).
 *
 * Pass a module-level constant: the object identity feeds the panel's
 * fetch callback dependency list, so an inline literal would re-fetch
 * the channel list on every render.
 */
export interface MessagingChannelConfig {
    /** API channel_type value, e.g. 'slack' or 'telegram'. */
    channelType: string;
    /** Human-readable platform name shown in headings and messages. */
    platformName: string;
    /** Credential fields this platform needs, in display order. */
    fields: MessagingChannelField[];
}

/** Blank credential values for every descriptor. */
export const emptyFieldValues = (
    fields: MessagingChannelField[],
): Record<string, string> =>
    Object.fromEntries(fields.map((field) => [field.key, '']));

/**
 * Credential values to show when editing an existing channel.
 *
 * Secrets are always blank — the server never returns them, and a
 * blank value at save time means "keep the stored one". Non-secret
 * fields are pre-populated from `channelValues` so the user can see
 * and amend them.
 */
export const editFieldValues = (
    fields: MessagingChannelField[],
    channelValues: Record<string, string>,
): Record<string, string> =>
    Object.fromEntries(
        fields.map((field) => [
            field.key,
            field.secret ? '' : channelValues[field.key] || '',
        ]),
    );

/**
 * Whether a field must be filled in before the form can be submitted.
 *
 * `configured` holds the per-field "already stored" flags of the
 * channel being edited, or `null` when creating a new channel.
 *
 * A required secret is mandatory on create, and on edit only while the
 * channel has none stored; leaving it blank otherwise preserves the
 * stored value. Required non-secret fields are always mandatory.
 */
export const isFieldMandatory = (
    field: MessagingChannelField,
    configured: Record<string, boolean> | null,
): boolean => {
    if (!field.required) {
        return false;
    }
    if (!field.secret) {
        return true;
    }
    return configured === null || !configured[field.key];
};

/**
 * First mandatory field the user has left blank, or `undefined` when
 * the credential fields are complete.
 *
 * Shared by the submit button's disabled state and the save handler so
 * the two can never disagree about what counts as complete.
 */
export const findMissingRequiredField = (
    fields: MessagingChannelField[],
    values: Record<string, string>,
    configured: Record<string, boolean> | null,
): MessagingChannelField | undefined =>
    fields.find(
        (field) =>
            isFieldMandatory(field, configured)
            && !(values[field.key] || '').trim(),
    );
