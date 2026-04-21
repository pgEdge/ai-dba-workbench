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
 * Synchronous stand-in for `react-transition-group`'s `Transition`.
 *
 * Invokes the enter/exit lifecycle callbacks immediately on mount so
 * MUI components that chain off them (Dialog, Popover, Modal) complete
 * their state machine inside the same React commit; no `setTimeout` is
 * scheduled and no state update escapes `act()`.
 *
 * Shared between `renderWithTheme` (which wires it as a default
 * `TransitionComponent` on `MuiDialog`/`MuiPopover`) and test setup
 * (which uses it to mock the project-local `SlideTransition` module so
 * explicit `TransitionComponent={SlideTransition}` props on dialogs do
 * not bypass the synchronous behaviour).
 */

import React from 'react';
import { TransitionProps } from '@mui/material/transitions';

const ImmediateTransition = React.forwardRef<
    HTMLElement,
    TransitionProps & { children: React.ReactElement }
>(function ImmediateTransition(props, ref) {
    const {
        children,
        in: inProp,
        onEnter,
        onEntering,
        onEntered,
        onExit,
        onExiting,
        onExited,
    } = props;

    // useLayoutEffect runs synchronously inside the same commit that
    // userEvent's act() wraps, so the setState calls that Modal/Dialog
    // fire from these lifecycle callbacks stay inside the act boundary.
    React.useLayoutEffect(() => {
        if (inProp) {
            onEnter?.(null as unknown as HTMLElement, false);
            onEntering?.(null as unknown as HTMLElement, false);
            onEntered?.(null as unknown as HTMLElement, false);
        } else {
            onExit?.(null as unknown as HTMLElement);
            onExiting?.(null as unknown as HTMLElement);
            onExited?.(null as unknown as HTMLElement);
        }
    }, [inProp, onEnter, onEntering, onEntered, onExit, onExiting, onExited]);

    if (!inProp) {
        return null;
    }
    return React.cloneElement(children, { ref });
});

export default ImmediateTransition;
