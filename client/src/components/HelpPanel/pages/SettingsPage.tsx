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
import { Palette as ThemeIcon } from '@mui/icons-material';
import { SectionTitle, HelpTip } from '../components';
import { styles } from '../helpPanelStyles';

/**
 * Settings Page - Theme and user settings help
 */
const SettingsPage: React.FC = () => (
    <Box>
        <Typography variant="h5" sx={styles.pageHeading}>
            Settings & Preferences
        </Typography>
        <Typography sx={styles.bodyTextMb3}>
            Customize your AI DBA Workbench experience with the available
            settings and preferences.
        </Typography>

        <SectionTitle icon={ThemeIcon}>Theme</SectionTitle>
        <Typography sx={styles.bodyText}>
            Click the sun/moon icon in the header to toggle between light and
            dark mode. Your preference is saved automatically and persists
            across sessions.
        </Typography>

        <SectionTitle>User Account</SectionTitle>
        <Typography sx={styles.bodyText}>
            Click your avatar in the header to access the user menu. From here
            you can see your username and sign out of the workbench.
        </Typography>

        <SectionTitle>Navigator State</SectionTitle>
        <Typography sx={styles.bodyText}>
            The Cluster Navigator remembers which groups and clusters are
            expanded or collapsed, as well as your current selection. This
            state is preserved when you return to the workbench.
        </Typography>

        <HelpTip>
            Your theme preference and navigator state are stored in your
            browser's local storage.
        </HelpTip>
    </Box>
);

export default SettingsPage;
