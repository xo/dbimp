package databend

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that Databend reported, with its HTTP status and its
// code, such as 1005 for a statement that does not parse (measured). A
// statement that fails has HTTP 200 and the error in its body.
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Code is the code of the server, or the HTTP status when the body held
	// none.
	Code int
	// Message is the message of the server.
	Message string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	return "databend: " + strconv.Itoa(err.Code) + ": " + err.Message
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
// the body, and returns an *Error with the code and the message of the body,
// which is {"error": {"code", "message"}} (measured).
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	var body struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	e := &Error{HTTPStatus: serr.Code, Code: serr.Code, status: serr, Message: strings.TrimSpace(serr.Body)}
	if json.Unmarshal([]byte(serr.Body), &body) == nil && body.Error.Message != "" {
		e.Code, e.Message = body.Error.Code, body.Error.Message
	}
	if e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
