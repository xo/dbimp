package drill_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/drill"
)

// TestNoRetry holds that HTTP 429 and HTTP 503 are errors that reach the
// caller, and that the driver sends the request once (D8).
func TestNoRetry(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		var mu sync.Mutex
		hits := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			hits++
			mu.Unlock()
			w.Header().Set("Retry-After", "1")
			http.Error(w, "busy", status)
		}))
		db, err := sql.Open(drill.Name, strings.Replace(srv.URL, "http://", "drill://", 1))
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.ExecContext(t.Context(), "SELECT 1")
		se, ok := errors.AsType[*dbimp.StatusError](err)
		if !ok || se.Code != status {
			t.Errorf("HTTP %d: the error is %v, want one that wraps a *dbimp.StatusError", status, err)
		}
		mu.Lock()
		if hits != 1 {
			t.Errorf("HTTP %d: the server got %d requests, want 1", status, hits)
		}
		mu.Unlock()
		db.Close()
		srv.Close()
	}
}

// TestNothingSentForABadStatement holds that a statement whose arguments do not
// match sends no request, and that a context that ended sends none.
func TestNothingSentForABadStatement(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	if _, err := db.ExecContext(t.Context(), "SELECT ?, ?", int64(1)); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a missing argument gave %v, want dbimp.ErrArguments", err)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT 'x"+"'' ?", int64(1)); !errors.Is(err, dbimp.ErrArguments) && !errors.Is(err, dbimp.ErrUnterminated) {
		t.Errorf("an argument for a literal gave %v, want an error of the arguments", err)
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
	ended, stop := context.WithCancel(t.Context())
	stop()
	if _, err := db.ExecContext(ended, "SELECT a FROM t"); err == nil {
		t.Error("a context that ended gave no error")
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v for a context that ended, want nothing", reqs)
	}
}

// TestTwoQueriesAtOnce holds that two queries run at the same time on one
// database, each on a connection of its own.
func TestTwoQueriesAtOnce(t *testing.T) {
	t.Parallel()
	var wg sync.WaitGroup
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		arrived <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, answer("q"))
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(drill.Name, strings.Replace(srv.URL, "http://", "drill://", 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for range 2 {
		wg.Go(func() {
			var a int
			if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&a); err != nil {
				t.Error(err)
			}
		})
	}
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			t.Fatal("the second query did not reach the server while the first one ran")
		}
	}
	close(release)
	wg.Wait()
}

// TestConnectorOwnsItsTransport holds that Close of the connector lets go of
// the idle connections, so that no goroutine is left running (step 12).
func TestConnectorOwnsItsTransport(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, answer("q"))
	}))
	t.Cleanup(srv.Close)
	cfg, err := drill.ParseDSN(strings.Replace(srv.URL, "http://", "drill://", 1))
	if err != nil {
		t.Fatal(err)
	}
	c := drill.NewConnector(*cfg)
	db := sql.OpenDB(c)
	var a int
	if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("closing the connector a second time: %v", err)
	}
}
