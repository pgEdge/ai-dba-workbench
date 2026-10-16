/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import { type Page } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';

/**
 * Directory that per-tool chat-response screenshots are written to. It is
 * git-ignored (`screenshots/*.png`) but kept in the tree via a `.gitkeep`,
 * mirroring the filesystem precedent in fixtures/global.setup.ts.
 */
export const SCREENSHOT_DIR = path.join(__dirname, '..', 'screenshots');

/**
 * Comfortable reading width (CSS px) the AI Chat Panel is widened to for
 * capture. The live panel is a NARROW side column (350–800px; see
 * ChatPanel's MIN_WIDTH/MAX_WIDTH), which caused BOTH reported defects:
 *
 *  - TRUNCATION: wide tool output (tables, JSON, resource lists) scrolls
 *    horizontally inside the message list (`overflowX: hidden`) and inside
 *    per-block scrollers (markdown table wrapper `overflowX: auto`, code
 *    `<pre>` `overflow: auto`), so anything past the narrow column was clipped.
 *  - PROPORTIONS: a narrow column makes even a modest response a tall, thin
 *    strip.
 *
 * Widening to a fixed reading width lets wide content reflow into view and
 * gives short replies a sensible landscape frame. 960px sits in the 900–1000px
 * band: wider than the 800px sidebar maximum (so the shot is unmistakably the
 * widened capture, not the un-expanded sidebar) yet comfortably inside the
 * 1280px e2e viewport. Content genuinely wider than this is still captured in
 * full — the captured transcript box grows to its `scrollWidth` (see
 * {@link expandChatPanel}).
 */
export const CAPTURE_WIDTH = 960;

/**
 * Slugify a token for use in a filename: lower-case, collapse any run of
 * non-alphanumerics to a single hyphen, and trim leading/trailing hyphens.
 *
 * Inlined (not imported) from client/src/utils/textHelpers.ts on purpose:
 * the e2e tsconfig rootDir is the e2e package, so reaching across the
 * package boundary into `../../src` is disallowed. This one-liner is the
 * same logic, kept local to the test package.
 */
export function slugify(text: string): string {
    return text
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '');
}

/** Parameters identifying a single tool-response screenshot. */
export interface CaptureToolResponseOptions {
    /** MCP tool name (e.g. `query_database`). */
    tool: string;
    /** Provider slug the tool exercised (reasoning, or embedding). */
    provider: string;
    /** ISO date (YYYY-MM-DD) the run was captured on. */
    date: string;
}

/**
 * Attribute used to stash each element's original inline `style.cssText`
 * while the chat panel is temporarily expanded for capture, so it can be
 * restored exactly afterward.
 */
const SAVED_STYLE_ATTR = 'data-e2e-saved-style';

/**
 * Attribute the chosen capture target (the transcript/message-list element, or
 * the panel root as a fallback) is tagged with during capture so the outer
 * Playwright locator can screenshot exactly that element. Removed on restore.
 */
const CAPTURE_TARGET_ATTR = 'data-e2e-capture-target';

/**
 * Geometry of the CAPTURED element measured in-page right after expansion, so
 * tests can verify the produced PNG framing/size without hardcoded pixels.
 *
 * The captured element is the transcript/message-list region when it can be
 * located (the common case — see {@link expandChatPanel}), otherwise the panel
 * root. `width`/`height` describe that captured element, not the whole panel.
 */
export interface PanelGeometry {
    /**
     * Captured element css width after expansion. The panel is widened to
     * {@link CAPTURE_WIDTH}; the transcript fills that width less the panel's
     * left border and its own padding, and grows beyond it only when a response
     * is wider still (so nothing is truncated horizontally). Always well above
     * the ≤800px live sidebar width.
     */
    width: number;
    /**
     * Full content css height of the captured element — the transcript sized to
     * its own `scrollHeight` (all messages), with NO empty flex filler and NO
     * input composer, so short and tall responses frame identically.
     */
    height: number;
    /** `window.devicePixelRatio` — element screenshots scale css px by this. */
    dpr: number;
    /** `window.innerHeight` (css) — the viewport height at capture time. */
    viewportHeight: number;
    /**
     * True when the transcript/message-list element was located and captured
     * (the intended framing: header excluded, input composer excluded). False
     * only in the fallback where the panel root itself was captured.
     */
    capturedTranscript: boolean;
}

