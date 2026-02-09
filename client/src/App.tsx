/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import React, { useState, useEffect, useMemo } from 'react';
import { ThemeProvider } from '@mui/material/styles';
import { Box, CircularProgress, CssBaseline } from '@mui/material';
import { AuthProvider, useAuth } from './contexts/AuthContext';
import { ClusterProvider, useCluster } from './contexts/ClusterContext';
import { AlertsProvider } from './contexts/AlertsContext';
import { BlackoutProvider } from './contexts/BlackoutContext';
import ErrorBoundary from './components/ErrorBoundary';
import Header from './components/Header';
import Login from './components/Login';
import ClusterNavigator from './components/ClusterNavigator';
import StatusPanel from './components/StatusPanel';
import { createPgedgeTheme, loginTheme } from './theme/pgedgeTheme';

// Style constants
const styles = {
    loadingContainer: {
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        bgcolor: 'background.default',
    },
    mainLayoutRoot: {
        height: '100vh',
        display: 'flex',
        flexDirection: 'column',
        bgcolor: 'background.default',
        overflow: 'hidden',
    },
    mainLayoutBody: {
        flex: 1,
        display: 'flex',
        overflow: 'hidden',
        position: 'relative',
    },
    contentArea: {
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
    },
};

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
                <Box sx={styles.loadingContainer}>
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
                <AlertsProvider>
                    <ErrorBoundary>
                        <MainLayout mode={mode} onToggleTheme={toggleTheme} />
                    </ErrorBoundary>
                </AlertsProvider>
            </ClusterProvider>
        </ThemeProvider>
    );
};

interface MainLayoutProps {
    mode: string;
    onToggleTheme: () => void;
}

const MainLayout: React.FC<MainLayoutProps> = ({ mode, onToggleTheme }) => {
    const {
        clusterData,
        selectedServer,
        selectedCluster,
        selectionType,
        loading,
        fetchClusterData,
        selectServer,
        selectCluster,
        selectEstate,
    } = useCluster();

    // Determine help context based on current selection
    const helpContext = useMemo(() => {
        if (selectionType === 'server') {return 'server';}
        if (selectionType === 'cluster') {return 'cluster';}
        if (selectionType === 'estate') {return 'navigator';}
        return null;
    }, [selectionType]);

    // Build selection object based on current selection type
    const selection = useMemo(() => {
        if (selectionType === 'server' && selectedServer) {
            return {
                type: 'server',
                id: selectedServer.id,
                name: selectedServer.name,
                status: selectedServer.status || 'unknown',
                host: selectedServer.host,
                port: selectedServer.port,
                role: selectedServer.role,
                version: selectedServer.version,
                // Extended server info (may not be available yet)
                database: selectedServer.database_name || selectedServer.database,
                username: selectedServer.username,
                os: selectedServer.os,
                platform: selectedServer.platform,
                spockNodeName: selectedServer.spock_node_name,
                spockVersion: selectedServer.spock_version,
            };
        }

        if (selectionType === 'cluster' && selectedCluster) {
            // Collect all servers in the cluster (including nested children)
            const collectServers = (servers: Array<{ id: number; name: string; status?: string; children?: typeof servers; [key: string]: unknown }>): typeof servers => {
                const result: typeof servers = [];
                servers?.forEach(s => {
                    result.push(s);
                    if (s.children) {
                        result.push(...collectServers(s.children));
                    }
                });
                return result;
            };
            const servers = collectServers(selectedCluster.servers);
            const serverIds = servers.map(s => s.id);

            return {
                type: 'cluster',
                id: selectedCluster.id,
                name: selectedCluster.name,
                servers: servers,
                serverIds: serverIds,
                status: servers.every(s => s.status === 'offline') && servers.length > 0
                    ? 'offline'
                    : servers.some(s => s.status === 'offline' || s.status === 'warning')
                        ? 'warning'
                        : 'online',
            };
        }

        if (selectionType === 'estate') {
            return {
                type: 'estate',
                name: 'All Servers',
                groups: clusterData,
                status: 'online', // Will be calculated by StatusPanel
            };
        }

        return null;
    }, [selectionType, selectedServer, selectedCluster, clusterData]);

    return (
        <BlackoutProvider selection={selection}>
            <Box sx={styles.mainLayoutRoot}>
                <Header
                    onToggleTheme={onToggleTheme}
                    mode={mode}
                    helpContext={helpContext}
                />
                <Box sx={styles.mainLayoutBody}>
                    {/* Cluster Navigator */}
                    <ClusterNavigator
                        data={clusterData}
                        selectedServerId={selectedServer?.id}
                        selectedClusterId={selectedCluster?.id}
                        selectionType={selectionType}
                        onSelectServer={selectServer}
                        onSelectCluster={selectCluster}
                        onSelectEstate={selectEstate}
                        onRefresh={fetchClusterData}
                        loading={loading}
                        mode={mode}
                    />

                    {/* Main content area */}
                    <Box sx={styles.contentArea}>
                        <StatusPanel
                            selection={selection}
                            mode={mode}
                        />
                    </Box>
                </Box>
            </Box>
        </BlackoutProvider>
    );
};

const App = () => {
    return (
        <ErrorBoundary>
            <AuthProvider>
                <AppContent />
            </AuthProvider>
        </ErrorBoundary>
    );
};

export default App;
