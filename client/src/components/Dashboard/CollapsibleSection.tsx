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
import { useState, useCallback, useEffect } from 'react';
import Box from '@mui/material/Box';
import Collapse from '@mui/material/Collapse';
import IconButton from '@mui/material/IconButton';
import Typography from '@mui/material/Typography';
import type { SxProps, Theme } from '@mui/material/styles';
import {
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
} from '@mui/icons-material';
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
    storageKey?: string;
    sx?: SxProps<Theme>;
    children: React.ReactNode;
    /**
     * When true, forces the section to initially display as collapsed
     * regardless of the user's stored preference. Users can still
     * manually expand/collapse the section, but this temporary state
     * is NOT persisted to localStorage. When forceCollapsed becomes
     * false, the section reverts to the stored preference.
     */
    forceCollapsed?: boolean;
    /**
     * Message to display in the header when forceCollapsed is true AND
     * the section is in its collapsed state. The message disappears
     * when the user manually expands the section.
     */
    forceCollapsedMessage?: string;
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
    storageKey,
    sx,
    children,
    forceCollapsed = false,
    forceCollapsedMessage,
}) => {
    /** Derive a localStorage key from the title when none is given. */
    const effectiveStorageKey = storageKey
        ?? `dashboard-section-${title.toLowerCase().replace(/\s+/g, '-')}-expanded`;

    /** The persisted expand/collapse state stored in localStorage. */
    const [expanded, setExpanded] = useState<boolean>(() => {
        try {
            const stored = localStorage.getItem(
                effectiveStorageKey
            );
            if (stored !== null && stored !== undefined) {
                return stored === 'true';
            }
        } catch {
            // localStorage may be unavailable; fall through
        }
        return defaultExpanded;
    });

    /**
     * Temporary override state when forceCollapsed is true.
     * null means no override (use default collapsed state).
     * true/false means user has manually toggled while forceCollapsed is active.
     */
    const [temporaryOverride, setTemporaryOverride] = useState<boolean | null>(null);

    // Reset the temporary override when forceCollapsed changes to false
    useEffect(() => {
        if (!forceCollapsed) {
            setTemporaryOverride(null);
        }
    }, [forceCollapsed]);

    const handleToggle = useCallback((): void => {
        if (forceCollapsed) {
            // When forceCollapsed, toggle the temporary override instead
            // of modifying the persisted state
            setTemporaryOverride(prev => {
                // If no override yet, we're currently collapsed, so expand
                if (prev === null) { return true; }
                // Otherwise toggle the override
                return !prev;
            });
        } else {
            setExpanded(prev => {
                const next = !prev;
                try {
                    localStorage.setItem(
                        effectiveStorageKey, String(next)
                    );
                } catch {
                    // localStorage may be unavailable; ignore
                }
                return next;
            });
        }
    }, [effectiveStorageKey, forceCollapsed]);

    // Determine effective display state:
    // - When forceCollapsed is true: use temporaryOverride if set, otherwise collapsed
    // - When forceCollapsed is false: use the persisted expanded state
    const isExpanded = forceCollapsed
        ? (temporaryOverride ?? false)
        : expanded;

    // Show message only when forceCollapsed is true AND section is actually collapsed
    const showForceCollapsedMessage = forceCollapsed && !isExpanded && forceCollapsedMessage;

    return (
        <Box sx={[SECTION_CONTAINER_SX, ...(Array.isArray(sx) ? sx : sx ? [sx] : [])]}>
            <Box
                sx={SECTION_HEADER_SX}
                onClick={handleToggle}
                role="button"
                tabIndex={0}
                aria-expanded={isExpanded}
                aria-label={`${isExpanded ? 'Collapse' : 'Expand'} ${title} section`}
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
                {showForceCollapsedMessage && (
                    <Typography
                        component="span"
                        sx={{
                            ml: 1.5,
                            fontSize: '0.75rem',
                            color: 'text.secondary',
                            fontStyle: 'italic',
                        }}
                    >
                        {forceCollapsedMessage}
                    </Typography>
                )}
                <Box sx={{ flex: 1 }} />
                {headerRight && (
                    <Box
                        onClick={(e: React.MouseEvent) => { e.stopPropagation(); }}
                        onKeyDown={(e: React.KeyboardEvent) => { e.stopPropagation(); }}
                        sx={{ display: 'flex', alignItems: 'center' }}
                    >
                        {headerRight}
                    </Box>
                )}
                <IconButton size="small" sx={EXPAND_BUTTON_SX}>
                    {isExpanded
                        ? <ExpandLessIcon sx={EXPAND_ICON_SX} />
                        : <ExpandMoreIcon sx={EXPAND_ICON_SX} />
                    }
                </IconButton>
            </Box>
            <Collapse in={isExpanded} timeout="auto" unmountOnExit>
                <Box sx={{ mt: 1 }}>
                    {children}
                </Box>
            </Collapse>
        </Box>
    );
};

export default CollapsibleSection;
