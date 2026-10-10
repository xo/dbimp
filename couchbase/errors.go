package couchbase

import (
	"fmt"
	"strings"

	"github.com/xo/dbimp"
)

// Error is one error that the query service reported, such as code 3000 for
// a syntax error or 12009 for an insert of a key that exists.
type Error struct {
	// Code is the code of the error.
	Code int `json:"code"`
	// Msg is the message of the error.
	Msg string `json:"msg"`
}

// Error satisfies the error interface.
func (err Error) Error() string {
	return fmt.Sprintf("couchbase: %d: %s", err.Code, err.Msg)
}

// codeAuthentication is the code that the query service sends, with HTTP 401,
// when it cannot authenticate the user: a wrong password (D197).
const codeAuthentication = 2120

// Is reports whether err matches target. It matches dbimp.ErrAuthentication
// for code 2120 alone. Code 13014 is also HTTP 401, but it means that the
// user lacks a role, so it does not match (D197).
func (err Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.Code == codeAuthentication
}

// ResponseError holds the errors of one response, with its HTTP status and
// the status that the server reported, such as "fatal" or "stopped". Use
// errors.As to read an Error of it.
type ResponseError struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Status is the status in the body of the response.
	Status string
	// Errs are the errors, in the order that the server sent them.
	Errs []Error
}

// Error satisfies the error interface.
func (err *ResponseError) Error() string {
	var msgs []string
	for _, e := range err.Errs {
		msgs = append(msgs, fmt.Sprintf("%d: %s", e.Code, e.Msg))
	}
	if len(msgs) == 0 {
		return fmt.Sprintf("couchbase: status %s, HTTP %d", err.Status, err.HTTPStatus)
	}
	return "couchbase: " + strings.Join(msgs, ". ")
}

// Unwrap returns each Error, for errors.As and errors.Is.
func (err *ResponseError) Unwrap() []error {
	errs := make([]error, len(err.Errs))
	for i, e := range err.Errs {
		errs[i] = e
	}
	return errs
}
