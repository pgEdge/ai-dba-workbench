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
 * Batching for the endpoints that accept a `connection_ids` list.
 *
 * The metrics endpoints fan out per connection, so the server caps the
 * list a single request may name and answers a longer one with a 400
 * ("Too many connection_ids: at most 100 are allowed per request"). The
 * estate dashboard legitimately asks about every server it can see, so
 * the client splits a long list into requests the server will accept and
 * merges the answers rather than letting the dashboard break at the
 * hundred-and-first server.
 */

/**
 * The most connection IDs one request may name.
 *
 * This MUST match `maxConnectionIDsPerRequest` in
 * `server/src/internal/api/request_helpers.go`, which is the authority:
 * that constant carries the reasoning for the figure, and the server
 * rejects anything above it. Deliberately the same number rather than a
 * safety margin below it, so there is a single figure to reason about
 * and the two cannot drift silently. If the server's cap changes, change
 * this in the same commit.
 */
export const MAX_CONNECTION_IDS_PER_REQUEST = 100;

/**
 * Split a list of connection IDs into batches the server will accept.
 *
 * A list at or under the cap yields exactly one batch holding the list
 * itself, so the common case issues exactly the one request it always
 * did. An empty list yields no batches, so a caller mapping over the
 * result makes no request at all.
 *
 * @param ids   The connection IDs to batch.
 * @param size  The maximum batch length; defaults to the server's cap.
 *              Values below one are treated as one, since a zero or
 *              negative batch size would never consume the list.
 */
export const chunkConnectionIds = (
    ids: number[],
    size: number = MAX_CONNECTION_IDS_PER_REQUEST,
): number[][] => {
    const batchSize = size < 1 ? 1 : Math.floor(size);
    const batches: number[][] = [];

    for (let i = 0; i < ids.length; i += batchSize) {
        batches.push(ids.slice(i, i + batchSize));
    }

    return batches;
};
