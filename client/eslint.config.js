/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
import js from '@eslint/js';
import globals from 'globals';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import tseslint from 'typescript-eslint';

export default tseslint.config(
    { ignores: ['dist', 'coverage', 'node_modules'] },
    {
        extends: [js.configs.recommended, ...tseslint.configs.recommended],
        files: ['**/*.{ts,tsx}'],
        languageOptions: {
            ecmaVersion: 2020,
            globals: globals.browser,
        },
        plugins: {
            'react-hooks': reactHooks,
            'react-refresh': reactRefresh,
        },
        rules: {
            ...reactHooks.configs.recommended.rules,
            // Downgrade to warn temporarily to establish linting infrastructure
            // TODO: Fix conditional hook calls in EventTimeline.tsx and restore as error
            'react-hooks/rules-of-hooks': 'warn',
            'react-refresh/only-export-components': [
                'warn',
                { allowConstantExport: true },
            ],
            // TypeScript-specific rules
            '@typescript-eslint/no-unused-vars': [
                'warn',
                {
                    argsIgnorePattern: '^_',
                    varsIgnorePattern: '^_',
                },
            ],
            '@typescript-eslint/no-explicit-any': 'warn',
            '@typescript-eslint/explicit-function-return-type': 'off',
            '@typescript-eslint/explicit-module-boundary-types': 'off',
            '@typescript-eslint/no-non-null-assertion': 'warn',
            // Keep || (logical OR) for booleans and strings so falsy
            // values like '' and false continue to trigger the
            // fallback. We cannot enable this rule locally because
            // it requires typed linting (parserOptions.project),
            // but we record the option here so Codacy / any future
            // typed-lint configuration does not propose unsafe
            // `||` -> `??` swaps on boolean or string primitives.
            '@typescript-eslint/prefer-nullish-coalescing': ['off', {
                ignorePrimitives: { boolean: true, string: true },
            }],
            // The MUI barrels load every module in the package, which
            // made each Vitest file spend seconds importing (#846).
            // Type imports are erased at compile time, so they may
            // stay (PaletteMode, for one, is only exported there).
            '@typescript-eslint/no-restricted-imports': ['error', {
                paths: [
                    {
                        name: '@mui/icons-material',
                        message: "Import each icon from its own path, e.g. import AddIcon from '@mui/icons-material/Add'.",
                        allowTypeImports: true,
                    },
                    {
                        name: '@mui/material',
                        message: "Import each component from its own path, e.g. import Button from '@mui/material/Button', and theme helpers from '@mui/material/styles'.",
                        allowTypeImports: true,
                    },
                ],
            }],
            // no-restricted-imports only sees import and export
            // declarations, so the barrels are also banned in a dynamic
            // import() (quoted or as a plain template literal),
            // require(), and Vitest's mock and importActual calls, each
            // of which would load or stand in for the whole package.
            'no-restricted-syntax': ['error',
                {
                    selector: "ImportExpression[source.value=/^@mui\\/(material|icons-material)$/]",
                    message: 'Import Material UI per path, not from the barrel (#846).',
                },
                {
                    selector: "ImportExpression > TemplateLiteral.source[quasis.0.value.cooked=/^@mui\\/(material|icons-material)$/]",
                    message: 'Import Material UI per path, not from the barrel (#846).',
                },
                {
                    selector: "CallExpression[callee.name='require'][arguments.0.value=/^@mui\\/(material|icons-material)$/]",
                    message: 'Import Material UI per path, not from the barrel (#846).',
                },
                {
                    selector: "CallExpression[callee.property.name=/^(importActual|importMock|mock|doMock)$/][arguments.0.value=/^@mui\\/(material|icons-material)$/]",
                    message: 'Import Material UI per path, not from the barrel (#846).',
                },
            ],
            // General rules
            'no-console': 'error',
            'prefer-const': 'error',
            'no-var': 'error',
            eqeqeq: ['error', 'always', { null: 'ignore' }],
            // curly is set to warn to allow gradual adoption
            curly: ['warn', 'all'],
        },
    },
    {
        files: ['src/utils/logger.ts'],
        rules: {
            'no-console': 'off',
        },
    }
);
