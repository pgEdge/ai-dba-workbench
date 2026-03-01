/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Non-component re-exports for markdown utilities
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Utility function
export { createCleanTheme } from './markdownUtils';

// SQL detection utilities
export {
    extractExecutableSQL,
    isSqlCodeBlock,
    extractLanguage,
    SQL_KEYWORDS_RE,
    SQL_STATEMENT_KEYWORDS,
} from './sqlDetection';

// Style constants and getters
export {
    sxMonoFont,
    sxH3,
    sxParagraph,
    sxList,
    sxUnorderedList,
    sxStrong,
    sxEm,
    sxConfirmationActions,
    sxContentFadeBox,
    sxErrorFlexRow,
    sxTitleFlexBox,
    sxCloseIconSize,
    sxTitleTypography,
    getHeadingSx,
    sxH1,
    sxH2,
    getInlineCodeSx,
    getCodeBlockWrapperSx,
    getCodeBlockCustomStyle,
    getLinkSx,
    getBlockquoteSx,
    getTableSx,
    getCodeBlockButtonGroupSx,
    getCopyButtonSx,
    getRunButtonSx,
    getQueryResultWrapperSx,
    getQueryResultHeaderSx,
    getQueryErrorSx,
    getConfirmationPanelSx,
    getConfirmationTitleSx,
    getConfirmationTextSx,
    getConfirmationStatementSx,
    getSkeletonBgSx,
    getDialogPaperSx,
    getDialogTitleSx,
    getIconBoxSx,
    getIconColorSx,
    getContentSx,
    getLoadingBannerSx,
    getPulseDotSx,
    getLoadingTextSx,
    getErrorBoxSx,
    getErrorTitleSx,
    getAnalysisBoxSx,
    getFooterSx,
    getDownloadButtonSx,
    getCloseButtonSx,
} from './markdownStyles';

// Type exports
export type { QueryResponse, StatementResult, QueryState } from './RunnableCodeBlock';
