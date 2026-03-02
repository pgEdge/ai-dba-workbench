/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useEffect, useState, useCallback } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import CircularProgress from '@mui/material/CircularProgress';
import IconButton from '@mui/material/IconButton';
import Alert from '@mui/material/Alert';
import Tabs from '@mui/material/Tabs';
import Tab from '@mui/material/Tab';
import Tooltip from '@mui/material/Tooltip';
import RefreshIcon from '@mui/icons-material/Refresh';
import CollapsibleSection from '../CollapsibleSection';
import PlanTree from './PlanTree';
import { useQueryPlan } from '../../../hooks/useQueryPlan';

interface QueryPlanPanelProps {
    connectionId: number;
    databaseName: string;
    queryText: string;
}

/** Monospace text style for the plan output. */
const PLAN_TEXT_SX = {
    fontFamily: '"JetBrains Mono", "SF Mono", monospace',
    fontSize: '0.8125rem',
    lineHeight: 1.5,
    p: 1.5,
    borderRadius: 1,
    bgcolor: 'background.default',
    whiteSpace: 'pre' as const,
    overflow: 'auto',
    maxHeight: 500,
};

/** Spinning animation for the refresh icon. */
const SPIN_ANIMATION_SX = {
    fontSize: 16,
    animation: 'spin 1s linear infinite',
    '@keyframes spin': {
        '0%': { transform: 'rotate(0deg)' },
        '100%': { transform: 'rotate(360deg)' },
    },
};

/** Static refresh icon style. */
const STATIC_ICON_SX = {
    fontSize: 16,
};

/**
 * Friendly error message for parameterized query failures
 * on older PostgreSQL versions.
 */
const PARAM_ERROR_MSG =
    'This query uses parameters ($1, $2, ...) that cannot be '
    + 'resolved for EXPLAIN on this PostgreSQL version. '
    + 'PostgreSQL 16+ supports EXPLAIN for parameterized queries.';

/**
 * Check whether the error indicates a parameterized query
 * that cannot be explained.
 */
function isParameterError(error: string): boolean {
    const lower = error.toLowerCase();
    return lower.includes(
        'could not determine data type of parameter',
    ) || lower.includes(
        'parameter placeholders',
    ) || lower.includes(
        'unrecognized explain option',
    );
}

/**
 * QueryPlanPanel displays PostgreSQL EXPLAIN output in both
 * text and visual tree formats. The panel is collapsed by
 * default to avoid running EXPLAIN on page load.
 */
const QueryPlanPanel: React.FC<QueryPlanPanelProps> = ({
    connectionId,
    databaseName,
    queryText,
}) => {
    const {
        textPlan,
        jsonPlan,
        loading,
        error,
        fetch: fetchPlan,
    } = useQueryPlan(queryText, connectionId, databaseName);

    const [tabIndex, setTabIndex] = useState<number>(0);

    const handleTabChange = useCallback(
        (_event: React.SyntheticEvent, newValue: number): void => {
            setTabIndex(newValue);
        },
        [],
    );

    // Fetch the plan when the component mounts (section expands).
    // CollapsibleSection uses unmountOnExit, so this fires each
    // time the user expands the section. The hook's internal cache
    // prevents redundant API calls.
    useEffect(() => {
        fetchPlan();
    }, []); // eslint-disable-line react-hooks/exhaustive-deps

    const refreshButton = (
        <Tooltip title={loading ? 'Refreshing...' : 'Refresh plan'}>
            <IconButton
                size="small"
                onClick={fetchPlan}
                disabled={loading}
                aria-label="Refresh query plan"
                sx={{ p: 0.25 }}
            >
                <RefreshIcon
                    sx={loading ? SPIN_ANIMATION_SX : STATIC_ICON_SX}
                />
            </IconButton>
        </Tooltip>
    );

    return (
        <CollapsibleSection
            title="Query Plan"
            defaultExpanded
            storageKey="queryPlanExpanded"
            headerRight={refreshButton}
        >
            {loading && !textPlan && !jsonPlan && (
                <Box sx={{
                    display: 'flex',
                    justifyContent: 'center',
                    py: 4,
                }}>
                    <CircularProgress
                        size={32}
                        aria-label="Loading query plan"
                    />
                </Box>
            )}

            {error && !textPlan && !jsonPlan && (
                <Alert severity="info" sx={{ mt: 1 }}>
                    {isParameterError(error)
                        ? PARAM_ERROR_MSG
                        : error}
                </Alert>
            )}

            {(textPlan || jsonPlan) && (
                <>
                    <Tabs
                        value={tabIndex}
                        onChange={handleTabChange}
                        size="small"
                        sx={{ minHeight: 36, mb: 1 }}
                    >
                        <Tab
                            label="Visual"
                            sx={{
                                minHeight: 36,
                                py: 0,
                                textTransform: 'none',
                                fontSize: '0.8125rem',
                            }}
                        />
                        <Tab
                            label="Text"
                            sx={{
                                minHeight: 36,
                                py: 0,
                                textTransform: 'none',
                                fontSize: '0.8125rem',
                            }}
                        />
                    </Tabs>

                    {tabIndex === 0 && jsonPlan && (
                        <PlanTree plan={jsonPlan} />
                    )}

                    {tabIndex === 0 && !jsonPlan && (
                        <Typography
                            variant="body2"
                            color="text.secondary"
                            sx={{ py: 2, textAlign: 'center' }}
                        >
                            Visual plan not available. The JSON
                            format plan could not be generated.
                        </Typography>
                    )}

                    {tabIndex === 1 && textPlan && (
                        <Typography
                            component="pre"
                            sx={PLAN_TEXT_SX}
                        >
                            {textPlan}
                        </Typography>
                    )}

                    {tabIndex === 1 && !textPlan && (
                        <Typography
                            variant="body2"
                            color="text.secondary"
                            sx={{ py: 2, textAlign: 'center' }}
                        >
                            Text plan not available.
                        </Typography>
                    )}
                </>
            )}
        </CollapsibleSection>
    );
};

export default QueryPlanPanel;
