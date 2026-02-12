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
import Typography from '@mui/material/Typography';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import {
    SECTION_CONTAINER_SX,
    SECTION_HEADER_SX,
    SECTION_TITLE_SX,
} from './styles';

interface CollapsibleSectionProps {
    title: string;
    defaultExpanded?: boolean;
    children: React.ReactNode;
}

const ICON_SX = { fontSize: 18 };

/**
 * A reusable collapsible section with a clickable header that
 * toggles expand/collapse. Uses MUI Collapse for smooth animation.
 */
const CollapsibleSection: React.FC<CollapsibleSectionProps> = ({
    title,
    defaultExpanded = true,
    children,
}) => {
    const [expanded, setExpanded] = useState<boolean>(defaultExpanded);

    const handleToggle = useCallback((): void => {
        setExpanded(prev => !prev);
    }, []);

    return (
        <Box sx={SECTION_CONTAINER_SX}>
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
                {expanded
                    ? <ExpandMoreIcon sx={ICON_SX} />
                    : <ChevronRightIcon sx={ICON_SX} />
                }
                <Typography sx={SECTION_TITLE_SX}>
                    {title}
                </Typography>
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
