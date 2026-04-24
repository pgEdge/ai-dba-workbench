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
import type { TransitionProps } from '@mui/material/transitions';

/**
 * Creates a minimal mock HTMLElement for transition callbacks.
 * MUI Menu's handleEntering calls adjustStyleForScrollbar which needs
 * clientHeight and scrollHeight properties.
 */
const createMockElement = (): HTMLElement => {
    const mock = {
        clientHeight: 300,
        scrollHeight: 300,
        clientWidth: 200,
        scrollWidth: 200,
        style: {},
        ownerDocument: {
            defaultView: window,
        },
    };
    return mock as unknown as HTMLElement;
};

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

    // Keep the latest callback refs without making the lifecycle effect
    // depend on their identity. Parent components often pass fresh
    // function identities on every render (e.g., `TransitionProps={{
    // onEnter: handleEnter }}` with a non-memoised handler), which would
    // otherwise cause this effect to re-fire on every parent render and
    // clobber state changes that happened after the initial enter.
    const onEnterRef = React.useRef(onEnter);
    const onEnteringRef = React.useRef(onEntering);
    const onEnteredRef = React.useRef(onEntered);
    const onExitRef = React.useRef(onExit);
    const onExitingRef = React.useRef(onExiting);
    const onExitedRef = React.useRef(onExited);
    // Use useLayoutEffect (not useEffect) so ref sync runs before the
    // lifecycle useLayoutEffect below in the same commit. With useEffect,
    // the lifecycle effect would fire against stale refs whenever handler
    // identities change between renders.
    React.useLayoutEffect(() => {
        onEnterRef.current = onEnter;
        onEnteringRef.current = onEntering;
        onEnteredRef.current = onEntered;
        onExitRef.current = onExit;
        onExitingRef.current = onExiting;
        onExitedRef.current = onExited;
    });

    // useLayoutEffect runs synchronously inside the same commit that
    // userEvent's act() wraps, so the setState calls that Modal/Dialog
    // fire from these lifecycle callbacks stay inside the act boundary.
    // Only fire when `inProp` toggles, not on every render.
    React.useLayoutEffect(() => {
        // Create a mock element with required properties for MUI components
        // that access DOM properties in transition callbacks.
        const mockElement = createMockElement();
        if (inProp) {
            onEnterRef.current?.(mockElement, false);
            onEnteringRef.current?.(mockElement, false);
            onEnteredRef.current?.(mockElement, false);
        } else {
            onExitRef.current?.(mockElement);
            onExitingRef.current?.(mockElement);
            onExitedRef.current?.(mockElement);
        }
    }, [inProp]);

    if (!inProp) {
        return null;
    }
    return React.cloneElement(children, { ref });
});

export default ImmediateTransition;
