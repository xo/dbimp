package dbimp

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Error is an error of this package.
type Error string

// Error satisfies the error interface.
func (err Error) Error() string {
	return string(err)
}

// AuthenticationRefused reports whether err is ErrAuthentication. A caller
// that does not import this module reads it through an interface of its own,
// and so a driver can wrap ErrAuthentication with %w (D227).
func (err Error) AuthenticationRefused() bool {
	return err == ErrAuthentication
}

// Error values.
const (
	// ErrNotSupported is the error for a feature that the database does not
	// have, such as a transaction (D20).
	ErrNotSupported Error = "not supported"
	// ErrScheme is the error for a DSN whose scheme is not the name of the
	// driver (D35).
	ErrScheme Error = "wrong scheme"
	// ErrUnknownKey is the error for a key in the query of a DSN that the
	// driver does not know (D27).
	ErrUnknownKey Error = "unknown key"
	// ErrRepeatedKey is the error for a key that appears more than once in
	// the query of a DSN (D27).
	ErrRepeatedKey Error = "repeated key"
	// ErrInvalidValue is the error for a value that has the wrong form.
	ErrInvalidValue Error = "invalid value"
	// ErrExtraColumn is the error for a row that has a column that the first
	// row does not have (D18).
	ErrExtraColumn Error = "column not in the first row"
	// ErrColumnCount is the error for a row that has the wrong number of
	// columns.
	ErrColumnCount Error = "wrong number of columns"
	// ErrIncomplete is the error for a result set that failed, or that the
	// server cut short, after at least one of its rows reached the caller
	// (D21 and D107). A failure before the first row does not wrap it.
	ErrIncomplete Error = "result is incomplete"
	// ErrUnterminated is the error for a statement that ends inside a
	// literal, a quoted identifier or a comment.
	ErrUnterminated Error = "unterminated literal or comment"
	// ErrArguments is the error for arguments that do not match the
	// placeholders of a statement (D34).
	ErrArguments Error = "arguments do not match the placeholders"
	// ErrAuthentication is the error that the error of a driver matches with
	// errors.Is when the server refused the credential: a wrong password, key
	// or token. It never matches a refusal for lack of a permission (D197).
	ErrAuthentication Error = "authentication refused"
)

// maxErrorBody is the most of the body of a response that a StatusError
// keeps.
const maxErrorBody = 64 << 10

// StatusError is the error for a response whose status is not 2xx. HTTP 429
// and HTTP 503 are a StatusError too, and nothing retries them (D8).
type StatusError struct {
	// Code is the status code of the response.
	Code int
	// Body is the start of the body of the response, at most 64 KiB.
	Body string
}

// Error satisfies the error interface.
func (err *StatusError) Error() string {
	text := http.StatusText(err.Code)
	if body := strings.TrimSpace(err.Body); body != "" {
		return fmt.Sprintf("status %d %s: %s", err.Code, text, body)
	}
	return fmt.Sprintf("status %d %s", err.Code, text)
}

// Is reports whether err matches target. It matches ErrAuthentication for HTTP
// 401, which is the status for a request with no valid credential. HTTP 403 is
// not matched, because a server also sends it for a missing permission, so the
// error of a driver decides it (D197).
func (err *StatusError) Is(target error) bool {
	return target == ErrAuthentication && err.Code == http.StatusUnauthorized
}

// AuthenticationRefused reports whether the status is HTTP 401. It returns what
// Is reports for ErrAuthentication (D227).
func (err *StatusError) AuthenticationRefused() bool {
	return err.Is(ErrAuthentication)
}

// CheckStatus returns nil if the status of res is 2xx. Otherwise it reads at
// most 64 KiB of the body, closes the body, and returns a *StatusError.
func CheckStatus(res *http.Response) error {
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	if err != nil {
		return fmt.Errorf("reading the body of status %d: %w", res.StatusCode, err)
	}
	return &StatusError{Code: res.StatusCode, Body: string(body)}
}

// Verbose returns the text of an error for the command \errverbose of usql, in
// the layout that libpq writes in its verbose mode and that psql shows (D231):
//
//	ERROR:  CODE: message
//	LINE 1: select blah
//	               ^
//	DETAIL:  text
//
// The first line is "ERROR:" and two spaces, then the code and a colon and a
// space if code is set, then the message. code is a string or an int, and the
// code is left out when it is "" or 0. position is the text that Position
// returns, and it is left out when it is "". The arguments after position are
// pairs of an upper case name and a value, and each pair whose value is set
// becomes a line "NAME:  value". A value is set when it is not the zero value of
// its type. The text has no newline at its end. Verbose returns "" when there
// is no code, no message, no position and no pair that is set.
func Verbose(code any, message, position string, pairs ...any) string {
	var c string
	switch v := code.(type) {
	case string:
		c = v
	case int:
		if v != 0 {
			c = strconv.Itoa(v)
		}
	}
	var lines []string
	if c != "" || message != "" {
		first := "ERROR:  "
		if c != "" {
			first += c + ": "
		}
		lines = append(lines, first+message)
		if position != "" {
			lines = append(lines, position)
		}
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		name, _ := pairs[i].(string)
		var text string
		switch v := pairs[i+1].(type) {
		case string:
			text = v
		case int:
			if v != 0 {
				text = strconv.Itoa(v)
			}
		case time.Duration:
			if v != 0 {
				text = v.String()
			}
		case bool:
			if v {
				text = "true"
			}
		case fmt.Stringer:
			text = v.String()
		}
		if text != "" {
			lines = append(lines, name+":  "+text)
		}
	}
	return strings.Join(lines, "\n")
}

// Position returns the line of query that line points at, with the text
// "LINE n: " before it, and a caret under the character at column, on a line of
// its own, as libpq writes them after the message. line and column count from
// 1, and column counts characters. It returns the line with no caret when
// column is not set, and "" when line is not in query.
func Position(query string, line, column int) string {
	if line < 1 {
		return ""
	}
	lines := strings.Split(query, "\n")
	if line > len(lines) {
		return ""
	}
	text := strings.TrimSuffix(lines[line-1], "\r")
	prefix := "LINE " + strconv.Itoa(line) + ": "
	if column < 1 {
		return prefix + text
	}
	return prefix + text + "\n" + strings.Repeat(" ", len(prefix)+column-1) + "^"
}
