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
import { screen, fireEvent, act } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';
import ChatFAB from '../ChatFAB';
import { renderWithTheme } from '../../../test/renderWithTheme';

describe('ChatFAB', () => {
    const defaultProps = {
        onClick: vi.fn(),
        isOpen: false,
    };

    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('rendering', () => {
        it('renders the FAB button', () => {
            renderWithTheme(<ChatFAB {...defaultProps} />);
            expect(
                screen.getByRole('button', { name: /open chat/i })
            ).toBeInTheDocument();
        });

        it('renders chat icon when closed', () => {
            renderWithTheme(<ChatFAB {...defaultProps} isOpen={false} />);
            expect(
                screen.getByTestId('SmartToyOutlinedIcon')
            ).toBeInTheDocument();
        });

        it('renders close icon when open', () => {
            renderWithTheme(<ChatFAB {...defaultProps} isOpen={true} />);
            expect(screen.getByTestId('CloseIcon')).toBeInTheDocument();
        });
    });

    describe('accessibility', () => {
        it('has correct aria-label when closed', () => {
            renderWithTheme(<ChatFAB {...defaultProps} isOpen={false} />);
            expect(
                screen.getByRole('button', { name: /open chat/i })
            ).toBeInTheDocument();
        });

        it('has correct aria-label when open', () => {
            renderWithTheme(<ChatFAB {...defaultProps} isOpen={true} />);
            expect(
                screen.getByRole('button', { name: /close chat/i })
            ).toBeInTheDocument();
        });
    });

    describe('tooltip', () => {
        it('shows "Open AI Chat" tooltip when closed', async () => {
            renderWithTheme(<ChatFAB {...defaultProps} isOpen={false} />);
            const button = screen.getByRole('button', { name: /open chat/i });

            await act(async () => {
                fireEvent.mouseOver(button);
            });

            expect(
                await screen.findByRole('tooltip')
            ).toHaveTextContent('Open AI Chat');
        });

        it('shows "Close AI Chat" tooltip when open', async () => {
            renderWithTheme(<ChatFAB {...defaultProps} isOpen={true} />);
            const button = screen.getByRole('button', { name: /close chat/i });

            await act(async () => {
                fireEvent.mouseOver(button);
            });

            expect(
                await screen.findByRole('tooltip')
            ).toHaveTextContent('Close AI Chat');
        });
    });

    describe('interactions', () => {
        it('calls onClick when clicked', async () => {
            const onClick = vi.fn();
            renderWithTheme(<ChatFAB {...defaultProps} onClick={onClick} />);

            await act(async () => {
                fireEvent.click(
                    screen.getByRole('button', { name: /open chat/i })
                );
            });

            expect(onClick).toHaveBeenCalledTimes(1);
        });

        it('calls onClick when clicked in open state', async () => {
            const onClick = vi.fn();
            renderWithTheme(
                <ChatFAB {...defaultProps} onClick={onClick} isOpen={true} />
            );

            await act(async () => {
                fireEvent.click(
                    screen.getByRole('button', { name: /close chat/i })
                );
            });

            expect(onClick).toHaveBeenCalledTimes(1);
        });
    });
});
