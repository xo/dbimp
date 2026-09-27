package dbimp

import (
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"
)

// NewTransport returns the transport for one connector. It takes a proxy
// from the environment, and it bounds the dial and the TLS handshake. It
// sets no timeout for the headers of a response, because a server can send
// them only after a long query ends. The context of each request sets its
// deadline. The transport asks for gzip and decompresses it, because no
// driver sets Accept-Encoding itself.
func NewTransport(cfg *tls.Config) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         dialer.DialContext,
		TLSClientConfig:     cfg,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}
}

// NewClient returns a client for rt with no timeout, because the context of
// each request sets its deadline. If redirects is false, the client returns
// a redirect as the response and does not follow it. A driver decides in
// step 9 of docs/DRIVER.md whether it follows a redirect. When a client
// follows one to another host, net/http drops the Authorization header.
func NewClient(rt http.RoundTripper, redirects bool) *http.Client {
	c := &http.Client{Transport: rt}
	if !redirects {
		c.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return c
}

// Send sends req with c. It sends each request once, and never again after
// it can have reached the server (D8).
//
// If c made no connection for the request, such as when the dial or the TLS
// handshake failed, the error wraps driver.ErrBadConn, so that database/sql
// can try a new connection. If the context of req ended, the error wraps the
// error of the context, and never driver.ErrBadConn. The error never holds a
// password, because net/http removes it from the URL of the error.
func Send(c *http.Client, req *http.Request) (*http.Response, error) {
	var connected atomic.Bool
	trace := &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) {
			connected.Store(true)
		},
	}
	ctx := req.Context()
	res, err := c.Do(req.WithContext(httptrace.WithClientTrace(ctx, trace)))
	switch {
	case err == nil:
		return res, nil
	case ctx.Err() != nil:
		return nil, fmt.Errorf("sending the request: %w", err)
	case !connected.Load():
		return nil, fmt.Errorf("sending the request: %w: %w", driver.ErrBadConn, err)
	}
	return nil, fmt.Errorf("sending the request: %w", err)
}
