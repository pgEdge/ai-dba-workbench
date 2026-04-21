/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/**
 * Shared test render helper that wraps components in an MUI ThemeProvider
 * with all transitions disabled.
 *
 * MUI components (Dialog, Accordion, Tabs, Fade, Slide, etc.) default to
 * ~225ms CSS transitions driven by `react-transition-group`. Even with
 * `timeout: 0`, the underlying `Transition` primitive still schedules a
 * `setTimeout` callback that fires outside React's `act()` boundary,
 * producing `Warning: An update to ... was not wrapped in act(...)` in
 * test output. Under real timers on slow CI runners these same deferred
 * callbacks also accumulate enough wall-clock time to exceed Vitest's
 * default 5000ms per-test timeout.
 *
 * This helper builds a theme that:
 *
 * - Replaces `theme.transitions.create()` with a no-op so any inline
 *   MUI style call produces no CSS transition.
 *
 * - Zeros every entry in `theme.transitions.duration`.
 *
 * - Swaps `Dialog`'s and `Popover`'s `TransitionComponent` for a
 *   synchronous passthrough component (`ImmediateTransition`) that
 *   fires the enter/exit lifecycle callbacks inline instead of via
 *   `setTimeout`. This eliminates the deferred state updates that
 *   generate `act(...)` warnings.
 *
 * - Sets `timeout: 0` on `Collapse` (used by `Accordion`) as a
 *   belt-and-suspenders default for components that render it directly.
 *
 * - Disables ripples on `ButtonBase` so clicks do not spawn their own
 *   deferred animations.
 *
 * Note: when a component passes its own `TransitionComponent` prop on a
 * Dialog element (for example, the project-shared `SlideTransition`),
 * that prop overrides the theme's default. The global `vi.mock` in
 * `src/test/setup.ts` replaces the `SlideTransition` module with the
 * same synchronous component so those dialogs also stay inside
 * `act()`.
 *
 * Use this helper in place of `@testing-library/react`'s `render` for any
 * test that mounts a Dialog, Accordion, Drawer, Menu, Popover, Snackbar,
 * or similar transition-wrapped component.
 */

import React from 'react';
import { render, RenderOptions, RenderResult } from '@testing-library/react';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import ImmediateTransition from './ImmediateTransition';

const noTransitionsTheme = createTheme({
    palette: {
        custom: {
            status: {
                connected: '#22B8CF',
                online: '#40C057',
                sky: '#339AF0',
                skyDark: '#1C7ED6',
                skyLight: '#74C0FC',
                purple: '#9775FA',
                purpleLight: '#B197FC',
                cyan: '#15AABF',
            },
            accent: '#15AABF',
            accentHover: '#0C8599',
            accentLight: '#22B8CF',
        },
    },
    transitions: {
        create: () => 'none',
        duration: {
            shortest: 0,
            shorter: 0,
            short: 0,
            standard: 0,
            complex: 0,
            enteringScreen: 0,
            leavingScreen: 0,
        },
    },
    components: {
        MuiDialog: {
            defaultProps: {
                transitionDuration: 0,
                TransitionComponent: ImmediateTransition as never,
            },
        },
        MuiPopover: {
            defaultProps: {
                transitionDuration: 0,
                TransitionComponent: ImmediateTransition as never,
            },
        },
        MuiModal: {
            defaultProps: {
                closeAfterTransition: false,
            },
        },
        MuiAccordion: {
            defaultProps: {
                TransitionProps: { timeout: 0 },
                TransitionComponent: ImmediateTransition as never,
            },
        },
        MuiCollapse: {
            defaultProps: {
                timeout: 0,
            },
        },
        MuiButtonBase: {
            defaultProps: {
                disableRipple: true,
            },
        },
    },
});

/**
 * Render a React element wrapped in a ThemeProvider whose theme disables
 * all MUI transitions and ripples. Returns the standard
 * `@testing-library/react` RenderResult so callers can destructure
 * `rerender`, `unmount`, `container`, etc.
 */
export const renderWithTheme = (
    ui: React.ReactElement,
    options?: Omit<RenderOptions, 'wrapper'>,
): RenderResult => {
    const Wrapper: React.FC<{ children: React.ReactNode }> = ({ children }) => (
        <ThemeProvider theme={noTransitionsTheme}>{children}</ThemeProvider>
    );
    return render(ui, { wrapper: Wrapper, ...options });
};

export default renderWithTheme;
