package hive

import (
	"errors"
	"net/http"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that HiveServer2 reported. A statement that does not
// compile fails at ExecuteStatement with HTTP 200 and a TStatus whose status
// code is 3. A statement that fails while it runs shows in the state ERROR of
// GetOperationStatus, with the same fields. A request that the server refused
// has another HTTP status, such as 401 for a request with no credential, and a
// call that the server cannot run is a Thrift exception (measured, docs/HIVE.md,
// Errors).
type Error struct {
	// SQLState is the SQLSTATE of the server, or "".
	SQLState string
	// Code is the native code of the server, or 0.
	Code int32
	// Message is the message of the server.
	Message string
	// Info holds the info messages of the status, such as "Server-side error;
	// please check HS2 logs.".
	Info []string
	// HTTPStatus is the status code of the response, or 0 for an error that
	// arrived with HTTP 200.
	HTTPStatus int

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	return "hive: " + err.Message
}

// ErrVerbose returns the error as psql writes it for \errverbose: the SQLSTATE,
// or the native code when there is none, and the message on the first line, and
// then each field that the server sent, one on each line with its name in upper
// case. A caller that does not import this module reads it through an interface
// of its own (D231).
func (err *Error) ErrVerbose(_ string) string {
	var code any = err.SQLState
	if err.SQLState == "" {
		code = err.Code
	}
	pairs := []any{"HTTP STATUS", err.HTTPStatus}
	if err.SQLState != "" {
		pairs = append(pairs, "ERROR CODE", int(err.Code))
	}
	if len(err.Info) > 0 {
		pairs = append(pairs, "DETAIL", strings.Join(err.Info, "\n"))
	}
	return dbimp.Verbose(code, err.Message, "", pairs...)
}

// Unwrap returns the *dbimp.StatusError of a response whose status is not 2xx,
// or nil.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// Is reports whether err matches target. It matches dbimp.ErrAuthentication for
// HTTP 401, which Hive answers to a request with no credential (recorded: "no
// credentials"). Both servers accept any user and any password, so no other
// answer means a refused credential (D197).
func (err *Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.authRefused()
}

// AuthenticationRefused reports whether the server refused the credential. It
// returns what Is reports for dbimp.ErrAuthentication, and a caller that does
// not import this module reads it through an interface of its own (D227).
func (err *Error) AuthenticationRefused() bool {
	return err.Is(dbimp.ErrAuthentication)
}

// authRefused reports whether the server refused the credential.
func (err *Error) authRefused() bool {
	return err.HTTPStatus == http.StatusUnauthorized
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes the
// body, and returns an *Error with the text of the body.
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	e := &Error{HTTPStatus: serr.Code, Message: strings.TrimSpace(serr.Body), status: serr}
	if e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
