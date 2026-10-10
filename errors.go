package dbimp

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Error is an error of this package.
type Error string

// Error satisfies the error interface.
func (err Error) Error() string {
	return string(err)
}

// Error values.
const (
	// ErrNotSupported is the error for a feature that the database does not
	// have, such as a transaction (D20).
	ErrNotSupported Error = "not supported"
	// ErrScheme is the error for a DSN whose scheme is not the name of the
	// driver (D35).
	ErrScheme Error = "wrong scheme"
	// ErrUnknownKey is the error for a key in the query of a DSN that the
	// driver does not know (D27).
	ErrUnknownKey Error = "unknown key"
	// ErrRepeatedKey is the error for a key that appears more than once in
	// the query of a DSN (D27).
	ErrRepeatedKey Error = "repeated key"
	// ErrInvalidValue is the error for a value that has the wrong form.
	ErrInvalidValue Error = "invalid value"
	// ErrExtraColumn is the error for a row that has a column that the first
	// row does not have (D18).
	ErrExtraColumn Error = "column not in the first row"
	// ErrColumnCount is the error for a row that has the wrong number of
	// columns.
	ErrColumnCount Error = "wrong number of columns"
	// ErrIncomplete is the error for a result set that failed, or that the
	// server cut short, after at least one of its rows reached the caller
	// (D21 and D107). A failure before the first row does not wrap it.
	ErrIncomplete Error = "result is incomplete"
	// ErrUnterminated is the error for a statement that ends inside a
	// literal, a quoted identifier or a comment.
	ErrUnterminated Error = "unterminated literal or comment"
	// ErrArguments is the error for arguments that do not match the
	// placeholders of a statement (D34).
	ErrArguments Error = "arguments do not match the placeholders"
	// ErrAuthentication is the error that the error of a driver matches with
	// errors.Is when the server refused the credential: a wrong password, key
	// or token. It never matches a refusal for lack of a permission (D197).
	ErrAuthentication Error = "authentication refused"
)

// maxErrorBody is the most of the body of a response that a StatusError
// keeps.
const maxErrorBody = 64 << 10

// StatusError is the error for a response whose status is not 2xx. HTTP 429
// and HTTP 503 are a StatusError too, and nothing retries them (D8).
type StatusError struct {
	// Code is the status code of the response.
	Code int
	// Body is the start of the body of the response, at most 64 KiB.
	Body string
}

// Error satisfies the error interface.
func (err *StatusError) Error() string {
	text := http.StatusText(err.Code)
	if body := strings.TrimSpace(err.Body); body != "" {
		return fmt.Sprintf("status %d %s: %s", err.Code, text, body)
	}
	return fmt.Sprintf("status %d %s", err.Code, text)
}

// Is reports whether err matches target. It matches ErrAuthentication for HTTP
// 401, which is the status for a request with no valid credential. HTTP 403 is
// not matched, because a server also sends it for a missing permission, so the
// error of a driver decides it (D197).
func (err *StatusError) Is(target error) bool {
	return target == ErrAuthentication && err.Code == http.StatusUnauthorized
}

// CheckStatus returns nil if the status of res is 2xx. Otherwise it reads at
// most 64 KiB of the body, closes the body, and returns a *StatusError.
func CheckStatus(res *http.Response) error {
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	if err != nil {
		return fmt.Errorf("reading the body of status %d: %w", res.StatusCode, err)
	}
	return &StatusError{Code: res.StatusCode, Body: string(body)}
}
