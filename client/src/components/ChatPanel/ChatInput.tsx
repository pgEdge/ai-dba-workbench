/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useRef, useCallback, useEffect } from 'react';
import {
    Box,
    TextField,
    IconButton,
    Typography,
    alpha,
} from '@mui/material';
import { Theme } from '@mui/material/styles';
import { Send as SendIcon } from '@mui/icons-material';

// ---------------------------------------------------------------------------
// Style constants and style-getter functions
// ---------------------------------------------------------------------------

const containerSx = {
    display: 'flex',
    flexDirection: 'column',
    gap: 0.75,
    p: 2,
    borderTop: '1px solid',
    borderColor: 'divider',
};

const inputRowSx = {
    display: 'flex',
    alignItems: 'center',
    gap: 1,
};

const getTextFieldSx = (theme: Theme) => ({
    '& .MuiOutlinedInput-root': {
        fontSize: '1rem',
        fontFamily: '"Inter", sans-serif',
        bgcolor: theme.palette.mode === 'dark'
            ? alpha(theme.palette.background.paper, 0.5)
            : theme.palette.background.paper,
        '& fieldset': {
            borderColor: theme.palette.divider,
        },
        '&:hover fieldset': {
            borderColor: theme.palette.mode === 'dark'
                ? theme.palette.grey[500]
                : theme.palette.grey[400],
        },
        '&.Mui-focused fieldset': {
            borderColor: theme.palette.primary.main,
            borderWidth: 2,
        },
    },
    '& .MuiOutlinedInput-input': {
        py: 1.25,
    },
});

const getSendButtonSx = (disabled: boolean) => (theme: Theme) => ({
    width: 40,
    height: 40,
    borderRadius: 1,
    bgcolor: disabled
        ? 'transparent'
        : theme.palette.primary.main,
    color: disabled
        ? theme.palette.text.disabled
        : theme.palette.primary.contrastText,
    '&:hover': {
        bgcolor: disabled
            ? 'transparent'
            : theme.palette.custom.accentHover,
    },
    '&.Mui-disabled': {
        color: theme.palette.text.disabled,
    },
    transition: 'background-color 0.15s',
});

const hintSx = {
    fontSize: '1rem',
    color: 'text.disabled',
    textAlign: 'right',
    px: 0.5,
    userSelect: 'none',
};

const sendIconSx = { fontSize: 22 };

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface ChatInputProps {
    onSend: (message: string) => void;
    disabled: boolean;
    inputHistory: string[];
}

const ChatInput: React.FC<ChatInputProps> = ({
    onSend,
    disabled,
    inputHistory,
}) => {
    const [value, setValue] = useState('');
    const [historyIndex, setHistoryIndex] = useState(-1);
    const [draftMessage, setDraftMessage] = useState('');
    const inputRef = useRef<HTMLTextAreaElement>(null);
    const prevDisabledRef = useRef(disabled);

    useEffect(() => {
        // Focus the input when it transitions from disabled to enabled
        // (i.e., after the LLM response completes)
        if (prevDisabledRef.current && !disabled && inputRef.current) {
            inputRef.current.focus();
        }
        prevDisabledRef.current = disabled;
    }, [disabled]);

    const handleSend = useCallback(() => {
        const trimmed = value.trim();
        if (!trimmed || disabled) {return;}
        onSend(trimmed);
        setValue('');
        setHistoryIndex(-1);
        setDraftMessage('');
    }, [value, disabled, onSend]);

    const handleKeyDown = useCallback(
        (event: React.KeyboardEvent<HTMLDivElement>) => {
            // Enter sends message; Shift+Enter adds newline
            if (event.key === 'Enter' && !event.shiftKey) {
                event.preventDefault();
                handleSend();
                return;
            }

            // Navigate input history with Up/Down arrows
            if (event.key === 'ArrowUp' && inputHistory.length > 0) {
                const textarea = inputRef.current;
                if (!textarea) {return;}

                // Only navigate when the cursor is at the very start
                const cursorPos = textarea.selectionStart;
                if (cursorPos !== 0) {return;}

                event.preventDefault();
                const newIndex = historyIndex < inputHistory.length - 1
                    ? historyIndex + 1
                    : historyIndex;

                if (historyIndex === -1) {
                    setDraftMessage(value);
                }
                setHistoryIndex(newIndex);
                setValue(inputHistory[newIndex]);
                return;
            }

            if (event.key === 'ArrowDown' && historyIndex >= 0) {
                const textarea = inputRef.current;
                if (!textarea) {return;}

                // Only navigate when the cursor is at the very end
                const cursorPos = textarea.selectionStart;
                if (cursorPos !== value.length) {return;}

                event.preventDefault();
                const newIndex = historyIndex - 1;
                setHistoryIndex(newIndex);
                if (newIndex < 0) {
                    setValue(draftMessage);
                } else {
                    setValue(inputHistory[newIndex]);
                }
                return;
            }
        },
        [handleSend, inputHistory, historyIndex, value, draftMessage],
    );

    const isEmpty = value.trim().length === 0;

    return (
        <Box sx={containerSx}>
            <Box sx={inputRowSx}>
                <TextField
                    inputRef={inputRef}
                    fullWidth
                    multiline
                    maxRows={4}
                    placeholder={disabled ? 'Waiting for response...' : 'Ask Ellie a question...'}
                    value={value}
                    onChange={(e) => {
                        setValue(e.target.value);
                        if (historyIndex >= 0) {
                            setHistoryIndex(-1);
                        }
                    }}
                    onKeyDown={handleKeyDown}
                    disabled={disabled}
                    size="small"
                    sx={getTextFieldSx}
                    aria-label="Chat message input"
                />
                <IconButton
                    onClick={handleSend}
                    disabled={disabled || isEmpty}
                    sx={getSendButtonSx(disabled || isEmpty)}
                    aria-label="Send message"
                >
                    <SendIcon sx={sendIconSx} />
                </IconButton>
            </Box>
            <Typography sx={hintSx}>
                Enter to send, Shift+Enter for newline
            </Typography>
        </Box>
    );
};

export default ChatInput;
