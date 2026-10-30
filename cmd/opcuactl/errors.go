// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/otfabric/go-opcua/ua"
)

// Process exit codes: the organisation table (docs/exitcodes.md).
const (
	exitOK           = 0
	exitFindings     = 1 // the command completed and the result reports a problem
	exitUsage        = 2
	exitInvalidInput = 3
	exitTransport    = 4
	exitTimeout      = 5
	exitProtocol     = 6
	exitPartial      = 7
	exitUnsupported  = 8
	exitPermission   = 9
	exitInterrupted  = 130 // 128 + SIGINT, as a shell reports it
)

// exitError wraps an error with a specific exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// ExitCode returns the exit code for the error. An error without a code is
// a rejected command line or flag value: exitUsage.
func ExitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return refine(exitUsage, err)
}

// exitCode gives err the code a command chose, or the more specific one that
// refine finds in err.
func exitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{code: refine(code, err), err: err}
}

func exitCodef(code int, format string, args ...interface{}) error {
	return exitCode(code, fmt.Errorf(format, args...))
}

// refine returns the exit code of what err says happened, code when it says
// nothing more specific. A partial result and findings stay as they are.
func refine(code int, err error) int {
	if code == exitPartial || code == exitFindings {
		return code
	}
	var pe *fs.PathError
	switch {
	case errors.Is(err, context.Canceled):
		return exitInterrupted
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ua.StatusBadTimeout):
		return exitTimeout
	case errors.Is(err, ua.StatusBadServiceUnsupported):
		return exitUnsupported
	case errors.Is(err, ua.StatusBadUserAccessDenied), errors.Is(err, fs.ErrPermission):
		return exitPermission
	case code == exitUsage && errors.As(err, &pe):
		return exitInvalidInput
	}
	return code
}
