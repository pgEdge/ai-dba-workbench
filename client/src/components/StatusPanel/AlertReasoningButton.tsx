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
import { useId, useMemo, useState } from 'react';
import {
    Box,
    Button,
    CircularProgress,
    IconButton,
    Popover,
    Tooltip,
    Typography,
    alpha,
} from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { HelpOutline as ReasoningIcon } from '@mui/icons-material';
import { ICON_14_SX, ICON_16_SX } from '../../theme';
import {
    useAlertReasoning,
    type AlertReasoningState,
} from '../../hooks/useAlertReasoning';

const TOOLTIP = 'Why was this flagged?';

const PAPER_SX = {
    p: 1.5,
    width: 420,
    maxWidth: 'calc(100vw - 32px)',
};

const REASONING_TEXT_SX = {
    // LLM output: plain text only, keeping its line breaks.
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-word',
    maxHeight: 320,
    overflowY: 'auto',
};

const CAPTION_SX = {
    color: 'text.secondary',
    fontStyle: 'italic',
    mt: 1,
};

interface AlertReasoningButtonProps {
    alertId: number | string;
    /** Use the smaller icon and padding of a grouped instance row. */
    compact?: boolean;
}

interface ReasoningContentProps {
    state: AlertReasoningState;
    onRetry: () => void;
}

/**
 * ReasoningContent - The popover body for each state of the lookup.
 */
const ReasoningContent: React.FC<ReasoningContentProps> = ({ state, onRetry }) => {
    switch (state.status) {
        case 'available':
            return (
                <>
                    <Typography variant="body2" sx={REASONING_TEXT_SX}>
                        {state.reasoning}
                    </Typography>
                    {state.source === 'decision' && (
                        <Typography variant="body2" sx={CAPTION_SX}>
                            Explanation generated after the decision
                            model&apos;s verdict.
                        </Typography>
                    )}
                </>
            );
        case 'not_applicable':
            return (
                <Typography variant="body2" color="text.secondary">
                    No detection reasoning is recorded for this alert.
                </Typography>
            );
        case 'unavailable':
        case 'error': {
            const message = state.status === 'unavailable'
                ? 'The detection reasoning is not available right now.'
                : state.message;
            const retryable = state.status === 'unavailable' || state.retryable;
            return (
                <Box>
                    <Typography variant="body2" color="text.secondary">
                        {message}
                    </Typography>
                    {retryable && (
                        <Box sx={{ display: 'flex', justifyContent: 'flex-end', mt: 1 }}>
                            <Button size="small" onClick={onRetry}>
                                Retry
                            </Button>
                        </Box>
                    )}
                </Box>
            );
        }
        default:
            // idle or loading: the request starts as the popover opens.
            return (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <CircularProgress size={16} aria-label="Loading detection reasoning" />
                    <Typography variant="body2" color="text.secondary">
                        Loading detection reasoning; this can take a
                        while the first time.
                    </Typography>
                </Box>
            );
    }
};

/**
 * AlertReasoningButton - Icon button that shows an anomaly alert's
 * Tier 3 detection reasoning in a popover. The reasoning is fetched
 * only when the button is clicked, never on render or polling, since
 * the first request can make the server call an LLM.
 */
const AlertReasoningButton: React.FC<AlertReasoningButtonProps> = ({
    alertId,
    compact = false,
}) => {
    const theme = useTheme();
    const titleId = useId();
    const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
    const { state, load } = useAlertReasoning(alertId);
    const open = anchorEl !== null;

    const buttonSx = useMemo(() => ({
        p: compact ? 0.25 : 0.5,
        color: theme.palette.secondary.main,
        '&:hover': {
            bgcolor: alpha(theme.palette.secondary.main, 0.12),
        },
    }), [compact, theme.palette.secondary]);

    return (
        <>
            <Tooltip title={TOOLTIP} placement="left">
                <IconButton
                    size="small"
                    aria-label={TOOLTIP}
                    aria-haspopup="dialog"
                    aria-expanded={open}
                    onClick={(e) => {
                        e.stopPropagation();
                        setAnchorEl(e.currentTarget);
                        load();
                    }}
                    sx={buttonSx}
                >
                    <ReasoningIcon sx={compact ? ICON_14_SX : ICON_16_SX} />
                </IconButton>
            </Tooltip>
            <Popover
                open={open}
                anchorEl={anchorEl}
                onClose={() => { setAnchorEl(null); }}
                // The popover is portalled, but React events still
                // bubble to the row's ancestors (such as a collapsible
                // group header); keep clicks inside it.
                onClick={(e) => { e.stopPropagation(); }}
                anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
                transformOrigin={{ vertical: 'top', horizontal: 'right' }}
                slotProps={{
                    paper: {
                        sx: PAPER_SX,
                        role: 'dialog',
                        'aria-labelledby': titleId,
                    },
                }}
            >
                <Typography id={titleId} variant="subtitle2" sx={{ mb: 0.75 }}>
                    Detection reasoning
                </Typography>
                <Box aria-live="polite" aria-busy={state.status === 'loading'}>
                    <ReasoningContent state={state} onRetry={() => { load(true); }} />
                </Box>
            </Popover>
        </>
    );
};

export default AlertReasoningButton;
