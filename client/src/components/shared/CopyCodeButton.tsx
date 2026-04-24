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

import type React from 'react';
import { useState, useCallback } from 'react';
import { IconButton, Tooltip } from '@mui/material';
import type { Theme } from '@mui/material/styles';
import {
    ContentCopy as ContentCopyIcon,
    Check as CheckIcon,
} from '@mui/icons-material';
import { getCopyButtonSx } from './markdownStyles';
import { copyToClipboard } from '../../utils/clipboard';
import { logger } from '../../utils/logger';

interface CopyCodeButtonProps {
    code: string;
    theme: Theme;
}

const CopyCodeButton: React.FC<CopyCodeButtonProps> = ({ code, theme }) => {
    const [copied, setCopied] = useState(false);

    const handleCopy = useCallback(async () => {
        try {
            await copyToClipboard(code);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
        } catch (err) {
            logger.error('Failed to copy code:', err);
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
