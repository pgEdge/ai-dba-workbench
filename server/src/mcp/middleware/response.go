/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package middleware

import "encoding/json"

// TextResponse creates a standard MCP text response
func TextResponse(message string) interface{} {
    return map[string]interface{}{
        "content": []map[string]interface{}{
            {
                "type": "text",
                "text": message,
            },
        },
    }
}

// JSONResponse creates a JSON-formatted MCP text response
func JSONResponse(data interface{}) interface{} {
    jsonData, err := json.MarshalIndent(data, "", "  ")
    if err != nil {
        return TextResponse("Error formatting response: " + err.Error())
    }
    return TextResponse(string(jsonData))
}
