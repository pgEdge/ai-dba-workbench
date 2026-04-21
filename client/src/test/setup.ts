/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import '@testing-library/jest-dom';
import { configure as configureDom } from '@testing-library/dom';
import { act } from '@testing-library/react';

/*
 * Capture a reference to the native `setTimeout` at module init so the
 * asyncWrapper below keeps working when a test calls
 * `vi.useFakeTimers()`. Without this, RTL async helpers (`findBy*`,
 * `waitFor`) invoked under fake timers would deadlock because the
 * replaced global `setTimeout` never fires until the fake clock is
 * advanced.
 */
const nativeSetTimeout = globalThis.setTimeout;

/*
 * React Testing Library (RTL) ships with its own nested copy of
 * `@testing-library/dom` (v9) and, on import, patches THAT copy's
 * `asyncWrapper` / `eventWrapper` / `unstable_advanceTimersWrapper`
 * so every `userEvent` call is wrapped in `act()`. The project's
 * direct dependency on `@testing-library/user-event` pulls in
 * `@testing-library/dom` v10 at the top of `node_modules`, so the
 * user-event code resolves the OUTER dom package and reads an
 * unpatched `getConfig()`. The result is that none of the
 * `userEvent` APIs are wrapped in `act()`, and every MUI internal
 * state update (`InputBase.checkDirty`, `FormControl.onFilled`,
 * `ButtonBase` mount flag, the `open` reset effect in `ServerDialog`,
 * etc.) surfaces a `Warning: An update to ... was not wrapped in
 * act(...)` message in stderr. See
 * https://github.com/testing-library/react-testing-library/issues/1338
 * for the upstream report.
 *
 * Re-apply the same wrappers on the outer `@testing-library/dom`
 * instance so `userEvent` sees them and every event dispatch runs
 * inside `act()`. Copied from RTL's own configure block in
 * `node_modules/@testing-library/react/dist/pure.js`.
 */
function getIsReactActEnvironment(): boolean {
    return (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean })
        .IS_REACT_ACT_ENVIRONMENT ?? false;
}

function setIsReactActEnvironment(value: boolean): void {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean })
        .IS_REACT_ACT_ENVIRONMENT = value;
}

configureDom({
    unstable_advanceTimersWrapper: (cb: () => unknown) => {
        return act(cb as () => void | Promise<void>);
    },
    asyncWrapper: async (cb: () => unknown) => {
        const previous = getIsReactActEnvironment();
        setIsReactActEnvironment(false);
        try {
            const result = await cb();
            await new Promise<void>((resolve) => {
                nativeSetTimeout(() => resolve(), 0);
            });
            return result;
        } finally {
            setIsReactActEnvironment(previous);
        }
    },
    eventWrapper: (cb: () => unknown) => {
        let result: unknown;
        act(() => {
            result = cb();
        });
        return result;
    },
});

// Mock localStorage
const localStorageMock = {
    getItem: vi.fn(),
    setItem: vi.fn(),
    removeItem: vi.fn(),
    clear: vi.fn(),
};
Object.defineProperty(window, 'localStorage', { value: localStorageMock });

// Mock sessionStorage
const sessionStorageMock = {
    getItem: vi.fn(),
    setItem: vi.fn(),
    removeItem: vi.fn(),
    clear: vi.fn(),
};
Object.defineProperty(window, 'sessionStorage', { value: sessionStorageMock });

/*
 * Replace the project-shared `SlideTransition` with a synchronous
 * passthrough in every test. Several dialogs (ServerDialog,
 * ServerInfoDialog, GroupDialog, ClusterConfigDialog, AdminPanel, and
 * the analysis dialogs) pass `TransitionComponent={SlideTransition}`
 * explicitly on their `<Dialog>` element; that prop overrides the
 * default `TransitionComponent` that `renderWithTheme` installs on the
 * theme, so MUI's `Slide` still mounts a real `react-transition-group`
 * `Transition` and schedules `setTimeout` callbacks that fire outside
 * React's `act()` boundary, emitting `not wrapped in act(...)` warnings.
 *
 * Mocking `SlideTransition` at the module level bypasses the Slide
 * entirely and keeps the enter/exit lifecycle inside the current commit,
 * so every dialog test inherits the same act-safe behaviour without
 * needing per-file `vi.mock` calls. Any test that genuinely needs the
 * real slide behaviour can opt back in with
 * `vi.unmock('./components/shared/SlideTransition')`.
 */
vi.mock('../components/shared/SlideTransition', async () => {
    const mod = await import('./ImmediateTransition');
    return { default: mod.default };
});

