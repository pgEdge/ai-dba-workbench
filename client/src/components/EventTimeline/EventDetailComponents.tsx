/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useMemo, memo } from 'react';
import {
    Box,
    Typography,
    Chip,
    useTheme,
} from '@mui/material';
import {
    ExpandMore as ExpandMoreIcon,
    ExpandLess as ExpandLessIcon,
} from '@mui/icons-material';
import { formatFullTime } from './utils';
import {
    sectionLabelSx,
    sectionLabelShortSx,
    getCodeBlockSx,
    getCodeBlockSmallSx,
    settingNameSx,
    settingValueSx,
    hbaRuleTextSx,
    falsePositiveChipSx,
    metricValueMonoSx,
    metricUnitSx,
    thresholdSx,
    severityChipSx,
    restartCodeBlockSx,
    databaseNameSx,
    ackNameSx,
    ackMessageSx,
    expandableShowMoreBaseSx,
    expandIconSmallSx,
} from './styles';

/**
 * ExpandableList - A list that can be expanded to show all items
 */
export const ExpandableList = memo(({ items, initialLimit, renderItem, emptyText }) => {
    const [showAll, setShowAll] = useState(false);
    const totalCount = items?.length || 0;
    const hasMore = totalCount > initialLimit;
    const displayItems = showAll ? items : items?.slice(0, initialLimit);

    if (!items || items.length === 0) {
        return (
            <Typography sx={{ color: 'text.secondary', fontSize: '0.875rem' }}>
                {emptyText || '0 items'}
            </Typography>
        );
    }

    return (
        <>
            {displayItems.map((item, i) => renderItem(item, i, displayItems.length))}
            {hasMore && (
                <Box
                    onClick={() => setShowAll(!showAll)}
                    sx={expandableShowMoreBaseSx}
                >
                    {showAll ? (
                        <>
                            <ExpandLessIcon sx={expandIconSmallSx} />
                            Show less
                        </>
                    ) : (
                        <>
                            <ExpandMoreIcon sx={expandIconSmallSx} />
                            Show all {totalCount} items (+{totalCount - initialLimit} more)
                        </>
                    )}
                </Box>
            )}
        </>
    );
});

ExpandableList.displayName = 'ExpandableList';

/**
 * ConfigChangeDetails - Shows configuration change details with expandable list
 */
export const ConfigChangeDetails = memo(({ details }) => {
    const theme = useTheme();
    const settings = details?.settings || [];
    const count = details?.setting_count || settings.length || 0;

    const codeBlockSx = useMemo(() => getCodeBlockSx(theme), [theme]);

    return (
        <Box sx={{ mt: 1 }}>
            <Typography sx={sectionLabelSx}>
                Settings ({count})
            </Typography>
            <Box sx={codeBlockSx}>
                <ExpandableList
                    items={settings}
                    initialLimit={10}
                    emptyText={`${count} settings`}
                    renderItem={(setting, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.5 : 0 }}>
                            <Typography
                                component="span"
                                sx={settingNameSx}
                            >
                                {setting.name}
                            </Typography>
                            <Typography
                                component="span"
                                sx={settingValueSx}
                            >
                                {' = '}{setting.value}
                            </Typography>
                        </Box>
                    )}
                />
            </Box>
        </Box>
    );
});

ConfigChangeDetails.displayName = 'ConfigChangeDetails';

/**
 * ExtensionChangeDetails - Shows extension change details
 */
export const ExtensionChangeDetails = memo(({ details }) => {
    const theme = useTheme();
    const extensions = details?.extensions || [];
    const count = details?.extension_count || extensions.length || 0;

    const codeBlockSx = useMemo(() => getCodeBlockSx(theme), [theme]);

    return (
        <Box sx={{ mt: 1 }}>
            <Typography sx={sectionLabelSx}>
                Extensions ({count})
            </Typography>
            <Box sx={codeBlockSx}>
                <ExpandableList
                    items={extensions}
                    initialLimit={10}
                    emptyText={`${count} extensions`}
                    renderItem={(ext, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.5 : 0 }}>
                            <Typography
                                component="span"
                                sx={{
                                    color: theme.palette.custom.status.cyan,
                                    fontWeight: 600,
                                    fontFamily: 'inherit',
                                    fontSize: 'inherit',
                                }}
                            >
                                {ext.name}
                            </Typography>
                            <Typography
                                component="span"
                                sx={settingValueSx}
                            >
                                {' v'}{ext.version}
                            </Typography>
                            {ext.database && (
                                <Typography
                                    component="span"
                                    sx={{ color: 'text.disabled', fontFamily: 'inherit', fontSize: 'inherit' }}
                                >
                                    {' in '}{ext.database}
                                </Typography>
                            )}
                        </Box>
                    )}
                />
            </Box>
        </Box>
    );
});

