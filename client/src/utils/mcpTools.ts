/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { apiFetch } from './apiClient';

export interface AnalysisTool {
    name: string;
    description: string;
    inputSchema: {
        type: string;
        properties: Record<string, unknown>;
        required: string[];
    };
}

interface McpToolsResponse {
    tools: Array<{
        name: string;
        description: string;
        inputSchema: {
            type: string;
            properties: Record<string, unknown>;
            required?: string[];
        };
    }>;
}

export async function getKnowledgebaseTool(): Promise<AnalysisTool | null> {
    try {
        const response = await apiFetch('/api/v1/mcp/tools');

        if (!response.ok) {
            return null;
        }

        const data: McpToolsResponse = await response.json();
        const tool = data.tools.find((t) => t.name === 'search_knowledgebase');

        if (!tool) {
            return null;
        }

        return {
            name: tool.name,
            description: tool.description,
            inputSchema: {
                type: tool.inputSchema.type,
                properties: tool.inputSchema.properties,
                required: tool.inputSchema.required ?? [],
            },
        };
    } catch {
        return null;
    }
}
