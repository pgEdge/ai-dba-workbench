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
import { Box, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Tune as TuneIcon } from '@mui/icons-material';
import { Section } from '../components';
import { formatPgSetting } from '../../../utils/formatters';
import { sxMono, getSectionIconSx, getSettingRowSx } from '../serverInfoStyles';
import type { SettingInfoItem } from '../serverInfoTypes';

export interface ConfigurationSectionProps {
    settingsByCategory: Record<string, SettingInfoItem[]>;
    totalCount: number;
}

/**
 * Configuration section displaying PostgreSQL settings grouped by category.
 */
const ConfigurationSection: React.FC<ConfigurationSectionProps> = ({
    settingsByCategory,
    totalCount,
}) => {
    const theme = useTheme();

    return (
        <Section
            sectionId="configuration"
            icon={<TuneIcon sx={getSectionIconSx(theme)} />}
            title="Configuration"
            badge={`${totalCount}`}
            defaultOpen={false}
        >
            {Object.entries(settingsByCategory).map(([category, catSettings]) => (
                <Box key={category} sx={{ mb: 1.5, '&:last-child': { mb: 0 } }}>
                    <Typography sx={{
                        fontSize: '0.875rem',
                        fontWeight: 700,
                        textTransform: 'uppercase',
                        letterSpacing: '0.1em',
                        color: theme.palette.grey[500],
                        mb: 0.5,
                    }}>
                        {category}
                    </Typography>
                    {catSettings.map((s) => (
                        <Box key={s.name} sx={getSettingRowSx(theme)}>
                            <Typography sx={{
                                fontSize: '1rem',
                                color: 'text.secondary',
                                ...sxMono,
                                flexShrink: 0,
                            }}>
                                {s.name}
                            </Typography>
                            <Typography sx={{
                                fontSize: '1rem',
                                color: 'text.primary',
                                fontWeight: 500,
                                ...sxMono,
                                textAlign: 'right',
                            }}>
                                {formatPgSetting(s.setting, s.unit)}
                            </Typography>
                        </Box>
                    ))}
                </Box>
            ))}
        </Section>
    );
};

export default ConfigurationSection;
