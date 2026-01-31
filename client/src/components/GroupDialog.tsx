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
    alpha,
} from '@mui/material';
import { Theme } from '@mui/material/styles';

// --- Style constants (Issue 23) ---

const dialogPaperSx = {
    borderRadius: 2,
};

const dialogTitleSx = {
    fontWeight: 600,
    color: 'text.primary',
    pb: 1,
};

const alertSx = {
    mb: 2,
    borderRadius: 1,
};

const textFieldSx = {
    '& .MuiOutlinedInput-root': {
        borderRadius: 1,
        '&:hover .MuiOutlinedInput-notchedOutline': {
            borderColor: 'grey.400',
        },
        '&.Mui-focused .MuiOutlinedInput-notchedOutline': {
            borderColor: 'primary.main',
            borderWidth: 2,
        },
    },
    '& .MuiInputLabel-root.Mui-focused': {
        color: 'primary.main',
    },
};

const descriptionFieldSx = {
    mt: 2,
    ...textFieldSx,
};

const checkboxSx = {
    color: 'grey.400',
    '&.Mui-checked': {
        color: 'primary.main',
    },
};

const sharedHelpTextSx = {
    display: 'block',
    color: 'text.secondary',
    ml: 4,
    mt: -0.5,
};

const cancelButtonSx = (theme: Theme) => ({
    color: theme.palette.text.secondary,
    '&:hover': {
        backgroundColor: alpha(theme.palette.text.secondary, 0.08),
    },
});

const getSaveButtonSx = (theme: Theme) => ({
    minWidth: 80,
    borderRadius: 1,
    fontWeight: 600,
    textTransform: 'none',
    background: theme.palette.primary.main,
    boxShadow: '0 4px 14px 0 rgba(14, 165, 233, 0.39)',
    '&:hover': {
        background: theme.palette.primary.dark,
        boxShadow: '0 6px 20px 0 rgba(14, 165, 233, 0.5)',
    },
    '&.Mui-disabled': {
        background: theme.palette.grey[200],
        color: theme.palette.grey[400],
    },
});

const dialogActionsSx = {
    px: 3,
    pb: 2,
};

// --- Component ---

interface GroupData {
    name: string;
    description?: string;
    is_shared?: boolean;
}

interface GroupDialogProps {
    open: boolean;
    onClose: () => void;
    onSave: (data: GroupData) => Promise<void>;
    mode?: 'create' | 'edit';
    group?: { name?: string; description?: string; is_shared?: boolean } | null;
    isSuperuser?: boolean;
}

/**
 * GroupDialog - Dialog for creating and editing cluster groups
 */
const GroupDialog: React.FC<GroupDialogProps> = ({
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

    const handleSubmit = async (e: React.FormEvent<HTMLFormElement>) => {
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
        } catch (err: unknown) {
            setError(err instanceof Error ? err.message : 'Failed to save group');
        } finally {
            setIsSaving(false);
        }
    };

    const handleNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
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
                sx: dialogPaperSx,
            }}
        >
            <form onSubmit={handleSubmit} noValidate>
                <DialogTitle sx={dialogTitleSx}>
                    {mode === 'create' ? 'Add Cluster Group' : 'Edit Cluster Group'}
                </DialogTitle>

                <DialogContent>
                    {error && (
                        <Alert
                            severity="error"
                            sx={alertSx}
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
                        sx={textFieldSx}
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
                        sx={descriptionFieldSx}
                    />

                    {isSuperuser && (
                        <Box sx={{ mt: 2 }}>
                            <FormControlLabel
                                control={
                                    <Checkbox
                                        checked={isShared}
                                        onChange={(e) => setIsShared(e.target.checked)}
                                        disabled={isSaving}
                                        sx={checkboxSx}
                                    />
                                }
                                label="Share with all users"
                            />
                            <Typography
                                variant="caption"
                                sx={sharedHelpTextSx}
                            >
                                Shared groups are visible to all users
                            </Typography>
                        </Box>
                    )}
                </DialogContent>

                <DialogActions sx={dialogActionsSx}>
                    <Button
                        onClick={handleClose}
                        disabled={isSaving}
                        sx={cancelButtonSx}
                    >
                        Cancel
                    </Button>
                    <Button
                        type="submit"
                        variant="contained"
                        disabled={isSaving}
                        sx={getSaveButtonSx}
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
