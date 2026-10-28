/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { useEffect, useState } from 'react';

/**
 * Return `value` once it has stopped changing for `delayMs`.
 *
 * Each change restarts the timer, so a burst of keystrokes in a
 * search field yields a single update after the user pauses, rather
 * than one request per character. The first render returns `value`
 * immediately so that callers start from their initial state without
 * waiting out the delay.
 */
export const useDebouncedValue = <T>(value: T, delayMs: number): T => {
    const [debounced, setDebounced] = useState<T>(value);

    useEffect(() => {
        const timer = setTimeout(() => { setDebounced(value); }, delayMs);
        return () => { clearTimeout(timer); };
    }, [value, delayMs]);

    return debounced;
};

export default useDebouncedValue;
