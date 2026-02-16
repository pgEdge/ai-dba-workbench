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
    oldValueSx,
    newValueSx,
    removedLineSx,
    addedLineSx,
    changeArrowSx,
    changeTypePrefixSx,
    noChangesSx,
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
 * ConfigChangeDetails - Shows configuration change details with expandable list.
 * Supports both diff format (details.changes) and snapshot format (details.settings).
 */
export const ConfigChangeDetails = memo(({ details }) => {
    const theme = useTheme();
    const codeBlockSx = useMemo(() => getCodeBlockSx(theme), [theme]);

    // New diff format: details.changes array exists
    if (details?.changes) {
        const changes = details.changes;
        const count = details.change_count || changes.length || 0;

        if (changes.length === 0 || count === 0) {
            return (
                <Box sx={{ mt: 1 }}>
                    <Typography sx={sectionLabelSx}>
                        Changes (0)
                    </Typography>
                    <Typography sx={noChangesSx}>
                        No actual changes detected
                    </Typography>
                </Box>
            );
        }

        return (
            <Box sx={{ mt: 1 }}>
                <Typography sx={sectionLabelSx}>
                    Changes ({count})
                </Typography>
                <Box sx={codeBlockSx}>
                    <ExpandableList
                        items={changes}
                        initialLimit={10}
                        emptyText={`${count} changes`}
                        renderItem={(change, i, total) => (
                            <Box key={i} sx={{ mb: i < total - 1 ? 0.5 : 0 }}>
                                {change.change_type === 'modified' && (
                                    <Box>
                                        <Typography
                                            component="span"
                                            sx={settingNameSx}
                                        >
                                            {change.name}:
                                        </Typography>
                                        {' '}
                                        <Typography
                                            component="span"
                                            sx={oldValueSx}
                                        >
                                            {change.old_value}
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={changeArrowSx}
                                        >
                                            {'\u2192'}
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={newValueSx}
                                        >
                                            {change.new_value}
                                        </Typography>
                                    </Box>
                                )}
                                {change.change_type === 'added' && (
                                    <Box>
                                        <Typography
                                            component="span"
                                            sx={{ ...changeTypePrefixSx, color: 'success.main' }}
                                        >
                                            +
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={settingNameSx}
                                        >
                                            {change.name}
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={addedLineSx}
                                        >
                                            {' = '}{change.new_value}
                                        </Typography>
                                    </Box>
                                )}
                                {change.change_type === 'removed' && (
                                    <Box>
                                        <Typography
                                            component="span"
                                            sx={{ ...changeTypePrefixSx, color: 'error.main' }}
                                        >
                                            -
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={{ ...settingNameSx, ...removedLineSx }}
                                        >
                                            {change.name}
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={removedLineSx}
                                        >
                                            {' = '}{change.old_value}
                                        </Typography>
                                    </Box>
                                )}
                            </Box>
                        )}
                    />
                </Box>
            </Box>
        );
    }

    // Old snapshot format: details.settings
    const settings = details?.settings || [];
    const count = details?.setting_count || settings.length || 0;

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
 * ExtensionChangeDetails - Shows extension change details.
 * Supports both diff format (details.changes) and snapshot format (details.extensions).
 */
export const ExtensionChangeDetails = memo(({ details }) => {
    const theme = useTheme();
    const codeBlockSx = useMemo(() => getCodeBlockSx(theme), [theme]);

    const extNameSx = useMemo(() => ({
        color: theme.palette.custom.status.cyan,
        fontWeight: 600,
        fontFamily: 'inherit',
        fontSize: 'inherit',
    }), [theme]);

    const extDbSx = { color: 'text.disabled', fontFamily: 'inherit', fontSize: 'inherit' };

    // New diff format: details.changes array exists
    if (details?.changes) {
        const changes = details.changes;
        const count = details.change_count || changes.length || 0;

        if (changes.length === 0 || count === 0) {
            return (
                <Box sx={{ mt: 1 }}>
                    <Typography sx={sectionLabelSx}>
                        Changes (0)
                    </Typography>
                    <Typography sx={noChangesSx}>
                        No actual changes detected
                    </Typography>
                </Box>
            );
        }

        return (
            <Box sx={{ mt: 1 }}>
                <Typography sx={sectionLabelSx}>
                    Changes ({count})
                </Typography>
                <Box sx={codeBlockSx}>
                    <ExpandableList
                        items={changes}
                        initialLimit={10}
                        emptyText={`${count} changes`}
                        renderItem={(change, i, total) => (
                            <Box key={i} sx={{ mb: i < total - 1 ? 0.5 : 0 }}>
                                {change.change_type === 'added' && (
                                    <Box>
                                        <Typography
                                            component="span"
                                            sx={{ ...changeTypePrefixSx, color: 'success.main' }}
                                        >
                                            +
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={addedLineSx}
                                        >
                                            {change.name} v{change.version}
                                        </Typography>
                                        {change.database && (
                                            <Typography
                                                component="span"
                                                sx={addedLineSx}
                                            >
                                                {' in '}{change.database}
                                            </Typography>
                                        )}
                                    </Box>
                                )}
                                {change.change_type === 'removed' && (
                                    <Box>
                                        <Typography
                                            component="span"
                                            sx={{ ...changeTypePrefixSx, color: 'error.main' }}
                                        >
                                            -
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={removedLineSx}
                                        >
                                            {change.name} v{change.old_version}
                                        </Typography>
                                        {change.database && (
                                            <Typography
                                                component="span"
                                                sx={removedLineSx}
                                            >
                                                {' in '}{change.database}
                                            </Typography>
                                        )}
                                    </Box>
                                )}
                                {change.change_type === 'modified' && (
                                    <Box>
                                        <Typography
                                            component="span"
                                            sx={extNameSx}
                                        >
                                            {change.name}
                                        </Typography>
                                        {' '}
                                        <Typography
                                            component="span"
                                            sx={oldValueSx}
                                        >
                                            v{change.old_version}
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={changeArrowSx}
                                        >
                                            {'\u2192'}
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={newValueSx}
                                        >
                                            v{change.version}
                                        </Typography>
                                        {change.database && (
                                            <Typography
                                                component="span"
                                                sx={extDbSx}
                                            >
                                                {' in '}{change.database}
                                            </Typography>
                                        )}
                                    </Box>
                                )}
                            </Box>
                        )}
                    />
                </Box>
            </Box>
        );
    }

    // Old snapshot format: details.extensions
    const extensions = details?.extensions || [];
    const count = details?.extension_count || extensions.length || 0;

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
                                sx={extNameSx}
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
                                    sx={extDbSx}
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
 * HbaChangeDetails - Shows HBA rule change details with expandable list.
 * Supports both diff format (details.changes) and snapshot format (details.rules).
 */
export const HbaChangeDetails = memo(({ details }) => {
    const theme = useTheme();
    const codeBlockSx = useMemo(() => getCodeBlockSmallSx(theme), [theme]);

    // New diff format: details.changes array exists
    if (details?.changes) {
        const changes = details.changes;
        const count = details.change_count || changes.length || 0;

        if (changes.length === 0 || count === 0) {
            return (
                <Box sx={{ mt: 1 }}>
                    <Typography sx={sectionLabelSx}>
                        Changes (0)
                    </Typography>
                    <Typography sx={noChangesSx}>
                        No actual changes detected
                    </Typography>
                </Box>
            );
        }

        return (
            <Box sx={{ mt: 1 }}>
                <Typography sx={sectionLabelSx}>
                    Changes ({count})
                </Typography>
                <Box sx={codeBlockSx}>
                    <ExpandableList
                        items={changes}
                        initialLimit={8}
                        emptyText={`${count} changes`}
                        renderItem={(change, i, total) => (
                            <Box key={i} sx={{ mb: i < total - 1 ? 0.25 : 0 }}>
                                {change.change_type === 'added' && (
                                    <Box>
                                        <Typography
                                            component="span"
                                            sx={{ ...changeTypePrefixSx, color: 'success.main' }}
                                        >
                                            +
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={addedLineSx}
                                        >
                                            {change.type} {change.database} {change.user_name} {change.address || ''} {change.auth_method}
                                        </Typography>
                                    </Box>
                                )}
                                {change.change_type === 'removed' && (
                                    <Box>
                                        <Typography
                                            component="span"
                                            sx={{ ...changeTypePrefixSx, color: 'error.main' }}
                                        >
                                            -
                                        </Typography>
                                        <Typography
                                            component="span"
                                            sx={removedLineSx}
                                        >
                                            {change.prev_type} {change.prev_database} {change.prev_user_name} {change.prev_address || ''} {change.prev_auth_method}
                                        </Typography>
                                    </Box>
                                )}
                                {change.change_type === 'modified' && (
                                    <Box>
                                        <Box>
                                            <Typography
                                                component="span"
                                                sx={{ ...changeTypePrefixSx, color: 'error.main' }}
                                            >
                                                -
                                            </Typography>
                                            <Typography
                                                component="span"
                                                sx={removedLineSx}
                                            >
                                                {change.prev_type} {change.prev_database} {change.prev_user_name} {change.prev_address || ''} {change.prev_auth_method}
                                            </Typography>
                                        </Box>
                                        <Box>
                                            <Typography
                                                component="span"
                                                sx={{ ...changeTypePrefixSx, color: 'success.main' }}
                                            >
                                                +
                                            </Typography>
                                            <Typography
                                                component="span"
                                                sx={hbaRuleTextSx}
                                            >
                                                {change.type} {change.database} {change.user_name} {change.address || ''} {change.auth_method}
                                            </Typography>
                                        </Box>
                                    </Box>
                                )}
                            </Box>
                        )}
                    />
                </Box>
            </Box>
        );
    }

    // Old snapshot format: details.rules
    const rules = details?.rules || [];
    const count = details?.rule_count || rules.length || 0;

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
