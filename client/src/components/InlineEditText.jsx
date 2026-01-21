/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Reusable inline text editing component
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useRef, useEffect } from 'react';
import { Typography, TextField, CircularProgress, Box } from '@mui/material';

/**
 * InlineEditText - A component that displays text and allows inline editing
 *
 * @param {string} value - The current text value
 * @param {function} onSave - Async function called with new value on save
 * @param {boolean} canEdit - Whether editing is allowed
 * @param {object} typographyProps - Props to pass to Typography when displaying
 * @param {object} textFieldProps - Props to pass to TextField when editing
 * @param {object} sx - Additional styles for the container
 */
const InlineEditText = ({
    value,
    onSave,
    canEdit = false,
    typographyProps = {},
    textFieldProps = {},
    sx = {},
}) => {
    const [isEditing, setIsEditing] = useState(false);
    const [editValue, setEditValue] = useState(value);
    const [isSaving, setIsSaving] = useState(false);
    const [error, setError] = useState(null);
    const inputRef = useRef(null);

    // Update editValue when value prop changes
    useEffect(() => {
        if (!isEditing) {
            setEditValue(value);
        }
    }, [value, isEditing]);

    // Focus input when entering edit mode
    useEffect(() => {
        if (isEditing && inputRef.current) {
            inputRef.current.focus();
            inputRef.current.select();
        }
    }, [isEditing]);

    const handleDoubleClick = (e) => {
        if (!canEdit) return;
        e.stopPropagation();
        e.preventDefault();
        setIsEditing(true);
        setEditValue(value);
        setError(null);
    };

    const handleKeyDown = (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            handleSave();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            handleCancel();
        }
    };

    const handleCancel = () => {
        setIsEditing(false);
        setEditValue(value);
        setError(null);
    };

    const handleSave = async () => {
        const trimmedValue = editValue.trim();

        // Don't save if value hasn't changed
        if (trimmedValue === value) {
            setIsEditing(false);
            return;
        }

        // Validate non-empty
        if (!trimmedValue) {
            setError('Name cannot be empty');
            return;
        }

        setIsSaving(true);
        setError(null);

        try {
            await onSave(trimmedValue);
            setIsEditing(false);
        } catch (err) {
            setError(err.message || 'Failed to save');
            // Keep editing mode open so user can retry or cancel
        } finally {
            setIsSaving(false);
        }
    };

    const handleBlur = () => {
        // Don't cancel on blur if we're saving
        if (!isSaving) {
            handleSave();
        }
    };

    if (isEditing) {
        return (
            <Box
                sx={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 0.5,
                    minWidth: 0,
                    flex: 1,
                    ...sx,
                }}
                onClick={(e) => e.stopPropagation()}
            >
                <TextField
                    inputRef={inputRef}
                    value={editValue}
                    onChange={(e) => setEditValue(e.target.value)}
                    onKeyDown={handleKeyDown}
                    onBlur={handleBlur}
                    disabled={isSaving}
                    error={!!error}
                    helperText={error}
                    size="small"
                    variant="standard"
                    sx={{
                        flex: 1,
                        minWidth: 0,
                        '& .MuiInputBase-input': {
                            fontSize: '0.8125rem',
                            padding: '2px 4px',
                        },
                        '& .MuiInput-underline:before': {
                            borderBottomColor: 'primary.main',
                        },
                    }}
                    {...textFieldProps}
                />
                {isSaving && (
                    <CircularProgress size={14} />
                )}
            </Box>
        );
    }

    return (
        <Typography
            onDoubleClick={handleDoubleClick}
            sx={{
                cursor: canEdit ? 'text' : 'default',
                userSelect: canEdit ? 'none' : 'auto',
                ...typographyProps.sx,
            }}
            {...typographyProps}
        >
            {value}
        </Typography>
    );
};

export default InlineEditText;
