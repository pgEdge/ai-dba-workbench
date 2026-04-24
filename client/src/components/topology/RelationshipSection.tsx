/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type React from 'react';
import {
    Box,
    Typography,
    Button,
    Alert,
    TextField,
    MenuItem,
    IconButton,
    Divider,
    List,
    ListItem,
    ListItemText,
    ListItemSecondaryAction,
    Chip,
} from '@mui/material';
import {
    Add as AddIcon,
    Delete as DeleteIcon,
} from '@mui/icons-material';
import { SELECT_FIELD_DEFAULT_BG_SX } from '../shared/formStyles';
import type { NodeRelationship, ClusterServerInfo } from '../ServerDialog/ServerDialog.types';
import { getRelationshipLabel } from './topologyHelpers';

export interface RelationshipSectionProps {
    relationships: NodeRelationship[];
    clusterServers: ClusterServerInfo[];
    selectedSourceId: number | '';
    selectedTargetId: number | '';
    selectedRelType: string;
    relationshipError: string | null;
    onSourceChange: (sourceId: number | '') => void;
    onTargetChange: (targetId: number | '') => void;
    onRelTypeChange: (relType: string) => void;
    onAddRelationship: () => void;
    onDeleteRelationship: (relationshipId: number) => void;
    onClearError: () => void;
    availableTargets: ClusterServerInfo[];
    allRelationshipsExist: boolean;
}

/**
 * RelationshipSection provides the UI for viewing and managing
 * relationships between cluster servers.
 */
const RelationshipSection: React.FC<RelationshipSectionProps> = ({
    relationships,
    clusterServers,
    selectedSourceId,
    selectedTargetId,
    selectedRelType,
    relationshipError,
    onSourceChange,
    onTargetChange,
    onRelTypeChange,
    onAddRelationship,
    onDeleteRelationship,
    onClearError,
    availableTargets,
    allRelationshipsExist,
}) => {
    return (
        <Box>
            <Typography
                variant="subtitle2"
                sx={{
                    color: 'text.secondary',
                    textTransform: 'uppercase',
                    fontSize: '0.875rem',
                    letterSpacing: '0.05em',
                    mb: 1.5,
                }}
            >
                Relationships
            </Typography>
            <Box
                sx={{
                    p: 2,
                    border: '1px solid',
                    borderColor: 'divider',
                    borderRadius: 1.5,
                    bgcolor: 'background.paper',
                }}
            >
                {relationshipError && (
                    <Alert
                        severity="error"
                        sx={{ mb: 1.5, borderRadius: 1 }}
                        onClose={onClearError}
                    >
                        {relationshipError}
                    </Alert>
                )}

                {/* Existing relationships list */}
                {relationships.length > 0 ? (
                    <List dense disablePadding>
                        {relationships.map((rel) => (
                            <ListItem
                                key={rel.id}
                                disableGutters
                                sx={{ pr: 6 }}
                            >
                                <ListItemText
                                    primary={
                                        <Box
                                            sx={{
                                                display: 'flex',
                                                alignItems: 'center',
                                                gap: 1,
                                            }}
                                        >
                                            <Typography
                                                variant="body2"
                                                component="span"
                                            >
                                                <strong>{rel.source_name}</strong>
                                                {' '}
                                                {getRelationshipLabel(
                                                    rel.relationship_type,
                                                ).toLowerCase()}
                                                {' '}
                                                <strong>{rel.target_name}</strong>
                                            </Typography>
                                            {rel.is_auto_detected && (
                                                <Chip
                                                    label="Auto"
                                                    size="small"
                                                    variant="outlined"
                                                    sx={{
                                                        height: 20,
                                                        fontSize: '0.7rem',
                                                    }}
                                                />
                                            )}
                                        </Box>
                                    }
                                />
                                <ListItemSecondaryAction>
                                    <IconButton
                                        edge="end"
                                        size="small"
                                        onClick={() => onDeleteRelationship(rel.id)}
                                        aria-label={`Remove relationship between ${rel.source_name} and ${rel.target_name}`}
                                        sx={{
                                            color: 'text.disabled',
                                            '&:hover': {
                                                color: 'error.main',
                                            },
                                        }}
                                    >
                                        <DeleteIcon fontSize="small" />
                                    </IconButton>
                                </ListItemSecondaryAction>
                            </ListItem>
                        ))}
                    </List>
                ) : (
                    <Typography
                        variant="body2"
                        sx={{
                            color: 'text.secondary',
                            mb: 1.5,
                        }}
                    >
                        No relationships defined.
                    </Typography>
                )}

                {/* Add relationship controls */}
                <Divider sx={{ my: 1.5 }} />
                {allRelationshipsExist ? (
                    <Typography
                        variant="body2"
                        sx={{
                            fontStyle: 'italic',
                            color: 'text.secondary',
                        }}
                    >
                        All members already have this relationship type.
                    </Typography>
                ) : (
                    <Box
                        sx={{
                            display: 'flex',
                            gap: 1,
                            alignItems: 'center',
                        }}
                    >
                        <TextField
                            select
                            margin="dense"
                            sx={{ flex: 1, ...SELECT_FIELD_DEFAULT_BG_SX }}
                            label="Source"
                            value={selectedSourceId}
                            onChange={(e) => {
                                const val = e.target.value;
                                onSourceChange(val === '' ? '' : Number(val));
                            }}
                            InputLabelProps={{ shrink: true }}
                        >
                            {clusterServers.map((s) => (
                                <MenuItem key={s.id} value={s.id}>
                                    {s.name}
                                </MenuItem>
                            ))}
                        </TextField>
                        <TextField
                            select
                            margin="dense"
                            sx={{ flex: 1, ...SELECT_FIELD_DEFAULT_BG_SX }}
                            label="Target"
                            value={selectedTargetId}
                            onChange={(e) => {
                                const val = e.target.value;
                                onTargetChange(val === '' ? '' : Number(val));
                            }}
                            disabled={
                                selectedSourceId === '' ||
                                availableTargets.length === 0
                            }
                            InputLabelProps={{ shrink: true }}
                        >
                            {availableTargets.map((s) => (
                                <MenuItem key={s.id} value={s.id}>
                                    {s.name}
                                </MenuItem>
                            ))}
                        </TextField>
                        <TextField
                            select
                            margin="dense"
                            sx={{ flex: 1, ...SELECT_FIELD_DEFAULT_BG_SX }}
                            label="Type"
                            value={selectedRelType}
                            onChange={(e) => onRelTypeChange(e.target.value)}
                            InputLabelProps={{ shrink: true }}
                        >
                            <MenuItem value="streams_from">
                                Streams from (physical)
                            </MenuItem>
                            <MenuItem value="subscribes_to">
                                Subscribes to (logical)
                            </MenuItem>
                            <MenuItem value="replicates_with">
                                Replicates with (Spock)
                            </MenuItem>
                        </TextField>
                        <Button
                            variant="outlined"
                            startIcon={<AddIcon />}
                            onClick={onAddRelationship}
                            disabled={
                                selectedSourceId === '' ||
                                selectedTargetId === '' ||
                                !selectedRelType
                            }
                            aria-label="Add relationship"
                            sx={{
                                textTransform: 'none',
                                whiteSpace: 'nowrap',
                                height: 40,
                            }}
                        >
                            Add
                        </Button>
                    </Box>
                )}
            </Box>
        </Box>
    );
};

export default RelationshipSection;
