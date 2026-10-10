package rqlite

import (
	"errors"
	"net/http"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that rqlite reported. An error of SQL arrives with HTTP
// 200, as the key error of the result of the statement, such as
// `no such table: t` (measured). A request that the server refused arrives
// with another status: HTTP 400 for a body that is not JSON, HTTP 401 for a
// wrong password, and HTTP 500 for a result that JSON cannot hold, such as an
// infinity (measured).
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Message is the message of the server.
	Message string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	return "rqlite: " + err.Message
}

// Unwrap returns the *dbimp.StatusError of a response whose status is not
// 2xx, or nil.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// Is reports whether err matches target. It matches dbimp.ErrAuthentication
// for HTTP 401, which is the answer for a wrong password (recorded: "a wrong
// password"). rqlite also sends HTTP 401, with the same empty body, for an
// endpoint that a user has no permission for (recorded: "the status"), but
// the driver sends only /db/execute, /db/query and /db/request, so a 401 that
// the driver sees is a refused credential (D197).
func (err *Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.authRefused()
}

// authRefused reports whether the server refused the credential.
func (err *Error) authRefused() bool {
	return err.HTTPStatus == http.StatusUnauthorized
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with the text of the body, which is plain
// text, or empty for HTTP 401 (measured).
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	e := &Error{HTTPStatus: serr.Code, Message: strings.TrimSpace(serr.Body), status: serr}
	if e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