/** Argument bundle for {@link expandChatPanel} (Locator.evaluate takes one). */
interface ExpandArg {
    /** Attribute each element's original inline style is stashed under. */
    savedAttr: string;
    /** Attribute the chosen capture-target element is tagged with. */
    targetAttr: string;
    /** Fixed css width the panel is widened to before capture. */
    captureWidth: number;
}

/** Argument bundle for {@link restoreChatPanel}. */
interface RestoreArg {
    /** Attribute original inline styles were stashed under. */
    savedAttr: string;
    /** Attribute the capture target was tagged with. */
    targetAttr: string;
}

/**
 * Expand the AI Chat Panel so the TRANSCRIPT (message list) paints its entire
 * content in both axes, tag it as the capture target, and return its geometry.
 * Runs in the browser via `Locator.evaluate`; values MUST be passed as the
 * single `arg` because module-scope values are not visible inside `evaluate`.
 *
 * Layout (see client/src/components/ChatPanel/index.tsx and App.tsx):
 *
 *  - ANCESTORS: the panel root (`role="complementary"`) is a flex child of
 *    `mainLayoutBody` (App.tsx `flex: 1; overflow: hidden`) inside
 *    `mainLayoutRoot` (App.tsx `height: 100vh; overflow: hidden`). Those
 *    `overflow: hidden` boxes CLIP any panel content past the 720px viewport,
 *    which is what left tall responses cut at ~viewport height with a blank
 *    tail below. `captureBeyondViewport` does NOT help here: `100vh` stays the
 *    original viewport height and the ancestor still clips its children.
 *  - PANEL: inside the root, a chain of `flex: 1; overflow: hidden` boxes
 *    (panelContent → body → chatArea) holds the `messageListSx` scroller
 *    (`flex: 1; overflowY: auto; overflowX: hidden` — the TRANSCRIPT) followed
 *    by the input composer (`inputAreaSx`, the "Enter to send" area). Wide tool
 *    output additionally sits inside per-block scrollers (the markdown table
 *    wrapper's `overflowX: auto`, code `<pre>` `overflow: auto`).
 *
 * The strategy — capture the transcript, sized to its own content, with every
 * clipping box (ancestors AND the panel subtree) neutralized:
 *
 *  0. LOCATE the transcript BEFORE mutating any `overflow`: it is the unique
 *     panel descendant whose computed `overflow-y` is `auto`/`scroll` AND
 *     `overflow-x` is `hidden` (the table wrapper is `overflowX: auto`, code is
 *     `overflow: auto` on both axes, the other flex boxes are `hidden` on
 *     both). This is the message list holding every user/assistant bubble and
 *     the tool-activity chips.
 *  1. NEUTRALIZE CLIPPING ANCESTORS: walk up to `<html>` and set
 *     `overflow: visible`, `height: auto`, `min-height: 0`, `max-height: none`
 *     on each, so no `100vh` + `overflow: hidden` box clips the enlarged panel.
 *  2. WIDEN + free the ROOT: explicit `captureWidth` (overriding the narrow
 *     sidebar width) plus `flex: none`, `align-self: flex-start`,
 *     `height: auto`, `max-height: none`, `overflow: visible`. The still-`flex:
 *     1` content chain reflows to fill the wide root before any width is read.
 *  3. Free EVERY descendant in BOTH axes, pinning its reflowed width first, so
 *     row-flex children keep the full width, the right-aligned user bubble
 *     keeps its place, and the vertical message-list scroller AND the
 *     horizontal table/JSON/code scrollers all release (content paints in full,
 *     `height: auto` makes each box size to its own content — no empty filler).
 *  4. Pick the capture target: the transcript when found (excludes header and
 *     input composer), else the root. Grow it to its `scrollWidth` (so a table
 *     wider than the panel still fits) and let `height: auto` size it exactly
 *     to its messages (no dead space, no cut). Tag it with `targetAttr`.
 *
 * Every touched element's inline style is saved under `savedAttr` first
 * (ancestors included, so {@link restoreChatPanel} restores them too).
 *
 * @returns the captured element's geometry.
 */
