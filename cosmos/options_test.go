package cosmos_test

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/cosmos"
)

// last returns the last request that the fake server got.
func (f *fake) last(t *testing.T) (string, http.Header) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.headers) == 0 {
		t.Fatal("the fake server got no request")
	}
	return f.bodies[len(f.bodies)-1], f.headers[len(f.headers)-1]
}

// requests returns the number of requests that the fake server got.
func (f *fake) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.headers)
}

func okFake() *fake {
	return &fake{pages: map[string]page{"": {body: docs("1")}}}
}

// TestOptions holds D109: an option comes from the DSN, then from the context,
// then from an argument, and a later one wins. Each option changes what the
// driver sends, and a test with a fake server holds each one.
func TestOptions(t *testing.T) {
	t.Parallel()
	const query = "SELECT c.id FROM c"
	t.Run("the DSN", func(t *testing.T) {
		t.Parallel()
		f := okFake()
		srv := httptest.NewServer(f)
		t.Cleanup(srv.Close)
		db := openFake(t, srv.URL, "c", "pagesize=7&partitionkey=p1")
		if _, _, err := read(t, db, query); err != nil {
			t.Fatal(err)
		}
		_, h := f.last(t)
		if h.Get("X-Ms-Max-Item-Count") != "7" || h.Get("X-Ms-Documentdb-Partitionkey") != `["p1"]` || h.Get("X-Ms-Documentdb-Query-Enablecrosspartition") != "" {
			t.Errorf("the headers are %v, want the page size 7 and the partition key [\"p1\"], and no cross partition", h)
		}
	})
	t.Run("the context and then an argument", func(t *testing.T) {
		t.Parallel()
		f := okFake()
		srv := httptest.NewServer(f)
		t.Cleanup(srv.Close)
		db := openFake(t, srv.URL, "c", "pagesize=7&partitionkey=p1")
		ctx := cosmos.WithOptions(t.Context(), cosmos.WithPageSize(8), cosmos.WithPartitionKey(2))
		rows, err := db.QueryContext(ctx, query, cosmos.WithPageSize(9))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := readRows(t, rows); err != nil {
			t.Fatal(err)
		}
		_, h := f.last(t)
		if h.Get("X-Ms-Max-Item-Count") != "9" || h.Get("X-Ms-Documentdb-Partitionkey") != `[2]` {
			t.Errorf("the headers are %v, want the page size 9 of the argument and the partition key [2] of the context", h)
		}
	})
	t.Run("WithPartitionKey takes a value of any type", func(t *testing.T) {
		t.Parallel()
		for _, tt := range []struct {
			key  any
			want string
		}{
			{"a", `["a"]`}, {1, `[1]`}, {1.5, `[1.5]`}, {true, `[true]`}, {nil, `[null]`},
		} {
			f := okFake()
			srv := httptest.NewServer(f)
			t.Cleanup(srv.Close)
			if err := drain(t, openFake(t, srv.URL, "c", ""), query, cosmos.WithPartitionKey(tt.key)); err != nil {
				t.Fatal(err)
			}
			if _, h := f.last(t); h.Get("X-Ms-Documentdb-Partitionkey") != tt.want {
				t.Errorf("WithPartitionKey(%#v) sent %q, want %q", tt.key, h.Get("X-Ms-Documentdb-Partitionkey"), tt.want)
			}
		}
	})
	t.Run("WithDatabase and WithContainer", func(t *testing.T) {
		t.Parallel()
		f := okFake()
		srv := httptest.NewServer(f)
		t.Cleanup(srv.Close)
		if err := drain(t, openFake(t, srv.URL, "c", ""), query, cosmos.WithDatabase("other db"), cosmos.WithContainer("kv")); err != nil {
			t.Fatal(err)
		}
		if want := "/dbs/other db/colls/kv/docs"; len(f.paths) != 1 || f.paths[0] != want {
			t.Errorf("the path is %q, want %q", f.paths, want)
		}
	})
	t.Run("WithParameter", func(t *testing.T) {
		t.Parallel()
		f := okFake()
		srv := httptest.NewServer(f)
		t.Cleanup(srv.Close)
		if err := drain(t, openFake(t, srv.URL, "c", ""), query,
			cosmos.WithParameter("query", "SELECT 1"), cosmos.WithParameter("x", map[string]any{"y": 1})); err != nil {
			t.Fatal(err)
		}
		if b, _ := f.last(t); b != `{"query":"SELECT 1","x":{"y":1}}` {
			t.Errorf("the body is %s, want the key query replaced and the key x added", b)
		}
	})
	t.Run("WithReadonly", func(t *testing.T) {
		t.Parallel()
		// The driver reads only, so a read-only statement runs, and so does a
		// statement that asks for no limit.
		f := okFake()
		srv := httptest.NewServer(f)
		t.Cleanup(srv.Close)
		for _, ro := range []bool{true, false} {
			if err := drain(t, openFake(t, srv.URL, "c", ""), query, cosmos.WithReadonly(ro)); err != nil {
				t.Fatalf("WithReadonly(%t): %v", ro, err)
			}
		}
	})
	t.Run("WithTimeout", func(t *testing.T) {
		t.Parallel()
		// The server has no setting for it, so a positive value fails, and
		// zero asks for nothing (D109).
		f := okFake()
		srv := httptest.NewServer(f)
		t.Cleanup(srv.Close)
		db := openFake(t, srv.URL, "c", "")
		if err := drain(t, db, query, cosmos.WithTimeout(time.Second)); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("WithTimeout(1s) = %v, want %v", err, dbimp.ErrNotSupported)
		}
		if err := drain(t, db, query, cosmos.WithTimeout(-time.Second)); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("WithTimeout(-1s) = %v, want %v", err, dbimp.ErrInvalidValue)
		}
		if f.requests() != 0 {
			t.Errorf("a statement with an option that fails sent %d requests, want none", f.requests())
		}
		if err := drain(t, db, query, cosmos.WithTimeout(0)); err != nil {
			t.Fatalf("WithTimeout(0): %v", err)
		}
	})
}

