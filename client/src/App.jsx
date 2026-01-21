/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState, useEffect, useMemo } from 'react';
import { ThemeProvider } from '@mui/material/styles';
import { Box, CircularProgress, CssBaseline, Typography } from '@mui/material';
import { AuthProvider, useAuth } from './contexts/AuthContext';
import { ClusterProvider, useCluster } from './contexts/ClusterContext';
import Header from './components/Header';
import Login from './components/Login';
import ClusterNavigator from './components/ClusterNavigator';
import { createPgedgeTheme, loginTheme } from './theme/pgedgeTheme';

const AppContent = () => {
    const [mode, setMode] = useState(() => {
        // Load theme preference from localStorage
        const savedMode = localStorage.getItem('theme-mode');
        return savedMode || 'light';
    });
    const { user, loading } = useAuth();

    // Save theme preference to localStorage when it changes
    useEffect(() => {
        localStorage.setItem('theme-mode', mode);
    }, [mode]);

    // Create theme using pgEdge theme configuration
    const theme = useMemo(() => createPgedgeTheme(mode), [mode]);

    const toggleTheme = () => {
        setMode((prevMode) => (prevMode === 'light' ? 'dark' : 'light'));
    };

    if (loading) {
        return (
            <ThemeProvider theme={theme}>
                <CssBaseline />
                <Box
                    sx={{
                        minHeight: '100vh',
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        bgcolor: 'background.default',
                    }}
                >
                    <CircularProgress />
                </Box>
            </ThemeProvider>
        );
    }

    if (!user) {
        return (
            <ThemeProvider theme={loginTheme}>
                <CssBaseline />
                <Login />
            </ThemeProvider>
        );
    }

    return (
        <ThemeProvider theme={theme}>
            <CssBaseline />
            <ClusterProvider>
                <MainLayout mode={mode} onToggleTheme={toggleTheme} />
            </ClusterProvider>
        </ThemeProvider>
    );
};

const MainLayout = ({ mode, onToggleTheme }) => {
    const {
        clusterData,
        selectedServer,
        loading,
        fetchClusterData,
        selectServer,
    } = useCluster();

    return (
        <Box sx={{
            height: '100vh',
            display: 'flex',
            flexDirection: 'column',
            bgcolor: 'background.default',
            overflow: 'hidden'
        }}>
            <Header
                onToggleTheme={onToggleTheme}
                mode={mode}
            />
            <Box sx={{
                flex: 1,
                display: 'flex',
                overflow: 'hidden',
                position: 'relative'
            }}>
                {/* Cluster Navigator */}
                <ClusterNavigator
                    data={clusterData}
                    selectedServerId={selectedServer?.id}
                    onSelectServer={selectServer}
                    onRefresh={fetchClusterData}
                    loading={loading}
                    mode={mode}
                />

                {/* Main content area */}
                <Box sx={{
                    flex: 1,
                    display: 'flex',
                    flexDirection: 'column',
                    overflow: 'hidden',
                }}>
                    {selectedServer ? (
                        <Box sx={{
                            flex: 1,
                            p: 3,
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'center',
                            justifyContent: 'center',
                        }}>
                            <Typography variant="h6" color="text.secondary">
                                Connected to: {selectedServer.name}
                            </Typography>
                            <Typography variant="body2" color="text.disabled" sx={{ mt: 1 }}>
                                {selectedServer.host}:{selectedServer.port}
                            </Typography>
                        </Box>
                    ) : (
                        <Box sx={{
                            flex: 1,
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'center',
                            justifyContent: 'center',
                        }}>
                            <Typography variant="h6" color="text.secondary">
                                Select a server to get started
                            </Typography>
                            <Typography variant="body2" color="text.disabled" sx={{ mt: 1 }}>
                                Choose a database server from the navigation panel
                            </Typography>
                        </Box>
                    )}
                </Box>
            </Box>
        </Box>
    );
};

const App = () => {
    return (
        <AuthProvider>
            <AppContent />
        </AuthProvider>
    );
};

export default App;
