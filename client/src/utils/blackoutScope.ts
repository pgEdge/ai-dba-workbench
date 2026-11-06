/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 * Scope helpers shared by the create and schedule blackout dialogs
 *
 *-------------------------------------------------------------------------
 */

import type { Selection } from '../types/selection';

const DB_BACKED_GROUP = /^group-\d+$/;

/**
 * Extract the trailing numeric ID from a prefixed topology ID such as
 * `group-3`, or pass a number straight through.
 */
const extractNumericId = (
    prefixedId: string | number | undefined | null,
): number | undefined => {
    if (prefixedId == null) {return undefined;}
    if (typeof prefixedId === 'number') {return prefixedId;}
    const match = /(\d+)$/.exec(prefixedId);
    return match ? parseInt(match[1], 10) : undefined;
};

/**
 * Report whether a blackout scope can target the current selection.
 * Group and cluster scopes need a database row: a group must have a
 * `group-{id}` ID, and a cluster must carry `dbClusterId`, which
 * auto-detected clusters expose separately from their transient
 * topology ID (issue #809).
 */
export const isScopeAvailable = (
    scopeValue: string,
    selection: Selection | null | undefined,
): boolean => {
    if (!selection) {return scopeValue === 'estate';}

    if (scopeValue === 'estate') {return true;}

    if (scopeValue === 'group') {
        if (selection.type === 'server' || selection.type === 'cluster') {
            const gid = selection.groupId;
            return !!gid && DB_BACKED_GROUP.test(gid);
        }
        return false;
    }

    if (scopeValue === 'cluster') {
        if (selection.type === 'server') {
            return selection.dbClusterId != null && !selection.isStandalone;
        }
        if (selection.type === 'cluster') {
            return selection.dbClusterId != null;
        }
        return false;
    }

    if (scopeValue === 'server') {
        return selection.type === 'server';
    }

    return false;
};

/**
 * Name of the entity a blackout scope would target for the current
 * selection, for display beside the scope option.
 */
export const scopeLabel = (
    scopeValue: string,
    selection: Selection | null | undefined,
): string | undefined => {
    if (!selection) {return undefined;}

    if (scopeValue === 'estate') {return 'All Servers';}

    if (scopeValue === 'group') {
        if (selection.type === 'server' || selection.type === 'cluster') {
            return selection.groupName || undefined;
        }
        return undefined;
    }

    if (scopeValue === 'cluster') {
        if (selection.type === 'server') {
            return selection.clusterName || undefined;
        }
        if (selection.type === 'cluster') {
            return selection.name || undefined;
        }
        return undefined;
    }

    if (scopeValue === 'server') {
        return selection.type === 'server'
            ? selection.name || undefined
            : undefined;
    }

    return undefined;
};

/**
 * Numeric ID to send as connection_id, cluster_id or group_id for a
 * blackout of the given scope on the current selection.
 */
export const resolveEntityId = (
    scopeValue: string,
    selection: Selection | null | undefined,
): number | undefined => {
    if (!selection) {return undefined;}

    if (scopeValue === 'server') {
        if (selection.type === 'server') {
            return extractNumericId(selection.id);
        }
        return undefined;
    }
    if (scopeValue === 'cluster') {
        if (selection.type === 'server' || selection.type === 'cluster') {
            return selection.dbClusterId;
        }
        return undefined;
    }
    if (scopeValue === 'group') {
        if (selection.type === 'server' || selection.type === 'cluster') {
            return extractNumericId(selection.groupId);
        }
        return undefined;
    }
    return undefined;
};
