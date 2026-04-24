/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { ServerFormData, FormErrors } from './ServerDialog.types';

/**
 * Validates all form fields and returns an object containing error messages.
 *
 * @param formData - The form data to validate.
 * @param isEditMode - Whether the form is in edit mode (password not required).
 * @returns An object mapping field names to error messages.
 */
export const validateServerForm = (
    formData: ServerFormData,
    isEditMode: boolean
): FormErrors => {
    const errors: FormErrors = {};

    // Name validation
    const trimmedName = formData.name.trim();
    if (!trimmedName) {
        errors.name = 'Name is required';
    }

    // Host validation
    const trimmedHost = formData.host.trim();
    if (!trimmedHost) {
        errors.host = 'Host is required';
    }

    // Port validation
    const port = parseInt(String(formData.port), 10);
    if (Number.isNaN(port) || port < 1 || port > 65535) {
        errors.port = 'Port must be between 1 and 65535';
    }

    // Database validation
    if (!formData.database.trim()) {
        errors.database = 'Maintenance database is required';
    }

    // Username validation
    if (!formData.username.trim()) {
        errors.username = 'Username is required';
    }

    // Password validation - required only in create mode
    if (!isEditMode && !formData.password) {
        errors.password = 'Password is required';
    }

    return errors;
};

/**
 * Prepares form data for saving by trimming strings and parsing port.
 *
 * @param formData - The form data to prepare.
 * @returns The prepared data object ready for API submission.
 */
export const prepareSaveData = (
    formData: ServerFormData
): Record<string, unknown> => {
    const saveData: Record<string, unknown> = {
        name: formData.name.trim(),
        host: formData.host.trim(),
        port: parseInt(String(formData.port), 10),
        database_name: formData.database.trim(),
        username: formData.username.trim(),
        ssl_mode: formData.ssl_mode,
        ssl_cert_path: formData.ssl_cert_path.trim(),
        ssl_key_path: formData.ssl_key_path.trim(),
        ssl_root_cert_path: formData.ssl_root_cert_path.trim(),
        description: formData.description.trim(),
        is_monitored: formData.is_monitored,
        is_shared: formData.is_shared,
    };

    // Only include password if provided
    if (formData.password) {
        saveData.password = formData.password;
    }

    return saveData;
};
