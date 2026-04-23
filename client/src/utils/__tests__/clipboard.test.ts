/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { copyToClipboard } from '../clipboard';

// Hold the original clipboard descriptor so we can restore it after each test.
const originalDescriptor = Object.getOwnPropertyDescriptor(
    navigator,
    'clipboard'
);

/**
 * Replace navigator.clipboard with a custom value (or remove it entirely
 * when `value` is undefined).
 */
const setClipboard = (value: unknown) => {
    if (value === undefined) {
        // Simulate a non-secure context where the property does not exist.
        Object.defineProperty(navigator, 'clipboard', {
            configurable: true,
            value: undefined,
        });
    } else {
        Object.defineProperty(navigator, 'clipboard', {
            configurable: true,
            value,
        });
    }
};

const restoreClipboard = () => {
    if (originalDescriptor) {
        Object.defineProperty(navigator, 'clipboard', originalDescriptor);
    }
};

describe('copyToClipboard', () => {
    afterEach(() => {
        restoreClipboard();
        vi.restoreAllMocks();
    });

    // -- Modern Clipboard API path ------------------------------------------

    describe('when the Clipboard API is available', () => {
        let writeTextMock: ReturnType<typeof vi.fn>;

        beforeEach(() => {
            writeTextMock = vi.fn().mockResolvedValue(undefined);
            setClipboard({ writeText: writeTextMock });
        });

        it('calls navigator.clipboard.writeText with the supplied text',
            async () => {
                await copyToClipboard('hello');
                expect(writeTextMock).toHaveBeenCalledWith('hello');
            }
        );

        it('resolves on success', async () => {
            await expect(copyToClipboard('text')).resolves.toBeUndefined();
        });

        it('rejects when writeText rejects', async () => {
            writeTextMock.mockRejectedValueOnce(
                new Error('Permission denied')
            );
            await expect(copyToClipboard('x')).rejects.toThrow(
                'Permission denied'
            );
        });
    });

    // -- execCommand fallback path ------------------------------------------

    describe('when the Clipboard API is unavailable', () => {
        let execCommandMock: ReturnType<typeof vi.fn>;
        let appendChildSpy: ReturnType<typeof vi.fn>;
        let removeChildSpy: ReturnType<typeof vi.fn>;

        beforeEach(() => {
            // Remove the Clipboard API so the fallback is triggered.
            setClipboard(undefined);

            // jsdom does not provide document.execCommand; define it so
            // we can spy on it in the fallback tests.
            execCommandMock = vi.fn().mockReturnValue(true);
            document.execCommand = execCommandMock;

            appendChildSpy = vi.spyOn(
                document.body, 'appendChild'
            ) as unknown as ReturnType<typeof vi.fn>;
            removeChildSpy = vi.spyOn(
                document.body, 'removeChild'
            ) as unknown as ReturnType<typeof vi.fn>;
        });

        it('creates a textarea, selects its content, and calls execCommand',
            async () => {
                await copyToClipboard('fallback text');

                // A textarea should have been appended and then removed.
                expect(appendChildSpy).toHaveBeenCalledTimes(1);
                expect(removeChildSpy).toHaveBeenCalledTimes(1);

                const textarea = appendChildSpy.mock
                    .calls[0][0] as HTMLTextAreaElement;
                expect(textarea.tagName).toBe('TEXTAREA');
                expect(textarea.value).toBe('fallback text');

                expect(execCommandMock).toHaveBeenCalledWith('copy');
            }
        );

        it('throws when execCommand returns false', async () => {
            execCommandMock.mockReturnValue(false);
            await expect(copyToClipboard('fail')).rejects.toThrow(
                /execCommand.*failed/i
            );
        });

        it('removes the textarea even when execCommand throws', async () => {
            execCommandMock.mockImplementation(() => {
                throw new Error('boom');
            });

            await expect(copyToClipboard('cleanup')).rejects.toThrow('boom');

            // The finally block must still remove the element.
            expect(removeChildSpy).toHaveBeenCalledTimes(1);
        });

        describe('with a custom container', () => {
            let container: HTMLDivElement;
            let containerAppendSpy: ReturnType<typeof vi.fn>;
            let containerRemoveSpy: ReturnType<typeof vi.fn>;

            beforeEach(() => {
                container = document.createElement('div');
                document.body.appendChild(container);
                containerAppendSpy = vi.spyOn(
                    container, 'appendChild'
                ) as unknown as ReturnType<typeof vi.fn>;
                containerRemoveSpy = vi.spyOn(
                    container, 'removeChild'
                ) as unknown as ReturnType<typeof vi.fn>;

                // Reset the document.body spies after creating the container
                // so they only capture calls made by copyToClipboard.
                appendChildSpy.mockClear();
                removeChildSpy.mockClear();
            });

            afterEach(() => {
                document.body.removeChild(container);
            });

            it('appends the textarea to the container instead of body',
                async () => {
                    await copyToClipboard('container text', container);

                    expect(containerAppendSpy).toHaveBeenCalledTimes(1);
                    expect(containerRemoveSpy).toHaveBeenCalledTimes(1);

                    const textarea = containerAppendSpy.mock
                        .calls[0][0] as HTMLTextAreaElement;
                    expect(textarea.tagName).toBe('TEXTAREA');
                    expect(textarea.value).toBe('container text');

                    // document.body should NOT have been touched.
                    expect(appendChildSpy).not.toHaveBeenCalled();
                    expect(removeChildSpy).not.toHaveBeenCalled();

                    expect(execCommandMock).toHaveBeenCalledWith('copy');
                }
            );

            it('cleans up the textarea from the container on failure',
                async () => {
                    execCommandMock.mockImplementation(() => {
                        throw new Error('boom');
                    });

                    await expect(
                        copyToClipboard('cleanup', container)
                    ).rejects.toThrow('boom');

                    expect(containerRemoveSpy).toHaveBeenCalledTimes(1);
                }
            );
        });
    });

    // -- Clipboard API present but writeText missing (edge case) ------------

    describe('when navigator.clipboard exists but writeText is absent', () => {
        beforeEach(() => {
            setClipboard({});
            document.execCommand = vi.fn().mockReturnValue(true);
        });

        it('falls back to execCommand', async () => {
            await copyToClipboard('edge case');
            expect(document.execCommand).toHaveBeenCalledWith('copy');
        });
    });
});
