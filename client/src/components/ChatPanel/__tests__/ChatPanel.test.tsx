/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { screen, fireEvent } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import ChatPanel from '../index';
import { renderWithTheme } from '../../../test/renderWithTheme';
import type { ChatContextType } from '../../../contexts/ChatContext';

const mockContext = vi.hoisted(() => ({ current: null as unknown }));
const mockDownload = vi.hoisted(() => vi.fn());

vi.mock('../../../contexts/useChatContext', () => ({
    useChatContext: () => mockContext.current,
}));

vi.mock('../../../utils/downloadMarkdown', () => ({
    downloadAsMarkdown: mockDownload,
}));

// Replace the history sidebar with buttons that call each callback,
// so these tests cover the panel's wiring rather than the sidebar.
vi.mock('../ConversationHistory', () => ({
    default: (props: {
        onSelect: (id: string) => void;
        onDelete: (id: string) => void;
        onRename: (id: string, title: string) => void;
        onClear: () => void;
        onRefresh: () => void;
        onClose: () => void;
    }) => (
        <div data-testid="history">
            <button onClick={() => { props.onSelect('c1'); }}>select</button>
            <button onClick={() => { props.onDelete('c1'); }}>delete</button>
            <button onClick={() => { props.onRename('c1', 'New'); }}>
                rename
            </button>
            <button onClick={props.onClear}>clear</button>
            <button onClick={props.onRefresh}>refresh</button>
            <button onClick={props.onClose}>close history</button>
        </div>
    ),
}));

type Messages = ChatContextType['messages'];

const summary = (
    id: string,
    title: string,
): ChatContextType['conversations'][number] => ({
    id,
    title,
    preview: '',
    created_at: 'now',
    updated_at: 'now',
});

const makeContext = (
    overrides: Partial<ChatContextType> = {},
): ChatContextType => ({
    isOpen: true,
    openChat: vi.fn(),
    closeChat: vi.fn(),
    toggleChat: vi.fn(),
    messages: [],
    conversationId: null,
    conversationTitle: '',
    isLoading: false,
    error: null,
    inputHistory: [],
    sendMessage: vi.fn().mockResolvedValue(undefined),
    clearChat: vi.fn(),
    conversations: [],
    loadConversation: vi.fn().mockResolvedValue(undefined),
    deleteConversation: vi.fn().mockResolvedValue(undefined),
    renameConversation: vi.fn().mockResolvedValue(undefined),
    refreshConversations: vi.fn().mockResolvedValue(undefined),
    activeTools: [],
    ...overrides,
});

const conversation: Messages = [
    { role: 'user', content: 'How is the server?', timestamp: 't1' },
    {
        role: 'assistant',
        content: [{ type: 'text', text: 'It is healthy.' }],
        timestamp: 't2',
        activity: [{ name: 'list_connections', status: 'completed' }],
    },
    { role: 'system', content: 'note' },
];

const setContext = (overrides: Partial<ChatContextType> = {}) => {
    const context = makeContext(overrides);
    mockContext.current = context;
    return context;
};

