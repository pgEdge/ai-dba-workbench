# Fix Clipboard Copy in Token Dialog

## Problem

Issue #71: the copy button on the token-created dialog does not
copy the token value to the clipboard.

The `copyToClipboard` utility appends a temporary textarea to
`document.body` when the modern Clipboard API is unavailable
(non-secure HTTP contexts). MUI `<Dialog>` enforces a focus trap
that prevents focus from leaving the dialog portal. The textarea's
`select()` call fails because focus cannot move outside the
dialog, so `execCommand('copy')` returns `false` and throws.

## Solution

Add an optional `container` parameter to `copyToClipboard`. When
provided, the temporary textarea is appended to the container
element instead of `document.body`. This keeps the textarea inside
the dialog's focus trap, allowing `select()` and
`execCommand('copy')` to succeed.

The modern Clipboard API path (secure contexts) is unaffected.

## File Changes

### `client/src/utils/clipboard.ts`

Add an optional second parameter `container?: HTMLElement` to
`copyToClipboard`. In the fallback path, append the textarea to
`container ?? document.body` instead of `document.body`.

```typescript
export async function copyToClipboard(
    text: string,
    container?: HTMLElement
): Promise<void> {
    if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
        return;
    }

    const target = container ?? document.body;
    const textarea = document.createElement('textarea');
    textarea.value = text;
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
                'Clipboard API unavailable and '
                + 'execCommand("copy") failed.'
            );
        }
    } finally {
        target.removeChild(textarea);
    }
}
```

### `client/src/components/AdminPanel/AdminTokenScopes.tsx`

Add a `useRef<HTMLDivElement>(null)` ref for the dialog content
area. Attach the ref to the `<DialogContent>` element of the
token-created dialog. Pass `ref.current` as the second argument
to `copyToClipboard` in `handleCopyToken`. Update the
`useCallback` dependency array to include the ref if needed.

### `client/src/utils/__tests__/clipboard.test.ts`

Add test cases for the `container` parameter:

- When a container is provided, the textarea is appended to and
  removed from that container instead of `document.body`.
- When `execCommand` fails with a container, the textarea is
  still cleaned up from the container.

### `client/src/components/AdminPanel/__tests__/AdminTokenScopes.test.tsx`

Verify that the copy button works correctly in the dialog
context. Existing tests that mock `navigator.clipboard.writeText`
remain valid since the modern path is unchanged. Add or adjust
tests for the fallback path if coverage requires it.

## Scope

Four files touched. The change is backward-compatible; the new
parameter is optional and defaults to `document.body`. No changes
to focus trap behavior or accessibility. All existing callers of
`copyToClipboard` (e.g., `CopyCodeButton`) continue to work
without modification.
