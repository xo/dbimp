package tablestore

import (
	"encoding/xml"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// ErrNoCredentials is the error of a connector whose Config holds no access key
// id and no secret, because the driver reads no credential from the environment
// (D7 and D222).
const ErrNoCredentials dbimp.Error = "the configuration has no access key id or secret"

// ErrExists and ErrNotFound are the errors of CreateTable, CreateIndex,
// DeleteTable, CreateSearchIndex, DeleteSearchIndex and ListSearchIndexes for an
// object that is there, and for an object that is not. An *Error matches them by
// its code (D225). The calls on search indexes get the code OTSParameterInvalid for
// a table that is not there, so ErrNotFound also matches that code with the text
// "table [x] does not exist" (D226).
const (
	ErrExists   dbimp.Error = "object already exists"
	ErrNotFound dbimp.Error = "object does not exist"
)

// The codes that the server gives for them.
const (
	codeAlreadyExist = "OTSObjectAlreadyExist"
	codeNotExist     = "OTSObjectNotExist"

	codeParameterInvalid = "OTSParameterInvalid"
)

// codeAuthFailed is the code of the error for a request that the server cannot
// authenticate: HTTP 403 with the text "Signature mismatch." for a wrong
// secret (recorded: "a wrong secret"). A refusal for a missing permission has
// another code, so it is no refusal of the credential (D197).
const codeAuthFailed = "OTSAuthFailed"

// Error is an error that Tablestore reported. A request that the server refuses
// answers HTTP 400 or 403 with a protobuf message that holds a code and a text,
// such as OTSParameterInvalid with the text of a syntax error (recorded: "a
// syntax error"). A request with no credentials answers HTTP 400 with the same
// two members as XML (recorded: "no credentials").
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Code is the code of the error, such as OTSParameterInvalid.
	Code string
	// Message is the text of the error.
	Message string
	// RequestID is the id that the server gave the request, in the header
	// X-Ots-Requestid. It can be empty.
	RequestID string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "tablestore: "
	switch {
	case err.Code != "":
		s += err.Code + ": "
	case err.HTTPStatus != 0:
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

// Is reports whether target is dbimp.ErrAuthentication and the server refused the
// credential (D197), or ErrExists or ErrNotFound and the code says so (D225).
func (err *Error) Is(target error) bool {
	switch target {
	case dbimp.ErrAuthentication:
		return err.Code == codeAuthFailed
	case ErrExists:
		return err.Code == codeAlreadyExist
	case ErrNotFound:
		return err.Code == codeNotExist || err.missingTable()
	}
	return false
}

// missingTable reports whether the error is the answer of the server to a call on
// a search index of a table that is not there: OTSParameterInvalid with the text
// "table [x] does not exist" (recorded: "list the search indexes of a table that is
// not there").
func (err *Error) missingTable() bool {
	return err.Code == codeParameterInvalid && strings.HasPrefix(err.Message, "table [") && strings.Contains(err.Message, "] does not exist")
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes the
// body, and returns an *Error with what the body says.
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	e := newError(serr)
	if id := res.Header.Get("X-Ots-Requestid"); id != "" {
		e.RequestID = id
	}
	return e
}

// newError returns the *Error of a response whose status is not 2xx.
func newError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr}
	text := strings.TrimSpace(serr.Body)
	if strings.HasPrefix(text, "<") {
		var body struct {
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
			ID      string `xml:"RequestID"`
		}
		if xml.Unmarshal([]byte(text), &body) == nil {
			e.Code, e.Message, e.RequestID = body.Code, body.Message, body.ID
		}
	} else if code, msg, ok := parseError([]byte(serr.Body)); ok {
		e.Code, e.Message = code, msg
	}
	if e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
