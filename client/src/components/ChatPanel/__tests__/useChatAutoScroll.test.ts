/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { renderHook } from '@testing-library/react';
import { describe, it, expect, beforeEach } from 'vitest';
import useChatAutoScroll, {
    NEAR_BOTTOM_THRESHOLD_PX,
    type ScrollableMessage,
} from '../useChatAutoScroll';

const LIST_TOP = 100;
const CLIENT_HEIGHT = 400;

/**
 * A message list whose geometry the test controls: jsdom does no
 * layout, so scrollHeight, clientHeight, scrollTop and each child's
 * bounding rectangle are stubbed. Children are laid out top to bottom
 * by their heights, offset by the current scroll position.
 */
const createList = () => {
    const list = document.createElement('div');
    const heights: number[] = [];
    let scrollTop = 0;

    const scrollHeight = () => heights.reduce((a, b) => a + b, 0);

    Object.defineProperty(list, 'clientHeight', {
        configurable: true,
        get: () => CLIENT_HEIGHT,
    });
    Object.defineProperty(list, 'scrollHeight', {
        configurable: true,
        get: scrollHeight,
    });
    Object.defineProperty(list, 'scrollTop', {
        configurable: true,
        get: () => scrollTop,
        set: (value: number) => {
            const max = Math.max(0, scrollHeight() - CLIENT_HEIGHT);
            scrollTop = Math.min(Math.max(0, value), max);
        },
    });
    list.getBoundingClientRect = () =>
        ({ top: LIST_TOP, height: CLIENT_HEIGHT }) as DOMRect;

    const addChild = (height: number) => {
        const index = heights.length;
        heights.push(height);
        const child = document.createElement('div');
        child.getBoundingClientRect = () => {
            const offset = heights
                .slice(0, index)
                .reduce((a, b) => a + b, 0);
            return {
                top: LIST_TOP + offset - scrollTop,
                height,
            } as DOMRect;
        };
        list.appendChild(child);
    };

    const replaceChildren = (newHeights: number[]) => {
        heights.length = 0;
        list.replaceChildren();
        newHeights.forEach(addChild);
    };

    return { list, addChild, replaceChildren };
};

const user = (): ScrollableMessage => ({ role: 'user' });
const assistant = (): ScrollableMessage => ({ role: 'assistant' });

