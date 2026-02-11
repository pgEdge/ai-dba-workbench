/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useRef, useCallback } from 'react';
import { Box, CircularProgress, Typography, alpha } from '@mui/material';
import { Theme } from '@mui/material/styles';

// ---------------------------------------------------------------------------
// Phrases
// ---------------------------------------------------------------------------

const THINKING_PHRASES = [
    'Herding elephants...',
    'Traversing the indexes...',
    'Vacuuming the tables...',
    'Analyzing the query plan...',
    'Consulting the WAL...',
    'Warming up shared buffers...',
    'Checking the catalog...',
    'Scanning the heap...',
    'Joining the relations...',
    'Parsing the query tree...',
    'Optimizing the plan...',
    'Rewriting the query...',
    'Gathering statistics...',
    'Counting the tuples...',
    'Walking the B-tree...',
    'Inspecting the toast...',
    'Following the foreign keys...',
    'Sorting the results...',
    'Aggregating the data...',
    'Materializing the view...',
    'Locking the resources...',
    'Resolving the dependencies...',
    'Feeding the elephants...',
    'Polishing the tusks...',
    'Migrating the herd...',
    'Trumpeting a response...',
    'Wading through the data lake...',
    'Rummaging through the tablespace...',
    'Peeking at pg_stat_activity...',
    'Defragmenting the fillfactor...',
    'Untangling the execution plan...',
    'Negotiating with the planner...',
    'Asking the oracle... er, PostgreSQL...',
    'Checking the elephant\'s memory...',
    'Rebalancing the partitions...',
];

const CYCLE_INTERVAL_MS = 2500;
const FADE_DURATION_MS = 300;

// ---------------------------------------------------------------------------
// Style constants and style-getter functions
// ---------------------------------------------------------------------------

const containerSx = (theme: Theme) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 1,
    px: 2,
    py: 0.75,
    borderTop: '1px solid',
    borderColor: theme.palette.divider,
});

const spinnerSx = (theme: Theme) => ({
    color: theme.palette.mode === 'dark'
        ? theme.palette.primary.light
        : theme.palette.primary.main,
});

const getTextSx = (opacity: number) => (theme: Theme) => ({
    fontSize: '1rem',
    fontWeight: 500,
    color: theme.palette.mode === 'dark'
        ? alpha(theme.palette.primary.light, 0.85)
        : theme.palette.primary.dark,
    opacity,
    transition: `opacity ${FADE_DURATION_MS}ms ease-in-out`,
    whiteSpace: 'nowrap' as const,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
});

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

const getRandomIndex = (): number =>
    Math.floor(Math.random() * THINKING_PHRASES.length);

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface ThinkingIndicatorProps {
    visible: boolean;
}

const ThinkingIndicator: React.FC<ThinkingIndicatorProps> = ({ visible }) => {
    const [phraseIndex, setPhraseIndex] = useState<number>(getRandomIndex);
    const [opacity, setOpacity] = useState(1);
    const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

    const cyclePhrase = useCallback(() => {
        // Fade out
        setOpacity(0);

        // After fade-out completes, switch phrase and fade in
        setTimeout(() => {
            setPhraseIndex((prev) => {
                let next = getRandomIndex();
                // Avoid repeating the same phrase consecutively
                while (next === prev && THINKING_PHRASES.length > 1) {
                    next = getRandomIndex();
                }
                return next;
            });
            setOpacity(1);
        }, FADE_DURATION_MS);
    }, []);

    useEffect(() => {
        if (visible) {
            // Pick a fresh random phrase each time the indicator appears
            setPhraseIndex(getRandomIndex());
            setOpacity(1);

            intervalRef.current = setInterval(cyclePhrase, CYCLE_INTERVAL_MS);
        } else {
            if (intervalRef.current) {
                clearInterval(intervalRef.current);
                intervalRef.current = null;
            }
        }

        return () => {
            if (intervalRef.current) {
                clearInterval(intervalRef.current);
                intervalRef.current = null;
            }
        };
    }, [visible, cyclePhrase]);

    if (!visible) {
        return null;
    }

    return (
        <Box sx={containerSx} role="status" aria-live="polite">
            <CircularProgress size={14} thickness={5} sx={spinnerSx} />
            <Typography sx={getTextSx(opacity)}>
                {THINKING_PHRASES[phraseIndex]}
            </Typography>
        </Box>
    );
};

export default ThinkingIndicator;
