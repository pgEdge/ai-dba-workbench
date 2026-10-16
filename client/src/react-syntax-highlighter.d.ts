/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

/*
 * Ambient declarations for react-syntax-highlighter, which ships no types of
 * its own and has no @types package installed. Only the surface the client
 * actually uses is declared here.
 */

declare module 'react-syntax-highlighter' {
    import type { ComponentType, CSSProperties, ElementType, ReactNode } from 'react';

    /** A Prism/highlight.js style object, keyed by token selector. */
    export type SyntaxHighlighterStyle = Record<string, unknown>;

    export interface SyntaxHighlighterProps {
        children?: ReactNode;
        language?: string;
        style?: SyntaxHighlighterStyle;
        customStyle?: CSSProperties;
        codeTagProps?: Record<string, unknown>;
        useInlineStyles?: boolean;
        showLineNumbers?: boolean;
        startingLineNumber?: number;
        lineNumberStyle?: CSSProperties | ((lineNumber: number) => CSSProperties);
        wrapLines?: boolean;
        wrapLongLines?: boolean;
        lineProps?: Record<string, unknown> | ((lineNumber: number) => Record<string, unknown>);
        renderer?: (props: Record<string, unknown>) => ReactNode;
        PreTag?: ElementType;
        CodeTag?: ElementType;
        className?: string;
    }

    export const Prism: ComponentType<SyntaxHighlighterProps>;
    export const Light: ComponentType<SyntaxHighlighterProps>;
    export const LightAsync: ComponentType<SyntaxHighlighterProps>;
    export const PrismAsync: ComponentType<SyntaxHighlighterProps>;
    export const PrismAsyncLight: ComponentType<SyntaxHighlighterProps>;
    export const PrismLight: ComponentType<SyntaxHighlighterProps>;

    const SyntaxHighlighter: ComponentType<SyntaxHighlighterProps>;
    export default SyntaxHighlighter;
}

declare module 'react-syntax-highlighter/dist/esm/styles/prism' {
    import type { SyntaxHighlighterStyle } from 'react-syntax-highlighter';

    export const oneDark: SyntaxHighlighterStyle;
    export const oneLight: SyntaxHighlighterStyle;

    const styles: Record<string, SyntaxHighlighterStyle>;
    export default styles;
}
