package snowflake //nolint:testpackage // The test builds a watch in the state that a race of the transport leaves, which is not exported.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestEndCancelsAfterTheContextEnded holds D183: when the context ended, and
// the transport saw it before the function of the watch started, stop returns
// true and the function never runs. end must send the cancel itself, because
// the statement can still run on the server.
func TestEndCancelsAfterTheContextEnded(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		ended error
		want  int
	}{
		{"the context ended", context.Canceled, 1},
		{"the context lives", nil, 0},
	} {
		sent := 0
		w := &watch{
			done: make(chan struct{}),
			stop: func() bool { return true },
			err:  func() error { return tt.ended },
			now:  func() { sent++ },
		}
		w.end()
		if sent != tt.want {
			t.Errorf("%s: end sent %d cancels, want %d", tt.name, sent, tt.want)
		}
	}
}

// TestNoCancelWithoutAHandle holds that rows of an answer that names no handle,
// not in the header Link and not in its members, are closed early with no cancel,
// because the driver has nothing to name.
func TestNoCancelWithoutAHandle(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		_, _ = io.WriteString(w, `{"resultSetMetaData":{"format":"jsonv2","rowType":[{"name":"A","type":"fixed","precision":1,"scale":0}]},"data":[["1"],["2"]]}`)
	}))
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL, true)
	rows, err := db.QueryContext(t.Context(), "SELECT a")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	//nolint:sqlclosecheck // The test closes the rows at a chosen point, and looks at the requests after it.
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 {
		t.Errorf("the requests are %v, want the statement only", requests)
	}
}
