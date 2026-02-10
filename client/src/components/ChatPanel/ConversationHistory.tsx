/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState } from 'react';
import {
    Box,
    Typography,
    List,
    ListItemButton,
    ListItemText,
    IconButton,
    Menu,
    MenuItem,
    ListItemIcon,
    Divider,
    Tooltip,
    alpha,
    TextField,
    Button,
} from '@mui/material';
import { Theme } from '@mui/material/styles';
import {
    MoreVert as MoreIcon,
    Delete as DeleteIcon,
    Edit as EditIcon,
    DeleteSweep as ClearAllIcon,
    Refresh as RefreshIcon,
    Check as CheckIcon,
    Close as CancelIcon,
    Close as CloseIcon,
} from '@mui/icons-material';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface ConversationSummary {
    id: string;
    title: string;
    preview?: string;
    updated_at: string;
    message_count?: number;
}

// ---------------------------------------------------------------------------
// Style constants and style-getter functions
// ---------------------------------------------------------------------------

const overlaySx = {
    position: 'absolute' as const,
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    zIndex: 10,
    display: 'flex',
    flexDirection: 'column',
    bgcolor: 'background.default',
    overflow: 'hidden',
};

const headerSx = {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    px: 1.5,
    py: 1,
    borderBottom: '1px solid',
    borderColor: 'divider',
};

const headerTitleSx = {
    fontSize: '0.8125rem',
    fontWeight: 600,
    color: 'text.secondary',
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
};

const headerActionsSx = {
    display: 'flex',
    gap: 0.25,
};

const smallIconButtonSx = {
    p: 0.5,
};

const smallIconSx = { fontSize: 18 };

const listContainerSx = {
    flex: 1,
    overflowY: 'auto',
    py: 0.5,
    px: 0.5,
};

const getConvItemSx = (isActive: boolean) => (theme: Theme) => ({
    borderRadius: 1,
    mb: 0.25,
    py: 0.75,
    px: 1,
    bgcolor: isActive
        ? alpha(theme.palette.primary.main, 0.15)
        : 'transparent',
    '&:hover': {
        bgcolor: isActive
            ? alpha(theme.palette.primary.main, 0.2)
            : alpha(theme.palette.grey[500], 0.08),
    },
    display: 'flex',
    alignItems: 'flex-start',
    gap: 0.5,
});

