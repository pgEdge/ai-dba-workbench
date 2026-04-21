/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { describe, it, expect, vi, beforeEach, afterEach, MockInstance } from 'vitest';
import { downloadAsMarkdown } from '../downloadMarkdown';

describe('downloadAsMarkdown', () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    let mockCreateElement: MockInstance<any[], any>;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    let mockAppendChild: MockInstance<any[], any>;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    let mockRemoveChild: MockInstance<any[], any>;
    let mockCreateObjectURL: ReturnType<typeof vi.fn>;
    let mockRevokeObjectURL: ReturnType<typeof vi.fn>;
    let mockLink: HTMLAnchorElement;

    // Save original URL methods to restore after tests
    const originalCreateObjectURL = URL.createObjectURL;
    const originalRevokeObjectURL = URL.revokeObjectURL;

    beforeEach(() => {
        // Create a mock anchor element
        mockLink = {
            href: '',
            download: '',
            click: vi.fn(),
        } as unknown as HTMLAnchorElement;

        mockCreateElement = vi.spyOn(document, 'createElement')
            .mockReturnValue(mockLink as unknown as HTMLElement);

        mockAppendChild = vi.spyOn(document.body, 'appendChild')
            .mockReturnValue(mockLink as unknown as HTMLElement);

        mockRemoveChild = vi.spyOn(document.body, 'removeChild')
            .mockReturnValue(mockLink as unknown as HTMLElement);

        // jsdom doesn't implement URL.createObjectURL, so we need to define it
        mockCreateObjectURL = vi.fn()
            .mockReturnValue('blob:http://localhost/test-uuid');
        mockRevokeObjectURL = vi.fn();

        (URL.createObjectURL as unknown) = mockCreateObjectURL;
        (URL.revokeObjectURL as unknown) = mockRevokeObjectURL;
    });

    afterEach(() => {
        vi.restoreAllMocks();
        // Restore original URL methods to prevent cross-test pollution
        URL.createObjectURL = originalCreateObjectURL;
        URL.revokeObjectURL = originalRevokeObjectURL;
    });

    describe('basic functionality', () => {
        it('creates an anchor element', () => {
            downloadAsMarkdown('# Test Content', 'test.md');
            expect(mockCreateElement).toHaveBeenCalledWith('a');
        });

        it('creates a Blob with text/markdown content type', () => {
            const content = '# Test Content\n\nSome text here';
            downloadAsMarkdown(content, 'test.md');

            expect(mockCreateObjectURL).toHaveBeenCalledTimes(1);
            const blobArg = mockCreateObjectURL.mock.calls[0][0] as Blob;
            expect(blobArg).toBeInstanceOf(Blob);
            expect(blobArg.type).toBe('text/markdown');
        });

        it('sets the href to the object URL', () => {
            downloadAsMarkdown('content', 'file.md');
            expect(mockLink.href).toBe('blob:http://localhost/test-uuid');
        });

        it('sets the download attribute to the filename', () => {
            downloadAsMarkdown('content', 'my-document.md');
            expect(mockLink.download).toBe('my-document.md');
        });

        it('triggers a click on the anchor element', () => {
            downloadAsMarkdown('content', 'test.md');
            expect(mockLink.click).toHaveBeenCalledTimes(1);
        });
    });

    describe('DOM manipulation', () => {
        it('appends the link to document.body', () => {
            downloadAsMarkdown('content', 'test.md');
            expect(mockAppendChild).toHaveBeenCalledWith(mockLink);
        });

        it('removes the link from document.body after clicking', () => {
            downloadAsMarkdown('content', 'test.md');
            expect(mockRemoveChild).toHaveBeenCalledWith(mockLink);
        });

        it('calls appendChild before click', () => {
            const callOrder: string[] = [];
            mockAppendChild.mockImplementation(() => {
                callOrder.push('appendChild');
                return mockLink;
            });
            (mockLink.click as ReturnType<typeof vi.fn>).mockImplementation(() => {
                callOrder.push('click');
            });

            downloadAsMarkdown('content', 'test.md');
            expect(callOrder.indexOf('appendChild')).toBeLessThan(callOrder.indexOf('click'));
        });

        it('calls removeChild after click', () => {
            const callOrder: string[] = [];
            (mockLink.click as ReturnType<typeof vi.fn>).mockImplementation(() => {
                callOrder.push('click');
            });
            mockRemoveChild.mockImplementation(() => {
                callOrder.push('removeChild');
                return mockLink;
            });

            downloadAsMarkdown('content', 'test.md');
            expect(callOrder.indexOf('click')).toBeLessThan(callOrder.indexOf('removeChild'));
        });
    });

    describe('cleanup', () => {
        it('revokes the object URL after download', () => {
            downloadAsMarkdown('content', 'test.md');
            expect(mockRevokeObjectURL).toHaveBeenCalledWith('blob:http://localhost/test-uuid');
        });

        it('revokes URL after removing the link', () => {
            const callOrder: string[] = [];
            mockRemoveChild.mockImplementation(() => {
                callOrder.push('removeChild');
                return mockLink;
            });
            mockRevokeObjectURL.mockImplementation(() => {
                callOrder.push('revokeObjectURL');
            });

            downloadAsMarkdown('content', 'test.md');
            expect(callOrder.indexOf('removeChild')).toBeLessThan(callOrder.indexOf('revokeObjectURL'));
        });
    });

    describe('content handling', () => {
        it('handles empty content', () => {
            downloadAsMarkdown('', 'empty.md');
            expect(mockCreateObjectURL).toHaveBeenCalled();
            expect(mockLink.click).toHaveBeenCalled();
        });

        it('handles large content', () => {
            const largeContent = '# Large File\n' + 'Lorem ipsum '.repeat(10000);
            downloadAsMarkdown(largeContent, 'large.md');

            const blobArg = mockCreateObjectURL.mock.calls[0][0] as Blob;
            expect(blobArg.size).toBeGreaterThan(100000);
        });

        it('handles content with special characters', () => {
            const specialContent = '# Test\n\nCode: `SELECT * FROM users;`\n\n```sql\nSELECT * FROM "table";\n```';
            downloadAsMarkdown(specialContent, 'special.md');
            expect(mockLink.click).toHaveBeenCalled();
        });

        it('handles unicode content', () => {
            const unicodeContent = '# Unicode Test\n\n\u4e2d\u6587\u5185\u5bb9\n\nEmoji: \ud83d\udcca';
            downloadAsMarkdown(unicodeContent, 'unicode.md');
            expect(mockLink.click).toHaveBeenCalled();
        });
    });

    describe('filename handling', () => {
        it('handles filenames with spaces', () => {
            downloadAsMarkdown('content', 'my document.md');
            expect(mockLink.download).toBe('my document.md');
        });

        it('handles filenames without extension', () => {
            downloadAsMarkdown('content', 'document');
            expect(mockLink.download).toBe('document');
        });

        it('handles filenames with special characters', () => {
            downloadAsMarkdown('content', 'report-2024-01-15.md');
            expect(mockLink.download).toBe('report-2024-01-15.md');
        });
    });
});
