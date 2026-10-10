package spanner

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The sentinel errors of the driver. Each one wraps into a failure with
// errors.Is (D6).
const (
	// ErrAborted is the error of a transaction that the server aborted, with
	// the status ABORTED and HTTP 409. The server holds no lock for it any
	// more. The caller must run the whole transaction again, and
	// Error.RetryDelay holds the wait that the server asks for (D191 item 6).
	ErrAborted dbimp.Error = "the transaction was aborted"
	// ErrSessionNotFound is the error of a statement whose session the server
	// does not know any more. The statement did not run. The connector drops
	// the session and makes a new one for the next statement (D191 item 3).
	ErrSessionNotFound dbimp.Error = "the session was not found"
	// ErrCanceled is the error of a long running operation that someone
	// canceled, with the gRPC code 1. The driver cancels an operation when the
	// context of a DDL statement ends (D191 item 4).
	ErrCanceled dbimp.Error = "the operation was canceled"
)

// sessionType is the resource type that the server names in the details of a
// NOT_FOUND error for a session (recorded: "getSession after the delete").
const sessionType = "google.spanner.v1.Session"

// grpcStatus names each gRPC code, for an error of a long running operation,
// which holds the number and no name (recorded: "a DDL call that drops a
// table that does not exist: poll 1 of the operation").
var grpcStatus = [...]string{
	"OK", "CANCELLED", "UNKNOWN", "INVALID_ARGUMENT", "DEADLINE_EXCEEDED",
	"NOT_FOUND", "ALREADY_EXISTS", "PERMISSION_DENIED", "RESOURCE_EXHAUSTED",
	"FAILED_PRECONDITION", "ABORTED", "OUT_OF_RANGE", "UNIMPLEMENTED",
	"INTERNAL", "UNAVAILABLE", "DATA_LOSS", "UNAUTHENTICATED",
}

// Error is an error that Spanner reported. The body of an error is
// {"error": {"code", "message", "status", "details"}}. The code is the HTTP
// status in an answer, and the gRPC number in the error of a long running
// operation or of executeBatchDml (recorded).
type Error struct {
	// HTTPStatus is the status code of the response, and 0 for an error that
	// came inside a body with HTTP 200, as an error after some rows does.
	HTTPStatus int
	// Code is the member code.
	Code int
	// Status is the name of the gRPC code, such as ABORTED. An error of an
	// operation has no name, so the driver names it from the number.
	Status string
	// Message is the message of the server. The member message holds a
	// backslash and an n where a line break belongs (recorded: "a syntax
	// error"), so the driver takes the text of the detail LocalizedMessage,
	// and turns the two characters into a line break when there is none.
	Message string
	// RetryDelay is the wait that the detail RetryInfo asks for, or 0.
	RetryDelay time.Duration

	resource string
	status   *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "spanner: "
	if err.Status != "" {
		s += err.Status + ": "
	}
	return s + err.Message
}

// Unwrap returns the *dbimp.StatusError of the response, or nil.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// Is reports whether target is the sentinel that the status of the error
// names.
func (err *Error) Is(target error) bool {
	switch target {
	case ErrAborted:
		return err.Status == "ABORTED"
	case ErrSessionNotFound:
		return err.Status == "NOT_FOUND" && strings.HasSuffix(err.resource, sessionType)
	case ErrCanceled:
		return err.Status == "CANCELLED"
	}
	return false
}

// wireError is the member error of a body.
type wireError struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Status  string   `json:"status"`
	Details []detail `json:"details"`
}

// detail is one element of the member details. It holds the members of the
// types that the driver reads, and skips the others.
type detail struct {
	Type         string `json:"@type"`
	RetryDelay   string `json:"retryDelay"`
	Message      string `json:"message"`
	ResourceType string `json:"resourceType"`
}

// lineBreaks turns the two characters that the server writes for a line break
// and a quote into the characters themselves.
var lineBreaks = strings.NewReplacer(`\n`, "\n", `\"`, `"`)

// newErr returns the *Error of a wireError, which comes in a body of any HTTP
// status. It has no HTTP status of its own, and checkStatus sets it. An error
// with no name takes the name of its gRPC code.
func newErr(w wireError) *Error {
	e := &Error{Code: w.Code, Status: w.Status, Message: w.Message}
	if e.Status == "" && w.Code >= 0 && w.Code < len(grpcStatus) {
		e.Status = grpcStatus[w.Code]
	}
	local := ""
	for _, d := range w.Details {
		switch {
		case strings.HasSuffix(d.Type, "google.rpc.RetryInfo"):
			if delay, err := time.ParseDuration(d.RetryDelay); err == nil {
				e.RetryDelay = delay
			}
		case strings.HasSuffix(d.Type, "google.rpc.LocalizedMessage"):
			local = d.Message
		case strings.HasSuffix(d.Type, "google.rpc.ResourceInfo"):
			e.resource = d.ResourceType
		}
	}
	if local != "" {
		e.Message = local
	} else {
		e.Message = lineBreaks.Replace(e.Message)
	}
	return e
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes the
// body, and returns an *Error with what the body says. The body is a JSON
// object, or an array of one object for a stream, or a page of HTML for a
// path that the server does not know (recorded: "a path with no version").
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	if err == nil {
		return nil
	}
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	e := parseError(serr.Body)
	if e == nil {
		e = &Error{Code: serr.Code, Message: http.StatusText(serr.Code)}
		if text := strings.TrimSpace(serr.Body); text != "" && !strings.HasPrefix(text, "<") {
			e.Message = text
		}
	}
	e.HTTPStatus, e.status = serr.Code, serr
	return e
}

// parseError reads the member error from a body that is an object, or an
// array whose first element is an object. It returns nil for any other body.
func parseError(body string) *Error {
	w, ok := readWireError(strings.TrimSpace(body))
	if !ok {
		return nil
	}
	return newErr(w)
}

// readWireError reads the member error of the body text, and reports whether the
// body has one.
func readWireError(text string) (wireError, bool) {
	var one struct {
		Error *wireError `json:"error"`
	}
	switch {
	case strings.HasPrefix(text, "{"):
		if json.Unmarshal([]byte(text), &one) != nil {
			return wireError{}, false
		}
	case strings.HasPrefix(text, "["):
		var many []struct {
			Error *wireError `json:"error"`
		}
		if json.Unmarshal([]byte(text), &many) != nil || len(many) == 0 {
			return wireError{}, false
		}
		one.Error = many[0].Error
	}
	if one.Error == nil {
		return wireError{}, false
	}
	return *one.Error, true
}
