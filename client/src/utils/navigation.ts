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
 * Send the browser to a URL, leaving the single-page application
 * behind. This is for endpoints that answer with a redirect, such as
 * the federated sign-in start endpoint, which must be reached by a
 * browser navigation rather than by `fetch`.
 *
 * The target is a parameter so that tests can pass a double: jsdom
 * makes `window.location.assign` read-only, so it can be neither
 * reassigned nor spied upon.
 *
 * @param url - the URL to navigate to.
 * @param target - the location to navigate; defaults to the window's.
 */
export const navigateTo = (
    url: string,
    target: Location = window.location,
): void => {
    target.assign(url);
};
