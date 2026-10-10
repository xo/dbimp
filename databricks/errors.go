package databricks

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/xo/dbimp"
)

// The sentinel errors of the driver. Each one wraps into a failure with
// errors.Is (D6).
const (
	// ErrCanceled is the error of a statement that the server ended in the
	// state CANCELED, when the context of the statement did not end. Someone
	// else canceled it, such as an administrator (measured).
	ErrCanceled dbimp.Error = "the statement was canceled"
	// ErrClosed is the error of a statement whose state is CLOSED. The
	// documentation names the state, and no recording shows it (D193).
	ErrClosed dbimp.Error = "the result of the statement is closed"
	// ErrTruncated is the error of a result that the server cut at a limit,
	// which the manifest names with truncated of true (D193 item 2). The
	// driver sends no limit, so the server cuts a result only when the
	// result is larger than the limit of the disposition INLINE.
	ErrTruncated dbimp.Error = "the server cut the result"
	// ErrCut is the error of a result that ends before the count of rows that
	// the server named for it. After a row, it comes wrapped with
	// dbimp.ErrIncomplete (D21 and D107).
	ErrCut dbimp.Error = "the answer was cut short"
)

// The states of a statement (measured, except RUNNING and CLOSED, which the
// documentation names).
const (
	statePending   = "PENDING"
	stateRunning   = "RUNNING"
	stateSucceeded = "SUCCEEDED"
	stateFailed    = "FAILED"
	stateCanceled  = "CANCELED"
	stateClosed    = "CLOSED"
)

// Error is an error that Databricks reported. A statement that fails in the
// engine is HTTP 200 with the state FAILED, and the error is in the member
// status of the answer (measured). A request that the server refuses before
// it runs a statement is an HTTP status of 400, 403 or 404 with a JSON object
// of the code and the message (measured).
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Code is error_code, such as BAD_REQUEST or PERMISSION_DENIED. A wrong
	// token answers with a JSON number, 403, which Code holds as text.
	Code string
	// SQLState is sql_state of a statement that failed, such as 42601, and
	// empty when the answer has none.
	SQLState string
	// Message is the message of the error, with no white space at its ends.
	Message string
	// StatementID is statement_id, and empty when the answer has none.
	StatementID string

	state  string
	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "databricks: "
	switch {
	case err.Code != "" && err.SQLState != "":
		s += err.Code + " (" + err.SQLState + "): "
	case err.Code != "":
		s += err.Code + ": "
	}
	return s + err.Message
}

// Unwrap returns the *dbimp.StatusError of the response, when the error
// came with an HTTP status that is not 2xx.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// Is reports whether target is the sentinel that the state of the error
// names, ErrCanceled or ErrClosed.
func (err *Error) Is(target error) bool {
	switch target {
	case ErrCanceled:
		return err.state == stateCanceled
	case ErrClosed:
		return err.state == stateClosed
	}
	return false
}

// body is the object of an error answer with an HTTP status that is not 2xx.
// error_code is a text everywhere except for a wrong token, where it is the
// number 403 (measured), so the decoder takes it as a JSON value.
type body struct {
	Code    jsontext.Value `json:"error_code"`
	Message string         `json:"message"`
}

// codeText returns the error_code of an answer as text.
func codeText(v jsontext.Value) string {
	if len(v) == 0 || dbimp.IsNull(v) {
		return ""
	}
	if s, err := dbimp.String(v); err == nil {
		return s
	}
	return string(v)
}

// checkStatus returns nil if the status of res is one of the codes in ok.
// Otherwise it closes the body, and returns an *Error with what the body
// says.
func checkStatus(res *http.Response, ok ...int) error {
	if slices.Contains(ok, res.StatusCode) {
		return nil
	}
	err := dbimp.CheckStatus(res)
	if err == nil {
		// The status is 2xx and is not one that the driver reads.
		_ = res.Body.Close()
		return &Error{HTTPStatus: res.StatusCode, Message: "the status is " + http.StatusText(res.StatusCode) + ", which the driver does not read"}
	}
	serr, isStatus := errors.AsType[*dbimp.StatusError](err)
	if !isStatus {
		return err
	}
	return newError(serr)
}

// newError returns the *Error of a response whose status is not one that the
// driver reads. The body is a JSON object, or text such as a page of HTML
// from a proxy, or empty (measured for a path that the API lacks).
func newError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr}
	var b body
	text := strings.TrimSpace(serr.Body)
	if json.Unmarshal([]byte(text), &b) != nil {
		e.Message = http.StatusText(serr.Code)
		if text != "" && !strings.HasPrefix(text, "<") {
			e.Message = text
		}
		return e
	}
	e.Code = codeText(b.Code)
	if e.Message = strings.TrimSpace(b.Message); e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
