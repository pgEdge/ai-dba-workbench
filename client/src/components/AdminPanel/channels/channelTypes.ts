/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import type { ReactNode } from 'react';

/** Fields common to every notification channel. */
export interface BaseChannel {
    id: number;
    name: string;
    description: string;
    enabled: boolean;
    is_estate_default: boolean;
}

/** Definition for an extra column in the channel table. */
export interface ChannelColumnDef<T> {
    label: string;
    render: (channel: T) => ReactNode;
}
