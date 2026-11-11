/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { vi } from 'vitest';
// Use the Vitest entry point: it registers the matchers with Vitest's
// expect and augments Vitest's Assertion type. Under Vitest 5 the bare
// '@testing-library/jest-dom' import leaves the matchers untyped.
import '@testing-library/jest-dom/vitest';
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
 * Override the wrappers that React Testing Library (RTL) installs on
 * `@testing-library/dom` when it is imported. They are copied from
 * RTL's own configure block in
 * `node_modules/@testing-library/react/dist/pure.js`, with one
 * difference: RTL's `asyncWrapper` drains the task queue with the
 * global `setTimeout` and only advances fake timers when the `jest`
 * global exists, so under `vi.useFakeTimers()` that `setTimeout` never
 * fires and `findBy*` / `waitFor` hang. This copy uses the native
 * `setTimeout` captured above instead.
 *
 * Before RTL 16 this block also worked around RTL carrying its own
 * nested copy of `@testing-library/dom`, which left `userEvent` calls
 * outside `act()` (see
 * https://github.com/testing-library/react-testing-library/issues/1338);
 * `@testing-library/dom` is now a single peer dependency.
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
                nativeSetTimeout(() => { resolve(); }, 0);
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

