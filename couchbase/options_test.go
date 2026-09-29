package couchbase_test

import (
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/couchbase"
)

// TestOptionArguments holds that an Option among the arguments leaves the
// other arguments in their order, that an Option of one statement does not
// reach the next one, and that WithDatabase sets query_context (D109).
func TestOptionArguments(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		mu.Lock()
		bodies = append(bodies, m)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"signature":"json","results":[1],"status":"success"}`)
	}))
	defer srv.Close()
	db := openAt(t, srv.URL)
	defer db.Close()
	db.SetMaxOpenConns(1)
	var one int64
	if err := db.QueryRowContext(t.Context(), "SELECT RAW $1 + $2", "a", couchbase.WithTimeout(time.Second), "b").Scan(&one); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT RAW $1", "c", couchbase.WithDatabase("default:b.s")).Scan(&one); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("the server received %d requests, want 2", len(bodies))
	}
	args, _ := bodies[0]["args"].([]any)
	if !slices.Equal(args, []any{"a", "b"}) || bodies[0]["timeout"] != "1s" {
		t.Errorf("the first body is %v, want the args a and b, and the timeout 1s", bodies[0])
	}
	if _, ok := bodies[1]["timeout"]; ok || bodies[1]["query_context"] != "default:b.s" {
		t.Errorf("the second body is %v, want the query_context of WithDatabase, and no timeout, which belonged to the first statement", bodies[1])
	}
}

// TestOptionValues holds that an option with a value that the DSN refuses
// fails the statement or the transaction, and sends nothing (D109).
func TestOptionValues(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		http.Error(w, "unexpected", http.StatusTeapot)
	}))
	defer srv.Close()
	db := openAt(t, srv.URL)
	defer db.Close()
	for _, opt := range []couchbase.Option{couchbase.WithScanConsistency("at_plus"), couchbase.WithTimeout(-time.Second)} {
		if _, err := db.ExecContext(t.Context(), "SELECT RAW 1", opt); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("an option with a bad value gave %v, want dbimp.ErrInvalidValue", err)
		}
	}
	for _, opt := range []couchbase.Option{couchbase.WithDurability("all"), couchbase.WithTransactionTimeout(-time.Second)} {
		if _, err := db.BeginTx(couchbase.WithOptions(t.Context(), opt), nil); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("a transaction option with a bad value gave %v, want dbimp.ErrInvalidValue", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if n != 0 {
		t.Errorf("the server received %d requests, want none", n)
	}
}
