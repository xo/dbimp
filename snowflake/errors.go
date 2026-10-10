package snowflake

import (
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
	// ErrCut is the error of a partition of a result that ends before the
	// count of rows that the server named for it. The server runs the whole
	// statement before it sends the first partition, so only a partition can
	// be cut (D21 and D183). After a row, it comes wrapped with
	// dbimp.ErrIncomplete (D107).
	ErrCut dbimp.Error = "the answer was cut short"
	// ErrCanceled is the error of a statement that the server ended with the
	// code 000604, when the context of the statement did not end. Someone
	// else canceled it, such as an administrator (measured).
	ErrCanceled dbimp.Error = "the statement was canceled"
	// ErrTimeout is the error of a statement that reached its timeout on the
	// server, which answers HTTP 408 and the code 000630 (measured).
	ErrTimeout dbimp.Error = "the statement reached its timeout"
)

// The codes of the errors that the driver tells apart (measured).
const (
	codeCanceled = "000604"
	codeTimeout  = "000630"
)

// Error is an error that Snowflake reported. Every answer that is not a
// result has a JSON object with the code, the SQLSTATE and the message, with
// HTTP 422 for a statement that failed, HTTP 408 for one that reached its
// timeout, HTTP 400 for a request that the server refused, and HTTP 401 for
// a token that is not valid (measured). An error comes before any row,
// because the server runs the whole statement before it sends the first
// partition.
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Code is code, such as 001003 for a syntax error, and empty when the
	// answer has none.
	Code string
	// SQLState is sqlState, such as 42000, and empty when the answer has
	// none. A wrong token has none.
	SQLState string
	// Message is message, or the text of the answer when it has none.
	Message string
	// Handle is statementHandle, and empty when the answer has none.
	Handle string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "snowflake: "
	switch {
	case err.Code != "" && err.SQLState != "":
		s += err.Code + " (" + err.SQLState + "): "
	case err.Code != "":
		s += err.Code + ": "
	}
	return s + err.Message
}

// Unwrap returns the *dbimp.StatusError of the response.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// codeInvalidToken is the code of a JWT that the server refused, with HTTP 401
// (measured).
const codeInvalidToken = "390144"

// Is reports whether target is the sentinel that the code of the error
// names, ErrCanceled or ErrTimeout, or dbimp.ErrAuthentication when the server
// refused the token: the code 390144, or HTTP 401 (D197). A statement that the
// role may not run, such as code 002003 with HTTP 422, does not match.
func (err *Error) Is(target error) bool {
	switch target {
	case dbimp.ErrAuthentication:
		return err.Code == codeInvalidToken || err.HTTPStatus == http.StatusUnauthorized
	case ErrCanceled:
		return err.Code == codeCanceled
	case ErrTimeout:
		return err.Code == codeTimeout || err.HTTPStatus == http.StatusRequestTimeout
	}
	return false
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
// from a proxy.
func newError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr}
	var body struct {
		Code     string `json:"code"`
		SQLState string `json:"sqlState"`
		Message  string `json:"message"`
		Handle   string `json:"statementHandle"`
	}
	text := strings.TrimSpace(serr.Body)
	if json.Unmarshal([]byte(text), &body) != nil {
		e.Message = http.StatusText(serr.Code)
		if text != "" && !strings.HasPrefix(text, "<") {
			e.Message = text
		}
		return e
	}
	e.Code, e.SQLState, e.Handle = body.Code, body.SQLState, body.Handle
	if e.Message = body.Message; e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