ExtensionChangeDetails.displayName = 'ExtensionChangeDetails';

/**
 * HbaChangeDetails - Shows HBA rule change details with expandable list
 */
export const HbaChangeDetails = memo(({ details }) => {
    const theme = useTheme();
    const rules = details?.rules || [];
    const count = details?.rule_count || rules.length || 0;

    const codeBlockSx = useMemo(() => getCodeBlockSmallSx(theme), [theme]);

    return (
        <Box sx={{ mt: 1 }}>
            <Typography sx={sectionLabelSx}>
                HBA Rules ({count})
            </Typography>
            <Box sx={codeBlockSx}>
                <ExpandableList
                    items={rules}
                    initialLimit={8}
                    emptyText={`${count} rules`}
                    renderItem={(rule, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.25 : 0 }}>
                            <Typography sx={hbaRuleTextSx}>
                                {rule.type} {rule.database} {rule.user_name} {rule.address || ''} {rule.auth_method}
                            </Typography>
                        </Box>
                    )}
                />
            </Box>
        </Box>
    );
});

HbaChangeDetails.displayName = 'HbaChangeDetails';

/**
 * IdentChangeDetails - Shows ident mapping change details with expandable list
 */
export const IdentChangeDetails = memo(({ details }) => {
    const theme = useTheme();
    const mappings = details?.mappings || [];
    const count = details?.mapping_count || mappings.length || 0;

    const codeBlockSx = useMemo(() => getCodeBlockSmallSx(theme), [theme]);

    return (
        <Box sx={{ mt: 1 }}>
            <Typography sx={sectionLabelSx}>
                Ident Mappings ({count})
            </Typography>
            <Box sx={codeBlockSx}>
                <ExpandableList
                    items={mappings}
                    initialLimit={8}
                    emptyText={`${count} mappings`}
                    renderItem={(mapping, i, total) => (
                        <Box key={i} sx={{ mb: i < total - 1 ? 0.25 : 0 }}>
                            <Typography sx={hbaRuleTextSx}>
                                {mapping.map_name}: {mapping.sys_name} → {mapping.pg_username}
                            </Typography>
                        </Box>
                    )}
                />
            </Box>
        </Box>
    );
});

IdentChangeDetails.displayName = 'IdentChangeDetails';

/**
 * AlertDetails - Shows alert fired/cleared/acknowledged details
 */
export const AlertDetails = memo(({ details, config }) => {
    const theme = useTheme();

    const dbNameSx = useMemo(() => databaseNameSx(theme), [theme]);
    const fpChipSx = useMemo(() => falsePositiveChipSx(theme), [theme]);
    const sevChipSx = useMemo(() => severityChipSx(config.color), [config.color]);

    return (
        <Box sx={{ mt: 1 }}>
            {/* Database name if present */}
            {details?.database_name && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Database
                    </Typography>
                    <Typography sx={dbNameSx}>
                        {details.database_name}
                    </Typography>
                </Box>
            )}
            {/* Acknowledged by info */}
            {details?.acknowledged_by && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Acknowledged By
                    </Typography>
                    <Typography sx={ackNameSx}>
                        {details.acknowledged_by}
                        {details.false_positive && (
                            <Chip
                                label="False Positive"
                                size="small"
                                sx={fpChipSx}
                            />
                        )}
                    </Typography>
                    {details.message && (
                        <Typography sx={ackMessageSx}>
                            "{details.message}"
                        </Typography>
                    )}
                </Box>
            )}
            {details?.metric_value !== undefined && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Metric Value
                    </Typography>
                    <Typography
                        sx={{
                            ...metricValueMonoSx,
                            color: config.color,
                        }}
                    >
                        {details.metric_value}
                        {details.metric_unit && (
                            <Typography
                                component="span"
                                sx={metricUnitSx}
                            >
                                {details.metric_unit}
                            </Typography>
                        )}
                        {details.threshold_value !== undefined && (
                            <Typography
                                component="span"
                                sx={thresholdSx}
                            >
                                {' '}/ threshold: {details.threshold_value}
                                {details.metric_unit && ` ${details.metric_unit}`}
                            </Typography>
                        )}
                    </Typography>
                </Box>
            )}
            {/* Show severity chip - use original_severity for acks, severity otherwise */}
            {(details?.severity || details?.original_severity) && (
                <Chip
                    label={details.original_severity || details.severity}
                    size="small"
                    sx={sevChipSx}
                />
            )}
        </Box>
    );
});

