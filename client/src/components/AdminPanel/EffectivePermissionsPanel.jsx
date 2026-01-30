/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import { Box, Typography, Chip, alpha } from '@mui/material';
import {
    Storage as ConnectionIcon,
    AdminPanelSettings as AdminIcon,
    SmartToy as McpIcon,
    Group as GroupIcon,
} from '@mui/icons-material';

const ACCENT_COLOR = '#15AABF';

const ADMIN_PERMISSION_LABELS = {
    manage_users: 'Manage Users',
    manage_groups: 'Manage Groups',
    manage_permissions: 'Manage Permissions',
    manage_connections: 'Manage Connections',
    manage_token_scopes: 'Manage Token Scopes',
};

const chipSx = {
    borderColor: alpha(ACCENT_COLOR, 0.3),
    color: 'text.primary',
    fontSize: '0.75rem',
    height: 26,
    '& .MuiChip-label': { px: 1 },
};

const CategoryCard = ({ icon: Icon, label, children }) => (
    <Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, mb: 1 }}>
            <Icon sx={{ fontSize: 16, color: 'text.secondary' }} />
            <Typography sx={{ fontSize: '0.7rem', fontWeight: 600, color: 'text.secondary', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
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

const EffectivePermissionsPanel = ({
    connectionPrivileges,
    adminPermissions,
    mcpPrivileges,
    isSuperuser,
    connections,
    isDark,
    groups,
}) => {
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
                        <Typography sx={{ fontSize: '0.7rem', fontWeight: 600, color: 'text.secondary', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
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
