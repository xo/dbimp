package dynamodb

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that DynamoDB reported. An error answers HTTP 400 with a
// JSON object that names its type in __type and its text in Message, such as
// com.amazon.coral.validate#ValidationException for a syntax error and
// com.amazonaws.dynamodb.v20120810#ResourceNotFoundException for an unknown
// table (recorded). DynamoDB Local answers HTTP 500 and InternalFailure for a
// body that it cannot read. The driver sends no request again after an
// error, so HTTP 429 and HTTP 503 reach the caller (D8).
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Type is the name of the type of the error, such as ValidationException,
	// without its namespace. It is empty when the answer has none.
	Type string
	// Message is the text of the error, or the text of the answer when it
	// has none.
	Message string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "dynamodb: "
	if err.Type != "" {
		s += err.Type + ": "
	} else {
		s += strconv.Itoa(err.HTTPStatus) + ": "
	}
	return s + err.Message
}

// Unwrap returns the *dbimp.StatusError of the response.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
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

// newError returns the *Error of a response whose status is not 2xx. The
// body is a JSON object with __type and Message, which Alternator writes as
// message, or text (recorded).
func newError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr}
	var body struct {
		Type    string `json:"__type"`
		Message string `json:"Message"`
		Lower   string `json:"message"`
	}
	text := strings.TrimSpace(serr.Body)
	if json.Unmarshal([]byte(text), &body) != nil {
		e.Message = http.StatusText(serr.Code)
		if text != "" && !strings.HasPrefix(text, "<") {
			e.Message = text
		}
		return e
	}
	if _, name, ok := strings.Cut(body.Type, "#"); ok {
		e.Type = name
	} else {
		e.Type = body.Type
	}
	switch {
	case body.Message != "":
		e.Message = body.Message
	case body.Lower != "":
		e.Message = body.Lower
	default:
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
