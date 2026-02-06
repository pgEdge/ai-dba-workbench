/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
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
    Tabs,
    Tab,
    AppBar,
    Toolbar,
    IconButton,
    Slide,
    alpha,
} from '@mui/material';
import { Theme } from '@mui/material/styles';
import { TransitionProps } from '@mui/material/transitions';
import { Close as CloseIcon } from '@mui/icons-material';
import AlertOverridesPanel from './AlertOverridesPanel';
import ProbeOverridesPanel from './ProbeOverridesPanel';
import ChannelOverridesPanel from './ChannelOverridesPanel';

// --- Slide transition for fullScreen dialog ---

const Transition = React.forwardRef(function Transition(
    props: TransitionProps & { children: React.ReactElement },
    ref: React.Ref<unknown>,
) {
    return <Slide direction="up" ref={ref} {...props} />;
});

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
    group?: { id?: number; name?: string; description?: string; is_shared?: boolean } | null;
    isSuperuser?: boolean;
    initialTab?: number;
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
    initialTab = 0,
}) => {
    const [name, setName] = useState('');
    const [description, setDescription] = useState('');
    const [isShared, setIsShared] = useState(false);
    const [error, setError] = useState('');
    const [nameError, setNameError] = useState('');
    const [isSaving, setIsSaving] = useState(false);
    const [activeTab, setActiveTab] = useState(0);

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
            setActiveTab(initialTab);
        }
    }, [open, mode, group, initialTab]);

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

    const formContent = (
        <>
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
        </>
    );

    if (mode === 'edit') {
        return (
            <Dialog
                fullScreen
                open={open}
                onClose={handleClose}
                TransitionComponent={Transition}
            >
                <AppBar
                    position="static"
                    elevation={0}
                    sx={{
                        bgcolor: 'background.paper',
                        borderBottom: '1px solid',
                        borderColor: 'divider',
                    }}
                >
                    <Toolbar>
                        <IconButton
                            edge="start"
                            onClick={handleClose}
                            aria-label="close edit group"
                            sx={{ color: 'text.secondary', mr: 2 }}
                        >
                            <CloseIcon />
                        </IconButton>
                        <Typography
                            variant="h6"
                            component="div"
                            sx={{
                                flexGrow: 1,
                                fontWeight: 600,
                                color: 'text.primary',
                            }}
                        >
                            Edit Cluster Group
                        </Typography>
                    </Toolbar>
                </AppBar>
                <Tabs
                    value={activeTab}
                    onChange={(_, v) => setActiveTab(v)}
                    sx={{ px: 3, borderBottom: 1, borderColor: 'divider' }}
                >
                    <Tab label="Details" />
                    <Tab label="Alert Overrides" />
                    <Tab label="Probe Configuration" />
                    <Tab label="Notification Channels" />
                </Tabs>
                <Box sx={{ flex: 1, overflow: 'auto', p: 3, bgcolor: 'background.default' }}>
                    {activeTab === 0 && (
                        <Box sx={{ maxWidth: 600 }}>
                            <form onSubmit={handleSubmit} noValidate>
                                {formContent}
                                <Box sx={{
                                    mt: 3,
                                    display: 'flex',
                                    gap: 1,
                                    justifyContent: 'flex-end',
                                }}>
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
                                            <CircularProgress
                                                size={20}
                                                color="inherit"
                                            />
                                        ) : (
                                            'Save'
                                        )}
                                    </Button>
                                </Box>
                            </form>
                        </Box>
                    )}
                    {activeTab === 1 && (
                        <AlertOverridesPanel
                            scope="group"
                            scopeId={group?.id as number}
                        />
                    )}
                    {activeTab === 2 && (
                        <ProbeOverridesPanel
                            scope="group"
                            scopeId={group?.id as number}
                        />
                    )}
                    {activeTab === 3 && (
                        <ChannelOverridesPanel
                            scope="group"
                            scopeId={group?.id as number}
                        />
                    )}
                </Box>
            </Dialog>
        );
    }

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
            <DialogTitle sx={dialogTitleSx}>
                Add Cluster Group
            </DialogTitle>
            <form onSubmit={handleSubmit} noValidate>
                <DialogContent>
                    {formContent}
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
