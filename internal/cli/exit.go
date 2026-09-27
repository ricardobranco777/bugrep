// SPDX-License-Identifier: BSD-2-Clause

package cli

// exitCodeErr wraps an error with a specific process exit code, for cases
// (like a partially failed federated search) that need something other than
// the default "1" for any error.
type exitCodeErr struct {
	err  error
	code int
}

func (e *exitCodeErr) Error() string { return e.err.Error() }
func (e *exitCodeErr) Unwrap() error { return e.err }

func withExitCode(err error, code int) error {
	if err == nil {
		return nil
	}
	return &exitCodeErr{err: err, code: code}
}