const convTitleSx = {
    fontSize: '0.9375rem',
    fontWeight: 500,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

const convPreviewSx = {
    fontSize: '0.8125rem',
    color: 'text.secondary',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
};

const emptyStateSx = {
    flex: 1,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    p: 2,
};

const emptyTextSx = {
    fontSize: '0.875rem',
    color: 'text.disabled',
    textAlign: 'center',
};

const moreMenuIconSx = { fontSize: 18 };

const menuItemTextSx = { fontSize: '0.9375rem' };

const getDeleteMenuItemSx = (theme: Theme) => ({
    color: theme.palette.error.main,
    '&:hover': {
        bgcolor: alpha(theme.palette.error.main, 0.08),
    },
});

const footerSx = {
    borderTop: '1px solid',
    borderColor: 'divider',
    p: 1,
};

const getClearAllButtonSx = (theme: Theme) => ({
    fontSize: '0.875rem',
    color: theme.palette.text.secondary,
    textTransform: 'none',
    justifyContent: 'flex-start',
    px: 1,
    '&:hover': {
        bgcolor: alpha(theme.palette.error.main, 0.08),
        color: theme.palette.error.main,
    },
});

const renameFieldSx = {
    '& .MuiOutlinedInput-root': {
        fontSize: '0.9375rem',
    },
    '& .MuiOutlinedInput-input': {
        py: 0.5,
        px: 0.75,
    },
};

const renameRowSx = {
    display: 'flex',
    alignItems: 'center',
    gap: 0.25,
    flex: 1,
    minWidth: 0,
};

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface ConversationHistoryProps {
    conversations: ConversationSummary[];
    currentId: string | null;
    onSelect: (id: string) => void;
    onDelete: (id: string) => void;
    onRename: (id: string, title: string) => void;
    onClear: () => void;
    onRefresh: () => void;
    onClose: () => void;
}

const ConversationHistory: React.FC<ConversationHistoryProps> = ({
    conversations,
    currentId,
    onSelect,
    onDelete,
    onRename,
    onClear,
    onRefresh,
    onClose,
}) => {
    const [menuAnchor, setMenuAnchor] = useState<null | HTMLElement>(null);
    const [menuConvId, setMenuConvId] = useState<string | null>(null);
    const [renamingId, setRenamingId] = useState<string | null>(null);
    const [renameValue, setRenameValue] = useState('');

    const handleMenuOpen = (
        event: React.MouseEvent<HTMLElement>,
        convId: string,
    ) => {
        event.stopPropagation();
        setMenuAnchor(event.currentTarget);
        setMenuConvId(convId);
    };

    const handleMenuClose = () => {
        setMenuAnchor(null);
        setMenuConvId(null);
    };

    const handleRenameStart = () => {
        if (!menuConvId) {return;}
        const conv = conversations.find((c) => c.id === menuConvId);
        if (conv) {
            setRenamingId(menuConvId);
            setRenameValue(conv.title);
        }
        handleMenuClose();
    };

    const handleRenameSubmit = () => {
        if (renamingId && renameValue.trim()) {
            onRename(renamingId, renameValue.trim());
        }
        setRenamingId(null);
        setRenameValue('');
    };

    const handleRenameCancel = () => {
        setRenamingId(null);
        setRenameValue('');
    };

    const handleDeleteItem = () => {
        if (menuConvId) {
            onDelete(menuConvId);
        }
        handleMenuClose();
    };

    const sorted = [...conversations].sort(
        (a, b) =>
            new Date(b.updated_at).getTime() -
            new Date(a.updated_at).getTime(),
    );

    return (
        <Box sx={overlaySx}>
            {/* Header */}
            <Box sx={headerSx}>
                <Typography sx={headerTitleSx}>History</Typography>
                <Box sx={headerActionsSx}>
                    <Tooltip title="Refresh">
                        <IconButton
                            size="small"
                            onClick={onRefresh}
                            sx={smallIconButtonSx}
                        >
                            <RefreshIcon sx={smallIconSx} />
                        </IconButton>
                    </Tooltip>
                    <Tooltip title="Close">
                        <IconButton
                            size="small"
                            onClick={onClose}
                            sx={smallIconButtonSx}
                            aria-label="Close history"
                        >
                            <CloseIcon sx={smallIconSx} />
                        </IconButton>
                    </Tooltip>
                </Box>
            </Box>

            {/* Conversation list */}
            {sorted.length === 0 ? (
                <Box sx={emptyStateSx}>
                    <Typography sx={emptyTextSx}>
                        No conversations yet
                    </Typography>
                </Box>
            ) : (
                <Box sx={listContainerSx}>
                    <List dense disablePadding>
                        {sorted.map((conv) => {
                            const isActive = conv.id === currentId;
                            const isRenaming = conv.id === renamingId;

                            return (
                                <ListItemButton
                                    key={conv.id}
                                    onClick={() => {
                                        if (!isRenaming) {
                                            onSelect(conv.id);
                                            onClose();
                                        }
                                    }}
                                    sx={getConvItemSx(isActive)}
                                    disableRipple={isRenaming}
                                >
                                    {isRenaming ? (
                                        <Box sx={renameRowSx}>
                                            <TextField
                                                size="small"
                                                value={renameValue}
                                                onChange={(e) =>
                                                    setRenameValue(
                                                        e.target.value,
                                                    )
                                                }
                                                onKeyDown={(e) => {
                                                    if (e.key === 'Enter') {
                                                        handleRenameSubmit();
                                                    }
                                                    if (e.key === 'Escape') {
                                                        handleRenameCancel();
                                                    }
                                                }}
                                                onClick={(e) =>
                                                    e.stopPropagation()
                                                }
                                                autoFocus
                                                fullWidth
                                                sx={renameFieldSx}
                                                aria-label="Rename conversation"
                                            />
                                            <IconButton
                                                size="small"
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    handleRenameSubmit();
                                                }}
                                                sx={smallIconButtonSx}
                                            >
                                                <CheckIcon
                                                    sx={smallIconSx}
                                                />
                                            </IconButton>
                                            <IconButton
                                                size="small"
                                                onClick={(e) => {
                                                    e.stopPropagation();
                                                    handleRenameCancel();
                                                }}
                                                sx={smallIconButtonSx}
                                            >
                                                <CancelIcon
                                                    sx={smallIconSx}
                                                />
                                            </IconButton>
                                        </Box>
                                    ) : (
                                        <>
                                            <ListItemText
                                                primary={conv.title}
                                                secondary={conv.preview}
                                                primaryTypographyProps={
                                                    convTitleSx
                                                }
                                                secondaryTypographyProps={
                                                    convPreviewSx
                                                }
                                                sx={{ m: 0, flex: 1, minWidth: 0 }}
                                            />
                                            <IconButton
                                                size="small"
                                                onClick={(e) =>
                                                    handleMenuOpen(
                                                        e,
                                                        conv.id,
                                                    )
                                                }
                                                sx={{
                                                    ...smallIconButtonSx,
                                                    opacity: 0.5,
                                                    '&:hover': {
                                                        opacity: 1,
                                                    },
                                                    flexShrink: 0,
                                                }}
                                            >
                                                <MoreIcon
                                                    sx={moreMenuIconSx}
                                                />
                                            </IconButton>
                                        </>
                                    )}
                                </ListItemButton>
                            );
                        })}
                    </List>
                </Box>
            )}

            {/* Footer */}
            {sorted.length > 0 && (
                <Box sx={footerSx}>
                    <Button
                        fullWidth
                        size="small"
                        startIcon={<ClearAllIcon sx={smallIconSx} />}
                        onClick={onClear}
                        sx={getClearAllButtonSx}
                    >
                        Clear all
                    </Button>
                </Box>
            )}

            {/* Context menu */}
            <Menu
                anchorEl={menuAnchor}
                open={Boolean(menuAnchor)}
                onClose={handleMenuClose}
                anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
                transformOrigin={{ vertical: 'top', horizontal: 'right' }}
            >
                <MenuItem onClick={handleRenameStart}>
                    <ListItemIcon>
                        <EditIcon sx={smallIconSx} />
                    </ListItemIcon>
                    <Typography sx={menuItemTextSx}>Rename</Typography>
                </MenuItem>
                <Divider />
                <MenuItem onClick={handleDeleteItem} sx={getDeleteMenuItemSx}>
                    <ListItemIcon sx={{ color: 'inherit' }}>
                        <DeleteIcon sx={smallIconSx} />
                    </ListItemIcon>
                    <Typography sx={menuItemTextSx}>Delete</Typography>
                </MenuItem>
            </Menu>
        </Box>
    );
};

export default ConversationHistory;
