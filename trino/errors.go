package trino

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that Trino or Presto reported. A statement that fails
// answers HTTP 200 and a page with an error member, before any row or after
// some (measured). An error of the protocol has the status of HTTP, and a
// body of plain text or, on Presto, a page of HTML: HTTP 400 for an empty
// statement or a user that Presto lacks, HTTP 401 for a user that Trino
// lacks, HTTP 404 for a query or a token that the server does not know, and
// HTTP 410 for a nextUri that the client read before (measured).
type Error struct {
	// HTTPStatus is the status code of the response, and 0 for an error that
	// came in the body of a page.
	HTTPStatus int
	// Name is errorName, such as SYNTAX_ERROR or TABLE_NOT_FOUND, and empty
	// for an error of the protocol.
	Name string
	// Type is errorType, such as USER_ERROR, and empty for an error of the
	// protocol.
	Type string
	// Code is errorCode, a number that the product gives the name, and 0 for
	// an error of the protocol.
	Code int
	// Message is message, or the text of the answer for an error of the
	// protocol.
	Message string
	// Line and Column are the place of the error in the statement, from
	// errorLocation, and 0 when the error has no place.
	Line   int
	Column int

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "trino: "
	switch {
	case err.Name != "":
		s += err.Name + ": "
	case err.HTTPStatus != 0:
		s += strconv.Itoa(err.HTTPStatus) + ": "
	}
	s += err.Message
	if err.Line > 0 {
		s += " (line " + strconv.Itoa(err.Line) + ":" + strconv.Itoa(err.Column) + ")"
	}
	return s
}

// Unwrap returns the *dbimp.StatusError of the response, if the error has
// one.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// failure is the error member of a page. The member failureInfo holds the
// stack of the server, which the driver never shows (measured).
type failure struct {
	Message  string `json:"message"`
	Code     int    `json:"errorCode"`
	Name     string `json:"errorName"`
	Type     string `json:"errorType"`
	Location struct {
		Line   int `json:"lineNumber"`
		Column int `json:"columnNumber"`
	} `json:"errorLocation"`
}

// newFailure returns the *Error of the error member of a page.
func newFailure(f failure) *Error {
	return &Error{
		Name:    f.Name,
		Type:    f.Type,
		Code:    f.Code,
		Message: f.Message,
		Line:    f.Location.Line,
		Column:  f.Location.Column,
	}
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with what the body says.
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	return newProtocolError(serr)
}

// tags matches an element of HTML.
var tags = regexp.MustCompile(`(?s)<[^>]*>`)

// newProtocolError returns the *Error of a response whose status is not 2xx.
// The body is plain text on Trino, and text or a page of HTML on Presto
// (measured).
func newProtocolError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr}
	text := strings.TrimSpace(serr.Body)
	if strings.HasPrefix(text, "<") {
		text = strings.TrimSpace(strings.Join(strings.Fields(tags.ReplaceAllString(text, " ")), " "))
	}
	var body failure
	if json.Unmarshal([]byte(text), &body) == nil && body.Message != "" {
		text = body.Message
	}
	if text == "" {
		text = http.StatusText(serr.Code)
	}
	e.Message = text
	return e
}
