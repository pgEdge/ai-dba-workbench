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
import { useState } from 'react';
import { Box, Typography, Collapse } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
} from '@mui/icons-material';
import {
    SECTION_STATE_KEY,
    MONO_FONT,
    getSectionHeaderSx,
    getSectionTitleSx,
    getSectionContentSx,
} from '../serverInfoStyles';

export interface SectionProps {
    sectionId: string;
    icon: React.ReactNode;
    title: string;
    defaultOpen?: boolean;
    children: React.ReactNode;
    badge?: string;
}

/**
 * Collapsible section wrapper with localStorage state persistence.
 */
const Section: React.FC<SectionProps> = ({
    sectionId,
    icon,
    title,
    defaultOpen = true,
    children,
    badge,
}) => {
    const theme = useTheme();
    const [open, setOpen] = useState(() => {
        try {
            const stored = localStorage.getItem(SECTION_STATE_KEY);
            if (stored) {
                const state = JSON.parse(stored);
                if (sectionId in state) {
                    return state[sectionId];
                }
            }
        } catch {
            /* ignore */
        }
        return defaultOpen;
    });

    const handleToggle = () => {
        const next = !open;
        setOpen(next);
        try {
            const stored = localStorage.getItem(SECTION_STATE_KEY);
            const state = stored ? JSON.parse(stored) : {};
            state[sectionId] = next;
            localStorage.setItem(SECTION_STATE_KEY, JSON.stringify(state));
        } catch {
            /* ignore */
        }
    };

    return (
        <Box>
            <Box
                sx={getSectionHeaderSx(theme)}
                onClick={handleToggle}
                role="button"
                tabIndex={0}
                aria-expanded={open}
                onKeyDown={(e: React.KeyboardEvent) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        handleToggle();
                    }
                }}
            >
                {icon}
                <Typography sx={getSectionTitleSx()}>
                    {title}
                </Typography>
                {badge && (
                    <Typography sx={{
                        fontSize: '0.875rem',
                        fontWeight: 600,
                        color: 'text.disabled',
                        fontFamily: MONO_FONT,
                    }}>
                        {badge}
                    </Typography>
                )}
                {open
                    ? <ExpandLessIcon sx={{ fontSize: 16, color: 'text.disabled' }} />
                    : <ExpandMoreIcon sx={{ fontSize: 16, color: 'text.disabled' }} />
                }
            </Box>
            <Collapse in={open}>
                <Box sx={getSectionContentSx(theme)}>
                    {children}
                </Box>
            </Collapse>
        </Box>
    );
};

export default Section;
