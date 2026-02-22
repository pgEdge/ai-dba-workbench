/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ConnectionSelectorCodeBlock component. Wraps
 * a RunnableCodeBlock with a dropdown that lets the user choose which
 * cluster node to run the SQL against.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useCallback } from 'react';
import {
    Box,
    Select,
    MenuItem,
    FormControl,
} from '@mui/material';
import { Theme } from '@mui/material/styles';
import type { SelectChangeEvent } from '@mui/material';
import RunnableCodeBlock from './RunnableCodeBlock';
import { sxMonoFont } from './markdownStyles';

interface ConnectionSelectorCodeBlockProps {
    codeContent: string;
    language: string;
    isDark: boolean;
    connectionMap: Map<number, string>;
    databaseName?: string;
    syntaxTheme: Record<string, unknown>;
    customBackground: string;
    theme: Theme;
    props: Record<string, unknown>;
}

const ConnectionSelectorCodeBlock: React.FC<ConnectionSelectorCodeBlockProps> = ({
    codeContent,
    language,
    isDark,
    connectionMap,
    databaseName,
    syntaxTheme,
    customBackground,
    theme,
    props,
}) => {
    const entries = Array.from(connectionMap.entries());
    const [selectedId, setSelectedId] = useState<number>(entries[0]?.[0] ?? 0);

    const handleChange = useCallback((event: SelectChangeEvent<number>) => {
        setSelectedId(event.target.value as number);
    }, []);

    const selectedName = connectionMap.get(selectedId) || '';

    return (
        <Box>
            <FormControl size="small" sx={{ mb: 0.5, minWidth: 200 }}>
                <Select
                    value={selectedId}
                    onChange={handleChange}
                    sx={{
                        fontSize: '0.875rem',
                        ...sxMonoFont,
                        '& .MuiSelect-select': { py: 0.5, px: 1 },
                    }}
                >
                    {entries.map(([id, name]) => (
                        <MenuItem key={id} value={id} sx={{ fontSize: '0.875rem', ...sxMonoFont }}>
                            {name} (ID: {id})
                        </MenuItem>
                    ))}
                </Select>
            </FormControl>
            <RunnableCodeBlock
                codeContent={codeContent}
                language={language}
                isDark={isDark}
                connectionId={selectedId}
                databaseName={databaseName}
                serverName={selectedName}
                syntaxTheme={syntaxTheme}
                customBackground={customBackground}
                theme={theme}
                isSql={true}
                props={props}
            />
        </Box>
    );
};

export default ConnectionSelectorCodeBlock;
