package maxcompute

import (
	"encoding/xml"
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
	// state Cancelled, when the context of the statement did not end. Someone
	// else canceled it (recorded: "cancel: the result after the terminate").
	ErrCanceled dbimp.Error = "the statement was canceled"
	// ErrCut is the error of a result that ends before its end marker. After
	// a row, it comes wrapped with dbimp.ErrIncomplete (D21 and D107).
	ErrCut dbimp.Error = "the answer was cut short"
)

// The states of a task (recorded).
const (
	stateRunning   = "Running"
	stateSuccess   = "Success"
	stateFailed    = "Failed"
	stateCancelled = "Cancelled"
	stateWaiting   = "Waiting"
)

// codeSignature is the code of the error of a signature that does not match,
// which the server sends for a wrong secret (recorded: "a wrong secret").
const codeSignature = "SignatureNotMatch"

// Error is an error that MaxCompute reported. A statement that does not parse
// is HTTP 400 with an XML body of a code and a message. A statement that parses
// and then fails, such as one for a missing table, is HTTP 200 and 201 with a
// task in the state Failed, and the message in its result (recorded). A
// request that the server refuses is HTTP 403 or 404 with the same XML body.
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Code is the code of the XML body, such as ParseError or
	// SignatureNotMatch. It is empty for a task that failed.
	Code string
	// Message is the message, with no white space at its ends. For a task
	// that failed it starts with a code such as ODPS-0130131.
	Message string
	// RequestID is the id of the request, and empty when the body has none.
	RequestID string

	state  string
	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "maxcompute: "
	if err.Code != "" {
		s += err.Code + ": "
	}
	return s + err.Message
}

// Unwrap returns the *dbimp.StatusError of the response, when the error came
// with an HTTP status that is not 2xx.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// Is reports whether target is ErrCanceled and the task ended in the state
// Cancelled. It reports whether target is dbimp.ErrAuthentication and the
// server refused the credential: HTTP 403 with the code SignatureNotMatch. Any
// other 403 is a missing permission, and it is not a refusal (D197).
func (err *Error) Is(target error) bool {
	switch target {
	case ErrCanceled:
		return err.state == stateCancelled
	case dbimp.ErrAuthentication:
		return err.HTTPStatus == http.StatusForbidden && err.Code == codeSignature
	}
	return false
}

// AuthenticationRefused reports whether the server refused the credential. It
// returns what Is reports for dbimp.ErrAuthentication, and a caller that does
// not import this module reads it through an interface of its own (D227).
func (err *Error) AuthenticationRefused() bool {
	return err.Is(dbimp.ErrAuthentication)
}

// body is the XML body of an error answer.
type body struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	RequestID string   `xml:"RequestId"`
}

// checkStatus returns nil if the status of res is one of the codes in ok.
// Otherwise it closes the body, and returns an *Error with what the body says.
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
		return err //nolint:wrapcheck // The error comes from dbimp.CheckStatus, which wraps it.
	}
	return newError(serr)
}

// newError returns the *Error of a response whose status is not one that the
// driver reads. The body is XML, or text such as a page of HTML from a proxy, or
// empty.
func newError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr}
	var b body
	text := strings.TrimSpace(serr.Body)
	if xml.Unmarshal([]byte(text), &b) != nil {
		e.Message = http.StatusText(serr.Code)
		if text != "" && !strings.HasPrefix(text, "<") {
			e.Message = text
		}
		return e
	}
	e.Code = strings.TrimSpace(b.Code)
	e.RequestID = strings.TrimSpace(b.RequestID)
	if e.Message = strings.TrimSpace(b.Message); e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}

// taskError returns the error of a task that did not succeed. The text is the
// message that the result of the task holds.
func taskError(status, text string) error {
	e := &Error{HTTPStatus: http.StatusOK, state: status, Message: strings.TrimSpace(text)}
	switch status {
	case stateFailed:
		if e.Message == "" {
			e.Message = "the statement failed"
		}
	case stateCancelled:
		if e.Message == "" {
			e.Message = "the statement was canceled"
		}
	default:
		e.Message = "the state of the statement is " + status + ", which the driver does not know"
	}
	return e
}
