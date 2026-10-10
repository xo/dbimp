package influxdb

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that InfluxDB reported. It is the answer to a request
// whose status is not 2xx, the error of one InfluxQL statement or of the
// whole InfluxQL answer with HTTP 200, or an answer with HTTP 200 that is not
// JSON (measured).
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Statement is the statement_id of the InfluxQL statement that failed.
	// It is -1 for an error of the whole request.
	Statement int
	// Message is the message of the server.
	Message string

	// status is the error of the status, for errors.As.
	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	var b strings.Builder
	b.WriteString("influxdb: ")
	if err.Statement >= 0 {
		b.WriteString("statement " + strconv.Itoa(err.Statement) + ": ")
	} else if err.HTTPStatus >= http.StatusMultipleChoices {
		b.WriteString("status " + strconv.Itoa(err.HTTPStatus) + ": ")
	}
	b.WriteString(err.Message)
	return b.String()
}

// Unwrap returns the *dbimp.StatusError of a status that is not 2xx, or nil.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// Is reports whether err matches target. It matches dbimp.ErrAuthentication
// for HTTP 401, which every release sends for a wrong password or token
// (recorded: "a wrong password"). HTTP 403 and a statement that failed with
// HTTP 200, such as `insufficient permissions` on InfluxDB 2, are a missing
// permission and do not match (D197).
func (err *Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.authRefused()
}

// authRefused reports whether the server refused the credential.
func (err *Error) authRefused() bool {
	return err.HTTPStatus == http.StatusUnauthorized
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body and returns an *Error with the message of the server, which is
// JSON with "error" on InfluxDB 1 and 3, JSON with "message" on InfluxDB 2,
// or plain text (measured).
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	msg := strings.TrimSpace(serr.Body)
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(msg), &body) == nil {
		switch {
		case body.Error != "":
			msg = body.Error
		case body.Message != "":
			msg = body.Message
		}
	}
	if msg == "" {
		msg = http.StatusText(serr.Code)
	}
	return &Error{HTTPStatus: serr.Code, Statement: -1, Message: msg, status: serr}
}

// checkJSON returns an error, and closes the body, if res is not JSON. A SQL
// request to InfluxDB 2 answers HTTP 200 with the HTML of its user interface
// (measured), and a proxy can answer with a page too. The error names the
// content type, and not the page.
func checkJSON(res *http.Response) error {
	ct := res.Header.Get("Content-Type")
	if ct == "" || strings.HasPrefix(ct, "application/json") {
		return nil
	}
	_ = res.Body.Close()
	return &Error{
		HTTPStatus: res.StatusCode,
		Statement:  -1,
		Message:    "the answer is " + ct + " and not JSON, so the server has no endpoint " + res.Request.URL.Path,
	}
}
