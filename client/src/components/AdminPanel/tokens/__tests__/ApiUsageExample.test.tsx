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
import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import ApiUsageExample from '../ApiUsageExample';

const theme = createTheme();

const renderComponent = () => {
    return render(
        <ThemeProvider theme={theme}>
            <ApiUsageExample />
        </ThemeProvider>
    );
};

describe('ApiUsageExample', () => {
    it('renders the section title', () => {
        renderComponent();
        expect(screen.getByText('API Usage Examples')).toBeInTheDocument();
    });

    it('renders the description text', () => {
        renderComponent();
        expect(
            screen.getByText('Use tokens with the Authorization header to access the API.')
        ).toBeInTheDocument();
    });

    it('renders curl examples', () => {
        renderComponent();

        // Check for specific example commands
        expect(screen.getByText(/# List connections/)).toBeInTheDocument();
        expect(screen.getByText(/# Get connection details/)).toBeInTheDocument();
        expect(screen.getByText(/# Create a connection/)).toBeInTheDocument();
        expect(screen.getByText(/# Delete a connection/)).toBeInTheDocument();
        expect(screen.getByText(/# Chat with the AI assistant/)).toBeInTheDocument();
    });

    it('renders Authorization header placeholder', () => {
        renderComponent();
        const preElement = screen.getByText(/Authorization: Bearer/);
        expect(preElement).toBeInTheDocument();
    });

    it('renders the code block with monospace font', () => {
        renderComponent();
        const preElement = screen.getByText(/# List connections/).closest('pre');
        expect(preElement).toBeInTheDocument();
        expect(preElement).toHaveStyle({ fontFamily: 'monospace' });
    });
});
