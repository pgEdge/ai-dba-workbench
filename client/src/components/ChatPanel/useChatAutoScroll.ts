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

const isNearBottom = (list: HTMLElement): boolean =>
    list.scrollHeight - list.scrollTop - list.clientHeight
        <= NEAR_BOTTOM_THRESHOLD_PX;

const scrollToBottom = (list: HTMLElement): void => {
    list.scrollTop = list.scrollHeight;
};

/**
 * Keeps a chat message list scrolled to the right place as messages
 * arrive (issue #629).
 *
 * - A message the user sends, or a conversation being loaded or
 *   cleared, scrolls to the bottom so the latest content is visible.
 * - A new assistant reply that fits in the list also scrolls to the
 *   bottom; one taller than the list scrolls so that the top of the
 *   reply sits at the top of the list (below the list's top padding),
 *   leaving the rest below for the user to read down through.
 * - If the user has scrolled up away from the bottom whilst waiting
 *   for a reply, the reply does not move the view.
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
): (() => void) => {
    // Whether the user was at the bottom of the list the last time it
    // scrolled. Tracked from scroll events rather than measured when a
    // reply arrives, because the thinking indicator shrinks the list
    // whilst a reply is pending without the user scrolling at all.
    const followingRef = useRef(true);
    const previousRef = useRef<readonly ScrollableMessage[]>([]);

    const handleScroll = useCallback(() => {
        const list = listRef.current;
        if (list) {
            followingRef.current = isNearBottom(list);
        }
    }, [listRef]);

    useLayoutEffect(() => {
        const previous = previousRef.current;
        previousRef.current = messages;

        const list = listRef.current;
        if (!list || messages.length === 0) {
            followingRef.current = true;
            return;
        }

        // An append keeps every earlier message object; anything else
        // (loading a conversation, starting a new one) replaces them.
        const appended = messages.length > previous.length
            && (previous.length === 0
                || messages[previous.length - 1]
                    === previous[previous.length - 1]);
        const latest = messages[messages.length - 1];

        if (!appended || latest.role !== 'assistant') {
            scrollToBottom(list);
            followingRef.current = true;
            return;
        }

        if (!followingRef.current) {
            return;
        }

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
    }, [listRef, messages]);

    return handleScroll;
};

export default useChatAutoScroll;
