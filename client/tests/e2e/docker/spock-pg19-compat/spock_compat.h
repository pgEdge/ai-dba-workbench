/*-------------------------------------------------------------------------
 *
 * VENDORED FOR THE E2E SUITE — spock PG19 RC1 compatibility shim.
 *
 * This is spock's own src/compat/19/spock_compat.h taken from tag
 * v6.0.0-test1 (the only spock ref that ships a compat/19), with three
 * deltas added at the bottom so that spock v5.0.13's sources build and run
 * correctly against PostgreSQL 19 RC1. See the per-delta comments near the
 * end of this file and .claude/notes/spock-pg19-rc1-delete-bug.md for the
 * full rationale. The deltas:
 *   1. RelationFindReplTupleByIndex gained a bool skipduplicates 3rd arg in
 *      PG19 — without this, DELETE replication silently breaks.
 *   2. log_min_messages became a per-backend-type array in PG19.
 *   3. The SPK*ProcessUtility wrappers v5.0.13 uses (dropped from the 6.x
 *      compat base) re-provided against PG19's 8-arg ProcessUtility.
 * Copied into the spock build tree by docker/Dockerfile.pg19. Remove once
 * pgEdge ships a spock release with real PG19 support.
 *
 * Original spock header follows.
 *
 *-------------------------------------------------------------------------
 */

/*-------------------------------------------------------------------------
 *
 * spock_compat.h
 *             compatibility functions (mainly with different PG versions)
 *
 * NOTE:
 * To avoid compilation conflicts follow the rules:
 * 1. Include this file into the *.c-files only.
 * 2. Set it into the last position of the 'include' list.
 *
 * Copyright (c) 2021-2023, pgEdge
 * Portions Copyright (c) 1996-2021, PostgreSQL Global Development Group
 * Portions Copyright (c) 1994, The Regents of the University of California
 *
 *-------------------------------------------------------------------------
 */
#ifndef SPOCK_COMPAT_H
#define SPOCK_COMPAT_H

#include "access/amapi.h"
#include "access/heapam.h"
#include "access/table.h"
#include "access/tableam.h"
#include "utils/varlena.h"


/* Redefine macros with a SUFFIX arg, to be passed-on to PG_TRY/PG_CATCH */
#define PG_ENSURE_ERROR_CLEANUP_SUFFIX(cleanup_function, arg, _suf)	\
	do { \
		before_shmem_exit(cleanup_function, arg); \
		PG_TRY(_suf)

#define PG_END_ENSURE_ERROR_CLEANUP_SUFFIX(cleanup_function, arg, _suf)	\
		cancel_before_shmem_exit(cleanup_function, arg); \
		PG_CATCH(_suf); \
		{ \
			cancel_before_shmem_exit(cleanup_function, arg); \
			cleanup_function (0, arg); \
			PG_RE_THROW(); \
		} \
		PG_END_TRY(_suf); \
	} while (0)

#define WaitLatchOrSocket(latch, wakeEvents, sock, timeout) \
	WaitLatchOrSocket(latch, wakeEvents, sock, timeout, PG_WAIT_EXTENSION)

#define WaitLatch(latch, wakeEvents, timeout) \
	WaitLatch(latch, wakeEvents, timeout, PG_WAIT_EXTENSION)

#define GetCurrentIntegerTimestamp() GetCurrentTimestamp()

#define pg_analyze_and_rewrite(parsetree, query_string, paramTypes, numParams) \
	pg_analyze_and_rewrite_fixedparams(parsetree, query_string, paramTypes, numParams, NULL)

#define CreateCommandTag(raw_parsetree) \
	CreateCommandTag(raw_parsetree->stmt)

#define ExecAlterExtensionStmt(stmt) \
	ExecAlterExtensionStmt(NULL, stmt)

#define ExecBRDeleteTriggers(estate, epqstate, relinfo, tupleid, fdw_trigtuple) \
	ExecBRDeleteTriggers(estate, epqstate, relinfo, tupleid, fdw_trigtuple, NULL, NULL, NULL, false)

