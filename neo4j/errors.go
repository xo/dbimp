package neo4j

import (
	"fmt"
	"strings"
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

// ResponseError holds the errors of one response, with its HTTP status. The
// status is 202 for an error that follows some rows, and 4xx for a request
// that the server refused. Use errors.As to read an Error of it.
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

// Unwrap returns each Error, for errors.As and errors.Is.
func (err *ResponseError) Unwrap() []error {
	errs := make([]error, len(err.Errs))
	for i, e := range err.Errs {
		errs[i] = e
	}
	return errs
}
