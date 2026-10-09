/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - ConnectionSelectorCodeBlock component. Wraps
 * a RunnableCodeBlock with a dropdown that lets the user choose which
 * server (and optionally database) to run the SQL against.
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import { useState, useCallback, useMemo } from 'react';
import {
    Box,
    Select,
    MenuItem,
    FormControl,
} from '@mui/material';
import type { Theme } from '@mui/material/styles';
import type { SelectChangeEvent } from '@mui/material';
import RunnableCodeBlock from './RunnableCodeBlock';
import { sxMonoFont } from './markdownStyles';

/**
 * One server/database pair the user may choose to run a query on.
 */
export interface RunTargetOption {
    connectionId: number;
    serverName: string;
    /** Database to run on; omitted means the connection's default. */
    databaseName?: string;
}

interface ConnectionSelectorCodeBlockProps {
    codeContent: string;
    language: string;
    isDark: boolean;
    /** Connection ID to name map; used when `options` is not given. */
    connectionMap?: Map<number, string>;
    /** Explicit server/database choices; takes precedence over the map. */
    options?: RunTargetOption[];
    /** Database applied to map entries and options that name none. */
    databaseName?: string;
    /**
     * When true, nothing is selected initially and the Run button is
     * hidden until the user picks a target, so no target is assumed.
     */
    requireSelection?: boolean;
    syntaxTheme: Record<string, unknown>;
    customBackground: string;
    theme: Theme;
    props: Record<string, unknown>;
}

const NO_SELECTION = '';

const sxSelect = {
    fontSize: '0.875rem',
    ...sxMonoFont,
    '& .MuiSelect-select': { py: 0.5, px: 1 },
};

const sxMenuItem = { fontSize: '0.875rem', ...sxMonoFont };

/**
 * Stable identity of an option: its connection and database.
 */
const optionKey = (option: RunTargetOption): string =>
    `${option.connectionId}/${option.databaseName ?? ''}`;

/**
 * Format the dropdown label for a target option.
 */
const formatOptionLabel = (option: RunTargetOption): string => {
    const name = option.databaseName
        ? `${option.serverName}/${option.databaseName}`
        : option.serverName;
    return `${name} (ID: ${option.connectionId})`;
};

const ConnectionSelectorCodeBlock: React.FC<ConnectionSelectorCodeBlockProps> = ({
    codeContent,
    language,
    isDark,
    connectionMap,
    options,
    databaseName,
    requireSelection = false,
    syntaxTheme,
    customBackground,
    theme,
    props,
}) => {
    const targets = useMemo<RunTargetOption[]>(() => {
        if (options) {
            return options;
        }
        return Array.from(connectionMap?.entries() ?? []).map(
            ([connectionId, serverName]) => ({ connectionId, serverName }),
        );
    }, [options, connectionMap]);

    // The selection is held as the option's key rather than its index,
    // so a refreshed server list can never silently re-point it at a
    // different server. Until the user chooses, the first option is used
    // unless a selection is required.
    const [chosenKey, setChosenKey] = useState<string | null>(null);

    const handleChange = useCallback((event: SelectChangeEvent) => {
        setChosenKey(event.target.value);
    }, []);

    const selectedTarget = chosenKey !== null
        ? targets.find((target) => optionKey(target) === chosenKey)
        : requireSelection ? undefined : targets[0];
    const selected = selectedTarget ? optionKey(selectedTarget) : NO_SELECTION;
    const hasSelection = selectedTarget !== undefined;

    return (
        <Box>
            <FormControl size="small" sx={{ mb: 0.5, minWidth: 200 }}>
                <Select
                    value={selected}
                    onChange={handleChange}
                    displayEmpty
                    inputProps={{
                        'aria-label': 'Server to run this query on',
                    }}
                    renderValue={() => (selectedTarget
                        ? formatOptionLabel(selectedTarget)
                        : 'Choose a server to run on')}
                    sx={sxSelect}
                >
                    {targets.map((target) => (
                        <MenuItem
                            key={optionKey(target)}
                            value={optionKey(target)}
                            sx={sxMenuItem}
                        >
                            {formatOptionLabel(target)}
                        </MenuItem>
                    ))}
                </Select>
            </FormControl>
            <RunnableCodeBlock
                // Remount when the target changes so results from one
                // server are never shown under another server's label.
                key={selected}
                codeContent={codeContent}
                language={language}
                isDark={isDark}
                connectionId={selectedTarget?.connectionId ?? 0}
                databaseName={selectedTarget?.databaseName ?? databaseName}
                serverName={selectedTarget?.serverName ?? ''}
                syntaxTheme={syntaxTheme}
                customBackground={customBackground}
                theme={theme}
                isSql={hasSelection}
                props={props}
            />
        </Box>
    );
};

export default ConnectionSelectorCodeBlock;