function expandChatPanel(root: Element, arg: ExpandArg): PanelGeometry {
    const { savedAttr, targetAttr, captureWidth } = arg;

    const save = (node: HTMLElement): void => {
        if (!node.hasAttribute(savedAttr)) {
            node.setAttribute(savedAttr, node.style.cssText);
        }
    };

    if (!(root instanceof HTMLElement)) {
        const rect = root.getBoundingClientRect();
        return {
            width: Math.ceil(rect.width),
            height: Math.ceil(rect.height),
            dpr: window.devicePixelRatio,
            viewportHeight: window.innerHeight,
            capturedTranscript: false,
        };
    }
    const rootEl = root;

    // 0. Locate the transcript (message list) BEFORE any overflow is mutated:
    //    the unique descendant with overflow-y auto/scroll and overflow-x
    //    hidden. Its content (all messages + tool chips) is what we frame.
    let transcript: HTMLElement | null = null;
    for (const node of Array.from(rootEl.querySelectorAll('*'))) {
        if (!(node instanceof HTMLElement)) {
            continue;
        }
        const cs = window.getComputedStyle(node);
        if (
            (cs.overflowY === 'auto' || cs.overflowY === 'scroll') &&
            cs.overflowX === 'hidden'
        ) {
            transcript = node;
            break;
        }
    }

    // 1. Neutralize every clipping ancestor up to <html>. The App-shell
    //    mainLayoutBody/mainLayoutRoot boxes are `overflow: hidden` on a
    //    `height: 100vh` column, so without this they clip the enlarged panel
    //    at the viewport boundary (the tall-response cut + blank tail).
    let ancestor = rootEl.parentElement;
    while (ancestor) {
        save(ancestor);
        ancestor.style.overflow = 'visible';
        ancestor.style.height = 'auto';
        ancestor.style.minHeight = '0';
        ancestor.style.maxHeight = 'none';
        ancestor = ancestor.parentElement;
    }

    // 2. Widen + free the panel root. The wide explicit width overrides the
    //    sx-driven narrow sidebar width; the still-`flex: 1` content chain
    //    reflows to fill it before any descendant width is measured in step 3.
    save(rootEl);
    rootEl.style.flex = 'none';
    rootEl.style.alignSelf = 'flex-start';
    rootEl.style.width = `${captureWidth}px`;
    rootEl.style.minWidth = `${captureWidth}px`;
    rootEl.style.maxWidth = 'none';
    rootEl.style.height = 'auto';
    rootEl.style.minHeight = '0';
    rootEl.style.maxHeight = 'none';
    rootEl.style.overflow = 'visible';

    // Force the reflow so descendant measurements below see the wide layout.
    void rootEl.offsetWidth;

    // 3. Free every descendant in BOTH axes, pinning its reflowed width first.
    rootEl.querySelectorAll('*').forEach((node) => {
        if (!(node instanceof HTMLElement)) {
            return;
        }
        save(node);
        const width = node.getBoundingClientRect().width;
        if (width > 0) {
            node.style.width = `${width}px`;
        }
        node.style.flex = 'none';
        node.style.maxWidth = 'none';
        node.style.height = 'auto';
        node.style.minHeight = '0';
        node.style.maxHeight = 'none';
        node.style.overflow = 'visible';
    });

    // 4. Choose the capture target: the transcript (frames just the messages —
    //    no header, no input composer, no empty filler) when found, else the
    //    panel root. Grow its WIDTH to cover any content that paints beyond the
    //    pinned box (a very wide table/JSON line); `height: auto` (step 3)
    //    already sizes it exactly to its messages, so there is no dead tail.
    const target = transcript ?? rootEl;
    target.setAttribute(targetAttr, '');
    if (target.scrollWidth > target.clientWidth) {
        target.style.width = `${target.scrollWidth}px`;
        target.style.minWidth = `${target.scrollWidth}px`;
    }
    // Re-flow after any width change so the height measured below reflects the
    // final wrapping. Defensive height growth for the rare fixed-height case.
    void target.offsetWidth;
    if (target.scrollHeight > target.clientHeight) {
        target.style.height = `${target.scrollHeight}px`;
    }

    const rect = target.getBoundingClientRect();
    return {
        width: Math.max(target.scrollWidth, Math.ceil(rect.width)),
        height: Math.max(target.scrollHeight, Math.ceil(rect.height)),
        dpr: window.devicePixelRatio,
        viewportHeight: window.innerHeight,
        capturedTranscript: transcript !== null,
    };
}

/**
 * Restore every element's original inline style saved by
 * {@link expandChatPanel} and remove the capture-target tag. Document-wide
 * (queries every element carrying `savedAttr`) so nothing is left mutated for
 * the next assertion, interaction, or test — this covers BOTH the panel-local
 * elements AND the neutralized ancestors, all of which carry `savedAttr`.
 * Runs in the browser; the attribute names are passed as an arg.
 */
