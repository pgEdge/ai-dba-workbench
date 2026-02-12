/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useCallback } from 'react';
import Box from '@mui/material/Box';
import Collapse from '@mui/material/Collapse';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';
import { SxProps, Theme } from '@mui/material/styles';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import ExpandLessIcon from '@mui/icons-material/ExpandLess';
import {
    SECTION_CONTAINER_SX,
    SECTION_HEADER_SX,
    SECTION_TITLE_SX,
} from './styles';

interface CollapsibleSectionProps {
    title: string;
    icon?: React.ReactNode;
    headerRight?: React.ReactNode;
    defaultExpanded?: boolean;
    sx?: SxProps<Theme>;
    children: React.ReactNode;
}

const SECTION_ICON_SX = { fontSize: 16, color: 'primary.main' };
const EXPAND_ICON_SX = { fontSize: 16 };
const EXPAND_BUTTON_SX = { p: 0.25 };

/**
 * A reusable collapsible section with a clickable header that
 * toggles expand/collapse. Uses MUI Collapse for smooth animation.
 */
const CollapsibleSection: React.FC<CollapsibleSectionProps> = ({
    title,
    icon,
    headerRight,
    defaultExpanded = true,
    sx,
    children,
}) => {
    const [expanded, setExpanded] = useState<boolean>(defaultExpanded);

    const handleToggle = useCallback((): void => {
        setExpanded(prev => !prev);
    }, []);

    return (
        <Box sx={[SECTION_CONTAINER_SX, ...(Array.isArray(sx) ? sx : sx ? [sx] : [])]}>
            <Box
                sx={SECTION_HEADER_SX}
                onClick={handleToggle}
                role="button"
                tabIndex={0}
                aria-expanded={expanded}
                aria-label={`${expanded ? 'Collapse' : 'Expand'} ${title} section`}
                onKeyDown={(e: React.KeyboardEvent) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        handleToggle();
                    }
                }}
            >
                {icon && (
                    <Box
                        component="span"
                        sx={{
                            display: 'flex',
                            alignItems: 'center',
                            ...SECTION_ICON_SX,
                        }}
                    >
                        {icon}
                    </Box>
                )}
                <Typography sx={SECTION_TITLE_SX}>
                    {title}
                </Typography>
                <Box sx={{ flex: 1 }} />
                {headerRight && (
                    <Box
                        onClick={(e: React.MouseEvent) => e.stopPropagation()}
                        onKeyDown={(e: React.KeyboardEvent) => e.stopPropagation()}
                        sx={{ display: 'flex', alignItems: 'center' }}
                    >
                        {headerRight}
                    </Box>
                )}
                <IconButton size="small" sx={EXPAND_BUTTON_SX}>
                    {expanded
                        ? <ExpandLessIcon sx={EXPAND_ICON_SX} />
                        : <ExpandMoreIcon sx={EXPAND_ICON_SX} />
                    }
                </IconButton>
            </Box>
            <Collapse in={expanded} timeout="auto" unmountOnExit>
                <Box sx={{ mt: 1 }}>
                    {children}
                </Box>
            </Collapse>
        </Box>
    );
};

export default CollapsibleSection;
