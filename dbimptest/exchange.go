// Package dbimptest holds the test helpers that every driver in
// github.com/xo/dbimp shares. It replays and records the responses of a real
// server, checks that a test leaves no goroutine behind, runs the contract
// of docs/DRIVER.md against a driver, and compares the type table and the
// interface table of a driver with its document, or writes them.
//
// Only a test imports this package. It imports testing and
// net/http/httptest, which a driver must not pull into the build of a
// consumer, so it is not in the root package (D37 in docs/decisions/).
package dbimptest

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"unicode/utf8"
)

// Exchange is one request to a real server and its response, as a file
// under testdata/<driver>/ holds it.
type Exchange struct {
	Request  Request  `json:"request"`
	Response Response `json:"response"`
}

// Request is the recorded part of a request. A body that is text is in
// Body, and a binary body, such as one in CBOR, is in Binary, which the file
// holds as base64.
type Request struct {
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Query  string      `json:"query,omitzero"`
	Header http.Header `json:"header,omitzero"`
	Body   string      `json:"body,omitzero"`
	Binary []byte      `json:"binary,omitzero"`
}

// Content returns the bytes of the body.
func (r Request) Content() []byte {
	if len(r.Binary) > 0 {
		return r.Binary
	}
	return []byte(r.Body)
}

// Response is the recorded part of a response. A body that is text is in
// Body, and a binary body is in Binary, as for a Request. Truncated is true
// when the server closed the connection before the end of the body, as
// InfluxDB 3 does for an error after some rows. Body then holds what arrived.
type Response struct {
	Status    int         `json:"status"`
	Header    http.Header `json:"header,omitzero"`
	Body      string      `json:"body"`
	Binary    []byte      `json:"binary,omitzero"`
	Truncated bool        `json:"truncated,omitzero"`
}

// Content returns the bytes of the body.
func (r Response) Content() []byte {
	if len(r.Binary) > 0 {
		return r.Binary
	}
	return []byte(r.Body)
}

// body returns the text and the binary form of b: the text if b is valid
// UTF-8, and the binary form otherwise.
func body(b []byte) (string, []byte) {
	if utf8.Valid(b) {
		return string(b), nil
	}
	return "", b
}

// ReadExchange reads the exchange in the file at path.
func ReadExchange(path string) (*Exchange, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading an exchange: %w", err)
	}
	var ex Exchange
	if err := json.Unmarshal(b, &ex); err != nil {
		return nil, fmt.Errorf("reading the exchange %s: %w", path, err)
	}
	return &ex, nil
}

// Match reports whether a request that a fake server received, with its
// body, is the request of ex.
type Match func(r *http.Request, body []byte, ex *Exchange) bool

// DefaultMatch matches the method, the path and the query exactly. It
// matches two JSON bodies if their canonical forms are equal, and any other
// two bodies if their bytes are equal.
func DefaultMatch(r *http.Request, body []byte, ex *Exchange) bool {
	if r.Method != ex.Request.Method || r.URL.Path != ex.Request.Path || r.URL.RawQuery != ex.Request.Query {
		return false
	}
	return sameBody(body, ex.Request.Content())
}

func sameBody(a, b []byte) bool {
	va, vb := jsontext.Value(bytes.Clone(a)), jsontext.Value(bytes.Clone(b))
	if va.Canonicalize() == nil && vb.Canonicalize() == nil {
		return bytes.Equal(va, vb)
	}
	return bytes.Equal(a, b)
}

// Replay starts a fake server that answers each request with the response
// of the first exchange in dir that match accepts. If match is nil, it uses
// DefaultMatch. A request that no exchange matches fails the test. Replay
// reads every file in dir except the manifest, the script of requests and
// the survey, and closes the server when the test ends.
func Replay(t *testing.T, dir string, match Match) *httptest.Server {
	t.Helper()
	return replay(t, dir, "*.json", match)
}

// ReplayRelease is Replay for the exchanges of one release only, whose files
// start with the name of the release, as dbrun names it. A test uses it when
// releases answer the same request in different ways.
func ReplayRelease(t *testing.T, dir, release string, match Match) *httptest.Server {
	t.Helper()
	return replay(t, dir, release+"-*.json", match)
}

func replay(t *testing.T, dir, pattern string, match Match) *httptest.Server {
	t.Helper()
	if match == nil {
		match = DefaultMatch
	}
	paths, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatalf("finding the exchanges in %s: %v", dir, err)
	}
	var exchanges []*Exchange
	for _, path := range paths {
		if base := filepath.Base(path); base == ManifestName || base == RequestsName || base == FeaturesName {
			continue
		}
		ex, err := ReadExchange(path)
		if err != nil {
			t.Fatal(err)
		}
		exchanges = append(exchanges, ex)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading a request to the fake server: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		i := slices.IndexFunc(exchanges, func(ex *Exchange) bool {
			return match(r, body, ex)
		})
		if i < 0 {
			t.Errorf("no exchange in %s matches %s %s %s", dir, r.Method, r.URL, body)
			http.Error(w, "no exchange matches", http.StatusTeapot)
			return
		}
		res := exchanges[i].Response
		for key, vals := range res.Header {
			for _, val := range vals {
				w.Header().Add(key, val)
			}
		}
		w.Header().Del("Content-Length")
		w.WriteHeader(res.Status)
		if _, err := w.Write(res.Content()); err != nil {
			t.Errorf("writing a response from the fake server: %v", err)
		}
		if res.Truncated {
			// The client gets what the server sent, and then the connection
			// closes before the end of the body, as it did on the server.
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Errorf("flushing a response from the fake server: %v", err)
			}
			panic(http.ErrAbortHandler)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
