/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { Fab, Tooltip, keyframes } from '@mui/material';
import type { Theme } from '@mui/material/styles';
import {
    SmartToyOutlined as ChatIcon,
    Close as CloseIcon,
} from '@mui/icons-material';

// ---------------------------------------------------------------------------
// Style constants and style-getter functions
// ---------------------------------------------------------------------------

const pulseRing = keyframes`
    0% {
        box-shadow: 0 0 0 0 rgba(21, 170, 191, 0.4);
    }
    70% {
        box-shadow: 0 0 0 10px rgba(21, 170, 191, 0);
    }
    100% {
        box-shadow: 0 0 0 0 rgba(21, 170, 191, 0);
    }
`;

const getFabSx = (isOpen: boolean) => (theme: Theme) => ({
    position: 'fixed',
    bottom: 24,
    right: 24,
    zIndex: 1050,
    bgcolor: theme.palette.primary.main,
    color: theme.palette.primary.contrastText,
    '&:hover': {
        bgcolor: theme.palette.custom.accentHover,
    },
    boxShadow: theme.palette.mode === 'dark'
        ? '0 4px 12px rgba(0, 0, 0, 0.4)'
        : '0 4px 12px rgba(0, 0, 0, 0.15)',
    transition: 'background-color 0.2s, transform 0.2s, box-shadow 0.2s',
    ...(!isOpen && {
        animation: `${pulseRing} 2s ease-out 1`,
    }),
});

const iconSx = {
    fontSize: 26,
    transition: 'transform 0.2s ease-in-out',
};

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface ChatFABProps {
    onClick: () => void;
    isOpen: boolean;
}

const ChatFAB: React.FC<ChatFABProps> = ({ onClick, isOpen }) => {
    return (
        <Tooltip
            title={isOpen ? 'Close AI Chat' : 'Open AI Chat'}
            placement="left"
        >
            <Fab
                onClick={onClick}
                aria-label={isOpen ? 'close chat' : 'open chat'}
                sx={getFabSx(isOpen)}
            >
                {isOpen ? (
                    <CloseIcon sx={iconSx} />
                ) : (
                    <ChatIcon sx={iconSx} />
                )}
            </Fab>
        </Tooltip>
    );
};

export default ChatFAB;
