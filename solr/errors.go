package solr

import (
	"errors"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that Solr reported. A statement that fails answers HTTP
// 200 with the tuple EOF that holds EXCEPTION, before any row or after some
// (measured). A wrong password is HTTP 401, a request that the role does not
// allow is HTTP 403, and a statement that the SQL layer cannot plan can be
// HTTP 500, each with a page of HTML (measured).
type Error struct {
	// HTTPStatus is the status code of the response. It is 200 for an
	// exception that came in the body.
	HTTPStatus int
	// Message is EXCEPTION, or the title of the page of HTML, or the status
	// text of the response when it has neither.
	Message string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "solr: "
	if err.HTTPStatus != http.StatusOK {
		s += strconv.Itoa(err.HTTPStatus) + ": "
	}
	return s + err.Message
}

// Unwrap returns the *dbimp.StatusError of the response, or nil for an
// exception of the body.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// ErrCut is the error of an answer that ends before its tuple EOF. The
// server ends an answer so when the connection breaks, and after some rows it
// comes wrapped with dbimp.ErrIncomplete (D107 and D166).
const ErrCut dbimp.Error = "the answer was cut short"

// Is reports whether err matches target. It matches dbimp.ErrAuthentication
// when the status is HTTP 401, which is a wrong password (recorded: "a wrong password"). Every other refusal does not match, such as HTTP 403 for a missing
// permission (D197).
func (err *Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.authRefused()
}

// authRefused reports whether the status is HTTP 401.
func (err *Error) authRefused() bool {
	return err.HTTPStatus == http.StatusUnauthorized
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with what the body says.
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	return newError(serr)
}

// title matches the title of the page of HTML that the server of Solr
// answers an error with, such as "Error 401 require authentication".
var title = regexp.MustCompile(`(?is)<title>\s*(?:Error\s+\d+\s*)?(.*?)\s*</title>`)

// newError returns the *Error of a response whose status is not 2xx. The
// body is a page of HTML, and the title of the page holds the message
// (measured).
func newError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr, Message: http.StatusText(serr.Code)}
	text := strings.TrimSpace(serr.Body)
	if m := title.FindStringSubmatch(text); m != nil {
		if msg := strings.TrimSpace(html.UnescapeString(m[1])); msg != "" {
			e.Message = msg
		}
	} else if text != "" && !strings.HasPrefix(text, "<") {
		e.Message = text
	}
	return e
}
