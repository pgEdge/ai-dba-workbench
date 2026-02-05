/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { Box, Typography, Chip } from '@mui/material';
import { alpha, useTheme } from '@mui/material/styles';
import type { SvgIconComponent } from '@mui/icons-material';
import {
    Storage as ConnectionIcon,
    AdminPanelSettings as AdminIcon,
    SmartToy as McpIcon,
    Group as GroupIcon,
} from '@mui/icons-material';
import { categoryLabelSx } from './styles';

const ADMIN_PERMISSION_LABELS: Record<string, string> = {
    manage_blackouts: 'Manage Blackouts',
    manage_connections: 'Manage Connections',
    manage_groups: 'Manage Groups',
    manage_permissions: 'Manage Permissions',
    manage_probes: 'Manage Probes',
    manage_alert_rules: 'Manage Alert Rules',
    manage_token_scopes: 'Manage Token Scopes',
    manage_notification_channels: 'Manage Notification Channels',
    manage_users: 'Manage Users',
};

interface CategoryCardProps {
    icon: SvgIconComponent;
    label: string;
    children: React.ReactNode;
}

const CategoryCard: React.FC<CategoryCardProps> = ({ icon: Icon, label, children }) => (
    <Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mb: 1 }}>
            <Icon sx={{ fontSize: 16, color: 'text.secondary' }} />
            <Typography sx={categoryLabelSx}>
                {label}
            </Typography>
        </Box>
        {children}
    </Box>
);

const EmptyState = () => (
    <Typography sx={{ fontSize: '0.8rem', color: 'text.disabled', fontStyle: 'italic' }}>
        None
    </Typography>
);

interface EffectivePermissionsPanelProps {
    connectionPrivileges?: Record<string, string[]> | Array<{ server_id: number; privileges: string[] }>;
    adminPermissions?: string[];
    mcpPrivileges?: Record<string, string[]> | Array<{ server_id: number; tools: string[] }>;
    isSuperuser?: boolean;
    connections?: Array<{ id: number; name: string }>;
    isDark?: boolean;
    groups?: Array<{ id: number; name: string }>;
}

const EffectivePermissionsPanel: React.FC<EffectivePermissionsPanelProps> = ({
    connectionPrivileges,
    adminPermissions,
    mcpPrivileges,
    isSuperuser,
    connections,
    isDark,
    groups,
}) => {
    const theme = useTheme();

    const chipSx = {
        borderColor: alpha(theme.palette.primary.main, 0.3),
        color: 'text.primary',
        fontSize: '0.75rem',
        height: 26,
        '& .MuiChip-label': { px: 1 },
    };

    // Normalize connectionPrivileges to array format
    let connArray = [];
    if (connectionPrivileges) {
        if (Array.isArray(connectionPrivileges)) {
            connArray = connectionPrivileges;
        } else if (typeof connectionPrivileges === 'object') {
            connArray = Object.entries(connectionPrivileges).map(
                ([connection_id, access_level]) => ({ connection_id, access_level })
            );
        }
    }

    const getConnectionName = (id) => {
        if (id === 0 || id === '0' || String(id) === '0') return 'All Connections';
        if (connections) {
            const conn = connections.find((c) => String(c.id) === String(id));
            if (conn) return conn.name;
        }
        return `Connection ${id}`;
    };

    return (
        <Box>
            {groups && groups.length > 0 && (
                <Box sx={{ mb: 2, display: 'flex', alignItems: 'center', gap: 0.5, flexWrap: 'wrap' }}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mr: 0.5 }}>
                        <GroupIcon sx={{ fontSize: 16, color: 'text.secondary' }} />
                        <Typography sx={categoryLabelSx}>
                            Groups:
                        </Typography>
                    </Box>
                    {groups.map((g) => (
                        <Chip key={g} label={g} variant="outlined" size="small" sx={chipSx} />
                    ))}
                </Box>
            )}

            <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 2 }}>
                <CategoryCard icon={ConnectionIcon} label="Connections">
                    {connArray.length > 0 ? (
                        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                            {connArray.map((cp, i) => (
                                <Chip
                                    key={i}
                                    label={`${getConnectionName(cp.connection_id)} (${cp.access_level})`}
                                    variant="outlined"
                                    size="small"
                                    sx={chipSx}
                                />
                            ))}
                        </Box>
                    ) : (
                        <EmptyState />
                    )}
                </CategoryCard>

                {isSuperuser && (
                    <CategoryCard icon={AdminIcon} label="Admin">
                        {adminPermissions && adminPermissions.length > 0 ? (
                            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                                {adminPermissions.map((perm) => (
                                    <Chip
                                        key={perm}
                                        label={ADMIN_PERMISSION_LABELS[perm] || perm}
                                        variant="outlined"
                                        size="small"
                                        sx={chipSx}
                                    />
                                ))}
                            </Box>
                        ) : (
                            <EmptyState />
                        )}
                    </CategoryCard>
                )}

                <CategoryCard icon={McpIcon} label="MCP">
                    {mcpPrivileges && mcpPrivileges.length > 0 ? (
                        <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                            {mcpPrivileges.map((priv, i) => {
                                const label = typeof priv === 'string'
                                    ? priv.replace(/_/g, ' ')
                                    : (priv.privilege || priv.name || String(priv)).replace(/_/g, ' ');
                                return (
                                    <Chip
                                        key={i}
                                        label={label}
                                        variant="outlined"
                                        size="small"
                                        sx={chipSx}
                                    />
                                );
                            })}
                        </Box>
                    ) : (
                        <EmptyState />
                    )}
                </CategoryCard>
            </Box>
        </Box>
    );
};

export default EffectivePermissionsPanel;
