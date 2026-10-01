package avatica

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that the server of Avatica reported. It arrives with
// HTTP 500, as {"response": "error", "exceptions": [...], "errorMessage":
// ..., "errorCode": ..., "sqlState": ..., "severity": ...} (measured).
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Message is the message of the server: errorMessage, or the first line
	// of the first exception when the server sent none.
	Message string
	// Code is errorCode, such as 1 for an unknown connection.
	Code int
	// SQLState is sqlState, which HSQLDB leaves as 00000.
	SQLState string
	// Exception is the first line of the first exception, such as
	// java.sql.SQLSyntaxErrorException: unexpected token: SELEC.
	Exception string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	return "avatica: " + err.Message
}

// Unwrap returns the *dbimp.StatusError of a response whose status is not
// 2xx, or nil.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// noSuchConnection is the exception of a connection that the server does not
// know, such as one whose cache entry expired (measured).
const noSuchConnection = "org.apache.calcite.avatica.NoSuchConnectionException"

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with the fields of the error of the server.
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	e := &Error{HTTPStatus: serr.Code, status: serr}
	var body struct {
		Exceptions   []string `json:"exceptions"`
		ErrorMessage string   `json:"errorMessage"`
		ErrorCode    int      `json:"errorCode"`
		SQLState     string   `json:"sqlState"`
	}
	if json.Unmarshal([]byte(serr.Body), &body) == nil {
		e.Message, e.Code, e.SQLState = body.ErrorMessage, body.ErrorCode, body.SQLState
		if len(body.Exceptions) > 0 {
			e.Exception, _, _ = strings.Cut(body.Exceptions[0], "\n")
		}
	}
	if e.Message == "" {
		e.Message = e.Exception
	}
	if e.Message == "" {
		e.Message = strings.TrimSpace(serr.Body)
	}
	if e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
