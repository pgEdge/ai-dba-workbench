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
import { Box, Typography, Skeleton, alpha } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
    Memory as MemoryIcon,
    AutoAwesome as SparkleIcon,
} from '@mui/icons-material';
import { Section } from '../components';
import { formatBytes } from '../serverInfoFormatters';
import {
    sxMono,
    getSectionIconSx,
    getDbRowSx,
    getExtChipSx,
    getAiBoxSx,
} from '../serverInfoStyles';
import type { DatabaseInfoItem, ExtensionInfoItem, AIAnalysisInfo } from '../serverInfoTypes';

export interface DatabasesSectionProps {
    databases: DatabaseInfoItem[];
    extsByDb: Record<string, ExtensionInfoItem[]> | null;
    aiAnalysis: AIAnalysisInfo | null;
    aiLoading: boolean;
}

/**
 * Databases section with extensions and AI analysis.
 */
const DatabasesSection: React.FC<DatabasesSectionProps> = ({
    databases: dbs,
    extsByDb,
    aiAnalysis: ai,
    aiLoading,
}) => {
    const theme = useTheme();

    return (
        <Section
            sectionId="databases"
            icon={<MemoryIcon sx={getSectionIconSx(theme)} />}
            title="Databases"
            badge={`${dbs.length}`}
        >
            {dbs.map((db) => (
                <Box key={db.name} sx={getDbRowSx(theme)}>
                    <Box sx={{ flex: 1, minWidth: 0 }}>
                        <Box sx={{
                            display: 'flex',
                            alignItems: 'baseline',
                            gap: 1,
                            mb: 0.25,
                        }}>
                            <Typography sx={{
                                fontSize: '1rem',
                                fontWeight: 600,
                                color: 'text.primary',
                                ...sxMono,
                            }}>
                                {db.name}
                            </Typography>
                            {db.encoding && (
                                <Typography sx={{
                                    fontSize: '0.875rem',
                                    color: 'text.disabled',
                                }}>
                                    {db.encoding}
                                </Typography>
                            )}
                        </Box>
                        <Box sx={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: 1.5,
                            mb: 0.5,
                        }}>
                            <Typography sx={{
                                fontSize: '0.875rem',
                                color: 'text.secondary',
                                ...sxMono,
                            }}>
                                {formatBytes(db.size_bytes)}
                            </Typography>
                            {db.connection_limit != null && db.connection_limit >= 0 && (
                                <Typography sx={{
                                    fontSize: '0.875rem',
                                    color: 'text.disabled',
                                }}>
                                    limit: {db.connection_limit}
                                </Typography>
                            )}
                        </Box>
                        {/* Extensions inline */}
                        {extsByDb?.[db.name] && extsByDb[db.name].length > 0 && (
                            <Box sx={{
                                display: 'flex',
                                flexWrap: 'wrap',
                                gap: 0.5,
                                mb: 0.5,
                            }}>
                                {extsByDb[db.name].map((ext) => (
                                    <Box key={`${db.name}-${ext.name}`} sx={getExtChipSx(theme)}>
                                        <Typography component="span" sx={{
                                            fontSize: '1rem',
                                            color: 'text.primary',
                                            fontWeight: 500,
                                            ...sxMono,
                                        }}>
                                            {ext.name}
                                        </Typography>
                                        {ext.version && (
                                            <Typography component="span" sx={{
                                                fontSize: '0.875rem',
                                                color: 'text.disabled',
                                                ...sxMono,
                                            }}>
                                                {ext.version}
                                            </Typography>
                                        )}
                                    </Box>
                                ))}
                            </Box>
                        )}
                        {/* AI Analysis for this database */}
                        {ai?.databases?.[db.name] ? (
                            <Box sx={getAiBoxSx(theme)}>
                                <Box sx={{
                                    display: 'flex',
                                    alignItems: 'flex-start',
                                    gap: 0.5,
                                }}>
                                    <SparkleIcon sx={{
                                        fontSize: 12,
                                        color: 'primary.main',
                                        mt: 0.125,
                                        flexShrink: 0,
                                    }} />
                                    <Typography sx={{
                                        fontSize: '1rem',
                                        color: 'text.secondary',
                                        lineHeight: 1.5,
                                    }}>
                                        {ai.databases[db.name]}
                                    </Typography>
                                </Box>
                            </Box>
                        ) : aiLoading ? (
                            <Box sx={getAiBoxSx(theme)}>
                                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                                    <SparkleIcon sx={{ fontSize: 12, color: 'primary.main', flexShrink: 0 }} />
                                    <Skeleton
                                        variant="text"
                                        width="60%"
                                        sx={{ bgcolor: alpha(theme.palette.grey[500], 0.1) }}
                                    />
                                </Box>
                            </Box>
                        ) : null}
                    </Box>
                </Box>
            ))}
        </Section>
    );
};

export default DatabasesSection;
