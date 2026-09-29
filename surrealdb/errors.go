package surrealdb

import (
	"fmt"
	"strconv"
	"strings"
)

// Error is one error that SurrealDB reported, such as a statement that
// failed, a parse error of the RPC call with code -32000, or a request that
// the server refused with HTTP 400 (D55).
type Error struct {
	// Code is the code of the error: the code of a failed RPC call, -1 for an
	// RPC error with no code, the HTTP status of a refused request, or 0 for
	// a statement that failed.
	Code int
	// Kind is the kind of the error that 3.x names, such as "NotFound" or
	// "Thrown", or "" when the server names none.
	Kind string
	// Msg is the message of the error.
	Msg string
}

// Error satisfies the error interface.
func (err Error) Error() string {
	return "surrealdb: " + err.text()
}

// text returns the code, the kind and the message that the server sent.
func (err Error) text() string {
	var parts []string
	if err.Code != 0 {
		parts = append(parts, strconv.Itoa(err.Code))
	}
	if err.Kind != "" {
		parts = append(parts, err.Kind)
	}
	return strings.Join(append(parts, err.Msg), ": ")
}

// ResponseError holds the errors of one response, with its HTTP status and
// the status of the statement that failed, such as "ERR". Use errors.As to
// read an Error of it.
type ResponseError struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Status is the status of the statement that failed, or "" for a failed
	// RPC call and for a refused request.
	Status string
	// Errs are the errors, in the order that the server sent them.
	Errs []Error
}

// Error satisfies the error interface.
func (err *ResponseError) Error() string {
	var msgs []string
	for _, e := range err.Errs {
		msgs = append(msgs, e.text())
	}
	if len(msgs) == 0 {
		return fmt.Sprintf("surrealdb: status %s, HTTP %d", err.Status, err.HTTPStatus)
	}
	return "surrealdb: " + strings.Join(msgs, ". ")
}

// Unwrap returns each Error, for errors.As and errors.Is.
func (err *ResponseError) Unwrap() []error {
	errs := make([]error, len(err.Errs))
	for i, e := range err.Errs {
		errs[i] = e
	}
	return errs
}
