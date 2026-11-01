package exasol

import (
	"strings"

	"github.com/xo/dbimp"
)

// The codes of the server that the driver reads (docs/EXASOL.md, Errors).
const (
	// CodeConnection is the code of a login that the server refused. It is
	// also the code of a login on a plain ws connection, so the text decides
	// whether the credential was wrong (measured).
	CodeConnection = "08004"
	// CodeTimeout is the code of a statement that ran over the query
	// timeout.
	CodeTimeout = "R0001"
	// CodeAborted is the code of a statement that abortQuery ended.
	CodeAborted = "R0003"
)

// authFailed is the text of the answer for a wrong user, a wrong password and
// a wrong token (recorded: "login with a wrong password: send the
// credentials").
const authFailed = "authentication failed"

// Error is an error that Exasol reported: a frame with the status error, which
// holds the text and the code of the exception. The code is the SQLSTATE of the
// server, five characters, such as 42000 for a syntax error, and the text ends
// with the id of the session.
type Error struct {
	// Code is the code of the exception, such as 42000.
	Code string
	// Message is the text of the exception.
	Message string
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	if err.Code == "" {
		return "exasol: " + err.Message
	}
	return "exasol: " + err.Code + ": " + err.Message
}

// Is reports whether err matches target. It matches dbimp.ErrAuthentication
// for the code 08004 with the text of a failed authentication, which is the
// answer for a wrong user, a wrong password and a wrong token (recorded: "login
// with a wrong user: send the credentials"). The same code answers a login on a
// connection with no TLS, which is no refusal of the credential, and a missing
// privilege is 42500, so neither matches (D197).
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
	return err.Code == CodeConnection && strings.Contains(strings.ToLower(err.Message), authFailed)
}
