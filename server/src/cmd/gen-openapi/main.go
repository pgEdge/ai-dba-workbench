/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pgedge/ai-workbench/server/internal/api"
)

func main() {
	spec := api.BuildOpenAPISpec()

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling OpenAPI spec: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 {
		if err := os.WriteFile(os.Args[1], data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing file %s: %v\n", os.Args[1], err)
			os.Exit(1)
		}
	} else {
		fmt.Println(string(data))
	}
}