#define ExecBRUpdateTriggers(estate, epqstate, relinfo, tupleid, fdw_trigtuple, slot) \
	ExecBRUpdateTriggers(estate, epqstate, relinfo, tupleid, fdw_trigtuple, slot, NULL, NULL, false)

#define Form_pg_sequence Form_pg_sequence_data

#define ExecARUpdateTriggers(estate, relinfo, tupleid, fdw_trigtuple, newslot, recheckIndexes) \
	ExecARUpdateTriggers(estate, relinfo, NULL, NULL, tupleid, fdw_trigtuple, newslot, recheckIndexes, NULL, false)

#define ExecARInsertTriggers(estate, relinfo, slot, recheckIndexes) \
	ExecARInsertTriggers(estate, relinfo, slot, recheckIndexes, NULL)

#define ExecARDeleteTriggers(estate, relinfo, tupleid, fdw_trigtuple) \
	ExecARDeleteTriggers(estate, relinfo, tupleid, fdw_trigtuple, NULL, false)

#define SPKCreateTrigger(stmt, queryString, relOid, refRelOid, constraintOid, indexOid, isInternal) \
	CreateTrigger(stmt, queryString, relOid, refRelOid, constraintOid, indexOid, InvalidOid, InvalidOid, NULL, isInternal, false);

#define	SPKDoCopy(stmt, queryString, processed) \
	do \
	{ \
		ParseState* pstate = make_parsestate(NULL); \
		DoCopy(pstate, stmt, -1, 0, processed); \
		free_parsestate(pstate); \
	} while (false);

#define SPKReplicationSlotCreate(name, db_specific, persistency) ReplicationSlotCreate(name, db_specific, persistency)

#define ACL_OBJECT_RELATION OBJECT_TABLE
#define ACL_OBJECT_SEQUENCE OBJECT_SEQUENCE

#define DatumGetJsonb DatumGetJsonbP

#define spk_heap_attisnull(tup, attnum, tupledesc) \
	heap_attisnull(tup, attnum, tupledesc)

#define getObjectDescription(object) getObjectDescription(object, false)

#define replorigin_session_setup(node) \
	replorigin_session_setup(node, 0)

#define simple_heap_update(relation, otid, tup) \
	do \
	{ \
		TU_UpdateIndexes updateIndexes;	\
		simple_heap_update(relation, otid, tup, &updateIndexes);	\
	} while (false);

#define LogLogicalMessage(_prefix, _message, _size, _transactional) \
	LogLogicalMessage(_prefix, _message, _size, _transactional, false)

/* Must use this interface for access HeapTuple in ReorderBufferChange */
#define ReorderBufferChangeHeapTuple(change, tuple_type) \
	change->data.tp.tuple_type

/*
 * IndexScanInstrumentation parameter may be NULL.  PostgreSQL 19 also added a
 * trailing ScanOptions "flags" argument (commit 1a0ed14f5c5); pass SO_NONE to
 * match core's own logical-replication index scans (execReplication.c).
 */
#define index_beginscan(heapRelation, indexRelation, snapshot, nkeys, norderbys) \
	index_beginscan(heapRelation, indexRelation, snapshot, NULL, nkeys, norderbys, SO_NONE)

/* Deferrable true is ok */
#define RelationGetPrimaryKeyIndex(relation) \
	RelationGetPrimaryKeyIndex(relation, true)

#define ExecInitRangeTable(estate, rangeTable, permInfos) \
	ExecInitRangeTable(estate, rangeTable, permInfos, bms_make_singleton(1))

/* PG19 dropped the separate max_size argument from ShmemInitHash */
#define ShmemInitHash(name, init_size, max_size, infoP, flags) \
	ShmemInitHash(name, init_size, infoP, flags)

/*
 * PostgreSQL 19 renamings.  Spock's source still uses the historical spellings;
 * map them onto the current core names here so the rest of the tree keeps
 * building unchanged.
 *
 * - RepOriginId / InvalidRepOriginId were renamed to ReplOriginId /
 *   InvalidReplOriginId (access/xlogdefs.h, replication/origin.h).
 * - The per-session replication-origin globals (replorigin_session_origin,
 *   _lsn and _timestamp) were folded into the replorigin_xact_state struct.
 * - AssertVariableIsOfType was renamed to StaticAssertVariableIsOfType (c.h).
 */
