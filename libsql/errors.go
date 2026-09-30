package libsql

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strings"

	"github.com/xo/dbimp"
)

// The codes of Hrana that the driver reads (docs/LIBSQL.md, Errors).
const (
	// CodeStreamExpired is the code of a stream that had no request for 10
	// seconds. The server rolled back its transaction (measured).
	CodeStreamExpired = "STREAM_EXPIRED"
)

// Error is an error that libSQL reported. An error of a statement arrives
// with HTTP 200, as a result of the type error with a message and a code,
// such as SQL_PARSE_ERROR or SQLITE_CONSTRAINT (measured). A request that the
// server refused arrives with another status: HTTP 400 for a fault of the
// protocol, as text, or for an expired stream, HTTP 401 for a token that is
// not valid, and HTTP 403 for a write with a token that can only read
// (measured).
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Code is the code of Hrana or of SQLite, or "" when the server sent
	// none.
	Code string
	// Message is the message of the server.
	Message string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	if err.Code == "" {
		return "libsql: " + err.Message
	}
	return "libsql: " + err.Code + ": " + err.Message
}

// Unwrap returns the *dbimp.StatusError of a response whose status is not
// 2xx, or nil.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with the message of the body, which is
// {"message": ..., "code": ...} for an expired stream, {"error": ...} for a
// refusal, and plain text for a fault of the protocol (measured).
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	e := &Error{HTTPStatus: serr.Code, Message: strings.TrimSpace(serr.Body), status: serr}
	var body struct {
		Message string `json:"message"`
		Code    string `json:"code"`
		Error   string `json:"error"`
	}
	if json.Unmarshal([]byte(serr.Body), &body) == nil {
		switch {
		case body.Message != "":
			e.Message, e.Code = body.Message, body.Code
		case body.Error != "":
			e.Message = body.Error
		}
	}
	if e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
