/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { Component } from 'react';
import { Box, Typography, Button, Paper, Container } from '@mui/material';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';
import RefreshIcon from '@mui/icons-material/Refresh';

/**
 * ErrorBoundary catches JavaScript errors in its child component tree,
 * logs those errors, and displays a fallback UI instead of crashing.
 *
 * Usage:
 *   <ErrorBoundary>
 *     <YourComponent />
 *   </ErrorBoundary>
 *
 * Or with a custom fallback:
 *   <ErrorBoundary fallback={<CustomErrorUI />}>
 *     <YourComponent />
 *   </ErrorBoundary>
 */
class ErrorBoundary extends Component {
    constructor(props) {
        super(props);
        this.state = {
            hasError: false,
            error: null,
            errorInfo: null,
        };
    }

    static getDerivedStateFromError(error) {
        // Update state so the next render shows the fallback UI
        return { hasError: true, error };
    }

    componentDidCatch(error, errorInfo) {
        // Log the error to the console for debugging
        console.error('ErrorBoundary caught an error:', error);
        console.error('Error info:', errorInfo);

        // Store error info for display
        this.setState({ errorInfo });

        // Call optional onError callback if provided
        if (this.props.onError) {
            this.props.onError(error, errorInfo);
        }
    }

    handleReset = () => {
        this.setState({
            hasError: false,
            error: null,
            errorInfo: null,
        });
    };

    handleReload = () => {
        window.location.reload();
    };

    render() {
        if (this.state.hasError) {
            // If a custom fallback is provided, use it
            if (this.props.fallback) {
                return this.props.fallback;
            }

            // Default fallback UI
            return (
                <Container maxWidth="sm">
                    <Box
                        sx={{
                            minHeight: '100vh',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                        }}
                    >
                        <Paper
                            elevation={3}
                            sx={{
                                p: 4,
                                textAlign: 'center',
                                maxWidth: 500,
                            }}
                        >
                            <ErrorOutlineIcon
                                sx={{
                                    fontSize: 64,
                                    color: 'error.main',
                                    mb: 2,
                                }}
                            />
                            <Typography
                                variant="h5"
                                component="h1"
                                gutterBottom
                                color="error"
                            >
                                Something went wrong
                            </Typography>
                            <Typography
                                variant="body1"
                                color="text.secondary"
                                sx={{ mb: 3 }}
                            >
                                An unexpected error occurred. You can try
                                refreshing the page or contact support if the
                                problem persists.
                            </Typography>

                            {/* Show error details in development */}
                            {process.env.NODE_ENV === 'development' &&
                                this.state.error && (
                                    <Paper
                                        variant="outlined"
                                        sx={{
                                            p: 2,
                                            mb: 3,
                                            bgcolor: 'grey.100',
                                            textAlign: 'left',
                                            overflow: 'auto',
                                            maxHeight: 200,
                                        }}
                                    >
                                        <Typography
                                            variant="caption"
                                            component="pre"
                                            sx={{
                                                fontFamily: 'monospace',
                                                whiteSpace: 'pre-wrap',
                                                wordBreak: 'break-word',
                                                m: 0,
                                            }}
                                        >
                                            {this.state.error.toString()}
                                            {this.state.errorInfo?.componentStack}
                                        </Typography>
                                    </Paper>
                                )}

                            <Box
                                sx={{
                                    display: 'flex',
                                    gap: 2,
                                    justifyContent: 'center',
                                }}
                            >
                                <Button
                                    variant="outlined"
                                    startIcon={<RefreshIcon />}
                                    onClick={this.handleReset}
                                >
                                    Try Again
                                </Button>
                                <Button
                                    variant="contained"
                                    startIcon={<RefreshIcon />}
                                    onClick={this.handleReload}
                                >
                                    Reload Page
                                </Button>
                            </Box>
                        </Paper>
                    </Box>
                </Container>
            );
        }

        return this.props.children;
    }
}

export default ErrorBoundary;
