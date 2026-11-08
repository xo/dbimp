package neo4j

import (
	"fmt"
	"slices"
	"strings"

	"github.com/xo/dbimp"
)

// Error is one error that Neo4j reported, such as
// Neo.ClientError.Statement.SyntaxError (D66).
type Error struct {
	// Code is the code of the error.
	Code string `json:"code"`
	// Message is the message of the error.
	Message string `json:"message"`
}

// Error satisfies the error interface.
func (err Error) Error() string {
	return "neo4j: " + err.Code + ": " + err.Message
}

// ErrVerbose returns every field that the server sent, with the message on the
// first line and one field on each line after it, for the command \errverbose
// of usql. It returns "" when no field has a value but the message. A caller
// that does not import this module reads it through an interface of its own
// (D230).
func (err Error) ErrVerbose(_ string) string {
	return dbimp.Verbose(err.Message,
		"Code", err.Code)
}

// codeUnauthorized is the code that Neo4j sends, with HTTP 401, for a
// credential that it refused, such as a wrong password (recorded). The code
// Neo.ClientError.Security.Forbidden is a missing privilege, and it is not
// a refused credential (D197).
const codeUnauthorized = "Neo.ClientError.Security.Unauthorized"

// Is reports whether err matches target. It matches dbimp.ErrAuthentication
// for the code Neo.ClientError.Security.Unauthorized alone (D197).
func (err Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.Code == codeUnauthorized
}

// AuthenticationRefused reports whether the server refused the credential. It
// returns what Is reports for dbimp.ErrAuthentication, and a caller that does
// not import this module reads it through an interface of its own (D227).
func (err Error) AuthenticationRefused() bool {
	return err.Is(dbimp.ErrAuthentication)
}

// ResponseError holds the errors of one response, with its HTTP status. The
// status is 202 for an error that follows some rows, and 4xx or 5xx for a
// request that the server refused, such as 500 for TransactionStartFailed on
// 5.26.31. Use errors.As to read an Error of it.
type ResponseError struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Errs are the errors, in the order that the server sent them.
	Errs []Error
}

// Error satisfies the error interface.
func (err *ResponseError) Error() string {
	var msgs []string
	for _, e := range err.Errs {
		msgs = append(msgs, e.Code+": "+e.Message)
	}
	if len(msgs) == 0 {
		return fmt.Sprintf("neo4j: HTTP %d", err.HTTPStatus)
	}
	return "neo4j: " + strings.Join(msgs, ". ")
}

// ErrVerbose returns every field that the server sent, for the command
// \errverbose of usql: the status of the response, and then each error with its
// fields. A caller that does not import this module reads it through an
// interface of its own (D230).
func (err *ResponseError) ErrVerbose(query string) string {
	parts := []string{dbimp.Verbose("", "HTTP status", err.HTTPStatus)}
	for _, e := range err.Errs {
		if s := e.ErrVerbose(query); s != "" {
			parts = append(parts, s)
		} else {
			parts = append(parts, e.Error())
		}
	}
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), "\n")
}

// Unwrap returns each Error, for errors.As and errors.Is.
func (err *ResponseError) Unwrap() []error {
	errs := make([]error, len(err.Errs))
	for i, e := range err.Errs {
		errs[i] = e
	}
	return errs
}
