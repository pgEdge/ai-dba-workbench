/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Help Panel
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React from 'react';
import {
    Drawer,
    Box,
    Typography,
    IconButton,
    Divider,
    List,
    ListItem,
    ListItemText,
} from '@mui/material';
import { Close as CloseIcon } from '@mui/icons-material';
import { CLIENT_VERSION } from '../lib/version';

const HelpPanel = ({ open, onClose }) => {
    return (
        <Drawer
            anchor="right"
            open={open}
            onClose={onClose}
            sx={{
                '& .MuiDrawer-paper': {
                    width: { xs: '100%', sm: 500 },
                    p: 3,
                },
            }}
        >
            <Box>
                {/* Header */}
                <Box sx={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    mb: 3
                }}>
                    <Typography variant="h5" component="h2">
                        Help & Documentation
                    </Typography>
                    <IconButton onClick={onClose} aria-label="close help">
                        <CloseIcon />
                    </IconButton>
                </Box>

                <Divider sx={{ mb: 3 }} />

                {/* Getting Started */}
                <Typography variant="h6" gutterBottom>
                    Getting Started
                </Typography>
                <Typography variant="body2" paragraph>
                    The AI DBA Workbench provides AI-powered tools for PostgreSQL database
                    administration, monitoring, and optimization.
                </Typography>

                <Divider sx={{ my: 3 }} />

                {/* Features Overview */}
                <Typography variant="h6" gutterBottom>
                    Features
                </Typography>
                <List dense>
                    <ListItem>
                        <ListItemText
                            primary="Database Monitoring"
                            secondary="Monitor PostgreSQL database health, performance metrics, and resource utilization in real-time."
                        />
                    </ListItem>
                    <ListItem>
                        <ListItemText
                            primary="AI-Powered Analysis"
                            secondary="Get intelligent insights and recommendations for database optimization and troubleshooting."
                        />
                    </ListItem>
                    <ListItem>
                        <ListItemText
                            primary="Query Analysis"
                            secondary="Analyze slow queries, identify bottlenecks, and receive optimization suggestions."
                        />
                    </ListItem>
                    <ListItem>
                        <ListItemText
                            primary="Alerting"
                            secondary="Configure alerts for critical database events and performance thresholds."
                        />
                    </ListItem>
                </List>

                <Divider sx={{ my: 3 }} />

                {/* Settings */}
                <Typography variant="h6" gutterBottom>
                    Settings & Options
                </Typography>
                <List dense>
                    <ListItem>
                        <ListItemText
                            primary="Theme"
                            secondary="Click the sun/moon icon in the header to switch between light and dark mode."
                        />
                    </ListItem>
                    <ListItem>
                        <ListItemText
                            primary="User Account"
                            secondary="Click your avatar in the header to access account options and sign out."
                        />
                    </ListItem>
                </List>

                <Divider sx={{ my: 3 }} />

                {/* Support */}
                <Typography variant="h6" gutterBottom>
                    Support
                </Typography>
                <Typography variant="body2" paragraph>
                    For technical support and documentation, please contact your administrator
                    or visit the pgEdge documentation portal.
                </Typography>

                <Divider sx={{ my: 3 }} />

                {/* Version Info */}
                <Box sx={{ mt: 4 }}>
                    <Typography variant="body2" color="text.secondary">
                        AI DBA Workbench
                    </Typography>
                    <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                        Client: v{CLIENT_VERSION}
                    </Typography>
                    <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                        Copyright &copy; 2025 - 2026, pgEdge, Inc.
                    </Typography>
                </Box>
            </Box>
        </Drawer>
    );
};

export default HelpPanel;
