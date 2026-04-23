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
import { screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { Home as HomeIcon, Settings as SettingsIcon } from '@mui/icons-material';
import { renderWithTheme } from '../../../test/renderWithTheme';
import {
    HelpNavItem,
    SectionTitle,
    HelpTip,
    FeatureItem,
} from '../components';

describe('HelpPanel components', () => {
    describe('HelpNavItem', () => {
        const defaultProps = {
            icon: HomeIcon,
            label: 'Test Label',
            pageId: 'testPage',
            currentPage: 'overview',
            onClick: vi.fn(),
        };

        it('renders with label text', () => {
            renderWithTheme(<HelpNavItem {...defaultProps} />);
            expect(screen.getByText('Test Label')).toBeInTheDocument();
        });

        it('renders the icon component', () => {
            renderWithTheme(<HelpNavItem {...defaultProps} />);
            const icon = document.querySelector('[data-testid="HomeIcon"]');
            expect(icon).toBeInTheDocument();
        });

        it('calls onClick with pageId when clicked', () => {
            const onClick = vi.fn();
            renderWithTheme(
                <HelpNavItem {...defaultProps} onClick={onClick} />
            );

            fireEvent.click(screen.getByText('Test Label'));

            expect(onClick).toHaveBeenCalledTimes(1);
            expect(onClick).toHaveBeenCalledWith('testPage');
        });

        it('does not show chevron when not active', () => {
            renderWithTheme(
                <HelpNavItem {...defaultProps} currentPage="otherPage" />
            );
            const chevron = document.querySelector(
                '[data-testid="ChevronRightIcon"]'
            );
            expect(chevron).not.toBeInTheDocument();
        });

        it('shows chevron when active', () => {
            renderWithTheme(
                <HelpNavItem {...defaultProps} currentPage="testPage" />
            );
            const chevron = document.querySelector(
                '[data-testid="ChevronRightIcon"]'
            );
            expect(chevron).toBeInTheDocument();
        });

        it('renders with selected state when active', () => {
            renderWithTheme(
                <HelpNavItem {...defaultProps} currentPage="testPage" />
            );
            const button = screen.getByRole('button', { name: /Test Label/i });
            expect(button).toHaveAttribute('aria-current', 'page');
        });

        it('renders without selected state when inactive', () => {
            renderWithTheme(
                <HelpNavItem {...defaultProps} currentPage="otherPage" />
            );
            const button = screen.getByRole('button', { name: /Test Label/i });
            expect(button).not.toHaveAttribute('aria-current');
        });

        it('renders with different icon', () => {
            renderWithTheme(
                <HelpNavItem {...defaultProps} icon={SettingsIcon} />
            );
            const icon = document.querySelector('[data-testid="SettingsIcon"]');
            expect(icon).toBeInTheDocument();
        });

        it('handles multiple clicks', () => {
            const onClick = vi.fn();
            renderWithTheme(
                <HelpNavItem {...defaultProps} onClick={onClick} />
            );

            const button = screen.getByRole('button');
            fireEvent.click(button);
            fireEvent.click(button);
            fireEvent.click(button);

            expect(onClick).toHaveBeenCalledTimes(3);
        });
    });

    describe('SectionTitle', () => {
        it('renders title text', () => {
            renderWithTheme(<SectionTitle>Test Section</SectionTitle>);
            expect(screen.getByText('Test Section')).toBeInTheDocument();
        });

        it('renders as h6 variant', () => {
            renderWithTheme(<SectionTitle>Section Heading</SectionTitle>);
            const heading = screen.getByRole('heading', { level: 6 });
            expect(heading).toHaveTextContent('Section Heading');
        });

        it('renders without icon by default', () => {
            renderWithTheme(<SectionTitle>No Icon Section</SectionTitle>);
            expect(screen.getByText('No Icon Section')).toBeInTheDocument();
            const homeIcon = document.querySelector('[data-testid="HomeIcon"]');
            expect(homeIcon).not.toBeInTheDocument();
        });

        it('renders with icon when provided', () => {
            renderWithTheme(
                <SectionTitle icon={HomeIcon}>With Icon</SectionTitle>
            );
            expect(screen.getByText('With Icon')).toBeInTheDocument();
            const icon = document.querySelector('[data-testid="HomeIcon"]');
            expect(icon).toBeInTheDocument();
        });

        it('renders with different icon', () => {
            renderWithTheme(
                <SectionTitle icon={SettingsIcon}>Settings Section</SectionTitle>
            );
            const icon = document.querySelector('[data-testid="SettingsIcon"]');
            expect(icon).toBeInTheDocument();
        });

        it('renders children as ReactNode', () => {
            renderWithTheme(
                <SectionTitle>
                    <span data-testid="custom-child">Custom Content</span>
                </SectionTitle>
            );
            expect(screen.getByTestId('custom-child')).toBeInTheDocument();
        });
    });

    describe('HelpTip', () => {
        it('renders tip text', () => {
            renderWithTheme(<HelpTip>This is a helpful tip.</HelpTip>);
            expect(screen.getByText('This is a helpful tip.')).toBeInTheDocument();
        });

        it('renders with "Tip:" label', () => {
            renderWithTheme(<HelpTip>Some tip content</HelpTip>);
            expect(screen.getByText('Tip:')).toBeInTheDocument();
        });

        it('renders children content', () => {
            renderWithTheme(
                <HelpTip>Click the button to continue.</HelpTip>
            );
            expect(
                screen.getByText('Click the button to continue.')
            ).toBeInTheDocument();
        });

        it('renders complex children', () => {
            renderWithTheme(
                <HelpTip>
                    Use <strong>Ctrl+S</strong> to save.
                </HelpTip>
            );
            expect(screen.getByText(/Use/)).toBeInTheDocument();
            expect(screen.getByText('Ctrl+S')).toBeInTheDocument();
        });

        it('renders tip label with primary color styling', () => {
            renderWithTheme(<HelpTip>Tip content here</HelpTip>);
            const tipLabel = screen.getByText('Tip:');
            expect(tipLabel).toBeInTheDocument();
            expect(tipLabel.tagName.toLowerCase()).toBe('strong');
        });
    });

    describe('FeatureItem', () => {
        it('renders feature title', () => {
            renderWithTheme(
                <FeatureItem
                    title="Feature Title"
                    description="Feature description text."
                />
            );
            expect(screen.getByText('Feature Title')).toBeInTheDocument();
        });

        it('renders feature description', () => {
            renderWithTheme(
                <FeatureItem
                    title="My Feature"
                    description="This describes the feature in detail."
                />
            );
            expect(
                screen.getByText('This describes the feature in detail.')
            ).toBeInTheDocument();
        });

        it('renders both title and description', () => {
            renderWithTheme(
                <FeatureItem
                    title="Cluster Organization"
                    description="Organize your database servers into logical clusters."
                />
            );
            expect(screen.getByText('Cluster Organization')).toBeInTheDocument();
            expect(
                screen.getByText(
                    'Organize your database servers into logical clusters.'
                )
            ).toBeInTheDocument();
        });

        it('renders empty description', () => {
            renderWithTheme(<FeatureItem title="Empty Feature" description="" />);
            expect(screen.getByText('Empty Feature')).toBeInTheDocument();
        });

        it('renders long description text', () => {
            const longDescription =
                'This is a very long description that contains many words and ' +
                'explains the feature in great detail. It may span multiple ' +
                'lines when rendered in the UI.';
            renderWithTheme(
                <FeatureItem title="Long Feature" description={longDescription} />
            );
            expect(screen.getByText(longDescription)).toBeInTheDocument();
        });

        it('renders special characters in title', () => {
            renderWithTheme(
                <FeatureItem
                    title="Host & Port"
                    description="Connection settings."
                />
            );
            expect(screen.getByText('Host & Port')).toBeInTheDocument();
        });

        it('renders special characters in description', () => {
            renderWithTheme(
                <FeatureItem
                    title="Query"
                    description='Use "SELECT * FROM table;" to query.'
                />
            );
            expect(
                screen.getByText('Use "SELECT * FROM table;" to query.')
            ).toBeInTheDocument();
        });
    });
});
