/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { RefObject } from 'react';
import { useCallback, useLayoutEffect, useRef } from 'react';

/**
 * How close to the bottom of the message list, in pixels, the view must
 * be for the user to count as following the conversation. A small
 * allowance absorbs sub-pixel rounding and the last few pixels of a
 * scroll gesture.
 */
export const NEAR_BOTTOM_THRESHOLD_PX = 48;

/** The only part of a chat message that the scrolling logic reads. */
export interface ScrollableMessage {
    role: string;
}

/** Where the list should scroll to after its messages change. */
type ScrollAction = 'bottom' | 'reply';

const isNearBottom = (list: HTMLElement): boolean =>
    list.scrollHeight - list.scrollTop - list.clientHeight
        <= NEAR_BOTTOM_THRESHOLD_PX;

const scrollToBottom = (list: HTMLElement): void => {
    list.scrollTop = list.scrollHeight;
};

/**
 * Scrolls so that the list's newest message, a reply, starts at the top
 * of the list if it is taller than the list, or to the bottom if it fits.
 */
const scrollToReply = (list: HTMLElement): void => {
    const reply = list.lastElementChild;
    const replyRect = reply?.getBoundingClientRect();
    if (!replyRect || replyRect.height <= list.clientHeight) {
        scrollToBottom(list);
        return;
    }

    // Leave the list's own top padding above the reply, so it sits
    // where the first message of a conversation does.
    const paddingTop =
        parseFloat(getComputedStyle(list).paddingTop) || 0;
    list.scrollTop +=
        replyRect.top - list.getBoundingClientRect().top - paddingTop;
};

/**
 * Keeps a chat message list scrolled to the right place as messages
 * arrive (issue #629).
 *
 * - A message the user sends, or a conversation being loaded (into an
 *   empty list or over another) or cleared, scrolls to the bottom so
 *   the latest content is visible.
 * - A new assistant reply that fits in the list also scrolls to the
 *   bottom; one taller than the list scrolls so that the top of the
 *   reply sits at the top of the list (below the list's top padding),
 *   leaving the rest below for the user to read down through.
 * - If the user has scrolled up away from the bottom whilst waiting
 *   for a reply, the reply does not move the view.
 * - Whilst `active` is false (the chat panel is closed, leaving the
 *   list mounted but almost no width), scroll events are ignored and
 *   any scroll is deferred until `active` turns true, because the list
 *   cannot be measured meaningfully until it has its real width.
 *
 * Replies arrive whole rather than streamed, and the thinking and tool
 * progress indicators are rendered outside the message list, so the
 * list changes once per reply and the scroll happens once per reply.
 *
 * The list's last element child must be the newest message's root
 * element. Returns a scroll handler to attach to the list's `onScroll`.
 */
const useChatAutoScroll = (
    listRef: RefObject<HTMLElement | null>,
    messages: readonly ScrollableMessage[],
    active = true,
): (() => void) => {
    // Whether the user was at the bottom of the list the last time it
    // scrolled. Tracked from scroll events rather than measured when a
    // reply arrives, because the thinking indicator shrinks the list
    // whilst a reply is pending without the user scrolling at all.
    const followingRef = useRef(true);
    const previousRef = useRef<readonly ScrollableMessage[]>([]);
    const activeRef = useRef(active);
    activeRef.current = active;
    // A scroll held back whilst the list was inactive.
    const pendingRef = useRef<ScrollAction | null>(null);

    const handleScroll = useCallback(() => {
        const list = listRef.current;
        if (list && activeRef.current) {
            followingRef.current = isNearBottom(list);
        }
    }, [listRef]);

    useLayoutEffect(() => {
        const previous = previousRef.current;
        if (messages === previous) {
            return;
        }
        previousRef.current = messages;

        const list = listRef.current;
        if (!list || messages.length === 0) {
            followingRef.current = true;
            pendingRef.current = null;
            return;
        }

        // An append adds a single message and keeps every earlier
        // message object; anything else (loading a conversation, even
        // into an empty list, or starting a new one) replaces them.
        const appended = messages.length === previous.length + 1
            && (previous.length === 0
                || messages[previous.length - 1]
                    === previous[previous.length - 1]);
        const latest = messages[messages.length - 1];

        let action: ScrollAction | null = null;
        if (!appended || latest.role !== 'assistant') {
            action = 'bottom';
            followingRef.current = true;
        } else if (followingRef.current) {
            action = 'reply';
        }

        if (!activeRef.current) {
            pendingRef.current = action;
        } else if (action === 'bottom') {
            scrollToBottom(list);
        } else if (action === 'reply') {
            scrollToReply(list);
        }
    }, [listRef, messages]);

    useLayoutEffect(() => {
        const list = listRef.current;
        const action = pendingRef.current;
        if (!active || !list || !action) {
            return;
        }
        pendingRef.current = null;
        if (action === 'bottom') {
            scrollToBottom(list);
        } else {
            scrollToReply(list);
        }
    }, [listRef, active]);

    return handleScroll;
};

export default useChatAutoScroll;
