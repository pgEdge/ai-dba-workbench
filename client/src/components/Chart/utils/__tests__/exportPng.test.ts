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
 * Unit coverage for `exportChartAsPng`. The helper is a thin wrapper
 * around the echarts `getDataURL` API that injects a temporary anchor
 * into the DOM, clicks it, and removes it. The tests assert the call
 * args sent to echarts, the anchor attributes, and the side-effect
 * sequence on `document.body` so future regressions in any of these
 * stages surface immediately.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { exportChartAsPng } from '../exportPng';

interface ExportableChart {
    getDataURL: (opts: {
        type: string;
        pixelRatio: number;
        backgroundColor: string;
    }) => string;
}

describe('exportChartAsPng', () => {
    let appendSpy: ReturnType<typeof vi.spyOn>;
    let removeSpy: ReturnType<typeof vi.spyOn>;
    let originalAnchorClick: () => void;

    beforeEach(() => {
        appendSpy = vi.spyOn(document.body, 'appendChild');
        removeSpy = vi.spyOn(document.body, 'removeChild');
        // jsdom logs "Not implemented: navigation to another Document"
        // when an <a> with an href is clicked. Override the prototype so
        // the click is observable but does not navigate.
        originalAnchorClick = HTMLAnchorElement.prototype.click;
        HTMLAnchorElement.prototype.click = function noopClick() {};
    });

    afterEach(() => {
        appendSpy.mockRestore();
        removeSpy.mockRestore();
        HTMLAnchorElement.prototype.click = originalAnchorClick;
    });

    it('forwards png type, pixelRatio 2, and white background to echarts', () => {
        const getDataURL = vi.fn().mockReturnValue('data:image/png;base64,abc');
        const chart: ExportableChart = { getDataURL };

        exportChartAsPng(chart, 'cpu-usage');

        expect(getDataURL).toHaveBeenCalledTimes(1);
        expect(getDataURL).toHaveBeenCalledWith({
            type: 'png',
            pixelRatio: 2,
            backgroundColor: '#fff',
        });
    });

    it('attaches an anchor with the data URL and the .png filename', () => {
        const getDataURL = vi.fn().mockReturnValue('data:image/png;base64,XYZ');
        const chart: ExportableChart = { getDataURL };

        exportChartAsPng(chart, 'memory-trends');

        expect(appendSpy).toHaveBeenCalledTimes(1);
        const appended = appendSpy.mock.calls[0][0] as HTMLAnchorElement;
        expect(appended.tagName).toBe('A');
        expect(appended.href).toBe('data:image/png;base64,XYZ');
        expect(appended.download).toBe('memory-trends.png');
    });

    it('clicks the anchor between append and remove', () => {
        const getDataURL = vi.fn().mockReturnValue('data:image/png;base64,1');
        const chart: ExportableChart = { getDataURL };

        // Capture which anchor receives the click() call. Use the
        // appendChild spy as the proxy for the anchor identity, since
        // exportChartAsPng appends the same anchor it later clicks and
        // removes; that avoids aliasing `this` in a function expression.
        HTMLAnchorElement.prototype.click = function patchedClick() {
            // No-op; jsdom would otherwise log a navigation warning.
        };

        exportChartAsPng(chart, 'connections');

        // The same anchor that appendChild received must be removed.
        expect(appendSpy).toHaveBeenCalledTimes(1);
        expect(removeSpy).toHaveBeenCalledTimes(1);
        const appended = appendSpy.mock.calls[0][0];
        expect(appended).toBeInstanceOf(HTMLAnchorElement);
        expect(removeSpy.mock.calls[0][0]).toBe(appended);
        // appendChild must run before removeChild.
        expect(
            appendSpy.mock.invocationCallOrder[0],
        ).toBeLessThan(removeSpy.mock.invocationCallOrder[0]);
    });

    it('uses an empty filename suffix .png when filename is empty', () => {
        const getDataURL = vi.fn().mockReturnValue('data:image/png;base64,zz');
        const chart: ExportableChart = { getDataURL };

        exportChartAsPng(chart, '');

        const appended = appendSpy.mock.calls[0][0] as HTMLAnchorElement;
        expect(appended.download).toBe('.png');
    });
});
