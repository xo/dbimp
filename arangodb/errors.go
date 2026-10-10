package arangodb

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that ArangoDB reported, with its HTTP status and its
// errorNum, such as 1501 for a statement that does not parse (measured).
// Compare Num, and not the message, as the vendor says.
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Num is the errorNum of the server, or 0 when the body held none.
	Num int
	// Message is the errorMessage of the server.
	Message string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	if err.Num != 0 {
		return "arangodb: " + strconv.Itoa(err.Num) + ": " + err.Message
	}
	return "arangodb: status " + strconv.Itoa(err.HTTPStatus) + ": " + err.Message
}

// Unwrap returns the *dbimp.StatusError of the response. It returns nil for
// an HTTP 401 that is not a refused credential, because the status alone
// would then match dbimp.ErrAuthentication (D197).
func (err *Error) Unwrap() error {
	if err.status == nil || err.HTTPStatus == http.StatusUnauthorized && !err.authRefused() {
		return nil
	}
	return err.status
}

// Is reports whether err matches target. It matches dbimp.ErrAuthentication
// for a refused credential only (D197).
func (err *Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.authRefused()
}

// messageNotAuthenticated is the message that the server sends with HTTP 401
// and errorNum 11 for a request whose credential it refused (measured). A
// user without access to a database gets the same status and errorNum with
// the message "No read access to database.", so the message tells the two
// apart.
const messageNotAuthenticated = "User not authenticated"

// authRefused reports whether the server refused the credential.
func (err *Error) authRefused() bool {
	return err.HTTPStatus == http.StatusUnauthorized && err.Num == numNotAuthorized &&
		err.Message == messageNotAuthenticated
}

// The errorNum values that the driver reads (errors.dat of 3.12.12).
const (
	numNotAuthorized      = 11
	numCursorNotFound     = 1600
	numDuplicateName      = 1207
	numCollectionNotFound = 1203
)

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with the errorNum and the errorMessage of
// the body.
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	var body struct {
		Num     int    `json:"errorNum"`
		Message string `json:"errorMessage"`
	}
	e := &Error{HTTPStatus: serr.Code, status: serr, Message: strings.TrimSpace(serr.Body)}
	if json.Unmarshal([]byte(serr.Body), &body) == nil && body.Num != 0 {
		e.Num, e.Message = body.Num, body.Message
	}
	if e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}

// is reports whether err is an *Error with the errorNum num.
func is(err error, num int) bool {
	e, ok := errors.AsType[*Error](err)
	return ok && e.Num == num
}
