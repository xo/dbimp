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

// Unwrap returns the *dbimp.StatusError of the response.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// The errorNum values that the driver reads (errors.dat of 3.12.12).
const (
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