AlertDetails.displayName = 'AlertDetails';

/**
 * RestartDetails - Shows server restart details
 */
export const RestartDetails = memo(({ details }) => {
    const theme = useTheme();
    const codeBlockSx = useMemo(() => restartCodeBlockSx(theme), [theme]);

    if (!details?.previous_timeline && !details?.old_timeline_id) {
        return null;
    }

    return (
        <Box sx={{ mt: 1 }}>
            <Box sx={codeBlockSx}>
                <Typography sx={{ fontSize: '0.875rem', color: 'text.secondary', mb: 0.25 }}>
                    Timeline ID
                </Typography>
                <Typography sx={{ fontFamily: 'inherit', fontSize: 'inherit' }}>
                    {details.previous_timeline || details.old_timeline_id} {'->'} {details.new_timeline || details.new_timeline_id}
                </Typography>
            </Box>
        </Box>
    );
});

RestartDetails.displayName = 'RestartDetails';

/**
 * BlackoutDetails - Shows blackout started/ended details
 */
export const BlackoutDetails = memo(({ details, eventType }) => {
    return (
        <Box sx={{ mt: 1 }}>
            {details?.scope && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Scope
                    </Typography>
                    <Typography sx={ackNameSx}>
                        {details.scope}
                    </Typography>
                </Box>
            )}
            {details?.reason && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Reason
                    </Typography>
                    <Typography sx={{ fontSize: '0.875rem', color: 'text.secondary' }}>
                        {details.reason}
                    </Typography>
                </Box>
            )}
            {details?.created_by && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        Created By
                    </Typography>
                    <Typography sx={ackNameSx}>
                        {details.created_by}
                    </Typography>
                </Box>
            )}
            {eventType === 'blackout_started' && details?.end_time && (
                <Box sx={{ mb: 1 }}>
                    <Typography sx={sectionLabelShortSx}>
                        End Time
                    </Typography>
                    <Typography sx={{ fontSize: '0.875rem', color: 'text.secondary' }}>
                        {formatFullTime(details.end_time)}
                    </Typography>
                </Box>
            )}
        </Box>
    );
});

BlackoutDetails.displayName = 'BlackoutDetails';

/**
 * EventDetails - Renders the appropriate details component based on event type
 */
export const EventDetails = memo(({ event, config }) => {
    if (!event.details) {return null;}

    switch (event.event_type) {
        case 'config_change':
            return <ConfigChangeDetails details={event.details} />;
        case 'extension_change':
            return <ExtensionChangeDetails details={event.details} />;
        case 'hba_change':
            return <HbaChangeDetails details={event.details} />;
        case 'ident_change':
            return <IdentChangeDetails details={event.details} />;
        case 'alert_fired':
        case 'alert_cleared':
        case 'alert_acknowledged':
            return <AlertDetails details={event.details} config={config} />;
        case 'restart':
            return <RestartDetails details={event.details} />;
        case 'blackout_started':
        case 'blackout_ended':
            return <BlackoutDetails details={event.details} eventType={event.event_type} />;
        default:
            return null;
    }
});

EventDetails.displayName = 'EventDetails';