function restoreChatPanel(arg: RestoreArg): void {
    const { savedAttr, targetAttr } = arg;
    document.querySelectorAll(`[${savedAttr}]`).forEach((node) => {
        if (!(node instanceof HTMLElement)) {
            return;
        }
        const saved = node.getAttribute(savedAttr);
        if (saved !== null) {
            node.style.cssText = saved;
            node.removeAttribute(savedAttr);
        }
    });
    document.querySelectorAll(`[${targetAttr}]`).forEach((node) => {
        node.removeAttribute(targetAttr);
    });
}

/**
 * Capture a well-framed ELEMENT screenshot of the AI Chat Panel containing
 * the FULL response to `filePath`, and return the panel geometry measured
 * during expansion (or `null` when the panel is absent and a full-page
 * fallback was used).
 *
 * {@link expandChatPanel} first locates the AI Chat Panel
 * (`role="complementary"`, name "AI Chat Panel"), NEUTRALIZES the App-shell
 * clipping ancestors (`mainLayoutBody`/`mainLayoutRoot`, which are
 * `100vh` + `overflow: hidden` and otherwise cut tall responses at the viewport
 * boundary), WIDENS the panel to {@link CAPTURE_WIDTH}, frees the subtree in
 * both axes, and tags the TRANSCRIPT (message list) as the capture target so
 * the produced PNG frames just header-less messages sized to their own content
 * — no page chrome, no input composer, no empty tail. Playwright captures the
 * whole tagged element even when it is larger than the viewport. Styles (and
 * the target tag) are always restored in `finally`.
 *
 * Unlike {@link captureToolResponse} this DOES propagate errors, so the
 * verification test can assert on it; the tool-path wrapper swallows them.
 */
export async function captureChatPanel(
    page: Page,
    filePath: string,
): Promise<PanelGeometry | null> {
    fs.mkdirSync(path.dirname(filePath), { recursive: true });

    const panel = page
        .getByRole('complementary', { name: /AI Chat Panel/i })
        .first();

    if ((await panel.count()) === 0) {
        // No panel (rare negative cases) — still capture something.
        await page.screenshot({ path: filePath, fullPage: true });
        return null;
    }

    try {
        const geometry = await panel.evaluate(expandChatPanel, {
            savedAttr: SAVED_STYLE_ATTR,
            targetAttr: CAPTURE_TARGET_ATTR,
            captureWidth: CAPTURE_WIDTH,
        });
        // Screenshot the tagged capture target: the transcript when it was
        // located (the common case), otherwise the panel root as a fallback.
        const captureTarget = geometry.capturedTranscript
            ? page.locator(`[${CAPTURE_TARGET_ATTR}]`).first()
            : panel;
        await captureTarget.screenshot({ path: filePath });
        return geometry;
    } finally {
        // Always restore, even if the capture threw. Guarded so a restore
        // failure cannot itself escape.
        await page
            .evaluate(restoreChatPanel, {
                savedAttr: SAVED_STYLE_ATTR,
                targetAttr: CAPTURE_TARGET_ATTR,
            })
            .catch(() => undefined);
    }
}

/**
 * Capture a screenshot of the ENTIRE current chat response and write it to
 * `screenshots/<tool>_<provider>_<date>.png`, creating the directory if it
 * does not yet exist. Delegates to {@link captureChatPanel} (element
 * screenshot of the expanded panel).
 *
 * This function NEVER throws: it runs in the caller's `finally`, so any
 * throw here would mask the real assertion failure. Internals are wrapped in
 * try/catch and only `console.warn` on error. This resilience is
 * screenshot-only; the test assertions live in the caller's `try` and their
 * failures always propagate.
 *
 * The filename components are slugified so an unexpected tool/provider value
 * can never produce a path-traversing or otherwise invalid filename.
 *
 * @returns The absolute path the screenshot was (attempted to be) written to.
 */
export async function captureToolResponse(
    page: Page,
    options: CaptureToolResponseOptions,
): Promise<string> {
    const fileName =
        `${slugify(options.tool)}_${slugify(options.provider)}_` +
        `${slugify(options.date)}.png`;
    const filePath = path.join(SCREENSHOT_DIR, fileName);

    try {
        await captureChatPanel(page, filePath);
    } catch (err) {
        const reason = err instanceof Error ? err.message : String(err);
        // Screenshot-only resilience: never mask the caller's assertion.
        // eslint-disable-next-line no-console -- intentional diagnostic
        console.warn(
            `[E2E] captureToolResponse failed for ` +
            `${options.tool} (${options.provider}): ${reason}`,
        );
    }

    return filePath;
}
