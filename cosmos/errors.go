package cosmos

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// Error is an error that Cosmos DB reported. The body of an error has code
// and message. On the hosted service the message is text that holds a JSON
// object and a line ActivityId, such as
// Message: {"errors":[{"code":"SC1001",...}]}, and the driver reads the
// object to get the code and the text of a syntax error (recorded: "a syntax
// error"). The emulator writes the object as the body (recorded). The driver
// sends no request again after an error, so HTTP 429 reaches the caller with
// the wait that the server asks for in RetryAfter (D8 and D190).
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// SubStatus is the header X-Ms-Substatus, such as 1004 for a query across
	// partitions that the gateway cannot serve. It is 0 when the response has
	// none.
	SubStatus int
	// Code is the code of the first error in the message, such as SC1001, and
	// else the code of the body, such as BadRequest. It is empty when the
	// answer has none.
	Code string
	// Message is the text of the error, or the text of the answer when it
	// has none.
	Message string
	// RetryAfter is the wait that the header X-Ms-Retry-After-Ms names. It
	// is 0 when the response has none. A response of HTTP 429 did not run
	// the request, so a caller can send it again after the wait (recorded:
	// "lead: a burst of reads, number 1").
	RetryAfter time.Duration

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "cosmos: " + strconv.Itoa(err.HTTPStatus)
	if err.SubStatus != 0 {
		s += "." + strconv.Itoa(err.SubStatus)
	}
	if err.Code != "" {
		s += " " + err.Code
	}
	return s + ": " + err.Message
}

// Unwrap returns the *dbimp.StatusError of the response.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with what the headers and the body say.
func checkStatus(res *http.Response) error {
	sub, _ := strconv.Atoi(res.Header.Get("X-Ms-Substatus"))
	var wait time.Duration
	if ms, err := strconv.ParseFloat(res.Header.Get("X-Ms-Retry-After-Ms"), 64); err == nil && ms > 0 {
		wait = time.Duration(ms * float64(time.Millisecond))
	}
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	e := newError(serr)
	e.SubStatus, e.RetryAfter = sub, wait
	return e
}

// body is the form of the body of an error, and of the object that the
// message of the hosted service holds. The emulator writes errors as a list
// of objects and Errors as a list of text, and the hosted service writes
// both forms inside the message (recorded). A code is text or a number, such
// as 4001.
type body struct {
	Code    jsontext.Value `json:"code"`
	Message string         `json:"message"`
	Lower   []detail       `json:"errors"`
	Upper   []string       `json:"Errors"`
}

type detail struct {
	Code    jsontext.Value `json:"code"`
	Message string         `json:"message"`
}

// newError returns the *Error of a response whose status is not 2xx.
func newError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr}
	text := strings.TrimSpace(serr.Body)
	var b body
	if json.Unmarshal([]byte(text), &b) != nil {
		e.Message = http.StatusText(serr.Code)
		if text != "" && !strings.HasPrefix(text, "<") {
			e.Message = text
		}
		return e
	}
	e.Code, e.Message = codeText(b.Code), firstLine(b.Message)
	// The message of the hosted service is "Message: " and a JSON object
	// that holds the errors, and then the line ActivityId. The message of a
	// refusal of the gateway is text, with no object.
	inner := b
	if rest, ok := strings.CutPrefix(b.Message, "Message: "); ok {
		inner = body{}
		if json.Unmarshal([]byte(firstLine(rest)), &inner) != nil {
			inner = body{}
			e.Message = firstLine(rest)
		}
	}
	switch {
	case len(inner.Lower) > 0:
		if c := codeText(inner.Lower[0].Code); c != "" {
			e.Code = c
		}
		if m := inner.Lower[0].Message; m != "" {
			e.Message = m
		}
	case len(inner.Upper) > 0:
		e.Message = inner.Upper[0]
	}
	if e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}

// codeText returns the text of a code, which the server writes as a string
// or as a number.
func codeText(v jsontext.Value) string {
	switch v.Kind() {
	case '"':
		s, _ := dbimp.String(v)
		return s
	case '0':
		return string(v)
	}
	return ""
}

// firstLine returns s up to its first line break.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