describe('useChatAutoScroll', () => {
    let harness: ReturnType<typeof createList>;

    beforeEach(() => {
        harness = createList();
    });

    const render = (initial: ScrollableMessage[]) =>
        renderHook(
            ({ messages }) => useChatAutoScroll(
                { current: harness.list },
                messages,
            ),
            { initialProps: { messages: initial } },
        );

    /** Builds a conversation of `turns` short exchanges. */
    const history = (turns: number): ScrollableMessage[] => {
        const messages: ScrollableMessage[] = [];
        for (let i = 0; i < turns; i++) {
            messages.push(user(), assistant());
            harness.addChild(60);
            harness.addChild(200);
        }
        return messages;
    };

    it('scrolls to the bottom when the user sends a message', () => {
        const initial = history(3);
        const { rerender } = render(initial);
        expect(harness.list.scrollTop).toBe(780 - CLIENT_HEIGHT);

        harness.addChild(60);
        rerender({ messages: [...initial, user()] });

        expect(harness.list.scrollTop).toBe(840 - CLIENT_HEIGHT);
    });

    it('scrolls a long reply so that its top is at the top of the list',
        () => {
            const initial = [...history(3), user()];
            harness.addChild(60);
            const { rerender } = render(initial);

            harness.addChild(1000);
            rerender({ messages: [...initial, assistant()] });

            // The reply starts 840px down the content.
            expect(harness.list.scrollTop).toBe(840);
            expect(
                harness.list.lastElementChild?.getBoundingClientRect().top,
            ).toBe(LIST_TOP);
        });

    it('leaves the top padding of the list above a long reply', () => {
        harness.list.style.paddingTop = '16px';
        const initial = [...history(3), user()];
        harness.addChild(60);
        const { rerender } = render(initial);

        harness.addChild(1000);
        rerender({ messages: [...initial, assistant()] });

        expect(harness.list.scrollTop).toBe(840 - 16);
    });

    it('scrolls to the bottom when the reply fits in the list', () => {
        const initial = [...history(3), user()];
        harness.addChild(60);
        const { rerender } = render(initial);

        harness.addChild(CLIENT_HEIGHT);
        rerender({ messages: [...initial, assistant()] });

        expect(harness.list.scrollTop).toBe(1240 - CLIENT_HEIGHT);
    });

    it('does not move the view when the user has scrolled up', () => {
        const initial = [...history(3), user()];
        harness.addChild(60);
        const { result, rerender } = render(initial);

        harness.list.scrollTop = 100;
        result.current();

        harness.addChild(1000);
        rerender({ messages: [...initial, assistant()] });

        expect(harness.list.scrollTop).toBe(100);
    });

    it('still follows a reply when the user is within the threshold',
        () => {
            const initial = [...history(3), user()];
            harness.addChild(60);
            const { result, rerender } = render(initial);

            harness.list.scrollTop =
                840 - CLIENT_HEIGHT - NEAR_BOTTOM_THRESHOLD_PX;
            result.current();

            harness.addChild(1000);
            rerender({ messages: [...initial, assistant()] });

            expect(harness.list.scrollTop).toBe(840);
        });

    it('scrolls to the bottom on send even after scrolling up', () => {
        const initial = history(3);
        const { result, rerender } = render(initial);

        harness.list.scrollTop = 0;
        result.current();

        harness.addChild(60);
        rerender({ messages: [...initial, user()] });

        expect(harness.list.scrollTop).toBe(840 - CLIENT_HEIGHT);
    });

    it('follows again once the user scrolls back to the bottom', () => {
        const initial = [...history(3), user()];
        harness.addChild(60);
        const { result, rerender } = render(initial);

        harness.list.scrollTop = 0;
        result.current();
        harness.list.scrollTop = harness.list.scrollHeight;
        result.current();

        harness.addChild(1000);
        rerender({ messages: [...initial, assistant()] });

        expect(harness.list.scrollTop).toBe(840);
    });

    it('scrolls to the bottom when a conversation is loaded', () => {
        const { result, rerender } = render(history(1));
        harness.list.scrollTop = 0;
        result.current();

        harness.replaceChildren([60, 2000, 60, 2000]);
        const loaded = [user(), assistant(), user(), assistant()];
        rerender({ messages: loaded });

        expect(harness.list.scrollTop).toBe(4120 - CLIENT_HEIGHT);
    });

    it('treats a shorter message list as a replacement', () => {
        const initial = history(2);
        const { rerender } = render(initial);

        harness.replaceChildren([60, 2000]);
        rerender({ messages: [user(), assistant()] });

        expect(harness.list.scrollTop).toBe(2060 - CLIENT_HEIGHT);
    });

    it('follows the first reply after the conversation is cleared', () => {
        const { result, rerender } = render(history(3));
        harness.list.scrollTop = 0;
        result.current();

        harness.replaceChildren([]);
        rerender({ messages: [] });

        harness.addChild(60);
        const first = [user()];
        rerender({ messages: first });
        harness.addChild(1000);
        rerender({ messages: [...first, assistant()] });

        expect(harness.list.scrollTop).toBe(60);
    });

    it('falls back to the bottom when the reply element is missing', () => {
        const initial = [user()];
        const { rerender } = render(initial);

        rerender({ messages: [...initial, assistant()] });

        expect(harness.list.lastElementChild).toBeNull();
        expect(harness.list.scrollTop).toBe(0);
    });

    it('does nothing without a list element', () => {
        const { result, rerender } = renderHook(
            ({ messages }) => useChatAutoScroll({ current: null }, messages),
            { initialProps: { messages: [user()] } },
        );

        expect(() => {
            result.current();
            rerender({ messages: [user(), assistant()] });
        }).not.toThrow();
    });
});
