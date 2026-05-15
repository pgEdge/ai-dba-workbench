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
import { useState, useEffect, useMemo } from 'react';
import { ThemeProvider } from '@mui/material/styles';
import { Box, CircularProgress, CssBaseline, type PaletteMode } from '@mui/material';
import { AuthProvider } from './contexts/AuthContext';
import { useAuth } from './contexts/useAuth';
import { ClusterProvider } from './contexts/ClusterContext';
import { useCluster } from './contexts/useCluster';
import { AlertsProvider } from './contexts/AlertsContext';
import { DashboardProvider } from './contexts/DashboardContext';
import { BlackoutProvider } from './contexts/BlackoutContext';
import { ChatProvider } from './contexts/ChatContext';
import { useChatContext } from './contexts/useChatContext';
import { ConnectionStatusProvider } from './contexts/ConnectionStatusContext';
import { AICapabilitiesProvider } from './contexts/AICapabilitiesContext';
import { useAICapabilities } from './contexts/useAICapabilities';
import ConnectionLostOverlay from './components/ConnectionLostOverlay';
import ErrorBoundary from './components/ErrorBoundary';
import Header from './components/Header';
import Login from './components/Login';
import ClusterNavigator from './components/ClusterNavigator';
import StatusPanel from './components/StatusPanel';
import ChatPanel from './components/ChatPanel';
import ChatFAB from './components/ChatPanel/ChatFAB';
import { createPgedgeTheme, loginTheme } from './theme/pgedgeTheme';
import { buildSelection } from './utils/buildSelection';
import type { Selection } from './types/selection';

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
    const [mode, setMode] = useState<PaletteMode>(() => {
        // Load theme preference from localStorage
        const savedMode = localStorage.getItem('theme-mode');
        if (savedMode === 'light' || savedMode === 'dark') {
            return savedMode;
        }
        return 'light';
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
                    <CircularProgress aria-label="Loading application" />
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
            <ConnectionStatusProvider>
                <ConnectionLostOverlay />
                <AICapabilitiesProvider>
                <ClusterProvider>
                    <DashboardProvider>
                        <AlertsProvider>
                            <ChatProvider>
                                <ErrorBoundary>
                                    <MainLayout onToggleTheme={toggleTheme} />
                                </ErrorBoundary>
                            </ChatProvider>
                        </AlertsProvider>
                    </DashboardProvider>
                </ClusterProvider>
                </AICapabilitiesProvider>
            </ConnectionStatusProvider>
        </ThemeProvider>
    );
};

interface MainLayoutProps {
    onToggleTheme: () => void;
}

const MainLayout: React.FC<MainLayoutProps> = ({ onToggleTheme }) => {
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

    // AI capabilities
    const { aiEnabled } = useAICapabilities();

    // Chat panel state from context
    const {
        isOpen: chatOpen,
        toggleChat: handleToggleChat,
        closeChat: handleCloseChat,
    } = useChatContext();

    // Determine help context based on current selection
    const helpContext = useMemo(() => {
        if (selectionType === 'server') {return 'server';}
        if (selectionType === 'cluster') {return 'cluster';}
        if (selectionType === 'estate') {return 'navigator';}
        return null;
    }, [selectionType]);

    // Build selection object based on current selection type.
    // Parent IDs are resolved from clusterData for blackout scope. #33
    // The actual logic lives in buildSelection so it can be unit
    // tested without rendering the full layout; the helper is also
    // defensive about a group's clusters field being null (the server
    // can emit `null` for an empty group, which would otherwise crash
    // the layout into the ErrorBoundary, see issue #242).
    const selection = useMemo<Selection | null>(
        () => buildSelection(
            selectionType,
            selectedServer,
            selectedCluster,
            clusterData,
        ),
        [selectionType, selectedServer, selectedCluster, clusterData],
    );

    return (
        <BlackoutProvider selection={selection}>
            <Box sx={styles.mainLayoutRoot}>
                <Header
                    onToggleTheme={onToggleTheme}
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
                    />

                    {/* Main content area */}
                    <Box
                        sx={styles.contentArea}
                    >
                        <StatusPanel
                            selection={selection}
                        />
                    </Box>

                    {/* AI Chat Panel */}
                    {aiEnabled && (
                        <ChatPanel
                            open={chatOpen}
                            onClose={handleCloseChat}
                        />
                    )}
                </Box>

                {/* AI Chat FAB (hidden when panel is open) */}
                {!chatOpen && aiEnabled && (
                    <ChatFAB
                        onClick={handleToggleChat}
                        isOpen={false}
                    />
                )}
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
