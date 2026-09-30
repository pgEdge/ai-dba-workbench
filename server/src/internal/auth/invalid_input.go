/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import "errors"

// ErrInvalidInput marks an error the store returns because the caller asked
// for something the store's rules refuse, as opposed to the store failing.
// Test for it with errors.Is. The message to show the caller is the text of
// the *InvalidInputError that errors.As finds, which carries no wrapping
// context added on the way out.
var ErrInvalidInput = errors.New("invalid input")

// InvalidInputError carries the reason a request was refused. Its message is
// the reason alone, written to be shown to the person who made the request.
type InvalidInputError struct {
	Err error
}

// Error returns the reason the request was refused.
func (e *InvalidInputError) Error() string {
	return e.Err.Error()
}

// Unwrap returns the underlying reason, so that errors.Is and errors.As
// still reach any error it wraps.
func (e *InvalidInputError) Unwrap() error {
	return e.Err
}

// Is reports whether target is ErrInvalidInput.
func (e *InvalidInputError) Is(target error) bool {
	return target == ErrInvalidInput
}

// invalidInput marks err as a refusal of the caller's request. A nil err is
// returned unchanged, so a validator's result can be passed straight in.
func invalidInput(err error) error {
	if err == nil {
		return nil
	}
	return &InvalidInputError{Err: err}
}
