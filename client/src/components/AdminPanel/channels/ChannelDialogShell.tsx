/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import {
    Dialog,
    DialogTitle,
    DialogContent,
    DialogActions,
    Button,
    Tab,
    Tabs,
    Alert,
    CircularProgress,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { dialogTitleSx, dialogActionsSx, getContainedButtonSx } from '../styles';

export interface ChannelDialogShellProps {
    open: boolean;
    onClose: () => void;
    title: string;
    tabs: string[];
    activeTab: number;
    onTabChange: (tab: number) => void;
    error: string | null;
    saving: boolean;
    onSave: () => void;
    saveDisabled: boolean;
    saveLabel: string;
    maxWidth?: 'sm' | 'md';
    children: React.ReactNode;
}

export function ChannelDialogShell({
    open,
    onClose,
    title,
    tabs,
    activeTab,
    onTabChange,
    error,
    saving,
    onSave,
    saveDisabled,
    saveLabel,
    maxWidth = 'sm',
    children,
}: ChannelDialogShellProps): React.ReactElement | null {
    const theme = useTheme();
    const containedButtonSx = getContainedButtonSx(theme);

    if (!open) {
        return null;
    }

    return (
        <Dialog
            open={open}
            onClose={onClose}
            maxWidth={maxWidth}
            fullWidth
        >
            <DialogTitle sx={dialogTitleSx}>{title}</DialogTitle>
            <DialogContent>
                {error && (
                    <Alert severity="error" sx={{ mb: 2, mt: 1, borderRadius: 1 }}>
                        {error}
                    </Alert>
                )}
                <Tabs
                    value={activeTab}
                    onChange={(_e, newValue: number) => onTabChange(newValue)}
                    sx={{ mb: 2, borderBottom: 1, borderColor: 'divider' }}
                >
                    {tabs.map((label) => (
                        <Tab key={label} label={label} />
                    ))}
                </Tabs>
                {children}
            </DialogContent>
            <DialogActions sx={dialogActionsSx}>
                <Button onClick={onClose} disabled={saving}>
                    Cancel
                </Button>
                <Button
                    onClick={onSave}
                    variant="contained"
                    disabled={saving || saveDisabled}
                    sx={containedButtonSx}
                >
                    {saving ? (
                        <CircularProgress
                            size={20}
                            color="inherit"
                            aria-label="Saving"
                        />
                    ) : (
                        saveLabel
                    )}
                </Button>
            </DialogActions>
        </Dialog>
    );
}