describe('ChatPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('shows the empty state and refreshes conversations on open', () => {
        const context = setContext();
        renderWithTheme(<ChatPanel open onClose={vi.fn()} />);

        expect(
            screen.getByText(/ask questions about your postgresql/i),
        ).toBeInTheDocument();
        expect(screen.queryByRole('log')).not.toBeInTheDocument();
        expect(context.refreshConversations).toHaveBeenCalledTimes(1);
    });

    it('does not refresh conversations when closed', () => {
        const context = setContext();
        renderWithTheme(<ChatPanel open={false} onClose={vi.fn()} />);
        expect(context.refreshConversations).not.toHaveBeenCalled();
    });

    it('renders messages, errors and tool status', () => {
        setContext({
            messages: conversation,
            error: 'Something broke',
            isLoading: true,
            activeTools: [{ name: 'query_database', status: 'running' }],
        });
        renderWithTheme(<ChatPanel open onClose={vi.fn()} />);

        expect(
            screen.getByRole('log', { name: 'Chat messages' }),
        ).toBeInTheDocument();
        expect(screen.getByText('How is the server?')).toBeInTheDocument();
        expect(screen.getByText('It is healthy.')).toBeInTheDocument();
        expect(screen.getByText('Something broke')).toBeInTheDocument();
    });

    it('sends messages, starts new chats and closes', () => {
        const onClose = vi.fn();
        const context = setContext();
        const { container } = renderWithTheme(
            <ChatPanel open onClose={onClose} />,
        );

        const input = container.querySelector('textarea');
        expect(input).not.toBeNull();
        fireEvent.change(input as HTMLTextAreaElement, {
            target: { value: 'Hello' },
        });
        fireEvent.click(screen.getByRole('button', { name: /send message/i }));
        expect(context.sendMessage).toHaveBeenCalledWith('Hello');

        fireEvent.click(
            screen.getByRole('button', { name: /start new conversation/i }),
        );
        expect(context.clearChat).toHaveBeenCalled();

        fireEvent.click(
            screen.getByRole('button', { name: /close chat panel/i }),
        );
        expect(onClose).toHaveBeenCalled();
    });

    it('wires the conversation history callbacks', () => {
        const context = setContext({
            conversations: [
                summary('a', 'A'),
                summary('b', 'B'),
            ],
        });
        vi.mocked(context.deleteConversation)
            .mockResolvedValueOnce(undefined)
            .mockResolvedValueOnce(undefined)
            .mockRejectedValueOnce(new Error('nope'));
        renderWithTheme(<ChatPanel open onClose={vi.fn()} />);

        const toggle = screen.getByRole('button', {
            name: /toggle conversation history/i,
        });
        fireEvent.click(toggle);
        expect(screen.getByTestId('history')).toBeInTheDocument();

        fireEvent.click(screen.getByText('select'));
        expect(context.loadConversation).toHaveBeenCalledWith('c1');
        fireEvent.click(screen.getByText('delete'));
        expect(context.deleteConversation).toHaveBeenCalledWith('c1');
        fireEvent.click(screen.getByText('rename'));
        expect(context.renameConversation).toHaveBeenCalledWith('c1', 'New');
        fireEvent.click(screen.getByText('clear'));
        expect(context.deleteConversation).toHaveBeenCalledWith('a');
        expect(context.deleteConversation).toHaveBeenCalledWith('b');
        fireEvent.click(screen.getByText('refresh'));
        expect(context.refreshConversations).toHaveBeenCalledTimes(2);

        fireEvent.click(screen.getByText('close history'));
        expect(screen.queryByTestId('history')).not.toBeInTheDocument();

        fireEvent.click(toggle);
        fireEvent.click(toggle);
        expect(screen.queryByTestId('history')).not.toBeInTheDocument();
    });

    it('downloads the conversation as markdown', () => {
        setContext({
            messages: conversation,
            conversationId: 'c1',
            conversations: [summary('c1', 'Health')],
        });
        renderWithTheme(<ChatPanel open onClose={vi.fn()} />);

        fireEvent.click(
            screen.getByRole('button', { name: /download conversation/i }),
        );

        expect(mockDownload).toHaveBeenCalledTimes(1);
        const [content, filename] = mockDownload.mock.calls[0] as [
            string,
            string,
        ];
        expect(filename).toMatch(/^ellie-chat-\d{4}-\d{2}-\d{2}\.md$/);
        expect(content).toContain('# Ellie Chat - Health');
        expect(content).toContain('## User\n\nHow is the server?');
        expect(content).toContain('## Ellie\n\nIt is healthy.');
        expect(content).not.toContain('note');
    });

    it('titles a download of an unsaved conversation generically', () => {
        setContext({ messages: conversation });
        renderWithTheme(<ChatPanel open onClose={vi.fn()} />);

        fireEvent.click(
            screen.getByRole('button', { name: /download conversation/i }),
        );

        const [content] = mockDownload.mock.calls[0] as [string];
        expect(content).toContain('# Ellie Chat - Conversation');
    });

    it('disables the download button without messages', () => {
        setContext();
        renderWithTheme(<ChatPanel open onClose={vi.fn()} />);
        expect(
            screen.getByRole('button', { name: /download conversation/i }),
        ).toBeDisabled();
    });

    describe('width', () => {
        it('restores a saved width and ignores invalid ones', () => {
            vi.mocked(localStorage.getItem).mockReturnValueOnce('600');
            setContext();
            const { unmount } = renderWithTheme(
                <ChatPanel open onClose={vi.fn()} />,
            );
            expect(
                screen.getByRole('complementary', { name: 'AI Chat Panel' }),
            ).toHaveStyle({ width: '600px' });
            unmount();

            vi.mocked(localStorage.getItem).mockReturnValueOnce('wide');
            renderWithTheme(<ChatPanel open onClose={vi.fn()} />);
            expect(
                screen.getByRole('complementary', { name: 'AI Chat Panel' }),
            ).toHaveStyle({ width: '450px' });
        });

        it('resizes by dragging and persists the clamped width', () => {
            setContext();
            renderWithTheme(<ChatPanel open onClose={vi.fn()} />);

            const handle = screen.getByRole('separator', {
                name: /resize chat panel/i,
            });
            fireEvent.mouseDown(handle, { clientX: 500 });
            expect(document.body.style.cursor).toBe('col-resize');

            fireEvent.mouseMove(document, { clientX: 400 });
            expect(
                screen.getByRole('complementary', { name: 'AI Chat Panel' }),
            ).toHaveStyle({ width: '550px' });

            fireEvent.mouseMove(document, { clientX: -1000 });
            fireEvent.mouseUp(document);

            expect(localStorage.setItem)
                .toHaveBeenCalledWith('chat-panel-width', '800');
            expect(document.body.style.cursor).toBe('');
        });
    });

    describe('scrolling (issue #629)', () => {
        const LIST_TOP = 100;
        const CLIENT_HEIGHT = 400;
        let scrollTop = 0;
        let scrollHeight = 0;
        let replyTop = 0;

        /** Stubs the layout of the message list, which jsdom lacks. */
        const stubGeometry = () => {
            const proto = HTMLElement.prototype;
            vi.spyOn(proto, 'clientHeight', 'get')
                .mockReturnValue(CLIENT_HEIGHT);
            vi.spyOn(proto, 'scrollHeight', 'get')
                .mockImplementation(() => scrollHeight);
            vi.spyOn(proto, 'scrollTop', 'get')
                .mockImplementation(() => scrollTop);
            vi.spyOn(proto, 'scrollTop', 'set')
                .mockImplementation((value: number) => { scrollTop = value; });
            vi.spyOn(Element.prototype, 'getBoundingClientRect')
                .mockImplementation(function (this: Element) {
                    if (this.getAttribute('role') === 'log') {
                        return { top: LIST_TOP, height: CLIENT_HEIGHT } as
                            DOMRect;
                    }
                    return { top: replyTop - scrollTop, height: 1000 } as
                        DOMRect;
                });
        };

        const pending: Messages = [
            { role: 'user', content: 'Earlier question' },
            { role: 'assistant', content: 'Earlier answer' },
            { role: 'user', content: 'Explain bloat' },
        ];
        const reply: Messages[number] = {
            role: 'assistant',
            content: 'A long answer',
        };

        beforeEach(() => {
            scrollTop = 0;
            scrollHeight = 800;
            replyTop = LIST_TOP + 800;
            stubGeometry();
        });

        it('scrolls to the bottom when the user sends a message', () => {
            setContext({ messages: pending.slice(0, 2) });
            const { rerender } = renderWithTheme(
                <ChatPanel open onClose={vi.fn()} />,
            );

            scrollHeight = 900;
            setContext({ messages: pending });
            rerender(<ChatPanel open onClose={vi.fn()} />);

            expect(scrollTop).toBe(900);
        });

        it('scrolls a long reply to its start', () => {
            setContext({ messages: pending });
            const { rerender } = renderWithTheme(
                <ChatPanel open onClose={vi.fn()} />,
            );
            scrollTop = 400;
            fireEvent.scroll(screen.getByRole('log'));

            scrollHeight = 1800;
            setContext({ messages: [...pending, reply] });
            rerender(<ChatPanel open onClose={vi.fn()} />);

            // The reply starts 800px down; the list keeps its 16px top
            // padding above it.
            expect(scrollTop).toBe(800 - 16);
        });

        it('leaves the view alone if the user scrolled up', () => {
            setContext({ messages: pending });
            const { rerender } = renderWithTheme(
                <ChatPanel open onClose={vi.fn()} />,
            );
            scrollTop = 50;
            fireEvent.scroll(screen.getByRole('log'));

            scrollHeight = 1800;
            setContext({ messages: [...pending, reply] });
            rerender(<ChatPanel open onClose={vi.fn()} />);

            expect(scrollTop).toBe(50);
        });

        it('defers a reply scroll until the panel reopens', () => {
            setContext({ messages: pending });
            const { rerender } = renderWithTheme(
                <ChatPanel open onClose={vi.fn()} />,
            );
            scrollTop = 400;
            fireEvent.scroll(screen.getByRole('log'));

            rerender(<ChatPanel open={false} onClose={vi.fn()} />);
            scrollHeight = 1800;
            setContext({ messages: [...pending, reply] });
            rerender(<ChatPanel open={false} onClose={vi.fn()} />);

            expect(scrollTop).toBe(400);

            rerender(<ChatPanel open onClose={vi.fn()} />);

            expect(scrollTop).toBe(800 - 16);
        });
    });
});
