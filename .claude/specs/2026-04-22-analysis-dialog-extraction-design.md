# Analysis Dialog Extraction

## Summary

Extract a shared `BaseAnalysisDialog` component from the four
analysis dialog components (`AlertAnalysisDialog`,
`QueryAnalysisDialog`, `ServerAnalysisDialog`,
`ChartAnalysisDialog`). Each dialog shares 95% of its chrome
but differs in toolbar content, download logic, and hook
integration. This is the second of several PRs addressing
issue #80.

## Problem

The four analysis dialogs total 1,595 lines and share
identical UI scaffolding:

- Full-screen `Dialog` with `SlideTransition`.
- `AppBar` with close button, icon box, spacer, download
  button.
- Content area with scrollbar styling, `Fade` wrapper, and
  max-width container.
- Loading UI: banner, pulsing dot, progress message, tool
  badge loop, `AnalysisSkeleton`.
- Error UI: error box with icon and message.
- Analysis content: `MarkdownContent` rendering.

Each dialog reimplements this scaffolding (~250 lines) with
only ~100-150 lines of unique toolbar content, download logic,
and hook wiring.

## Design

### New Component: `BaseAnalysisDialog`

Create `client/src/components/shared/BaseAnalysisDialog.tsx`
that encapsulates the shared chrome and accepts props for
the unique parts.

#### Props Interface

```typescript
interface BaseAnalysisDialogProps {
    open: boolean;
    onClose: () => void;
    title: string;
    icon: React.ReactNode;
    toolLabels: string[];
    analysis: string | null;
    loading: boolean;
    error: string | null;
    progressMessage: string;
    activeTools: string[];
    onDownload: () => void;
    toolbarContent?: React.ReactNode;
    markdownContentProps: {
        isDark: boolean;
        connectionId?: number;
        databaseName?: string;
        serverName?: string;
        connectionMap?: Map<number, string>;
    };
}
```

#### Responsibilities

The `BaseAnalysisDialog` renders:

- The `Dialog` wrapper (fullScreen, SlideTransition).
- The `AppBar` with: close button, icon box, title text,
  `toolbarContent` slot, spacer, download button.
- The scrollable content area with consistent scrollbar
  styling.
- The `Fade` wrapper with max-width container.
- Conditional rendering of loading, error, and analysis
  states.
- Tool badge loop for loading state.
- `MarkdownContent` with forwarded props for analysis state.

#### What Callers Provide

Each dialog becomes a thin wrapper (~80-100 lines) that:

- Manages its own hook (`useAlertAnalysis`, etc.).
- Provides `toolLabels` as a constant array.
- Provides `toolbarContent` as JSX (badges, pills,
  severity indicators).
- Provides `onDownload` callback that builds dialog-specific
  markdown content.
- Provides `markdownContentProps` (connectionId, etc.).
- Provides `icon` (PsychologyIcon with optional severity
  dot, type-specific icon, etc.).
- Manages its own `useEffect` trigger logic.

### Files Modified

| File | Change |
|------|--------|
| `shared/BaseAnalysisDialog.tsx` | New shared component (~250 lines) |
| `AlertAnalysisDialog.tsx` | Refactor to use BaseAnalysisDialog (~100 lines, from 447) |
| `QueryAnalysisDialog.tsx` | Refactor to use BaseAnalysisDialog (~100 lines, from 444) |
| `ServerAnalysisDialog.tsx` | Refactor to use BaseAnalysisDialog (~90 lines, from 340) |
| `ChartAnalysisDialog.tsx` | Refactor to use BaseAnalysisDialog (~90 lines, from 364) |

### What Does Not Change

- Hook implementations (`useAlertAnalysis`, etc.) are
  untouched.
- `MarkdownContent`, `MarkdownExports`, `SlideTransition`,
  `analysisStyles`, `downloadMarkdown` are untouched.
- `useAnalysisState` is untouched.
- No behavior changes; this is a pure refactor.

## Testing

- Add unit tests for `BaseAnalysisDialog` in
  `shared/__tests__/BaseAnalysisDialog.test.tsx`.
- Test rendering in loading, error, and analysis states.
- Test that close and download callbacks fire correctly.
- Test that `toolbarContent` slot renders.
- Test tool badge rendering with active tools.
- Verify 90% line coverage on `BaseAnalysisDialog`.
- Existing hook tests must continue to pass unchanged.
- Run `make test-all` before completing.

## Out of Scope

- Hook refactoring or consolidation.
- Typed Selection discriminated union (future PR).
- Oversized component decomposition (future PR).
