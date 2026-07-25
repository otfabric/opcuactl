// SPDX-License-Identifier: MIT

package opcuactl

import "fmt"

// exitError wraps an error with a specific exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// ExitCode returns the exit code for the error, or 1 if unknown.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exitError); ok {
		return ee.code
	}
	return 1
}

func exitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{code: code, err: err}
}

func exitCodef(code int, format string, args ...interface{}) error {
	return exitCode(code, fmt.Errorf(format, args...))
}
