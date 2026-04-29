/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { Component, type ErrorInfo, type ReactNode } from 'react';
import {
    Alert,
    AlertTitle,
    Box,
    Button,
    Container,
    Paper,
    Typography,
    Collapse,
    IconButton,
} from '@mui/material';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutline';
import RefreshIcon from '@mui/icons-material/Refresh';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import ExpandLessIcon from '@mui/icons-material/ExpandLess';
import { logger } from '../utils/logger';

interface ErrorBoundaryProps {
    children: ReactNode;
    fallback?: ReactNode;
    onError?: (error: Error, errorInfo: ErrorInfo) => void;
}

interface ErrorBoundaryState {
    hasError: boolean;
    error: Error | null;
    errorInfo: ErrorInfo | null;
    detailsOpen: boolean;
}

/*
 * Style constants for the fallback UI.  Centralised so the visual
 * design stays consistent with the rest of the application even when
 * the rest of the React tree has crashed.
 */
const styles = {
    outerContainer: {
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        py: 4,
    },
    errorPaper: {
        p: 4,
        width: '100%',
        maxWidth: 720,
    },
    iconRow: {
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        mb: 1,
    },
    errorIcon: {
        fontSize: 40,
        color: 'error.main',
    },
    errorMessage: {
        mb: 3,
    },
    detailsToggleRow: {
        display: 'flex',
        alignItems: 'center',
        gap: 0.5,
        mb: 1,
    },
    detailsPaper: {
        p: 2,
        mb: 3,
        bgcolor: 'background.default',
        textAlign: 'left',
        overflow: 'auto',
        maxHeight: 320,
    },
    detailsText: {
        fontFamily: 'monospace',
        fontSize: '0.8rem',
        whiteSpace: 'pre-wrap',
        wordBreak: 'break-word',
        m: 0,
    },
    buttonContainer: {
        display: 'flex',
        gap: 2,
        justifyContent: 'flex-end',
    },
};

/**
 * ErrorBoundary catches uncaught render errors anywhere in its child
 * tree, logs them via the project logger, and renders a fallback UI
 * that lets the user recover and report the failure.
 *
 * The fallback is intentionally Docker-friendly: it always exposes the
 * error message and component stack in a collapsible section so users
 * (and bug reporters) can copy the details into an issue regardless of
 * whether the build is development or production.
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
class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
    constructor(props: ErrorBoundaryProps) {
        super(props);
        this.state = {
            hasError: false,
            error: null,
            errorInfo: null,
            detailsOpen: true,
        };
    }

    static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
        // Update state so the next render shows the fallback UI.
        return { hasError: true, error };
    }

    componentDidCatch(error: Error, errorInfo: ErrorInfo): void {
        // Route both records through the centralised logger so the
        // no-console rule is honoured and operators can scrape the same
        // log stream as the rest of the app.
        logger.error('ErrorBoundary caught an error:', error);
        logger.error('Error info:', errorInfo);

        // Persist the error info on state so the fallback UI can render
        // the component stack.  React only passes errorInfo into
        // componentDidCatch, never into getDerivedStateFromError.
        this.setState({ errorInfo });

        if (this.props.onError) {
            this.props.onError(error, errorInfo);
        }
    }

    handleToggleDetails = (): void => {
        this.setState((prev) => ({ detailsOpen: !prev.detailsOpen }));
    };

    handleReload = (): void => {
        // Hard reload so any module-level state (e.g. cached fetch
        // promises, EventSource subscriptions) is torn down.  This is
        // the safest recovery path for the Docker UI-bricking scenario
        // tracked in issue #182.
        window.location.reload();
    };

    /**
     * Compose the displayable error text (message + component stack)
     * so we can show it inline and so reporters can copy it verbatim
     * into a bug report.
     */
    private getDetailsText(): string {
        const { error, errorInfo } = this.state;
        const head = error ? error.toString() : 'Unknown error';
        const stack = error?.stack ? `\n\n${error.stack}` : '';
        const componentStack = errorInfo?.componentStack
            ? `\n\nComponent stack:${errorInfo.componentStack}`
            : '';
        return `${head}${stack}${componentStack}`;
    }

    render(): ReactNode {
        if (!this.state.hasError) {
            return this.props.children;
        }

        if (this.props.fallback) {
            return this.props.fallback;
        }

        const detailsText = this.getDetailsText();

        return (
            <Container maxWidth="md">
                <Box sx={styles.outerContainer}>
                    <Paper elevation={3} sx={styles.errorPaper}>
                        <Box sx={styles.iconRow}>
                            <ErrorOutlineIcon sx={styles.errorIcon} />
                            <Typography
                                variant="h5"
                                component="h1"
                                color="error"
                            >
                                Something went wrong
                            </Typography>
                        </Box>
                        <Alert
                            severity="error"
                            variant="outlined"
                            sx={styles.errorMessage}
                        >
                            <AlertTitle>The application has crashed</AlertTitle>
                            <Typography variant="body2" sx={{ mb: 1 }}>
                                Reload the page to continue. If the problem
                                persists, copy the details below and include
                                them in a bug report.
                            </Typography>
                        </Alert>

                        <Box sx={styles.detailsToggleRow}>
                            <IconButton
                                size="small"
                                onClick={this.handleToggleDetails}
                                aria-label={
                                    this.state.detailsOpen
                                        ? 'Hide error details'
                                        : 'Show error details'
                                }
                                aria-expanded={this.state.detailsOpen}
                            >
                                {this.state.detailsOpen ? (
                                    <ExpandLessIcon />
                                ) : (
                                    <ExpandMoreIcon />
                                )}
                            </IconButton>
                            <Typography
                                variant="subtitle2"
                                sx={{ color: 'text.secondary' }}
                            >
                                Error details
                            </Typography>
                        </Box>
                        <Collapse in={this.state.detailsOpen}>
                            <Paper
                                variant="outlined"
                                sx={styles.detailsPaper}
                            >
                                <Typography
                                    component="pre"
                                    sx={styles.detailsText}
                                    data-testid="error-boundary-details"
                                >
                                    {detailsText}
                                </Typography>
                            </Paper>
                        </Collapse>

                        <Box sx={styles.buttonContainer}>
                            <Button
                                variant="contained"
                                color="primary"
                                startIcon={<RefreshIcon />}
                                onClick={this.handleReload}
                            >
                                Reload
                            </Button>
                        </Box>
                    </Paper>
                </Box>
            </Container>
        );
    }
}

export default ErrorBoundary;