// TestOptionsThatTheDSNWouldRefuse holds D109: a value that the DSN would
// refuse fails the statement with dbimp.ErrInvalidValue, and sends nothing.
func TestOptionsThatTheDSNWouldRefuse(t *testing.T) {
	t.Parallel()
	f := okFake()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	db := openFake(t, srv.URL, "c", "")
	for name, opt := range map[string]cosmos.Option{
		"WithPageSize(-2)":          cosmos.WithPageSize(-2),
		"WithDatabase(\"\")":        cosmos.WithDatabase(""),
		"WithDatabase(\"a/b\")":     cosmos.WithDatabase("a/b"),
		"WithContainer(\"\")":       cosmos.WithContainer(""),
		"WithContainer(\"a\\\\b\")": cosmos.WithContainer(`a\b`),
		"WithPartitionKey(func)":    cosmos.WithPartitionKey(func() {}),
	} {
		if err := drain(t, db, "SELECT 1", opt); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("%s = %v, want %v", name, err, dbimp.ErrInvalidValue)
		}
	}
	if f.requests() != 0 {
		t.Errorf("the driver sent %d requests, want none", f.requests())
	}
}

// TestNoDatabase holds that a statement with no database or no container
// fails before it sends anything, and says what to do.
func TestNoDatabase(t *testing.T) {
	t.Parallel()
	f := okFake()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	for _, path := range []string{"", "/db"} {
		db, err := sql.Open(cosmos.Name, dsnFor(srv.URL, path, ""))
		if err != nil {
			t.Fatal(err)
		}
		err = drain(t, db, "SELECT 1")
		if !errors.Is(err, dbimp.ErrInvalidValue) || !strings.Contains(err.Error(), "WithContainer") {
			t.Errorf("the path %q gave %v, want %v with the advice", path, err, dbimp.ErrInvalidValue)
		}
		// The options give what the path lacks.
		if err := drain(t, db, "SELECT c.id FROM c", cosmos.WithDatabase("db"), cosmos.WithContainer("c")); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

// TestAValueTheDriverCannotBind holds that a value that neither the driver nor
// database/sql can bind fails the statement before it sends anything.
func TestAValueTheDriverCannotBind(t *testing.T) {
	t.Parallel()
	f := okFake()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	db := openFake(t, srv.URL, "c", "")
	if err := drain(t, db, "SELECT @p", sql.Named("p", struct{ A int }{1})); err == nil {
		t.Error("a struct was bound, want an error")
	}
	if f.requests() != 0 {
		t.Errorf("the driver sent %d requests, want none", f.requests())
	}
}
