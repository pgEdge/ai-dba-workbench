/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - CopyCodeButton component. A small icon button
 * that copies code block content to the clipboard and shows brief feedback.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useCallback } from 'react';
import { IconButton, Tooltip } from '@mui/material';
import { Theme } from '@mui/material/styles';
import {
    ContentCopy as ContentCopyIcon,
    Check as CheckIcon,
} from '@mui/icons-material';
import { getCopyButtonSx } from './markdownStyles';

interface CopyCodeButtonProps {
    code: string;
    theme: Theme;
}

const CopyCodeButton: React.FC<CopyCodeButtonProps> = ({ code, theme }) => {
    const [copied, setCopied] = useState(false);

    const handleCopy = useCallback(async () => {
        try {
            if (!navigator.clipboard?.writeText) {
                throw new Error('Clipboard API unavailable');
            }
            await navigator.clipboard.writeText(code);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        } catch (err) {
            console.error('Failed to copy code:', err);
        }
    }, [code]);

    return (
        <Tooltip title={copied ? 'Copied!' : 'Copy to clipboard'} placement="top">
            <IconButton
                size="small"
                onClick={handleCopy}
                sx={getCopyButtonSx(theme)}
                aria-label="Copy to clipboard"
            >
                {copied ? (
                    <CheckIcon sx={{ fontSize: 16 }} />
                ) : (
                    <ContentCopyIcon sx={{ fontSize: 14 }} />
                )}
            </IconButton>
        </Tooltip>
    );
};

export default CopyCodeButton;
