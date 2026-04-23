# Token Dialog Copy Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the broken copy-to-clipboard button on the
token-created dialog (issue #71).

**Architecture:** Add an optional `container` parameter to the
`copyToClipboard` utility so the execCommand fallback textarea
is appended inside the MUI Dialog's focus trap instead of
`document.body`. Pass a ref from the dialog's `<DialogContent>`
as the container.

**Tech Stack:** React, TypeScript, MUI, Vitest

---

## Task 1: Add container parameter to clipboard utility — tests

**Files:**

- Modify: `client/src/utils/__tests__/clipboard.test.ts`

- [ ] **Step 1: Write failing tests for the container parameter**

Add a new `describe` block inside the existing
`'when the Clipboard API is unavailable'` section. These tests
verify the textarea is appended to and cleaned up from a custom
container element rather than `document.body`.

```typescript
describe('with a custom container', () => {
    let container: HTMLDivElement;
    let containerAppendSpy: ReturnType<typeof vi.fn>;
    let containerRemoveSpy: ReturnType<typeof vi.fn>;

    beforeEach(() => {
        container = document.createElement('div');
        document.body.appendChild(container);
        containerAppendSpy = vi.spyOn(
            container, 'appendChild'
        ) as unknown as ReturnType<typeof vi.fn>;
        containerRemoveSpy = vi.spyOn(
            container, 'removeChild'
        ) as unknown as ReturnType<typeof vi.fn>;
    });

    afterEach(() => {
        document.body.removeChild(container);
    });

    it('appends the textarea to the container instead of body',
        async () => {
            await copyToClipboard('container text', container);

            expect(containerAppendSpy).toHaveBeenCalledTimes(1);
            expect(containerRemoveSpy).toHaveBeenCalledTimes(1);

            const textarea = containerAppendSpy.mock
                .calls[0][0] as HTMLTextAreaElement;
            expect(textarea.tagName).toBe('TEXTAREA');
            expect(textarea.value).toBe('container text');

            // document.body should NOT have been touched.
            expect(appendChildSpy).not.toHaveBeenCalled();
            expect(removeChildSpy).not.toHaveBeenCalled();

            expect(execCommandMock).toHaveBeenCalledWith('copy');
        }
    );

    it('cleans up the textarea from the container on failure',
        async () => {
            execCommandMock.mockImplementation(() => {
                throw new Error('boom');
            });

            await expect(
                copyToClipboard('cleanup', container)
            ).rejects.toThrow('boom');

            expect(containerRemoveSpy).toHaveBeenCalledTimes(1);
        }
    );
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /workspaces/ai-dba-workbench/client && npx vitest run src/utils/__tests__/clipboard.test.ts`

Expected: FAIL — `copyToClipboard` does not accept a second
argument yet, so the textarea still appends to `document.body`
and the `containerAppendSpy` assertion fails.

---

## Task 2: Add container parameter to clipboard utility — implementation

**Files:**

- Modify: `client/src/utils/clipboard.ts:22-52`

- [ ] **Step 1: Update the function signature and fallback path**

Replace the existing `copyToClipboard` function body (lines
22–52) with the version that accepts an optional `container`
parameter:

```typescript
export async function copyToClipboard(
    text: string,
    container?: HTMLElement
): Promise<void> {
    // Prefer the modern Clipboard API when available.
    if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
        return;
    }

    // Fallback: create a temporary textarea, select its content, and
    // invoke the deprecated execCommand('copy').
    // When a container is supplied (e.g. inside a dialog portal) the
    // textarea is appended there so it remains within any active focus
    // trap; otherwise it falls back to document.body.
    const target = container ?? document.body;
    const textarea = document.createElement('textarea');
    textarea.value = text;

    // Keep the element invisible and out of the layout flow.
    textarea.style.position = 'fixed';
    textarea.style.left = '-9999px';
    textarea.style.top = '-9999px';
    textarea.style.opacity = '0';

    target.appendChild(textarea);
    try {
        textarea.select();
        const ok = document.execCommand('copy');
        if (!ok) {
            throw new Error(
                'Clipboard API unavailable and execCommand("copy") failed.'
            );
        }
    } finally {
        target.removeChild(textarea);
    }
}
```

Also update the JSDoc `@param` block above the function to
document the new parameter:

```typescript
/**
 * Copy text to the clipboard.
 *
 * Tries the modern Clipboard API first (requires a secure context). When
 * that is unavailable (plain HTTP), falls back to the legacy
 * `document.execCommand('copy')` approach with a temporary textarea.
 *
 * @param text - The string to place on the clipboard.
 * @param container - Optional DOM element to append the temporary textarea
 *                    to. Useful when calling from within a focus-trapped
 *                    dialog; defaults to `document.body`.
 * @returns A promise that resolves on success.
 * @throws On failure so callers can report the error to the user.
 */
```

- [ ] **Step 2: Run clipboard tests to verify they pass**

Run: `cd /workspaces/ai-dba-workbench/client && npx vitest run src/utils/__tests__/clipboard.test.ts`

Expected: All tests PASS, including the new container tests from
Task 1.

- [ ] **Step 3: Commit**

```bash
git add client/src/utils/clipboard.ts \
       client/src/utils/__tests__/clipboard.test.ts
git commit -m "fix: add container parameter to copyToClipboard for dialog focus traps (#71)"
```

---

## Task 3: Pass dialog ref from AdminTokenScopes — tests

**Files:**

- Modify: `client/src/components/AdminPanel/__tests__/AdminTokenScopes.test.tsx`

No new tests are needed for the `AdminTokenScopes` component.
The existing tests mock `navigator.clipboard.writeText` so they
exercise the modern Clipboard API path, which is unchanged. The
ref wiring is an internal implementation detail verified by the
clipboard utility tests (Task 1) and by manual testing. The
existing tests already confirm the copy button calls
`copyToClipboard` and renders the correct feedback.

- [ ] **Step 1: Run the existing AdminTokenScopes tests**

Run: `cd /workspaces/ai-dba-workbench/client && npx vitest run src/components/AdminPanel/__tests__/AdminTokenScopes.test.tsx`

Expected: All existing tests PASS (nothing has changed in this
file yet).

---

## Task 4: Pass dialog ref from AdminTokenScopes — implementation

**Files:**

- Modify: `client/src/components/AdminPanel/AdminTokenScopes.tsx:199-203,511-516,1027`

- [ ] **Step 1: Add a ref for the dialog content area**

After line 203 (`const copyResetTimerRef = ...`), add:

```typescript
const createdDialogContentRef = useRef<HTMLDivElement>(null);
```

- [ ] **Step 2: Pass the ref as the container argument**

In `handleCopyToken` (line 516), change:

```typescript
await copyToClipboard(createdToken);
```

to:

```typescript
await copyToClipboard(
    createdToken,
    createdDialogContentRef.current ?? undefined
);
```

Note: `useRef.current` is `null` when the dialog is not mounted;
we convert to `undefined` so the utility falls back to
`document.body`. This is defensive only — the ref will always
be populated when the dialog is open.

- [ ] **Step 3: Attach the ref to DialogContent**

On line 1027, change:

```tsx
<DialogContent>
```

to:

```tsx
<DialogContent ref={createdDialogContentRef}>
```

- [ ] **Step 4: Run the AdminTokenScopes tests**

Run: `cd /workspaces/ai-dba-workbench/client && npx vitest run src/components/AdminPanel/__tests__/AdminTokenScopes.test.tsx`

Expected: All tests PASS.

- [ ] **Step 5: Run the full client test suite**

Run: `cd /workspaces/ai-dba-workbench/client && npx vitest run`

Expected: All tests PASS.

- [ ] **Step 6: Commit**

```bash
git add client/src/components/AdminPanel/AdminTokenScopes.tsx
git commit -m "fix: wire dialog content ref to copyToClipboard container (#71)"
```

---

## Task 5: Final verification

- [ ] **Step 1: Run make test-all from the repository root**

Run: `cd /workspaces/ai-dba-workbench && make test-all`

Expected: All sub-project test suites pass.

- [ ] **Step 2: Run client coverage check**

Run: `cd /workspaces/ai-dba-workbench/client && make coverage`

Expected: `clipboard.ts` and `AdminTokenScopes.tsx` both meet
the 90% line coverage floor.

- [ ] **Step 3: Run linter**

Run: `cd /workspaces/ai-dba-workbench/client && npx eslint src/utils/clipboard.ts src/components/AdminPanel/AdminTokenScopes.tsx`

Expected: No errors or warnings.
