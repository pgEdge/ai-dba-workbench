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
import { Box, Typography, LinearProgress } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Computer as ComputerIcon } from '@mui/icons-material';
import { Section, KV, UsageBar } from '../components';
import { formatBytes, formatClockSpeed, pct } from '../serverInfoFormatters';
import {
    sxMono,
    getSectionIconSx,
    getKvGridSx,
    getKvLabelSx,
    getProgressBarSx,
} from '../serverInfoStyles';
import type { SystemInfo } from '../serverInfoTypes';

export interface SystemSectionProps {
    system: SystemInfo;
}

/**
 * System and hardware section displaying OS, CPU, memory, and disk info.
 */
const SystemSection: React.FC<SystemSectionProps> = ({ system: sys }) => {
    const theme = useTheme();

    return (
        <Section
            sectionId="system"
            icon={<ComputerIcon sx={getSectionIconSx(theme)} />}
            title="System & Hardware"
        >
            <Box sx={getKvGridSx()}>
                {sys.os_name && (
                    <KV
                        label="Operating System"
                        value={[sys.os_name, sys.os_version].filter(Boolean).join(' ')}
                        mono={false}
                    />
                )}
                {sys.architecture && (
                    <KV label="Architecture" value={sys.architecture} />
                )}
                {sys.hostname && (
                    <KV label="Hostname" value={sys.hostname} />
                )}
                {sys.cpu_model && (
                    <KV
                        label="CPU"
                        value={sys.cpu_model}
                        mono={false}
                        span
                    />
                )}
                {sys.cpu_cores != null && (
                    <KV
                        label="Cores"
                        value={`${sys.cpu_cores} physical${sys.cpu_logical != null ? ` / ${sys.cpu_logical} logical` : ''}`}
                    />
                )}
                {sys.cpu_clock_speed != null && (
                    <KV label="Clock Speed" value={formatClockSpeed(sys.cpu_clock_speed)} />
                )}
                <UsageBar
                    label="Memory"
                    used={sys.memory_used_bytes}
                    total={sys.memory_total_bytes}
                />
                {sys.swap_total_bytes != null && sys.swap_total_bytes > 0 && (
                    <UsageBar
                        label="Swap"
                        used={sys.swap_used_bytes}
                        total={sys.swap_total_bytes}
                    />
                )}
            </Box>

            {/* Disk info */}
            {sys.disks && sys.disks.length > 0 && (
                <Box sx={{ mt: 1.5 }}>
                    <Typography sx={{
                        ...getKvLabelSx(theme),
                        mb: 0.75,
                    }}>
                        Disks
                    </Typography>
                    {sys.disks.map((disk, idx) => {
                        const diskPct = pct(disk.used_bytes, disk.total_bytes);
                        return (
                            <Box key={idx} sx={{ mb: idx < sys.disks!.length - 1 ? 1 : 0 }}>
                                <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 0.25 }}>
                                    <Typography sx={{
                                        fontSize: '1rem',
                                        ...sxMono,
                                        color: 'text.primary',
                                        fontWeight: 500,
                                    }}>
                                        {disk.mount_point}
                                        <Typography component="span" sx={{
                                            fontSize: '0.875rem',
                                            color: 'text.disabled',
                                            ml: 0.75,
                                        }}>
                                            {disk.filesystem_type}
                                        </Typography>
                                    </Typography>
                                    <Typography sx={{
                                        fontSize: '0.875rem',
                                        color: 'text.disabled',
                                        ...sxMono,
                                    }}>
                                        {formatBytes(disk.used_bytes)} / {formatBytes(disk.total_bytes)}
                                        {diskPct != null && ` (${diskPct}%)`}
                                    </Typography>
                                </Box>
                                {diskPct != null && (
                                    <LinearProgress
                                        variant="determinate"
                                        value={diskPct}
                                        sx={getProgressBarSx(theme, diskPct)}
                                    />
                                )}
                            </Box>
                        );
                    })}
                </Box>
            )}
        </Section>
    );
};

export default SystemSection;
