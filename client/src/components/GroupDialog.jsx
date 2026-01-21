/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Dialog component for adding and editing cluster groups
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect } from 'react';
import {
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    TextField,
    Button,
    Alert,
    FormControlLabel,
    Checkbox,
    Box,
    CircularProgress,
    Typography,
} from '@mui/material';

/**
 * GroupDialog - Dialog for creating and editing cluster groups
 *
 * @param {boolean} open - Whether the dialog is visible
 * @param {function} onClose - Handler called when dialog should close
 * @param {function} onSave - Async handler called with group data on save
 * @param {string} mode - 'create' or 'edit'
 * @param {object} group - Existing group data (for edit mode)
 * @param {boolean} isSuperuser - Whether user can modify shared status
 */
const GroupDialog = ({
    open,
    onClose,
    onSave,
    mode = 'create',
    group = null,
    isSuperuser = false,
}) => {
    const [name, setName] = useState('');
    const [description, setDescription] = useState('');
    const [isShared, setIsShared] = useState(false);
    const [error, setError] = useState('');
    const [nameError, setNameError] = useState('');
    const [isSaving, setIsSaving] = useState(false);

    // Reset form when dialog opens or group changes
    useEffect(() => {
        if (open) {
            if (mode === 'edit' && group) {
                setName(group.name || '');
                setDescription(group.description || '');
                setIsShared(group.is_shared || false);
            } else {
                setName('');
                setDescription('');
                setIsShared(false);
            }
            setError('');
            setNameError('');
        }
    }, [open, mode, group]);

    const validateForm = () => {
        let isValid = true;
        setNameError('');

        const trimmedName = name.trim();
        if (!trimmedName) {
            setNameError('Name is required');
            isValid = false;
        }

        return isValid;
    };

    const handleSubmit = async (e) => {
        e.preventDefault();
        setError('');

        if (!validateForm()) {
            return;
        }

        setIsSaving(true);

        try {
            await onSave({
                name: name.trim(),
                description: description.trim(),
                is_shared: isShared,
            });
            onClose();
        } catch (err) {
            setError(err.message || 'Failed to save group');
        } finally {
            setIsSaving(false);
        }
    };

    const handleNameChange = (e) => {
        setName(e.target.value);
        if (nameError) {
            setNameError('');
        }
    };

    const handleClose = () => {
        if (!isSaving) {
            onClose();
        }
    };

    return (
        <Dialog
            open={open}
            onClose={handleClose}
            maxWidth="xs"
            fullWidth
            PaperProps={{
                sx: {
                    borderRadius: 2,
                },
            }}
        >
            <form onSubmit={handleSubmit} noValidate>
                <DialogTitle
                    sx={{
                        fontWeight: 600,
                        color: '#1F2937',
                        pb: 1,
                    }}
                >
                    {mode === 'create' ? 'Add Cluster Group' : 'Edit Cluster Group'}
                </DialogTitle>

                <DialogContent>
                    {error && (
                        <Alert
                            severity="error"
                            sx={{
                                mb: 2,
                                borderRadius: 1,
                            }}
                        >
                            {error}
                        </Alert>
                    )}

                    <TextField
                        autoFocus
                        fullWidth
                        label="Name"
                        value={name}
                        onChange={handleNameChange}
                        error={!!nameError}
                        helperText={nameError}
                        required
                        disabled={isSaving}
                        margin="dense"
                        sx={{
                            '& .MuiOutlinedInput-root': {
                                borderRadius: 1,
                                '&:hover .MuiOutlinedInput-notchedOutline': {
                                    borderColor: '#9CA3AF',
                                },
                                '&.Mui-focused .MuiOutlinedInput-notchedOutline': {
                                    borderColor: '#15AABF',
                                    borderWidth: 2,
                                },
                            },
                            '& .MuiInputLabel-root.Mui-focused': {
                                color: '#15AABF',
                            },
                        }}
                    />

                    <TextField
                        fullWidth
                        label="Description"
                        value={description}
                        onChange={(e) => setDescription(e.target.value)}
                        disabled={isSaving}
                        margin="dense"
                        multiline
                        rows={3}
                        sx={{
                            mt: 2,
                            '& .MuiOutlinedInput-root': {
                                borderRadius: 1,
                                '&:hover .MuiOutlinedInput-notchedOutline': {
                                    borderColor: '#9CA3AF',
                                },
                                '&.Mui-focused .MuiOutlinedInput-notchedOutline': {
                                    borderColor: '#15AABF',
                                    borderWidth: 2,
                                },
                            },
                            '& .MuiInputLabel-root.Mui-focused': {
                                color: '#15AABF',
                            },
                        }}
                    />

                    {isSuperuser && (
                        <Box sx={{ mt: 2 }}>
                            <FormControlLabel
                                control={
                                    <Checkbox
                                        checked={isShared}
                                        onChange={(e) => setIsShared(e.target.checked)}
                                        disabled={isSaving}
                                        sx={{
                                            color: '#9CA3AF',
                                            '&.Mui-checked': {
                                                color: '#15AABF',
                                            },
                                        }}
                                    />
                                }
                                label="Share with all users"
                            />
                            <Typography
                                variant="caption"
                                sx={{
                                    display: 'block',
                                    color: '#6B7280',
                                    ml: 4,
                                    mt: -0.5,
                                }}
                            >
                                Shared groups are visible to all users
                            </Typography>
                        </Box>
                    )}
                </DialogContent>

                <DialogActions sx={{ px: 3, pb: 2 }}>
                    <Button
                        onClick={handleClose}
                        disabled={isSaving}
                        sx={{
                            color: '#6B7280',
                            '&:hover': {
                                backgroundColor: 'rgba(107, 114, 128, 0.08)',
                            },
                        }}
                    >
                        Cancel
                    </Button>
                    <Button
                        type="submit"
                        variant="contained"
                        disabled={isSaving}
                        sx={{
                            minWidth: 80,
                            borderRadius: 1,
                            fontWeight: 600,
                            textTransform: 'none',
                            background: '#15AABF',
                            boxShadow: '0 4px 14px 0 rgba(14, 165, 233, 0.39)',
                            '&:hover': {
                                background: '#0C8599',
                                boxShadow: '0 6px 20px 0 rgba(14, 165, 233, 0.5)',
                            },
                            '&.Mui-disabled': {
                                background: '#E5E7EB',
                                color: '#9CA3AF',
                            },
                        }}
                    >
                        {isSaving ? (
                            <CircularProgress size={20} color="inherit" />
                        ) : (
                            'Save'
                        )}
                    </Button>
                </DialogActions>
            </form>
        </Dialog>
    );
};

export default GroupDialog;