#define RepOriginId ReplOriginId
#define InvalidRepOriginId InvalidReplOriginId
#define replorigin_session_origin (replorigin_xact_state.origin)
#define replorigin_session_origin_lsn (replorigin_xact_state.origin_lsn)
#define replorigin_session_origin_timestamp (replorigin_xact_state.origin_timestamp)
#define AssertVariableIsOfType StaticAssertVariableIsOfType

/*
 * check_simple_rowfilter_expr() is provided by core on PG19 and exported via
 * commands/publicationcmds.h by the pg19-035-row-filter-check.diff patch, so
 * (unlike PG18) spock does not carry its own copy in spock_compat.c.
 */

/*
 * PG19 removed isQueryUsingTempRelation() (commit 698fa924b11).  Its ad-hoc
 * RangeTblEntry walk was incomplete -- it missed temporary types, regclass
 * constants, etc. -- so it was replaced by the dependency-based
 * query_uses_temp_object(), which is complete by definition and is what core's
 * own CREATE VIEW / materialized-view paths now use.  Spock only needs the
 * boolean answer, so discard the reported object.
 *
 * Note this also widens detection: query_uses_temp_object() flags temp types,
 * regclass constants, etc., not just relations.  So on PG19 a CREATE TABLE AS
 * referencing such an object is skipped from DDL replication where PG<=18 would
 * have replicated it -- in a mixed-version cluster the same statement can thus
 * be treated differently depending on the node's major version.
 */
#include "catalog/dependency.h"
#define isQueryUsingTempRelation(query) \
	query_uses_temp_object((query), &(ObjectAddress){0})


/*
 * PG19 added a bool skipduplicates 3rd argument to RelationFindReplTupleByIndex
 * (executor/executor.h). Spock has a single direct call site
 * (spock_apply_heap.c), which is the primary-key / replica-identity index path;
 * core itself passes idxisreplident there, which is true for a PK/RI index, so
 * inject true. The self-referential macro is not re-expanded (standard C
 * preprocessor rule), and spock_compat.h is included after the core headers, so
 * the real prototype in executor.h is unaffected.
 */
#define RelationFindReplTupleByIndex(rel, idxoid, lockmode, searchslot, outslot) \
	RelationFindReplTupleByIndex(rel, idxoid, true, lockmode, searchslot, outslot)


/*
 * PG19 turned log_min_messages into a per-backend-type array
 * (int log_min_messages[], utils/guc.h), indexed by MyBackendType, which is how
 * core elog.c itself reads it. v5.0.13 source references the threshold via
 * SPOCK_LOG_MIN_MESSAGES (dropped from the 6.x-derived compat/19 base, which no
 * longer uses the macro), so re-provide it in the PG19 array form.
 */
#include "miscadmin.h"
#define SPOCK_LOG_MIN_MESSAGES (log_min_messages[MyBackendType])


/*
 * PG19 ProcessUtility / standard_ProcessUtility keep the PG18 8-arg signature
 * (tcop/utility.h). v5.0.13 source calls these via the SPK* wrappers (which also
 * carry spock's extra sentToRemote arg); the 6.x-derived compat/19 base dropped
 * them, so re-provide them exactly as v5.0.13's compat/18 did.
 */
#define SPKstandard_ProcessUtility(pstmt, queryString, readOnlyTree, context, params, queryEnv, dest, sentToRemote, qc) \
	standard_ProcessUtility(pstmt, queryString, readOnlyTree, context, params, queryEnv, dest, qc)
#define SPKnext_ProcessUtility_hook(pstmt, queryString, readOnlyTree, context, params, queryEnv, dest, sentToRemote, qc) \
	next_ProcessUtility_hook(pstmt, queryString, readOnlyTree, context, params, queryEnv, dest, qc)

#endif
