package bigquery

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/xo/dbimp"
)

// The sentinel errors of the driver. Each one wraps into a failure with
// errors.Is (D6).
const (
	// ErrCut is the error of a result that ends before the count of rows that
	// the server named in totalRows. The service runs the whole statement before
	// it sends the first page, so only a later page can be cut (D21 and D189).
	// After a row, it comes wrapped with dbimp.ErrIncomplete (D107).
	ErrCut dbimp.Error = "the answer was cut short"
	// ErrCanceled is the error of a job that the service ended with the reason
	// stopped, when the context of the statement did not end. Someone else
	// canceled it (recorded: bigquery-212).
	ErrCanceled dbimp.Error = "the job was canceled"
	// ErrNoCredential is the error of a connector that has no way to log in: it
	// has no key file, no access token, and no disable_auth.
	ErrNoCredential dbimp.Error = "no credential" //nolint:gosec // The text is a message, not a secret.
)

// reasonStopped is the reason of a job that someone canceled (recorded:
// bigquery-212).
const reasonStopped = "stopped"

// Error is an error that BigQuery reported. Every answer that is not a
// result has a JSON object with an error member, which holds the HTTP code, the
// message, a list of errors with a reason, and a status name (recorded:
// bigquery-181 and bigquery-182). An error of a query comes before any row,
// because the service runs the whole statement before it answers.
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Reason is the reason of the first entry of errors, such as invalidQuery,
	// notFound or accessDenied, and empty when the answer has none.
	Reason string
	// Status is status, such as INVALID_ARGUMENT, and empty when the answer has
	// none.
	Status string
	// Message is message, or the text of the answer when it has none.
	Message string
	// Location is the location of the first entry of errors, such as q for the
	// text of a query, and empty when the answer has none.
	Location string
	// JobID is the id of the job, and empty when the driver did not know it.
	JobID string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "bigquery: "
	switch {
	case err.Reason != "" && err.Status != "":
		s += err.Reason + " (" + err.Status + "): "
	case err.Reason != "":
		s += err.Reason + ": "
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

// Is reports whether target is ErrCanceled, which the reason stopped names.
func (err *Error) Is(target error) bool {
	return target == ErrCanceled && (err.Reason == reasonStopped || err.Status == "CANCELLED")
}

// checkStatus returns nil if the status of res is one of the codes in ok.
// Otherwise it closes the body, and returns an *Error with what the body says.
func checkStatus(res *http.Response, ok ...int) error {
	if slices.Contains(ok, res.StatusCode) {
		return nil
	}
	err := dbimp.CheckStatus(res)
	if err == nil {
		// The status is 2xx and is not one that the driver reads.
		_ = res.Body.Close()
		return &Error{HTTPStatus: res.StatusCode, Message: "the status is " + http.StatusText(res.StatusCode) + ", which the driver does not read"}
	}
	serr, isStatus := errors.AsType[*dbimp.StatusError](err)
	if !isStatus {
		return err
	}
	return newError(serr)
}

// newError returns the *Error of a response whose status is not one that the
// driver reads. The body is a JSON object, or text such as a page of HTML from
// a proxy (recorded: bigquery-195).
func newError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr}
	var body struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Errors  []struct {
				Reason   string `json:"reason"`
				Location string `json:"location"`
			} `json:"errors"`
		} `json:"error"`
	}
	text := strings.TrimSpace(serr.Body)
	if json.Unmarshal([]byte(text), &body) != nil || body.Error.Message == "" && len(body.Error.Errors) == 0 {
		e.Message = http.StatusText(serr.Code)
		if text != "" && !strings.HasPrefix(text, "<") {
			e.Message = text
		}
		return e
	}
	e.Status = body.Error.Status
	if len(body.Error.Errors) > 0 {
		e.Reason, e.Location = body.Error.Errors[0].Reason, body.Error.Errors[0].Location
	}
	if e.Message = body.Error.Message; e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
