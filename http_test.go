package dbimp_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xo/dbimp"
)

func newRequest(t *testing.T, ctx context.Context, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// send sends req with c, and returns only the error. It closes the body of
// any response, because these tests expect none.
func send(c *http.Client, req *http.Request) error {
	res, err := dbimp.Send(c, req)
	if res != nil {
		res.Body.Close()
	}
	return err
}

func TestSendToAClosedServerIsABadConnection(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := dbimp.NewClient(dbimp.NewTransport(nil), false)
	err := send(c, newRequest(t, t.Context(), url))
	if !errors.Is(err, driver.ErrBadConn) {
		t.Errorf("Send = %v, want driver.ErrBadConn", err)
	}
}

func TestSendAfterTheServerReadTheRequestIsNotABadConnection(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		if conn, _, err := hj.Hijack(); err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	c := dbimp.NewClient(dbimp.NewTransport(nil), false)
	err := send(c, newRequest(t, t.Context(), srv.URL))
	if err == nil || errors.Is(err, driver.ErrBadConn) {
		t.Errorf("Send = %v, want an error that is not driver.ErrBadConn (D8)", err)
	}
}

func TestSendWithACancelledContext(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c := dbimp.NewClient(dbimp.NewTransport(nil), false)
	err := send(c, newRequest(t, ctx, url))
	if !errors.Is(err, context.Canceled) || errors.Is(err, driver.ErrBadConn) {
		t.Errorf("Send = %v, want context.Canceled and not driver.ErrBadConn", err)
	}
}

func TestNewClientDoesNotFollowARedirect(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.RedirectHandler("/elsewhere", http.StatusFound))
	defer srv.Close()
	c := dbimp.NewClient(dbimp.NewTransport(nil), false)
	res, err := dbimp.Send(c, newRequest(t, t.Context(), srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Errorf("the status is %d, want the redirect itself", res.StatusCode)
	}
}

func TestCheckStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"slow down"}`)
	}))
	defer srv.Close()
	c := dbimp.NewClient(dbimp.NewTransport(nil), false)
	res, err := dbimp.Send(c, newRequest(t, t.Context(), srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	err = dbimp.CheckStatus(res)
	var serr *dbimp.StatusError
	if !errors.As(err, &serr) || serr.Code != http.StatusTooManyRequests || !strings.Contains(serr.Body, "slow down") {
		t.Errorf("CheckStatus = %v, want a StatusError for 429 with the body", err)
	}
}

func TestStream(t *testing.T) {
	t.Parallel()
	s := dbimp.NewStream(io.NopCloser(strings.NewReader(`{"a":1} `)))
	if err := s.Decoder().SkipValue(); err != nil {
		t.Fatal(err)
	}
	if err := s.End(); err != nil {
		t.Errorf("End = %v", err)
	}
	first, second := s.Close(), s.Close()
	if first != nil || second != nil {
		t.Errorf("Close = %v, then %v, want nil twice", first, second)
	}
	s = dbimp.NewStream(io.NopCloser(strings.NewReader(`{"a":1} {"b":2}`)))
	if err := s.Decoder().SkipValue(); err != nil {
		t.Fatal(err)
	}
	if err := s.End(); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("End with data after the value = %v, want ErrInvalidValue", err)
	}
}
