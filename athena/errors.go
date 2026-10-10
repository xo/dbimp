package athena

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The sentinel errors of the driver. Each one wraps into a failure with
// errors.Is (D6).
const (
	// ErrCanceled is the error of a query that ended in the state CANCELLED.
	// It is returned also when the driver did not stop the query, because
	// someone else did, such as an administrator (D192 item 8).
	ErrCanceled dbimp.Error = "the query was canceled"
	// ErrNoCredentials is the error of a connector whose Config holds no
	// access key and no secret key, because the driver reads no credential
	// from the environment (D7 and D192).
	ErrNoCredentials dbimp.Error = "the configuration has no access key or secret key"
)

// Error is an error that Athena reported. It has two forms. A request that
// the server refuses answers HTTP 400 with a JSON object that names its type
// in __type, and its text in Message or message, and that holds
// AthenaErrorCode for an InvalidRequestException (recorded). A query that
// fails answers HTTP 200, and GetQueryExecution then has the state FAILED
// with the text in StateChangeReason and the code in AthenaError (recorded).
// A query that was canceled has the state CANCELLED and no AthenaError.
type Error struct {
	// HTTPStatus is the status code of the response, and zero for a query that
	// ended in a state.
	HTTPStatus int
	// Type is the name of the type of the error, such as
	// InvalidRequestException. It is empty for a query that ended in a state.
	Type string
	// Code is AthenaErrorCode, such as MALFORMED_QUERY, and empty when the
	// answer has none.
	Code string
	// State is the state of a query that ended without rows, FAILED or
	// CANCELLED, and empty for a request that the server refused.
	State string
	// ErrorType is the member ErrorType of AthenaError, such as 1301, and zero
	// when the answer has none.
	ErrorType int
	// Category is the member ErrorCategory of AthenaError: 1 is a fault of the
	// system, 2 a fault of the user, and 3 another one. It is zero when the
	// answer has none.
	Category int
	// Message is the text of the error.
	Message string
	// QueryID is the id of the query, and empty for a request that the server
	// refused before a query existed.
	QueryID string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "athena: "
	switch {
	case err.State != "":
		s += err.State + ": "
	case err.Code != "":
		s += err.Code + ": "
	case err.Type != "":
		s += err.Type + ": "
	case err.HTTPStatus != 0:
		s += strconv.Itoa(err.HTTPStatus) + ": "
	}
	return s + err.Message
}

// Unwrap returns the *dbimp.StatusError of the response, and nil for a query
// that ended in a state.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// Is reports whether target is ErrCanceled and the query ended in the state
// CANCELLED.
func (err *Error) Is(target error) bool {
	return target == ErrCanceled && err.State == stateCancelled
}

// The states of a query (recorded).
const (
	stateQueued    = "QUEUED"
	stateRunning   = "RUNNING"
	stateSucceeded = "SUCCEEDED"
	stateFailed    = "FAILED"
	stateCancelled = "CANCELLED"
)

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with what the body says.
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	return newError(serr)
}

// newError returns the *Error of a response whose status is not 2xx. The body
// is a JSON object, or text such as a page of HTML from a proxy.
func newError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr}
	var body struct {
		Type    string `json:"__type"`
		Code    string `json:"AthenaErrorCode"`
		Message string `json:"Message"`
		Lower   string `json:"message"`
	}
	text := strings.TrimSpace(serr.Body)
	if json.Unmarshal([]byte(text), &body) != nil {
		e.Message = http.StatusText(serr.Code)
		if text != "" && !strings.HasPrefix(text, "<") {
			e.Message = text
		}
		return e
	}
	if _, name, ok := strings.Cut(body.Type, "#"); ok {
		e.Type = name
	} else {
		e.Type = body.Type
	}
	e.Code = body.Code
	switch {
	case body.Message != "":
		e.Message = body.Message
	case body.Lower != "":
		e.Message = body.Lower
	default:
		e.Message = http.StatusText(serr.Code)
	}
	return e
}

// stateError returns the error of a query that ended in the state FAILED or
// CANCELLED. The text is StateChangeReason, because AthenaError can lack it
// (recorded: the type 1106 has an empty ErrorMessage).
func stateError(id string, q *execution) *Error {
	st := q.QueryExecution.Status
	e := &Error{State: st.State, Message: st.StateChangeReason, QueryID: id}
	if a := st.AthenaError; a != nil {
		e.ErrorType, e.Category = a.ErrorType, a.ErrorCategory
		if e.Message == "" {
			e.Message = a.ErrorMessage
		}
	}
	if e.Message == "" {
		e.Message = "the query ended with no reason"
	}
	return e
}
