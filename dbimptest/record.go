package dbimptest

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EnvRecord is the environment variable that a driver test reads to decide
// whether it records. A driver test that records runs only when it is set.
const EnvRecord = "DBIMP_RECORD"

// redacted replaces the value of a header that holds a credential.
const redacted = "REDACTED"

// Recorder is an http.RoundTripper that writes each exchange with a real
// server into a directory, as step 6 of docs/DRIVER.md requires. It removes
// the value of each header that holds a credential. It reads each response
// whole, so it is for a recording and never for a driver.
type Recorder struct {
	next    http.RoundTripper
	dir     string
	release string
	date    string

	mu        sync.Mutex
	n         int
	item      int
	principal string
	entries   []Entry
}

// NewRecorder returns a Recorder that sends each request with next and
// writes each exchange into dir. release is the release of the server, as
// dbrun names it.
func NewRecorder(dir, release string, next http.RoundTripper) *Recorder {
	return &Recorder{
		next:    next,
		dir:     dir,
		release: release,
		date:    time.Now().Format(time.DateOnly),
	}
}

// Label sets the item of step 6 and the principal of the exchanges that
// follow.
func (r *Recorder) Label(item int, principal string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.item, r.principal = item, principal
}

// RoundTrip sends req, and writes the exchange.
func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	var reqBody []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("recording a request: %w", err)
		}
		if err := req.Body.Close(); err != nil {
			return nil, fmt.Errorf("recording a request: %w", err)
		}
		reqBody = b
		req.Body = io.NopCloser(bytes.NewReader(b))
	}
	res, err := r.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resBody, err := io.ReadAll(res.Body)
	if cerr := res.Body.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("recording a response: %w", err)
	}
	res.Body = io.NopCloser(bytes.NewReader(resBody))
	ex := Exchange{
		Request: Request{
			Method: req.Method,
			Path:   req.URL.Path,
			Query:  req.URL.RawQuery,
			Header: redact(req.Header, "Authorization", "Cookie"),
			Body:   string(reqBody),
		},
		Response: Response{
			Status: res.StatusCode,
			Header: redact(res.Header, "Set-Cookie"),
			Body:   string(resBody),
		},
	}
	if err := r.write(&ex); err != nil {
		return nil, err
	}
	return res, nil
}

// WriteManifest adds the entries of the recording to the manifest of
// driver in the directory, and writes the manifest.
func (r *Recorder) WriteManifest(driver string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	path := filepath.Join(r.dir, ManifestName)
	m, err := ReadManifest(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		m = &Manifest{Driver: driver}
	case err != nil:
		return err
	}
	m.Entries = append(m.Entries, r.entries...)
	r.entries = nil
	return WriteManifest(path, m)
}

func (r *Recorder) write(ex *Exchange) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	name := fmt.Sprintf("%03d-%s.json", r.n, slug(ex.Request.Method+" "+ex.Request.Path))
	b, err := json.Marshal(ex, jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("recording %s: %w", name, err)
	}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return fmt.Errorf("recording %s: %w", name, err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, name), append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("recording %s: %w", name, err)
	}
	r.entries = append(r.entries, Entry{
		Item:      r.item,
		Principal: r.principal,
		Release:   r.release,
		Date:      r.date,
		File:      name,
	})
	return nil
}

func redact(h http.Header, keys ...string) http.Header {
	h = h.Clone()
	for _, key := range keys {
		if h.Get(key) != "" {
			h.Set(key, redacted)
		}
	}
	return h
}

func slug(s string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		switch {
		case 'a' <= r && r <= 'z', '0' <= r && r <= '9':
			return r
		case 'A' <= r && r <= 'Z':
			return r + 'a' - 'A'
		}
		return '-'
	}, s), "-")
}
