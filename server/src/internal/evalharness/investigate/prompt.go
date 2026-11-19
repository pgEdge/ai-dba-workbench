/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package investigate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClientPromptFile is the client source that defines Ellie's chat system
// prompt, relative to the repository root.
const ClientPromptFile = "client/src/hooks/chat/chatConstants.ts"

// clientPromptDecl introduces the prompt's template literal.
const clientPromptDecl = "export const SYSTEM_PROMPT = `"

// LoadSystemPrompt reads Ellie's system prompt from path. A .ts file is
// taken to be the client's chatConstants.ts and the SYSTEM_PROMPT
// template literal is extracted from it, so the harness always tests the
// prompt the chat panel ships; any other file is read as the prompt
// text itself, which lets a contributor evaluate a candidate prompt.
func LoadSystemPrompt(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the prompt path comes from the operator
	if err != nil {
		return "", fmt.Errorf("read system prompt: %w", err)
	}
	if filepath.Ext(path) != ".ts" {
		prompt := strings.TrimSpace(string(data))
		if prompt == "" {
			return "", fmt.Errorf("system prompt file %s is empty", path)
		}
		return prompt, nil
	}
	prompt, err := extractTemplateLiteral(string(data), clientPromptDecl)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return prompt, nil
}

// extractTemplateLiteral returns the value of the template literal that
// starts right after decl. It understands the escapes a prompt needs
// (\`, \\ and \$) and refuses ${...} interpolation, which it cannot
// evaluate, rather than silently returning the wrong prompt.
func extractTemplateLiteral(src, decl string) (string, error) {
	start := strings.Index(src, decl)
	if start < 0 {
		return "", errors.New("SYSTEM_PROMPT template literal not found")
	}
	var b strings.Builder
	for i := start + len(decl); i < len(src); i++ {
		switch ch := src[i]; ch {
		case '`':
			return b.String(), nil
		case '\\':
			if i+1 >= len(src) {
				return "", errors.New("SYSTEM_PROMPT ends inside an escape")
			}
			i++
			switch next := src[i]; next {
			case '`', '\\', '$':
				b.WriteByte(next)
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				return "", fmt.Errorf("SYSTEM_PROMPT uses the escape \\%c, which the harness does not support", next)
			}
		case '$':
			if i+1 < len(src) && src[i+1] == '{' {
				return "", errors.New("SYSTEM_PROMPT uses ${...} interpolation, which the harness cannot evaluate; " +
					"pass a plain-text prompt file instead")
			}
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
		}
	}
	return "", errors.New("SYSTEM_PROMPT template literal is not terminated")
}
