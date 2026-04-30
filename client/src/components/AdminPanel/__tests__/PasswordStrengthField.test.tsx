/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

import React, { useState } from 'react';
import { screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {
    describe,
    it,
    expect,
    vi,
    beforeEach,
    afterEach,
} from 'vitest';
import renderWithTheme from '../../../test/renderWithTheme';
import PasswordStrengthField from '../PasswordStrengthField';
import {
    PASSWORD_MIN_LENGTH,
    PASSWORD_MAX_LENGTH,
    scorePasswordStrength,
} from '../passwordStrength';

/**
 * Tiny controlled wrapper used to drive the field through user
 * interactions. The wrapper forwards the validity callback prop so tests
 * can assert on the values reported by the field.
 */
type HarnessProps = {
    initialValue?: string;
    hideFeedbackWhenEmpty?: boolean;
    onValidityChange?: (info: {
        meetsMinimum: boolean;
        isEmpty: boolean;
        strength: number;
        tooLong: boolean;
        byteLength: number;
    }) => void;
    helperText?: React.ReactNode;
    label?: string;
    required?: boolean;
    error?: boolean;
    inputProps?: Record<string, unknown>;
};

const Harness: React.FC<HarnessProps> = ({
    initialValue = '',
    hideFeedbackWhenEmpty,
    onValidityChange,
    helperText,
    label = 'Password',
    required,
    error,
    inputProps,
}) => {
    const [value, setValue] = useState(initialValue);
    return (
        <PasswordStrengthField
            label={label}
            value={value}
            onChange={setValue}
            hideFeedbackWhenEmpty={hideFeedbackWhenEmpty}
            onValidityChange={onValidityChange}
            helperText={helperText}
            required={required}
            error={error}
            inputProps={inputProps}
        />
    );
};

const getInput = (): HTMLInputElement => {
    const input = screen.getByLabelText(/password/i);
    return input as HTMLInputElement;
};

describe('scorePasswordStrength', () => {
    it('returns 0 for empty values', () => {
        expect(scorePasswordStrength('')).toBe(0);
    });

    it('returns 0 for values shorter than the minimum', () => {
        expect(scorePasswordStrength('shorty11')).toBe(0);
        expect(scorePasswordStrength('eleven_chars')).not.toBe(0);
    });

    it('penalises trivial repeat-character runs', () => {
        // 12 lowercase chars all the same has only the run penalty,
        // which keeps the score in the fair bucket rather than strong.
        expect(scorePasswordStrength('aaaaaaaaaaaa')).toBeLessThanOrEqual(2);
        expect(
            scorePasswordStrength('aaaaaaaaaaaa')
        ).toBeLessThan(scorePasswordStrength('Mxz9!Pq3@Lk7'));
    });

    it('penalises monotonic sequences', () => {
        // 12 sequential chars trigger several sequence penalties and
        // collapse into the lowest non-zero bucket.
        expect(scorePasswordStrength('abcdefghijkl')).toBe(1);
    });

    it('rates a mixed-case password as fair or better', () => {
        const score = scorePasswordStrength('Tr0ub4dor&3xq');
        expect(score).toBeGreaterThanOrEqual(2);
    });

    it('rates a long, varied password as strong', () => {
        const score = scorePasswordStrength(
            'Correct-Horse-Battery-Staple-99!'
        );
        expect(score).toBe(4);
    });

    it('treats non-ASCII characters as part of the symbol class', () => {
        // The smiley character is non-ASCII; it should fall into the
        // symbol bucket and bump entropy beyond a 12-letter password.
        const baseline = scorePasswordStrength('abcdefghijklm');
        const withSymbol = scorePasswordStrength('abcdefghijkl☺');
        expect(withSymbol).toBeGreaterThanOrEqual(baseline);
    });
});

describe('PasswordStrengthField', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    afterEach(() => {
        vi.restoreAllMocks();
    });

    it('renders the policy hint when empty', () => {
        renderWithTheme(<Harness />);
        expect(
            screen.getByText(
                /at least 12 characters\. avoid common passwords/i
            )
        ).toBeInTheDocument();
        expect(
            screen.queryByRole('progressbar', { name: /password strength/i })
        ).not.toBeInTheDocument();
    });

    it('hides the policy hint when hideFeedbackWhenEmpty is set', () => {
        renderWithTheme(<Harness hideFeedbackWhenEmpty />);
        expect(
            screen.queryByText(
                /at least 12 characters\. avoid common passwords/i
            )
        ).not.toBeInTheDocument();
    });

    it('shows an error helper when the value is below the minimum', async () => {
        const user = userEvent.setup({ delay: null });
        renderWithTheme(<Harness />);
        const input = getInput();
        await user.type(input, 'short');
        expect(
            screen.getByText(/password is 5 of 12 minimum characters/i)
        ).toBeInTheDocument();
        // The TextField has its error state set via a CSS class.
        const root = input.closest('.MuiFormControl-root');
        expect(root).not.toBeNull();
        expect(root?.querySelector('.Mui-error')).not.toBeNull();
    });

    it('exposes type=password on the underlying input', () => {
        renderWithTheme(<Harness />);
        const input = getInput();
        expect(input.type).toBe('password');
    });

    it('caps input length at the bcrypt limit', () => {
        renderWithTheme(<Harness />);
        const input = getInput();
        expect(input.maxLength).toBe(PASSWORD_MAX_LENGTH);
    });

    it('switches to OK feedback at exactly the minimum length', async () => {
        const user = userEvent.setup({ delay: null });
        renderWithTheme(<Harness />);
        const input = getInput();
        await user.type(input, 'a'.repeat(PASSWORD_MIN_LENGTH));
        // The error helper should be gone and the strength meter shown.
        expect(
            screen.queryByText(/minimum characters/i)
        ).not.toBeInTheDocument();
        expect(
            screen.getByText(
                new RegExp(`${PASSWORD_MIN_LENGTH} / ${PASSWORD_MIN_LENGTH} characters`)
            )
        ).toBeInTheDocument();
        const meter = screen.getByRole('progressbar', {
            name: /password strength/i,
        });
        expect(meter).toBeInTheDocument();
        // Twelve repeated lowercase characters score the "fair" bucket
        // (one run penalty against ~56 bits of entropy = 50). The
        // determinate progress maps the bucket to 50.
        expect(meter).toHaveAttribute('aria-valuenow', '50');
    });

    it('updates aria-valuenow as strength increases', async () => {
        const user = userEvent.setup({ delay: null });
        renderWithTheme(<Harness />);
        const input = getInput();
        await user.type(input, 'Correct-Horse-Battery-Staple-99!');
        const meter = screen.getByRole('progressbar', {
            name: /password strength/i,
        });
        expect(meter).toHaveAttribute('aria-valuenow', '100');
        expect(screen.getByText(/Strength: Strong/i)).toBeInTheDocument();
    });

    it('reports the good bucket for mixed character classes', async () => {
        const user = userEvent.setup({ delay: null });
        renderWithTheme(<Harness />);
        const input = getInput();
        await user.type(input, 'Tr0ub4dor&3xq');
        const meter = screen.getByRole('progressbar', {
            name: /password strength/i,
        });
        // Thirteen mixed-class characters score the "good" bucket
        // (~85 bits of entropy with no penalties), which maps to 75.
        expect(meter).toHaveAttribute('aria-valuenow', '75');
    });

    it('invokes onValidityChange with the latest validity info', async () => {
        const onValidityChange = vi.fn();
        const user = userEvent.setup({ delay: null });
        renderWithTheme(
            <Harness onValidityChange={onValidityChange} />
        );
        // Initial render reports an empty value with the new
        // byte-length/too-long fields included in the payload.
        expect(onValidityChange).toHaveBeenCalledWith({
            meetsMinimum: false,
            isEmpty: true,
            strength: 0,
            tooLong: false,
            byteLength: 0,
        });
        const input = getInput();
        await user.type(input, 'Correct-Horse-Battery-Staple-99!');
        const calls = onValidityChange.mock.calls;
        const last = calls[calls.length - 1]?.[0];
        expect(last).toMatchObject({
            meetsMinimum: true,
            isEmpty: false,
            strength: 4,
            tooLong: false,
            byteLength: 32,
        });
    });

    it('forwards a custom helperText override when provided', () => {
        renderWithTheme(<Harness helperText="Use a passphrase" />);
        expect(
            screen.getByText(/use a passphrase/i)
        ).toBeInTheDocument();
    });

    it('respects the externally provided error prop', async () => {
        const user = userEvent.setup({ delay: null });
        renderWithTheme(<Harness error />);
        const input = getInput();
        await user.type(input, 'aaaaaaaaaaaa');
        const root = input.closest('.MuiFormControl-root');
        // External error should win even with a value that meets the
        // minimum length.
        expect(root?.querySelector('.Mui-error')).not.toBeNull();
    });

    it('reports updates via the onChange callback', () => {
        const onChange = vi.fn();
        renderWithTheme(
            <PasswordStrengthField
                label="Password"
                value=""
                onChange={onChange}
            />
        );
        const input = screen.getByLabelText(/password/i) as HTMLInputElement;
        fireEvent.change(input, { target: { value: 'abc' } });
        expect(onChange).toHaveBeenCalledWith('abc');
    });

    it('defaults autoComplete to new-password to deter autofill', () => {
        renderWithTheme(
            <PasswordStrengthField
                label="Password"
                value=""
                onChange={() => {}}
            />
        );
        const input = screen.getByLabelText(/password/i) as HTMLInputElement;
        expect(input.getAttribute('autocomplete')).toBe('new-password');
    });

    it('lets a caller-supplied autoComplete override the default', () => {
        renderWithTheme(
            <PasswordStrengthField
                label="Password"
                value=""
                onChange={() => {}}
                autoComplete="off"
            />
        );
        const input = screen.getByLabelText(/password/i) as HTMLInputElement;
        expect(input.getAttribute('autocomplete')).toBe('off');
    });

    it('keeps the strength meter hidden when feedback is suppressed', async () => {
        renderWithTheme(<Harness hideFeedbackWhenEmpty />);
        // No characters typed yet, hideFeedbackWhenEmpty: meter must not
        // render.
        expect(
            screen.queryByRole('progressbar', { name: /password strength/i })
        ).not.toBeInTheDocument();
    });

    it('shows the strength meter once the user types past the minimum', async () => {
        const user = userEvent.setup({ delay: null });
        renderWithTheme(<Harness hideFeedbackWhenEmpty />);
        const input = getInput();
        await user.type(input, 'Correct-Horse-Battery-Staple-99!');
        expect(
            screen.getByRole('progressbar', { name: /password strength/i })
        ).toBeInTheDocument();
    });

    it('counts emoji as code points rather than UTF-16 code units', () => {
        // Six smiley emoji form a 12-code-unit string under JavaScript's
        // UTF-16 indexing but only 6 code points; the field must report
        // the lower count so it agrees with the server's rune-based
        // minimum check.
        const onValidityChange = vi.fn();
        renderWithTheme(
            <Harness
                initialValue={'\u{1F600}'.repeat(6)}
                onValidityChange={onValidityChange}
            />
        );
        expect(
            screen.getByText(/password is 6 of 12 minimum characters/i)
        ).toBeInTheDocument();
        const last = onValidityChange.mock.calls.at(-1)?.[0];
        expect(last).toMatchObject({
            meetsMinimum: false,
            isEmpty: false,
            tooLong: false,
            byteLength: 24,
        });
        // The strength meter must stay hidden while the value is below
        // the 12-code-point minimum, even though the UTF-16 length is 12.
        expect(
            screen.queryByRole('progressbar', { name: /password strength/i })
        ).not.toBeInTheDocument();
    });

    it('flags strings whose UTF-8 byte length exceeds the bcrypt limit', () => {
        // 20 four-byte emoji = 80 UTF-8 bytes, which exceeds the
        // 72-byte bcrypt limit even though the code-point count
        // satisfies the minimum.
        const onValidityChange = vi.fn();
        renderWithTheme(
            <Harness
                initialValue={'\u{1F600}'.repeat(20)}
                onValidityChange={onValidityChange}
            />
        );
        expect(
            screen.getByText(
                /password exceeds the 72-byte server limit \(currently 80 bytes\)/i
            )
        ).toBeInTheDocument();
        const root = getInput().closest('.MuiFormControl-root');
        expect(root?.querySelector('.Mui-error')).not.toBeNull();
        const last = onValidityChange.mock.calls.at(-1)?.[0];
        expect(last).toMatchObject({
            meetsMinimum: true,
            isEmpty: false,
            tooLong: true,
            byteLength: 80,
        });
        // Strength meter is suppressed for over-limit values because
        // submitting them would fail server-side regardless of
        // strength.
        expect(
            screen.queryByRole('progressbar', { name: /password strength/i })
        ).not.toBeInTheDocument();
    });

    it('treats a 12-character ASCII passphrase as valid', () => {
        const onValidityChange = vi.fn();
        renderWithTheme(
            <Harness
                initialValue="Tr0ub4dor&3xQ"
                onValidityChange={onValidityChange}
            />
        );
        // No error helper, byte-length helper, or below-minimum helper.
        expect(
            screen.queryByText(/minimum characters/i)
        ).not.toBeInTheDocument();
        expect(
            screen.queryByText(/exceeds the 72-byte server limit/i)
        ).not.toBeInTheDocument();
        const last = onValidityChange.mock.calls.at(-1)?.[0];
        expect(last).toMatchObject({
            meetsMinimum: true,
            isEmpty: false,
            tooLong: false,
            byteLength: 13,
        });
    });

    it('ignores caller attempts to widen the maxLength input attribute', () => {
        // The field spreads caller-supplied inputProps before it sets
        // the security-critical maxLength so the bcrypt cap cannot be
        // overridden by a parent.
        renderWithTheme(<Harness inputProps={{ maxLength: 1000 }} />);
        const input = getInput();
        expect(input.maxLength).toBe(PASSWORD_MAX_LENGTH);
    });
});
