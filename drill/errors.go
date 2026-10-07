package drill

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that Drill reported. Drill answers most errors of a
// statement with HTTP 200 and queryState FAILED, such as a syntax error, an
// unknown table and a permission error. With the verbose option, which the
// driver sends, the answer holds the message. A request that the server
// cannot start gets HTTP 500 and "Query submission failed" with no reason,
// a body that is not JSON gets HTTP 400 in plain text, and a wrong password
// gets HTTP 307 to the login page with an empty body (measured).
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// QueryID is the queryId of the query, or empty when the answer has none.
	QueryID string
	// Kind is the start of the message of Drill, such as VALIDATION ERROR,
	// PERMISSION ERROR or PLAN ERROR, and empty when the message has none.
	Kind string
	// Exception is the name of the Java class of the error, such as
	// org.apache.calcite.runtime.CalciteContextException, and empty when the
	// answer has none.
	Exception string
	// Message is the message of the server, or the text of the status when
	// the answer has none.
	Message string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "drill: "
	if err.HTTPStatus != http.StatusOK {
		s += strconv.Itoa(err.HTTPStatus) + ": "
	}
	return s + err.Message
}

// Unwrap returns the *dbimp.StatusError of the response, if the status was
// not 2xx.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// ErrLogin is the error of a request that the server sends to its login page,
// which is the answer to a wrong user or a wrong password (measured).
const ErrLogin dbimp.Error = "the server asked for a login"

// Is reports whether err is target, for ErrLogin.
func (err *Error) Is(target error) bool {
	return target == ErrLogin && err.HTTPStatus == http.StatusTemporaryRedirect
}

// kindPattern matches the start of the message of Drill, such as
// "VALIDATION ERROR: ".
var kindPattern = regexp.MustCompile(`^([A-Z][A-Z ]*ERROR): `)

// failure returns the *Error of a query that ended FAILED, with the message
// that the answer or the profile gave.
func failure(id, exception, message string) *Error {
	e := &Error{HTTPStatus: http.StatusOK, QueryID: id, Exception: exception, Message: message}
	if e.Message == "" {
		e.Message = "the query failed, and the server gave no reason"
	}
	if m := kindPattern.FindStringSubmatch(e.Message); m != nil {
		e.Kind = m[1]
	}
	return e
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with what the body says.
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	return newError(serr, res.Header.Get("Location"))
}

// newError returns the *Error of a response whose status is not 2xx. The
// body is a JSON object with errorMessage, plain text, or a page of HTML
// (measured).
func newError(serr *dbimp.StatusError, location string) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr, Message: http.StatusText(serr.Code)}
	text := strings.TrimSpace(serr.Body)
	var body struct {
		Message string `json:"errorMessage"`
	}
	switch {
	case serr.Code == http.StatusTemporaryRedirect && strings.Contains(location, "/mainLogin"):
		e.Message = "the server sent the request to its login page: the user or the password is wrong"
	case json.Unmarshal([]byte(text), &body) == nil && body.Message != "":
		e.Message = body.Message
	case text != "" && !strings.HasPrefix(text, "<") && !strings.HasPrefix(text, "{"):
		e.Message = text
	}
	return e
}
