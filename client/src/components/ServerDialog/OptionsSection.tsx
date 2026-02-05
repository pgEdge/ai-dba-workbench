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
import {
    Box,
    Typography,
    FormControlLabel,
    Checkbox,
} from '@mui/material';
import { ServerFormData, FieldChangeHandler } from './ServerDialog.types';
import {
    optionsSectionLabelSx,
    checkboxSx,
    formControlLabelSx,
} from './ServerDialog.styles';

interface OptionsSectionProps {
    formData: ServerFormData;
    isSaving: boolean;
    isSuperuser: boolean;
    onFieldChange: FieldChangeHandler;
}

/**
 * OptionsSection renders the monitoring and sharing options checkboxes.
 */
const OptionsSection: React.FC<OptionsSectionProps> = ({
    formData,
    isSaving,
    isSuperuser,
    onFieldChange,
}) => {
    return (
        <>
            <Typography variant="subtitle2" sx={optionsSectionLabelSx}>
                Options
            </Typography>

            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                <FormControlLabel
                    control={
                        <Checkbox
                            checked={formData.is_monitored}
                            onChange={(e) => onFieldChange('is_monitored', e.target.checked)}
                            disabled={isSaving}
                            sx={checkboxSx}
                        />
                    }
                    label="Monitor this server"
                    sx={formControlLabelSx}
                />

                {isSuperuser && (
                    <FormControlLabel
                        control={
                            <Checkbox
                                checked={formData.is_shared}
                                onChange={(e) => onFieldChange('is_shared', e.target.checked)}
                                disabled={isSaving}
                                sx={checkboxSx}
                            />
                        }
                        label="Share with all users"
                        sx={formControlLabelSx}
                    />
                )}
            </Box>
        </>
    );
};

export default OptionsSection;
