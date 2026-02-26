import { describe, it, expect } from 'vitest';
import { getToolDisplayName } from '../toolDisplayNames';

describe('getToolDisplayName', () => {
    it('returns "Validating query" for test_query', () => {
        expect(getToolDisplayName('test_query')).toBe('Validating query');
    });

    it('returns "Querying metrics" for query_metrics', () => {
        expect(getToolDisplayName('query_metrics')).toBe('Querying metrics');
    });

    it('returns "Querying database" for query_database', () => {
        expect(getToolDisplayName('query_database')).toBe('Querying database');
    });

    it('returns "Inspecting schema" for get_schema_info', () => {
        expect(getToolDisplayName('get_schema_info')).toBe('Inspecting schema');
    });

    it('returns "Running EXPLAIN" for execute_explain', () => {
        expect(getToolDisplayName('execute_explain')).toBe('Running EXPLAIN');
    });

    it('returns "Counting rows" for count_rows', () => {
        expect(getToolDisplayName('count_rows')).toBe('Counting rows');
    });

    it('returns "Storing memory" for store_memory', () => {
        expect(getToolDisplayName('store_memory')).toBe('Storing memory');
    });

    it('returns "Recalling memories" for recall_memories', () => {
        expect(getToolDisplayName('recall_memories')).toBe('Recalling memories');
    });

    it('returns "Deleting memory" for delete_memory', () => {
        expect(getToolDisplayName('delete_memory')).toBe('Deleting memory');
    });

    it('returns the raw tool name when no mapping exists', () => {
        expect(getToolDisplayName('unknown_tool')).toBe('unknown_tool');
    });

    it('returns the raw tool name for an empty string', () => {
        expect(getToolDisplayName('')).toBe('');
    });
});
