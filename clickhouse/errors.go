package clickhouse

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that ClickHouse reported. The text of each error has the
// form "Code: 60. DB::Exception: Table t does not exist. (UNKNOWN_TABLE)
// (version 26.9.2.8 (official build))" (measured). An error that comes before
// the first byte of the body has the status of HTTP that follows its code: 400
// for a syntax error, 404 for an unknown table, database or setting, 401 or 403
// for a wrong password, 500 for a type error, and 501 for a feature that is not
// there. An error after some rows has HTTP 200, or HTTP 500 on 26.9, and the
// text comes after the rows (measured).
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Code is the code of the error, such as 60, from the header
	// X-ClickHouse-Exception-Code or from the text. It is 0 when neither
	// holds one.
	Code int
	// Name is the name of the code, such as UNKNOWN_TABLE, and empty when the
	// text has none.
	Name string
	// Message is the text of the error without the code, the name and the
	// version.
	Message string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "clickhouse: "
	switch {
	case err.Name != "":
		s += err.Name + " (" + strconv.Itoa(err.Code) + "): "
	case err.Code != 0:
		s += strconv.Itoa(err.Code) + ": "
	default:
		s += strconv.Itoa(err.HTTPStatus) + ": "
	}
	return s + err.Message
}

// Unwrap returns the *dbimp.StatusError of the response, if the error has
// one.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// The codes of the errors that name a refused credential (D197). Code 194 is
// the wrong password of the user default, and code 516 is the wrong password of
// another user (measured). Code 497, ACCESS_DENIED, is a missing privilege, and
// it is not one of them.
const (
	codeAuthRequired = 194
	codeAuthFailed   = 516
)

// Is reports whether target is dbimp.ErrAuthentication, which the error
// matches when the server refused the credential: the code 194 or 516, or HTTP
// 401 (D197). A missing privilege, code 497 with HTTP 403, does not match.
func (err *Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.authRefused()
}

// authRefused reports whether the fields that the server sent say that it
// refused the credential.
func (err *Error) authRefused() bool {
	return err.Code == codeAuthRequired || err.Code == codeAuthFailed || err.HTTPStatus == http.StatusUnauthorized
}

// headerCode is the header of a response that holds the code of an error.
const headerCode = "X-Clickhouse-Exception-Code"

// codeText matches the start of the text of an error, and nameText matches the
// end of it.
var (
	codeText = regexp.MustCompile(`^Code: (\d+)\. (?:DB::Exception: )?`)
	nameText = regexp.MustCompile(`(?s) \(([A-Z][A-Z0-9_]+)\)(?: \(version [^\n]*\))?\s*$`)
)

// checkStatus returns nil if the status of res is 200. Any other status is an
// error, even when rows came before it, so it closes the body and returns an
// *Error with what the body says (D176).
func checkStatus(res *http.Response) error {
	if res.StatusCode == http.StatusOK {
		return nil
	}
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	e := newError(serr.Body, res.Header.Get(headerCode))
	e.HTTPStatus, e.status = serr.Code, serr
	if serr.Code >= 200 && serr.Code < 300 {
		// The status is 2xx and not 200, which no answer of the server is.
		e.Message = "the status is " + strconv.Itoa(serr.Code) + ", and the driver reads only 200: " + e.Message
	}
	return e
}

// newError returns the *Error that the text of an error holds. The text can
// follow rows, which an answer of HTTP 500 on 26.9 writes before it (measured),
// so the error starts at the first line that starts with Code. code is the
// header of the response with the code, or "".
func newError(body, code string) *Error {
	text := jsonText(strings.TrimSpace(body))
	if !strings.HasPrefix(text, "Code: ") {
		if i := strings.Index(text, "\nCode: "); i >= 0 {
			text = text[i+1:]
		}
	}
	e := &Error{}
	e.Code, _ = strconv.Atoi(code)
	if m := codeText.FindStringSubmatchIndex(text); m != nil {
		if n, err := strconv.Atoi(text[m[2]:m[3]]); err == nil && e.Code == 0 {
			e.Code = n
		}
		text = text[m[1]:]
	}
	if m := nameText.FindStringSubmatchIndex(text); m != nil {
		e.Name = text[m[2]:m[3]]
		text = text[:m[0]]
	}
	e.Message = strings.TrimSpace(text)
	if e.Message == "" {
		e.Message = "the server gave no text"
	}
	return e
}

// jsonText returns the text of the error that an answer holds as the last row of
// its format, such as ["Code: 60. DB::Exception: ..."], which the server writes
// after the two lines of the names and the types when the setting
// http_write_exception_in_output_format is 1 (measured on 25.3 and 25.8). The
// driver sets it to 0, and a statement whose settings the caller replaced can
// bring it back. Any other text is returned as it is.
func jsonText(text string) string {
	if !strings.HasPrefix(text, "[") {
		return text
	}
	lines := strings.Split(text, "\n")
	var row []string
	if json.Unmarshal([]byte(lines[len(lines)-1]), &row) == nil && len(row) == 1 && strings.HasPrefix(row[0], "Code: ") {
		return row[0]
	}
	return text
}
